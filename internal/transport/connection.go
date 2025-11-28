// Copyright (c) 2024 RoseLoverX

package transport

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

type tcpConn struct {
	reader  *Reader
	conn    *net.TCPConn
	timeout time.Duration
}

func NewTCP(cfg TCPConnConfig) (Conn, bool, error) {
	if cfg.Socks != nil && !cfg.Socks.IsEmpty() {
		if cfg.Socks.Type == "mtproxy" {
			return newMTProxyTCP(cfg)
		}
		return newSocksTCP(cfg)
	}

	if cfg.Logger != nil {
		cfg.Logger.Trace("[tcp] connecting to %s", cfg.Host)
	}

	tcpPrefix := "tcp"
	if cfg.IpV6 && !strings.Contains(cfg.Host, ".") {
		tcpPrefix = "tcp6"
	}

	cfg.Host = strings.TrimPrefix(cfg.Host, ":")

	tcpAddr, err := net.ResolveTCPAddr(tcpPrefix, cfg.Host)
	if err != nil {
		return nil, false, fmt.Errorf("resolving tcp addr: %w", err)
	}

	// Resolve a local address if provided
	var localAddr *net.TCPAddr
	if cfg.LocalAddr != "" {
		localAddr, err = net.ResolveTCPAddr(tcpPrefix, cfg.LocalAddr)
		if err != nil {
			return nil, false, fmt.Errorf("resolving local tcp addr: %w", err)
		}
	}

	conn, err := net.DialTCP(tcpPrefix, localAddr, tcpAddr)
	// if there is a timeout error, wait 2 secs and retry (only once)
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		time.Sleep(2 * time.Second)
		conn, err = net.DialTCP(tcpPrefix, localAddr, tcpAddr)
	}

	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.WithError(err).Error("[tcp] connection failed")
		}
		return nil, false, fmt.Errorf("dialing tcp: %w", err)
	}

	conn.SetKeepAlive(true)

	if cfg.Logger != nil {
		cfg.Logger.Trace("[tcp] connected to %s", cfg.Host)
	}

	return &tcpConn{
		reader:  NewReader(cfg.Ctx, conn),
		conn:    conn,
		timeout: cfg.Timeout,
	}, false, nil
}

func newSocksTCP(cfg TCPConnConfig) (Conn, bool, error) {
	if cfg.Logger != nil {
		cfg.Logger.Debug("[%s] connecting to %s via proxy %s", cfg.Socks.Type, cfg.Host, cfg.Socks.Host)
	}

	conn, err := dialProxy(cfg.Socks.ToURL(), cfg.Host, cfg.LocalAddr)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Error("[%s] connection failed: %v", cfg.Socks.Type, err)
		}
		return nil, false, err
	}

	if cfg.Logger != nil {
		cfg.Logger.Debug("[%s] connected to %s", cfg.Socks.Type, cfg.Host)
	}

	return &tcpConn{
		reader:  NewReader(cfg.Ctx, conn),
		conn:    conn.(*net.TCPConn),
		timeout: cfg.Timeout,
	}, false, nil
}

func newMTProxyTCP(cfg TCPConnConfig) (Conn, bool, error) {
	dcID := int16(cfg.DC)
	if dcID == 0 {
		dcID = 2
	}

	conn, err := DialMTProxy(cfg.Socks, cfg.Host, dcID, cfg.ModeVariant, cfg.LocalAddr, cfg.Logger)
	if err != nil {
		return nil, false, fmt.Errorf("establishing mtproxy connection: %w", err)
	}

	return conn, true, nil
}

func (t *tcpConn) Close() error {
	if t.reader != nil {
		t.reader.Close()
	}
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
				return 0, fmt.Errorf("required to reconnect: %w", err)
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
