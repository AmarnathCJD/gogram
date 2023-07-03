// Copyright (c) 2023 RoseLoverX

package gogram

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	ige "github.com/amarnathcjd/gogram/internal/aes_ige"
	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/keys"
	"github.com/amarnathcjd/gogram/internal/math"
	"github.com/amarnathcjd/gogram/internal/mtproto/objects"
	"github.com/pkg/errors"
)

// https://core.telegram.org/mtproto/auth_key
func (m *MTProto) makeAuthKey() error {
	m.serviceModeActivated = true
	nonceFirst := tl.RandomInt128()
	res, err := m.reqPQ(nonceFirst)
	if err != nil {
		return fmt.Errorf("reqPQ: %w", err)
	}

	if nonceFirst.Cmp(res.Nonce.Int) != 0 {
		return fmt.Errorf("reqPQ: nonce mismatch")
	}
	found := false
	for _, b := range res.Fingerprints {
		if uint64(b) == binary.LittleEndian.Uint64(keys.RSAFingerprint(m.PublicKey)) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("reqPQ: no matching fingerprint")
	}

	// (encoding) p_q_inner_data
	pq := big.NewInt(0).SetBytes(res.Pq)
	p, q := math.SplitPQ(pq)
	nonceSecond := tl.RandomInt256()
	nonceServer := res.ServerNonce

	message, err := tl.Marshal(&objects.PQInnerData{
		Pq:          res.Pq,
		P:           p.Bytes(),
		Q:           q.Bytes(),
		Nonce:       nonceFirst,
		ServerNonce: nonceServer,
		NewNonce:    nonceSecond,
	})
	if err != nil {
		m.Logger.Warn("makeAuthKey: failed to marshal pq inner data")
		return err
	}

	hashAndMsg := make([]byte, 255)
	copy(hashAndMsg, append(Sha1(string(message)), message...))

	encryptedMessage := math.DoRSAencrypt(hashAndMsg, m.PublicKey)

	keyFingerprint := int64(binary.LittleEndian.Uint64(keys.RSAFingerprint(m.PublicKey)))
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

	// check of hash, random bytes trail removing occurs in this func already
	decodedMessage, err := ige.DecryptMessageWithTempKeys(dhParams.EncryptedAnswer, nonceSecond.Int, nonceServer.Int)
	if err != nil {
		m.Logger.Warn(err.Error() + " - <retrying>")
		return m.makeAuthKey()
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

	// this apparently is just part of diffie hellman, so just leave it as it is, hope that it will just work
	_, gB, gAB := math.MakeGAB(dhi.G, big.NewInt(0).SetBytes(dhi.GA), big.NewInt(0).SetBytes(dhi.DhPrime))

	authKey := gAB.Bytes()
	if authKey[0] == 0 {
		authKey = authKey[1:]
	}

	m.SetAuthKey(authKey)

	t4 := make([]byte, 32+1+8)
	copy(t4[0:], nonceSecond.Bytes())
	t4[32] = 1
	copy(t4[33:], Sha1Byte(m.GetAuthKey())[0:8])
	nonceHash1 := Sha1Byte(t4)[4:20]
	salt := make([]byte, tl.LongLen)
	copy(salt, nonceSecond.Bytes()[:8])
	math.Xor(salt, nonceServer.Bytes()[:8])
	m.serverSalt = int64(binary.LittleEndian.Uint64(salt))

	// (encoding) client_DH_inner_data
	clientDHData, err := tl.Marshal(&objects.ClientDHInnerData{
		Nonce:       nonceFirst,
		ServerNonce: nonceServer,
		Retry:       0,
		GB:          gB.Bytes(),
	})
	if err != nil {
		log.Printf("TgCrypto - dh: %s", err)
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

	m.serviceModeActivated = false
	m.encrypted = true
	if !m.memorySession {
		err = m.SaveSession()
		if err != nil {
			m.Logger.Error("Saving session: ", err)
		}
	}
	return err
}

func Sha1(input string) []byte {
	r := sha1.Sum([]byte(input))
	return r[:]
}

func Sha1Byte(input []byte) []byte {
	r := sha1.Sum(input)
	return r[:]
}
