// Copyright (c) 2024 RoseLoverX

package transport

import (
	"context"
	"io"
	"sync"
)

// Reader is a context-aware wrapper around io.Reader that allows cancellation.
type Reader struct {
	ctx    context.Context
	cancel context.CancelFunc
	r      io.Reader
	mu     sync.Mutex
	closed bool
}

// NewReader creates a new context-aware Reader.
func NewReader(ctx context.Context, r io.Reader) *Reader {
	ctx, cancel := context.WithCancel(ctx)
	return &Reader{
		ctx:    ctx,
		cancel: cancel,
		r:      r,
	}
}

func (c *Reader) Read(p []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	c.mu.Unlock()

	// check context before reading
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
	}

	// use a channel to make the read cancellable
	type readResult struct {
		n   int
		err error
	}

	resultCh := make(chan readResult, 1)

	go func() {
		n, err := io.ReadFull(c.r, p)
		select {
		case resultCh <- readResult{n, err}:
		case <-c.ctx.Done():
		}
	}()

	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	case result := <-resultCh:
		return result.n, result.err
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
	defer c.mu.Unlock()

	if !c.closed {
		c.closed = true
		c.cancel()
	}
	return nil
}
