// Copyright (c) 2025 @AmarnathCJD

package e2e

import (
	"crypto/rand"
	"crypto/sha1"
	"errors"
	"math/big"

	ige "github.com/amarnathcjd/gogram/internal/aes_ige"
)

const KeySize = 256

type DH struct {
	Prime     *big.Int
	G         int32
	GA        *big.Int
	GB        *big.Int
	SharedKey []byte
	A         *big.Int
}

func NewDH(prime []byte, g int32) (*DH, error) {
	p := new(big.Int).SetBytes(prime)

	if !IsSafePrime(p, g) {
		return nil, errors.New("invalid DH prime or generator")
	}

	return &DH{
		Prime: p,
		G:     g,
	}, nil
}

// GenerateKey samples 'a' uniformly from [2, p-2] (with extra server entropy
// mixed in) and computes g^a mod p. Repeats until g_a passes the safety
// bound check from the spec.
func (dh *DH) GenerateKey(extraEntropy []byte) error {
	if dh == nil || !IsSafePrime(dh.Prime, dh.G) {
		return errors.New("invalid DH parameters")
	}
	pMinus3 := new(big.Int).Sub(dh.Prime, big.NewInt(3))

	for range 64 {
		raw := make([]byte, KeySize)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		for i := 0; i < len(extraEntropy) && i < len(raw); i++ {
			raw[i] ^= extraEntropy[i]
		}

		// a = 2 + (raw mod (p-3))  -> a in [2, p-2]
		a := new(big.Int).SetBytes(raw)
		a.Mod(a, pMinus3)
		a.Add(a, big.NewInt(2))

		g := big.NewInt(int64(dh.G))
		gA := new(big.Int).Exp(g, a, dh.Prime)

		if !IsValidGAOrGB(gA, dh.Prime) {
			continue
		}

		dh.A = a
		dh.GA = gA
		return nil
	}
	return errors.New("failed to generate valid DH key after 64 attempts")
}

func (dh *DH) ComputeSharedKey(otherPublicKey []byte) error {
	if dh == nil || dh.A == nil || dh.Prime == nil || len(otherPublicKey) > KeySize {
		return errors.New("invalid DH key state")
	}
	dh.GB = new(big.Int).SetBytes(otherPublicKey)

	if !IsValidGAOrGB(dh.GB, dh.Prime) {
		return errors.New("invalid g_b received")
	}

	sharedKey := new(big.Int).Exp(dh.GB, dh.A, dh.Prime)
	dh.SharedKey = PadKeyTo256(sharedKey.Bytes())

	return nil
}

// IsSafePrime validates the DH parameters used by secret chats.
func IsSafePrime(p *big.Int, g int32) bool { return ige.IsSafePrime(p, g) }

func IsValidGAOrGB(value, prime *big.Int) bool { return ige.IsSafePublic(value, prime) }

// ComputeKeyFingerprint returns the lower 64 bits of SHA1(auth_key) interpreted
// little-endian, matching Telegram's auth_key_id convention.
func ComputeKeyFingerprint(key []byte) int64 {
	if len(key) < KeySize {
		key = PadKeyTo256(key)
	}

	h := sha1.New()
	h.Write(key)
	hash := h.Sum(nil)

	var fp int64
	for i := range 8 {
		fp |= int64(hash[12+i]) << (i * 8)
	}
	return fp
}

func PadKeyTo256(key []byte) []byte {
	if len(key) >= KeySize {
		return key[:KeySize]
	}

	padded := make([]byte, KeySize)
	copy(padded[KeySize-len(key):], key)
	return padded
}
