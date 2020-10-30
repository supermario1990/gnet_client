package main

import (
	"encoding/binary"
	"fmt"
	"github.com/smallnest/goframe"
	"github.com/supermario1990/gnet_client"
)

func main() {

	encoderConfig := goframe.EncoderConfig{
		ByteOrder:                       binary.BigEndian,
		LengthFieldLength:               4,
		LengthAdjustment:                0,
		LengthIncludesLengthFieldLength: false,
	}

	decoderConfig := goframe.DecoderConfig{
		ByteOrder:           binary.BigEndian,
		LengthFieldOffset:   0,
		LengthFieldLength:   4,
		LengthAdjustment:    0,
		InitialBytesToStrip: 4,
	}

	cli, err := gnet_client.NewCilent("tcp://127.0.0.1:9000")
	if err != nil {
		panic(err)
	}
	fc := goframe.NewLengthFieldBasedFrameConn(encoderConfig, decoderConfig, cli.Conn)
	cli.Init(fc)

	rep, _ := cli.SyncCall("hello")
	fmt.Println(string(rep))

	call := cli.AsyncCall("hello")
	rep1 := call.Done()
	fmt.Println(rep1)
}
