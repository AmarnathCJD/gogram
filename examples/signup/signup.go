package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
	"github.com/quic-go/quic-go/http3"
)

const (
	appID             = 4
	appHash           = "014b35b6184100b085b0d0572f9b5103"
	packageID         = "org.telegram.messenger"
	versionCode       = 71751
	versionName       = "13.0.2"
	certificateBase64 = "MIICFzCCAYCgAwIBAgIEUh+dSTANBgkqhkiG9w0BAQUFADBQMRkwFwYDVQQHExBTYWludC1QZXRlcnNidXJnMQswCQYDVQQKEwJWSzELMAkGA1UECxMCVksxGTAXBgNVBAMTEE5pa29sYXkgS3VkYXNob3YwHhcNMTMwODI5MTkxMzEzWhcNMzgwODIzMTkxMzEzWjBQMRkwFwYDVQQHExBTYWludC1QZXRlcnNidXJnMQswCQYDVQQKEwJWSzELMAkGA1UECxMCVksxGTAXBgNVBAMTEE5pa29sYXkgS3VkYXNob3YwgZ8wDQYJKoZIhvcNAQEBBQADgY0AMIGJAoGBAN9emToN7Aq1tVff/3fgsiJxhsvxPR/R7Y6d61ZQxf1EZ7tRv6WFIo0IS9JwRfdBW3xOOPCL42Jjmi7rmwx0naRg8nBfan4UrKdqvjNgrwC3GcxfP/TU2gWVgyfpSLNnnmQXrXuqh3m51ol5m6NFg5oEn9RDYkmQVKCAOgF4x3N5AgMBAAEwDQYJKoZIhvcNAQEFBQADgYEA3aWM3ZAVnEMezEoVkC6vsHpQ4Bup1PjmVewUsGvY6HcSOXEKKJkQOeAuNSdi61JK8HYCu9+0edNxhlilNNQR36swEiyNCl79FlpiBmnYCiIaBKx9aLOBEVDHac+X0ydL6bnyfExYd+q7z4mQQJ5ZQ9+N61CfqD1o6rx098WXZ0M="
)

var input = bufio.NewReader(os.Stdin)

