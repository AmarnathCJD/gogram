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
	"strings"
	"sync"
	"time"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/mtproto/messages"
)

type httpTransport struct {
	cfg    HTTPConnConfig
	m      messages.MessageInformator
	client *http.Client
	url    string
	rxCh   chan []byte
	errCh  chan error
	closed chan struct{}
	once   sync.Once
	ctx    context.Context
	cancel context.CancelFunc
	txCh   chan []byte
	pollCh chan struct{}
	wg     sync.WaitGroup
}

func NewHTTPTransport(m messages.MessageInformator, cfg HTTPConnConfig) (Transport, error) {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	host := strings.TrimPrefix(cfg.Host, ":")
	if cfg.TLS && net.ParseIP(stripPort(host)) != nil && cfg.DC >= 1 && cfg.DC <= 5 {
		names := [...]string{"pluto", "venus", "aurora", "vesta", "flora"}
		host = net.JoinHostPort(names[cfg.DC-1]+".web.telegram.org", "443")
	}
	path := cfg.Path
	if path == "" {
		path = "/api"
		if strings.HasSuffix(stripPort(host), ".web.telegram.org") {
			path = "/apiw1"
			if cfg.TestMode {
				path = "/apiw_test1"
			}
		}
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
		tr.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialProxyContext(ctx, s, addr, cfg.LocalAddr)
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

	parent := cfg.Ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	t := &httpTransport{
		cfg:    cfg,
		m:      m,
		url:    fmt.Sprintf("%s://%s%s", scheme, host, path),
		client: &http.Client{Transport: tr, Timeout: 60 * time.Second},
		rxCh:   make(chan []byte, 32),
		errCh:  make(chan error, 4),
		closed: make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
		txCh:   make(chan []byte, 32),
		pollCh: make(chan struct{}, 1),
	}
	for range 4 {
		t.wg.Go(func() {
			for {
				select {
				case <-t.ctx.Done():
					return
				case data := <-t.txCh:
					t.post(data)
				}
			}
		})
	}
	t.wg.Go(func() {
		for {
			select {
			case <-t.ctx.Done():
				return
			case <-t.pollCh:
				t.longPoll()
			}
		}
	})
	if cfg.Logger != nil {
		cfg.Logger.Trace("[http] transport ready: %s", t.url)
	}
	return t, nil
}

func stripPort(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return strings.Trim(hostport, "[]")
}

func (t *httpTransport) IsHTTP() bool { return true }

func (t *httpTransport) Close() error {
	t.once.Do(func() {
		close(t.closed)
		t.cancel()
		if t.client != nil {
			if tr, ok := t.client.Transport.(*http.Transport); ok {
				tr.CloseIdleConnections()
			}
		}
	})
	t.wg.Wait()
	return nil
}

func (t *httpTransport) isClosed() bool {
	select {
	case <-t.closed:
		return true
	case <-t.ctx.Done():
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
		if m == nil {
			return fmt.Errorf("nil unencrypted message")
		}
		data, _ = m.Serialize(t.m)
	case *messages.Encrypted:
		if m == nil {
			return fmt.Errorf("nil encrypted message")
		}
		var err error
		data, err = m.Serialize(t.m, seqNo)
		if err != nil {
			return fmt.Errorf("serializing message: %w", err)
		}
	default:
		return fmt.Errorf("supported only mtproto predefined messages, got %T", msg)
	}

	select {
	case <-t.ctx.Done():
		return io.ErrClosedPipe
	case t.txCh <- data:
		return nil
	default:
		return fmt.Errorf("http request queue is full")
	}
}

func (t *httpTransport) post(data []byte) {
	t.doPost(data, true)
}

func (t *httpTransport) doPost(data []byte, scheduleFollowup bool) {
	if t.isClosed() {
		return
	}
	ctx, cancel := context.WithTimeout(t.ctx, 60*time.Second)
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

	body, err := readHTTPBody(resp.Body)
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
		case <-t.ctx.Done():
			return
		}
	}
	if scheduleFollowup {
		select {
		case t.pollCh <- struct{}{}:
		default:
		}
	}
}

func (t *httpTransport) longPoll() {
	for !t.isClosed() {
		ctx, cancel := context.WithTimeout(t.ctx, 30*time.Second)
		req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(nil))
		if err != nil {
			cancel()
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Connection", "keep-alive")
		req.ContentLength = 0

		resp, err := t.client.Do(req)
		if err != nil {
			cancel()
			if t.isClosed() {
				return
			}
			return
		}
		body, readErr := readHTTPBody(resp.Body)
		resp.Body.Close()
		cancel()
		if readErr != nil {
			t.pushErr(fmt.Errorf("http poll body: %w", readErr))
			return
		}
		if resp.StatusCode != http.StatusOK {
			return
		}
		if len(body) == 0 {
			return
		}
		select {
		case t.rxCh <- body:
		case <-t.ctx.Done():
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
	case <-t.ctx.Done():
		return nil, io.EOF
	}
}

func readHTTPBody(r io.Reader) ([]byte, error) {
	const maxBodySize = 16 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(r, maxBodySize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBodySize {
		return nil, fmt.Errorf("http body exceeds %d bytes", maxBodySize)
	}
	return body, nil
}

func (t *httpTransport) decode(data []byte) (messages.Common, error) {
	if len(data) == tl.WordLen {
		code := int64(int32(binary.LittleEndian.Uint32(data)))
		return nil, ErrCode(code)
	}
	var (
		msg messages.Common
		err error
	)
	if isPacketEncrypted(data) {
		msg, err = messages.DeserializeEncrypted(data, messages.AuthKeyForPacket(t.m, data))
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
