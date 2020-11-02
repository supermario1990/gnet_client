package gnet_client

import (
	"errors"
	"fmt"
	"github.com/smallnest/goframe"
	"go.uber.org/atomic"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout   = time.Second * 10
	heartbeatTimeout = time.Second * 6
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

const (
	STATUS_CONNECTED int32 = iota
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
	mutex         sync.Mutex        // 锁
	seq           uint64            // 消息序号
	msgPending    map[uint64]*Call
	init          bool
	Quit          *Call
	heartbeatTime time.Time
	status        atomic.Int32
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
		heartbeatMsg := "0 " + MSG_TYPE_HEARTBEAT + " ok"
		cli.FrameConn.WriteFrame([]byte(heartbeatMsg))
	}
}

func (cli *Client) reConn() {
	t := time.NewTicker(time.Second * 1)
	for {
		<-t.C
		if cli.status.Load() == STATUS_CLOSED {
			fmt.Println("reconnect to server:", cli.address)
			err := cli.connect()
			if err != nil {
				fmt.Println("reconnect failed:", err)
			} else {
				cli.status.Store(STATUS_CONNECTED)
			}
		}
	}
}

func (cli *Client) ParseReply() {
	var err error
	var rep []byte
	for err == nil {
		rep, err = cli.FrameConn.ReadFrame()
		if err != nil {
			cli.Quit.Err = err
			cli.Quit.Done <- ""
			break
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
				fmt.Println("receive heartbeat message")
				// 心跳超时了
				if cli.heartbeatTime.Add(heartbeatTimeout).Before(time.Now()) {
					fmt.Println("server is shutdown")
					if err := cli.Close(); err != nil {
						panic(err)
					}
				}
				cli.heartbeatTime = time.Now()
			default:
				fmt.Println("receive wrong message type:", msgType)
			}
		} else {
			err = errors.New("wrong reply format:" + string(rep))
			cli.Quit.Err = err
			cli.Quit.Done <- ""
		}
	}
	if err != nil {
		fmt.Println(err)
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
	cli.heartbeatTime = time.Now()
	return cli, nil
}

func (cli *Client) connect() error {
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
	cli.status.Store(STATUS_CONNECTED)
	return nil
}

func (cli *Client) Init(fc goframe.FrameConn) {
	cli.mutex.Lock()
	defer cli.mutex.Unlock()
	if cli.init {
		return
	}
	cli.FrameConn = fc
	go cli.ParseReply()
	cli.init = true

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
	cli.mutex.Lock()
	defer cli.mutex.Unlock()

	if cli.status.Load() != STATUS_CONNECTED {
		return nil, errors.New("connect failed")
	}

	call := new(Call)
	call.Done = make(chan string, 1)

	cli.seq++
	cli.msgPending[cli.seq] = call
	reqMsg := cli.makeReq(req)
	err := cli.FrameConn.WriteFrame([]byte(reqMsg))
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
func (cli *Client) AsyncCall(req string) *Call {
	cli.mutex.Lock()
	defer cli.mutex.Unlock()

	call := new(Call)
	if cli.status.Load() != STATUS_CONNECTED {
		call.Err = errors.New("connect failed")
		return call
	}
	call.Done = make(chan string, 1)

	cli.seq++
	cli.msgPending[cli.seq] = call
	reqMsg := cli.makeReq(req)
	err := cli.FrameConn.WriteFrame([]byte(reqMsg))
	if err != nil {
		call.Err = err
		call.Done <- err.Error()
	}

	return call
}

// Close close cli
func (cli *Client) Close() error {
	cli.mutex.Lock()
	defer cli.mutex.Unlock()
	if err := cli.Conn.Close(); err != nil {
		fmt.Println("close connection failed:", err)
		return err
	}
	cli.status.Store(STATUS_CLOSED)
	return nil
}
