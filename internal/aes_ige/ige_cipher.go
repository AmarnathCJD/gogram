// Copyright (c) 2025 @AmarnathCJD

package ige

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"fmt"

	"github.com/amarnathcjd/gogram/internal/utils"
)

type Cipher struct {
	block   cipher.Block
	v       [3]AesBlock
	t, x, y []byte
}

// NewCipher
func NewCipher(key, iv []byte) (*Cipher, error) {
	const (
		firstBlock = iota
		secondBlock
		thirdBlock
	)

	var err error

	c := new(Cipher)
	c.block, err = aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating new cipher: %w", err)
	}

	c.t = c.v[firstBlock][:]
	c.x = c.v[secondBlock][:]
	c.y = c.v[thirdBlock][:]
	copy(c.x, iv[:aes.BlockSize])
	copy(c.y, iv[aes.BlockSize:])

	return c, nil
}

func (c *Cipher) doAES256IGEencrypt(in, out []byte) error { //nolint:dupl
	if err := isCorrectData(in); err != nil {
		return err
	}

	for i := 0; i < len(in); i += aes.BlockSize {
		utils.Xor(c.x, in[i:i+aes.BlockSize])
		c.block.Encrypt(c.t, c.x)
		utils.Xor(c.t, c.y)
		c.x, c.y = c.t, in[i:i+aes.BlockSize]
		copy(out[i:], c.t)
	}
	return nil
}

func (c *Cipher) doAES256IGEdecrypt(in, out []byte) error { //nolint:dupl
	if err := isCorrectData(in); err != nil {
		return err
	}

	for i := 0; i < len(in); i += aes.BlockSize {
		utils.Xor(c.y, in[i:i+aes.BlockSize])
		c.block.Decrypt(c.t, c.y)
		utils.Xor(c.t, c.x)
		c.y, c.x = c.t, in[i:i+aes.BlockSize]
		copy(out[i:], c.t)
	}
	return nil
}

func isCorrectData(data []byte) error {
	if len(data) < aes.BlockSize {
		return ErrDataTooSmall
	}
	if len(data)%aes.BlockSize != 0 {
		return ErrDataNotDivisible
	}
	return nil
}

// --------------------------------------------------------------------------------------------------

func aesKeys(msgKey, authKey []byte, decode bool) (aesKey, aesIv [32]byte) {
	var x int
	if decode {
		x = 8
	} else {
		x = 0
	}

	// aes_key = substr (sha256_a, 0, 8) + substr (sha256_b, 8, 16) + substr (sha256_a, 24, 8);
	computeAesKey := func(sha256a, sha256b []byte) (v [32]byte) {
		n := copy(v[:], sha256a[:8])
		n += copy(v[n:], sha256b[8:16+8])
		copy(v[n:], sha256a[24:24+8])
		return v
	}
	// aes_iv = substr (sha256_b, 0, 8) + substr (sha256_a, 8, 16) + substr (sha256_b, 24, 8);
	computeAesIV := func(sha256b, sha256a []byte) (v [32]byte) {
		n := copy(v[:], sha256a[:8])
		n += copy(v[n:], sha256b[8:16+8])
		copy(v[n:], sha256a[24:24+8])
		return v
	}

	var sha256a, sha256b [256]byte
	// sha256_a = SHA256 (msg_key + substr (auth_key, x, 36));
	{
		h := sha256.New()

		_, _ = h.Write(msgKey)
		_, _ = h.Write(authKey[x : x+36])

		h.Sum(sha256a[:0])
	}
	// sha256_b = SHA256 (substr (auth_key, 40+x, 36) + msg_key);
	{
		h := sha256.New()

		substr := authKey[40+x:]
		_, _ = h.Write(substr[:36])
		_, _ = h.Write(msgKey)

		h.Sum(sha256b[:0])
	}

	return computeAesKey(sha256a[:], sha256b[:]), computeAesIV(sha256a[:], sha256b[:])
}
