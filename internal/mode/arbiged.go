// Copyright (c) 2025 @AmarnathCJD

package mode

import (
	"encoding/binary"
	"fmt"

	"io"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

type abridged struct {
	conn io.ReadWriter
}

var _ Mode = (*abridged)(nil)

var transportModeAbridged = [...]byte{0xef} // 0xef is a magic number

func (*abridged) getModeAnnouncement() []byte {
	return transportModeAbridged[:]
}

const (
	// If the packet length is greater than or equal to 127 words, we encode 4 bytes for the length:
	//   - 1 byte is a magic number (0x7f).
	//   - The remaining 3 bytes represent the actual length in little-endian order.
	//
	// See: https://core.telegram.org/mtproto/mtproto-transports#abridged
	magicValueSizeMoreThanSingleByte = 0x7f
)

func (m *abridged) WriteMsg(msg []byte) error {
	if len(msg)%4 != 0 {
		return ErrNotMultiple{Len: len(msg)}
	}

	bsize := make([]byte, 4)

	msgLength := len(msg) / tl.WordLen
	if msgLength < int(magicValueSizeMoreThanSingleByte) {
		bsize[0] = byte(msgLength)
		bsize = bsize[:1]
	} else {
		binary.LittleEndian.PutUint32(bsize, uint32(msgLength)<<8|magicValueSizeMoreThanSingleByte)
	}

	if _, err := m.conn.Write(bsize); err != nil {
		return err
	}
	if _, err := m.conn.Write(msg); err != nil {
		return err
	}

	return nil
}

func (m *abridged) ReadMsg() ([]byte, error) {
	sizeBuf := make([]byte, 4)
	n, err := m.conn.Read(sizeBuf[:1])
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, fmt.Errorf("need to read at least 1 byte")
	}

	size := int(sizeBuf[0])

	if size == magicValueSizeMoreThanSingleByte {
		n, err := m.conn.Read(sizeBuf[:3])
		if err != nil {
			return nil, err
		}
		if n != 3 {
			return nil, fmt.Errorf("need to read 3 bytes, got %v", n)
		}

		size = int(binary.LittleEndian.Uint32(sizeBuf))
	}

	size *= tl.WordLen

	msg := make([]byte, size)

	n, err = m.conn.Read(msg)
	if err != nil {
		return nil, err
	}
	if n != int(size) {
		return nil, fmt.Errorf("expected to read %d bytes, got %d", size, n)
	}

	return msg, nil
}
