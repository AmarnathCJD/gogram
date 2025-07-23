package examples

import (
	"github.com/bs9/spread_service_gogram/telegram"
)

// Broadcasting to bot users / chats using updates.GetDifference
// client.Broadcast returns peers in update history via channels
// these peers can be used for broadcasting messages
// no external database is required to store user / chat ids

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

	users, chats, err := client.Broadcast()
	if err != nil {
		panic(err)
	}

	for user := range users {
		client.SendMessage(user, "Hello, This is a broadcast message")
	}

	for chat := range chats {
		client.SendMessage(chat, "Hello, This is a broadcast message")
	}
}
