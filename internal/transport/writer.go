package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)


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
        req:    make(chan readRequest, 4),
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

            // always non-blocking, always paired per-request
            select {
            case req.res <- readResult{n, err}:
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
        return 0, ErrClosed
    }
    c.mu.Unlock()

    resCh := make(chan readResult, 1)

    // send request
    select {
    case c.req <- readRequest{p, resCh}:
    case <-c.ctx.Done():
        return 0, c.ctx.Err()
    }

    // wait for the response
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

    c.cancel()

    // IMPORTANT: close AFTER cancel so worker exits cleanly
    close(c.req)

    c.wg.Wait()
    return nil
}
