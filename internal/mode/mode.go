// Copyright (c) 2025 @AmarnathCJD

package mode

import (
	"fmt"
	"io"
)

// Mode is an interface that defines how the connection sides determine the size of transmitted messages.
// Unlike HTTP or UDP connections, raw TCP connections and WebSockets don't have a standard way to
// determine the size of transmitted or received messages. Their main purpose is to transmit bytes in
// the correct order. Mode allows the connection sides to avoid analyzing traffic or using end-of-message
// sequences.
//
// In the MTProto world, Mode acts like a microprotocol. It packages messages in a container that
// announces its size in advance.
type Mode interface {
	WriteMsg([]byte) error // this is not same as the io.Writer
	ReadMsg() ([]byte, error)

	// getModeAnnouncement returns announce byte sequence to other side
	getModeAnnouncement() []byte
}

type Variant uint8

const (
	Abridged Variant = iota
	Intermediate
	PaddedIntermediate
	Full
)

func New(v Variant, conn io.ReadWriter) (Mode, error) {
	if conn == nil {
		return nil, ErrInterfaceIsNil
	}

	m, err := initMode(v, conn)
	if err != nil {
		return nil, err
	}
	announcement := m.getModeAnnouncement()
	_, err = conn.Write(announcement)
	if err != nil {
		return nil, fmt.Errorf("can't setup connection: %w", err)
	}

	return m, nil
}

// NewWithoutAnnouncement creates a mode without sending the announcement
// Used for obfuscated connections where protocol ID is embedded in obfuscation handshake
func NewWithoutAnnouncement(v Variant, conn io.ReadWriter) (Mode, error) {
	if conn == nil {
		return nil, ErrInterfaceIsNil
	}

	return initMode(v, conn)
}

func initMode(v Variant, conn io.ReadWriter) (Mode, error) {
	switch v {
	case PaddedIntermediate:
		panic("not supported yet")
	case Abridged:
		return &abridged{conn: conn}, nil
	case Intermediate:
		return &intermediate{conn: conn}, nil
	case Full:
		return &full{conn: conn}, nil
	default:
		return nil, ErrModeNotSupported
	}
}
