// Copyright (c) 2025 @AmarnathCJD

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

// AuthCallback is a function type for authentication input callbacks.
type AuthCallback func() (string, error)

// WrongPasswordCallback is called when an incorrect password is entered.
// It receives the attempt number and max retries, and returns whether to continue trying.
type WrongPasswordCallback func(attempt, maxRetries int) bool

func DefaultWrongPasswordCallback(attempt, maxRetries int) bool {
	fmt.Printf("Incorrect password (attempt %d/%d). Please try again.\n", attempt, maxRetries)
	return true // continue retrying
}

func DefaultCodeCallback() (string, error) {
	fmt.Printf("Enter authentication code: ")
	var code string
	if _, err := fmt.Scanln(&code); err != nil {
		return "", fmt.Errorf("reading code: %w", err)
	}
	return strings.TrimSpace(code), nil
}

func DefaultPasswordCallback() (string, error) {
	fmt.Printf("Enter password: ")
	var password string
	if _, err := fmt.Scanln(&password); err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return strings.TrimSpace(password), nil
}

type LoginOptions struct {
	Password         string `json:"password,omitempty"`
	Code             string `json:"code,omitempty"`
	CodeHash         string `json:"code_hash,omitempty"`
	MaxRetries       int    `json:"max_retries,omitempty"`
	CodeCallback     AuthCallback
	PasswordCallback AuthCallback
	OnWrongPassword  WrongPasswordCallback
	OnWrongCode      func(attempt, maxRetries int) bool
	FirstName        string `json:"first_name,omitempty"`
	LastName         string `json:"last_name,omitempty"`
}

// Login authenticates the client using a phone number.
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
	applyLoginDefaults(opts)

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
		return false, errors.New("this phone number is not registered. Please sign up via the official Telegram app")

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

func applyLoginDefaults(opts *LoginOptions) {
	if opts.CodeCallback == nil {
		opts.CodeCallback = DefaultCodeCallback
	}
	if opts.PasswordCallback == nil {
		opts.PasswordCallback = DefaultPasswordCallback
	}
	if opts.OnWrongPassword == nil {
		opts.OnWrongPassword = DefaultWrongPasswordCallback
	}
	if opts.OnWrongCode == nil {
		opts.OnWrongCode = func(attempt, maxRetries int) bool {
			fmt.Printf("Invalid code (attempt %d/%d). Please try again.\n", attempt, maxRetries)
			return true
		}
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 3
	}
}

// CodeAuthAttempt handles the authentication process with code and optional 2FA password.
func CodeAuthAttempt(c *Client, phoneNumber string, opts *LoginOptions, maxRetries int) (AuthAuthorization, error) {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		code, err := opts.CodeCallback()
		if err != nil {
			if errno, ok := err.(syscall.Errno); ok && errno == syscall.EINTR {
				return nil, errors.New("authentication canceled by user")
			}
			return nil, fmt.Errorf("code input failed: %w", err)
		}

		if isCancelInput(code) {
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
			if opts.OnWrongCode != nil && !opts.OnWrongCode(attempt, maxRetries) {
				return nil, errors.New("authentication canceled: wrong code")
			}
			continue
		}

		if MatchError(err, "SESSION_PASSWORD_NEEDED") {
			return c.handlePasswordAuth(opts, maxRetries)
		}

		if MatchError(err, "PHONE_NUMBER_UNOCCUPIED") || MatchError(err, "The code is valid but no user with the given number") {
			return nil, errors.New("account registration required via official Telegram app")
		}

		return nil, fmt.Errorf("sign in failed: %w", err)
	}

	return nil, fmt.Errorf("code authentication failed after %d attempts", maxRetries)
}

func isCancelInput(input string) bool {
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "cancel" || input == "exit" || input == "quit" || input == "q"
}