func prompt(label string, optional ...bool) (string, error) {
	fmt.Print(label + ": ")
	for {
		line, err := input.ReadString('\n')
		if err != nil && !(errors.Is(err, io.EOF) && len(line) > 0) {
			return "", fmt.Errorf("reading input: %w", err)
		}
		value := strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(value) != "" || (len(optional) > 0 && optional[0]) {
			return value, nil
		}
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	fcmToken, cert, err := bootstrapFirebase("login.gms.json")
	if err != nil {
		return err
	}
	device := telegram.DeviceConfig{
		DeviceModel:    "GooglePixel 9",
		SystemVersion:  "SDK 35",
		AppVersion:     "13.0.2 (71751)",
		SystemLangCode: "en-us",
		LangPack:       "android",
		LangCode:       "en",
		Params: &telegram.JsonObject{Value: []*telegram.JsonObjectValue{
			{
				Key:   "device_token",
				Value: &telegram.JsonString{Value: fcmToken},
			},
			{
				Key:   "data",
				Value: &telegram.JsonString{Value: cert.SHA256},
			},
			{
				Key:   "installer",
				Value: &telegram.JsonString{Value: "com.android.vending"},
			},
			{
				Key:   "package_id",
				Value: &telegram.JsonString{Value: packageID},
			},
			{
				Key:   "tz_offset",
				Value: &telegram.JsonNumber{Value: 19800},
			},
			{
				Key:   "perf_cat",
				Value: &telegram.JsonNumber{Value: 1},
			},
		}},
	}
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:         appID,
		AppHash:       appHash,
		DataCenter:    5,
		DeviceConfig:  device,
		MemorySession: true,
		NoPreconnect:  true,
		NoUpdates:     true,
		DisableCache:  true,
		Timeout:       45,
		ReqTimeout:    45,
		LogLevel:      telegram.LogError,
		FloodHandler:  func(error) bool { return false },
		ErrorHandler:  func(error) bool { return false },
	})
	if err != nil {
		return err
	}
	defer client.Terminate()
	if err := client.Connect(); err != nil {
		return err
	}
	call := func(q telegram.TLObject) (any, error) {
		pending := q
		usedCaptcha, usedIntegrity := false, false
		for range 6 {
			dc := client.GetDC()
			result, err := invoke(client, pending)
			if err == nil {
				return result, nil
			}
			if client.GetDC() != dc {
				if err := client.InitialRequest(); err != nil {
					return nil, err
				}
				pending, usedCaptcha, usedIntegrity = q, false, false
				continue
			}
			rpc := client.ToRpcError(err)
			if rpc != nil && rpc.Code == 403 && strings.HasPrefix(rpc.Message, "INTEGRITY_CHECK_CLASSIC_") && !usedIntegrity {
				projectText, nonce, ok := strings.Cut(strings.TrimPrefix(rpc.Message, "INTEGRITY_CHECK_CLASSIC_"), "_")
				project, parseErr := strconv.ParseInt(projectText, 10, 64)
				if !ok || parseErr != nil || project <= 0 || nonce == "" {
					return nil, err
				}
				token, err := integrityToken(project, nonce)
				if err != nil {
					return nil, err
				}
				pending = &telegram.InvokeWithGooglePlayIntegrityParams{
					Nonce: nonce,
					Token: token,
					Query: pending,
				}
				usedIntegrity = true
				continue
			}
			if rpc != nil && rpc.Code == 403 && strings.HasPrefix(rpc.Message, "RECAPTCHA_CHECK_") && !usedCaptcha {
				action, key, ok := strings.Cut(strings.TrimPrefix(rpc.Message, "RECAPTCHA_CHECK_"), "__")
				if !ok || action == "" || key == "" {
					return nil, err
				}
				// fmt.Printf("reCAPTCHA action: %s\nSite key: %s\n", action, key)
				token := "AA"
				if token == "" || strings.ContainsAny(token, " \t\r\n\"{}") || strings.HasPrefix(token, "RECAPTCHA_FAILED_") {
					return nil, errors.New("paste only the captcha token value")
				}
				pending = &telegram.InvokeWithReCaptchaParams{
					Token: token,
					Query: pending,
				}
				usedCaptcha = true
				continue
			}
			return nil, err
		}
		return nil, errors.New("too many login redirects/challenges")
	}
	fmt.Println("Gogram client connected and initialized.")
	phone, err := prompt("Phone number (with +country code)")
	if err != nil {
		return err
	}
	phone = strings.TrimSpace(phone)
	result, err := call(&telegram.AuthSendCodeParams{
		PhoneNumber: phone,
		APIID:       appID,
		APIHash:     appHash,
		Settings: &telegram.CodeSettings{
			AllowAppHash:  true,
			AllowFirebase: true,
			LogoutTokens:  [][]byte{},
		},
	})
	if err != nil {
		return err
	}
	phoneCodeHash := ""

	for transitions := 0; transitions < 12; transitions++ {
		switch value := result.(type) {
		case *telegram.AuthSentCodeSuccess:
			result = value.Authorization
		case *telegram.AuthSentCodeObj:
			if value.PhoneCodeHash == "" {
				return errors.New("Telegram returned an empty phone_code_hash")
			}
			phoneCodeHash = value.PhoneCodeHash
			if _, ok := value.Type.(*telegram.AuthSentCodeTypeSetUpEmailRequired); ok {
				result, err = setupEmail(phone, value.PhoneCodeHash, call, prompt, os.Stdout)
				if err != nil {
					return err
				}
				continue
			}
			signIn := &telegram.AuthSignInParams{
				PhoneNumber:   phone,
				PhoneCodeHash: value.PhoneCodeHash,
			}
			switch delivery := value.Type.(type) {
			case *telegram.AuthSentCodeTypeFirebaseSms:
				if delivery.PlayIntegrityProjectID <= 0 || len(delivery.PlayIntegrityNonce) == 0 {
					return errors.New("Firebase SMS response is missing Classic Play Integrity challenge fields")
				}
				token, err := integrityToken(delivery.PlayIntegrityProjectID, androidNonce(delivery.PlayIntegrityNonce))
				if err != nil {
					return err
				}
				accepted, err := call(&telegram.AuthRequestFirebaseSmsParams{
					PhoneNumber:        phone,
					PhoneCodeHash:      value.PhoneCodeHash,
					PlayIntegrityToken: token,
				})
				if err != nil {
					return err
				}
				if ok, _ := accepted.(bool); !ok {
					return errors.New("Telegram did not accept the Firebase SMS request")
				}
				signIn.PhoneCode, err = prompt("SMS code")
				if err != nil {
					return err
				}
			case *telegram.AuthSentCodeTypeEmailCode:
				fmt.Println("Code sent to", delivery.EmailPattern)
				var code string
				code, err = prompt("Email login code")
				signIn.EmailVerification = &telegram.EmailVerificationCode{Code: code}
			case *telegram.AuthSentCodeTypeApp, *telegram.AuthSentCodeTypeSms,
				*telegram.AuthSentCodeTypeCall, *telegram.AuthSentCodeTypeMissedCall,
				*telegram.AuthSentCodeTypeFlashCall, *telegram.AuthSentCodeTypeFragmentSms,
				*telegram.AuthSentCodeTypeSmsWord, *telegram.AuthSentCodeTypeSmsPhrase:
				fmt.Printf("Delivery: %T\n", delivery)
				signIn.PhoneCode, err = prompt("Telegram / SMS / call code")
			default:
				return fmt.Errorf("unsupported delivery: %T", delivery)
			}
			if err != nil {
				return err
			}
			result, err = call(signIn)
			if telegram.MatchError(err, "SESSION_PASSWORD_NEEDED") {
				passwordResult, err := call(&telegram.AccountGetPasswordParams{})
				if err != nil {
					return err
				}
				params, ok := passwordResult.(*telegram.AccountPassword)
				if !ok || params == nil {
					return fmt.Errorf("unexpected password response: %T", passwordResult)
				}
				password, err := prompt("Two-step verification password")
				if err != nil {
					return err
				}
				proof, err := telegram.GetInputCheckPassword(password, params)
				if err != nil {
					return err
				}
				result, err = call(&telegram.AuthCheckPasswordParams{Password: proof})
				if err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		case *telegram.AuthAuthorizationObj:
			file, err := os.CreateTemp(".", "login-*.session")
			if err != nil {
				return err
			}
			_, err = file.WriteString(client.ExportSession())
			closeErr := file.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			fmt.Println("Logged in. String session saved to", file.Name())
			return nil
		case *telegram.AuthAuthorizationSignUpRequired:
			result, err = signUp(phone, phoneCodeHash, value, call, prompt, os.Stdout)
			if errors.Is(err, errSignupCanceled) {
				fmt.Println("Account creation canceled.")
				return nil
			}
			if err != nil {
				return err
			}
		case *telegram.AuthSentCodePaymentRequired:
			return errors.New("Telegram requires payment to continue this login")
		default:
			return fmt.Errorf("unexpected login response: %T", result)
		}
	}
	return errors.New("too many login steps")
}

