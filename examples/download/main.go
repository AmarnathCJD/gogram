package main

import (
	"time"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	botToken = "YOUR_BOT_TOKEN"
)

func main() {
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
	})

	client.LoginBot(botToken)

	client.On(telegram.OnMessage, func(message *telegram.NewMessage) error {
		if !message.IsMedia() {
			return nil
		}

		pm := telegram.NewProgressManager(5).SetMessage(message)

		_, err := message.Download(&telegram.DownloadOptions{
			ProgressManager: pm,
			Timeout:         2 * time.Minute,
		})
		if err != nil {
			message.Reply("download error: " + err.Error())
			return err
		}

		message.Reply("download finished")
		return nil
	}, telegram.FilterPrivate)

	client.Idle()
}
