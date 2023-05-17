// Copyright (c) 2023 RoseLoverX

package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"hash"
	"math/big"
	"math/rand"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
)

// AuthPromt will prompt user to enter phone number or bot token to authorize client
func (c *Client) AuthPrompt() error {
	if au, _ := c.IsAuthorized(); au {
		return nil
	}
	var input string
	MAX_RETRIES := 3
	for i := 0; i < MAX_RETRIES; i++ {
		fmt.Printf("Enter phone number (with country code) or bot token: ")
		fmt.Scanln(&input)
		if input != "" {
			botTokenRegex := regexp.MustCompile(`^\d+:\w+$`)
			if botTokenRegex.MatchString(input) {
				err := c.LoginBot(input)
				if err != nil {
					return err
				}
			} else {
				phoneRegex := regexp.MustCompile(`^\+?\d+$`)
				if phoneRegex.MatchString(input) {
					_, err := c.Login(input)
					if err != nil {
						return err
					}
				} else {
					fmt.Println("Invalid input, try again")
					continue
				}
			}
			break
		} else {
			fmt.Println("Invalid response, try again")
		}
	}
	return nil
}

// Authorize client with bot token
func (c *Client) LoginBot(botToken string) error {
	if au, _ := c.IsAuthorized(); au {
		return nil
	}
	_, err := c.AuthImportBotAuthorization(1, c.AppID(), c.AppHash(), botToken)
	if err == nil {
		c.clientData.botAcc = true
	}
	if au, e := c.IsAuthorized(); !au {
		if dc, code := getErrorCode(e); code == 303 {
			err = c.switchDC(dc)
			if err != nil {
				return err
			}
			return c.LoginBot(botToken)
		}
	}
	return err
}

// sendCode and return phoneCodeHash
func (c *Client) SendCode(phoneNumber string) (hash string, err error) {
	resp, err := c.AuthSendCode(phoneNumber, c.AppID(), c.AppHash(), &CodeSettings{
		AllowAppHash:  true,
		CurrentNumber: true,
	})
	if err != nil {
		if dc, code := getErrorCode(err); code == 303 {
			err = c.switchDC(dc)
			if err != nil {
				return "", err
			}
			return c.SendCode(phoneNumber)
		}
		return "", err
	}
	return resp.PhoneCodeHash, nil
}

type LoginOptions struct {
	Password     string        `json:"password,omitempty"`
	Code         string        `json:"code,omitempty"`
	CodeHash     string        `json:"code_hash,omitempty"`
	CodeCallback func() string `json:"-"`
	FirstName    string        `json:"first_name,omitempty"`
	LastName     string        `json:"last_name,omitempty"`
}

// Authorize client with phone number, code and phone code hash,
// If phone code hash is empty, it will be requested from telegram server
func (c *Client) Login(phoneNumber string, options ...*LoginOptions) (bool, error) {
	if au, _ := c.IsAuthorized(); au {
		return true, nil
	}
	var opts *LoginOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if opts == nil {
		opts = &LoginOptions{}
	}
	var Auth AuthAuthorization
	var err error
	if opts.Code == "" {
		hash, e := c.SendCode(phoneNumber)
		if e != nil {
			return false, e
		}
		opts.CodeHash = hash
		for {
			fmt.Printf("Enter code: ")
			var codeInput string
			fmt.Scanln(&codeInput)
			if codeInput != "" {
				opts.Code = codeInput
				Auth, err = c.AuthSignIn(phoneNumber, opts.CodeHash, opts.Code, nil)
				if err == nil {
					break
				}
				if matchError(err, "The phone code entered was invalid") {
					fmt.Println("The phone code entered was invalid, please try again!")
					continue
				} else if matchError(err, "Two-steps verification is enabled") {
					var passwordInput string
					fmt.Println("Two-steps verification is enabled")
				acceptPasswordInput:
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
						if strings.Contains(err.Error(), "PASSWORD_HASH_INVALID") {
							fmt.Println("Password is incorrect, please try again!")
							goto acceptPasswordInput
						}
						return false, err
					}
					break
				} else if matchError(err, "The code is valid but no user with the given number") {
					return false, errors.New("Since Feb 2023, Telegram does not allow to create new accounts using API. Please use Telegram app to create an account and then use this library to login.")
					// c.AcceptTOS()
					// _, err = c.AuthSignUp(phoneNumber, opts.CodeHash, opts.FirstName, opts.LastName)
					// if err != nil {
					// return false, err
					// }
				} else {
					return false, err
				}
			} else if codeInput == "cancel" {
				return false, nil
			} else {
				if err, ok := err.(syscall.Errno); ok && err == syscall.EINTR {
					return false, nil
				}
				fmt.Println("Invalid code, try again")
			}
		}
	} else {
		if opts.CodeHash == "" {
			return false, errors.New("Code hash is empty, but code is not")
		}
		Auth, err = c.AuthSignIn(phoneNumber, opts.CodeHash, opts.Code, nil)
		if err != nil {
			return false, err
		}
	}
	switch auth := Auth.(type) {
	case *AuthAuthorizationSignUpRequired:
		return false, errors.New("Since Feb 2023, Telegram does not allow to create new accounts using API. Please use Telegram app to create an account and then use this library to login.")
	case *AuthAuthorizationObj:
		switch u := auth.User.(type) {
		case *UserObj:
			c.clientData.botAcc = u.Bot
			go c.Cache.UpdateUser(u)
		case *UserEmpty:
			return false, errors.New("user is empty")
		}
	case nil:
		return false, nil // doesnt mean error
	}
	return true, nil
}

