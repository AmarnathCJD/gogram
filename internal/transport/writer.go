// Copyright (c) 2024 RoseLoverX

package transport

import (
	"context"
	"io"
	"sync"
)

type Reader struct {
	ctx    context.Context
	cancel context.CancelFunc
	r      io.Reader

	mu     sync.Mutex
	closed bool

	reqCh chan readRequest
	wg    sync.WaitGroup
}

type readRequest struct {
	buf    []byte
	respCh chan readResponse
}

type readResponse struct {
	n   int
	err error
}

// NewReader creates a new context-aware Reader.
func NewReader(ctx context.Context, r io.Reader) *Reader {
	ctx, cancel := context.WithCancel(ctx)
	reader := &Reader{
		ctx:    ctx,
		cancel: cancel,
		r:      r,
		reqCh:  make(chan readRequest),
	}
	reader.wg.Add(1)
	go reader.worker()
	return reader
}

func (c *Reader) worker() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		case req, ok := <-c.reqCh:
			if !ok {
				return
			}
			n, err := io.ReadFull(c.r, req.buf)
			select {
			case req.respCh <- readResponse{n, err}:
			case <-c.ctx.Done():
				return
			}
		}
	}
}

func (c *Reader) Read(p []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	c.mu.Unlock()

	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
	}

	respCh := make(chan readResponse, 1)
	req := readRequest{buf: p, respCh: respCh}

	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	case c.reqCh <- req:
	}

	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	case resp := <-respCh:
		return resp.n, resp.err
	}
}

func (c *Reader) ReadByte() (byte, error) {
	b := make([]byte, 1)
	n, err := c.Read(b)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return b[0], nil
}

func (c *Reader) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	c.cancel()
	c.wg.Wait()
	return nil
}