func invoke(client *telegram.Client, request telegram.TLObject) (any, error) {
	switch q := request.(type) {
	case *telegram.AuthSendCodeParams:
		return client.AuthSendCode(q.PhoneNumber, q.APIID, q.APIHash, q.Settings)
	case *telegram.AccountSendVerifyEmailCodeParams:
		return client.AccountSendVerifyEmailCode(q.Purpose, q.Email)
	case *telegram.AccountVerifyEmailParams:
		return client.AccountVerifyEmail(q.Purpose, q.Verification)
	case *telegram.AuthRequestFirebaseSmsParams:
		return client.AuthRequestFirebaseSms(q)
	case *telegram.AuthSignInParams:
		return client.AuthSignIn(q.PhoneNumber, q.PhoneCodeHash, q.PhoneCode, q.EmailVerification)
	case *telegram.AccountGetPasswordParams:
		return client.AccountGetPassword()
	case *telegram.AuthCheckPasswordParams:
		return client.AuthCheckPassword(q.Password)
	case *telegram.AuthSignUpParams:
		return client.AuthSignUp(q)
	case *telegram.InvokeWithReCaptchaParams:
		return client.InvokeWithReCaptcha(q.Token, q.Query)
	case *telegram.InvokeWithGooglePlayIntegrityParams:
		return client.InvokeWithGooglePlayIntegrity(q.Nonce, q.Token, q.Query)
	default:
		return nil, fmt.Errorf("unsupported signup request %T", request)
	}
}

