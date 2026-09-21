// Copyright (c) 2025 @AmarnathCJD
package math

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"fmt"
	"math"
	"math/big"
	"math/bits"

	ige "github.com/amarnathcjd/gogram/internal/aes_ige"
)

// DoRSAencrypt encrypts exactly one message block of size 255 bytes using the given public key.
// This is a custom algorithm for MTProto. The Telegram documentation does not specify
// whether this encryption follows OAEP or any other standard padding scheme.
// Use only for MTProto protocol blocks, not for general-purpose RSA encryption.
func DoRSAencrypt(block []byte, key *rsa.PublicKey) ([]byte, error) {
	if key == nil || key.N == nil || key.N.BitLen() != 2048 || key.E < 3 {
		return nil, fmt.Errorf("DoRSAencrypt: invalid RSA key")
	}
	if len(block) != math.MaxUint8 {
		return nil, fmt.Errorf("DoRSAencrypt: block size must be %d bytes, got %d", math.MaxUint8, len(block))
	}
	z := big.NewInt(0).SetBytes(block)
	exponent := big.NewInt(int64(key.E))

	c := big.NewInt(0).Exp(z, exponent, key.N)

	res := make([]byte, 256)
	c.FillBytes(res)

	return res, nil
}

// DoRSAPad implements the RSA_PAD scheme used by modern Telegram DCs (all CDN
// DCs, and required for MTProto 2.0 handshakes). Spec:
// https://core.telegram.org/mtproto/auth_key
func DoRSAPad(data []byte, key *rsa.PublicKey) ([]byte, error) {
	if key == nil || key.N == nil || key.N.BitLen() != 2048 || key.E < 3 {
		return nil, fmt.Errorf("DoRSAPad: invalid RSA key")
	}
	if len(data) > 144 {
		return nil, fmt.Errorf("DoRSAPad: data too long (%d > 144)", len(data))
	}

	dataWithPadding := make([]byte, 192)
	copy(dataWithPadding, data)
	if _, err := rand.Read(dataWithPadding[len(data):]); err != nil {
		return nil, fmt.Errorf("DoRSAPad: rand for padding: %w", err)
	}

	dataPadReversed := make([]byte, 192)
	for i, b := range dataWithPadding {
		dataPadReversed[192-1-i] = b
	}

	for range 20 {
		tempKey := make([]byte, 32)
		if _, err := rand.Read(tempKey); err != nil {
			return nil, fmt.Errorf("DoRSAPad: rand for temp_key: %w", err)
		}

		h := sha256.New()
		h.Write(tempKey)
		h.Write(dataWithPadding)
		dataWithHash := make([]byte, 0, 224)
		dataWithHash = append(dataWithHash, dataPadReversed...)
		dataWithHash = append(dataWithHash, h.Sum(nil)...)

		iv := make([]byte, 32)
		cipher, err := ige.NewCipher(tempKey, iv)
		if err != nil {
			return nil, fmt.Errorf("DoRSAPad: aes cipher: %w", err)
		}
		aesEncrypted := make([]byte, 224)
		if err := cipher.DoAES256IGEencrypt(dataWithHash, aesEncrypted); err != nil {
			return nil, fmt.Errorf("DoRSAPad: aes encrypt: %w", err)
		}

		aesHash := sha256.Sum256(aesEncrypted)
		tempKeyXor := make([]byte, 32)
		for i := range tempKeyXor {
			tempKeyXor[i] = tempKey[i] ^ aesHash[i]
		}

		keyAesEncrypted := make([]byte, 0, 256)
		keyAesEncrypted = append(keyAesEncrypted, tempKeyXor...)
		keyAesEncrypted = append(keyAesEncrypted, aesEncrypted...)

		z := big.NewInt(0).SetBytes(keyAesEncrypted)
		if z.Cmp(key.N) >= 0 {
			continue
		}

		exponent := big.NewInt(int64(key.E))
		c := big.NewInt(0).Exp(z, exponent, key.N)
		res := make([]byte, 256)
		cBytes := c.Bytes()
		copy(res[256-len(cBytes):], cBytes)
		return res, nil
	}
	return nil, fmt.Errorf("DoRSAPad: exhausted retries generating key_aes_encrypted < N")
}

func MakeGAB(g int32, g_a, dh_prime *big.Int) (b, g_b, g_ab *big.Int) {
	randmax := big.NewInt(0).SetBit(big.NewInt(0), 2048, 1)
	b = big.NewInt(0)
	randBytes := make([]byte, 256)
	rand.Read(randBytes)
	b.SetBytes(randBytes)
	b.Mod(b, randmax)
	g_b = big.NewInt(0).Exp(big.NewInt(int64(g)), b, dh_prime)
	g_ab = big.NewInt(0).Exp(g_a, b, dh_prime)

	return
}

