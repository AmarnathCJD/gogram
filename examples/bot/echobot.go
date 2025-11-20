package main

import (
	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	botToken = "YOUR_BOT_TOKEN"
)

func main() {
	// create a new client object
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
	})

	client.LoginBot(botToken)

	client.On(telegram.OnMessage, func(message *telegram.NewMessage) error {
		message.Respond(message)
		return nil
	}, telegram.FilterPrivate)

	client.On("message:/start", func(message *telegram.NewMessage) error {
		message.Reply("Hello, I am a bot!")
		return nil
	})

	// lock the main routine
	client.Idle()
}
