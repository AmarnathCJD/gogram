// Copyright (c) 2024 RoseLoverX

package transport

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

type tcpConn struct {
	reader  *Reader
	conn    *net.TCPConn
	timeout time.Duration
}

type TCPConnConfig struct {
	Ctx     context.Context
	Host    string
	IpV6    bool
	Timeout time.Duration
	Socks   *url.URL
}

func NewTCP(cfg TCPConnConfig) (Conn, error) {
	if cfg.Socks != nil && cfg.Socks.Host != "" {
		return newSocksTCP(cfg)
	}

	tcpPrefix := "tcp"
	if cfg.IpV6 && !strings.Contains(cfg.Host, ".") {
		tcpPrefix = "tcp6"
	}

	tcpAddr, err := net.ResolveTCPAddr(tcpPrefix, cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("resolving tcp: %w", err)
	}
	conn, err := net.DialTCP(tcpPrefix, nil, tcpAddr)
	if err != nil {
		return nil, fmt.Errorf("dialing tcp: %w", err)
	}

	return &tcpConn{
		reader:  NewReader(cfg.Ctx, conn),
		conn:    conn,
		timeout: cfg.Timeout,
	}, nil
}

func newSocksTCP(cfg TCPConnConfig) (Conn, error) {
	conn, err := dialProxy(cfg.Socks, cfg.Host)
	if err != nil {
		return nil, err
	}
	return &tcpConn{
		reader:  NewReader(cfg.Ctx, conn),
		conn:    conn.(*net.TCPConn),
		timeout: cfg.Timeout,
	}, nil
}

func (t *tcpConn) Close() error {
	return t.conn.Close()
}

func (t *tcpConn) Write(b []byte) (int, error) {
	return t.conn.Write(b)
}

func (t *tcpConn) Read(b []byte) (int, error) {
	if t.timeout > 0 {
		err := t.conn.SetReadDeadline(time.Now().Add(t.timeout))
		if err != nil {
			return 0, fmt.Errorf("setting read deadline: %w", err)
		}
	}

	n, err := t.reader.Read(b)
	if err != nil {
		if e, ok := err.(*net.OpError); ok || err == io.ErrClosedPipe {
			if e.Err.Error() == "i/o timeout" || err == io.ErrClosedPipe {
				return 0, fmt.Errorf("required to reconnect!: %w", err)
			}
		}
		switch err {
		case io.EOF, context.Canceled:
			return 0, err
		default:
			return 0, fmt.Errorf("unexpected error: %w", err)
		}
	}
	return n, nil
}
