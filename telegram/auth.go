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

	"errors"

	"github.com/amarnathcjd/gogram/internal/utils"
)

var (
	botTokenRegex = regexp.MustCompile(`^\d{8,10}:[A-Za-z0-9_-]{35}$`)
	phoneRegex    = regexp.MustCompile(`^\+?[1-9]\d{6,14}$`)
)

// ConnectBot connects to telegram using bot token
func (c *Client) ConnectBot(botToken string) error {
	if err := c.Connect(); err != nil {
		return err
	}
	return c.LoginBot(botToken)
}

// AuthPrompt prompts the user to enter a phone number or bot token to authorize the client.
func (c *Client) AuthPrompt() error {
	if authorized, _ := c.IsAuthorized(); authorized {
		return nil
	}

	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Printf("Enter phone number (with country code, e.g., +1234567890) or bot token: ")
		var input string
		if _, err := fmt.Scanln(&input); err != nil {
			return fmt.Errorf("reading input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Printf("Empty input received. Please try again (%d/%d)\n", attempt, maxRetries)
			continue
		}

		if botTokenRegex.MatchString(input) {
			if err := c.LoginBot(input); err != nil {
				return fmt.Errorf("bot login failed: %w", err)
			}
			return nil
		}

		if phoneRegex.MatchString(input) {
			if _, err := c.Login(input); err != nil {
				return fmt.Errorf("user login failed: %w", err)
			}
			return nil
		}

		fmt.Printf("Invalid format. Expected phone number or bot token (%d/%d)\n", attempt, maxRetries)
	}

	return errors.New("authentication failed: maximum retry attempts exceeded")
}

// LoginBot authenticates the client using a bot token.
func (c *Client) LoginBot(botToken string) error {
	if botToken == "" {
		return errors.New("bot token cannot be empty")
	}

	if !c.IsConnected() {
		if err := c.Connect(); err != nil {
			return fmt.Errorf("establishing connection: %w", err)
		}
	}

	if authorized, _ := c.IsAuthorized(); authorized {
		return nil
	}

	_, err := c.AuthImportBotAuthorization(1, c.AppID(), c.AppHash(), botToken)
	if err != nil {
		if MatchError(err, "DC_MIGRATE") {
			return c.LoginBot(botToken)
		}
		return fmt.Errorf("importing bot authorization: %w", err)
	}

	if is, err := c.IsAuthorized(); err != nil || !is {
		if MatchError(err, "DC_MIGRATE") {
			return c.LoginBot(botToken)
		}
	}

	c.clientData.botAcc = true
	return nil
}

// SendCode sends an authentication code to the specified phone number and returns the phone code hash.
func (c *Client) SendCode(phoneNumber string) (string, error) {
	if phoneNumber == "" {
		return "", errors.New("phone number cannot be empty")
	}

	resp, err := c.AuthSendCode(phoneNumber, c.AppID(), c.AppHash(), &CodeSettings{
		AllowAppHash:  true,
		CurrentNumber: true,
	})

	if err != nil {
		if MatchError(err, "CONNECTION_NOT_INITED") {
			if err := c.InitialRequest(); err != nil {
				return "", fmt.Errorf("initializing connection: %w", err)
			}
			return c.SendCode(phoneNumber)
		}

		if MatchError(err, "DC_MIGRATE") {
			return c.SendCode(phoneNumber)
		}

		return "", fmt.Errorf("sending code to %s: %w", phoneNumber, err)
	}

	switch resp := resp.(type) {
	case *AuthSentCodeObj:
		return resp.PhoneCodeHash, nil
	default:
		return "", fmt.Errorf("unexpected response type: %T", resp)
	}
}

type LoginOptions struct {
	Password         string `json:"password,omitempty"`
	Code             string `json:"code,omitempty"`
	CodeHash         string `json:"code_hash,omitempty"`
	MaxRetries       int    `json:"max_retries,omitempty"`
	CodeCallback     func() (string, error)
	PasswordCallback func() (string, error)
	FirstName        string `json:"first_name,omitempty"`
	LastName         string `json:"last_name,omitempty"`
}