func ValidateDHParams(g int32, g_a, dh_prime *big.Int) error {
	if dh_prime == nil || g_a == nil {
		return fmt.Errorf("dh: nil parameter")
	}
	if dh_prime.BitLen() != 2048 {
		return fmt.Errorf("dh: dh_prime is not 2048 bits (got %d)", dh_prime.BitLen())
	}
	if !ige.IsSafePrime(dh_prime, g) {
		return fmt.Errorf("dh: unsafe prime or generator")
	}

	two := big.NewInt(2)
	upper := new(big.Int).Sub(dh_prime, two)
	if g_a.Cmp(two) < 0 || g_a.Cmp(upper) > 0 {
		return fmt.Errorf("dh: g_a out of range [2, dh_prime-2]")
	}

	lowerSafe := new(big.Int).SetBit(big.NewInt(0), 1984, 1)
	upperSafe := new(big.Int).Sub(dh_prime, lowerSafe)
	if g_a.Cmp(lowerSafe) <= 0 || g_a.Cmp(upperSafe) >= 0 {
		return fmt.Errorf("dh: g_a outside recommended range [2^1984, dh_prime-2^1984]")
	}
	return nil
}

func ValidateGB(g_b, dh_prime *big.Int) error {
	if !ige.IsSafePublic(g_b, dh_prime) {
		return fmt.Errorf("dh: unsafe g_b")
	}
	return nil
}

func XOR(dst, src []byte) {
	for i := range dst {
		dst[i] ^= src[i]
	}
}

// Factorize splits the composite, at most 64-bit PQ used by MTProto.
// Invalid input or an exhausted search returns nil factors.
func Factorize(pq *big.Int) (*big.Int, *big.Int) {
	if pq == nil || pq.Cmp(big.NewInt(4)) < 0 || pq.BitLen() > 64 || pq.ProbablyPrime(16) {
		return nil, nil
	}
	p, q := factorizeU64(pq.Uint64())
	if p <= 1 || q <= 1 {
		return nil, nil
	}
	return new(big.Int).SetUint64(p), new(big.Int).SetUint64(q)
}

// gcd64 computes GCD(a, b) for uint64.
func gcd64(a, b uint64) uint64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// mulmod computes (a * b) % mod using 128-bit intermediate via bits.Mul64.
func mulmod(a, b, mod uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	_, r := bits.Div64(hi, lo, mod)
	return r
}

// f(x) = (x*x + c) % n
func fStep(x, c, n uint64) uint64 {
	x = mulmod(x, x, n)
	c %= n
	if x >= n-c {
		return x - (n - c)
	}
	return x + c
}

// factorizeU64 factors a 64-bit integer using Pollard's Rho (Brent variant).
// Returns (p, q) with p <= q, or (0, 0) on failure (extremely unlikely for 62-bit n).
func factorizeU64(n uint64) (uint64, uint64) {
	if n%2 == 0 {
		return 2, n / 2
	}
	if n < 3 {
		return 1, n
	}

	const maxIterPerTry = 1000000

	for c := uint64(1); c < 32; c++ {
		// Brent's Pollard-rho parameters
		y := uint64(2)
		m := uint64(128)
		g := uint64(1)
		r := uint64(1)
		q := uint64(1)

		iter := 0

		for g == 1 && iter < maxIterPerTry {
			x := y
			for i := uint64(0); i < r && iter < maxIterPerTry; i++ {
				y = fStep(y, c, n)
				iter++
			}

			k := uint64(0)
			for k < r && g == 1 && iter < maxIterPerTry {
				ys := y
				limit := min(r-k, m)

				for range limit {
					y = fStep(y, c, n)
					var diff uint64
					if x > y {
						diff = x - y
					} else {
						diff = y - x
					}
					if diff == 0 {
						q = 0
						iter++
						break
					}
					q = mulmod(q, diff, n)
					iter++
					if iter >= maxIterPerTry {
						break
					}
				}

				if q != 0 {
					g = gcd64(q, n)
				} else {
					// q == 0 means we multiplied by something divisible by n,
					// fall back to simple gcd on |x - ys|
					var diff uint64
					if x > ys {
						diff = x - ys
					} else {
						diff = ys - x
					}
					g = gcd64(diff, n)
				}

				k += limit
			}

			r <<= 1
		}

		if g == n || g == 1 {
			continue
		}

		p := g
		q = n / g
		if p > q {
			p, q = q, p
		}
		return p, q
	}

	return 0, 0
}
