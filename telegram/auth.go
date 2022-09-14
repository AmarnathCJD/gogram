// Copyright (c) 2022 RoseLoverX

package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"math/big"
	"math/rand"
	"strings"

	"github.com/pkg/errors"
)

const (
	randombyteLen = 256 // 2048 bit
)

// Authorize client with bot token
func (c *Client) LoginBot(botToken string) error {
	_, err := c.AuthImportBotAuthorization(1, c.AppID, c.ApiHash, botToken)
	if err == nil {
		c.bot = true
	}
	return err
}

// sendCode and return phoneCodeHash
func (c *Client) SendCode(phoneNumber string) (hash string, err error) {
	resp, err := c.AuthSendCode(phoneNumber, c.AppID, c.ApiHash, &CodeSettings{
		AllowAppHash:  true,
		CurrentNumber: true,
	})
	if err != nil {
		return "", err
	}
	return resp.PhoneCodeHash, nil
}

// Authorize client with phone number, code and phone code hash,
// If phone code hash is empty, it will be requested from telegram server
func (c *Client) Login(phoneNumber string, options ...*LoginOptions) (bool, error) {
	var opts *LoginOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if opts == nil {
		opts = &LoginOptions{}
	}
	if opts.Code == "" {
		hash, err := c.SendCode(phoneNumber)
		for {
			if err != nil {
				return false, err
			}
			fmt.Printf("Enter code: ")
			var codeInput string
			fmt.Scanln(&codeInput)
			if codeInput != "" {
				opts.Code = codeInput
				break
			} else if codeInput == "cancel" {
				return false, nil
			} else {
				fmt.Println("Invalid code, try again")
			}
		}
		opts.CodeHash = hash
	}
	_, SignInerr := c.AuthSignIn(phoneNumber, opts.CodeHash, opts.Code, nil)
	if SignInerr != nil {
		if strings.Contains(SignInerr.Error(), "Two-steps verification is enabled") {
			var passwordInput string
			fmt.Println("Two-step verification is enabled")
			for {
				fmt.Printf("Enter password: ")
				fmt.Scanln(&passwordInput)
				if passwordInput != "" {
					opts.Password = passwordInput
					break
				} else if passwordInput == "cancel" {
					return false, nil
				} else {
					fmt.Println("Invalid password, try again")
				}
			}
			AccPassword, err := c.AccountGetPassword()
			if err != nil {
				return false, err
			}
			inputPassword, err := GetInputCheckPassword(passwordInput, AccPassword)
			if err != nil {
				return false, err
			}
			_, err = c.AuthCheckPassword(inputPassword)
			if err != nil {
				return false, err
			}
			return true, nil
		} else if strings.Contains(SignInerr.Error(), "The code is valid but no user with the given number") {
			_, err := c.AuthSignUp(phoneNumber, opts.CodeHash, opts.FirstName, opts.LastName)
			if err != nil {
				return false, err
			}
			return true, nil
		} else {
			return false, SignInerr
		}
	}
	return true, nil
}

// Logs out from the current account
func (c *Client) LogOut() error {
	_, err := c.AuthLogOut()
	return err
}

// GetInputCheckPassword returns InputCheckPasswordSRP

func RandomBytes(size int) []byte {
	b := make([]byte, size)
	_, _ = rand.Read(b)
	return b
}

// GetInputCheckPassword returns the input check password for the given password and salt.
// https://core.telegram.org/api/srp#checking-the-password-with-srp
func GetInputCheckPasswordAlgo(password string, srpB []byte, mp *ModPow) (*SrpAnswer, error) {
	return getInputCheckPassword(password, srpB, mp, RandomBytes(randombyteLen))
}

func getInputCheckPassword(
	password string,
	srpB []byte,
	mp *ModPow,
	random []byte,
) (
	*SrpAnswer, error,
) {
	if password == "" {
		return nil, nil
	}

	err := validateCurrentAlgo(srpB, mp)
	if err != nil {
		return nil, errors.Wrap(err, "validating CurrentAlgo")
	}

	p := bytesToBig(mp.P)
	g := big.NewInt(int64(mp.G))
	gBytes := pad256(g.Bytes())

	// random 2048-bit number a
	a := bytesToBig(random)

	// g_a = pow(g, a) mod p
	ga := pad256(bigExp(g, a, p).Bytes())

	// g_b = srp_B
	gb := pad256(srpB)

	// u = H(g_a | g_b)
	u := bytesToBig(calcSHA256(ga, gb))

	// x = PH2(password, salt1, salt2)
	x := bytesToBig(passwordHash2([]byte(password), mp.Salt1, mp.Salt2))

	// v = pow(g, x) mod p
	v := bigExp(g, x, p)

	// k = (k * v) mod p
	k := bytesToBig(calcSHA256(mp.P, gBytes))

	// k_v = (k * v) % p
	kv := k.Mul(k, v).Mod(k, p)

	// t = (g_b - k_v) % p
	t := bytesToBig(srpB)
	if t.Sub(t, kv).Cmp(big.NewInt(0)) == -1 {
		t.Add(t, p)
	}

	sa := pad256(bigExp(t, u.Mul(u, x).Add(u, a), p).Bytes())

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
	if dhHandshakeCheckConfigIsError(mp.G, mp.P) {
		return errors.New("receive invalid config g")
	}

	p := bytesToBig(mp.P)
	gb := bytesToBig(srpB)

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

func passwordHash2(password, salt1, salt2 []byte) []byte {
	return saltingHashing(pbkdf2sha512(passwordHash1(password, salt1, salt2), salt1, 100000), salt2)
}

func pbkdf2sha512(hash1 []byte, salt1 []byte, i int) []byte {
	return AlgoKey(hash1, salt1, i, 64, sha512.New)
}

func pad256(b []byte) []byte {
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

func bytesToBig(b []byte) *big.Int {
	return new(big.Int).SetBytes(b)
}

func bigExp(x, y, m *big.Int) *big.Int {
	return new(big.Int).Exp(x, y, m)
}

func dhHandshakeCheckConfigIsError(gInt int32, primeStr []byte) bool {
	return false
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