func setupEmail(phone, hash string, call func(telegram.TLObject) (any, error), read func(string, ...bool) (string, error), out io.Writer) (telegram.AuthSentCode, error) {
	purpose := &telegram.EmailVerifyPurposeLoginSetup{
		PhoneNumber:   phone,
		PhoneCodeHash: hash,
	}
	for {
		email, err := read("Email address")
		if err != nil {
			return nil, err
		}
		email = strings.TrimSpace(email)
		address, parseErr := mail.ParseAddress(email)
		domain := email[strings.LastIndex(email, "@")+1:]
		dot := strings.LastIndex(domain, ".")
		if parseErr != nil || address.Address != email || dot < 1 || len(domain[dot+1:]) < 2 {
			fmt.Fprintln(out, "Enter a complete email address, for example name@proton.me.")
			continue
		}
		sent, err := call(&telegram.AccountSendVerifyEmailCodeParams{
			Purpose: purpose,
			Email:   email,
		})
		if telegram.MatchError(err, "[EMAIL_NOT_ALLOWED]") || telegram.MatchError(err, "[EMAIL_INVALID]") {
			fmt.Fprintln(out, "Telegram rejected that email address. Check the spelling or enter another address.")
			continue
		}
		if err != nil {
			return nil, err
		}
		if response, ok := sent.(*telegram.AccountSentEmailCode); !ok || response == nil {
			return nil, fmt.Errorf("unexpected email code response %T", sent)
		}
		for {
			code, err := read("Email verification code")
			if err != nil {
				return nil, err
			}
			code = strings.TrimSpace(code)
			verified, err := call(&telegram.AccountVerifyEmailParams{
				Purpose:      purpose,
				Verification: &telegram.EmailVerificationCode{Code: code},
			})
			if telegram.MatchError(err, "[EMAIL_NOT_ALLOWED]") || telegram.MatchError(err, "[EMAIL_INVALID]") {
				fmt.Fprintln(out, "Telegram rejected that email address. Check the spelling or enter another address.")
				break
			}
			if telegram.MatchError(err, "[CODE_INVALID]") || telegram.MatchError(err, "[EMAIL_CODE_INVALID]") {
				fmt.Fprintln(out, "Incorrect email code. Try again.")
				continue
			}
			if telegram.MatchError(err, "[CODE_EXPIRED]") || telegram.MatchError(err, "[EMAIL_CODE_EXPIRED]") || telegram.MatchError(err, "[EMAIL_VERIFY_EXPIRED]") {
				fmt.Fprintln(out, "The email code expired. Enter your email again to request a new code.")
				break
			}
			if err != nil {
				return nil, err
			}
			email, ok := verified.(*telegram.AccountEmailVerifiedLogin)
			if !ok || email == nil || email.SentCode == nil {
				return nil, errors.New("email verification did not return a login code step")
			}
			return email.SentCode, nil
		}
	}
}

var errSignupCanceled = errors.New("account creation canceled")

func signUp(phone, hash string, signup *telegram.AuthAuthorizationSignUpRequired,
	call func(telegram.TLObject) (any, error), read func(string, ...bool) (string, error), out io.Writer,
) (*telegram.AuthAuthorizationObj, error) {
	if phone == "" || hash == "" {
		return nil, errors.New("signup requires the verified phone number and phone_code_hash")
	}
	fmt.Fprintln(out, "Telegram requires a new account for this phone number.")
	label := "Type yes to create the account (anything else cancels)"
	if terms := signup.TermsOfService; terms != nil {
		fmt.Fprintln(out, "Telegram terms of service:", terms.Text)
		label = "Type yes to accept these terms and create the account"
		if terms.MinAgeConfirm > 0 {
			fmt.Fprintf(out, "You must be at least %d years old.\n", terms.MinAgeConfirm)
			label = "Type yes to accept these terms, confirm you meet the minimum age, and create the account"
		}
	}
	answer, err := read(label)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(answer), "yes") {
		return nil, errSignupCanceled
	}
	first, err := read("First name")
	if err != nil {
		return nil, err
	}
	first = strings.TrimSpace(first)
	if first == "" {
		return nil, errors.New("a first name is required")
	}
	last, err := read("Last name (optional; Enter to skip)", true)
	if err != nil {
		return nil, err
	}
	last = strings.TrimSpace(last)
	result, err := call(&telegram.AuthSignUpParams{
		PhoneNumber:   phone,
		PhoneCodeHash: hash,
		FirstName:     first,
		LastName:      last,
	})
	if err != nil {
		return nil, err
	}
	auth, ok := result.(*telegram.AuthAuthorizationObj)
	if !ok || auth == nil {
		return nil, fmt.Errorf("unexpected signup response: %T", result)
	}
	return auth, nil
}

