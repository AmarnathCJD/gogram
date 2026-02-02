package mtproto

import (
	"reflect"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/mtproto/objects"
	"github.com/amarnathcjd/gogram/internal/session"
	"github.com/amarnathcjd/gogram/internal/utils"
	"github.com/pkg/errors"
)

// helper methods

// GetSessionID returns the current session id 🧐
func (m *MTProto) GetSessionID() int64 {
	return m.sessionId
}

// GetSeqNo returns seqno 🧐
func (m *MTProto) GetSeqNo() int32 {
	return m.seqNo
}

// GetServerSalt returns current server salt 🧐
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

func (m *MTProto) AddCustomServerRequestHandler(handler customHandlerFunc) {
	m.serverRequestHandlers = append(m.serverRequestHandlers, handler)
}

// methods (or functions, you name it), which are taken from here https://core.telegram.org/schema/mtproto
// in fact, these ones are "aliases" of the methods of the described objects (which
// are in internal/mtproto/objects). The idea is taken from github.com/xelaj/vk

func (m *MTProto) reqPQ(nonce *tl.Int128) (*objects.ResPQ, error) {
	return objects.ReqPQ(m, nonce)
}

func (m *MTProto) reqDHParams(nonce, serverNonce *tl.Int128, p, q []byte, publicKeyFingerprint int64, encryptedData []byte) (objects.ServerDHParams, error) {
	return objects.ReqDHParams(m, nonce, serverNonce, p, q, publicKeyFingerprint, encryptedData)
}

func (m *MTProto) setClientDHParams(nonce, serverNonce *tl.Int128, encryptedData []byte) (objects.SetClientDHParamsAnswer, error) {
	return objects.SetClientDHParams(m, nonce, serverNonce, encryptedData)
}

func (m *MTProto) ping(pingID int64) (*objects.Pong, error) {
	return objects.Ping(m, pingID)
}

func (m *MTProto) warnError(err error) {
	if m.Warnings != nil && err != nil {
		m.Warnings <- err
	}
}

func (m *MTProto) SaveSession() (err error) {
	return m.tokensStorage.Store(&session.Session{
		Key:      m.authKey,
		Hash:     m.authKeyHash,
		Salt:     m.serverSalt,
		Hostname: m.addr,
	})
}

func (m *MTProto) LoadSession(s *session.Session) {
	m.authKey = s.Key
	m.authKeyHash = s.Hash
	m.serverSalt = s.Salt
	m.addr = s.Hostname
}
