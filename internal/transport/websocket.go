// Copyright (c) 2025 RoseLoverX
//go:build !js && !wasm
// +build !js,!wasm

package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

type WSConnConfig struct {
	Ctx         context.Context
	Host        string
	TLS         bool
	Timeout     time.Duration
	Socks       *url.URL
	LocalAddr   string
	ModeVariant uint8
	DC          int
	TestMode    bool
}

func formatWebSocketURI(host string, tls bool, dc int, testMode bool) string {
	isIP := net.ParseIP(strings.Split(host, ":")[0]) != nil

	if isIP {
		scheme := "ws"
		port := "80"
		if tls {
			scheme = "wss"
			port = "443"
		}
		hostOnly := strings.Split(host, ":")[0]
		return fmt.Sprintf("%s://%s:%s/apiws", scheme, hostOnly, port)
	}

	scheme, port := "wss", "443"
	if strings.Contains(host, "web.telegram.org") {
		return fmt.Sprintf("%s://%s/apiws", scheme, host)
	}

	dcName := [...]string{"pluto", "venus", "aurora", "vesta", "flora"}
	name := "vesta"
	if dc >= 1 && dc <= 5 {
		name = dcName[dc-1]
	}
	testSuffix := ""
	if testMode {
		testSuffix = "_test"
	}

	return fmt.Sprintf("%s://%s.web.telegram.org:%s/apiws%s", scheme, name, port, testSuffix)
}

func protocolID(variant uint8) []byte {
	switch variant {
	case 1:
		return []byte{0xee, 0xee, 0xee, 0xee}
	case 2:
		return []byte{0xdd, 0xdd, 0xdd, 0xdd}
	default:
		return []byte{0xef}
	}
}

type wsConn struct {
	reader  *Reader
	conn    *websocket.Conn
	timeout time.Duration
	buf     []byte
}

func NewWebSocket(cfg WSConnConfig) (Conn, error) {
	wsURL := formatWebSocketURI(cfg.Host, cfg.TLS, cfg.DC, cfg.TestMode)

	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		Subprotocols:     []string{"binary"},
	}

	if cfg.Socks != nil && cfg.Socks.Host != "" {
		proxyURL, _ := url.Parse(cfg.Socks.String())
		dialer.Proxy = http.ProxyURL(proxyURL)
	}

	if cfg.LocalAddr != "" {
		localAddr, _ := net.ResolveTCPAddr("tcp", cfg.LocalAddr)
		dialer.NetDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{LocalAddr: localAddr, Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
		}
	}

	if cfg.TLS {
		dialer.TLSClientConfig = &tls.Config{ServerName: strings.Split(cfg.Host, ":")[0]}
	}

	conn, _, err := dialer.DialContext(cfg.Ctx, wsURL, http.Header{
		"Sec-WebSocket-Protocol": []string{"binary"},
	})
	if err != nil {
		return nil, errors.Wrap(err, "websocket dial failed")
	}

	if conn.Subprotocol() != "binary" {
		conn.Close()
		return nil, errors.New("server did not accept binary subprotocol")
	}

	ws := &wsConn{conn: conn, timeout: cfg.Timeout}
	obf, err := newObfuscatedConn(ws, protocolID(cfg.ModeVariant))
	if err != nil {
		conn.Close()
		return nil, errors.Wrap(err, "obfuscation failed")
	}

	ws.reader = NewReader(cfg.Ctx, obf)
	return obf, nil
}

func (w *wsConn) Write(b []byte) (int, error) {
	if err := w.conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (w *wsConn) Read(b []byte) (int, error) {
	if len(w.buf) > 0 {
		n := copy(b, w.buf)
		w.buf = w.buf[n:]
		return n, nil
	}

	if w.timeout > 0 {
		_ = w.conn.SetReadDeadline(time.Now().Add(w.timeout))
	}

	_, data, err := w.conn.ReadMessage()
	if err != nil {
		return 0, err
	}

	n := copy(b, data)
	if n < len(data) {
		w.buf = append(w.buf, data[n:]...)
	}
	return n, nil
}

func (w *wsConn) Close() error {
	_ = w.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	return w.conn.Close()
}

func (w *wsConn) GetReader() *Reader {
	return w.reader
}
