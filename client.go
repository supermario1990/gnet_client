package gnet_client

import (
	"errors"
	"github.com/smallnest/goframe"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout = time.Second * 10
)

type Client struct {
	address   string            // 服务器地址
	conn      net.Conn          // 连接
	reconnect bool              // 是否断线重连
	heartbeat bool              // 是否支持心跳
	timeout   time.Duration     // 超时时间
	frameConn goframe.FrameConn // tcp组包拆包接口
	mutex     sync.Mutex
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

func WithFrameConn(frameConn goframe.FrameConn) Option {
	return func(client *Client) {
		client.frameConn = frameConn
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

	if cli.frameConn == nil {
		return nil, errors.New("frameConn must be assigned")
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

	cli.conn = conn

	return cli, nil
}

// SyncCall send to server synchronously
func (cli *Client) SyncCall(req string) ([]byte, error) {
	cli.mutex.Lock()
	defer cli.mutex.Unlock()
	err := cli.frameConn.WriteFrame([]byte(req))
	if err != nil {
		return nil, err
	}
	rep, err := cli.frameConn.ReadFrame()
	if err != nil {
		return nil, err
	}

	return rep, nil
}

// AsyncCall send to server asynchronously
func (cli *Client) AsyncCall() {
	cli.mutex.Lock()
	defer cli.mutex.Unlock()
}

// Close close cli
func (cli *Client) Close() {
	cli.conn.Close()
}
