// Copyright (c) 2022 RoseLoverX

package telegram

import (
	"log"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"time"

	mtproto "github.com/amarnathcjd/gogram"
	"github.com/pkg/errors"

	"github.com/amarnathcjd/gogram/internal/keys"
	"github.com/amarnathcjd/gogram/internal/session"
)

type (
	Client struct {
		*mtproto.MTProto
		config    *ClientConfig
		stop      chan struct{}
		bot       bool
		Cache     *CACHE
		ParseMode string
		AppID     int32
		ApiHash   string
		L         Log
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
//  - dataCenter: data center id (default: 2)
//  - allowUpdates: allow updates (default: true)
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
	}
	mtproto, err := mtproto.NewMTProto(mtproto.Config{
		AuthKeyFile:   c.SessionFile,
		ServerHost:    GetHostIp(dcID),
		PublicKey:     publicKeys[0],
		DataCenter:    dcID,
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
		ParseMode: getStr(c.ParseMode, "HTML"),
		L: Log{
			Logger: log.New(os.Stdout, "", log.LstdFlags),
		},
	}

	resp, err := client.InvokeWithLayer(ApiVersion, &InitConnectionParams{
		ApiID:          int32(c.AppID),
		DeviceModel:    getStr(c.DeviceModel, "Android Device"),
		SystemVersion:  getStr(c.SystemVersion, runtime.GOOS+" "+runtime.GOARCH),
		AppVersion:     getStr(c.AppVersion, "v2.3.6"),
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

func (c *Client) SwitchDC(dcID int) error {
	sender, err := c.MTProto.ReconnectToNewDC(dcID)
	if err != nil {
		return err
	}
	c.MTProto = sender
	if _, err = c.InvokeWithLayer(ApiVersion, &InitConnectionParams{
		ApiID:          c.AppID,
		DeviceModel:    "Gogram",
		SystemVersion:  runtime.GOOS + " " + runtime.GOARCH,
		AppVersion:     Version,
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
	}
	_, e := client.InvokeWithLayer(ApiVersion, &InitConnectionParams{
		ApiID:          c.AppID,
		DeviceModel:    "Gogram",
		SystemVersion:  runtime.GOOS + " " + runtime.GOARCH,
		AppVersion:     Version,
		SystemLangCode: "en",
		LangCode:       "en",
		Query:          &HelpGetConfigParams{},
	})
	if e != nil {
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
	// TODO: implement
	return true
}

func (c *Client) Close() {
	close(c.stop)
	c.MTProto.Disconnect()
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
