package main

import (
	"fmt"

	"github.com/amarnathcjd/gogram/telegram"
)

// supported proxy types
// socks5, sock4, http, https

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	phoneNum = "+YOUR_PHONE_NUMBER"
)

func main() {
	// socks5Proxy := &telegram.Socks5Proxy{
	// 	BaseProxy: telegram.BaseProxy{
	// 		Host: "127.0.0.1",
	// 		Port: 8080,
	// 	},
	// }

	socks, _ := telegram.ProxyFromURL("socks5://127.0.0.1:8080")

	// create a new client object
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
		Proxy:    socks, // you can also use socks5Proxy here
	})

	// authenticate the client using the bot token
	// this will send a code to the phone number if it is not already authenticated
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
