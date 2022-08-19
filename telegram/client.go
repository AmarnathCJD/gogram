// Copyright (c) 2020-2021 KHS Films
//
// This file is a part of mtproto package.
// See https://github.com/amarnathcjd/gogram/blob/master/LICENSE for details

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
	}

	ClientConfig struct {
		SessionFile   string
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
	c.SessionFile = Or(c.SessionFile, workDir+"/tg_session.json")
	publicKeys, err := keys.ReadFromNetwork()
	if err != nil {
		return nil, errors.Wrap(err, "reading public keys")
	}
	m, err := mtproto.NewMTProto(mtproto.Config{
		AuthKeyFile: c.SessionFile,
		ServerHost:  GetHostIp(c.DataCenter),
		PublicKey:   publicKeys[0],
		DataCenter:  c.DataCenter,
	})
	if err != nil {
		return nil, errors.Wrap(err, "MTProto client")
	}

	err = m.CreateConnection()
	if err != nil {
		return nil, errors.Wrap(err, "creating connection")
	}

	client := &Client{
		MTProto:   m,
		config:    &c,
		Cache:     cache,
		ParseMode: Or(c.ParseMode, "Markdown"),
		Logger:    log.New(os.Stderr, "Client - updates - ", log.LstdFlags),
	}

	resp, err := client.InvokeWithLayer(ApiVersion, &InitConnectionParams{
		ApiID:          int32(c.AppID),
		DeviceModel:    Or(c.DeviceModel, "iPhone X"),
		SystemVersion:  Or(c.SystemVersion, runtime.GOOS+" "+runtime.GOARCH),
		AppVersion:     Or(c.AppVersion, "v1.0.0"),
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
	go client.PingInfinity()
	return client, nil
}

func (*PingParams) CRC() uint32 {
	return 0x7abe77ec
}

func (c *Client) PingInfinity() {
	for {
		select {
		case <-time.After(time.Second * 20):
			err := c.MTProto.MakeRequestWithoutUpdates(&PingParams{
				PingID: 123456789,
			})
			if err != nil {
				c.Logger.Println(errors.Wrap(err, "pinging server"))
			}
		case <-c.stop:
			return
		}
	}
}

func (c *Client) PingInfinityh() {
	go func() {
		for range time.Tick(time.Second * 2) {
			fmt.Println("ping")
			c.MakeRequest(&PingParams{
				PingID: 123456789,
			})
		}
	}()
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
			return false, errors.New("user migrated")
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

func (c *Client) Disconnect() {
	c.MTProto.Disconnect()
}

// Authorize client with bot token
func (c *Client) LoginBot(botToken string) error {
	_, err := c.AuthImportBotAuthorization(1, c.AppID, c.ApiHash, botToken)
	return err
}
