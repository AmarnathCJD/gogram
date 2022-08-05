// Copyright (c) 2022 RoseLoverX

package mtproto

// methods (or functions, you name it), which are taken from here https://core.telegram.org/schema/mtproto
// in fact, these ones are "aliases" of the methods of the described objects (which
// are in internal/mtproto/objects). The idea is taken from github.com/xelaj/vk

import (
	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/mtproto/objects"
)

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
