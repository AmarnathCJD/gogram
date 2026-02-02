// Copyright (c) 2024 RoseLoverX

package gogram

import (
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/mtproto/messages"
	"github.com/amarnathcjd/gogram/internal/mtproto/objects"
	"github.com/amarnathcjd/gogram/internal/session"
	"github.com/amarnathcjd/gogram/internal/utils"
	"github.com/pkg/errors"
)

func (m *MTProto) sendPacket(request tl.Object, expectedTypes ...reflect.Type) (chan tl.Object, error) {
	msg, err := tl.Marshal(request)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling request")
	}

	var (
		data  messages.Common
		msgID = m.genMsgID(m.timeOffset)
	)

	// adding types for parser if required
	if len(expectedTypes) > 0 {
		m.expectedTypes.Add(int(msgID), expectedTypes)
	}

	// dealing with response channel
	resp := m.getRespChannel()
	if isNullableResponse(request) {
		go func() { resp <- &objects.Null{} }() // goroutine cuz we don't read from it RIGHT NOW
	} else {
		m.responseChannels.Add(int(msgID), resp)
	}

	if m.encrypted {
		data = &messages.Encrypted{
			Msg:         msg,
			MsgID:       msgID,
			AuthKeyHash: m.authKeyHash,
		}
	} else {
		data = &messages.Unencrypted{
			Msg:   msg,
			MsgID: msgID,
		}
	}

	var seqNo int32
	if isNotContentRelated(request) {
		seqNo = m.GetSeqNo()
	} else {
		seqNo = m.UpdateSeqNo()
	}

	if m.transport == nil {
		return nil, errors.New("transport is nil, please use SetTransport")
	}
	errorSendPacket := m.transport.WriteMsg(data, seqNo)
	if errorSendPacket != nil {
		return nil, fmt.Errorf("writing message: %w", errorSendPacket)
	}
	return resp, nil
}

func (m *MTProto) writeRPCResponse(msgID int, data tl.Object) error {
	v, ok := m.responseChannels.Get(msgID)
	if !ok {
		return errors.New("no response channel found for messageId " + fmt.Sprint(msgID))
	}
	v <- data
	m.responseChannels.Delete(msgID)
	m.expectedTypes.Delete(msgID)
	return nil
}

func (m *MTProto) getRespChannel() chan tl.Object {
	if m.serviceModeActivated {
		return m.serviceChannel
	}
	return make(chan tl.Object)
}

func isNotContentRelated(t tl.Object) bool {
	switch t.(type) {
	case *objects.PingParams,
		*objects.MsgsAck,
		*objects.GzipPacked:
		return true
	default:
		return false
	}
}

func isNullableResponse(t tl.Object) bool {
	switch t.(type) {
	case *objects.Pong, *objects.MsgsAck:
		return true
	default:
		return false
	}
}

func (m *MTProto) GetSessionID() int64 {
	return m.sessionId
}

// GetSeqNo returns seqno
func (m *MTProto) GetSeqNo() int32 {
	return m.currentSeqNo.Load() * 2
}

func (m *MTProto) UpdateSeqNo() int32 {
	// https://core.telegram.org/mtproto/description#message-sequence-number-msg-seqno
	return (m.currentSeqNo.Add(1)-1)*2 + 1
}

// GetServerSalt returns current server salt
func (m *MTProto) GetServerSalt() int64 {
	return m.serverSalt
}

// GetAuthKey returns decryption key of current session salt 🧐
func (m *MTProto) GetAuthKey() []byte {
	return m.authKey
}

func (m *MTProto) SetAuthKey(key []byte) {
	m.authKey = key
	m.authKeyHash = utils.AuthKeyHash(m.authKey)
}

func (m *MTProto) MakeRequest(msg tl.Object) (any, error) {
	return m.makeRequest(msg)
}

func (m *MTProto) MakeRequestWithHintToDecoder(msg tl.Object, expectedTypes ...reflect.Type) (any, error) {
	if len(expectedTypes) == 0 {
		return nil, errors.New("expected a few hints. If you don't need it, use m.MakeRequest")
	}
	return m.makeRequest(msg, expectedTypes...)
}

func (m *MTProto) AddCustomServerRequestHandler(handler func(i any) bool) {
	m.serverRequestHandlers = append(m.serverRequestHandlers, handler)
}

func (m *MTProto) SaveSession() (err error) {
	m.Logger.Debug("saving session -> ", filepath.Base(m.sessionStorage.Path()))
	return m.sessionStorage.Store(&session.Session{
		Key:      m.authKey,
		Hash:     m.authKeyHash,
		Salt:     m.serverSalt,
		Hostname: m.Addr,
		AppID:    m.appID,
	})
}

func (m *MTProto) DeleteSession() (err error) {
	return m.sessionStorage.Delete()
}

func (m *MTProto) _loadSession(s *session.Session) {
	m.authKey = s.Key
	m.authKeyHash = s.Hash
	m.serverSalt = s.Salt
	m.Addr = s.Hostname
	m.appID = s.AppID
}

func (m *MTProto) reqPQ(nonce *tl.Int128) (*objects.ResPQ, error) {
	return objects.ReqPQ(m, nonce)
}

func (m *MTProto) reqDHParams(nonce, serverNonce *tl.Int128, p, q []byte, publicKeyFingerprint int64, encryptedData []byte) (objects.ServerDHParams, error) {
	return objects.ReqDHParams(m, nonce, serverNonce, p, q, publicKeyFingerprint, encryptedData)
}

func (m *MTProto) setClientDHParams(nonce, serverNonce *tl.Int128, encryptedData []byte) (objects.SetClientDHParamsAnswer, error) {
	return objects.SetClientDHParams(m, nonce, serverNonce, encryptedData)
}
