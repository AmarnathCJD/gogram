// Copyright (c) 2022 RoseLoverX

package keys

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"log"
	"math/big"
	"os"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

// RSAFingerprint вычисляет отпечаток ключа
// т.к. rsa ключ в понятиях MTProto это TL объект, то используется буффер
// подробнее https://core.telegram.org/mtproto/auth_key
func RSAFingerprint(key *rsa.PublicKey) []byte {
	if key == nil {
		log.Fatal("key is nil")
	}
	exponentAsBigInt := (big.NewInt(0)).SetInt64(int64(key.E))

	buf := bytes.NewBuffer(nil)
	e := tl.NewEncoder(buf)
	e.PutMessage(key.N.Bytes())
	e.PutMessage(exponentAsBigInt.Bytes())

	fingerprint := Sha1(buf.String())
	return []byte(fingerprint)[12:] // последние 8 байт это и есть отпечаток
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ReadFromFile(path string) ([]*rsa.PublicKey, error) {
	if !FileExists(path) {
		return nil, fmt.Errorf("file %s not found", path)
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keys := make([]*rsa.PublicKey, 0)
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}

		key, err := pemBytesToRsa(block.Bytes)
		if err != nil {
			const offset = 1 // +1 потому что считаем с 0
			return nil, fmt.Errorf("failed to parse key at offset %d: %s", len(data)-len(rest)+offset, err)
		}

		keys = append(keys, key)
		data = rest
	}

	return keys, nil
}

func pemBytesToRsa(data []byte) (*rsa.PublicKey, error) {
	key, err := x509.ParsePKCS1PublicKey(data)
	if err == nil {
		return key, nil
	}

	if err.Error() == "x509: failed to parse public key (use ParsePKIXPublicKey instead for this key format)" {
		var k interface{}
		k, err = x509.ParsePKIXPublicKey(data)
		if err == nil {
			return k.(*rsa.PublicKey), nil
		}
	}

	return nil, err
}

func SaveRsaKey(key *rsa.PublicKey) string {
	data := x509.MarshalPKCS1PublicKey(key)
	buf := bytes.NewBufferString("")
	err := pem.Encode(buf, &pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: data,
	})
	if err != nil {
		log.Fatal(err)
	}

	return buf.String()
}

func Sha1(input string) []byte {
	r := sha1.Sum([]byte(input))
	return r[:]
}
