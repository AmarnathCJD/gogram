package ige

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"math/big"

	"errors"
)

func GetInputCheckPassword(password string, srpB []byte, mp *ModPow, random []byte) (*SrpAnswer, error) {
	if password == "" {
		return nil, nil
	}
	err := validateCurrentAlgo(srpB, mp)
	if err != nil {
		return nil, fmt.Errorf("validating CurrentAlgo: %w", err)
	}
	p := BytesToBig(mp.P)
	g := big.NewInt(int64(mp.G))
	gBytes := Pad256(g.Bytes())
	secret := RandomBytes(256)
	for i := 0; i < len(secret) && i < len(random); i++ {
		secret[i] ^= random[i]
	}
	a := BytesToBig(secret)
	ga := Pad256(BigExp(g, a, p).Bytes())
	if !IsSafePublic(BytesToBig(ga), p) {
		return nil, errors.New("unsafe SRP public value")
	}
	gb := Pad256(srpB)
	u := BytesToBig(calcSHA256(ga, gb))
	if u.Sign() == 0 {
		return nil, errors.New("invalid SRP scrambling parameter")
	}
	x := BytesToBig(PasswordHash2([]byte(password), mp.Salt1, mp.Salt2))
	v := BigExp(g, x, p)
	k := BytesToBig(calcSHA256(mp.P, gBytes))
	kv := k.Mul(k, v).Mod(k, p)
	t := BytesToBig(srpB)
	if t.Sub(t, kv).Cmp(big.NewInt(0)) == -1 {
		t.Add(t, p)
	}
	if !IsSafePublic(t, p) {
		return nil, errors.New("unsafe SRP server value")
	}

	sa := Pad256(BigExp(t, u.Mul(u, x).Add(u, a), p).Bytes())

	ka := calcSHA256(sa)

	M1 := calcSHA256(
		BytesXor(calcSHA256(mp.P), calcSHA256(gBytes)),
		calcSHA256(mp.Salt1),
		calcSHA256(mp.Salt2),
		ga,
		gb,
		ka,
	)

	return &SrpAnswer{
		GA: ga,
		M1: M1,
	}, nil
}

type ModPow struct {
	Salt1 []byte
	Salt2 []byte
	G     int32
	P     []byte
}

type SrpAnswer struct {
	GA []byte
	M1 []byte
}

func validateCurrentAlgo(srpB []byte, mp *ModPow) error {
	if mp == nil {
		return errors.New("missing password algorithm")
	}
	if len(mp.P) != 256 {
		return errors.New("invalid SRP prime length")
	}
	if dhHandshakeCheckConfigIsError(mp.G, mp.P) {
		return errors.New("receive invalid config g")
	}

	p := BytesToBig(mp.P)
	gb := BytesToBig(srpB)

	if big.NewInt(0).Cmp(gb) != -1 || gb.Cmp(p) != -1 || len(srpB) < 248 || len(srpB) > 256 {
		return errors.New("receive invalid value of B")
	}

	return nil
}

func saltingHashing(data, salt []byte) []byte {
	return calcSHA256(salt, data, salt)
}

func passwordHash1(password, salt1, salt2 []byte) []byte {
	return saltingHashing(saltingHashing(password, salt1), salt2)
}

func PasswordHash2(password, salt1, salt2 []byte) []byte {
	return saltingHashing(pbkdf2sha512(passwordHash1(password, salt1, salt2), salt1, 100000), salt2)
}

func pbkdf2sha512(hash1, salt1 []byte, i int) []byte {
	return AlgoKey(hash1, salt1, i, 64, sha512.New)
}

func Pad256(b []byte) []byte {
	if len(b) >= 256 {
		return b[len(b)-256:]
	}

	tmp := make([]byte, 256)
	copy(tmp[256-len(b):], b)

	return tmp
}