type config struct {
	PackageID   string
	VersionName string
	VersionCode int64
}

type certificate struct {
	SHA1   string
	SHA256 string
}

var safeRPCName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,100}$`)

func saveFirebaseState(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(".", "login-*.firebase.json")
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Println("Firebase credentials saved to", file.Name())
	return nil
}

func bootstrapFirebase(gmsPath string) (string, certificate, error) {
	c := config{
		PackageID:   packageID,
		VersionCode: versionCode,
		VersionName: versionName,
	}
	raw, err := base64.StdEncoding.DecodeString(certificateBase64)
	if err != nil {
		return "", certificate{}, err
	}
	sha1sum, sha256sum := sha1.Sum(raw), sha256.Sum256(raw)
	cert := certificate{
		SHA1:   hex.EncodeToString(sha1sum[:]),
		SHA256: strings.ToUpper(hex.EncodeToString(sha256sum[:])),
	}
	d := firebaseDevice
	state, err := loadOrCreateCheckin(gmsPath)
	if err != nil {
		return "", cert, err
	}
	d.AndroidID, d.SecurityToken, d.Info = state.AndroidID, state.SecurityToken, state.Info
	fmt.Println("Creating Firebase installation in", d.ProjectID)
	installation, err := createInstallation(context.Background(), &http.Client{Timeout: 45 * time.Second}, "https://firebaseinstallations.googleapis.com", c, cert, d)
	if err != nil {
		return "", cert, err
	}
	if err := saveFirebaseState(installation); err != nil {
		return "", cert, err
	}
	fmt.Println("Registering FCM over HTTP/3...")
	transport := &http3.Transport{}
	defer transport.Close()
	token, err := registerFCM(context.Background(), &http.Client{
		Transport: transport,
		Timeout:   45 * time.Second,
	}, "https://android.apis.google.com", c, cert, d, installation)
	if err != nil {
		return "", cert, err
	}
	if err := saveFirebaseState(map[string]string{
		"token":  token,
		"fid":    installation.FID,
		"sender": d.SenderID,
	}); err != nil {
		return "", cert, err
	}
	fmt.Println("Firebase ready. Connecting to Telegram...")
	return token, cert, nil
}

func integrityToken(project int64, nonce string) (string, error) {
	details, err := json.Marshal(map[string]any{
		"integrity_mode":                 "classic",
		"cloud_project_number":           strconv.FormatInt(project, 10),
		"nonce":                          nonce,
		"package_name":                   packageID,
		"version_code":                   versionCode,
		"signing_certificate_der_base64": certificateBase64,
	})
	if err != nil {
		return "", err
	}
	fmt.Printf("Classic Integrity challenge (use the exact JSON-decoded nonce): %s\n", details)
	token, err := prompt("Classic Integrity token")
	if err != nil {
		return "", err
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t\r\n\"{}") {
		return "", errors.New("paste only the Integrity token value")
	}
	return token, nil
}

// Android requires URL-safe Base64 with padding and a final newline.
func androidNonce(raw []byte) string {
	s := base64.URLEncoding.EncodeToString(raw)
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76])
		b.WriteByte('\n')
		s = s[76:]
	}
	if s != "" {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	return b.String()
}

type avdInfo struct {
	AndroidID     string
	SecurityToken string
	Info          string
	APIKey        string
	AppID         string
	SenderID      string
	ProjectID     string
	SDK           string
	Release       string
	Model         string
	BuildID       string
	GMSVersion    string
	TargetSDK     string
}

var firebaseDevice = avdInfo{
	APIKey:     "AIzaSyA-t0jLPjUt2FxrA8VPK2EiYHcYcboIR6k",
	AppID:      "1:760348033671:android:f6afd7b67eae3860",
	SenderID:   "760348033671",
	ProjectID:  "tmessages2",
	SDK:        "35",
	Release:    "15",
	Model:      "Pixel 9",
	BuildID:    "ZP11.260821.010",
	TargetSDK:  "36",
	GMSVersion: "263635038",
}

type installation struct {
	Name         string `json:"name"`
	FID          string `json:"fid"`
	RefreshToken string `json:"refreshToken"`
	AuthToken    struct {
		Token     string `json:"token"`
		ExpiresIn string `json:"expiresIn"`
	} `json:"authToken"`
}

func boundedResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Google HTTP %d (no automatic retry)", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(b) > 65536 {
		return nil, errors.New("Google response unreadable or larger than 64 KiB")
	}
	return b, nil
}

func createInstallation(ctx context.Context, client *http.Client, endpoint string, c config, cert certificate, d avdInfo) (*installation, error) {
	var fidBytes [17]byte
	if _, err := rand.Read(fidBytes[:16]); err != nil {
		return nil, err
	}
	fidBytes[16] = fidBytes[0]
	fidBytes[0] = (fidBytes[0] & 0x0f) | 0x70
	fid := base64.RawURLEncoding.EncodeToString(fidBytes[:])[:22]
	body, _ := json.Marshal(map[string]string{
		"fid":         fid,
		"appId":       d.AppID,
		"authVersion": "FIS_v2",
		"sdkVersion":  "a:17.0.0",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/projects/"+url.PathEscape(d.ProjectID)+"/installations", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("X-Android-Package", c.PackageID)
	req.Header.Set("X-Android-Cert", strings.ToUpper(cert.SHA1))
	req.Header.Set("X-Goog-Api-Key", d.APIKey)
	req.Header.Set("User-Agent", fmt.Sprintf("Dalvik/2.1.0 (Linux; U; Android %s; %s Build/%s)", d.Release, d.Model, d.BuildID))
	local := *client
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := local.Do(req)
	if err != nil {
		return nil, errors.New("Firebase installation request failed; remote outcome unknown")
	}
	raw, err := boundedResponse(resp)
	if err != nil {
		return nil, err
	}
	var result installation
	if json.Unmarshal(raw, &result) != nil || result.FID == "" || result.AuthToken.Token == "" || result.RefreshToken == "" || result.Name == "" {
		return nil, errors.New("Firebase returned an incomplete installation response")
	}
	return &result, nil
}

func registerFCM(ctx context.Context, client *http.Client, endpoint string, c config, cert certificate, d avdInfo, i *installation) (string, error) {
	version := strconv.FormatInt(c.VersionCode, 10)
	form := url.Values{
		"X-subtype":                          {d.SenderID},
		"sender":                             {d.SenderID},
		"X-app_ver":                          {version},
		"X-osv":                              {d.SDK},
		"X-cliv":                             {"fcm-22.0.0"},
		"X-gmsv":                             {d.GMSVersion},
		"X-appid":                            {i.FID},
		"X-scope":                            {"*"},
		"X-Goog-Firebase-Installations-Auth": {i.AuthToken.Token},
		"X-gmp_app_id":                       {d.AppID},
		"X-firebase-app-name-hash":           {"R1dAH9Ui7M-ynoznwBdw01tLxhI"},
		"X-app_ver_name":                     {c.VersionName},
		"app":                                {c.PackageID},
		"device":                             {d.AndroidID},
		"app_ver":                            {version},
		"info":                               {d.Info},
		"gcm_ver":                            {d.GMSVersion},
		"plat":                               {"0"},
		"cert":                               {cert.SHA1},
		"target_ver":                         {d.TargetSDK},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/c2dm/register3", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "AidLogin "+d.AndroidID+":"+d.SecurityToken)
	req.Header.Set("app", c.PackageID)
	req.Header.Set("app_ver", strconv.FormatInt(c.VersionCode, 10))
	req.Header.Set("gcm_ver", d.GMSVersion)
	req.Header.Set("request_type", "0")
	req.Header.Set("priority", "u=1, i")
	req.Header.Set("User-Agent", fmt.Sprintf("com.google.android.gms/%s (Linux; U; Android %s; en_US; %s; Build/%s; Cronet/151.0.7922.83)", d.GMSVersion, d.Release, d.Model, d.BuildID))
	local := *client
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := local.Do(req)
	if err != nil {
		return "", errors.New("FCM HTTP/3 registration failed; check UDP/443 connectivity (remote outcome unknown)")
	}
	raw, err := boundedResponse(resp)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(line, "Error=") {
			code := strings.TrimSpace(strings.TrimPrefix(line, "Error="))
			if !safeRPCName.MatchString(code) {
				code = "UNRECOGNIZED_ERROR"
			}
			return "", fmt.Errorf("FCM registration returned %s", code)
		}
	}
	body := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(body, "token=") {
		return "", errors.New("FCM response did not contain a registration token")
	}
	token := strings.TrimPrefix(body, "token=")
	if len(token) < 20 || strings.ContainsAny(token, " \r\n\t") {
		return "", errors.New("FCM returned an invalid registration token")
	}
	return token, nil
}

type checkinState struct {
	AndroidID     string `json:"android_id"`
	SecurityToken string `json:"security_token"`
	Info          string `json:"info"`
	Digest        string `json:"digest"`
	TimeMS        uint64 `json:"time_ms"`
	LoggingID     uint64 `json:"logging_id,string"`
}

type protoMessage []byte

func (m *protoMessage) number(field int, value uint64) {
	*m = binary.AppendUvarint(*m, uint64(field<<3))
	*m = binary.AppendUvarint(*m, value)
}

func (m *protoMessage) data(field int, value []byte) {
	*m = binary.AppendUvarint(*m, uint64(field<<3|2))
	*m = binary.AppendUvarint(*m, uint64(len(value)))
	*m = append(*m, value...)
}

func (m *protoMessage) text(field int, value string) { m.data(field, []byte(value)) }

func makeCheckinRequest(loggingID uint64, now time.Time) []byte {
	var build, event, checkin, device, request protoMessage
	for _, item := range []struct {
		field int
		value string
	}{
		{1, "google/tokay_beta/tokay:CANARY/ZP11.260821.010/16290768:user/release-keys"},
		{2, "tokay"}, {3, "google"}, {4, "1.0.0.0"}, {5, "tokay-1.0-13344233"},
		{6, "android-google"}, {9, "tokay"}, {11, "Pixel 9"}, {12, "Google"}, {13, "tokay_beta"},
	} {
		build.text(item.field, item.value)
	}
	build.number(7, 1730479187)
	build.number(10, 35)
	build.number(14, 0)
	event.text(1, "event_log_start")
	event.number(3, uint64(now.UnixMilli()))
	checkin.data(1, build)
	checkin.number(2, 0)
	checkin.data(3, event)
	checkin.text(8, "WIFI::")
	checkin.number(9, 0)
	for _, item := range []struct {
		field int
		value uint64
	}{
		{1, 3}, {2, 1}, {3, 1}, {4, 3}, {5, 0}, {6, 0}, {7, 600}, {8, 196608}, {12, 1440}, {13, 3168},
	} {
		device.number(item.field, item.value)
	}
	device.text(9, "android.test.runner")
	for _, feature := range []string{"android.hardware.wifi", "android.hardware.touchscreen", "android.hardware.location", "android.hardware.camera", "android.hardware.telephony"} {
		device.text(10, feature)
	}
	device.text(11, "x86_64")
	device.text(11, "arm64-v8a")
	device.text(14, "en-US")
	request.number(2, 0)
	request.text(3, "1-929a0dca0eee55513280171a8585da7dcd3700f8")
	request.data(4, checkin)
	request.text(6, "en_US")
	request.number(7, loggingID)
	request.text(11, "")
	request.text(12, "Asia/Kolkata")
	request.number(14, 3)
	request.data(18, device)
	request.number(20, 0)
	return request
}

func decodeCheckin(raw []byte) (checkinState, error) {
	var state checkinState
	for len(raw) > 0 {
		tag, n := binary.Uvarint(raw)
		if n <= 0 || tag>>3 == 0 {
			return state, errors.New("invalid check-in protobuf tag")
		}
		raw = raw[n:]
		field, wire := tag>>3, tag&7
		var number uint64
		var data []byte
		switch wire {
		case 0:
			number, n = binary.Uvarint(raw)
			if n <= 0 {
				return state, errors.New("invalid check-in varint")
			}
			raw = raw[n:]
		case 1:
			if len(raw) < 8 {
				return state, io.ErrUnexpectedEOF
			}
			number, raw = binary.LittleEndian.Uint64(raw[:8]), raw[8:]
		case 2:
			length, n := binary.Uvarint(raw)
			if n <= 0 || length > uint64(len(raw)-n) {
				return state, io.ErrUnexpectedEOF
			}
			raw = raw[n:]
			data, raw = raw[:int(length)], raw[int(length):]
		case 5:
			if len(raw) < 4 {
				return state, io.ErrUnexpectedEOF
			}
			raw = raw[4:]
		default:
			return state, fmt.Errorf("unsupported check-in wire type %d", wire)
		}
		switch field {
		case 3:
			if wire != 0 {
				return state, errors.New("wrong check-in timestamp wire type")
			}
			state.TimeMS = number
		case 4:
			if wire != 2 {
				return state, errors.New("wrong check-in digest wire type")
			}
			state.Digest = string(data)
		case 7, 8:
			if wire != 1 || number == 0 {
				return state, errors.New("invalid check-in identity field")
			}
			if field == 7 {
				state.AndroidID = strconv.FormatUint(number, 10)
			} else {
				state.SecurityToken = strconv.FormatUint(number, 10)
			}
		case 11:
			if wire != 2 {
				return state, errors.New("wrong check-in versionInfo wire type")
			}
			state.Info = string(data)
		}
	}
	if state.AndroidID == "" || state.SecurityToken == "" {
		return state, errors.New("check-in response did not issue an AndroidID/security token pair")
	}
	return state, nil
}

func requestCheckin(ctx context.Context, client *http.Client, endpoint string, request []byte) (checkinState, int, error) {
	var body bytes.Buffer
	zip := gzip.NewWriter(&body)
	if _, err := zip.Write(request); err != nil {
		return checkinState{}, 0, err
	}
	if err := zip.Close(); err != nil {
		return checkinState{}, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return checkinState{}, 0, err
	}
	req.Header.Set("Content-Type", "application/x-protobuffer")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", "Android-Checkin/2.0 (tokay ZP11.260821.010); gzip")
	local := *client
	local.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := local.Do(req)
	if err != nil {
		return checkinState{}, 0, fmt.Errorf("check-in transport failed (no automatic retry): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return checkinState{}, resp.StatusCode, fmt.Errorf("check-in HTTP %d; response body omitted", resp.StatusCode)
	}
	var reader io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		unzip, err := gzip.NewReader(resp.Body)
		if err != nil {
			return checkinState{}, resp.StatusCode, err
		}
		defer unzip.Close()
		reader = unzip
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 1024*1024+1))
	if err != nil {
		return checkinState{}, resp.StatusCode, err
	}
	if len(raw) > 1024*1024 {
		return checkinState{}, resp.StatusCode, errors.New("check-in response exceeds 1 MiB")
	}
	state, err := decodeCheckin(raw)
	return state, resp.StatusCode, err
}

func validateCheckinState(state checkinState) error {
	for _, value := range []string{state.AndroidID, state.SecurityToken} {
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil || n == 0 {
			return errors.New("saved check-in has an invalid identity/token")
		}
	}
	if state.Info == "" {
		return errors.New("Google check-in versionInfo is missing")
	}
	return nil
}

func loadOrCreateCheckin(path string) (checkinState, error) {
	var state checkinState
	raw, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(raw, &state) != nil {
			return state, errors.New("invalid saved Google check-in JSON")
		}
		if err := validateCheckinState(state); err != nil {
			return state, err
		}
		fmt.Println("Using saved Google check-in identity from", path)
		return state, nil
	}
	if !os.IsNotExist(err) {
		return state, err
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return state, err
	}
	loggingID := binary.LittleEndian.Uint64(random[:])
	fmt.Println("Requesting a fresh Google check-in identity...")
	state, status, err := requestCheckin(context.Background(), &http.Client{Timeout: 45 * time.Second}, "https://android.clients.google.com/checkin", makeCheckinRequest(loggingID, time.Now()))
	if err != nil {
		return state, err
	}
	state.LoggingID = loggingID
	raw, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		return state, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return state, err
	}
	_, writeErr := file.Write(raw)
	closeErr := file.Close()
	if writeErr != nil {
		return state, writeErr
	}
	if closeErr != nil {
		return state, closeErr
	}
	fmt.Printf("Google check-in HTTP %d. Identity saved to %s\n", status, path)
	return state, validateCheckinState(state)
}
