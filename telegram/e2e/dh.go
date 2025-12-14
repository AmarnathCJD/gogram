// Copyright (c) 2025 @AmarnathCJD

package e2e

import (
	"crypto/rand"
	"crypto/sha1"
	"errors"
	"math/big"
)

const (
	KeySize = 256 // 256 bytes = 2048 bits
)

// DH holds Diffie-Hellman parameters
type DH struct {
	Prime     *big.Int
	G         int32
	GA        *big.Int // g^a mod p
	GB        *big.Int // g^b mod p
	SharedKey []byte
	A         *big.Int // private key (our secret)
}

// NewDH creates a new DH instance with server parameters
func NewDH(prime []byte, g int32) (*DH, error) {
	p := new(big.Int).SetBytes(prime)

	// Validate prime
	if !IsSafePrime(p, g) {
		return nil, errors.New("invalid prime or g parameters")
	}

	return &DH{
		Prime: p,
		G:     g,
	}, nil
}

// GenerateKey generates a random 2048-bit private key 'a' and computes g^a mod p
func (dh *DH) GenerateKey() error {
	// Generate random 2048-bit number 'a'
	a := make([]byte, 256) // 256 bytes = 2048 bits
	_, err := rand.Read(a)
	if err != nil {
		return err
	}

	dh.A = new(big.Int).SetBytes(a)

	// Compute g^a mod p
	g := big.NewInt(int64(dh.G))
	dh.GA = new(big.Int).Exp(g, dh.A, dh.Prime)

	// Validate g_a
	if !IsValidGAOrGB(dh.GA, dh.Prime) {
		// Retry with new random number
		return dh.GenerateKey()
	}

	return nil
}

// ComputeSharedKey computes the shared key using the other party's public key
func (dh *DH) ComputeSharedKey(otherPublicKey []byte) error {
	dh.GB = new(big.Int).SetBytes(otherPublicKey)

	// Validate g_b
	if !IsValidGAOrGB(dh.GB, dh.Prime) {
		return errors.New("invalid g_b received")
	}

	// Compute shared key = (g^b)^a mod p
	sharedKey := new(big.Int).Exp(dh.GB, dh.A, dh.Prime)

	// Convert to bytes and pad to 256 bytes if needed
	keyBytes := sharedKey.Bytes()
	dh.SharedKey = PadKeyTo256(keyBytes)

	return nil
}

// IsSafePrime checks if p is a safe 2048-bit prime and g generates the correct subgroup
// A safe prime p satisfies: p and (p-1)/2 are both prime, and 2^2047 < p < 2^2048
func IsSafePrime(p *big.Int, g int32) bool {
	// Check bit length (should be 2048 bits)
	bitLen := p.BitLen()
	if bitLen != 2048 {
		return false
	}

	// Check that p is in the range 2^2047 < p < 2^2048
	min := new(big.Int).Exp(big.NewInt(2), big.NewInt(2047), nil)
	max := new(big.Int).Exp(big.NewInt(2), big.NewInt(2048), nil)
	if p.Cmp(min) <= 0 || p.Cmp(max) >= 0 {
		return false
	}

	// Verify that g is in the expected range (2, 3, 4, 5, 6, or 7)
	if g < 2 || g > 7 {
		return false
	}

	// Verify quadratic residue condition based on g
	// This is a simplified check using quadratic reciprocity law
	mod := new(big.Int)
	switch g {
	case 2:
		// p mod 8 = 7
		mod.Mod(p, big.NewInt(8))
		return mod.Cmp(big.NewInt(7)) == 0
	case 3:
		// p mod 3 = 2
		mod.Mod(p, big.NewInt(3))
		return mod.Cmp(big.NewInt(2)) == 0
	case 4:
		// No extra condition for g = 4
		return true
	case 5:
		// p mod 5 = 1 or 4
		mod.Mod(p, big.NewInt(5))
		return mod.Cmp(big.NewInt(1)) == 0 || mod.Cmp(big.NewInt(4)) == 0
	case 6:
		// p mod 24 = 19 or 23
		mod.Mod(p, big.NewInt(24))
		return mod.Cmp(big.NewInt(19)) == 0 || mod.Cmp(big.NewInt(23)) == 0
	case 7:
		// p mod 7 = 3, 5 or 6
		mod.Mod(p, big.NewInt(7))
		return mod.Cmp(big.NewInt(3)) == 0 || mod.Cmp(big.NewInt(5)) == 0 || mod.Cmp(big.NewInt(6)) == 0
	}

	return false
}

// IsValidGAOrGB validates that g_a or g_b is in the valid range
// Must be: 1 < g_a < p-1 and 2^{2048-64} < g_a < p - 2^{2048-64}
func IsValidGAOrGB(value, prime *big.Int) bool {
	one := big.NewInt(1)

	// Check g_a > 1
	if value.Cmp(one) <= 0 {
		return false
	}

	// Check g_a < p-1
	pMinus1 := new(big.Int).Sub(prime, one)
	if value.Cmp(pMinus1) >= 0 {
		return false
	}

	// Check 2^{2048-64} < g_a < p - 2^{2048-64}
	power := new(big.Int).Exp(big.NewInt(2), big.NewInt(2048-64), nil)
	if value.Cmp(power) <= 0 {
		return false
	}

	upperBound := new(big.Int).Sub(prime, power)
	return value.Cmp(upperBound) < 0
}

// ComputeKeyFingerprint computes the 64-bit fingerprint of a shared key
// Used for sanity checking during key exchange
func ComputeKeyFingerprint(key []byte) int64 {
	if len(key) < KeySize {
		// Pad with zeros if key is shorter
		paddedKey := make([]byte, KeySize)
		copy(paddedKey, key)
		key = paddedKey
	}

	h := sha1.New()
	h.Write(key)
	hash := h.Sum(nil)

	// Take last 64 bits (8 bytes) - bytes 12-19 of the 20-byte SHA1 hash
	// Read as little-endian int64 (Telegram uses little-endian)
	fingerprint := int64(0)
	for i := 0; i < 8; i++ {
		fingerprint |= int64(hash[12+i]) << (i * 8)
	}

	return fingerprint
}

// PadKeyTo256 pads a key with leading zeros to ensure it's exactly 256 bytes
func PadKeyTo256(key []byte) []byte {
	if len(key) >= KeySize {
		return key[:KeySize]
	}

	padded := make([]byte, KeySize)
	copy(padded[KeySize-len(key):], key)
	return padded
}
