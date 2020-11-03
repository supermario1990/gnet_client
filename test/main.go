package main

import (
	"encoding/binary"
	"fmt"
	"github.com/smallnest/goframe"
	"github.com/supermario1990/gnet_client"
	"net/http"
	_ "net/http/pprof"
	"time"
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

	cli, err := gnet_client.NewCilent("tcp://127.0.0.1:9000",
		gnet_client.WithEncode(encoderConfig),
		gnet_client.WithDecode(decoderConfig))
	if err != nil {
		panic(err)
	}
	cli.Init()
	defer cli.Close()

	go http.ListenAndServe("0.0.0.0:6060", nil)
	for {
		rep, err := cli.SyncCall("hello")
		if err != nil {
			fmt.Println("SyncCall", err)
			time.Sleep(time.Second)
			continue
		}
		fmt.Println(string(rep))

		call, err := cli.AsyncCall("hello")
		if err != nil {
			fmt.Println("AsyncCall", err)
			time.Sleep(time.Second)
			continue
		} else {
			select {
			case rep1 := <-call.Done:
				fmt.Println(rep1)
			case <-cli.Quit.Done:
			}
		}

		time.Sleep(time.Second)
	}
}
