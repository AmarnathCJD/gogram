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

	req  chan []byte
	done chan readResult

	mu     sync.Mutex
	closed bool

	wg sync.WaitGroup
}

type readResult struct {
	n   int
	err error
}

func NewReader(ctx context.Context, r io.Reader) *Reader {
	ctx, cancel := context.WithCancel(ctx)

	c := &Reader{
		r:      r,
		ctx:    ctx,
		cancel: cancel,
		req:    make(chan []byte, 4),
		done:   make(chan readResult, 1),
	}

	c.wg.Add(1)
	go c.worker()

	return c
}

func (c *Reader) worker() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return

		case buf, ok := <-c.req:
			if !ok {
				return
			}

			n, err := c.read(buf)

			select {
			case c.done <- readResult{n, err}:
			case <-c.ctx.Done():
				return
			}
		}
	}
}

func (c *Reader) interrupt() {
	if nc, ok := c.r.(net.Conn); ok {
		nc.SetReadDeadline(time.Now())
		nc.Close()
	} else if closer, ok := c.r.(io.Closer); ok {
		closer.Close()
	}
}

func (c *Reader) read(buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		select {
		case <-c.ctx.Done():
			return n, c.ctx.Err()
		default:
			k, err := c.r.Read(buf[n:])
			n += k
			if err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

func (c *Reader) Read(p []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, ErrClosed
	}

	select {
	case <-c.ctx.Done():
		c.mu.Unlock()
		return 0, c.ctx.Err()
	case c.req <- p:
		c.mu.Unlock()
	}

	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	case res := <-c.done:
		return res.n, res.err
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

	c.interrupt()
	c.cancel()
	close(c.req)
	c.wg.Wait()
	return nil
}
