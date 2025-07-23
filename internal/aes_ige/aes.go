// Copyright (c) 2024 RoseLoverX

package ige

import (
	"bytes"
	"crypto/aes"
	"crypto/rand"
	"crypto/sha256"
	"math/big"

	"github.com/bs9/spread_service_gogram/internal/utils"
	"github.com/pkg/errors"
)

type AesBlock [aes.BlockSize]byte
type AesKV [32]byte
type AesIgeBlock [48]byte

func MessageKey(authKey, msgPadded []byte, decode bool) []byte {
	var x int
	if decode {
		x = 8
	} else {
		x = 0
	}

	// `msg_key_large = SHA256 (substr (auth_key, 88+x, 32) + plaintext + random_padding);`
	var msgKeyLarge [sha256.Size]byte
	{
		h := sha256.New()

		substr := authKey[88+x:]
		_, _ = h.Write(substr[:32])
		_, _ = h.Write(msgPadded)

		h.Sum(msgKeyLarge[:0])
	}
	r := make([]byte, 16)
	// `msg_key = substr (msg_key_large, 8, 16);`
	copy(r, msgKeyLarge[8:8+16])
	return r
}

func Encrypt(msg, authKey []byte) (out, msgKey []byte, _ error) {
	return encrypt(msg, authKey, false)
}

func encrypt(msg, authKey []byte, decode bool) (out, msgKey []byte, _ error) {
	padding := 16 + (16-(len(msg)%16))&15
	data := make([]byte, len(msg)+padding)
	n := copy(data, msg)

	// Fill padding using secure PRNG.
	//
	// See https://core.telegram.org/mtproto/description#encrypted-message-encrypted-data.
	if _, err := rand.Read(data[n:]); err != nil {
		return nil, nil, err
	}

	msgKey = MessageKey(authKey, data, decode)
	aesKey, aesIV := aesKeys(msgKey[:], authKey, decode)

	c, err := NewCipher(aesKey[:], aesIV[:])
	if err != nil {
		return nil, nil, err
	}

	out = make([]byte, len(data))
	if err := c.doAES256IGEencrypt(data, out); err != nil {
		return nil, nil, err
	}

	return out, msgKey, nil
}

func Decrypt(msg, authKey, checkData []byte) ([]byte, error) {
	return decrypt(msg, authKey, checkData, true)
}

func decrypt(msg, authKey, msgKey []byte, decode bool) ([]byte, error) {
	aesKey, aesIV := aesKeys(msgKey, authKey, decode)

	c, err := NewCipher(aesKey[:], aesIV[:])
	if err != nil {
		return nil, err
	}

	out := make([]byte, len(msg))
	if err := c.doAES256IGEdecrypt(msg, out); err != nil {
		return nil, err
	}

	return out, nil
}

func doAES256IGEencrypt(data, out, key, iv []byte) error {
	c, err := NewCipher(key, iv)
	if err != nil {
		return err
	}
	return c.doAES256IGEencrypt(data, out)
}

func doAES256IGEdecrypt(data, out, key, iv []byte) error {
	c, err := NewCipher(key, iv)
	if err != nil {
		return err
	}
	return c.doAES256IGEdecrypt(data, out)
}

// DecryptMessageWithTempKeys decrypts a message using temporary keys obtained during the Diffie-Hellman key exchange.
func DecryptMessageWithTempKeys(msg []byte, nonceSecond, nonceServer *big.Int) ([]byte, error) {
	key, iv, errTemp := generateTempKeys(nonceSecond, nonceServer)
	if errTemp != nil {
		return nil, errTemp
	}
	decodedWithHash := make([]byte, len(msg))
	err := doAES256IGEdecrypt(msg, decodedWithHash, key, iv)
	if err != nil {
		return nil, err
	}

	// decodedWithHash := SHA1(answer) + answer + (0-15); 16;
	decodedHash := decodedWithHash[:20]
	decodedMessage := decodedWithHash[20:]

	for i := len(decodedMessage) - 1; i > len(decodedMessage)-16; i-- {
		if bytes.Equal(decodedHash, utils.Sha1Byte(decodedMessage[:i])) {
			return decodedMessage[:i], nil
		}
	}

	return nil, errors.New("couldn't trim message: hashes incompatible on more than 16 tries")
}

