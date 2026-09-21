// Copyright (c) 2025 @AmarnathCJD

package transport

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const DefaultTimeout = 5 * time.Second

func dialProxyContext(ctx context.Context, s *url.URL, address, localAddr string) (result net.Conn, err error) {
	if s == nil {
		return nil, fmt.Errorf("missing proxy URL")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	dialer := net.Dialer{Timeout: DefaultTimeout}
	if localAddr != "" {
		dialer.LocalAddr, err = net.ResolveTCPAddr("tcp", localAddr)
		if err != nil {
			return nil, fmt.Errorf("invalid local address: %w", err)
		}
	}
	port := s.Port()
	if port == "" {
		port = "1080"
		if s.Scheme == "http" {
			port = "8080"
		}
	}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.Hostname(), port))
	if err != nil {
		return nil, err
	}
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer func() {
		stopped := stop()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			err = context.DeadlineExceeded
		}
		if err == nil && (!stopped || ctx.Err() != nil) {
			err = ctx.Err()
			if err == nil {
				err = context.Canceled
			}
		}
		if err != nil {
			_ = conn.Close()
			result = nil
		} else {
			err = conn.SetDeadline(time.Time{})
			if err != nil {
				_ = conn.Close()
				result = nil
			}
		}
	}()
	switch s.Scheme {
	case "socks5":
		err = socks5Handshake(conn, s, address)
	case "socks4":
		err = socks4Handshake(conn, address)
	case "http":
		return httpProxyHandshake(conn, s, address)
	default:
		err = fmt.Errorf("unsupported proxy scheme: %s", s.Scheme)
	}
	return conn, err
}

type bufferedProxyConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedProxyConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func httpProxyHandshake(conn net.Conn, s *url.URL, address string) (net.Conn, error) {
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: address}, Host: address, Header: make(http.Header)}
	if s.User != nil {
		password, _ := s.User.Password()
		auth := base64.StdEncoding.EncodeToString([]byte(s.User.Username() + ":" + password))
		req.Header.Set("Proxy-Authorization", "Basic "+auth)
	}
	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("proxy CONNECT: %w", err)
	}
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, req)
	if err != nil {
		return nil, fmt.Errorf("proxy response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy CONNECT status %d", resp.StatusCode)
	}
	return &bufferedProxyConn{Conn: conn, reader: r}, nil
}

func socks5Handshake(conn net.Conn, s *url.URL, address string) error {
	methods := []byte{5, 1, 0}
	if s.User != nil {
		methods = []byte{5, 2, 0, 2}
	}
	if err := writeProxyData(conn, methods); err != nil {
		return err
	}
	var reply [4]byte
	if _, err := io.ReadFull(conn, reply[:2]); err != nil {
		return err
	}
	if reply[0] != 5 {
		return fmt.Errorf("invalid SOCKS version: %d", reply[0])
	}
	switch reply[1] {
	case 0:
	case 2:
		if s.User == nil {
			return fmt.Errorf("SOCKS proxy requires credentials")
		}
		username := s.User.Username()
		password, _ := s.User.Password()
		if len(username) == 0 || len(username) > 255 || len(password) > 255 {
			return fmt.Errorf("invalid SOCKS credential length")
		}
		data := append([]byte{1, byte(len(username))}, username...)
		data = append(data, byte(len(password)))
		data = append(data, password...)
		if err := writeProxyData(conn, data); err != nil {
			return err
		}
		if _, err := io.ReadFull(conn, reply[:2]); err != nil {
			return err
		}
		if reply[0] != 1 || reply[1] != 0 {
			return fmt.Errorf("SOCKS authentication failed")
		}
	default:
		return fmt.Errorf("unsupported SOCKS authentication method: %d", reply[1])
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil || p == 0 {
		return fmt.Errorf("invalid target port: %s", port)
	}
	request := []byte{5, 1, 0}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			request = append(request, 1)
			request = append(request, ip4...)
		} else {
			request = append(request, 4)
			request = append(request, ip.To16()...)
		}
	} else {
		if len(host) == 0 || len(host) > 255 {
			return fmt.Errorf("invalid SOCKS hostname length")
		}
		request = append(request, 3, byte(len(host)))
		request = append(request, host...)
	}
	request = binary.BigEndian.AppendUint16(request, uint16(p))
	if err := writeProxyData(conn, request); err != nil {
		return err
	}
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return err
	}
	if reply[0] != 5 || reply[1] != 0 || reply[2] != 0 {
		return fmt.Errorf("SOCKS connect failed: %v", reply[:3])
	}
	var length int
	switch reply[3] {
	case 1:
		length = 4
	case 4:
		length = 16
	case 3:
		if _, err := io.ReadFull(conn, reply[:1]); err != nil {
			return err
		}
		length = int(reply[0])
	default:
		return fmt.Errorf("unsupported SOCKS address type: %d", reply[3])
	}
	// Include the bound port; do not slice a four-byte header to 16/255 bytes.
	_, err = io.CopyN(io.Discard, conn, int64(length+2))
	return err
}

func socks4Handshake(conn net.Conn, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return fmt.Errorf("SOCKS4 requires an IPv4 target")
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil || p == 0 {
		return fmt.Errorf("invalid target port: %s", port)
	}
	request := []byte{4, 1, byte(p >> 8), byte(p), ip[0], ip[1], ip[2], ip[3], 0}
	if err := writeProxyData(conn, request); err != nil {
		return err
	}
	var reply [8]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return err
	}
	if reply[0] != 0 || reply[1] != 90 {
		return fmt.Errorf("SOCKS4 connect failed: %d", reply[1])
	}
	return nil
}

func writeProxyData(w io.Writer, data []byte) error {
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}
