package telegram

import (
	"net"
	"os"
	"runtime"
	"strconv"

	"fmt"

	"github.com/pkg/errors"

	mtproto "github.com/amarnathcjd/gogram"
	"github.com/amarnathcjd/gogram/internal/keys"
)

type Client struct {
	*mtproto.MTProto
	config       *ClientConfig
	serverConfig *Config
}

type ClientConfig struct {
	SessionFile     string
	ServerHost      string
	PublicKeysFile  string
	DeviceModel     string
	SystemVersion   string
	AppVersion      string
	AppID           int
	AppHash         string
	InitWarnChannel bool
}

const (
	warnChannelDefaultCapacity = 100
)

func NewClient(c ClientConfig) (*Client, error) {
	if _, err := os.Stat(c.PublicKeysFile); err != nil {
		return nil, fmt.Errorf("publickeysfile not found")
	}
	if c.DeviceModel == "" {
		c.DeviceModel = "Unknown"
	}

	if c.SystemVersion == "" {
		c.SystemVersion = runtime.GOOS + "/" + runtime.GOARCH
	}

	if c.AppVersion == "" {
		c.AppVersion = "v0.0.0"
	}

	publicKeys, err := keys.ReadFromFile(c.PublicKeysFile)
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

	if c.InitWarnChannel {
		m.Warnings = make(chan error, warnChannelDefaultCapacity)
	}

	err = m.CreateConnection()
	if err != nil {
		return nil, errors.Wrap(err, "creating connection")
	}

	client := &Client{
		MTProto: m,
		config:  &c,
	}

	//client.AddCustomServerRequestHandler(client.handleSpecialRequests())

	resp, err := client.InvokeWithLayer(139, &InitConnectionParams{
		ApiID:          int32(c.AppID),
		DeviceModel:    c.DeviceModel,
		SystemVersion:  c.SystemVersion,
		AppVersion:     c.AppVersion,
		SystemLangCode: "en",
		LangCode:       "en",
		Query:          &HelpGetConfigParams{},
	})

	if err != nil {
		return nil, err
	}

	config, ok := resp.(*Config)
	if !ok {
		return nil, fmt.Errorf("got wrong response")
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

/*
func (c *Client) handleSpecialRequests() func(any) bool {
	return func(i any) bool {
		switch msg := i.(type) {
		case *UpdatesObj:
			pp.Println(msg, "UPDATE")
			return true
		case *UpdateShort:
			pp.Println(msg, "SHORT UPDATE")
			return true
		}

		return false
	}
}
*/
//----------------------------------------------------------------------------
