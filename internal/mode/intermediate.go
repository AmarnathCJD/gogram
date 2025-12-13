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
	size := make([]byte, tl.WordLen)
	binary.LittleEndian.PutUint32(size, uint32(len(msg)))
	_, err := m.conn.Write(append(size, msg...))
	return err
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
	if size > 1<<30 {
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

	// Padded Intermediate mode (MTProxy): strip random padding from encrypted messages
	// Encrypted messages need (len - 24) % 16 == 0 for AES-IGE, so total len % 16 == 8
	if len(msg) >= 24 {
		authKeyID := binary.LittleEndian.Uint64(msg[:8])
		if authKeyID != 0 && len(msg)%16 != 8 {
			msg = msg[:((len(msg)-8)/16)*16+8]
		}
	}

	return msg, nil
}
