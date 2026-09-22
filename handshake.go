// Copyright (c) 2025 @AmarnathCJD

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

const maxAuthKeyDecryptRetries = 5

// https://core.telegram.org/mtproto/auth_key
func (m *MTProto) makeAuthKey(ctx context.Context, expiresIn int32) error {
	for attempt := 0; ; attempt++ {
		err := m.makeAuthKeyOnce(expiresIn, ctx)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errAuthKeyDecryptRetry) {
			return err
		}
		if attempt+1 >= maxAuthKeyDecryptRetries {
			return fmt.Errorf("auth key generation: decrypt failed after %d attempts: %w", maxAuthKeyDecryptRetries, err)
		}
		m.Logger.WithError(err).Debugf("decrypt failed - retrying auth key generation (attempt %d/%d)", attempt+2, maxAuthKeyDecryptRetries)
	}
}

var errAuthKeyDecryptRetry = errors.New("auth key decrypt failed")

func (m *MTProto) makeAuthKeyOnce(expiresIn int32, ctx context.Context) error {
	isTemp := expiresIn > 0

	m.serviceModeActivated.Store(true)
	defer m.serviceModeActivated.Store(false)

	// telegram sometimes gvs wrong nonce, idk
	const maxNonceRetries = 5
	nonceRetries := 0
	var nonceFirst *tl.Int128
	var res *objects.ResPQ
	var err error
	for {
		nonceFirst = tl.RandomInt128()
		res, err = objects.ReqPQMulti(ctx, m, nonceFirst)
		if err != nil {
			return fmt.Errorf("reqPQ: %w", err)
		}
		if nonceFirst.Cmp(res.Nonce.Int) == 0 {
			break
		}
		nonceRetries++
		if nonceRetries >= maxNonceRetries {
			return fmt.Errorf("reqPQ: nonce mismatch after %d retries (%v, %v)", maxNonceRetries, nonceFirst, res.Nonce)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	if m.cdn {
		key, ok := m.HasCdnKey(int32(m.GetDC()))
		if !ok {
			return errors.New("reqPQ: no RSA key for CDN DC")
		}
		m.publicKey = key
	}
	if m.publicKey == nil || m.publicKey.N == nil || m.publicKey.N.BitLen() != 2048 || m.publicKey.E < 3 {
		return errors.New("reqPQ: invalid RSA public key")
	}
	found := false
	for _, fingerprint := range res.Fingerprints {
		if uint64(fingerprint) == binary.LittleEndian.Uint64(keys.RSAFingerprint(m.publicKey)) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("reqPQ: no matching fingerprint")
	}

	pq := big.NewInt(0).SetBytes(res.Pq)
	if pq.Sign() <= 0 || pq.BitLen() > 64 || pq.ProbablyPrime(16) {
		return errors.New("reqPQ: expected a composite integer of at most 64 bits")
	}
	p, q := math.Factorize(pq)
	if p == nil || q == nil || p.Cmp(big.NewInt(1)) <= 0 || q.Cmp(big.NewInt(1)) <= 0 || !p.ProbablyPrime(16) || !q.ProbablyPrime(16) || new(big.Int).Mul(p, q).Cmp(pq) != 0 {
		return errors.New("reqPQ: invalid prime factorization")
	}
	nonceSecond := tl.RandomInt256()
	nonceServer := res.ServerNonce

	var message []byte
	switch {
	case isTemp:
		message, err = tl.Marshal(&objects.PQInnerDataTempDc{
			Pq:          res.Pq,
			P:           p.Bytes(),
			Q:           q.Bytes(),
			Nonce:       nonceFirst,
			ServerNonce: nonceServer,
			NewNonce:    nonceSecond,
			Dc:          m.handshakeDC(),
			ExpiresIn:   expiresIn,
		})
	default:
		message, err = tl.Marshal(&objects.PQInnerDataDc{
			Pq:          res.Pq,
			P:           p.Bytes(),
			Q:           q.Bytes(),
			Nonce:       nonceFirst,
			ServerNonce: nonceServer,
			NewNonce:    nonceSecond,
			Dc:          m.handshakeDC(),
		})
	}
	if err != nil {
		m.Logger.WithField("error", err).Debug("makeAuthKey: failed to marshal pq inner data")
		return err
	}

	encryptedMessage, err := math.DoRSAPad(message, m.publicKey)
	if err != nil {
		return fmt.Errorf("rsa encrypt: %w", err)
	}

	keyFingerprint := int64(binary.LittleEndian.Uint64(keys.RSAFingerprint(m.publicKey)))
	dhResponse, err := objects.ReqDHParams(ctx, m, nonceFirst, nonceServer, p.Bytes(), q.Bytes(), keyFingerprint, encryptedMessage)
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
		return fmt.Errorf("%w: %w", errAuthKeyDecryptRetry, err)
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

	dhPrime := big.NewInt(0).SetBytes(dhi.DhPrime)
	gA := big.NewInt(0).SetBytes(dhi.GA)
	if err := math.ValidateDHParams(dhi.G, gA, dhPrime); err != nil {
		return fmt.Errorf("dh params: %w", err)
	}

	const maxDHGenAttempts = 5
	var authKey []byte
	var newSalt int64
	var nonceHash1 []byte
	var retryID int64

	for attempt := 0; attempt < maxDHGenAttempts; attempt++ {
		_, gB, gAB := math.MakeGAB(dhi.G, gA, dhPrime)
		if err := math.ValidateGB(gB, dhPrime); err != nil {
			return fmt.Errorf("dh params: %w", err)
		}

		authKey = gAB.FillBytes(make([]byte, 256))

		t4 := make([]byte, 32+1+8)
		copy(t4[0:], nonceSecond.Bytes())
		t4[32] = 1
		copy(t4[33:], utils.Sha1Byte(authKey)[0:8])
		nonceHash1 = utils.Sha1Byte(t4)[4:20]
		salt := make([]byte, tl.LongLen)
		copy(salt, nonceSecond.Bytes()[:8])
		math.XOR(salt, nonceServer.Bytes()[:8])
		newSalt = int64(binary.LittleEndian.Uint64(salt))

		clientDHData, err := tl.Marshal(&objects.ClientDHInnerData{
			Nonce:       nonceFirst,
			ServerNonce: nonceServer,
			Retry:       retryID,
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

		dhGenStatus, err := objects.SetClientDHParams(ctx, m, nonceFirst, nonceServer, encryptedMessage)
		if err != nil {
			return errors.New("dh: " + err.Error())
		}

		switch dhg := dhGenStatus.(type) {
		case *objects.DHGenOk:
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
			goto dhGenSuccess
		case *objects.DHGenRetry:
			if nonceFirst.Cmp(dhg.Nonce.Int) != 0 || nonceServer.Cmp(dhg.ServerNonce.Int) != 0 {
				return fmt.Errorf("dh_gen_retry: nonce mismatch")
			}
			t4[32] = 2
			if !bytes.Equal(utils.Sha1Byte(t4)[4:20], dhg.NewNonceHash2.Bytes()) {
				return errors.New("dh_gen_retry: new_nonce_hash2 mismatch")
			}
			authKeyAuxHash := utils.Sha1Byte(authKey)[:8]
			retryID = int64(binary.LittleEndian.Uint64(authKeyAuxHash))
			m.Logger.Debug("dh_gen_retry: regenerating g_b (attempt %d/%d)", attempt+2, maxDHGenAttempts)
			continue
		case *objects.DHGenFail:
			if nonceFirst.Cmp(dhg.Nonce.Int) != 0 || nonceServer.Cmp(dhg.ServerNonce.Int) != 0 {
				return errors.New("dh_gen_fail: nonce mismatch")
			}
			t4[32] = 3
			if !bytes.Equal(utils.Sha1Byte(t4)[4:20], dhg.NewNonceHash3.Bytes()) {
				return errors.New("dh_gen_fail: new_nonce_hash3 mismatch")
			}
			return fmt.Errorf("dh_gen_fail: server rejected auth key generation")
		default:
			return fmt.Errorf("dh_gen: unexpected response %T", dhGenStatus)
		}
	}
	return fmt.Errorf("dh_gen: exhausted %d retry attempts", maxDHGenAttempts)

dhGenSuccess:

	if isTemp {
		m.authMu.Lock()
		m.tempAuthKey = authKey
		m.tempAuthKeyHash = utils.AuthKeyHash(authKey)
		m.tempAuthExpiresAt = time.Now().Unix() + int64(expiresIn)
		m.tempServerSalt = newSalt
		m.authMu.Unlock()
		m.serverSalt.Store(newSalt)
	} else {
		m.SetAuthKey(authKey)
		m.serverSalt.Store(newSalt)
		m.encrypted.Store(true)
		if err := m.SaveSession(m.memorySession); err != nil {
			m.Logger.WithError(err).Error("failed to save session")
		}
	}

	return nil
}

// createTempAuthKey performs the temporary auth key handshake
func (m *MTProto) createTempAuthKey(parent context.Context, expiresIn int32) error {
	cfg := Config{
		AuthKeyFile:    "__pfs__temp",
		AuthAESKey:     "",
		SessionStorage: session.NewInMemory(),
		MemorySession:  true,
		AppID:          m.appID,
		EnablePFS:      false,
		ServerHost:     m.GetAddr(),
		PublicKey:      m.publicKey,
		DataCenter:     m.GetDC(),
		TestMode:       m.testMode,
		MediaDC:        m.mediaDC,
		Logger:         m.Logger.Clone().WithPrefix("gogram [mtp-pfs]"),
		Proxy:          m.proxy,
		Mode:           "Abridged",
		Ipv6:           m.IpV6,
		CustomHost:     true,
		LocalAddr:      m.localAddr,
		Timeout:        int(m.connConfig.Timeout.Seconds()),
		ReqTimeout:     int(m.reqTimeout.Seconds()),
		Transport:      m.txType,
		HTTPPath:       m.httpPath,
	}

	tmp, err := NewMTProto(cfg)
	if err != nil {
		return fmt.Errorf("createTempAuthKey: creating temp MTProto: %w", err)
	}
	defer tmp.Terminate() // Ensure cleanup in all cases

	tmp.mode = m.mode
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	if err := tmp.connect(ctx); err != nil {
		return fmt.Errorf("createTempAuthKey: connecting temp MTProto: %w", err)
	}
	tmp.tcpState.SetActive(true)
	tmp.startReadingResponses(ctx)

	if err := tmp.makeAuthKey(ctx, expiresIn); err != nil {
		return fmt.Errorf("createTempAuthKey: makeTempAuthKey on temp connection: %w", err)
	}

	m.authMu.Lock()
	tmp.authMu.RLock()
	m.pendingTempAuthKey = bytes.Clone(tmp.tempAuthKey)
	m.pendingTempKeyHash = bytes.Clone(tmp.tempAuthKeyHash)
	m.pendingTempExpiresAt = tmp.tempAuthExpiresAt
	m.pendingTempSalt = tmp.tempServerSalt
	tmp.authMu.RUnlock()
	m.authMu.Unlock()

	m.Logger.Debug("temporary auth key created successfully")
	return nil
}

// bindTempAuthKey binds the temporary auth key to the permanent auth key using auth.bindTempAuthKey.
func (m *MTProto) bindTempAuthKey(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, m.reqTimeout)
	defer cancel()
	m.authMu.RLock()
	newKey := bytes.Clone(m.pendingTempAuthKey)
	permKey := bytes.Clone(m.authKey)
	expiresAt := m.pendingTempExpiresAt
	m.authMu.RUnlock()
	if len(newKey) != 256 || len(permKey) != 256 {
		return errors.New("bindTempAuthKey: missing authorization key")
	}
	if expiresAt <= time.Now().Unix() {
		return errors.New("bindTempAuthKey: temporary key expired")
	}
	permHash := utils.AuthKeyHash(permKey)
	tempHash := utils.AuthKeyHash(newKey)
	permID := int64(binary.LittleEndian.Uint64(permHash))
	nonce := utils.GenerateSessionID()

	// The inner and outer request must use the same message ID. Reserve and
	// write it under the ordinary send lock, without holding that lock while
	// waiting for the response.
	send := func() (chan tl.Object, int64, error) {
		if err := m.writeMu.Lock(ctx); err != nil {
			return nil, 0, err
		}
		defer m.writeMu.Unlock()
		msgID := m.genMsgID(m.timeOffset.Load())
		inner := &objects.BindAuthKeyInner{Nonce: nonce, TempAuthKeyID: int64(binary.LittleEndian.Uint64(tempHash)), PermAuthKeyID: permID, TempSessionID: m.GetSessionID(), ExpiresAt: int32(expiresAt)}
		body, err := tl.Marshal(inner)
		if err != nil {
			return nil, 0, err
		}
		plaintext := utils.RandomBytes(16)
		plaintext = binary.LittleEndian.AppendUint64(plaintext, uint64(msgID))
		plaintext = binary.LittleEndian.AppendUint32(plaintext, 0)
		plaintext = binary.LittleEndian.AppendUint32(plaintext, uint32(len(body)))
		plaintext = append(plaintext, body...)
		encrypted, msgKey, err := ige.EncryptV1(plaintext, permKey)
		if err != nil {
			return nil, 0, err
		}
		payload := append(append(bytes.Clone(permHash), msgKey...), encrypted...)
		params := &objects.AuthBindTempAuthKeyParams{PermAuthKeyID: permID, Nonce: nonce, ExpiresAt: int32(expiresAt), EncryptedMessage: payload}
		return m.sendPacket(ctx, params, msgID)
	}
	ch, id, err := send()
	if err != nil {
		return fmt.Errorf("auth.bindTempAuthKey: %w", err)
	}
	defer m.responseChannels.Delete(id)
	defer m.expectedTypes.Delete(id)
	var response tl.Object
	select {
	case response = <-ch:
	case <-ctx.Done():
		return ctx.Err()
	}
	if rpcErr, ok := response.(*objects.RpcError); ok {
		return fmt.Errorf("auth.bindTempAuthKey: %w", RpcErrorToNative(rpcErr))
	}
	native := tl.UnwrapNativeTypes(response)
	success, ok := native.(bool)
	if !ok || !success {
		return fmt.Errorf("auth.bindTempAuthKey: unexpected response %T", native)
	}
	m.authMu.Lock()
	defer m.authMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !bytes.Equal(permKey, m.authKey) || !bytes.Equal(newKey, m.pendingTempAuthKey) {
		return errors.New("authorization changed during temporary key binding")
	}
	if m.pendingTempExpiresAt <= time.Now().Unix() {
		return errors.New("bindTempAuthKey: temporary key expired while binding")
	}
	m.previousTempAuthKey, m.previousTempKeyHash = m.tempAuthKey, m.tempAuthKeyHash
	m.previousTempSalt = m.tempServerSalt
	m.tempAuthKey, m.tempAuthKeyHash = m.pendingTempAuthKey, m.pendingTempKeyHash
	m.tempServerSalt = m.pendingTempSalt
	m.tempAuthExpiresAt = m.pendingTempExpiresAt
	m.pendingTempAuthKey = nil
	m.pendingTempKeyHash = nil
	m.pendingTempExpiresAt = 0
	m.pendingTempSalt = 0
	return nil
}
