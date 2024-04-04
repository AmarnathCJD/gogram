// Copyright (c) 2024 RoseLoverX

package transport

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"time"
)

const (
	DefaultTimeout = 5 * time.Second
)

func dialProxy(s *url.URL, address string) (net.Conn, error) {
	switch s.Scheme {
	case "socks5":
		return dialSocks5(s, address)
	case "socks4":
		return dialSocks4(s, address)
	case "http":
		return dialHTTP(s, address)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", s.Scheme)
	}
}

func dialHTTP(s *url.URL, addr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", s.Hostname()+":"+s.Port(), DefaultTimeout)
	if err != nil {
		return nil, err
	}
	_, err = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", addr, addr)
	if err != nil {
		return nil, err
	}
	if s.User != nil && s.User.Username() != "" {
		username := s.User.Username()
		password, _ := s.User.Password()
		_, err = fmt.Fprintf(conn, "Proxy-Authorization: Basic %s\r\n", basicAuth(username, password))
		if err != nil {
			return nil, err
		}
	}
	_, err = fmt.Fprint(conn, "\r\n")
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 12)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return nil, err
	}
	if string(buf[:9]) != "HTTP/1.1 " {
		return nil, errors.New("http connect failed")
	}
	if string(buf[9:12]) != "200" {
		return nil, errors.New("http connect failed")
	}
	return conn, nil
}

func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

func dialSocks5(s *url.URL, addr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", s.Hostname()+":"+s.Port(), DefaultTimeout)
	if err != nil {
		return nil, err
	}
	// auth
	if s.User != nil && s.User.Username() != "" {
		username := s.User.Username()
		password, _ := s.User.Password()
		_, err = conn.Write([]byte{5, 2, 0, 2})
		if err != nil {
			return nil, err
		}
		buf := make([]byte, 2)
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return nil, err
		}
		if buf[0] != 5 {
			return nil, errors.New("socks version not supported")
		}
		if buf[1] == 0 {
			// no auth
		} else if buf[1] == 2 {
			// username/password
			_, err = conn.Write(append([]byte{1, byte(len(username))}, []byte(username)...))
			if err != nil {
				return nil, err
			}
			_, err = conn.Write(append([]byte{byte(len(password))}, []byte(password)...))
			if err != nil {
				return nil, err
			}
			buf = make([]byte, 2)
			_, err = io.ReadFull(conn, buf)
			if err != nil {
				return nil, err
			}
			if buf[0] != 1 {
				return nil, errors.New("socks version not supported")
			}
			if buf[1] != 0 {
				return nil, errors.New("socks auth failed")
			}
		} else {
			return nil, errors.New("socks auth method not supported")
		}
	} else {
		_, err = conn.Write([]byte{5, 1, 0})
		if err != nil {
			return nil, err
		}
		buf := make([]byte, 2)
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return nil, err
		}
		if buf[0] != 5 {
			return nil, errors.New("socks version not supported")
		}
		if buf[1] != 0 {
			return nil, errors.New("socks auth failed")
		}
	}
	// connect
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	var atyp byte
	var dst []byte
	if ip == nil {
		atyp = 3
		dst = append([]byte{byte(len(host))}, []byte(host)...)
	}
	if ip4 := ip.To4(); ip4 != nil {
		atyp = 1
		dst = ip4
	}
	if ip6 := ip.To16(); ip6 != nil {
		atyp = 4
		dst = ip6
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}
	_, err = conn.Write([]byte{5, 1, 0, atyp})
	if err != nil {
		return nil, err
	}
	_, err = conn.Write(append(dst, byte(p>>8), byte(p)))
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 4)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return nil, err
	}
	if buf[0] != 5 {
		return nil, errors.New("socks version not supported")
	}
	if buf[1] != 0 {
		return nil, errors.New("socks connect failed")
	}
	switch buf[3] {
	case 1:
		_, err = io.ReadFull(conn, buf[:4])
		if err != nil {
			return nil, err
		}
	case 3:
		_, err = io.ReadFull(conn, buf[:1])
		if err != nil {
			return nil, err
		}
		n := int(buf[0])
		_, err = io.ReadFull(conn, buf[:n])
		if err != nil {
			return nil, err
		}
	case 4:
		_, err = io.ReadFull(conn, buf[:16])
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("socks address type not supported")
	}
	_, err = io.ReadFull(conn, buf[:2])
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func dialSocks4(s *url.URL, addr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", s.Hostname()+":"+s.Port(), DefaultTimeout)
	if err != nil {
		return nil, err
	}
	// connect
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, errors.New("socks4 only support ip address")
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, errors.New("socks4 only support ipv4 address")
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}
	_, err = conn.Write([]byte{4, 1, byte(p >> 8), byte(p), ip4[0], ip4[1], ip4[2], ip4[3], 0})
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 8)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return nil, err
	}
	if buf[0] != 0 {
		return nil, errors.New("socks version not supported")
	}
	if buf[1] != 90 {
		return nil, errors.New("socks connect failed")
	}
	return conn, nil
}
