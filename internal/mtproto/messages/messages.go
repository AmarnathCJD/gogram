// Copyright (c) 2024 RoseLoverX

package messages

// messages provides functions for encoding and decoding messages in MTProto.
// It handles the serialization and deserialization of messages using the MTProto protocol.
import (
	"bytes"
	"encoding/binary"
	"fmt"

	"errors"

	ige "github.com/amarnathcjd/gogram/internal/aes_ige"
	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/utils"
)

// Common is a message (either encrypted or unencrypted) used for communication between the client and server.
type Common interface {
	GetMsg() []byte
	GetMsgID() int
	GetSeqNo() int
}

type Encrypted struct {
	Msg         []byte
	MsgID       int64
	AuthKeyHash []byte

	Salt      int64
	SessionID int64
	SeqNo     int32
	MsgKey    []byte
}

func (msg *Encrypted) Serialize(client MessageInformator, seqNo int32) ([]byte, error) {
	obj := serializePacket(client, msg.Msg, msg.MsgID, seqNo)
	encryptedData, msgKey, err := ige.Encrypt(obj, client.GetAuthKey())
	if err != nil {
		return nil, fmt.Errorf("encrypting: %w", err)
	}

	buf := bytes.NewBuffer(nil)

	e := tl.NewEncoder(buf)
	e.PutRawBytes(utils.AuthKeyHash(client.GetAuthKey()))
	e.PutRawBytes(msgKey)
	e.PutRawBytes(encryptedData)

	return buf.Bytes(), nil
}

func DeserializeEncrypted(data, authKey []byte) (*Encrypted, error) {
	msg := new(Encrypted)

	buf := bytes.NewBuffer(data)
	d, err := tl.NewDecoder(buf)
	if err != nil {
		return nil, err
	}
	keyHash := d.PopRawBytes(tl.LongLen)
	if !bytes.Equal(keyHash, utils.AuthKeyHash(authKey)) {
		return nil, errors.New("wrong encryption key")
	}
	msg.MsgKey = d.PopRawBytes(tl.Int128Len)
	encryptedData := d.PopRawBytes(len(data) - (tl.LongLen + tl.Int128Len))

	decrypted, err := ige.Decrypt(encryptedData, authKey, msg.MsgKey)
	if err != nil {
		return nil, fmt.Errorf("decrypting message: %w", err)
	}
	buf = bytes.NewBuffer(decrypted)
	d, err = tl.NewDecoder(buf)
	if err != nil {
		return nil, err
	}
	msg.Salt = d.PopLong()
	msg.SessionID = d.PopLong()
	msg.MsgID = d.PopLong()
	msg.SeqNo = d.PopInt()
	messageLen := d.PopInt()

	if len(decrypted) < int(messageLen)-(tl.LongLen+tl.LongLen+tl.LongLen+tl.WordLen+tl.WordLen) {
		return nil, fmt.Errorf("message is smaller than it's defining: have %v, but messageLen is %v", len(decrypted), messageLen)
	}

	mod := msg.MsgID & 3
	if mod != 1 && mod != 3 {
		return nil, fmt.Errorf("wrong bits of message_id: %d", mod)
	}

	msgKey := ige.MessageKey(authKey, decrypted, true)
	if !bytes.Equal(msgKey, msg.MsgKey) {
		return nil, errors.New("wrong message key, can't trust to sender")
	}
	msg.Msg = d.PopRawBytes(int(messageLen))

	return msg, nil
}

func (msg *Encrypted) GetMsg() []byte {
	return msg.Msg
}

func (msg *Encrypted) GetMsgID() int {
	return int(msg.MsgID)
}

func (msg *Encrypted) GetSeqNo() int {
	return int(msg.SeqNo)
}

type Unencrypted struct {
	Msg   []byte
	MsgID int64
}

func (msg *Unencrypted) Serialize(_ MessageInformator) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	e := tl.NewEncoder(buf)
	// authKeyHash, always 0 if unencrypted
	e.PutLong(0)
	e.PutLong(msg.MsgID)
	e.PutInt(int32(len(msg.Msg)))
	e.PutRawBytes(msg.Msg)
	return buf.Bytes(), nil
}

func DeserializeUnencrypted(data []byte) (*Unencrypted, error) {
	msg := new(Unencrypted)
	d, _ := tl.NewDecoder(bytes.NewBuffer(data))
	_ = d.PopRawBytes(tl.LongLen) // authKeyHash, always 0 if unencrypted

	msg.MsgID = d.PopLong()

	mod := msg.MsgID & 3
	if mod != 1 && mod != 3 {
		return nil, fmt.Errorf("wrong bits of message_id: %#v", uint64(mod))
	}

	messageLen := d.PopUint()
	if len(data)-(tl.LongLen+tl.LongLen+tl.WordLen) != int(messageLen) {
		fmt.Println(len(data), int(messageLen), int(messageLen+(tl.LongLen+tl.LongLen+tl.WordLen)))
		return nil, fmt.Errorf("message not equal defined size: have %v, want %v", len(data), messageLen)
	}

	var err error
	msg.Msg, err = d.GetRestOfMessage()
	if err != nil {
		return nil, fmt.Errorf("getting real message: %w", err)
	}

	return msg, nil
}

func (msg *Unencrypted) GetMsg() []byte {
	return msg.Msg
}

func (msg *Unencrypted) GetMsgID() int {
	return int(msg.MsgID)
}

func (msg *Unencrypted) GetSeqNo() int {
	return 0
}

// ------------------------------------------------------------------------------------------
//
// MessageInformator is used to provide information about the current session for message serialization.
// It is essentially an MTProto data structure.
type MessageInformator interface {
	GetSessionID() int64
	GetSeqNo() int32
	GetServerSalt() int64
	GetAuthKey() []byte
}

func serializePacket(client MessageInformator, msg []byte, messageID int64, seqNo int32) []byte {
	buf := bytes.NewBuffer(nil)
	d := tl.NewEncoder(buf)

	saltBytes := make([]byte, tl.LongLen)
	binary.LittleEndian.PutUint64(saltBytes, uint64(client.GetServerSalt()))
	d.PutRawBytes(saltBytes)
	d.PutLong(client.GetSessionID())
	d.PutLong(messageID)
	d.PutInt(seqNo)
	d.PutInt(int32(len(msg)))
	d.PutRawBytes(msg)
	return buf.Bytes()
}
