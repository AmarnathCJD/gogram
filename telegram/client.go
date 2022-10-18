// Copyright (c) 2022 RoseLoverX

package telegram

import (
	"path/filepath"
	"runtime"
	"sync"
	"time"

	mtproto "github.com/amarnathcjd/gogram"
	"github.com/pkg/errors"

	"github.com/amarnathcjd/gogram/internal/keys"
	"github.com/amarnathcjd/gogram/internal/session"
	"github.com/amarnathcjd/gogram/internal/utils"
)

// Client is the main struct of the library
type Client struct {
	*mtproto.MTProto
	config        *ClientConfig
	stop          chan struct{}
	bot           bool
	Cache         *CACHE
	ParseMode     string
	AppID         int32
	deviceModel   string
	systemVersion string
	appVersion    string
	ApiHash       string
	wg            sync.WaitGroup
	Log           *utils.Logger
}

// ClientConfig is the configuration struct for the client
type ClientConfig struct {
	// Path to session file, default: ./session.session
	SessionFile string
	// String session
	StringSession string
	// Device model, default: Android Device
	DeviceModel string
	// System version, default: runtime.GOOS + " " + runtime.GOARCH
	SystemVersion string
	// App version
	AppVersion string
	// Telegram app id
	AppID int
	// Telegram app hash
	AppHash string
	// Parse mode (Markdown, HTML), default: HTML
	ParseMode string
	// Data center id, default: 4 (Not recommended to change)
	DataCenter int
	// Set log level (debug, info, warn, error, disable), default: info
	LogLevel string
}

// New instance of telegram client,
// If session file is not provided, it will create a new session file
// in the current working directory
//  Params:
//  - sessionFile: path to session file
//  - stringSession: string session
//  - deviceModel: device model
//  - systemVersion: system version
//  - appVersion: app version
//  - appID: telegram app id
//  - appHash: telegram app hash
//  - parseMode: parse mode (Markdown, HTML)
//  - dataCenter: data center id (default: 4)
//
func TelegramClient(c ClientConfig) (*Client, error) {
	c.SessionFile = getStr(c.SessionFile, filepath.Join(workDirectory(), "session.session"))
	dcID := getInt(c.DataCenter, DefaultDC)
	publicKeys, err := keys.GetRSAKeys()
	if err != nil {
		return nil, errors.Wrap(err, "reading public keys")
	}
	if c.AppID == 0 || c.AppHash == "" {
		return nil, errors.New("Your API ID or Hash cannot be empty or None. Please get your own API ID and Hash from https://my.telegram.org/apps")
		// TODO: no need APPID when using string session or session file
	}
	mtproto, err := mtproto.NewMTProto(mtproto.Config{
		AuthKeyFile:   c.SessionFile,
		ServerHost:    GetHostIp(dcID),
		PublicKey:     publicKeys[0],
		DataCenter:    dcID,
		StringSession: c.StringSession,
		LogLevel:      getStr(c.LogLevel, LogInfo),
	})
	if err != nil {
		return nil, errors.Wrap(err, "MTProto client")
	}

	err = mtproto.CreateConnection(true)
	if err != nil {
		return nil, errors.Wrap(err, "creating connection")
	}

	client := &Client{
		MTProto:       mtproto,
		config:        &c,
		Cache:         cache,
		ParseMode:     getStr(c.ParseMode, "HTML"),
		deviceModel:   getStr(c.DeviceModel, "Android Device"),
		systemVersion: getStr(c.SystemVersion, runtime.GOOS+" "+runtime.GOARCH),
		appVersion:    getStr(c.AppVersion, Version),
		Log:           utils.NewLogger("GoGram").SetLevel(getStr(c.LogLevel, LogInfo)),
		AppID:         int32(c.AppID),
		ApiHash:       c.AppHash,
		stop:          make(chan struct{}, 1),
		wg:            sync.WaitGroup{},
	}

	// First request should always be InvokeWithLayer
	if _, err := client.InvokeWithLayer(ApiVersion, &InitConnectionParams{
		ApiID:          int32(c.AppID),
		DeviceModel:    client.deviceModel,
		SystemVersion:  client.systemVersion,
		AppVersion:     client.appVersion,
		SystemLangCode: "en",
		LangCode:       "en",
		Query:          &HelpGetConfigParams{},
	}); err != nil {
		return nil, errors.Wrap(err, "Invoking with layer")
	}

	UpdateHandleDispatcher.client = client
	client.AddCustomServerRequestHandler(HandleIncomingUpdates)
	return client, nil
}

