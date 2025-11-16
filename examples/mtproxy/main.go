package main

import (
	"fmt"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID      = 6
	appHash    = "YOUR_APP_HASH"
	botToken   = "YOUR_BOT_TOKEN"
	mtproxyURL = "https://t.me/proxy?server=YOUR_PROXY_SERVER&port=443&secret=YOUR_PROXY_SECRET"
)

func main() {
	proxy, err := telegram.ProxyFromURL(mtproxyURL)
	if err != nil {
		panic(err)
	}

	// proxy := &telegram.MTProxy{
	// 	BaseProxy: telegram.BaseProxy{
	// 		Host: "YOUR_PROXY_SERVER",
	// 		Port: 443,
	// 	},
	// 	Secret: "YOUR_PROXY_SECRET",
	// } // Alternative way to define MTProxy

	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
		Proxy:    proxy,
	})

	if err := client.LoginBot(botToken); err != nil {
		panic(err)
	}

	me, err := client.GetMe()
	if err != nil {
		panic(err)
	}

	client.SendMessage("me", fmt.Sprintf("Hello from gogram via MTProxy, %s!", me.FirstName))
	fmt.Println("Connected via MTProxy as", me.Username)
}
