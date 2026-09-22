// Copyright (c) 2025 @AmarnathCJD

package gogram

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"reflect"

	"errors"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/mtproto/messages"
	"github.com/amarnathcjd/gogram/internal/mtproto/objects"
	"github.com/amarnathcjd/gogram/internal/session"
	"github.com/amarnathcjd/gogram/internal/utils"
)

// sendPacket sends a packet to the server and returns a channel that will receive the response.
func (m *MTProto) sendPacket(ctx context.Context, request tl.Object, msgID int64, expectedTypes ...reflect.Type) (chan tl.Object, int64, error) {
	if msgID == 0 {
		if err := m.writeMu.Lock(ctx); err != nil {
			return nil, 0, err
		}
		defer m.writeMu.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	msg, err := tl.Marshal(request)
	if err != nil {
		return nil, 0, fmt.Errorf("marshaling request: %w", err)
	}

	m.transportMu.Lock()
	tr := m.transport
	m.transportMu.Unlock()
	if tr == nil || !m.IsTcpActive() || m.disconnected.Load() || m.terminated.Load() {
		m.requestReconnect()
		return nil, 0, fmt.Errorf("transport is not active: %w", net.ErrClosed)
	}

	if msgID == 0 {
		msgID = m.genMsgID(m.timeOffset.Load())
	}

	if len(expectedTypes) > 0 && !isNullableResponse(request) {
		m.expectedTypes.Add(msgID, expectedTypes)
	}

	resp := m.getRespChannel()
	if isNullableResponse(request) {
		resp = make(chan tl.Object, 1)
		resp <- &objects.Null{}
	} else if !m.serviceModeActivated.Load() {
		m.responseChannels.Add(msgID, resp)
	}

	var data messages.Common
	if m.encrypted.Load() {
		key, salt := m.keyForRequest(request)
		data = &messages.Encrypted{Msg: msg, MsgID: msgID, AuthKey: key, AuthKeyHash: utils.AuthKeyHash(key), Salt: salt, SessionID: m.GetSessionID()}
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

	// A partially written frame cannot be resumed by another request. Closing
	// this transport is necessary when cancellation interrupts the write.
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = tr.Close(); close(interrupted) })
	errorSendPacket := tr.WriteMsg(data, seqNo)
	if !stop() {
		<-interrupted
		errorSendPacket = ctx.Err()
		m.requestReconnect()
	}

	if errorSendPacket != nil {
		m.responseChannels.Delete(msgID)
		m.expectedTypes.Delete(msgID)
		return nil, msgID, fmt.Errorf("writing message: %w", errorSendPacket)
	}
	return resp, msgID, nil
}

func (m *MTProto) writeRPCResponse(msgID int64, data tl.Object) error {
	v, ok := m.responseChannels.Pop(msgID)
	if !ok {
		return errors.New("no response channel found for messageId " + fmt.Sprint(msgID))
	}

	m.expectedTypes.Delete(msgID)
	if err := safeSend(v, data); err != nil {
		return fmt.Errorf("sending response: %w", err)
	}

	return nil
}

func safeSend(ch chan tl.Object, obj tl.Object) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("channel closed: %v", r)
		}
	}()

	select {
	case ch <- obj:
		return nil // Successfully sent
	default:
		return fmt.Errorf("channel is full or closed")
	}
}

func (m *MTProto) getRespChannel() chan tl.Object {
	if m.serviceModeActivated.Load() {
		return m.serviceChannel
	}
	return make(chan tl.Object, 1)
}

func isNotContentRelated(t tl.Object) bool {
	switch t.(type) {
	case *objects.PingParams,
		*objects.PingDelayDisconnectParams,
		*utils.PingParams,
		*objects.MsgsAck,
		*objects.Pong,
		*objects.MsgsStateReq,
		*objects.MsgsStateInfo,
		*objects.MsgResendReq,
		*objects.HttpWaitParams:
		return true
	default:
		return false
	}
}

func isNullableResponse(t tl.Object) bool {
	switch t.(type) {
	case *objects.Pong, *objects.MsgsAck, *objects.MsgsStateInfo, *objects.HttpWaitParams:
		return true
	default:
		return false
	}
}

func (m *MTProto) GetSessionID() int64 {
	return m.sessionId.Load()
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
	m.authMu.RLock()
	defer m.authMu.RUnlock()
	if m.enablePFS && len(m.tempAuthKey) > 0 {
		return m.tempServerSalt
	}
	return m.serverSalt.Load()
}

// GetAuthKey returns the current auth key used for message encryption.
// In PFS mode, once a temp key is available, it's used for all traffic.
// The permanent key is retained on m.authKey for re-binding.
func (m *MTProto) GetAuthKey() []byte {
	m.authMu.RLock()
	defer m.authMu.RUnlock()
	if m.enablePFS && len(m.tempAuthKey) > 0 {
		return bytes.Clone(m.tempAuthKey)
	}
	return bytes.Clone(m.authKey)
}