func (c *Client) SwitchDC(dcID int) error {
	sender, err := c.MTProto.ReconnectToNewDC(dcID)
	if err != nil {
		return err
	}
	c.MTProto = sender
	if _, err = c.InvokeWithLayer(ApiVersion, &InitConnectionParams{
		ApiID:          c.AppID,
		DeviceModel:    c.deviceModel,
		SystemVersion:  c.systemVersion,
		AppVersion:     c.appVersion,
		SystemLangCode: "en",
		LangCode:       "en",
		Query:          &HelpGetConfigParams{},
	}); err != nil {
		return err
	}
	return nil
}

func (c *Client) ExportSender(dcID int) (*Client, error) {
	sender, err := c.MTProto.ExportNewSender(dcID, true)
	if err != nil {
		return nil, err
	}
	client := &Client{
		MTProto:   sender,
		config:    c.config,
		ParseMode: c.ParseMode,
		Log:       c.Log,
		AppID:     c.AppID,
		ApiHash:   c.ApiHash,
		stop:      make(chan struct{}, 1),
		wg:        sync.WaitGroup{},
	}
	if _, e := client.InvokeWithLayer(ApiVersion, &InitConnectionParams{
		ApiID:          c.AppID,
		DeviceModel:    c.deviceModel,
		SystemVersion:  c.systemVersion,
		AppVersion:     c.appVersion,
		SystemLangCode: "en",
		LangCode:       "en",
		Query:          &HelpGetConfigParams{},
	}); e != nil {
		return nil, e
	}

	if c.MTProto.Addr != client.MTProto.Addr {
		authExport, err := c.AuthExportAuthorization(int32(dcID))
		if err != nil {
			return nil, err
		}
		_, err = client.AuthImportAuthorization(authExport.ID, authExport.Bytes)
		if err != nil {
			return nil, err
		}
	}
	return client, nil
}

func (c *Client) IsUserAuthorized() (bool, error) {
	_, err := c.UpdatesGetState()
	return err == nil, err
}

func (c *Client) IsConnected() bool {
	return c.MTProto.IsConnected()
}

func (c *Client) Close() {
	close(c.stop)
	c.MTProto.Disconnect()
}

func (c *Client) Idle() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		<-c.stop
	}()
	c.wg.Wait()
}

// Disconnect client from telegram server
func (c *Client) Disconnect() {
	c.MTProto.Disconnect()
}

// Ping telegram server TCP connection
func (c *Client) Ping() time.Duration {
	return c.MTProto.Ping()
}

// Gets the connected DC-ID
func (c *Client) GetDC() int {
	return c.MTProto.GetDC()
}

// IsBot returns true if client is logged in as bot
func (c *Client) IsBot() bool {
	return c.bot
}

func (c *Client) ExportSession() string {
	authKey, authKeyHash, IpAddr, DcID := c.MTProto.ExportAuth()
	return session.StringSession{
		AuthKey:     authKey,
		AuthKeyHash: authKeyHash,
		IpAddr:      IpAddr,
		DCID:        DcID,
	}.EncodeToString()
}

func (c *Client) ImportSession(sessionString string) (bool, error) {
	return c.MTProto.ImportAuth(sessionString)
}

func (c *Client) Terminate() {
	c.MTProto.Terminate()
}
