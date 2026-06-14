// Copyright (c) 2025 @AmarnathCJD

package ige

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

var (
	aesV1Magic = [4]byte{0xA3, 0x5C, 0x01, 0x00}
	aesV2Magic = [4]byte{0xA3, 0x5C, 0x02, 0x00}
)

const hmacSize = sha256.Size

func deriveKeys(key string) (encKey, macKey []byte) {
	hEnc := sha256.Sum256(append([]byte("gogram-enc:"), key...))
	hMac := sha256.Sum256(append([]byte("gogram-mac:"), key...))
	return hEnc[:], hMac[:]
}

func EncryptAES(data []byte, key string) ([]byte, error) {
	encKey, macKey := deriveKeys(key)
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	bs := block.BlockSize()

	padded := pkcs5Padding(data, bs)
	body := make([]byte, bs+len(padded))
	iv := body[:bs]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(body[bs:], padded)

	mac := hmac.New(sha256.New, macKey)
	mac.Write(aesV2Magic[:])
	mac.Write(body)
	tag := mac.Sum(nil)

	out := make([]byte, 0, len(aesV2Magic)+len(body)+len(tag))
	out = append(out, aesV2Magic[:]...)
	out = append(out, body...)
	out = append(out, tag...)
	return out, nil
}

func DecryptAES(data []byte, key string) ([]byte, error) {
	encKey, macKey := deriveKeys(key)
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	bs := block.BlockSize()

	if len(data) >= len(aesV2Magic) && bytes.Equal(data[:len(aesV2Magic)], aesV2Magic[:]) {
		if len(data) < len(aesV2Magic)+bs+bs+hmacSize {
			return nil, errors.New("decrypt: v2 ciphertext too short")
		}
		body := data[len(aesV2Magic) : len(data)-hmacSize]
		tag := data[len(data)-hmacSize:]
		if (len(body)-bs)%bs != 0 {
			return nil, errors.New("decrypt: v2 body not aligned")
		}
		mac := hmac.New(sha256.New, macKey)
		mac.Write(aesV2Magic[:])
		mac.Write(body)
		if !hmac.Equal(mac.Sum(nil), tag) {
			return nil, errors.New("decrypt: v2 MAC mismatch (file corrupted or wrong key)")
		}
		iv := body[:bs]
		ct := body[bs:]
		pt := make([]byte, len(ct))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(pt, ct)
		unpadded, ok := pkcs5UnPadding(pt, bs)
		if !ok {
			return nil, errors.New("decrypt: invalid padding")
		}
		return unpadded, nil
	}

	legacyBlock, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}

	if len(data) >= len(aesV1Magic)+bs && bytes.Equal(data[:len(aesV1Magic)], aesV1Magic[:]) {
		body := data[len(aesV1Magic):]
		if len(body) < bs+bs || (len(body)-bs)%bs != 0 {
			return nil, errors.New("decrypt: malformed ciphertext")
		}
		iv := body[:bs]
		ct := body[bs:]
		pt := make([]byte, len(ct))
		cipher.NewCBCDecrypter(legacyBlock, iv).CryptBlocks(pt, ct)
		unpadded, ok := pkcs5UnPadding(pt, bs)
		if !ok {
			return nil, errors.New("decrypt: invalid padding")
		}
		return unpadded, nil
	}

	if len(data) == 0 || len(data)%bs != 0 {
		return nil, errors.New("decrypt: ciphertext is not a multiple of the block size")
	}
	legacyIV := []byte(key)
	if len(legacyIV) < bs {
		return nil, errors.New("decrypt: key too short for legacy IV")
	}
	pt := make([]byte, len(data))
	cipher.NewCBCDecrypter(legacyBlock, legacyIV[:bs]).CryptBlocks(pt, data)
	unpadded, ok := pkcs5UnPadding(pt, bs)
	if !ok {
		return nil, errors.New("decrypt: invalid padding")
	}
	return unpadded, nil
}

func pkcs5Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

// pkcs5UnPadding validates and strips PKCS#5/#7 padding.
// Returns (data, true) on success, (nil, false) on malformed padding.
func pkcs5UnPadding(data []byte, blockSize int) ([]byte, bool) {
	length := len(data)
	if length == 0 || length%blockSize != 0 {
		return nil, false
	}
	pad := int(data[length-1])
	if pad < 1 || pad > blockSize || pad > length {
		return nil, false
	}
	for i := length - pad; i < length; i++ {
		if int(data[i]) != pad {
			return nil, false
		}
	}
	return data[:length-pad], true
}
