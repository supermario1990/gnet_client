package gnet_client

import (
	"errors"
)

type ServerError string

func (e ServerError) Error() string {
	return string(e)
}

var ErrShutdown = errors.New("connection is shut down")

type Call struct {
	ServiceMethod string
	args          interface{}
	reply         interface{}
	Error         error
	Done          chan *Call
}
