// Copyright (c) 2022 RoseLoverX

package telegram

import (
	"fmt"
	"log"
	"net"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	mtproto "github.com/amarnathcjd/gogram"
	"github.com/pkg/errors"

	"github.com/amarnathcjd/gogram/internal/keys"
	"github.com/amarnathcjd/gogram/internal/session"
)

var (
	workDir, _ = os.Getwd()
)

type (
	PingParams struct {
		PingID int64
	}

	Client struct {
		*mtproto.MTProto
		config    *ClientConfig
		stop      chan struct{}
		Cache     *CACHE
		ParseMode string
		AppID     int32
		ApiHash   string
		Logger    *log.Logger
		Session   interface{}
	}

	ClientConfig struct {
		SessionFile   string
		StringSession string
		DeviceModel   string
		SystemVersion string
		AppVersion    string
		AppID         int
		AppHash       string
		ParseMode     string
		DataCenter    int
		AllowUpdates  bool
	}
)

func TelegramClient(c ClientConfig) (*Client, error) {
	c.SessionFile = getStr(c.SessionFile, workDir+"/session.session")
	publicKeys, err := keys.GetRSAKeys()
	if err != nil {
		return nil, errors.Wrap(err, "reading public keys")
	}
	mtproto, err := mtproto.NewMTProto(mtproto.Config{
		AuthKeyFile:   c.SessionFile,
		ServerHost:    GetHostIp(c.DataCenter),
		PublicKey:     publicKeys[0],
		DataCenter:    c.DataCenter,
		AppID:         int32(c.AppID),
		StringSession: c.StringSession,
	})
	if err != nil {
		return nil, errors.Wrap(err, "MTProto client")
	}

	err = mtproto.CreateConnection(true)
	if err != nil {
		return nil, errors.Wrap(err, "creating connection")
	}

	client := &Client{
		MTProto:   mtproto,
		config:    &c,
		Cache:     cache,
		ParseMode: getStr(c.ParseMode, "Markdown"),
		Logger:    log.New(os.Stderr, "Client - ", log.LstdFlags),
	}

	resp, err := client.InvokeWithLayer(ApiVersion, &InitConnectionParams{
		ApiID:          int32(c.AppID),
		DeviceModel:    getStr(c.DeviceModel, "iPhone X"),
		SystemVersion:  getStr(c.SystemVersion, runtime.GOOS+" "+runtime.GOARCH),
		AppVersion:     getStr(c.AppVersion, "v1.0.0"),
		SystemLangCode: "en",
		LangCode:       "en",
		Query:          &HelpGetConfigParams{},
	})

	if err != nil {
		return nil, errors.Wrap(err, "getting server configs")
	}

	config, ok := resp.(*Config)
	if !ok {
		return nil, errors.New("got wrong response: " + reflect.TypeOf(resp).String())
	}

	dcList := make(map[int]string)
	for _, dc := range config.DcOptions {
		if dc.Cdn {
			continue
		}
		dcList[int(dc.ID)] = net.JoinHostPort(dc.IpAddress, strconv.Itoa(int(dc.Port)))
	}
	client.SetDCList(dcList)
	stop := make(chan struct{})
	client.stop = stop
	client.AppID = int32(c.AppID)
	client.ApiHash = c.AppHash
	c.AllowUpdates = true
	if c.AllowUpdates {
		client.AddCustomServerRequestHandler(HandleUpdate)
	}
	return client, nil
}

func (c *Client) ExportSender(dcID int) (*Client, error) {
	sender, err := c.MTProto.ExportNewSender(dcID)
	if err != nil {
		return nil, err
	}
	fmt.Println("done Export")
	senderClient := &Client{
		MTProto:   sender,
		config:    c.config,
		ParseMode: c.ParseMode,
	}
	authExport, err := senderClient.AuthExportAuthorization(int32(dcID))
	if err != nil {
		return nil, err
	}
	fmt.Println("done Export")
	_, err = senderClient.AuthImportAuthorization(authExport.ID, authExport.Bytes)
	if err != nil {
		return nil, err
	}
	return senderClient, nil
}

func (m *Client) IsSessionRegistred() (bool, error) {
	_, err := m.UsersGetFullUser(&InputUserSelf{})
	if err == nil {
		return true, nil
	}
	var errCode *mtproto.ErrResponseCode
	if errors.As(err, &errCode) {
		if errCode.Message == "AUTH_KEY_UNREGISTERED" {
			return false, nil
		} else if strings.Contains(errCode.Message, "USER_MIGRATE") {
			newDc := errCode.AdditionalInfo.(int)
			m.Logger.Printf("User migrated to DC %d", newDc)
			m.MTProto.SwitchDC(newDc)
		}
	} else {
		return false, err
	}
	return false, nil
}

func (m *Client) Close() {
	close(m.stop)
	m.MTProto.Disconnect()
}

func (m *Client) SetParseMode(mode string) {
	if mode == "" {
		mode = "Markdown"
	}
	for _, c := range []string{"Markdown", "HTML"} {
		if c == mode {
			m.ParseMode = mode
			return
		}
	}
}

func (c *Client) Idle() {
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Wait()
}

// Disconnect client from telegram server
func (c *Client) Disconnect() {
	c.MTProto.Disconnect()
}

// Authorize client with bot token
func (c *Client) LoginBot(botToken string) error {
	_, err := c.AuthImportBotAuthorization(1, c.AppID, c.ApiHash, botToken)
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
	_, SignInerr := c.AuthSignIn(phoneNumber, opts.CodeHash, opts.Code, &EmailVerificationCode{})
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

// Ping telegram server TCP connection
func (c *Client) Ping() time.Duration {
	return c.MTProto.Ping()
}

func (c *Client) ExportSession() (string, error) {
	var session = session.StringSession{}
	authKey, authKeyHash, IpAddr, DcID := c.MTProto.ExportAuth()
	session.AuthKey = authKey
	session.AuthKeyHash = authKeyHash
	session.IpAddr = IpAddr
	session.DCID = DcID
	return session.EncodeToString(), nil
}

func (c *Client) ImportSession(sessionString string) (bool, error) {
	return c.MTProto.ImportAuth(sessionString)
}
