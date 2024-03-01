package main

import (
	"fmt"
	"net/url"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	phoneNum = "+YOUR_PHONE_NUMBER"
)

func main() {
	// Create a new client
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
		SocksProxy: &url.URL{
			Scheme: "socks5",
			Host:   "127.0.0.1:1080",
			// User:   url.UserPassword("username", "password"),
		},
	})

	// Connect to the server
	if err := client.Connect(); err != nil {
		panic(err)
	}

	// Authenticate the client using the bot token
	// This will send a code to the phone number if it is not already authenticated
	if _, err := client.Login(phoneNum); err != nil {
		panic(err)
	}

	// Do something with the client
	// ...
	me, err := client.GetMe()
	if err != nil {
		panic(err)
	}
	client.SendMessage("me", fmt.Sprintf("Hello, %s!", me.FirstName))
	fmt.Println("Logged in as", me.Username)
}
