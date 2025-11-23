// Copyright (c) 2024 RoseLoverX

package gogram

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"errors"

	ige "github.com/amarnathcjd/gogram/internal/aes_ige"
	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/keys"
	"github.com/amarnathcjd/gogram/internal/math"
	"github.com/amarnathcjd/gogram/internal/mtproto/objects"
	"github.com/amarnathcjd/gogram/internal/session"
	"github.com/amarnathcjd/gogram/internal/utils"
)

// https://core.telegram.org/mtproto/auth_key
func (m *MTProto) makeAuthKey() error {
	return m.makeAuthKeyInternal(0)
}

func (m *MTProto) makeTempAuthKey(expiresIn int32) error {
	if expiresIn <= 0 {
		expiresIn = 24 * 60 * 60
	}
	return m.makeAuthKeyInternal(expiresIn)
}

func (m *MTProto) makeAuthKeyInternal(expiresIn int32) error {
	isTemp := expiresIn > 0

	m.serviceModeActivated = true
	defer func() { m.serviceModeActivated = false }()

	maxRetries := 5
nonceCreate:
	nonceFirst := tl.RandomInt128()
	var (
		res *objects.ResPQ
		err error
	)

	if m.cdn {
		res, err = m.reqPQMulti(nonceFirst)
	} else {
		res, err = m.reqPQ(nonceFirst)
	}

	if err != nil {
		return fmt.Errorf("reqPQ: %w", err)
	}

	if nonceFirst.Cmp(res.Nonce.Int) != 0 {
		if maxRetries > 0 {
			maxRetries--
			time.Sleep(200 * time.Millisecond)
			goto nonceCreate
		}
		return fmt.Errorf("reqPQ: nonce mismatch (%v, %v)", nonceFirst, res.Nonce)
	}

	found := false
	for _, b := range res.Fingerprints {
		if m.cdn {
			for _, key := range m.cdnKeys {
				if uint64(b) == binary.LittleEndian.Uint64(keys.RSAFingerprint(key)) {
					found = true
					m.publicKey = key
					break
				}
			}
		}
		if uint64(b) == binary.LittleEndian.Uint64(keys.RSAFingerprint(m.publicKey)) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("reqPQ: no matching fingerprint")
	}

	pq := big.NewInt(0).SetBytes(res.Pq)
	p, q := math.Factorize(pq)
	if p == nil || q == nil {
		p, q = math.Fac(pq)
	}
	nonceSecond := tl.RandomInt256()
	nonceServer := res.ServerNonce

	var message []byte
	if isTemp {
		message, err = tl.Marshal(&objects.PQInnerDataTempDc{
			Pq:          res.Pq,
			P:           p.Bytes(),
			Q:           q.Bytes(),
			Nonce:       nonceFirst,
			ServerNonce: nonceServer,
			NewNonce:    nonceSecond,
			Dc:          int32(m.GetDC()),
			ExpiresIn:   expiresIn,
		})
	} else {
		message, err = tl.Marshal(&objects.PQInnerData{
			Pq:          res.Pq,
			P:           p.Bytes(),
			Q:           q.Bytes(),
			Nonce:       nonceFirst,
			ServerNonce: nonceServer,
			NewNonce:    nonceSecond,
		})
	}
	if err != nil {
		m.Logger.WithField("error", err).Debug("makeAuthKey: failed to marshal pq inner data")
		return err
	}

	hashAndMsg := make([]byte, 255)
	copy(hashAndMsg, append(utils.Sha1(string(message)), message...))

	encryptedMessage := math.DoRSAencrypt(hashAndMsg, m.publicKey)

	keyFingerprint := int64(binary.LittleEndian.Uint64(keys.RSAFingerprint(m.publicKey)))
	dhResponse, err := m.reqDHParams(nonceFirst, nonceServer, p.Bytes(), q.Bytes(), keyFingerprint, encryptedMessage)
	if err != nil {
		return fmt.Errorf("reqDHParams: %w", err)
	}
	dhParams, ok := dhResponse.(*objects.ServerDHParamsOk)
	if !ok {
		return fmt.Errorf("reqDHParams: invalid response")
	}

	if nonceFirst.Cmp(dhParams.Nonce.Int) != 0 {
		return fmt.Errorf("reqDHParams: nonce mismatch")
	}
	if nonceServer.Cmp(dhParams.ServerNonce.Int) != 0 {
		return fmt.Errorf("reqDHParams: server nonce mismatch")
	}

	decodedMessage, err := ige.DecryptMessageWithTempKeys(dhParams.EncryptedAnswer, nonceSecond.Int, nonceServer.Int)
	if err != nil {
		m.Logger.WithError(err).Debug("decrypt failed - retrying auth key generation")
		return m.makeAuthKeyInternal(expiresIn)
	}

	data, err := tl.DecodeUnknownObject(decodedMessage)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	dhi, ok := data.(*objects.ServerDHInnerData)
	if !ok {
		return fmt.Errorf("decode: invalid response")
	}
	if nonceFirst.Cmp(dhi.Nonce.Int) != 0 {
		return fmt.Errorf("decode: nonce mismatch")
	}
	if nonceServer.Cmp(dhi.ServerNonce.Int) != 0 {
		return fmt.Errorf("decode: server nonce mismatch")
	}

	_, gB, gAB := math.MakeGAB(dhi.G, big.NewInt(0).SetBytes(dhi.GA), big.NewInt(0).SetBytes(dhi.DhPrime))

	authKey := gAB.Bytes()
	if authKey[0] == 0 {
		authKey = authKey[1:]
	}

	t4 := make([]byte, 32+1+8)
	copy(t4[0:], nonceSecond.Bytes())
	t4[32] = 1
	copy(t4[33:], utils.Sha1Byte(authKey)[0:8])
	nonceHash1 := utils.Sha1Byte(t4)[4:20]
	salt := make([]byte, tl.LongLen)
	copy(salt, nonceSecond.Bytes()[:8])
	math.Xor(salt, nonceServer.Bytes()[:8])
	newSalt := int64(binary.LittleEndian.Uint64(salt))

	clientDHData, err := tl.Marshal(&objects.ClientDHInnerData{
		Nonce:       nonceFirst,
		ServerNonce: nonceServer,
		Retry:       0,
		GB:          gB.Bytes(),
	})
	if err != nil {
		m.Logger.WithField("error", err).Debug("makeAuthKey: failed to marshal client dh inner data")
		return err
	}

	encryptedMessage, err = ige.EncryptMessageWithTempKeys(clientDHData, nonceSecond.Int, nonceServer.Int)
	if err != nil {
		return errors.New("dh: " + err.Error())
	}

	dhGenStatus, err := m.setClientDHParams(nonceFirst, nonceServer, encryptedMessage)
	if err != nil {
		return errors.New("dh: " + err.Error())
	}

	dhg, ok := dhGenStatus.(*objects.DHGenOk)
	if !ok {
		return fmt.Errorf("invalid response")
	}
	if nonceFirst.Cmp(dhg.Nonce.Int) != 0 {
		return fmt.Errorf("handshake: Wrong nonce: %v, %v", nonceFirst, dhg.Nonce)
	}
	if nonceServer.Cmp(dhg.ServerNonce.Int) != 0 {
		return fmt.Errorf("handshake: Wrong server_nonce: %v, %v", nonceServer, dhg.ServerNonce)
	}
	if !bytes.Equal(nonceHash1, dhg.NewNonceHash1.Bytes()) {
		return fmt.Errorf(
			"handshake: Wrong new_nonce_hash1: %v, %v",
			hex.EncodeToString(nonceHash1),
			hex.EncodeToString(dhg.NewNonceHash1.Bytes()),
		)
	}

	if isTemp {
		m.tempAuthKey = authKey
		m.tempAuthKeyHash = utils.AuthKeyHash(authKey)
		m.tempAuthExpiresAt = time.Now().Unix() + int64(expiresIn)
		m.serverSalt = newSalt
	} else {
		m.SetAuthKey(authKey)
		m.serverSalt = newSalt
		m.encrypted = true
		if err := m.SaveSession(m.memorySession); err != nil {
			m.Logger.WithError(err).Error("failed to save session")
		}
	}

	return nil
}

