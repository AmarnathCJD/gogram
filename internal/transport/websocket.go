// Copyright (c) 2025 RoseLoverX
//go:build !js && !wasm
// +build !js,!wasm

package transport

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
)

type wsConn struct {
	reader  *Reader
	conn    net.Conn
	timeout time.Duration
	buf     []byte
	masked  bool
}

func NewWebSocket(cfg WSConnConfig) (Conn, error) {
	wsURL := FormatWebSocketURI(cfg.Host, cfg.TLS, cfg.DC, cfg.TestMode)

	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, errors.Wrap(err, "invalid websocket URL")
	}

	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "wss" {
			host = u.Host + ":443"
		} else {
			host = u.Host + ":80"
		}
	}

	var conn net.Conn
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	if cfg.LocalAddr != "" {
		localAddr, _ := net.ResolveTCPAddr("tcp", cfg.LocalAddr)
		dialer.LocalAddr = localAddr
	}

	if u.Scheme == "wss" {
		tlsConfig := &tls.Config{
			ServerName: strings.Split(u.Host, ":")[0],
		}
		conn, err = tls.DialWithDialer(dialer, "tcp", host, tlsConfig)
	} else {
		conn, err = dialer.DialContext(cfg.Ctx, "tcp", host)
	}

	if err != nil {
		return nil, errors.Wrap(err, "dial failed")
	}

	key := make([]byte, 16)
	rand.Read(key)
	wsKey := base64.StdEncoding.EncodeToString(key)

	req := fmt.Sprintf("GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n"+
		"Sec-WebSocket-Protocol: binary\r\n"+
		"\r\n", u.Path, u.Host, wsKey)

	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, errors.Wrap(err, "write handshake failed")
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
	if err != nil {
		conn.Close()
		return nil, errors.Wrap(err, "read handshake response failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != 101 {
		conn.Close()
		return nil, fmt.Errorf("handshake failed: status %d", resp.StatusCode)
	}

	acceptKey := resp.Header.Get("Sec-WebSocket-Accept")
	h := sha1.New()
	h.Write([]byte(wsKey + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	expectedKey := base64.StdEncoding.EncodeToString(h.Sum(nil))

	if acceptKey != expectedKey {
		conn.Close()
		return nil, errors.New("invalid Sec-WebSocket-Accept")
	}

	ws := &wsConn{
		conn:    conn,
		timeout: cfg.Timeout,
		masked:  true,
	}

	obf, err := NewObfuscatedConn(ws, ProtocolID(cfg.ModeVariant))
	if err != nil {
		conn.Close()
		return nil, errors.Wrap(err, "obfuscation failed")
	}

	ws.reader = NewReader(cfg.Ctx, obf)
	return obf, nil
}

func (w *wsConn) Write(b []byte) (int, error) {
	frame := make([]byte, 0, len(b)+14)

	frame = append(frame, 0x82)

	if len(b) < 126 {
		if w.masked {
			frame = append(frame, byte(len(b))|0x80)
		} else {
			frame = append(frame, byte(len(b)))
		}
	} else if len(b) < 65536 {
		if w.masked {
			frame = append(frame, 126|0x80)
		} else {
			frame = append(frame, 126)
		}
		frame = append(frame, byte(len(b)>>8), byte(len(b)))
	} else {
		if w.masked {
			frame = append(frame, 127|0x80)
		} else {
			frame = append(frame, 127)
		}
		frame = append(frame, 0, 0, 0, 0, byte(len(b)>>24), byte(len(b)>>16), byte(len(b)>>8), byte(len(b)))
	}

	if w.masked {
		maskKey := make([]byte, 4)
		rand.Read(maskKey)
		frame = append(frame, maskKey...)

		masked := make([]byte, len(b))
		for i := range b {
			masked[i] = b[i] ^ maskKey[i%4]
		}
		frame = append(frame, masked...)
	} else {
		frame = append(frame, b...)
	}

	_, err := w.conn.Write(frame)
	if err != nil {
		return 0, errors.Wrap(err, "websocket write")
	}
	return len(b), nil
}

func (w *wsConn) Read(b []byte) (int, error) {
	if w.timeout > 0 {
		err := w.conn.SetReadDeadline(time.Now().Add(w.timeout))
		if err != nil {
			return 0, errors.Wrap(err, "setting read deadline")
		}
	}

	if len(w.buf) > 0 {
		n := copy(b, w.buf)
		w.buf = w.buf[n:]
		return n, nil
	}

	header := make([]byte, 2)
	if _, err := io.ReadFull(w.conn, header); err != nil {
		if err == io.EOF {
			return 0, io.EOF
		}
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return 0, errors.Wrap(err, "required to reconnect!")
		}
		return 0, errors.Wrap(err, "reading websocket frame header")
	}

	opcode := header[0] & 0x0F
	masked := header[1]&0x80 != 0
	payloadLen := int64(header[1] & 0x7F)

	if opcode == 0x08 {
		return 0, io.EOF
	}

	switch payloadLen {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(w.conn, ext); err != nil {
			return 0, errors.Wrap(err, "reading extended payload length")
		}
		payloadLen = int64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(w.conn, ext); err != nil {
			return 0, errors.Wrap(err, "reading extended payload length")
		}
		payloadLen = int64(binary.BigEndian.Uint64(ext))
	}

	var maskKey []byte
	if masked {
		maskKey = make([]byte, 4)
		if _, err := io.ReadFull(w.conn, maskKey); err != nil {
			return 0, errors.Wrap(err, "reading mask key")
		}
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(w.conn, payload); err != nil {
		return 0, errors.Wrap(err, "reading payload")
	}

	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}

	n := copy(b, payload)
	if n < len(payload) {
		w.buf = append(w.buf, payload[n:]...)
	}

	return n, nil
}

func (w *wsConn) Close() error {
	closeFrame := []byte{0x88, 0x80, 0, 0, 0, 0}
	w.conn.Write(closeFrame)
	return w.conn.Close()
}

func (w *wsConn) GetReader() *Reader {
	return w.reader
}
