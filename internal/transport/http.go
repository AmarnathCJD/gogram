// Copyright (c) 2025 @AmarnathCJD

package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/mtproto/messages"
)

type httpTransport struct {
	cfg     HTTPConnConfig
	m       messages.MessageInformator
	client  *http.Client
	url     string
	rxCh    chan []byte
	errCh   chan error
	closed  chan struct{}
	closeMu sync.Mutex
	once    sync.Once
}

func NewHTTPTransport(m messages.MessageInformator, cfg HTTPConnConfig) (Transport, error) {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	host := strings.TrimPrefix(cfg.Host, ":")
	path := cfg.Path
	if path == "" {
		path = "/api"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	tr := &http.Transport{
		DisableCompression:    true,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
	}
	if cfg.TLS {
		tr.TLSClientConfig = &tls.Config{ServerName: stripPort(host)}
	}

	if cfg.Socks != nil && !cfg.Socks.IsEmpty() {
		s := cfg.Socks.ToURL()
		tr.DialContext = func(_ context.Context, _, addr string) (net.Conn, error) {
			return dialProxy(s, addr, cfg.LocalAddr)
		}
	} else if cfg.LocalAddr != "" {
		local, err := net.ResolveTCPAddr("tcp", cfg.LocalAddr)
		if err != nil {
			return nil, fmt.Errorf("resolve local addr: %w", err)
		}
		d := &net.Dialer{Timeout: cfg.Timeout, LocalAddr: local}
		tr.DialContext = d.DialContext
	} else if cfg.Timeout > 0 {
		d := &net.Dialer{Timeout: cfg.Timeout}
		tr.DialContext = d.DialContext
	}

	t := &httpTransport{
		cfg:    cfg,
		m:      m,
		url:    fmt.Sprintf("%s://%s%s", scheme, host, path),
		client: &http.Client{Transport: tr, Timeout: 60 * time.Second},
		rxCh:   make(chan []byte, 32),
		errCh:  make(chan error, 4),
		closed: make(chan struct{}),
	}
	if cfg.Logger != nil {
		cfg.Logger.Trace("[http] transport ready: %s", t.url)
	}
	return t, nil
}

func stripPort(hostport string) string {
	if i := strings.LastIndex(hostport, ":"); i > 0 {
		return hostport[:i]
	}
	return hostport
}

func (t *httpTransport) IsHTTP() bool { return true }

func (t *httpTransport) Close() error {
	t.once.Do(func() {
		close(t.closed)
		if t.client != nil {
			if tr, ok := t.client.Transport.(*http.Transport); ok {
				tr.CloseIdleConnections()
			}
		}
	})
	return nil
}

func (t *httpTransport) isClosed() bool {
	select {
	case <-t.closed:
		return true
	default:
		return false
	}
}

func (t *httpTransport) WriteMsg(msg messages.Common, seqNo int32) error {
	if t.isClosed() {
		return io.ErrClosedPipe
	}
	var data []byte
	switch m := msg.(type) {
	case *messages.Unencrypted:
		data, _ = m.Serialize(t.m)
	case *messages.Encrypted:
		var err error
		data, err = m.Serialize(t.m, seqNo)
		if err != nil {
			return fmt.Errorf("serializing message: %w", err)
		}
	default:
		return fmt.Errorf("supported only mtproto predefined messages, got %v", reflect.TypeOf(msg).String())
	}

	go t.post(data)
	return nil
}

func (t *httpTransport) post(data []byte) {
	t.doPost(data, true)
}

func (t *httpTransport) doPost(data []byte, scheduleFollowup bool) {
	if t.isClosed() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(data))
	if err != nil {
		t.pushErr(fmt.Errorf("http build req: %w", err))
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Connection", "keep-alive")
	req.ContentLength = int64(len(data))

	resp, err := t.client.Do(req)
	if err != nil {
		if t.isClosed() {
			return
		}
		t.pushErr(fmt.Errorf("http do: %w", err))
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.pushErr(fmt.Errorf("http read body: %w", err))
		return
	}
	if resp.StatusCode != http.StatusOK {
		t.pushErr(fmt.Errorf("http status %d", resp.StatusCode))
		return
	}
	if len(body) > 0 {
		select {
		case t.rxCh <- body:
		case <-t.closed:
			return
		}
	}
	if scheduleFollowup {
		go t.longPoll()
	}
}

func (t *httpTransport) longPoll() {
	for !t.isClosed() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(nil))
		if err != nil {
			cancel()
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Connection", "keep-alive")
		req.ContentLength = 0

		resp, err := t.client.Do(req)
		cancel()
		if err != nil {
			if t.isClosed() {
				return
			}
			return
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return
		}
		if len(body) == 0 {
			return
		}
		select {
		case t.rxCh <- body:
		case <-t.closed:
			return
		}
	}
}

func (t *httpTransport) pushErr(err error) {
	select {
	case t.errCh <- err:
	default:
	}
}

func (t *httpTransport) ReadMsg() (messages.Common, error) {
	select {
	case data := <-t.rxCh:
		return t.decode(data)
	case err := <-t.errCh:
		return nil, err
	case <-t.closed:
		return nil, io.EOF
	}
}

func (t *httpTransport) decode(data []byte) (messages.Common, error) {
	if len(data) == tl.WordLen {
		code := int64(binary.LittleEndian.Uint32(data))
		return nil, ErrCode(code)
	}
	var (
		msg messages.Common
		err error
	)
	if isPacketEncrypted(data) {
		msg, err = messages.DeserializeEncrypted(data, t.m.GetAuthKey())
	} else {
		msg, err = messages.DeserializeUnencrypted(data)
	}
	if err != nil {
		return nil, fmt.Errorf("parsing message: %w", err)
	}
	mod := msg.GetMsgID() & 3
	if mod != 1 && mod != 3 {
		return nil, fmt.Errorf("wrong bits of message_id: %d", mod)
	}
	return msg, nil
}
