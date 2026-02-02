// Copyright (c) 2024 RoseLoverX

package ige

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
)

func EncryptAES(data []byte, key string) ([]byte, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	b := pkcs5Padding(data, block.BlockSize())
	blockMode := cipher.NewCBCEncrypter(block, []byte(key)[:block.BlockSize()])
	crypted := make([]byte, len(b))
	blockMode.CryptBlocks(crypted, b)
	return crypted, nil
}

func DecryptAES(data []byte, key string) ([]byte, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	blockMode := cipher.NewCBCDecrypter(block, []byte(key)[:block.BlockSize()])
	origData := make([]byte, len(data))
	blockMode.CryptBlocks(origData, data)
	origData = pkcs5UnPadding(origData)
	return origData, nil
}

func pkcs5Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

func pkcs5UnPadding(origData []byte) []byte {
	length := len(origData)
	if length == 0 {
		return origData
	}
	unpadding := int(origData[length-1])
	return origData[:(length - unpadding)]
}