// createTempAuthKey performs the temporary auth key handshake
func (m *MTProto) createTempAuthKey(expiresIn int32) error {
	cfg := Config{
		AuthKeyFile:     "",
		AuthAESKey:      "",
		SessionStorage:  session.NewInMemory(),
		MemorySession:   true,
		AppID:           m.appID,
		EnablePFS:       false,
		ServerHost:      m.Addr,
		PublicKey:       m.publicKey,
		DataCenter:      m.GetDC(),
		Logger:          m.Logger.Clone().WithPrefix("gogram [mtp-pfs]"),
		Proxy:           m.proxy,
		Mode:            "Abridged",
		Ipv6:            m.IpV6,
		CustomHost:      true,
		LocalAddr:       m.localAddr,
		Timeout:         int(m.timeout.Seconds()),
		ReqTimeout:      int(m.reqTimeout.Seconds()),
		UseWebSocket:    m.useWebSocket,
		UseWebSocketTLS: m.useWebSocketTLS,
	}

	tmp, err := NewMTProto(cfg)
	if err != nil {
		return fmt.Errorf("createTempAuthKey: creating temp MTProto: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tmp.connect(ctx); err != nil {
		return fmt.Errorf("createTempAuthKey: connecting temp MTProto: %w", err)
	}
	tmp.tcpState.SetActive(true)
	tmp.startReadingResponses(ctx)

	if err := tmp.makeTempAuthKey(expiresIn); err != nil {
		return fmt.Errorf("createTempAuthKey: makeTempAuthKey on temp connection: %w", err)
	}

	// Copy the generated temporary auth key and its metadata back to the main
	// MTProto instance. The server salt associated with the temporary key is
	// also propagated so that subsequent messages encrypted with the temp key
	// use the correct salt.
	m.tempAuthKey = tmp.tempAuthKey
	m.tempAuthKeyHash = tmp.tempAuthKeyHash
	m.tempAuthExpiresAt = tmp.tempAuthExpiresAt
	m.serverSalt = tmp.serverSalt

	tmp.Terminate()
	m.Logger.Debug("temporary auth key created successfully")
	return nil
}

// bindTempAuthKey binds the temporary auth key to the permanent auth key using auth.bindTempAuthKey.
func (m *MTProto) bindTempAuthKey() error {
	if m.tempAuthKey == nil {
		return errors.New("bindTempAuthKey: tempAuthKey is nil")
	}
	if len(m.authKey) == 0 {
		return errors.New("bindTempAuthKey: permanent authKey is nil")
	}

	permKeyHash := utils.AuthKeyHash(m.authKey)
	permAuthKeyID := int64(binary.LittleEndian.Uint64(permKeyHash))

	tempKeyHash := utils.AuthKeyHash(m.tempAuthKey)
	tempAuthKeyID := int64(binary.LittleEndian.Uint64(tempKeyHash))

	// Use current session ID as temp_session_id for the binding.
	tempSessionID := m.sessionId

	expiresAt := int32(m.tempAuthExpiresAt)
	if expiresAt == 0 {
		expiresAt = int32(time.Now().Unix() + 24*60*60)
	}

	nonce := utils.GenerateSessionID()
	msgID := m.genMsgID(m.timeOffset)

	inner := &objects.BindAuthKeyInner{
		Nonce:         nonce,
		TempAuthKeyID: tempAuthKeyID,
		PermAuthKeyID: permAuthKeyID,
		TempSessionID: tempSessionID,
		ExpiresAt:     expiresAt,
	}

	innerBytes, err := tl.Marshal(inner)
	if err != nil {
		return fmt.Errorf("marshal BindAuthKeyInner: %w", err)
	}

	// Build MTProto v1 plaintext: random:int128 + msg_id + seqno(0) + msg_len + inner.
	random128 := utils.RandomBytes(16)
	plaintext := make([]byte, 0, 16+8+4+4+len(innerBytes))
	plaintext = append(plaintext, random128...)

	buf8 := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf8, uint64(msgID))
	plaintext = append(plaintext, buf8...)

	buf4 := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf4, 0) // seqno = 0
	plaintext = append(plaintext, buf4...)

	binary.LittleEndian.PutUint32(buf4, uint32(len(innerBytes)))
	plaintext = append(plaintext, buf4...)
	plaintext = append(plaintext, innerBytes...)

	// Encrypt binding message with permanent auth key using MTProto 1.0 helpers.
	cipher, msgKey, err := ige.EncryptV1(plaintext, m.authKey)
	if err != nil {
		return fmt.Errorf("encrypt BindAuthKeyInner: %w", err)
	}

	encryptedMessage := make([]byte, 0, len(permKeyHash)+len(msgKey)+len(cipher))
	encryptedMessage = append(encryptedMessage, permKeyHash...)
	encryptedMessage = append(encryptedMessage, msgKey...)
	encryptedMessage = append(encryptedMessage, cipher...)

	params := &objects.AuthBindTempAuthKeyParams{
		PermAuthKeyID:    permAuthKeyID,
		Nonce:            nonce,
		ExpiresAt:        expiresAt,
		EncryptedMessage: encryptedMessage,
	}

	// Send auth.bindTempAuthKey with the same msg_id used inside the MTProto
	// v1 binding message, as required by the PFS specification.
	respCh, _, err := m.sendPacketWithMsgID(params, msgID)
	if err != nil {
		return fmt.Errorf("auth.bindTempAuthKey: %w", err)
	}

	response := <-respCh
	if rpcErr, ok := response.(*objects.RpcError); ok {
		return fmt.Errorf("auth.bindTempAuthKey: %w", RpcErrorToNative(rpcErr))
	}

	// Any non-error response is treated as success; the exact Bool value is not
	// critical here, as the server typically returns true on success.
	return nil
}

