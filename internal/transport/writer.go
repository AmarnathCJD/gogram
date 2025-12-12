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

	mu     sync.Mutex
	closed bool

	req chan readRequest

	wg sync.WaitGroup
}

type readRequest struct {
	buf []byte
	res chan readResult
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
		req:    make(chan readRequest, 16),
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

		case req, ok := <-c.req:
			if !ok {
				return
			}

			n, err := c.read(req.buf)

			select {
			case req.res <- readResult{n, err}:
			case <-c.ctx.Done():
				return
			}
		}
	}
}

func (c *Reader) read(buf []byte) (int, error) {
	n := 0

	for n < len(buf) {
		select {
		case <-c.ctx.Done():
			return n, c.ctx.Err()
		default:
			if nc, ok := c.r.(net.Conn); ok {
				nc.SetReadDeadline(time.Now().Add(15 * time.Second))
			}

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
	c.mu.Unlock()

	resCh := make(chan readResult, 1)

	select {
	case c.req <- readRequest{p, resCh}:
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	}

	select {
	case res := <-resCh:
		return res.n, res.err
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
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

	// Interrupt active reads safely
	if nc, ok := c.r.(net.Conn); ok {
		nc.SetReadDeadline(time.Now())
	}

	c.cancel()

	close(c.req)
	c.wg.Wait()
	return nil
}
