// Copyright (c) 2022 RoseLoverX

package keys

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

var RsaKeys = "-----BEGIN RSA PUBLIC KEY-----\nMIIBCgKCAQEAwVACPi9w23mF3tBkdZz+zwrzKOaaQdr01vAbU4E1pvkfj4sqDsm6\nlyDONS789sVoD/xCS9Y0hkkC3gtL1tSfTlgCMOOul9lcixlEKzwKENj1Yz/s7daS\nan9tqw3bfUV/nqgbhGX81v/+7RFAEd+RwFnK7a+XYl9sluzHRyVVaTTveB2GazTw\nEfzk2DWgkBluml8OREmvfraX3bkHZJTKX4EQSjBbbdJ2ZXIsRrYOXfaA+xayEGB+\n8hdlLmAjbCVfaigxX0CDqWeR1yFL9kwd9P0NsZRPsmoqVwMbMu7mStFai6aIhc3n\nSlv8kg9qv1m6XHVQY3PnEw+QQtqSIXklHwIDAQAB\n-----END RSA PUBLIC KEY-----\n\n-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAruw2yP/BCcsJliRoW5eB\nVBVle9dtjJw+OYED160Wybum9SXtBBLXriwt4rROd9csv0t0OHCaTmRqBcQ0J8fx\nhN6/cpR1GWgOZRUAiQxoMnlt0R93LCX/j1dnVa/gVbCjdSxpbrfY2g2L4frzjJvd\nl84Kd9ORYjDEAyFnEA7dD556OptgLQQ2e2iVNq8NZLYTzLp5YpOdO1doK+ttrltg\ngTCy5SrKeLoCPPbOgGsdxJxyz5KKcZnSLj16yE5HvJQn0CNpRdENvRUXe6tBP78O\n39oJ8BTHp9oIjd6XWXAsp2CvK45Ol8wFXGF710w9lwCGNbmNxNYhtIkdqfsEcwR5\nJwIDAQAB\n-----END PUBLIC KEY-----\n\n-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvfLHfYH2r9R70w8prHbl\nWt/nDkh+XkgpflqQVcnAfSuTtO05lNPspQmL8Y2XjVT4t8cT6xAkdgfmmvnvRPOO\nKPi0OfJXoRVylFzAQG/j83u5K3kRLbae7fLccVhKZhY46lvsueI1hQdLgNV9n1cQ\n3TDS2pQOCtovG4eDl9wacrXOJTG2990VjgnIKNA0UMoP+KF03qzryqIt3oTvZq03\nDyWdGK+AZjgBLaDKSnC6qD2cFY81UryRWOab8zKkWAnhw2kFpcqhI0jdV5QaSCEx\nvnsjVaX0Y1N0870931/5Jb9ICe4nweZ9kSDF/gip3kWLG0o8XQpChDfyvsqB9OLV\n/wIDAQAB\n-----END PUBLIC KEY-----\n\n-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAs/ditzm+mPND6xkhzwFI\nz6J/968CtkcSE/7Z2qAJiXbmZ3UDJPGrzqTDHkO30R8VeRM/Kz2f4nR05GIFiITl\n4bEjvpy7xqRDspJcCFIOcyXm8abVDhF+th6knSU0yLtNKuQVP6voMrnt9MV1X92L\nGZQLgdHZbPQz0Z5qIpaKhdyA8DEvWWvSUwwc+yi1/gGaybwlzZwqXYoPOhwMebzK\nUk0xW14htcJrRrq+PXXQbRzTMynseCoPIoke0dtCodbA3qQxQovE16q9zz4Otv2k\n4j63cz53J+mhkVWAeWxVGI0lltJmWtEYK6er8VqqWot3nqmWMXogrgRLggv/Nbbo\noQIDAQAB\n-----END PUBLIC KEY-----\n\n-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvmpxVY7ld/8DAjz6F6q0\n5shjg8/4p6047bn6/m8yPy1RBsvIyvuDuGnP/RzPEhzXQ9UJ5Ynmh2XJZgHoE9xb\nnfxL5BXHplJhMtADXKM9bWB11PU1Eioc3+AXBB8QiNFBn2XI5UkO5hPhbb9mJpjA\n9Uhw8EdfqJP8QetVsI/xrCEbwEXe0xvifRLJbY08/Gp66KpQvy7g8w7VB8wlgePe\nxW3pT13Ap6vuC+mQuJPyiHvSxjEKHgqePji9NP3tJUFQjcECqcm0yV7/2d0t/pbC\nm+ZH1sadZspQCEPPrtbkQBlvHb4OLiIWPGHKSMeRFvp3IWcmdJqXahxLCUS1Eh6M\nAQIDAQAB\n-----END PUBLIC KEY-----"

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
	return []byte(fingerprint)[12:]
}

func GetRSAKeys() ([]*rsa.PublicKey, error) {
	data := []byte(RsaKeys)
	keys := make([]*rsa.PublicKey, 0)
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}

		key, err := pemBytesToRsa(block.Bytes)
		if err != nil {
			const offset = 1
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
