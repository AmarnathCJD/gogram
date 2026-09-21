// Copyright (c) 2025 @AmarnathCJD
//go:build js && wasm

package transport

import (
	"context"
	"errors"
	"io"
	"sync"
	"syscall/js"
	"time"
)

const wasmWebSocketLimit = 16 * 1024 * 1024

type wsListener struct {
	event string
	fn    js.Func
}
type wsConn struct {
	wasmWS          js.Value
	readChan        chan []byte
	errChan         chan error
	closeCh         chan struct{}
	timeout         time.Duration
	buf             []byte
	readMu, writeMu sync.Mutex
	closeOnce       sync.Once
	handlers        []wsListener
}

func NewWebSocket(cfg WSConnConfig) (Conn, error) {
	if cfg.Ctx == nil {
		cfg.Ctx = context.Background()
	}
	if err := cfg.Ctx.Err(); err != nil {
		return nil, err
	}
	wsURL := FormatWebSocketURI(cfg.Host, cfg.TLS, cfg.DC, cfg.TestMode)
	wsObj := js.Global().Get("WebSocket").New(wsURL, js.ValueOf([]any{"binary"}))
	wsObj.Set("binaryType", "arraybuffer")
	w := &wsConn{wasmWS: wsObj, readChan: make(chan []byte, 8), errChan: make(chan error, 1), closeCh: make(chan struct{}), timeout: cfg.Timeout}
	connected := make(chan struct{}, 1)
	on := func(event string, fn func(js.Value, []js.Value) any) {
		handler := js.FuncOf(fn)
		w.handlers = append(w.handlers, wsListener{event, handler})
		wsObj.Call("addEventListener", event, handler)
	}
	fail := func(err error) {
		select {
		case w.errChan <- err:
		default:
		}
		_ = w.Close()
	}
	on("open", func(js.Value, []js.Value) any {
		select {
		case connected <- struct{}{}:
		default:
		}
		return nil
	})
	on("error", func(js.Value, []js.Value) any { fail(errors.New("websocket error")); return nil })
	on("close", func(js.Value, []js.Value) any { _ = w.Close(); return nil })
	on("message", func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		data := js.Global().Get("Uint8Array").New(args[0].Get("data"))
		length := data.Get("length").Int()
		if length > wasmWebSocketLimit {
			fail(errors.New("websocket message too large"))
			return nil
		}
		if length == 0 {
			return nil
		}
		buf := make([]byte, length)
		js.CopyBytesToGo(buf, data)
		select {
		case w.readChan <- buf:
		default:
			fail(errors.New("websocket receive queue full"))
		}
		return nil
	})
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-connected:
	case err := <-w.errChan:
		_ = w.Close()
		return nil, err
	case <-w.closeCh:
		return nil, io.ErrClosedPipe
	case <-cfg.Ctx.Done():
		_ = w.Close()
		return nil, cfg.Ctx.Err()
	case <-timer.C:
		_ = w.Close()
		return nil, errors.New("websocket timeout")
	}
	stop := context.AfterFunc(cfg.Ctx, func() { _ = w.Close() })
	go func() { <-w.closeCh; stop() }()
	obf, err := NewObfuscatedConn(w, ProtocolID(cfg.ModeVariant))
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	return obf, nil
}

func (w *wsConn) Write(b []byte) (n int, err error) {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	select {
	case <-w.closeCh:
		return 0, io.ErrClosedPipe
	default:
	}
	if len(b) == 0 {
		return 0, nil
	}
	if len(b) > wasmWebSocketLimit || w.wasmWS.Get("bufferedAmount").Int() > wasmWebSocketLimit-len(b) {
		return 0, errors.New("websocket send buffer full")
	}
	defer func() {
		if recover() != nil {
			n = 0
			err = io.ErrClosedPipe
		}
	}()
	u8 := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(u8, b)
	w.wasmWS.Call("send", u8)
	return len(b), nil
}

func (w *wsConn) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	w.readMu.Lock()
	defer w.readMu.Unlock()
	if len(w.buf) > 0 {
		n := copy(b, w.buf)
		w.buf = w.buf[n:]
		return n, nil
	}
	var timeout <-chan time.Time
	if w.timeout > 0 {
		timer := time.NewTimer(w.timeout)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case data := <-w.readChan:
		n := copy(b, data)
		w.buf = data[n:]
		return n, nil
	case err := <-w.errChan:
		return 0, err
	case <-w.closeCh:
		return 0, io.EOF
	case <-timeout:
		return 0, errors.New("websocket read timeout")
	}
}

func (w *wsConn) Close() error {
	w.closeOnce.Do(func() {
		close(w.closeCh)
		for _, h := range w.handlers {
			w.wasmWS.Call("removeEventListener", h.event, h.fn)
			h.fn.Release()
		}
		w.handlers = nil
		if w.wasmWS.Truthy() {
			w.wasmWS.Call("close")
		}
	})
	return nil
}