// handlePasswordAuth handles two-factor authentication password verification.
func (c *Client) handlePasswordAuth(opts *LoginOptions, maxRetries int) (AuthAuthorization, error) {
	fmt.Println("Two-factor authentication is enabled.")

	for attempt := 1; attempt <= maxRetries; attempt++ {
		password := opts.Password

		if password == "" {
			var err error
			password, err = opts.PasswordCallback()
			if err != nil {
				return nil, fmt.Errorf("password input failed: %w", err)
			}

			if isCancelInput(password) {
				return nil, errors.New("authentication canceled by user")
			}

			if password == "" {
				c.Log.Warn("empty password received, skipping attempt")
				continue
			}
		}

		auth, err := c.verifyPassword(password)
		if err == nil {
			return auth, nil
		}

		if MatchError(err, "PASSWORD_HASH_INVALID") {
			opts.Password = ""
			if opts.OnWrongPassword != nil && !opts.OnWrongPassword(attempt, maxRetries) {
				return nil, errors.New("authentication canceled: wrong password")
			}
			continue
		}

		return nil, fmt.Errorf("password verification failed: %w", err)
	}

	return nil, fmt.Errorf("password authentication failed after %d attempts", maxRetries)
}

// verifyPassword verifies the 2FA password and returns the auth result.
func (c *Client) verifyPassword(password string) (AuthAuthorization, error) {
	accPassword, err := c.AccountGetPassword()
	if err != nil {
		return nil, fmt.Errorf("retrieving password settings: %w", err)
	}

	inputPassword, err := GetInputCheckPassword(password, accPassword)
	if err != nil {
		return nil, fmt.Errorf("computing password hash: %w", err)
	}

	return c.AuthCheckPassword(inputPassword)
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
	qrBase64         string
	qrSmall          string
	passwordCallback AuthCallback
	onWrongPassword  WrongPasswordCallback
	maxRetries       int
	token            []byte
	url              string
	expiresIn        int32
	ignoredIDs       []int64
	client           *Client
	timeout          int32
}

// handleQrPasswordAuth handles 2FA password authentication during QR login.
func (q *QrToken) handleQrPasswordAuth() (AuthLoginToken, error) {
	if q.passwordCallback == nil {
		return nil, errors.New("password callback is nil: 2FA is enabled but no password handler provided")
	}

	maxRetries := q.maxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	onWrongPassword := q.onWrongPassword
	if onWrongPassword == nil {
		onWrongPassword = DefaultWrongPasswordCallback
	}

	fmt.Println("Two-factor authentication is enabled.")

	for attempt := 1; attempt <= maxRetries; attempt++ {
		password, err := q.passwordCallback()
		if err != nil {
			return nil, fmt.Errorf("password input failed: %w", err)
		}

		password = strings.TrimSpace(password)
		if isCancelInput(password) {
			return nil, errors.New("authentication canceled by user")
		}

		if password == "" {
			q.client.Log.Warn("empty password received, skipping attempt")
			continue
		}

		_, err = q.client.verifyPassword(password)
		if err != nil {
			if MatchError(err, "PASSWORD_HASH_INVALID") {
				if !onWrongPassword(attempt, maxRetries) {
					return nil, errors.New("authentication canceled: wrong password")
				}
				continue
			}
			return nil, fmt.Errorf("password verification failed: %w", err)
		}

		// Password verified, export login token
		return q.client.AuthExportLoginToken(q.client.AppID(), q.client.AppHash(), q.ignoredIDs)
	}

	return nil, fmt.Errorf("password authentication failed after %d attempts", maxRetries)
}

func (q *QrToken) Url() string {
	return q.url
}

func (q *QrToken) Token() []byte {
	return q.token
}

func (q *QrToken) Expires() int32 {
	return q.expiresIn
}

func (q *QrToken) IsExpired() bool {
	return time.Now().After(time.Unix(int64(q.expiresIn), 0))
}

func (q *QrToken) ExportAsPng() ([]byte, error) {
	base64Data := q.qrBase64
	return base64.StdEncoding.DecodeString(base64Data)
}

func (q *QrToken) ExportAsSmallString() string {
	return q.qrSmall
}

func (q *QrToken) PrintToConsole() {
	fmt.Println("Scan the following QR code to login:")
	fmt.Println(q.qrSmall)
}

