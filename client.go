package gnet_client

import "net"

type Client struct {
	address   string    // 服务器地址
	conn      *net.Conn // 连接
	reconnect bool      // 是否断线重连
	heartbeat bool      // 是否支持心跳
}
