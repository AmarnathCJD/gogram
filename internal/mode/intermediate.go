// Copyright (c) 2025 @AmarnathCJD

package mode

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

type intermediate struct {
	conn io.ReadWriter
}

var _ Mode = (*intermediate)(nil)

var transportModeIntermediate = [...]byte{0xee, 0xee, 0xee, 0xee} // meta:immutable

func (*intermediate) getModeAnnouncement() []byte {
	return transportModeIntermediate[:]
}

func (m *intermediate) WriteMsg(msg []byte) error {
	if len(msg) < 4 || len(msg) > maxMessageSize || len(msg)%4 != 0 {
		return fmt.Errorf("invalid message size: %d", len(msg))
	}
	size := make([]byte, tl.WordLen)
	binary.LittleEndian.PutUint32(size, uint32(len(msg)))
	return writeFrame(m.conn, append(size, msg...))
}

func (m *intermediate) ReadMsg() ([]byte, error) {
	sizeBuf := make([]byte, tl.WordLen)
	n, err := io.ReadFull(m.conn, sizeBuf)
	if err != nil {
		return nil, err
	}
	if n != tl.WordLen {
		return nil, fmt.Errorf("size is not length of int32, expected 4 bytes, got %d", n)
	}

	size := binary.LittleEndian.Uint32(sizeBuf)
	if size < 4 || size > maxMessageSize || size%4 != 0 {
		return nil, fmt.Errorf("invalid message size: %d", size)
	}

	msg := make([]byte, int(size))
	n, err = io.ReadFull(m.conn, msg)
	if err != nil {
		return nil, err
	}
	if n != int(size) {
		return nil, fmt.Errorf("expected to read %d bytes, got %d", size, n)
	}

	return msg, nil
}
