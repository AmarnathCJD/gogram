// Copyright (c) 2025 @AmarnathCJD

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
	conn    net.Conn
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

	var localAddr net.Addr
	if cfg.LocalAddr != "" {
		resolved, err := net.ResolveTCPAddr(tcpPrefix, cfg.LocalAddr)
		if err != nil {
			return nil, false, fmt.Errorf("resolving local tcp addr: %w", err)
		}
		localAddr = resolved
	}

	dialer := net.Dialer{Timeout: cfg.Timeout, LocalAddr: localAddr}
	rawConn, err := dialer.DialContext(cfg.Ctx, tcpPrefix, cfg.Host)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.WithError(err).Debug("[tcp] connection failed")
		}
		return nil, false, fmt.Errorf("dialing tcp: %w", err)
	}
	conn, ok := rawConn.(*net.TCPConn)
	if !ok {
		rawConn.Close()
		return nil, false, fmt.Errorf("dialing tcp: unexpected connection type %T", rawConn)
	}

	conn.SetKeepAlive(true)

	if cfg.Logger != nil {
		cfg.Logger.Trace("[tcp] connected to %s", cfg.Host)
	}

	tc := &tcpConn{
		reader:  NewReader(cfg.Ctx, conn),
		conn:    conn,
		timeout: cfg.Timeout,
	}

	if cfg.Obfuscated {
		obf, err := NewObfuscatedConn(tc, ProtocolID(cfg.ModeVariant))
		if err != nil {
			tc.Close()
			return nil, false, fmt.Errorf("obfuscating tcp: %w", err)
		}
		return &obfuscatedTCP{obf: obf, tc: tc, timeout: cfg.Timeout}, true, nil
	}

	return tc, false, nil
}

type obfuscatedTCP struct {
	obf     *obfuscatedConn
	tc      *tcpConn
	timeout time.Duration
}

func (o *obfuscatedTCP) Read(b []byte) (int, error) {
	return o.obf.Read(b)
}

func (o *obfuscatedTCP) Write(b []byte) (int, error) {
	return o.obf.Write(b)
}

func (o *obfuscatedTCP) Close() error {
	return o.tc.Close()
}

func newSocksTCP(cfg TCPConnConfig) (Conn, bool, error) {
	if cfg.Logger != nil {
		cfg.Logger.Debug("[%s] connecting to %s via proxy %s", cfg.Socks.Type, cfg.Host, cfg.Socks.Host)
	}

	conn, err := dialProxyContext(cfg.Ctx, cfg.Socks.ToURL(), cfg.Host, cfg.LocalAddr)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Debug("[%s] connection failed: %v", cfg.Socks.Type, err)
		}
		return nil, false, err
	}

	if cfg.Logger != nil {
		cfg.Logger.Debug("[%s] connected to %s", cfg.Socks.Type, cfg.Host)
	}

	tc := &tcpConn{
		reader:  NewReader(cfg.Ctx, conn),
		conn:    conn,
		timeout: cfg.Timeout,
	}
	if cfg.Obfuscated {
		obf, err := NewObfuscatedConn(tc, ProtocolID(cfg.ModeVariant))
		if err != nil {
			_ = tc.Close()
			return nil, false, fmt.Errorf("obfuscating proxy connection: %w", err)
		}
		return &obfuscatedTCP{obf: obf, tc: tc, timeout: cfg.Timeout}, true, nil
	}
	return tc, false, nil
}

func newMTProxyTCP(cfg TCPConnConfig) (Conn, bool, error) {
	dcID := int16(cfg.DC)
	if dcID == 0 {
		dcID = 2
	}

	conn, err := DialMTProxy(cfg.Ctx, cfg.Socks, cfg.Host, dcID, cfg.ModeVariant, cfg.LocalAddr, cfg.Logger)
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
	if t.timeout > 0 {
		if err := t.conn.SetWriteDeadline(time.Now().Add(t.timeout)); err != nil {
			return 0, err
		}
	}
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
		if e, ok := err.(*net.OpError); ok {
			if e.Err.Error() == "i/o timeout" {
				return n, fmt.Errorf("required to reconnect: %w", err)
			}
		} else if err == io.ErrClosedPipe {
			return n, fmt.Errorf("required to reconnect: %w", err)
		}
		switch err {
		case io.EOF, context.Canceled:
			return n, err
		default:
			return n, fmt.Errorf("unexpected error: %w", err)
		}
	}
	return n, nil
}
