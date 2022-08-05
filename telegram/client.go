// Copyright (c) 2020-2021 KHS Films
//
// This file is a part of mtproto package.
// See https://github.com/amarnathcjd/gogram/blob/master/LICENSE for details

package telegram

import (
	"net"
	"reflect"
	"runtime"
	"strconv"

	mtproto "github.com/amarnathcjd/gogram"
	"github.com/pkg/errors"

	"github.com/amarnathcjd/gogram/internal/keys"
)

type Client struct {
	*mtproto.MTProto
	config       *ClientConfig
	serverConfig *Config
	stop         chan struct{}
	Cache        *CACHE
	ParseMode    string
}

type ClientConfig struct {
	SessionFile    string
	ServerHost     string
	PublicKeysFile string
	DeviceModel    string
	SystemVersion  string
	AppVersion     string
	AppID          int
	AppHash        string
	ParseMode      string
}

func NewClient(c ClientConfig) (*Client, error) {
	if c.DeviceModel == "" {
		c.DeviceModel = "Unknown"
	}
	if c.SystemVersion == "" {
		c.SystemVersion = runtime.GOOS + "/" + runtime.GOARCH
	}
	if c.AppVersion == "" {
		c.AppVersion = "v0.0.0"
	}
	publicKeys, err := keys.ReadFromFile("tg_public_keys.pem")
	if err != nil {
		return nil, errors.Wrap(err, "reading public keys")
	}
	m, err := mtproto.NewMTProto(mtproto.Config{
		AuthKeyFile: c.SessionFile,
		ServerHost:  c.ServerHost,
		PublicKey:   publicKeys[0],
	})
	if err != nil {
		return nil, errors.Wrap(err, "setup common MTProto client")
	}

	err = m.CreateConnection()
	if err != nil {
		return nil, errors.Wrap(err, "creating connection")
	}
	var ParseMode = c.ParseMode
	if ParseMode == "" {
		ParseMode = "Markdown"
	}

	client := &Client{
		MTProto:   m,
		config:    &c,
		Cache:     cache,
		ParseMode: ParseMode,
	}

	resp, err := client.InvokeWithLayer(ApiVersion, &InitConnectionParams{
		ApiID:          int32(c.AppID),
		DeviceModel:    c.DeviceModel,
		SystemVersion:  c.SystemVersion,
		AppVersion:     c.AppVersion,
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

	client.serverConfig = config

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
	return client, nil
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
		}
		return false, err
	} else {
		return false, err
	}
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
