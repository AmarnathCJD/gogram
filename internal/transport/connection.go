// Copyright (c) 2024 RoseLoverX

package transport

import (
	"context"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
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

	cfg.Host = strings.TrimPrefix(cfg.Host, ":")

	tcpAddr, err := net.ResolveTCPAddr(tcpPrefix, cfg.Host)
	if err != nil {
		return nil, errors.Wrap(err, "resolving tcp")
	}
	conn, err := net.DialTCP(tcpPrefix, nil, tcpAddr)
	// if there is a timeout error, wait 2 secs and retry (only once)
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		time.Sleep(2 * time.Second)
		conn, err = net.DialTCP(tcpPrefix, nil, tcpAddr)
	}

	if err != nil {
		return nil, errors.Wrap(err, "dialing tcp")
	}

	conn.SetKeepAlive(true)

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
			return 0, errors.Wrap(err, "setting read deadline")
		}
	}

	n, err := t.reader.Read(b)
	if err != nil {
		if e, ok := err.(*net.OpError); ok || err == io.ErrClosedPipe {
			if e.Err.Error() == "i/o timeout" || err == io.ErrClosedPipe {
				return 0, errors.Wrap(err, "required to reconnect!")
			}
		}
		switch err {
		case io.EOF, context.Canceled:
			return 0, err
		default:
			return 0, errors.Wrap(err, "unexpected error")
		}
	}
	return n, nil
}
