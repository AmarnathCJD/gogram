// Copyright (c) 2025 @AmarnathCJD

package mode

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

// paddedIntermediate implements the padded intermediate MTProto transport.
// Frame format:
//   - 4 bytes little-endian: total length (payload + padding)
//   - payload bytes (must be multiple of 4)
//   - 0..15 bytes of random padding
type paddedIntermediate struct {
	conn io.ReadWriter
}

var _ Mode = (*paddedIntermediate)(nil)

var transportModePaddedIntermediate = [...]byte{0xdd, 0xdd, 0xdd, 0xdd} // meta:immutable

func (*paddedIntermediate) getModeAnnouncement() []byte {
	return transportModePaddedIntermediate[:]
}

func (m *paddedIntermediate) WriteMsg(msg []byte) error {
	if len(msg)%tl.WordLen != 0 {
		return ErrNotMultiple{Len: len(msg)}
	}

	// Choose random padding between 0..15 bytes
	pad := make([]byte, 1)
	if _, err := rand.Read(pad); err != nil {
		return err
	}
	padLen := int(pad[0] & 0x0F)

	total := len(msg) + padLen

	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32(total))

	if _, err := m.conn.Write(header); err != nil {
		return err
	}
	if _, err := m.conn.Write(msg); err != nil {
		return err
	}
	if padLen > 0 {
		padding := make([]byte, padLen)
		if _, err := rand.Read(padding); err != nil {
			return err
		}
		if _, err := m.conn.Write(padding); err != nil {
			return err
		}
	}

	return nil
}

func (m *paddedIntermediate) ReadMsg() ([]byte, error) {
	lenBuf := make([]byte, 4)
	n, err := io.ReadFull(m.conn, lenBuf)
	if err != nil {
		return nil, err
	}
	if n != 4 {
		return nil, fmt.Errorf("size is not length of int32, expected 4 bytes, got %d", n)
	}

	total := int(binary.LittleEndian.Uint32(lenBuf))
	if total < 0 || total > 1<<30 {
		return nil, fmt.Errorf("invalid message size: %d", total)
	}

	buf := make([]byte, total)
	if _, err := io.ReadFull(m.conn, buf); err != nil {
		return nil, err
	}

	// Payload must be 4-byte aligned; padding is whatever remains (0..15 bytes)
	payloadLen := total - (total % tl.WordLen)
	if payloadLen < 0 || payloadLen > len(buf) {
		return nil, fmt.Errorf("invalid payload length %d for total %d", payloadLen, total)
	}

	return buf[:payloadLen], nil
}
