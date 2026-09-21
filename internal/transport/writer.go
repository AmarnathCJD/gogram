package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

var ErrClosed = errors.New("reader closed")

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
		return 0, ErrClosed
	}
	r := c.r
	ctx := c.ctx
	c.mu.Unlock()

	total, emptyReads := 0, 0

	for total < len(p) {
		if err := ctx.Err(); err != nil {
			return total, err
		}

		n, err := r.Read(p[total:])
		if n < 0 || n > len(p)-total {
			return total, errors.New("reader returned an invalid byte count")
		}
		if n > 0 {
			total += n
			emptyReads = 0
		} else if err == nil {
			emptyReads++
			if emptyReads >= 100 {
				return total, io.ErrNoProgress
			}
		}

		if err != nil {
			return total, err
		}
	}

	return total, nil
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
