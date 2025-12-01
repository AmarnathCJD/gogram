// Copyright (c) 2025 @AmarnathCJD

package transport

import (
	"context"
	"io"
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

// CommonConfig contains shared configuration fields for all transport types
type CommonConfig struct {
	Ctx         context.Context
	Host        string
	Timeout     time.Duration
	Socks       *utils.Proxy
	LocalAddr   string
	ModeVariant uint8
	DC          int
	Logger      *utils.Logger
}

type TCPConnConfig struct {
	CommonConfig
	IpV6 bool
}

type WSConnConfig struct {
	CommonConfig
	TLS      bool
	TestMode bool
}