func (q *QrToken) Renew() error {
	options := QrOptions{
		IgnoredIDs:       q.ignoredIDs,
		PasswordCallback: q.passwordCallback,
		OnWrongPassword:  q.onWrongPassword,
		Timeout:          q.timeout,
		MaxRetries:       q.maxRetries,
	}

	qrNew, err := q.client.QRLogin(options)
	if err != nil {
		return err
	}

	q.token = qrNew.token
	q.url = qrNew.url
	q.expiresIn = qrNew.expiresIn
	q.timeout = qrNew.timeout
	q.maxRetries = qrNew.maxRetries
	q.passwordCallback = qrNew.passwordCallback
	q.onWrongPassword = qrNew.onWrongPassword

	qrMake, err := utils.NewQRCode(q.url)
	if err != nil {
		return err
	}

	q.qrBase64 = qrMake.Base64PNG(0)
	q.qrSmall = qrMake.ToSmallString(false)
	return nil
}

func (q *QrToken) WaitLogin(timeout ...int32) error {
	if au, err := q.client.IsAuthorized(); au || err == nil {
		return nil
	}

	q.timeout = getVariadic(timeout, q.timeout)
	ch := make(chan int)
	ev := q.client.AddRawHandler(&UpdateLoginToken{}, func(update Update, client *Client) error {
		ch <- 1
		return nil
	})
	select {
	case <-ch:
		go q.client.removeHandle(ev)
		resp, err := q.client.AuthExportLoginToken(q.client.AppID(), q.client.AppHash(), q.ignoredIDs)
		if err != nil {
			if MatchError(err, "SESSION_PASSWORD_NEEDED") {
				resp, err = q.handleQrPasswordAuth()
				if err != nil {
					return err
				}
			} else {
				return err
			}
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
			q.client.Log.Info("QR login successful")
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
	case <-time.After(time.Duration(q.timeout) * time.Second):
		go q.client.removeHandle(ev)
		return fmt.Errorf("qr login wait timeout after %d seconds", q.timeout)
	}
}

type QrOptions struct {
	IgnoredIDs       []int64               // IDs to be excluded from the QR login
	PasswordCallback AuthCallback          // Callback to get password if 2FA is enabled
	OnWrongPassword  WrongPasswordCallback // Called when password is incorrect
	Timeout          int32                 // Timeout for the QR login in seconds
	MaxRetries       int                   // Max password retry attempts (default: 3)
}

// QRLogin initiates a QR code login and returns the QrToken containing the QR code and related information.
func (c *Client) QRLogin(options ...QrOptions) (*QrToken, error) {
	var opts QrOptions
	if len(options) > 0 {
		opts = options[0]
	}
	applyQrDefaults(&opts)

	qr, err := c.AuthExportLoginToken(c.AppID(), c.AppHash(), opts.IgnoredIDs)
	if err != nil {
		return nil, err
	}

	switch qr := qr.(type) {
	case *AuthLoginTokenMigrateTo:
		if err := c.SwitchDc(int(qr.DcID)); err != nil {
			return nil, err
		}
		return c.QRLogin(options...)

	case *AuthLoginTokenObj:
		qrHash := base64.RawURLEncoding.EncodeToString(qr.Token)
		qrURL := fmt.Sprintf("tg://login?token=%s", qrHash)
		qrCodeMaker, err := utils.NewQRCode(qrURL)
		if err != nil {
			return nil, err
		}

		return &QrToken{
			qrBase64:         qrCodeMaker.Base64PNG(0),
			qrSmall:          qrCodeMaker.ToSmallString(true),
			passwordCallback: opts.PasswordCallback,
			onWrongPassword:  opts.OnWrongPassword,
			maxRetries:       opts.MaxRetries,
			token:            qr.Token,
			url:              qrURL,
			expiresIn:        qr.Expires,
			client:           c,
			ignoredIDs:       opts.IgnoredIDs,
			timeout:          opts.Timeout,
		}, nil
	}

	return nil, fmt.Errorf("unexpected qr login response type: %T", qr)
}

func applyQrDefaults(opts *QrOptions) {
	if opts.PasswordCallback == nil {
		opts.PasswordCallback = DefaultPasswordCallback
	}
	if opts.OnWrongPassword == nil {
		opts.OnWrongPassword = DefaultWrongPasswordCallback
	}
	if opts.Timeout == 0 {
		opts.Timeout = 600
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 3
	}
}

// Logs out from the current account
func (c *Client) LogOut() error {
	_, err := c.AuthLogOut()
	c.MTProto.DeleteSession()
	return err
}