// makeTempAuthKey creates a temporary authorization key using p_q_inner_data_temp_dc,
// as described in the MTProto auth_key documentation. The resulting key is stored
// in tempAuthKey/tempAuthKeyHash and is not persisted to the session.
// expiresIn is the desired lifetime of the temporary key in seconds.
func (m *MTProto) makeTempAuthKey(expiresIn int32) error {
	if expiresIn <= 0 {
		expiresIn = 24 * 60 * 60 // default to 24 hours
	}

	// Use service mode so that responses for the low-level MTProto handshake
	// (ResPQ, ServerDHParams, etc.) bypass the normal RPC pipeline.
	m.serviceModeActivated = true
	defer func() { m.serviceModeActivated = false }()

	maxRetries := 5
nonceCreate:
	nonceFirst := tl.RandomInt128()
	var (
		res *objects.ResPQ
		err error
	)

	if m.cdn {
		res, err = m.reqPQMulti(nonceFirst)
	} else {
		res, err = m.reqPQ(nonceFirst)
	}

	if err != nil {
		return fmt.Errorf("reqPQ: %w", err)
	}

	if nonceFirst.Cmp(res.Nonce.Int) != 0 {
		if maxRetries > 0 {
			maxRetries--
			time.Sleep(200 * time.Millisecond)
			goto nonceCreate
		}
		return fmt.Errorf("reqPQ: nonce mismatch (%v, %v)", nonceFirst, res.Nonce)
	}

	found := false
	for _, b := range res.Fingerprints {
		if m.cdn {
			for _, key := range m.cdnKeys {
				if uint64(b) == binary.LittleEndian.Uint64(keys.RSAFingerprint(key)) {
					found = true
					m.publicKey = key
					break
				}
			}
		}
		if uint64(b) == binary.LittleEndian.Uint64(keys.RSAFingerprint(m.publicKey)) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("reqPQ: no matching fingerprint")
	}

	// (encoding) p_q_inner_data_temp_dc
	pq := big.NewInt(0).SetBytes(res.Pq)
	p, q := math.Factorize(pq)
	if p == nil || q == nil {
		p, q = math.Fac(pq)
	}
	nonceSecond := tl.RandomInt256()
	nonceServer := res.ServerNonce

	dcID := int32(m.GetDC())
	message, err := tl.Marshal(&objects.PQInnerDataTempDc{
		Pq:          res.Pq,
		P:           p.Bytes(),
		Q:           q.Bytes(),
		Nonce:       nonceFirst,
		ServerNonce: nonceServer,
		NewNonce:    nonceSecond,
		Dc:          dcID,
		ExpiresIn:   expiresIn,
	})
	if err != nil {
		m.Logger.WithField("error", err).Debug("makeTempAuthKey: failed to marshal pq inner data temp dc")
		return err
	}

	hashAndMsg := make([]byte, 255)
	copy(hashAndMsg, append(utils.Sha1(string(message)), message...))

	encryptedMessage := math.DoRSAencrypt(hashAndMsg, m.publicKey)

	keyFingerprint := int64(binary.LittleEndian.Uint64(keys.RSAFingerprint(m.publicKey)))
	dhResponse, err := m.reqDHParams(nonceFirst, nonceServer, p.Bytes(), q.Bytes(), keyFingerprint, encryptedMessage)
	if err != nil {
		return fmt.Errorf("reqDHParams: %w", err)
	}
	dhParams, ok := dhResponse.(*objects.ServerDHParamsOk)
	if !ok {
		return fmt.Errorf("reqDHParams: invalid response")
	}

	if nonceFirst.Cmp(dhParams.Nonce.Int) != 0 {
		return fmt.Errorf("reqDHParams: nonce mismatch")
	}
	if nonceServer.Cmp(dhParams.ServerNonce.Int) != 0 {
		return fmt.Errorf("reqDHParams: server nonce mismatch")
	}

	decodedMessage, err := ige.DecryptMessageWithTempKeys(dhParams.EncryptedAnswer, nonceSecond.Int, nonceServer.Int)
	if err != nil {
		m.Logger.WithError(err).Debug("decrypt failed - retrying temp auth key generation")
		return m.makeTempAuthKey(expiresIn)
	}

	data, err := tl.DecodeUnknownObject(decodedMessage)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	dhi, ok := data.(*objects.ServerDHInnerData)
	if !ok {
		return fmt.Errorf("decode: invalid response")
	}
	if nonceFirst.Cmp(dhi.Nonce.Int) != 0 {
		return fmt.Errorf("decode: nonce mismatch")
	}
	if nonceServer.Cmp(dhi.ServerNonce.Int) != 0 {
		return fmt.Errorf("decode: server nonce mismatch")
	}

	_, gB, gAB := math.MakeGAB(dhi.G, big.NewInt(0).SetBytes(dhi.GA), big.NewInt(0).SetBytes(dhi.DhPrime))

	tempKey := gAB.Bytes()
	if tempKey[0] == 0 {
		tempKey = tempKey[1:]
	}

	// derive server salt for the temporary key
	t4 := make([]byte, 32+1+8)
	copy(t4[0:], nonceSecond.Bytes())
	t4[32] = 1
	copy(t4[33:], utils.Sha1Byte(tempKey)[0:8])
	nonceHash1 := utils.Sha1Byte(t4)[4:20]
	salt := make([]byte, tl.LongLen)
	copy(salt, nonceSecond.Bytes()[:8])
	math.Xor(salt, nonceServer.Bytes()[:8])
	newSalt := int64(binary.LittleEndian.Uint64(salt))

	clientDHData, err := tl.Marshal(&objects.ClientDHInnerData{
		Nonce:       nonceFirst,
		ServerNonce: nonceServer,
		Retry:       0,
		GB:          gB.Bytes(),
	})
	if err != nil {
		m.Logger.WithField("error", err).Debug("makeTempAuthKey: failed to marshal client dh inner data")
		return err
	}

	encryptedMessage, err = ige.EncryptMessageWithTempKeys(clientDHData, nonceSecond.Int, nonceServer.Int)
	if err != nil {
		return errors.New("dh: " + err.Error())
	}

	dhGenStatus, err := m.setClientDHParams(nonceFirst, nonceServer, encryptedMessage)
	if err != nil {
		return errors.New("dh: " + err.Error())
	}

	dhg, ok := dhGenStatus.(*objects.DHGenOk)
	if !ok {
		return fmt.Errorf("invalid response")
	}
	if nonceFirst.Cmp(dhg.Nonce.Int) != 0 {
		return fmt.Errorf("handshake: Wrong nonce: %v, %v", nonceFirst, dhg.Nonce)
	}
	if nonceServer.Cmp(dhg.ServerNonce.Int) != 0 {
		return fmt.Errorf("handshake: Wrong server_nonce: %v, %v", nonceServer, dhg.ServerNonce)
	}
	if !bytes.Equal(nonceHash1, dhg.NewNonceHash1.Bytes()) {
		return fmt.Errorf(
			"handshake: Wrong new_nonce_hash1: %v, %v",
			hex.EncodeToString(nonceHash1),
			hex.EncodeToString(dhg.NewNonceHash1.Bytes()),
		)
	}

	// Store temporary auth key and its metadata in memory only.
	m.tempAuthKey = tempKey
	m.tempAuthKeyHash = utils.AuthKeyHash(tempKey)
	m.tempAuthExpiresAt = time.Now().Unix() + int64(expiresIn)
	m.serverSalt = newSalt
	return nil
}