func (c *Client) AcceptTOS() (bool, error) {
	tos, err := c.HelpGetTermsOfServiceUpdate()
	if err != nil {
		return false, err
	}
	switch tos := tos.(type) {
	case *HelpTermsOfServiceUpdateObj:
		fmt.Println(tos.TermsOfService.Text)
		// Accept TOS
		fmt.Println("Do you accept the TOS? (y/n)")
		var input string
		fmt.Scanln(&input)
		if input != "y" {
			return false, nil
		}
		return c.HelpAcceptTermsOfService(tos.TermsOfService.ID)
	default:
		return false, nil
	}
}

type PasswordOptions struct {
	Hint              string        `json:"hint,omitempty"`
	Email             string        `json:"email,omitempty"`
	EmailCodeCallback func() string `json:"email_code_callback,omitempty"`
}

// Edit2FA changes the 2FA password of the current user,
// if 2fa is already enabled, should provide the current password.
func (c *Client) Edit2FA(currPwd string, newPwd string, opts ...*PasswordOptions) (bool, error) {
	if currPwd == "" && newPwd == "" {
		return false, errors.New("current password and new password both cannot be empty")
	}
	opt := &PasswordOptions{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Email != "" && opt.EmailCodeCallback == nil {
		return false, errors.New("email present without email_code_callback")
	}
	pwd, err := c.AccountGetPassword()
	if err != nil {
		return false, err
	}
	b := make([]byte, 32)
	_, err = rand.Read(b)
	if err != nil {
		return false, err
	}
	// add random bytes to the end of the password
	pwd.NewAlgo.(*PasswordKdfAlgoSHA256SHA256Pbkdf2Hmacsha512Iter100000SHA256ModPow).Salt1 = append(pwd.NewAlgo.(*PasswordKdfAlgoSHA256SHA256Pbkdf2Hmacsha512Iter100000SHA256ModPow).Salt1, b...)
	if !pwd.HasPassword && currPwd != "" {
		currPwd = ""
	}
	var password InputCheckPasswordSRP
	var newPasswordHash []byte
	if currPwd != "" {
		password, err = GetInputCheckPassword(currPwd, pwd)
		if err != nil {
			return false, err
		}
	} else {
		password = &InputCheckPasswordEmpty{}
	}
	if newPwd != "" {
		newPasswordHash = computeDigest(pwd.NewAlgo.(*PasswordKdfAlgoSHA256SHA256Pbkdf2Hmacsha512Iter100000SHA256ModPow), newPwd)
	} else {
		newPasswordHash = []byte{}
	}
	_, err = c.AccountUpdatePasswordSettings(password, &AccountPasswordInputSettings{
		NewAlgo:           pwd.NewAlgo,
		NewPasswordHash:   newPasswordHash,
		Hint:              opt.Hint,
		Email:             opt.Email,
		NewSecureSettings: &SecureSecretSettings{},
	})
	if err != nil {
		if matchError(err, "Email unconfirmed") {
			if opt.EmailCodeCallback == nil {
				return false, errors.New("email_code_callback is nil")
			}
			code := opt.EmailCodeCallback()
			_, err = c.AccountConfirmPasswordEmail(code)
			if err != nil {
				return false, err
			}
		} else {
			return false, err
		}
	}
	return true, nil
}

type (
	QrToken struct {
		// Token is the token to be used for logging in
		Token []byte
		// URL is the URL to be used for logging in
		Url string
		// ExpiresIn is the time in seconds after which the token will expire
		ExpiresIn int32
		// IgnoredIDs are the IDs of the users that will be ignored
		IgnoredIDs []int64
		// client is the client to be used for logging in
		client *Client
		// Timeout is the time after which the token will be considered expired
		Timeout int32
	}
)

func (q *QrToken) URL() string {
	return q.Url
}

func (q *QrToken) TOKEN() []byte {
	return q.Token
}

func (q *QrToken) Expires() int32 {
	return q.ExpiresIn
}

func (q *QrToken) Recreate() (*QrToken, error) {
	q, err := q.client.QRLogin(q.IgnoredIDs...)
	return q, err
}

func (q *QrToken) Wait(timeout ...int32) error {
	const def int32 = 600 // 10 minutes
	q.Timeout = getVariadic(timeout, def).(int32)
	ch := make(chan int)
	ev := q.client.AddRawHandler(&UpdateLoginToken{}, func(update Update) error {
		ch <- 1
		return nil
	})
	select {
	case <-ch:
		go ev.Remove()
		resp, err := q.client.AuthExportLoginToken(q.client.AppID(), q.client.AppHash(), q.IgnoredIDs)
		if err != nil {
			return err
		}
	QrResponseSwitch:
		switch req := resp.(type) {
		case *AuthLoginTokenMigrateTo:
			q.client.switchDC(int(req.DcID))
			resp, err = q.client.AuthImportLoginToken(req.Token)
			if err != nil {
				return err
			}
			goto QrResponseSwitch
		case *AuthLoginTokenSuccess:
			switch u := req.Authorization.(type) {
			case *AuthAuthorizationObj:
				switch u := u.User.(type) {
				case *UserObj:
					// q.client.bot = u.Bot
					q.client.Cache.UpdateUser(u)
				case *UserEmpty:
					return errors.New("authorization user is empty")
				}
			}
		}
		return nil
	case <-time.After(time.Duration(q.Timeout) * time.Second):
		go ev.Remove()
		return errors.New("qr login timed out")
	}
}

func (c *Client) QRLogin(IgnoreIDs ...int64) (*QrToken, error) {
	// Get QR code
	var ignoreIDs []int64
	ignoreIDs = append(ignoreIDs, IgnoreIDs...)
	qr, err := c.AuthExportLoginToken(c.AppID(), c.AppHash(), ignoreIDs)
	if err != nil {
		return nil, err
	}
	var (
		qrToken   []byte
		expiresIn int32 = 60
	)
	switch qr := qr.(type) {
	case *AuthLoginTokenMigrateTo:
		c.switchDC(int(qr.DcID))
		return c.QRLogin(IgnoreIDs...)
	case *AuthLoginTokenObj:
		qrToken = qr.Token
		expiresIn = qr.Expires
	}
	// Get QR code URL
	qrURL := base64.RawURLEncoding.EncodeToString(qrToken)
	return &QrToken{
		Token:      qrToken,
		Url:        fmt.Sprintf("tg://login?token=%s", qrURL),
		ExpiresIn:  expiresIn,
		client:     c,
		IgnoredIDs: ignoreIDs,
	}, nil
}

// Logs out from the current account
func (c *Client) LogOut() error {
	_, err := c.AuthLogOut()
	// c.bot = false
	c.MTProto.DeleteSession()
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

func getInputCheckPassword(password string, srpB []byte, mp *ModPow, random []byte) (*SrpAnswer, error) {
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
	a := bytesToBig(random)
	ga := pad256(bigExp(g, a, p).Bytes())
	gb := pad256(srpB)
	u := bytesToBig(calcSHA256(ga, gb))
	x := bytesToBig(passwordHash2([]byte(password), mp.Salt1, mp.Salt2))
	v := bigExp(g, x, p)
	k := bytesToBig(calcSHA256(mp.P, gBytes))
	kv := k.Mul(k, v).Mod(k, p)
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

func dhHandshakeCheckConfigIsError(_ int32, _ []byte) bool {
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

func computeDigest(algo *PasswordKdfAlgoSHA256SHA256Pbkdf2Hmacsha512Iter100000SHA256ModPow, password string) []byte {
	hash := passwordHash2([]byte(password), algo.Salt1, algo.Salt2)
	value := bigExp(big.NewInt(int64(algo.G)), bytesToBig(hash), bytesToBig(algo.P))
	return pad256(value.Bytes())
}
