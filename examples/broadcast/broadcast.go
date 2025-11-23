package main

import (
	"context"
	"log"

	"github.com/amarnathcjd/gogram/telegram"
)

// Broadcasting to bot users / chats using updates.GetDifference
// Broadcast now invokes callbacks for each unique peer instead of returning channels.
// No external database is required to store user / chat ids.

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	botToken = "YOUR_BOT_TOKEN"
)

func main() {
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
	})
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}

	if err := client.LoginBot(botToken); err != nil {
		log.Fatalf("bot login failed: %v", err)
	}

	ctx := context.Background()
	message := "Hello, this is a broadcast message"

	userCallback := func(user telegram.User) error {
		_, err := client.SendMessage(user, message)
		return err
	}

	chatCallback := func(chat telegram.Chat) error {
		_, err := client.SendMessage(chat, message)
		return err
	}

	if err := client.Broadcast(ctx, userCallback, chatCallback); err != nil {
		log.Fatalf("broadcast failed: %v", err)
	}
}
