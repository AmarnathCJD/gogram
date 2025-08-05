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
	"strings"
	"time"
)

const DefaultTimeout = 5 * time.Second

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

func dialHTTP(s *url.URL, targetAddr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", s.Host, DefaultTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to proxy: %v", err)
	}

	passwd, _ := s.User.Password()

	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", s.User.Username(), passwd)))
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nProxy-Authorization: Basic %s\r\n\r\n", targetAddr, auth)
	_, err = conn.Write([]byte(connectReq))
	if err != nil {
		return nil, fmt.Errorf("failed to send CONNECT request: %v", err)
	}

	buffer := make([]byte, 1024)
	n, _ := conn.Read(buffer)
	if !strings.Contains(string(buffer[:n]), "200") {
		conn.Close()
		return nil, fmt.Errorf("proxy tunnel failed: %s", strings.TrimSpace(string(buffer[:n])))
	}

	return conn, nil
}

func dialSocks5(s *url.URL, addr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", s.Hostname()+":"+s.Port(), DefaultTimeout)
	if err != nil {
		return nil, err
	}

	// Authentication
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
			// No authentication required
		} else if buf[1] == 2 {
			// Username/password authentication
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
				return nil, errors.New("socks authentication failed")
			}
		} else {
			return nil, errors.New("socks authentication method not supported")
		}
	} else {
		// No authentication required
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
			return nil, errors.New("socks authentication failed")
		}
	}

	// Connect to the target address
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ip := net.ParseIP(host)
	var atyp byte
	var dst []byte

	if ip == nil {
		atyp = 3 // Domain name
		dst = append([]byte{byte(len(host))}, []byte(host)...)
	} else if ip4 := ip.To4(); ip4 != nil {
		atyp = 1 // IPv4 address
		dst = ip4
	} else if ip6 := ip.To16(); ip6 != nil {
		atyp = 4 // IPv6 address
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
		return nil, errors.New("socks connection failed")
	}

	switch buf[3] {
	case 1: // IPv4 address
		_, err = io.ReadFull(conn, buf[:4])
		if err != nil {
			return nil, err
		}
	case 3: // Domain name
		_, err = io.ReadFull(conn, buf[:1])
		if err != nil {
			return nil, err
		}
		n := int(buf[0])
		_, err = io.ReadFull(conn, buf[:n])
		if err != nil {
			return nil, err
		}
	case 4: // IPv6 address
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

	// Connect to the target address
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil, errors.New("SOCKS4 only supports IP addresses")
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return nil, errors.New("SOCKS4 only supports IPv4 addresses")
	}

	p, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}

	// Send SOCKS4 connect request
	_, err = conn.Write([]byte{4, 1, byte(p >> 8), byte(p), ip4[0], ip4[1], ip4[2], ip4[3], 0})
	if err != nil {
		return nil, err
	}

	// Read the SOCKS4 response
	buf := make([]byte, 8)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return nil, err
	}

	if buf[0] != 0 {
		return nil, errors.New("SOCKS version not supported")
	}
	if buf[1] != 90 {
		return nil, errors.New("SOCKS connection failed")
	}

	return conn, nil
}
