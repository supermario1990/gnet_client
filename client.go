package gnet_client

import (
	"errors"
	"fmt"
	"github.com/smallnest/goframe"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout = time.Second * 10
)

type Call struct {
	done chan string
	Err  error
}

func (c *Call) Done() string {
	return <-c.done
}

type Client struct {
	address    string            // 服务器地址
	Conn       net.Conn          // 连接
	reconnect  bool              // 是否断线重连
	heartbeat  bool              // 是否支持心跳
	timeout    time.Duration     // 超时时间
	FrameConn  goframe.FrameConn // tcp组包拆包接口
	mutex      sync.Mutex        // 锁
	seq        uint64            // 消息序号
	msgPending map[uint64]*Call
	init       bool
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

func (cli *Client) ParseReply() {
	var err error
	var rep []byte
	for err == nil {
		rep, err = cli.FrameConn.ReadFrame()
		if err != nil {
			break
		}
		rs := strings.SplitN(string(rep), " ", 2)
		if len(rs) == 2 {
			req, _ := strconv.ParseUint(rs[0], 10, 64)
			msg := rs[1]

			call := cli.msgPending[req]
			call.done <- msg

		} else {
			err = errors.New("wrong reply format:" + string(rep))
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
	for _, option := range ops {
		option(cli)
	}

	network, address := parseAddr(addr)
	conn, err := net.DialTimeout(network, address, cli.timeout)
	if err != nil {
		return nil, err
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(3 * time.Minute)
	}

	cli.Conn = conn

	cli.msgPending = make(map[uint64]*Call)
	//go cli.parseReply()
	return cli, nil
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
}

func (cli *Client) makeReq(req string) string {
	return strconv.FormatUint(cli.seq, 10) + " " + req
}

// SyncCall send to server synchronously
func (cli *Client) SyncCall(req string) ([]byte, error) {
	cli.mutex.Lock()
	defer cli.mutex.Unlock()

	call := new(Call)
	call.done = make(chan string, 1)

	cli.seq++
	cli.msgPending[cli.seq] = call
	reqMsg := cli.makeReq(req)
	err := cli.FrameConn.WriteFrame([]byte(reqMsg))
	if err != nil {
		return nil, err
	}

	rep := <-call.done
	return []byte(rep), nil
}

// AsyncCall send to server asynchronously
func (cli *Client) AsyncCall(req string) *Call {
	cli.mutex.Lock()
	defer cli.mutex.Unlock()

	call := new(Call)
	call.done = make(chan string, 1)

	cli.seq++
	cli.msgPending[cli.seq] = call
	reqMsg := cli.makeReq(req)
	err := cli.FrameConn.WriteFrame([]byte(reqMsg))
	if err != nil {
		call.Err = err
		call.done <- err.Error()
	}

	return call
}

// Close close cli
func (cli *Client) Close() {
	cli.Conn.Close()
}