func calcSHA256(arrays ...[]byte) []byte {
	h := sha256.New()
	for _, arr := range arrays {
		h.Write(arr)
	}
	return h.Sum(nil)
}

func BytesToBig(b []byte) *big.Int {
	return new(big.Int).SetBytes(b)
}

func BigExp(x, y, m *big.Int) *big.Int {
	return new(big.Int).Exp(x, y, m)
}

func dhHandshakeCheckConfigIsError(g int32, p []byte) bool {
	return !IsSafePrime(new(big.Int).SetBytes(p), g)
}

func AlgoKey(password, salt []byte, iter, keyLen int, h func() hash.Hash) []byte {
	prf := hmac.New(h, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen

	var buf [4]byte
	dk := make([]byte, 0, numBlocks*hashLen)
	U := make([]byte, hashLen)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)
		prf.Write(buf[:4])
		dk = prf.Sum(dk)
		T := dk[len(dk)-hashLen:]
		copy(U, T)

		for n := 2; n <= iter; n++ {
			prf.Reset()
			prf.Write(U)
			U = U[:0]
			U = prf.Sum(U)
			for x := range U {
				T[x] ^= U[x]
			}
		}
	}
	return dk[:keyLen]
}

func BytesXor(a, b []byte) []byte {
	res := make([]byte, len(a))
	copy(res, a)
	for i := range res {
		res[i] ^= b[i]
	}
	return res
}

func RandomBytes(size int) []byte {
	b := make([]byte, size)
	_, _ = rand.Read(b)
	return b
}

const knownDHPrimeHex = "C71CAEB9C6B1C9048E6C522F70F13F73980D40238E3E21C14934D037563D930F" +
	"48198A0AA7C14058229493D22530F4DBFA336F6E0AC925139543AED44CCE7C37" +
	"20FD51F69458705AC68CD4FE6B6B13ABDC9746512969328454F18FAF8C595F64" +
	"2477FE96BB2A941D5BCD1D4AC8CC49880708FA9B378E3C4F3A9060BEE67CF9A4" +
	"A4A695811051907E162753B56B0F6B410DBA74D8A84B2A14B3144E0EF1284754" +
	"FD17ED950D5965B4B9DD46582DB1178D169C6BC465B0D6FF9CA3928FEF5B9AE4" +
	"E418FC15E83EBEA0F87FA9FF5EED70050DED2849F47BF959D956850CE929851F" +
	"0D8115F635B105EE2E4E15D04B2454BF6F4FADF034B10403119CD8E3B92FCC5B"

var knownDHPrime, _ = new(big.Int).SetString(knownDHPrimeHex, 16)

func IsSafePrime(p *big.Int, g int32) bool {
	if p == nil || p.BitLen() != 2048 || g < 2 || g > 7 {
		return false
	}
	mod := func(n int64) int64 { return new(big.Int).Mod(p, big.NewInt(n)).Int64() }
	switch g {
	case 2:
		if mod(8) != 7 {
			return false
		}
	case 3:
		if mod(3) != 2 {
			return false
		}
	case 5:
		if r := mod(5); r != 1 && r != 4 {
			return false
		}
	case 6:
		if r := mod(24); r != 19 && r != 23 {
			return false
		}
	case 7:
		if r := mod(7); r != 3 && r != 5 && r != 6 {
			return false
		}
	}
	if p.Cmp(knownDHPrime) == 0 {
		return true
	}
	if !p.ProbablyPrime(64) {
		return false
	}
	q := new(big.Int).Sub(p, big.NewInt(1))
	return q.Rsh(q, 1).ProbablyPrime(64)
}

func IsSafePublic(value, prime *big.Int) bool {
	if value == nil || prime == nil || prime.BitLen() != 2048 {
		return false
	}
	lower := new(big.Int).Lsh(big.NewInt(1), 1984)
	upper := new(big.Int).Sub(prime, lower)
	return value.Cmp(lower) > 0 && value.Cmp(upper) < 0
}
