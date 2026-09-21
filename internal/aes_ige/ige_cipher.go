// Copyright (c) 2025 @AmarnathCJD

package ige

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"fmt"
)

type Cipher struct {
	block              cipher.Block
	previousCiphertext [aes.BlockSize]byte
	previousPlaintext  [aes.BlockSize]byte
}

func NewCipher(key, iv []byte) (*Cipher, error) {
	if len(iv) != 2*aes.BlockSize {
		return nil, fmt.Errorf("IGE IV must be 32 bytes, got %d", len(iv))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating new cipher: %w", err)
	}
	c := &Cipher{block: block}
	copy(c.previousCiphertext[:], iv[:aes.BlockSize])
	copy(c.previousPlaintext[:], iv[aes.BlockSize:])
	return c, nil
}

// DoAES256IGEencrypt supports separate buffers or exact in-place operation.
// A Cipher retains its chaining state and must not be used concurrently.
func (c *Cipher) DoAES256IGEencrypt(in, out []byte) error {
	if err := isCorrectData(in); err != nil {
		return err
	}
	if len(out) < len(in) {
		return fmt.Errorf("IGE output is shorter than input")
	}
	var plain, block [aes.BlockSize]byte
	for i := 0; i < len(in); i += aes.BlockSize {
		copy(plain[:], in[i:i+aes.BlockSize])
		for j := range block {
			block[j] = plain[j] ^ c.previousCiphertext[j]
		}
		c.block.Encrypt(block[:], block[:])
		for j := range block {
			block[j] ^= c.previousPlaintext[j]
		}
		copy(out[i:], block[:])
		c.previousCiphertext = block
		c.previousPlaintext = plain
	}
	return nil
}

// DoAES256IGEdecrypt supports separate buffers or exact in-place operation.
func (c *Cipher) DoAES256IGEdecrypt(in, out []byte) error {
	if err := isCorrectData(in); err != nil {
		return err
	}
	if len(out) < len(in) {
		return fmt.Errorf("IGE output is shorter than input")
	}
	var encrypted, block [aes.BlockSize]byte
	for i := 0; i < len(in); i += aes.BlockSize {
		copy(encrypted[:], in[i:i+aes.BlockSize])
		for j := range block {
			block[j] = encrypted[j] ^ c.previousPlaintext[j]
		}
		c.block.Decrypt(block[:], block[:])
		for j := range block {
			block[j] ^= c.previousCiphertext[j]
		}
		copy(out[i:], block[:])
		c.previousCiphertext = encrypted
		c.previousPlaintext = block
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

	var sha256a, sha256b [sha256.Size]byte
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
