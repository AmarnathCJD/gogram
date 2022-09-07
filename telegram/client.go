// Copyright (c) 2022 RoseLoverX

package telegram

import (
	"log"
	"net"
	"os"
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
		bot       bool
		Cache     *CACHE
		ParseMode string
		AppID     int32
		ApiHash   string
		Logger    *log.Logger
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
	senderClient := &Client{
		MTProto:   sender,
		config:    c.config,
		ParseMode: c.ParseMode,
	}

	_, InvokeErr := senderClient.InvokeWithLayer(ApiVersion, &InitConnectionParams{
		ApiID:          int32(c.AppID),
		DeviceModel:    "iPhone X",
		SystemVersion:  getStr("", runtime.GOOS+" "+runtime.GOARCH),
		AppVersion:     getStr("", "v1.0.0"),
		SystemLangCode: "en",
		LangCode:       "en",
		Query:          &HelpGetConfigParams{},
	})
	if InvokeErr != nil {
		return nil, InvokeErr
	}
	if c.MTProto.Addr != senderClient.MTProto.Addr {
		authExport, err := c.AuthExportAuthorization(int32(dcID))
		if err != nil {
			return nil, err
		}
		_, err = senderClient.AuthImportAuthorization(authExport.ID, authExport.Bytes)
		if err != nil {
			return nil, err
		}
	}

	return senderClient, nil
}

func (c *Client) IsUserAuthorized() bool {
	_, err := c.UpdatesGetState()
	return err == nil
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
