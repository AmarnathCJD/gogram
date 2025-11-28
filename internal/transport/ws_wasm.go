// Copyright (c) 2025 RoseLoverX
//go:build js && wasm
// +build js,wasm

package transport

import (
	"io"
	"syscall/js"
	"time"

	"errors"
)

type wsConn struct {
	reader    *Reader
	wasmWS    js.Value
	readChan  chan []byte
	errChan   chan error
	closeCh   chan struct{}
	timeout   time.Duration
	buf       []byte
	closeOnce bool
	handlers  []js.Func
}

func NewWebSocket(cfg WSConnConfig) (Conn, error) {
	wsURL := FormatWebSocketURI(cfg.Host, cfg.TLS, cfg.DC, cfg.TestMode)
	wsObj := js.Global().Get("WebSocket").New(wsURL, js.ValueOf([]any{"binary"}))
	wsObj.Set("binaryType", "arraybuffer")

	w := &wsConn{
		wasmWS:   wsObj,
		readChan: make(chan []byte, 8),
		errChan:  make(chan error, 1),
		closeCh:  make(chan struct{}),
		timeout:  cfg.Timeout,
	}

	connected := make(chan error, 1)
	openHandler := js.FuncOf(func(this js.Value, args []js.Value) any {
		connected <- nil
		return nil
	})
	errHandler := js.FuncOf(func(this js.Value, args []js.Value) any {
		connected <- errors.New("websocket error")
		return nil
	})

	wsObj.Call("addEventListener", "open", openHandler)
	wsObj.Call("addEventListener", "error", errHandler)

	select {
	case err := <-connected:
		openHandler.Release()
		errHandler.Release()
		if err != nil {
			wsObj.Call("close")
			return nil, err
		}
	case <-cfg.Ctx.Done():
		openHandler.Release()
		errHandler.Release()
		wsObj.Call("close")
		return nil, cfg.Ctx.Err()
	case <-time.After(10 * time.Second):
		openHandler.Release()
		errHandler.Release()
		wsObj.Call("close")
		return nil, errors.New("websocket timeout")
	}

	msgHandler := js.FuncOf(func(this js.Value, args []js.Value) any {
		data := js.Global().Get("Uint8Array").New(args[0].Get("data"))
		buf := make([]byte, data.Get("length").Int())
		js.CopyBytesToGo(buf, data)
		select {
		case w.readChan <- buf:
		default:
		}
		return nil
	})

	closeHandler := js.FuncOf(func(this js.Value, args []js.Value) any {
		if !w.closeOnce {
			w.closeOnce = true
			close(w.closeCh)
		}
		return nil
	})

	w.handlers = []js.Func{msgHandler, closeHandler}
	wsObj.Call("addEventListener", "message", msgHandler)
	wsObj.Call("addEventListener", "close", closeHandler)

	obf, err := NewObfuscatedConn(w, ProtocolID(cfg.ModeVariant))
	if err != nil {
		w.Close()
		return nil, err
	}

	w.reader = NewReader(cfg.Ctx, obf)
	return obf, nil
}

func (w *wsConn) Write(b []byte) (int, error) {
	if w.wasmWS.Truthy() && !w.closeOnce {
		u8 := js.Global().Get("Uint8Array").New(len(b))
		js.CopyBytesToJS(u8, b)
		w.wasmWS.Call("send", u8)
		return len(b), nil
	}
	return 0, errors.New("websocket closed")
}

func (w *wsConn) Read(b []byte) (int, error) {
	if len(w.buf) > 0 {
		n := copy(b, w.buf)
		w.buf = w.buf[n:]
		return n, nil
	}

	select {
	case data := <-w.readChan:
		n := copy(b, data)
		if n < len(data) {
			w.buf = append(w.buf, data[n:]...)
		}
		return n, nil
	case <-w.closeCh:
		return 0, io.EOF
	case <-time.After(w.timeout):
		return 0, errors.New("read timeout")
	}
}

func (w *wsConn) Close() error {
	if w.reader != nil {
		w.reader.Close()
	}
	if w.wasmWS.Truthy() && !w.closeOnce {
		w.closeOnce = true
		w.wasmWS.Call("close")
		close(w.closeCh)
	}
	for _, h := range w.handlers {
		h.Release()
	}
	return nil
}

func (w *wsConn) GetReader() *Reader {
	return w.reader
}