// Login authenticates the client using a phone number.
//
// If code is not provided in options, it will be requested via callback.
func (c *Client) Login(phoneNumber string, options ...*LoginOptions) (bool, error) {
	if phoneNumber == "" {
		return false, errors.New("phone number cannot be empty")
	}

	if !c.IsConnected() {
		if err := c.Connect(); err != nil {
			return false, fmt.Errorf("establishing connection: %w", err)
		}
	}

	if authorized, _ := c.IsAuthorized(); authorized {
		return true, nil
	}

	opts := getVariadic(options, &LoginOptions{})

	if opts.CodeCallback == nil {
		opts.CodeCallback = func() (string, error) {
			fmt.Printf("Enter authentication code: ")
			var code string
			if _, err := fmt.Scanln(&code); err != nil {
				return "", fmt.Errorf("reading code: %w", err)
			}
			return strings.TrimSpace(code), nil
		}
	}

	if opts.PasswordCallback == nil {
		opts.PasswordCallback = func() (string, error) {
			fmt.Printf("Enter password: ")
			var password string
			if _, err := fmt.Scanln(&password); err != nil {
				return "", fmt.Errorf("reading password: %w", err)
			}
			return strings.TrimSpace(password), nil
		}
	}

	var auth AuthAuthorization
	var err error

	if opts.Code == "" {
		hash, err := c.SendCode(phoneNumber)
		if err != nil {
			return false, fmt.Errorf("sending verification code: %w", err)
		}
		opts.CodeHash = hash

		maxRetries := getValue(opts.MaxRetries, 3)
		auth, err = CodeAuthAttempt(c, phoneNumber, opts, maxRetries)
		if err != nil {
			return false, fmt.Errorf("authentication attempt failed: %w", err)
		}
	} else {
		if opts.CodeHash == "" {
			return false, errors.New("code hash is required when code is provided")
		}

		auth, err = c.AuthSignIn(phoneNumber, opts.CodeHash, opts.Code, nil)
		if err != nil {
			return false, fmt.Errorf("signing in: %w", err)
		}
	}

	if err := c.SaveSession(true); err != nil {
		c.Log.WithError(err).Warn("failed to save session")
	}

	switch auth := auth.(type) {
	case *AuthAuthorizationSignUpRequired:
		return false, errors.New("account registration required via official Telegram app")

	case *AuthAuthorizationObj:
		switch user := auth.User.(type) {
		case *UserObj:
			c.clientData.botAcc = user.Bot
			go c.Cache.UpdateUser(user)
			return true, nil

		case *UserEmpty:
			return false, errors.New("authorization failed: received empty user object")

		default:
			return false, fmt.Errorf("unexpected user type: %T", user)
		}
	case nil:
		return false, nil

	default:
		return false, fmt.Errorf("unexpected authorization type: %T", auth)
	}
}

// CodeAuthAttempt handles the authentication process with code and optional 2FA password.
func CodeAuthAttempt(c *Client, phoneNumber string, opts *LoginOptions, maxRetries int) (AuthAuthorization, error) {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		code, err := opts.CodeCallback()
		if err != nil {
			if err, ok := err.(syscall.Errno); ok && err == syscall.EINTR {
				return nil, errors.New("authentication canceled by user")
			}
			return nil, fmt.Errorf("code input failed: %w", err)
		}

		code = strings.TrimSpace(code)
		if code == "cancel" || code == "exit" {
			return nil, errors.New("authentication canceled by user")
		}

		if code == "" {
			c.Log.Warn("empty code received, skipping attempt")
			continue
		}

		opts.Code = code
		authResp, err := c.AuthSignIn(phoneNumber, opts.CodeHash, code, nil)
		if err == nil {
			return authResp, nil
		}

		if MatchError(err, "PHONE_CODE_INVALID") {
			c.Log.WithError(err).Warn("invalid verification code (attempt %d/%d)", attempt, maxRetries)
			continue
		}

		if MatchError(err, "SESSION_PASSWORD_NEEDED") {
			return c.handlePasswordAuth(opts, maxRetries)
		}

		if MatchError(err, "PHONE_NUMBER_UNOCCUPIED") || MatchError(err, "The code is valid but no user with the given number") {
			return nil, errors.New("account registration required via official Telegram app")
		}

		// unexpected error
		return nil, fmt.Errorf("sign in failed: %w", err)
	}

	return nil, fmt.Errorf("authentication failed after %d attempts", maxRetries)
}

