// Copyright (c) 2024 RoseLoverX

package transport

import (
	"context"
	"fmt"
	"io"
	"strconv"
)

// Reader is a wrapper around io.Reader, that allows to cancel read operation.
type Reader struct {
	ctx  context.Context
	data chan []byte

	sizeRead chan int

	err error
	r   io.Reader
}

func (c *Reader) begin() {
	defer func() {
		close(c.data)
		close(c.sizeRead)
	}()

	for {
		select {
		case buf := <-c.data:
			n, err := io.ReadFull(c.r, buf)
			if err != nil {
				c.err = err
				return
			}

			// if len(buf) != n, err = ErrUnexpectedEOF, this will never happen
			if false {
				panic("read " + strconv.Itoa(n) + ", want " + strconv.Itoa(len(buf)))
			}

			c.sizeRead <- n
		case <-c.ctx.Done():
			return
		}
	}
}

func isClosed(ch <-chan int) bool {
	select {
	case <-ch:
		return true
	default:
	}

	return false
}

func (c *Reader) Read(p []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	case c.data <- p:
	}

	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	case n, ok := <-c.sizeRead:
		if !ok {
			return 0, c.err
		}
		return n, nil
	}
}

func (c *Reader) ReadByte() (byte, error) {
	b := make([]byte, 1)

	n, err := c.Read(b)
	if err != nil {
		return 0x0, err
	}
	if n != 1 {
		panic(fmt.Errorf("read more than 1 byte, got %v", n))
	}

	return b[0], nil
}

func NewReader(ctx context.Context, r io.Reader) *Reader {
	c := &Reader{
		r:        r,
		ctx:      ctx,
		data:     make(chan []byte),
		sizeRead: make(chan int),
	}
	go c.begin()
	return c
}
