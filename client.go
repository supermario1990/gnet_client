package gnet_client

import (
	"errors"
	"fmt"
	"github.com/sasha-s/go-deadlock"
	"github.com/smallnest/goframe"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout   = time.Second * 10
	heartbeatTimeout = time.Second * 10
)

// 基于文本的消息协议
// MSG_SEQ 		消息系列
// MSG_TYPE 	消息类型
// MSG_BODY 	消息体
const (
	MSG_SEQ int = iota
	MSG_TYPE
	MSG_BODY
)

const (
	MSG_TYPE_NORMAL    = "1" // 正常消息 序号从1开始连续递增
	MSG_TYPE_HEARTBEAT = "2" // 心跳消息 序号为0
)

type status int

const (
	STATUS_CONNECTED status = iota
	STATUS_CLOSED
)

type Call struct {
	Done chan string
	Err  error
}

type Client struct {
	address       string            // 服务器地址
	Conn          net.Conn          // 连接
	reconnect     bool              // 是否断线重连
	heartbeat     bool              // 是否支持心跳
	timeout       time.Duration     // 超时时间
	FrameConn     goframe.FrameConn // tcp组包拆包接口
	mutex         deadlock.Mutex    // 锁
	seq           uint64            // 消息序号
	msgPending    map[uint64]*Call
	init          bool
	Quit          *Call
	heartbeatTime time.Time
	connStatus    status
	encodeConfig  goframe.EncoderConfig
	decodeConfig  goframe.DecoderConfig
}
type Option func(*Client)

func WithReconnect(reconnect bool) Option {
	return func(client *Client) {
		client.reconnect = reconnect
	}
}