func (m *MTProto) SetAuthKey(key []byte) {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	if !bytes.Equal(m.authKey, key) {
		m.tempAuthKey, m.tempAuthKeyHash = nil, nil
		m.pendingTempAuthKey, m.pendingTempKeyHash = nil, nil
		m.previousTempAuthKey, m.previousTempKeyHash = nil, nil
		m.tempAuthExpiresAt, m.pendingTempExpiresAt = 0, 0
		m.tempServerSalt, m.pendingTempSalt, m.previousTempSalt = 0, 0, 0
	}
	m.authKey = bytes.Clone(key)
	m.authKeyHash = utils.AuthKeyHash(m.authKey)
}

func (m *MTProto) AddCustomServerRequestHandler(handler func(i any) bool) {
	if handler == nil {
		return
	}
	m.handlersMu.Lock()
	defer m.handlersMu.Unlock()
	m.serverRequestHandlers = append(m.serverRequestHandlers, handler)
}

// AddRPCResponseHandler installs a hook that fires for every RPC response
// received from the server, before it is delivered to the caller waiting on
// the response channel. The hook is called synchronously on the read loop —
// keep it fast. Return values are ignored (the response is always delivered
// to the original caller regardless).
//
// This is used by the telegram package to tee update-carrying RPC responses
// (e.g. messages.sendMessage returning an Updates envelope) into the update
// pipeline so raw-update handlers observe them.
func (m *MTProto) AddRPCResponseHandler(handler func(i any)) {
	if handler == nil {
		return
	}
	m.handlersMu.Lock()
	defer m.handlersMu.Unlock()
	m.rpcResponseHandlers = append(m.rpcResponseHandlers, handler)
}

func (m *MTProto) SaveSession(mem bool) (err error) {
	key, hash := m.permanentAuth()
	sess := &session.Session{
		Key:      key,
		Hash:     hash,
		Salt:     m.serverSalt.Load(),
		Hostname: m.GetAddr(),
		AppID:    m.appID,
	}

	if !mem {
		m.Logger.Debug("saving session to '%s'", filepath.Base(m.sessionStorage.Path()))
		return m.sessionStorage.Store(sess)
	}

	return nil
}

func (m *MTProto) DeleteSession() (err error) {
	return m.sessionStorage.Delete()
}

func (m *MTProto) _loadSession(s *session.Session) {
	m.SetAuthKey(s.Key)
	m.serverSalt.Store(s.Salt)
	m.SetAddr(s.Hostname)
	m.dcID.Store(0)
	m.appID = s.AppID
}

func (m *MTProto) permanentAuth() ([]byte, []byte) {
	m.authMu.RLock()
	defer m.authMu.RUnlock()
	return bytes.Clone(m.authKey), bytes.Clone(m.authKeyHash)
}

func (m *MTProto) GetAuthKeyForHash(hash []byte) []byte {
	m.authMu.RLock()
	defer m.authMu.RUnlock()
	for _, key := range []struct{ key, hash []byte }{
		{m.authKey, m.authKeyHash}, {m.tempAuthKey, m.tempAuthKeyHash},
		{m.pendingTempAuthKey, m.pendingTempKeyHash}, {m.previousTempAuthKey, m.previousTempKeyHash},
	} {
		if len(hash) == 8 && bytes.Equal(hash, key.hash) {
			return bytes.Clone(key.key)
		}
	}
	return nil
}

func (m *MTProto) keyForRequest(request tl.Object) ([]byte, int64) {
	m.authMu.RLock()
	defer m.authMu.RUnlock()
	if _, bind := request.(*objects.AuthBindTempAuthKeyParams); bind {
		return bytes.Clone(m.pendingTempAuthKey), m.pendingTempSalt
	}
	if m.enablePFS && len(m.tempAuthKey) > 0 {
		return bytes.Clone(m.tempAuthKey), m.tempServerSalt
	}
	return bytes.Clone(m.authKey), m.serverSalt.Load()
}

func (m *MTProto) updateSalt(msg messages.Common, salt int64) {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	if encrypted, ok := msg.(*messages.Encrypted); ok && len(encrypted.AuthKeyHash) > 0 {
		h := encrypted.AuthKeyHash
		switch {
		case bytes.Equal(h, m.pendingTempKeyHash):
			m.pendingTempSalt = salt
			return
		case bytes.Equal(h, m.tempAuthKeyHash):
			m.tempServerSalt = salt
			return
		case bytes.Equal(h, m.previousTempKeyHash):
			m.previousTempSalt = salt
			return
		}
	}
	m.serverSalt.Store(salt)
}
