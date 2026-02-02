// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
)

// ConnectBot connects to telegram using bot token
func (c *Client) ConnectBot(botToken string) error {
	if err := c.Connect(); err != nil {
		return err
	}
	return c.LoginBot(botToken)
}

var (
	botTokenRegex = regexp.MustCompile(`^\d+:[\w\d_-]+$`)
	phoneRegex    = regexp.MustCompile(`^\+?\d+$`)
)

// AuthPromt will prompt user to enter phone number or bot token to authorize client
func (c *Client) AuthPrompt() error {
	if au, _ := c.IsAuthorized(); au {
		return nil
	}
	var input string
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		fmt.Printf("Enter phone number (with country code [+42xxx]) or bot token: ")
		fmt.Scanln(&input)
		if input != "" {
			if botTokenRegex.MatchString(input) {
				err := c.LoginBot(input)
				if err != nil {
					return err
				}
			} else {
				if phoneRegex.MatchString(strings.TrimSpace(input)) {
					_, err := c.Login(input)
					if err != nil {
						return err
					}
				} else {
					fmt.Println("The input is not a valid phone number or bot token, try again [", i+1, "/", maxRetries, "]")
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
	if !c.IsConnected() {
		if err := c.Connect(); err != nil {
			return err
		}
	}
	if au, _ := c.IsAuthorized(); au {
		return nil
	}

	_, err := c.AuthImportBotAuthorization(1, c.AppID(), c.AppHash(), botToken)
	if err == nil {
		c.clientData.botAcc = true
	}
	if au, e := c.IsAuthorized(); !au {
		if dc, code := GetErrorCode(e); code == 303 {
			err = c.SwitchDc(dc)
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
		if strings.Contains(err.Error(), "CONNECTION_NOT_INITED") {
			c.InitialRequest()
			return c.SendCode(phoneNumber)
		}

		if dc, code := GetErrorCode(err); code == 303 {
			err = c.SwitchDc(dc)
			if err != nil {
				return "", err
			}
			return c.SendCode(phoneNumber)
		}
		return "", err
	}
	switch resp := resp.(type) {
	case *AuthSentCodeObj:
		return resp.PhoneCodeHash, nil
	default:
		return "", nil
	}
}

type LoginOptions struct {
	Password         string `json:"password,omitempty"`
	Code             string `json:"code,omitempty"`
	CodeHash         string `json:"code_hash,omitempty"`
	CodeCallback     func() (string, error)
	PasswordCallback func() (string, error)
	FirstName        string `json:"first_name,omitempty"`
	LastName         string `json:"last_name,omitempty"`
}

// Authorize client with phone number, code and phone code hash,
// If phone code hash is empty, it will be requested from telegram server
func (c *Client) Login(phoneNumber string, options ...*LoginOptions) (bool, error) {
	if !c.IsConnected() {
		if err := c.Connect(); err != nil {
			return false, err
		}
	}
	if au, _ := c.IsAuthorized(); au {
		return true, nil
	}

	var opts = getVariadic(options, &LoginOptions{})

	var auth AuthAuthorization
	var err error
	if opts.Code == "" {
		hash, e := c.SendCode(phoneNumber)
		if e != nil {
			return false, e
		}
		opts.CodeHash = hash

		if opts.CodeCallback == nil {
			opts.CodeCallback = func() (string, error) {
				fmt.Printf("Enter code: ")
				var codeInput string
				fmt.Scanln(&codeInput)
				return codeInput, nil
			}
		}
		if opts.PasswordCallback == nil {
			opts.PasswordCallback = func() (string, error) {
				fmt.Printf("Two-steps verification is enabled\n")
				fmt.Printf("Enter password: ")
				var passwordInput string
				fmt.Scanln(&passwordInput)
				return passwordInput, nil
			}
		}

		auth, err = codeAuthAttempt(c, phoneNumber, opts)
		if err != nil {
			return false, err
		}
	} else {
		if opts.CodeHash == "" {
			return false, errors.New("Code hash is empty, but code is not")
		}
		auth, err = c.AuthSignIn(phoneNumber, opts.CodeHash, opts.Code, nil)
		if err != nil {
			return false, err
		}
	}

	c.SaveSession(true)
	switch auth := auth.(type) {
	case *AuthAuthorizationSignUpRequired:
		return false, errors.New("SignUp using official Telegram app is required")
	case *AuthAuthorizationObj:
		switch u := auth.User.(type) {
		case *UserObj:
			c.clientData.botAcc = u.Bot
			go c.Cache.UpdateUser(u)
		case *UserEmpty:
			return false, errors.New("user is empty, authorization failed")
		}
	case nil:
		return false, nil // need not mean error
	}
	return true, nil
}

func codeAuthAttempt(c *Client, phoneNumber string, opts *LoginOptions) (AuthAuthorization, error) {
	var err error

	for {
		opts.Code, err = opts.CodeCallback()

		if opts.Code == "cancel" || opts.Code == "exit" {
			return nil, errors.New("login canceled")
		} else if opts.Code != "" {
			authResp, err := c.AuthSignIn(phoneNumber, opts.CodeHash, opts.Code, nil)
			if err == nil {
				return authResp, nil
			}

			if MatchError(err, "PHONE_CODE_INVALID") {
				c.Log.Error(errors.Wrap(err, "invalid phone code"))
				continue
			} else if MatchError(err, "SESSION_PASSWORD_NEEDED") {
			acceptPasswordInput:
				if opts.Password == "" {
					for {
						passwordInput, err := opts.PasswordCallback()
						if err != nil {
							return nil, err
						}

						if passwordInput != "" {
							opts.Password = passwordInput
							break
						} else if passwordInput == "cancel" || passwordInput == "exit" {
							return nil, errors.New("login canceled")
						} else {
							fmt.Println("Invalid password, try again")
						}
					}
				}

				accPassword, err := c.AccountGetPassword()
				if err != nil {
					return nil, err
				}

				inputPassword, err := GetInputCheckPassword(opts.Password, accPassword)
				if err != nil {
					return nil, err
				}

				_, err = c.AuthCheckPassword(inputPassword)
				if err != nil {
					if strings.Contains(err.Error(), "PASSWORD_HASH_INVALID") {
						opts.Password = ""
						fmt.Println("password is incorrect, please try again!")
						goto acceptPasswordInput
					}
					return nil, err
				}
				break
			} else if MatchError(err, "The code is valid but no user with the given number") {
				return nil, errors.New("SignUp using official Telegram app is required")
			} else {
				return nil, err
			}
		} else {
			if err, ok := err.(syscall.Errno); ok && err == syscall.EINTR {
				return nil, errors.New("login canceled")
			}
			fmt.Println("Invalid code, try again")
		}
	}

	return nil, nil
}

type ScrapeConfig struct {
	PhoneNum          string
	WebCodeCallback   func() (string, error)
	CreateIfNotExists bool
}

func (c *Client) ScrapeAppConfig(config ...*ScrapeConfig) (int32, string, bool, error) {
	var conf = getVariadic(config, &ScrapeConfig{
		CreateIfNotExists: true,
	})

	if conf.PhoneNum == "" {
		fmt.Printf("Enter phone number (with country code [+1xxx]): ")
		fmt.Scanln(&conf.PhoneNum)
	}

	if conf.WebCodeCallback == nil {
		conf.WebCodeCallback = func() (string, error) {
			fmt.Printf("Enter received web login code: ")
			var password string
			fmt.Scanln(&password)
			return password, nil
		}
	}

	customClient := &http.Client{
		Timeout: time.Second * 10,
	}

	reqCode, err := http.NewRequest("POST", "https://my.telegram.org/auth/send_password", strings.NewReader("phone="+url.QueryEscape(conf.PhoneNum)))
	if err != nil {
		return 0, "", false, err
	}

	reqCode.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	respCode, err := customClient.Do(reqCode)
	if err != nil || respCode.StatusCode != 200 {
		return 0, "", false, err
	}

	var result struct {
		RandomHash string `json:"random_hash"`
	}

	if err := json.NewDecoder(respCode.Body).Decode(&result); err != nil {
		return 0, "", false, errors.Wrap(err, "Too many requests, try again later")
	}

	code, err := conf.WebCodeCallback()
	if err != nil {
		return 0, "", false, err
	}

	reqLogin, err := http.NewRequest("POST", "https://my.telegram.org/auth/login", strings.NewReader("phone="+url.QueryEscape(conf.PhoneNum)+"&random_hash="+result.RandomHash+"&password="+url.QueryEscape(code)))
	if err != nil {
		return 0, "", false, err
	}

	reqLogin.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	respLogin, err := customClient.Do(reqLogin)
	if err != nil || respLogin.StatusCode != 200 {
		return 0, "", false, err
	}

	cookies := respLogin.Cookies()
	ALREDY_TRIED_CREATION := false

BackToAppsPage:
	reqScrape, err := http.NewRequest("GET", "https://my.telegram.org/apps", nil)
	if err != nil {
		return 0, "", false, err
	}

	for _, cookie := range cookies {
		reqScrape.AddCookie(cookie)
	}

	respScrape, err := customClient.Do(reqScrape)
	if err != nil || respScrape.StatusCode != 200 {
		return 0, "", false, err
	}

	body, err := io.ReadAll(respScrape.Body)
	if err != nil {
		return 0, "", false, err
	}

	appIDRegex := regexp.MustCompile(`<label for="app_id".*?>.*?</label>\s*<div.*?>\s*<span.*?><strong>(\d+)</strong></span>`)
	appHashRegex := regexp.MustCompile(`<label for="app_hash".*?>.*?</label>\s*<div.*?>\s*<span.*?>([a-fA-F0-9]+)</span>`)

	appID := appIDRegex.FindStringSubmatch(string(body))
	appHash := appHashRegex.FindStringSubmatch(string(body))

	if len(appID) < 2 || len(appHash) < 2 || strings.Contains(string(body), "Create new application") && !ALREDY_TRIED_CREATION {
		ALREDY_TRIED_CREATION = true
		// assume app is not created, create app
		hiddenHashRegex := regexp.MustCompile(`<input type="hidden" name="hash" value="([a-fA-F0-9]+)"\/>`)
		hiddenHash := hiddenHashRegex.FindStringSubmatch(string(body))
		if len(hiddenHash) < 2 {
			return 0, "", false, errors.New("creation hash not found, try manual creation")
		}

		appRandomSuffix := make([]byte, 8)
		_, err := rand.Read(appRandomSuffix)

		if err != nil {
			return 0, "", false, err
		}

		reqCreateApp, err := http.NewRequest("POST", "https://my.telegram.org/apps/create", strings.NewReader("hash="+hiddenHash[1]+"&app_title=AppForGogram"+string(appRandomSuffix)+"&app_shortname=gogramapp"+string(appRandomSuffix)+"&app_platform=android&app_url=https%3A%2F%2Fgogram.vercel.app&app_desc=ForGoGram"+string(appRandomSuffix)))
		if err != nil {
			return 0, "", false, err
		}

		reqCreateApp.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		for _, cookie := range cookies {
			reqCreateApp.AddCookie(cookie)
		}

		respCreateApp, err := customClient.Do(reqCreateApp)
		if err != nil || respCreateApp.StatusCode != 200 {
			return 0, "", false, err
		}

		goto BackToAppsPage
	}

	appIdNum, err := strconv.Atoi(appID[1])
	if err != nil {
		return 0, "", false, err
	}

	if appIdNum > math.MaxInt32 || appIdNum < math.MinInt32 {
		return 0, "", false, errors.New("app id is out of range")
	}

	return int32(appIdNum), appHash[1], true, nil
}

func (c *Client) AcceptTOS() (bool, error) {
	tos, err := c.HelpGetTermsOfServiceUpdate()
	if err != nil {
		return false, err
	}
	switch tos := tos.(type) {
	case *HelpTermsOfServiceUpdateObj:
		fmt.Println(tos.TermsOfService.Text)
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
func (c *Client) Edit2FA(currPwd, newPwd string, opts ...*PasswordOptions) (bool, error) {
	if currPwd == "" && newPwd == "" {
		return false, errors.New("current password and new password both cannot be empty")
	}
	opt := &PasswordOptions{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Email != "" && opt.EmailCodeCallback == nil {
		opt.EmailCodeCallback = func() string {
			fmt.Printf("Enter received email code: ")
			var code string
			fmt.Scanln(&code)
			return code
		}
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
		newPasswordHash = ComputeDigest(pwd.NewAlgo.(*PasswordKdfAlgoSHA256SHA256Pbkdf2Hmacsha512Iter100000SHA256ModPow), newPwd)
	} else {
		newPasswordHash = []byte{}
	}

	_, err = c.AccountUpdatePasswordSettings(password, &AccountPasswordInputSettings{
		NewAlgo:         pwd.NewAlgo,
		NewPasswordHash: newPasswordHash,
		Hint:            getValue(opt.Hint, "no-hint"),
		Email:           opt.Email,
	})

	if err != nil {
		if MatchError(err, "EMAIL_UNCONFIRMED") {
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
	q.Timeout = getVariadic(timeout, def)
	ch := make(chan int)
	ev := q.client.AddRawHandler(&UpdateLoginToken{}, func(update Update, client *Client) error {
		ch <- 1
		return nil
	})
	select {
	case <-ch:
		go q.client.removeHandle(ev)
		resp, err := q.client.AuthExportLoginToken(q.client.AppID(), q.client.AppHash(), q.IgnoredIDs)
		if err != nil {
			return err
		}
	QrResponseSwitch:
		switch req := resp.(type) {
		case *AuthLoginTokenMigrateTo:
			q.client.SwitchDc(int(req.DcID))
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
					q.client.Cache.UpdateUser(u)
				case *UserEmpty:
					return errors.New("authorization user is empty")
				}
			}
		}
		return nil
	case <-time.After(time.Duration(q.Timeout) * time.Second):
		go q.client.removeHandle(ev)
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
		c.SwitchDc(int(qr.DcID))
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
