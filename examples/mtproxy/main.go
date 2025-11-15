package main

import (
	"fmt"
	"net/url"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID     = 6
	appHash   = "YOUR_APP_HASH"
	botToken  = "YOUR_BOT_TOKEN"
	mtproxyURL = "mtproxy://<secret>@<host>:<port>" // replace with your MTProxy URL
)

func main() {
	proxyURL, err := url.Parse(mtproxyURL)
	if err != nil {
		panic(err)
	}

	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
		Proxy:    proxyURL,
	})

	if err := client.LoginBot(botToken); err != nil {
		panic(err)
	}

	me, err := client.GetMe()
	if err != nil {
		panic(err)
	}

	client.SendMessage("me", fmt.Sprintf("Hello from gogram via MTProxy FakeTLS, %s!", me.FirstName))
	fmt.Println("Connected via MTProxy as", me.Username)
}
