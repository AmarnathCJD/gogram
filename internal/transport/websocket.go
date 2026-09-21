// Copyright (c) 2025 @AmarnathCJD
//go:build !js && !wasm
// +build !js,!wasm

package transport

import (
	"bufio"
	"context"
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
	"sync"
	"time"

	"errors"
)

type wsConn struct {
	writeMu      sync.Mutex
	closeOnce    sync.Once
	stopCancel   func() bool
	fragmented   bool
	fragmentSize int64
	conn         net.Conn
	timeout      time.Duration
	buf          []byte
	masked       bool
}

func NewWebSocket(cfg WSConnConfig) (Conn, error) {
	if cfg.Ctx == nil {
		cfg.Ctx = context.Background()
	}
	setupTimeout := cfg.Timeout
	if setupTimeout <= 0 {
		setupTimeout = 10 * time.Second
	}
	setupCtx, cancelSetup := context.WithTimeout(cfg.Ctx, setupTimeout)
	defer cancelSetup()
	wsURL := FormatWebSocketURI(cfg.Host, cfg.TLS, cfg.DC, cfg.TestMode)

	if cfg.Logger != nil {
		cfg.Logger.WithField("url", wsURL).Debug("[ws] connecting")
	}

	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, fmt.Errorf("invalid websocket URL: %w", err)
	}

	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "wss" {
			host = net.JoinHostPort(u.Hostname(), "443")
		} else {
			host = net.JoinHostPort(u.Hostname(), "80")
		}
	}

	if cfg.Logger != nil {
		cfg.Logger.WithFields(map[string]any{
			"host":   host,
			"scheme": u.Scheme,
		}).Debug("[ws] resolved connection details")
	}

	var conn net.Conn
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	if cfg.LocalAddr != "" {
		localAddr, err := net.ResolveTCPAddr("tcp", cfg.LocalAddr)
		if err != nil {
			return nil, fmt.Errorf("invalid local address: %w", err)
		}
		dialer.LocalAddr = localAddr
		if cfg.Logger != nil {
			cfg.Logger.WithField("local_addr", cfg.LocalAddr).Debug("[ws] using local address")
		}
	}

	if cfg.Socks != nil && !cfg.Socks.IsEmpty() {
		conn, err = dialProxyContext(setupCtx, cfg.Socks.ToURL(), host, cfg.LocalAddr)
	} else {
		conn, err = dialer.DialContext(setupCtx, "tcp", host)
	}

	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.WithError(err).Debug("[ws] connection failed")
		}
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Debug("[ws] connection established")
	}

	committed := false
	defer func() {
		if !committed {
			_ = conn.Close()
		}
	}()
	deadline, _ := setupCtx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	rawConn := conn
	stopSetup := context.AfterFunc(setupCtx, func() { _ = rawConn.Close() })
	defer stopSetup()
	if u.Scheme == "wss" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(setupCtx); err != nil {
			return nil, fmt.Errorf("TLS handshake: %w", err)
		}
		conn = tlsConn
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
		"\r\n", u.RequestURI(), u.Host, wsKey)

	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write handshake failed: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Debug("[ws] handshake sent")
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read handshake response failed: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.WithField("status", resp.StatusCode).Trace("[ws] handshake response received")
	}

	if resp.StatusCode != 101 || !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") || !headerHasToken(resp.Header.Get("Connection"), "upgrade") {
		conn.Close()

		return nil, fmt.Errorf("handshake failed: status %d", resp.StatusCode)
	}

	acceptKey := resp.Header.Get("Sec-WebSocket-Accept")
	h := sha1.New()
	h.Write([]byte(wsKey + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	expectedKey := base64.StdEncoding.EncodeToString(h.Sum(nil))

	if acceptKey != expectedKey {
		conn.Close()
		if cfg.Logger != nil {
			cfg.Logger.WithFields(map[string]any{
				"expected": expectedKey,
				"got":      acceptKey,
			}).Debug("[ws] invalid Sec-WebSocket-Accept")
		}
		return nil, errors.New("invalid Sec-WebSocket-Accept")
	}

	if cfg.Logger != nil {
		cfg.Logger.Trace("[ws] handshake validation successful")
	}

	ws := &wsConn{
		conn:    &bufferedProxyConn{Conn: conn, reader: reader},
		timeout: cfg.Timeout,
		masked:  true,
	}

	if cfg.Logger != nil {
		cfg.Logger.WithField("protocol", cfg.ModeVariant).Debug("[ws] initializing obfuscation")
	}

	obf, err := NewObfuscatedConn(ws, ProtocolID(cfg.ModeVariant))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("obfuscation failed: %w", err)
	}

	if !stopSetup() || setupCtx.Err() != nil {
		return nil, setupCtx.Err()
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	ws.stopCancel = context.AfterFunc(cfg.Ctx, func() { _ = rawConn.Close() })
	committed = true

	if cfg.Logger != nil {
		cfg.Logger.Debug("[ws] WebSocket connection fully established")
	}

	return obf, nil
}

const maxWebSocketMessage = 16 * 1024 * 1024

func headerHasToken(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func (w *wsConn) Write(b []byte) (int, error) {
	if err := w.writeFrame(2, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (w *wsConn) writeFrame(opcode byte, b []byte) error {
	if len(b) > maxWebSocketMessage {
		return errors.New("websocket frame too large")
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	timeout := w.timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if err := w.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	frame := make([]byte, 0, len(b)+14)
	frame = append(frame, 0x80|opcode)
	mask := byte(0)
	if w.masked {
		mask = 0x80
	}
	switch {
	case len(b) < 126:
		frame = append(frame, byte(len(b))|mask)
	case len(b) < 65536:
		frame = append(frame, 126|mask)
		frame = binary.BigEndian.AppendUint16(frame, uint16(len(b)))
	default:
		frame = append(frame, 127|mask)
		frame = binary.BigEndian.AppendUint64(frame, uint64(len(b)))
	}
	var key [4]byte
	if w.masked {
		if _, err := rand.Read(key[:]); err != nil {
			return err
		}
		frame = append(frame, key[:]...)
	}
	off := len(frame)
	frame = append(frame, b...)
	if w.masked {
		for i := range b {
			frame[off+i] ^= key[i%4]
		}
	}
	n, err := w.conn.Write(frame)
	if err == nil && n != len(frame) {
		err = io.ErrShortWrite
	}
	return err
}

func (w *wsConn) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	for {
		if len(w.buf) > 0 {
			n := copy(b, w.buf)
			w.buf = w.buf[n:]
			if len(w.buf) == 0 {
				w.buf = nil
			}
			return n, nil
		}
		if w.timeout > 0 {
			if err := w.conn.SetReadDeadline(time.Now().Add(w.timeout)); err != nil {
				return 0, err
			}
		}
		var header [2]byte
		if _, err := io.ReadFull(w.conn, header[:]); err != nil {
			return 0, err
		}
		fin, opcode := header[0]&0x80 != 0, header[0]&0xf
		if header[0]&0x70 != 0 || header[1]&0x80 != 0 {
			return 0, errors.New("invalid websocket server frame flags")
		}
		length := uint64(header[1] & 0x7f)
		switch length {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(w.conn, ext[:]); err != nil {
				return 0, err
			}
			length = uint64(binary.BigEndian.Uint16(ext[:]))
			if length < 126 {
				return 0, errors.New("noncanonical websocket length")
			}
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(w.conn, ext[:]); err != nil {
				return 0, err
			}
			length = binary.BigEndian.Uint64(ext[:])
			if length < 65536 {
				return 0, errors.New("noncanonical websocket length")
			}
		}
		if length > maxWebSocketMessage {
			return 0, errors.New("websocket frame too large")
		}
		control := opcode >= 8
		if control && (!fin || length > 125) {
			return 0, errors.New("invalid websocket control frame")
		}
		switch opcode {
		case 0:
			if !w.fragmented {
				return 0, errors.New("unexpected websocket continuation")
			}
		case 2:
			if w.fragmented {
				return 0, errors.New("missing websocket continuation")
			}
			w.fragmentSize = 0
		case 8, 9, 10:
		default:
			return 0, fmt.Errorf("unsupported websocket opcode %d", opcode)
		}
		if !control {
			w.fragmentSize += int64(length)
			if w.fragmentSize > maxWebSocketMessage {
				return 0, errors.New("websocket message too large")
			}
			w.fragmented = !fin
		}
		payload := make([]byte, int(length))
		if _, err := io.ReadFull(w.conn, payload); err != nil {
			return 0, err
		}
		switch opcode {
		case 8:
			return 0, io.EOF
		case 9:
			if err := w.writeFrame(10, payload); err != nil {
				return 0, err
			}
			continue
		case 10:
			continue
		}
		w.buf = payload
	}
}

func (w *wsConn) Close() error {
	var err error
	w.closeOnce.Do(func() {
		if w.stopCancel != nil {
			w.stopCancel()
		}
		err = w.conn.Close()
	})
	return err
}