// createTempAuthKey performs the temporary auth key handshake on a separate
// ephemeral MTProto connection. This avoids interfering with the main
// connection's RPC pipeline and ensures that the low-level MTProto handshake
// is always performed in unencrypted mode as required by the protocol.
func (m *MTProto) createTempAuthKey(expiresIn int32) error {
	cfg := Config{
		AuthKeyFile:    "",
		AuthAESKey:     "",
		SessionStorage: session.NewInMemory(),
		MemorySession:  true,
		AppID:          m.appID,
		EnablePFS:      false,
		ServerHost:     m.Addr,
		PublicKey:      m.publicKey,
		DataCenter:     m.GetDC(),
		Logger:         m.Logger.Clone().WithPrefix("gogram [mtproto-pfs]"),
		Proxy:          m.proxy,
		Mode:           "", // default transport mode (abridged)
		Ipv6:           m.IpV6,
		CustomHost:     true,
		LocalAddr:      m.localAddr,
		Timeout:        int(m.timeout.Seconds()),
		ReqTimeout:     int(m.reqTimeout.Seconds()),
		UseWebSocket:   m.useWebSocket,
		UseWebSocketTLS: m.useWebSocketTLS,
	}

	tmp, err := NewMTProto(cfg)
	if err != nil {
		return fmt.Errorf("createTempAuthKey: creating temp MTProto: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tmp.connect(ctx); err != nil {
		return fmt.Errorf("createTempAuthKey: connecting temp MTProto: %w", err)
	}
	tmp.tcpState.SetActive(true)
	tmp.startReadingResponses(ctx)

	if err := tmp.makeTempAuthKey(expiresIn); err != nil {
		return fmt.Errorf("createTempAuthKey: makeTempAuthKey on temp connection: %w", err)
	}

	// Copy the generated temporary auth key and its metadata back to the main
	// MTProto instance. The server salt associated with the temporary key is
	// also propagated so that subsequent messages encrypted with the temp key
	// use the correct salt.
	m.tempAuthKey = tmp.tempAuthKey
	m.tempAuthKeyHash = tmp.tempAuthKeyHash
	m.tempAuthExpiresAt = tmp.tempAuthExpiresAt
	m.serverSalt = tmp.serverSalt

	if tmp.transport != nil {
		_ = tmp.transport.Close()
	}
	tmp.tcpState.SetActive(false)

	return nil
}

// bindTempAuthKey binds the temporary auth key to the permanent auth key using
// auth.bindTempAuthKey. The outer request is encrypted with the temporary auth
// key, while the inner binding message is encrypted with the permanent key
// using MTProto 1.0 as required by the specification.
func (m *MTProto) bindTempAuthKey() error {
	if m.tempAuthKey == nil {
		return errors.New("bindTempAuthKey: tempAuthKey is nil")
	}
	if len(m.authKey) == 0 {
		return errors.New("bindTempAuthKey: permanent authKey is nil")
	}

	permKeyHash := utils.AuthKeyHash(m.authKey)
	permAuthKeyID := int64(binary.LittleEndian.Uint64(permKeyHash))

	tempKeyHash := utils.AuthKeyHash(m.tempAuthKey)
	tempAuthKeyID := int64(binary.LittleEndian.Uint64(tempKeyHash))

	// Use current session ID as temp_session_id for the binding.
	tempSessionID := m.sessionId

	expiresAt := int32(m.tempAuthExpiresAt)
	if expiresAt == 0 {
		expiresAt = int32(time.Now().Unix() + 24*60*60)
	}

	nonce := utils.GenerateSessionID()
	msgID := m.genMsgID(m.timeOffset)

	inner := &objects.BindAuthKeyInner{
		Nonce:         nonce,
		TempAuthKeyID: tempAuthKeyID,
		PermAuthKeyID: permAuthKeyID,
		TempSessionID: tempSessionID,
		ExpiresAt:     expiresAt,
	}

	innerBytes, err := tl.Marshal(inner)
	if err != nil {
		return fmt.Errorf("marshal BindAuthKeyInner: %w", err)
	}

	// Build MTProto v1 plaintext: random:int128 + msg_id + seqno(0) + msg_len + inner.
	random128 := utils.RandomBytes(16)
	plaintext := make([]byte, 0, 16+8+4+4+len(innerBytes))
	plaintext = append(plaintext, random128...)

	buf8 := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf8, uint64(msgID))
	plaintext = append(plaintext, buf8...)

	buf4 := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf4, 0) // seqno = 0
	plaintext = append(plaintext, buf4...)

	binary.LittleEndian.PutUint32(buf4, uint32(len(innerBytes)))
	plaintext = append(plaintext, buf4...)
	plaintext = append(plaintext, innerBytes...)

	// Encrypt binding message with permanent auth key using MTProto 1.0 helpers.
	cipher, msgKey, err := ige.EncryptV1(plaintext, m.authKey)
	if err != nil {
		return fmt.Errorf("encrypt BindAuthKeyInner: %w", err)
	}

	encryptedMessage := make([]byte, 0, len(permKeyHash)+len(msgKey)+len(cipher))
	encryptedMessage = append(encryptedMessage, permKeyHash...)
	encryptedMessage = append(encryptedMessage, msgKey...)
	encryptedMessage = append(encryptedMessage, cipher...)

	params := &objects.AuthBindTempAuthKeyParams{
		PermAuthKeyID:    permAuthKeyID,
		Nonce:            nonce,
		ExpiresAt:        expiresAt,
		EncryptedMessage: encryptedMessage,
	}

	// Send auth.bindTempAuthKey with the same msg_id used inside the MTProto
	// v1 binding message, as required by the PFS specification.
	respCh, _, err := m.sendPacketWithMsgID(params, msgID)
	if err != nil {
		return fmt.Errorf("auth.bindTempAuthKey: %w", err)
	}

	response := <-respCh
	if rpcErr, ok := response.(*objects.RpcError); ok {
		return fmt.Errorf("auth.bindTempAuthKey: %w", RpcErrorToNative(rpcErr))
	}

	// Any non-error response is treated as success; the exact Bool value is not
	// critical here, as the server typically returns true on success.
	return nil
}
