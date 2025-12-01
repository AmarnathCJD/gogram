// Copyright (c) 2025 @AmarnathCJD
package math

import (
	"crypto/rand"
	"crypto/rsa"
	"math"
	"math/big"
	"math/bits"
)

// DoRSAencrypt encrypts exactly one message block of size 255 bytes using the given public key.
// This is a custom algorithm for MTProto. The Telegram documentation does not specify
// whether this encryption follows OAEP or any other standard padding scheme.
// Use only for MTProto protocol blocks, not for general-purpose RSA encryption.
func DoRSAencrypt(block []byte, key *rsa.PublicKey) []byte {
	if len(block) != math.MaxUint8 {
		panic("block size isn't equal 255 bytes")
	}
	z := big.NewInt(0).SetBytes(block)
	exponent := big.NewInt(int64(key.E))

	c := big.NewInt(0).Exp(z, exponent, key.N)

	res := make([]byte, 256)
	copy(res, c.Bytes())

	return res
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

func XOR(dst, src []byte) {
	for i := range dst {
		dst[i] ^= src[i]
	}
}

func Factorize(pq *big.Int) (*big.Int, *big.Int) {
	if pq.BitLen() <= 64 {
		pqU64 := pq.Uint64()
		p, q := factorizeU64(pqU64)
		if p == 0 || q == 0 {
			return nil, nil // factorization failed
		}
		return big.NewInt(int64(p)), big.NewInt(int64(q))
	}

	p, q := SplitPQ(pq)
	if p.Cmp(q) > 0 {
		p, q = q, p
	}
	return p, q
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
	x += c
	if x >= n {
		x -= n
	}
	return x
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
			for i := uint64(0); i < r; i++ {
				y = fStep(y, c, n)
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

// SplitPQ factors a composite number pq into two prime numbers p and q, ensuring p < q.
// This is used in Diffie-Hellman key exchange, where factoring pq is required for protocol security checks.
// The function implements Pollard's rho algorithm for integer factorization, which is efficient for numbers with small factors.
func SplitPQ(pq *big.Int) (p, q *big.Int) {
	p = big.NewInt(0).Set(pq)
	q = big.NewInt(1)

	x := big.NewInt(2)
	y := big.NewInt(2)
	d := big.NewInt(1)

	for d.Cmp(big.NewInt(1)) == 0 {
		x = f(x, pq)
		y = f(f(y, pq), pq)

		temp := big.NewInt(0).Set(x)
		temp.Sub(temp, y)
		temp.Abs(temp)
		d.GCD(nil, nil, temp, pq)
	}

	p.Set(d)
	q.Div(pq, d)

	if p.Cmp(q) == 1 {
		p, q = q, p
	}

	return p, q
}

func f(x, n *big.Int) *big.Int {
	result := big.NewInt(0).Set(x)
	result.Mul(result, result)
	result.Add(result, big.NewInt(1))
	result.Mod(result, n)
	return result
}