func WithHeartbeat(heartbeat bool) Option {
	return func(client *Client) {
		client.heartbeat = heartbeat
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(client *Client) {
		client.timeout = timeout
	}
}

func WithEncode(encode goframe.EncoderConfig) Option {
	return func(client *Client) {
		client.encodeConfig = encode
	}
}

func WithDecode(decode goframe.DecoderConfig) Option {
	return func(client *Client) {
		client.decodeConfig = decode
	}
}

func parseAddr(addr string) (network, address string) {
	network = "tcp"
	address = strings.ToLower(addr)
	if strings.Contains(address, "://") {
		pair := strings.Split(address, "://")
		network = pair[0]
		address = pair[1]
	}
	return
}

// 心跳 “0 2 ok”
func (cli *Client) heartbeatGo() {
	t := time.NewTicker(time.Second * 3)
	for {
		<-t.C

		if cli.getStatus() == STATUS_CLOSED {
			continue
		}
		heartbeatMsg := "0 " + MSG_TYPE_HEARTBEAT + " ok"
		cli.mutex.Lock()
		cli.FrameConn.WriteFrame([]byte(heartbeatMsg))
		cli.mutex.Unlock()

		// 心跳超时了
		if cli.heartbeatTime.Add(heartbeatTimeout).Before(time.Now()) {
			fmt.Println("心跳超时，无法连接服务器")
			if err := cli.Close(); err != nil {
				panic(err)
			}
		}
	}
}

func (cli *Client) reConn() {
	t := time.NewTicker(time.Second * 1)
	for {
		<-t.C
		if cli.getStatus() == STATUS_CLOSED {
			fmt.Println("开始重连", cli.address)
			err := cli.connect()
			if err != nil {
				fmt.Println("重连失败:", err)
			} else {
				fmt.Println("重连成功")
			}
		}
	}
}

func (cli *Client) ParseReply() {
	var err error
	var rep []byte
	for {
		status := cli.getStatus()
		fmt.Println("接受协程:", status)
		if status == STATUS_CLOSED {
			time.Sleep(time.Second)
			continue
		}
		rep, err = cli.FrameConn.ReadFrame()
		if err != nil {
			fmt.Println("接受数据失败：", err)
			close(cli.Quit.Done)
			continue
		}
		rs := strings.SplitN(string(rep), " ", 3)
		if len(rs) == 3 {
			req, _ := strconv.ParseUint(rs[MSG_SEQ], 10, 64)
			msgType := rs[MSG_TYPE]
			msg := rs[MSG_BODY]

			switch msgType {
			case MSG_TYPE_NORMAL:
				call := cli.msgPending[req]
				call.Done <- msg
			case MSG_TYPE_HEARTBEAT:
				fmt.Println("收到心跳消息")
				cli.heartbeatTime = time.Now()
			default:
				fmt.Println("收到错误的消息类型:", msgType)
			}
		} else {
			fmt.Println("收到错误的消息格式:", string(rep))
		}
	}
}

// NewClient new a Client
// addr connect string
// ops option to client
func NewCilent(addr string, ops ...Option) (*Client, error) {
	cli := new(Client)
	cli.timeout = defaultTimeout
	cli.reconnect = true
	cli.heartbeat = true
	cli.address = addr
	for _, option := range ops {
		option(cli)
	}

	if err := cli.connect(); err != nil {
		return nil, err
	}

	cli.msgPending = make(map[uint64]*Call)
	cli.Quit = new(Call)
	cli.Quit.Done = make(chan string, 1)
	return cli, nil
}

func (cli *Client) connect() error {
	cli.mutex.Lock()
	defer cli.mutex.Unlock()
	network, address := parseAddr(cli.address)
	conn, err := net.DialTimeout(network, address, cli.timeout)
	if err != nil {
		return err
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(3 * time.Minute)
	}

	cli.Conn = conn
	cli.FrameConn = goframe.NewLengthFieldBasedFrameConn(cli.encodeConfig, cli.decodeConfig, cli.Conn)
	cli.ChangeStatus(STATUS_CONNECTED)
	cli.heartbeatTime = time.Now()
	return nil
}

func (cli *Client) Init() {
	cli.mutex.Lock()
	defer cli.mutex.Unlock()
	if cli.init {
		return
	}
	cli.init = true

	go cli.ParseReply()

	if cli.reconnect {
		go cli.reConn()
	}

	if cli.heartbeat {
		go cli.heartbeatGo()
	}
}

func (cli *Client) makeReq(req string) string {
	return strconv.FormatUint(cli.seq, 10) + " " + MSG_TYPE_NORMAL + " " + req
}

// SyncCall send to server synchronously
func (cli *Client) SyncCall(req string) ([]byte, error) {

	if cli.getStatus() != STATUS_CONNECTED {
		return nil, errors.New("SyncCall 连接处于断开状态")
	}

	cli.mutex.Lock()
	call := new(Call)
	call.Done = make(chan string, 1)
	cli.seq++
	cli.msgPending[cli.seq] = call
	reqMsg := cli.makeReq(req)
	err := cli.FrameConn.WriteFrame([]byte(reqMsg))
	cli.mutex.Unlock()

	if err != nil {
		return nil, err
	}

	select {
	case rep := <-call.Done:
		return []byte(rep), nil
	case <-cli.Quit.Done:
		return nil, cli.Quit.Err
	}
}

// AsyncCall send to server asynchronously
func (cli *Client) AsyncCall(req string) (*Call, error) {

	call := new(Call)
	call.Done = make(chan string, 1)
	if cli.getStatus() != STATUS_CONNECTED {
		return call, errors.New("AsyncCall 连接处于断开状态")
	}

	cli.mutex.Lock()
	cli.seq++
	cli.msgPending[cli.seq] = call
	reqMsg := cli.makeReq(req)
	err := cli.FrameConn.WriteFrame([]byte(reqMsg))
	cli.mutex.Unlock()

	if err != nil {
		call.Err = err
		call.Done <- err.Error()
	}

	return call, nil
}

// Close close cli
func (cli *Client) Close() error {

	if cli.getStatus() == STATUS_CLOSED {
		return nil
	}

	cli.mutex.Lock()
	if err := cli.Conn.Close(); err != nil {
		fmt.Println("关闭连接失败:", err)
		cli.mutex.Unlock()
		return err
	}
	cli.ChangeStatus(STATUS_CLOSED)
	cli.mutex.Unlock()

	return nil
}

func (cli *Client) ChangeStatus(s status) {
	cli.connStatus = s
}
func (cli *Client) getStatus() status {
	return cli.connStatus
}
