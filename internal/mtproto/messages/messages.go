// Copyright (c) 2025 @AmarnathCJD

package messages

// messages provides functions for encoding and decoding messages in MTProto.
// It handles the serialization and deserialization of messages using the MTProto protocol.
import (
	"bytes"
	"crypto/subtle"
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
	GetMsgID() int64
	GetSeqNo() int
}

type Encrypted struct {
	Msg         []byte
	MsgID       int64
	AuthKeyHash []byte
	AuthKey     []byte

	Salt      int64
	SessionID int64
	SeqNo     int32
	MsgKey    []byte
}

func (msg *Encrypted) Serialize(client MessageInformator, seqNo int32) ([]byte, error) {
	if msg.AuthKey != nil {
		client = packetInfo{key: msg.AuthKey, salt: msg.Salt, session: msg.SessionID, seq: seqNo}
	}
	obj := serializePacket(client, msg.Msg, msg.MsgID, seqNo)
	authKey := client.GetAuthKey()
	encryptedData, msgKey, err := ige.Encrypt(obj, authKey)
	if err != nil {
		return nil, fmt.Errorf("encrypting: %w", err)
	}

	buf := bytes.NewBuffer(nil)

	e := tl.NewEncoder(buf)
	e.PutRawBytes(utils.AuthKeyHash(authKey))
	e.PutRawBytes(msgKey)
	e.PutRawBytes(encryptedData)

	return buf.Bytes(), nil
}

func DeserializeEncrypted(data, authKey []byte) (*Encrypted, error) {
	if len(authKey) != 256 || len(data) < 24+48 || (len(data)-24)%16 != 0 {
		return nil, errors.New("invalid encrypted message or authorization key length")
	}
	msg := new(Encrypted)

	d := tl.NewDecoderBytes(data)
	keyHash := d.PopRawBytes(tl.LongLen)
	msg.AuthKeyHash = keyHash
	if !bytes.Equal(keyHash, utils.AuthKeyHash(authKey)) {
		return nil, errors.New("wrong encryption key")
	}
	msg.MsgKey = d.PopRawBytes(tl.Int128Len)
	encryptedData := d.PopRawBytes(len(data) - (tl.LongLen + tl.Int128Len))

	decrypted, err := ige.Decrypt(encryptedData, authKey, msg.MsgKey)
	if err != nil {
		return nil, fmt.Errorf("decrypting message: %w", err)
	}
	// Authenticate the entire plaintext before interpreting any of its fields.
	msgKey := ige.MessageKey(authKey, decrypted, true)
	if subtle.ConstantTimeCompare(msgKey, msg.MsgKey) != 1 {
		return nil, errors.New("wrong message key, can't trust sender")
	}
	d = tl.NewDecoderBytes(decrypted)
	msg.Salt = d.PopLong()
	msg.SessionID = d.PopLong()
	msg.MsgID = d.PopLong()
	msg.SeqNo = d.PopInt()
	messageLen := d.PopInt()

	if messageLen < 4 || messageLen%4 != 0 || int64(messageLen) > int64(len(decrypted)-32) {
		return nil, fmt.Errorf("invalid encrypted message length: %d (payload %d)", messageLen, len(decrypted))
	}
	paddingLen := len(decrypted) - 32 - int(messageLen)
	if paddingLen < 12 || paddingLen > 1024 {
		return nil, fmt.Errorf("invalid encrypted message padding length: %d", paddingLen)
	}
	if msg.SeqNo < 0 {
		return nil, errors.New("negative message sequence number")
	}

	mod := msg.MsgID & 3
	if mod != 1 && mod != 3 {
		return nil, fmt.Errorf("wrong bits of message_id: %d", mod)
	}

	msg.Msg = d.PopRawBytes(int(messageLen))

	return msg, d.CheckErr()
}

func (msg *Encrypted) GetMsg() []byte {
	return msg.Msg
}

func (msg *Encrypted) GetMsgID() int64 {
	return msg.MsgID
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
	if len(data) < 24 || binary.LittleEndian.Uint64(data[:8]) != 0 {
		return nil, errors.New("invalid unencrypted message header")
	}
	msg := new(Unencrypted)
	d := tl.NewDecoderBytes(data)
	_ = d.PopRawBytes(tl.LongLen) // authKeyHash, always 0 if unencrypted

	msg.MsgID = d.PopLong()

	mod := msg.MsgID & 3
	if mod != 1 && mod != 3 {
		return nil, fmt.Errorf("wrong bits of message_id: %#v", uint64(mod))
	}

	messageLen := d.PopUint()
	if messageLen < 4 || messageLen%4 != 0 || len(data)-(tl.LongLen+tl.LongLen+tl.WordLen) != int(messageLen) {
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

func (msg *Unencrypted) GetMsgID() int64 {
	return msg.MsgID
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

type packetInfo struct {
	key           []byte
	salt, session int64
	seq           int32
}

func (p packetInfo) GetAuthKey() []byte   { return p.key }
func (p packetInfo) GetSessionID() int64  { return p.session }
func (p packetInfo) GetServerSalt() int64 { return p.salt }
func (p packetInfo) GetSeqNo() int32      { return p.seq }

// AuthKeyForPacket permits a client rotating temporary keys to decrypt late
// replies with the key named in their authenticated envelope.
func AuthKeyForPacket(client MessageInformator, packet []byte) []byte {
	if resolver, ok := client.(interface{ GetAuthKeyForHash([]byte) []byte }); ok && len(packet) >= 8 {
		return resolver.GetAuthKeyForHash(packet[:8])
	}
	return client.GetAuthKey()
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
