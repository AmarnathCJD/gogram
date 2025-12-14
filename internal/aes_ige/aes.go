// Copyright (c) 2025 @AmarnathCJD

package ige

import (
	"bytes"
	"crypto/aes"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"math/big"

	"errors"

	"github.com/amarnathcjd/gogram/internal/utils"
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
	if err := c.DoAES256IGEencrypt(data, out); err != nil {
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
	if err := c.DoAES256IGEdecrypt(msg, out); err != nil {
		return nil, err
	}

	return out, nil
}

func doAES256IGEencrypt(data, out, key, iv []byte) error {
	c, err := NewCipher(key, iv)
	if err != nil {
		return err
	}
	return c.DoAES256IGEencrypt(data, out)
}

func doAES256IGEdecrypt(data, out, key, iv []byte) error {
	c, err := NewCipher(key, iv)
	if err != nil {
		return err
	}
	return c.DoAES256IGEdecrypt(data, out)
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

// aesKeysV1 derives AES key and IV according to MTProto 1.0 specification.
// See https://core.telegram.org/mtproto/description_v1#defining-aes-key-and-initialization-vector
func aesKeysV1(msgKey, authKey []byte, decode bool) (aesKey, aesIv [32]byte) {
	var x int
	if decode {
		x = 8
	} else {
		x = 0
	}

	// sha1_a = SHA1 (msg_key + substr (auth_key, x, 32));
	buf := make([]byte, 16+32)
	copy(buf, msgKey)
	copy(buf[16:], authKey[x:x+32])
	sha1a := sha1.Sum(buf)

	// sha1_b = SHA1 (substr (auth_key, 32+x, 16) + msg_key + substr (auth_key, 48+x, 16));
	buf = make([]byte, 16+16+16)
	copy(buf, authKey[32+x:32+x+16])
	copy(buf[16:], msgKey)
	copy(buf[32:], authKey[48+x:48+x+16])
	sha1b := sha1.Sum(buf)

	// sha1_c = SHA1 (substr (auth_key, 64+x, 32) + msg_key);
	buf = make([]byte, 32+16)
	copy(buf, authKey[64+x:64+x+32])
	copy(buf[32:], msgKey)
	sha1c := sha1.Sum(buf)

	// sha1_d = SHA1 (msg_key + substr (auth_key, 96+x, 32));
	buf = make([]byte, 16+32)
	copy(buf, msgKey)
	copy(buf[16:], authKey[96+x:96+x+32])
	sha1d := sha1.Sum(buf)

	// aes_key = substr (sha1_a, 0, 8) + substr (sha1_b, 8, 12) + substr (sha1_c, 4, 12);
	copy(aesKey[0:], sha1a[0:8])
	copy(aesKey[8:], sha1b[8:8+12])
	copy(aesKey[20:], sha1c[4:4+12])

	// aes_iv = substr (sha1_a, 8, 12) + substr (sha1_b, 0, 8) + substr (sha1_c, 16, 4) + substr (sha1_d, 0, 8);
	copy(aesIv[0:], sha1a[8:8+12])
	copy(aesIv[12:], sha1b[0:0+8])
	copy(aesIv[20:], sha1c[16:16+4])
	copy(aesIv[24:], sha1d[0:0+8])

	return aesKey, aesIv
}

// It returns the ciphertext and msg_key (128-bit).
// computeSecretChatAesKeyIV computes AES key and IV for secret chat encryption (MTProto 2.0)
func computeSecretChatAesKeyIV(msgKey, key []byte, isOriginator bool) (aesKey, aesIV []byte) {
	x := 0
	if !isOriginator {
		x = 8
	}

	// sha256_a = SHA256(msg_key + substr(key, x, 36))
	sha256A := sha256.New()
	sha256A.Write(msgKey)
	sha256A.Write(key[x : x+36])
	hashA := sha256A.Sum(nil)

	// sha256_b = SHA256(substr(key, 40+x, 36) + msg_key)
	sha256B := sha256.New()
	sha256B.Write(key[40+x : 40+x+36])
	sha256B.Write(msgKey)
	hashB := sha256B.Sum(nil)

	// aes_key = substr(sha256_a, 0, 8) + substr(sha256_b, 8, 16) + substr(sha256_a, 24, 8)
	aesKey = make([]byte, 32)
	copy(aesKey[0:8], hashA[0:8])
	copy(aesKey[8:24], hashB[8:24])
	copy(aesKey[24:32], hashA[24:32])

	// aes_iv = substr(sha256_b, 0, 8) + substr(sha256_a, 8, 16) + substr(sha256_b, 24, 8)
	aesIV = make([]byte, 32)
	copy(aesIV[0:8], hashB[0:8])
	copy(aesIV[8:24], hashA[8:24])
	copy(aesIV[24:32], hashB[24:32])

	return aesKey, aesIV
}

func encryptV1(plaintext, authKey []byte, decode bool) (out, msgKey []byte, _ error) {
	// msg_key = substr (SHA1 (plaintext), 4, 16);
	sha := sha1.Sum(plaintext)
	msgKey = make([]byte, 16)
	copy(msgKey, sha[4:4+16])

	aesKey, aesIV := aesKeysV1(msgKey, authKey, decode)

	// pad plaintext with random bytes to a multiple of 16 bytes
	padding := 16 - (len(plaintext) % 16)
	if padding == 16 {
		padding = 0
	}
	data := make([]byte, len(plaintext)+padding)
	copy(data, plaintext)
	if padding > 0 {
		if _, err := rand.Read(data[len(plaintext):]); err != nil {
			return nil, nil, err
		}
	}

	c, err := NewCipher(aesKey[:], aesIV[:])
	if err != nil {
		return nil, nil, err
	}

	out = make([]byte, len(data))
	if err := c.DoAES256IGEencrypt(data, out); err != nil {
		return nil, nil, err
	}

	return out, msgKey, nil
}

// EncryptV1 encryptV1.
func EncryptV1(plaintext, authKey []byte) (out, msgKey []byte, _ error) {
	return encryptV1(plaintext, authKey, false)
}

// EncryptMessageMTProto encrypts a message using MTProto 2.0 encryption for secret chats
// x = 0 for originator, x = 8 for responder
func EncryptMessageMTProto(key []byte, plaintext []byte, isOriginator bool) (msgKey []byte, encrypted []byte, err error) {
	if len(key) != 256 {
		return nil, nil, fmt.Errorf("key must be %d bytes", 256)
	}

	// Determine x based on direction
	x := 0
	if !isOriginator {
		x = 8
	}

	// Generate random number for padding
	randBytes := make([]byte, 2)
	_, err = rand.Read(randBytes)
	if err != nil {
		return nil, nil, err
	}
	randNum := int(randBytes[0])<<8 | int(randBytes[1])

	// Add random padding (12..1024 bytes) to make length divisible by 16
	paddingLen := 12 + (randNum % 1013) // 12 + [0..1012]
	// Total length must include the 4-byte length prefix
	totalLen := 4 + len(plaintext) + paddingLen
	remainder := totalLen % 16
	if remainder != 0 {
		paddingLen += 16 - remainder
		totalLen = 4 + len(plaintext) + paddingLen
	}

	// Prepend 4 bytes with length
	dataWithLength := make([]byte, totalLen)
	dataWithLength[0] = byte(len(plaintext))
	dataWithLength[1] = byte(len(plaintext) >> 8)
	dataWithLength[2] = byte(len(plaintext) >> 16)
	dataWithLength[3] = byte(len(plaintext) >> 24)
	copy(dataWithLength[4:], plaintext)

	// Add random padding
	if paddingLen > 0 {
		padding := make([]byte, paddingLen)
		rand.Read(padding)
		copy(dataWithLength[4+len(plaintext):], padding)
	} // Compute msg_key (MTProto 2.0)
	// msg_key_large = SHA256(substr(key, 88+x, 32) + plaintext + random_padding)
	// msg_key = substr(msg_key_large, 8, 16)
	h := sha256.New()
	h.Write(key[88+x : 88+x+32])
	h.Write(dataWithLength)
	msgKeyLarge := h.Sum(nil)
	msgKey = msgKeyLarge[8:24]

	// Compute AES key and IV (MTProto 2.0)
	aesKey, aesIV := computeSecretChatAesKeyIV(msgKey, key, isOriginator)

	// Encrypt using AES-256-IGE
	cipher, err := NewCipher(aesKey, aesIV)
	if err != nil {
		return nil, nil, err
	}

	encrypted = make([]byte, len(dataWithLength))
	if err = cipher.DoAES256IGEencrypt(dataWithLength, encrypted); err != nil {
		return nil, nil, err
	}

	return msgKey, encrypted, nil
}

// DecryptMessageMTProto2 decrypts a message using MTProto 2.0 encryption for secret chats
// x = 0 for originator, x = 8 for responder (same as encryption)
func DecryptMessageMTProto(key []byte, msgKey []byte, encrypted []byte, isOriginator bool) (plaintext []byte, err error) {
	if len(key) != 256 {
		return nil, ErrKeySize
	}
	if len(msgKey) != 16 {
		return nil, ErrMsgKeySize
	}

	// Compute AES key and IV (MTProto 2.0)
	aesKey, aesIV := computeSecretChatAesKeyIV(msgKey, key, isOriginator)

	// Decrypt using AES-256-IGE
	cipher, err := NewCipher(aesKey, aesIV)
	if err != nil {
		return nil, err
	}

	decrypted := make([]byte, len(encrypted))
	if err = cipher.DoAES256IGEdecrypt(encrypted, decrypted); err != nil {
		return nil, err
	}

	// Verify msg_key
	x := 0
	if !isOriginator {
		x = 8
	}
	h := sha256.New()
	h.Write(key[88+x : 88+x+32])
	h.Write(decrypted)
	msgKeyLarge := h.Sum(nil)
	expectedMsgKey := msgKeyLarge[8:24]

	if !bytes.Equal(msgKey, expectedMsgKey) {
		return nil, errors.New("msg_key mismatch")
	}

	// Extract length and plaintext
	if len(decrypted) < 4 {
		return nil, errors.New("decrypted data too short")
	}

	length := int(decrypted[0]) | int(decrypted[1])<<8 | int(decrypted[2])<<16 | int(decrypted[3])<<24
	if length < 0 || length > len(decrypted)-4 {
		return nil, fmt.Errorf("invalid plaintext length: %d", length)
	}

	plaintext = make([]byte, length)
	copy(plaintext, decrypted[4:4+length])

	return plaintext, nil
}
