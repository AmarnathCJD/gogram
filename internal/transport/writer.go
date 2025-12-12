package transport

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type Reader struct {
	r      io.Reader
	ctx    context.Context
	cancel context.CancelFunc

	mu            sync.Mutex
	closed        bool
	interruptOnce sync.Once
}

func NewReader(ctx context.Context, r io.Reader) *Reader {
	ctx, cancel := context.WithCancel(ctx)

	c := &Reader{
		r:      r,
		ctx:    ctx,
		cancel: cancel,
	}

	go func() {
		<-c.ctx.Done()
		c.interrupt()
	}()

	return c
}

func (c *Reader) interrupt() {
	c.interruptOnce.Do(func() {
		if nc, ok := c.r.(net.Conn); ok {
			nc.SetReadDeadline(time.Now())
			nc.Close()
		} else if closer, ok := c.r.(io.Closer); ok {
			closer.Close()
		}
	})
}

func (c *Reader) Read(p []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, fmt.Errorf("reader is closed")
	}
	r := c.r
	ctx := c.ctx
	c.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return 0, err
	}

	n, err := io.ReadFull(r, p)
	if err != nil {
		return n, err
	}

	select {
	case <-ctx.Done():
		return n, ctx.Err()
	default:
		return n, nil
	}
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
	c.interrupt()
	return nil
}
