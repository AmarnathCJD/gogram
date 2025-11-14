// Copyright (c) 2024 RoseLoverX

package transport

import (
	"context"
	"io"
	"net/url"
	"time"

	"github.com/amarnathcjd/gogram/internal/utils"
)

type ConnConfig any
type Conn io.ReadWriteCloser

type ModeConfig = func(Conn) (Mode, error)
type Mode interface {
	WriteMsg(msg []byte) error // this is not same as the io.Writer
	ReadMsg() ([]byte, error)
}

type TCPConnConfig struct {
	Ctx         context.Context
	Host        string
	IpV6        bool
	Timeout     time.Duration
	Socks       *url.URL
	LocalAddr   string
	ModeVariant uint8
	DC          int
	Logger      *utils.Logger
}

type WSConnConfig struct {
	Ctx         context.Context
	Host        string
	TLS         bool
	Timeout     time.Duration
	Socks       *url.URL
	LocalAddr   string
	ModeVariant uint8
	DC          int
	TestMode    bool
	Logger      *utils.Logger
}