// handlePasswordAuth handles two-factor authentication password verification.
func (c *Client) handlePasswordAuth(opts *LoginOptions, maxRetries int) (AuthAuthorization, error) {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if opts.Password == "" {
			password, err := opts.PasswordCallback()
			if err != nil {
				return nil, fmt.Errorf("password input failed: %w", err)
			}

			password = strings.TrimSpace(password)
			if password == "cancel" || password == "exit" {
				return nil, errors.New("authentication canceled by user")
			}

			if password == "" {
				c.Log.Warn("empty password received (attempt %d/%d)", attempt, maxRetries)
				continue
			}

			opts.Password = password
		}

		accPassword, err := c.AccountGetPassword()
		if err != nil {
			return nil, fmt.Errorf("retrieving password settings: %w", err)
		}

		inputPassword, err := GetInputCheckPassword(opts.Password, accPassword)
		if err != nil {
			return nil, fmt.Errorf("computing password hash: %w", err)
		}

		auth, err := c.AuthCheckPassword(inputPassword)
		if err == nil {
			return auth, nil
		}

		if MatchError(err, "PASSWORD_HASH_INVALID") {
			c.Log.Warn("incorrect password (attempt %d/%d)", attempt, maxRetries)
			opts.Password = "" // reset for next attempt
			continue
		}

		return nil, fmt.Errorf("password verification failed: %w", err)
	}

	return nil, fmt.Errorf("password authentication failed after %d attempts", maxRetries)
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

	formData := url.Values{}
	formData.Set("phone", conf.PhoneNum)

	reqCode, err := http.NewRequest("POST", "https://my.telegram.org/auth/send_password", strings.NewReader(formData.Encode()))
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
		return 0, "", false, fmt.Errorf("too many requests, try again later: %w", err)
	}

	code, err := conf.WebCodeCallback()
	if err != nil {
		return 0, "", false, err
	}

	loginData := url.Values{}
	loginData.Set("phone", conf.PhoneNum)
	loginData.Set("random_hash", result.RandomHash)
	loginData.Set("password", code)

	reqLogin, err := http.NewRequest("POST", "https://my.telegram.org/auth/login", strings.NewReader(loginData.Encode()))
	if err != nil {
		return 0, "", false, err
	}

	reqLogin.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	respLogin, err := customClient.Do(reqLogin)
	if err != nil || respLogin.StatusCode != 200 {
		return 0, "", false, err
	}

	cookies := respLogin.Cookies()
	ALREADY_TRIED_CREATION := false

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

	if len(appID) < 2 || len(appHash) < 2 || strings.Contains(string(body), "Create new application") && !ALREADY_TRIED_CREATION {
		ALREADY_TRIED_CREATION = true
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

		suffix := string(appRandomSuffix)
		createAppData := url.Values{}
		createAppData.Set("hash", hiddenHash[1])
		createAppData.Set("app_title", "AppForGogram"+suffix)
		createAppData.Set("app_shortname", "gogramapp"+suffix)
		createAppData.Set("app_platform", "android")
		createAppData.Set("app_url", "https://gogram.fun")
		createAppData.Set("app_desc", "ForGoGram"+suffix)

		reqCreateApp, err := http.NewRequest("POST", "https://my.telegram.org/apps/create", strings.NewReader(createAppData.Encode()))
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

type QrToken struct {
	qrBase64 string
	qrSmall  string

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

func (q *QrToken) URL() string {
	return q.Url
}

func (q *QrToken) TOKEN() []byte {
	return q.Token
}

func (q *QrToken) Expires() int32 {
	return q.ExpiresIn
}

func (q *QrToken) IsExpired() bool {
	return time.Now().After(time.Unix(int64(q.ExpiresIn), 0))
}

func (q *QrToken) ExportAsPng() ([]byte, error) {
	base64Data := q.qrBase64
	return base64.StdEncoding.DecodeString(base64Data)
}

func (q *QrToken) ExportAsSmallString() string {
	return q.qrSmall
}

func (q *QrToken) PrintToConsole() {
	fmt.Println(q.qrSmall)
}

func (q *QrToken) Renew() error {
	qrNew, err := q.client.QRLogin(q.IgnoredIDs...)
	if err != nil {
		return err
	}

	q.Token = qrNew.Token
	q.Url = qrNew.Url
	q.ExpiresIn = qrNew.ExpiresIn
	q.Timeout = 600
	qrMake, err := utils.NewQRCode(q.Url)
	if err != nil {
		return err
	}

	q.qrBase64 = qrMake.Base64PNG(0)
	q.qrSmall = qrMake.ToSmallString(false)
	return nil
}

func (q *QrToken) WaitLogin(timeout ...int32) error {
	q.Timeout = getVariadic(timeout, 600)
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
					return fmt.Errorf("empty user received after qr login")
				default:
					return fmt.Errorf("unexpected user type received after qr login: %T", u)
				}
			}
		}
		return nil
	case <-time.After(time.Duration(q.Timeout) * time.Second):
		go q.client.removeHandle(ev)
		return fmt.Errorf("qr login wait timeout after %d seconds", q.Timeout)
	}
}

// QRLogin initiates a QR code login and returns the QrToken containing the QR code and related information.
func (c *Client) QRLogin(IgnoreIDs ...int64) (*QrToken, error) {
	var ignoreIDs []int64
	ignoreIDs = append(ignoreIDs, IgnoreIDs...)
	qr, err := c.AuthExportLoginToken(c.AppID(), c.AppHash(), ignoreIDs)
	if err != nil {
		return nil, err
	}
	switch qr := qr.(type) {
	case *AuthLoginTokenMigrateTo:
		if err := c.SwitchDc(int(qr.DcID)); err != nil {
			return nil, err
		}
		return c.QRLogin(IgnoreIDs...)
	case *AuthLoginTokenObj:
		qrHash := base64.RawURLEncoding.EncodeToString(qr.Token)
		qrURL := fmt.Sprintf("tg://login?token=%s", qrHash)
		qrCodeMaker, err := utils.NewQRCode(qrURL)
		if err != nil {
			return nil, err
		}

		return &QrToken{
			qrBase64:   qrCodeMaker.Base64PNG(0),
			qrSmall:    qrCodeMaker.ToSmallString(false),
			Token:      qr.Token,
			Url:        qrURL,
			ExpiresIn:  qr.Expires,
			client:     c,
			IgnoredIDs: ignoreIDs,
		}, nil
	}

	return nil, fmt.Errorf("unexpected qr login response type: %T", qr)
}

// Logs out from the current account
func (c *Client) LogOut() error {
	_, err := c.AuthLogOut()
	c.MTProto.DeleteSession()
	return err
}
