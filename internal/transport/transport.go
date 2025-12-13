// Copyright (c) 2025 @AmarnathCJD

package transport

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"reflect"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/mode"
	"github.com/amarnathcjd/gogram/internal/mtproto/messages"
)

type Transport interface {
	Close() error
	WriteMsg(msg messages.Common, seqNo int32) error
	ReadMsg() (messages.Common, error)
}

type transport struct {
	conn Conn
	mode Mode
	m    messages.MessageInformator
}

func NewTransport(m messages.MessageInformator, conn ConnConfig, modeVariant mode.Variant) (Transport, error) {
	t := &transport{
		m: m,
	}

	var err error
	var isObfuscated bool
	var isMTProxy bool
	switch cfg := conn.(type) {
	case TCPConnConfig:
		t.conn, isObfuscated, err = NewTCP(cfg)
		if cfg.Socks != nil && cfg.Socks.Type == "mtproxy" {
			isMTProxy = true
		}
	case WSConnConfig:
		cfg.ModeVariant = uint8(modeVariant)
		t.conn, err = NewWebSocket(cfg)
		isObfuscated = true
	default:
		return nil, fmt.Errorf("unsupported connection type %v", reflect.TypeOf(conn).String())
	}
	if err != nil {
		return nil, fmt.Errorf("setup connection: %w", err)
	}

	// For MTProxy, always use Intermediate mode since the obfuscation header
	// contains the protocol tag (0xeeeeeeee or 0xdddddddd)
	if isMTProxy {
		modeVariant = mode.Intermediate
	}

	// already sent in obfuscation handshake
	if isObfuscated {
		t.mode, err = mode.NewWithoutAnnouncement(modeVariant, t.conn)
	} else {
		t.mode, err = mode.New(modeVariant, t.conn)
	}
	if err != nil {
		return nil, fmt.Errorf("setup mode: %w", err)
	}

	return t, nil
}

func (t *transport) Close() error {
	return t.conn.Close()
}

func (t *transport) WriteMsg(msg messages.Common, seqNo int32) error {
	var data []byte
	switch message := msg.(type) {
	case *messages.Unencrypted:
		data, _ = message.Serialize(t.m)

	case *messages.Encrypted:
		var err error
		data, err = message.Serialize(t.m, seqNo)
		if err != nil {
			return fmt.Errorf("serializing message: %w", err)
		}

	default:
		return fmt.Errorf("supported only mtproto predefined messages, got %v", reflect.TypeOf(msg).String())
	}

	err := t.mode.WriteMsg(data)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	return nil
}

func (t *transport) ReadMsg() (messages.Common, error) {
	data, err := t.mode.ReadMsg()
	if err != nil {
		switch err {
		case io.EOF, context.Canceled:
			return nil, err
		default:
			return nil, fmt.Errorf("reading message: %w", err)
		}
	}

	if len(data) == tl.WordLen {
		code := int64(binary.LittleEndian.Uint32(data))
		return nil, ErrCode(code)
	}

	var msg messages.Common
	if isPacketEncrypted(data) {
		msg, err = messages.DeserializeEncrypted(data, t.m.GetAuthKey())
	} else {
		msg, err = messages.DeserializeUnencrypted(data)
	}
	if err != nil {
		return nil, fmt.Errorf("parsing message: %w", err)
	}

	mod := msg.GetMsgID() & 3 // why 3? only god knows why
	if mod != 1 && mod != 3 {
		return nil, fmt.Errorf("wrong bits of message_id: %d", mod)
	}

	return msg, nil
}

func isPacketEncrypted(data []byte) bool {
	if len(data) < tl.DoubleLen {
		return false
	}
	authKeyHash := data[:tl.DoubleLen]
	return binary.LittleEndian.Uint64(authKeyHash) != 0
}

type ErrCode int64

func (e ErrCode) Error() string {
	return fmt.Sprintf("code %v", int64(e))
}
