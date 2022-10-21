package transport

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/pkg/errors"
)

type tcpConn struct {
	cancelReader *CancelableReader
	conn         *net.TCPConn
	timeout      time.Duration
}

type TCPConnConfig struct {
	Ctx     context.Context
	Host    string
	Timeout time.Duration
	Socks   *Socks
}

func NewTCP(cfg TCPConnConfig) (Conn, error) {
	if cfg.Socks.Host != "" {
		return newSocksTCP(cfg)
	}
	tcpAddr, err := net.ResolveTCPAddr("tcp", cfg.Host)
	if err != nil {
		return nil, errors.Wrap(err, "resolving tcp")
	}
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return nil, errors.Wrap(err, "dialing tcp")
	}

	return &tcpConn{
		cancelReader: NewCancelableReader(cfg.Ctx, conn),
		conn:         conn,
		timeout:      cfg.Timeout,
	}, nil
}

func newSocksTCP(cfg TCPConnConfig) (Conn, error) {
	conn, err := DialProxy(cfg.Socks, "tcp", cfg.Host)
	if err != nil {
		return nil, err
	}
	return &tcpConn{
		cancelReader: NewCancelableReader(cfg.Ctx, conn),
		conn:         conn.(*net.TCPConn),
		timeout:      cfg.Timeout,
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

	n, err := t.cancelReader.Read(b)
	if err != nil {
		if e, ok := err.(*net.OpError); ok {
			if e.Err.Error() == "i/o timeout" {
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
