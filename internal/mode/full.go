package mode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

var (
	ErrChecksumMismatch = errors.New("checksum mismatch")
)

type full struct {
	conn  io.ReadWriter
	seqNo uint32
}

func (m *full) WriteMsg(msg []byte) error {
	msgLen := uint32(len(msg) + 12) // Header size is 12 bytes

	buf := make([]byte, msgLen)
	binary.LittleEndian.PutUint32(buf[0:4], msgLen)
	binary.LittleEndian.PutUint32(buf[4:8], m.seqNo)
	copy(buf[8:8+len(msg)], msg)

	checksum := crc32sum(buf[:8+len(msg)])
	binary.LittleEndian.PutUint32(buf[8+len(msg):], checksum)

	if _, err := m.conn.Write(buf); err != nil {
		return err
	}

	m.seqNo++
	return nil
}

func (m *full) ReadMsg() ([]byte, error) {
	bsize := make([]byte, 4)
	_, err := io.ReadFull(m.conn, bsize)
	if err != nil {
		return nil, err
	}
	size := int(binary.LittleEndian.Uint32(bsize))

	if size > 16*1024*1024 { // 16MB
		return nil, fmt.Errorf("invalid message size: %d", size)
	}

	buf := make([]byte, size)
	_, err = io.ReadFull(m.conn, buf[4:])
	if err != nil {
		return nil, err
	}
	copy(buf, bsize)

	_ = binary.LittleEndian.Uint32(buf[4:]) // seqNo
	checksum := binary.LittleEndian.Uint32(buf[size-4:])

	if crc32sum(buf[:size-4]) != checksum {
		return nil, ErrChecksumMismatch
	}

	return buf[8 : size-4], nil
}

func (*full) getModeAnnouncement() []byte {
	return nil
}

var _ Mode = (*full)(nil)

func crc32sum(b []byte) uint32 {
	return crc32.ChecksumIEEE(b)
}
