// Copyright (c) 2024 RoseLoverX

package objects

import (
	"fmt"
	"reflect"

	"errors"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

type requester interface {
	MakeRequest(tl.Object) (any, error)
}

type ReqPQParams struct {
	Nonce *tl.Int128
}

func (*ReqPQParams) CRC() uint32 {
	return 0x60469778
}

func ReqPQ(m requester, nonce *tl.Int128) (*ResPQ, error) {
	data, err := m.MakeRequest(&ReqPQParams{Nonce: nonce})
	if err != nil {
		return nil, fmt.Errorf("sending ReqPQ: %w", err)
	}

	resp, ok := data.(*ResPQ)
	if !ok {
		return nil, errors.New("got invalid response type: " + reflect.TypeOf(data).String())
	}

	return resp, nil
}

type AuthBindTempAuthKeyParams struct {
	PermAuthKeyID    int64
	Nonce            int64
	ExpiresAt        int32
	EncryptedMessage []byte
}

func (*AuthBindTempAuthKeyParams) CRC() uint32 {
	return 0xcdd42a05
}

type ReqPQMultiParams struct {
	Nonce *tl.Int128
}

func (*ReqPQMultiParams) CRC() uint32 {
	return 0xbe7e8ef1
}

func ReqPQMulti(m requester, nonce *tl.Int128) (*ResPQ, error) {
	data, err := m.MakeRequest(&ReqPQMultiParams{Nonce: nonce})
	if err != nil {
		return nil, fmt.Errorf("sending ReqPQMulti: %w", err)
	}

	resp, ok := data.(*ResPQ)
	if !ok {
		return nil, errors.New("got invalid response type: " + reflect.TypeOf(data).String())
	}

	return resp, nil
}

type ReqDHParamsParams struct {
	Nonce                *tl.Int128
	ServerNonce          *tl.Int128
	P                    []byte
	Q                    []byte
	PublicKeyFingerprint int64
	EncryptedData        []byte
}

func (*ReqDHParamsParams) CRC() uint32 {
	return 0xd712e4be
}

func ReqDHParams(
	m requester,
	nonce, serverNonce *tl.Int128, p, q []byte, publicKeyFingerprint int64, encryptedData []byte,
) (ServerDHParams, error) {
	data, err := m.MakeRequest(&ReqDHParamsParams{
		Nonce:                nonce,
		ServerNonce:          serverNonce,
		P:                    p,
		Q:                    q,
		PublicKeyFingerprint: publicKeyFingerprint,
		EncryptedData:        encryptedData,
	})
	if err != nil {
		return nil, fmt.Errorf("sending ReqDHParams: %w", err)
	}

	resp, ok := data.(ServerDHParams)
	if !ok {
		return nil, errors.New("got invalid response type: " + reflect.TypeOf(data).String())
	}

	return resp, nil
}

type SetClientDHParamsParams struct {
	Nonce         *tl.Int128
	ServerNonce   *tl.Int128
	EncryptedData []byte
}

func (*SetClientDHParamsParams) CRC() uint32 {
	return 0xf5045f1f
}

func SetClientDHParams(m requester, nonce, serverNonce *tl.Int128, encryptedData []byte) (SetClientDHParamsAnswer, error) {
	data, err := m.MakeRequest(&SetClientDHParamsParams{
		Nonce:         nonce,
		ServerNonce:   serverNonce,
		EncryptedData: encryptedData,
	})
	if err != nil {
		return nil, fmt.Errorf("sending Ping: %w", err)
	}

	resp, ok := data.(SetClientDHParamsAnswer)
	if !ok {
		return nil, errors.New("got invalid response type: " + reflect.TypeOf(data).String())
	}

	return resp, nil
}

// rpc_drop_answer
// get_future_salts

type PingParams struct {
	PingID int64
}

func (*PingParams) CRC() uint32 {
	return 0x7abe77ec
}

type PingDelayDisconnectParams struct {
	PingID          int64
	DisconnectDelay int32
}

func (*PingDelayDisconnectParams) CRC() uint32 {
	return 0xf3427b8c
}

// ping_delay_disconnect
// destroy_session
// http_wait

// set_client_DH_params#f5045f1f nonce:int128 server_nonce:int128 encrypted_data:bytes = Set_client_DH_params_answer;

// rpc_drop_answer#58e4a740 req_msg_id:long = RpcDropAnswer;
// get_future_salts#b921bd04 num:int = FutureSalts;
// ping_delay_disconnect#f3427b8c ping_id:long disconnect_delay:int = Pong;
// destroy_session#e7512126 session_id:long = DestroySessionRes;

// http_wait#9299359f max_delay:int wait_after:int max_wait:int = HttpWait;
