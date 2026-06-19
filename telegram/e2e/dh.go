// Copyright (c) 2025 @AmarnathCJD

package e2e

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"math/big"
	"sync"
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

const telegramKnownPrimeHex = "C71CAEB9C6B1C9048E6C522F70F13F73980D40238E3E21C14934D037563D930F" +
	"48198A0AA7C14058229493D22530F4DBFA336F6E0AC925139543AED44CCE7C37" +
	"20FD51F69458705AC68CD4FE6B6B13ABDC9746512969328454F18FAF8C595F64" +
	"2477FE96BB2A941D5BCD1D4AC8CC49880708FA9B378E3C4F3A9060BEE67CF9A4" +
	"A4A695811051907E162753B56B0F6B410DBA74D8A84B2A14B3144E0EF1284754" +
	"FD17ED950D5965B4B9DD46582DB1178D169C6BC465B0D6FF9CA3928FEF5B9AE4" +
	"E418FC15E83EBEA0F87FA9FF5EED70050DED2849F47BF959D956850CE929851F" +
	"0D8115F635B105EE2E4E15D04B2454BF6F4FADF034B10403119CD8E3B92FCC5B"

var (
	telegramKnownPrime     *big.Int
	telegramKnownPrimeOnce sync.Once
)

func knownPrime() *big.Int {
	telegramKnownPrimeOnce.Do(func() {
		b, err := hex.DecodeString(telegramKnownPrimeHex)
		if err != nil {
			panic("invalid hardcoded Telegram prime: " + err.Error())
		}
		telegramKnownPrime = new(big.Int).SetBytes(b)
	})
	return telegramKnownPrime
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
	dh.GB = new(big.Int).SetBytes(otherPublicKey)

	if !IsValidGAOrGB(dh.GB, dh.Prime) {
		return errors.New("invalid g_b received")
	}

	sharedKey := new(big.Int).Exp(dh.GB, dh.A, dh.Prime)
	dh.SharedKey = PadKeyTo256(sharedKey.Bytes())

	return nil
}

// IsSafePrime validates the DH prime per the spec at
// https://core.telegram.org/api/end-to-end.
//
//	(1) p is 2048 bits, 2^2047 < p < 2^2048.
//	(2) g is 2..7 and matches the quadratic residue test for that g.
//	(3) p matches Telegram's well-known prime (pinned).
//	(4) Both p and (p-1)/2 are probabilistic primes (Miller-Rabin).
func IsSafePrime(p *big.Int, g int32) bool {
	if p.BitLen() != 2048 {
		return false
	}

	min := new(big.Int).Lsh(big.NewInt(1), 2047)
	max := new(big.Int).Lsh(big.NewInt(1), 2048)
	if p.Cmp(min) <= 0 || p.Cmp(max) >= 0 {
		return false
	}

	if g < 2 || g > 7 {
		return false
	}

	mod := new(big.Int)
	switch g {
	case 2:
		mod.Mod(p, big.NewInt(8))
		if mod.Cmp(big.NewInt(7)) != 0 {
			return false
		}
	case 3:
		mod.Mod(p, big.NewInt(3))
		if mod.Cmp(big.NewInt(2)) != 0 {
			return false
		}
	case 4:
	case 5:
		mod.Mod(p, big.NewInt(5))
		if mod.Cmp(big.NewInt(1)) != 0 && mod.Cmp(big.NewInt(4)) != 0 {
			return false
		}
	case 6:
		mod.Mod(p, big.NewInt(24))
		if mod.Cmp(big.NewInt(19)) != 0 && mod.Cmp(big.NewInt(23)) != 0 {
			return false
		}
	case 7:
		mod.Mod(p, big.NewInt(7))
		if mod.Cmp(big.NewInt(3)) != 0 && mod.Cmp(big.NewInt(5)) != 0 && mod.Cmp(big.NewInt(6)) != 0 {
			return false
		}
	default:
		return false
	}

	if p.Cmp(knownPrime()) == 0 {
		return true
	}

	// Unknown prime: confirm both p and (p-1)/2 are prime before trusting it.
	// 64 rounds gives a false-positive probability of 4^-64.
	if !p.ProbablyPrime(64) {
		return false
	}
	q := new(big.Int).Sub(p, big.NewInt(1))
	q.Rsh(q, 1)
	return q.ProbablyPrime(64)
}

func IsValidGAOrGB(value, prime *big.Int) bool {
	one := big.NewInt(1)

	if value.Cmp(one) <= 0 {
		return false
	}

	pMinus1 := new(big.Int).Sub(prime, one)
	if value.Cmp(pMinus1) >= 0 {
		return false
	}

	power := new(big.Int).Lsh(big.NewInt(1), 2048-64)
	if value.Cmp(power) <= 0 {
		return false
	}

	upperBound := new(big.Int).Sub(prime, power)
	return value.Cmp(upperBound) < 0
}

// ComputeKeyFingerprint returns the lower 64 bits of SHA1(auth_key) interpreted
// little-endian, matching Telegram's auth_key_id convention.
func ComputeKeyFingerprint(key []byte) int64 {
	if len(key) < KeySize {
		paddedKey := make([]byte, KeySize)
		copy(paddedKey, key)
		key = paddedKey
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