// EncryptMessageWithTempKeys encrypts a message using temporary keys obtained during the Diffie-Hellman key exchange.
func EncryptMessageWithTempKeys(msg []byte, nonceSecond, nonceServer *big.Int) ([]byte, error) {
	hash := utils.Sha1Byte(msg)

	totalLen := len(hash) + len(msg)
	overflowedLen := totalLen % 16
	needToAdd := 16 - overflowedLen

	msg = bytes.Join([][]byte{hash, msg, utils.RandomBytes(needToAdd)}, []byte{})
	return encryptMessageWithTempKeys(msg, nonceSecond, nonceServer)
}

func encryptMessageWithTempKeys(msg []byte, nonceSecond, nonceServer *big.Int) ([]byte, error) {
	key, iv, errTemp := generateTempKeys(nonceSecond, nonceServer)
	if errTemp != nil {
		return nil, errTemp
	}

	encodedWithHash := make([]byte, len(msg))
	err := doAES256IGEencrypt(msg, encodedWithHash, key, iv)
	if err != nil {
		return nil, err
	}

	return encodedWithHash, nil
}

// https://core.telegram.org/mtproto/auth_key#server-responds-in-two-ways
//
// generateTempKeys generates temporary keys for encryption during the key exchange process.
func generateTempKeys(nonceSecond, nonceServer *big.Int) (key, iv []byte, err error) {
	if nonceSecond == nil {
		return nil, nil, errors.New("nonceSecond is nil")
	}
	if nonceServer == nil {
		return nil, nil, errors.New("nonceServer is nil")
	}

	// nonceSecond + nonceServer
	t1 := make([]byte, 48)
	copy(t1[0:], nonceSecond.Bytes())
	copy(t1[32:], nonceServer.Bytes())
	// SHA1 of nonceSecond + nonceServer
	hash1 := utils.Sha1Byte(t1)

	// nonceServer + nonceSecond
	t2 := make([]byte, 48)
	copy(t2[0:], nonceServer.Bytes())
	copy(t2[16:], nonceSecond.Bytes())
	// SHA1 of nonceServer + nonceSecond
	hash2 := utils.Sha1Byte(t2)

	// SHA1(nonceSecond + nonceServer) + substr (SHA1(nonceServer + nonceSecond), 0, 12);
	tmpAESKey := make([]byte, 32)
	// SHA1 of nonceSecond + nonceServer
	copy(tmpAESKey[0:], hash1)
	// substr (0 to 12)  of SHA1 of nonceServer + nonceSecond
	copy(tmpAESKey[20:], hash2[0:12])

	t3 := make([]byte, 64) // nonceSecond + nonceSecond
	copy(t3[0:], nonceSecond.Bytes())
	copy(t3[32:], nonceSecond.Bytes())
	hash3 := utils.Sha1Byte(t3) // SHA1 of nonceSecond + nonceSecond

	// substr (SHA1(server_nonce + new_nonce), 12, 8) + SHA1(new_nonce + new_nonce) + substr (new_nonce, 0, 4);
	tmpAESIV := make([]byte, 32)
	// substr (12 to 8 ) of SHA1 of nonceServer + nonceSecond
	copy(tmpAESIV[0:], hash2[12:12+8])
	// SHA1 of nonceSecond + nonceSecond
	copy(tmpAESIV[8:], hash3)
	// substr (nonceSecond, 0, 4)
	copy(tmpAESIV[28:], nonceSecond.Bytes()[0:4])

	return tmpAESKey, tmpAESIV, nil
}
