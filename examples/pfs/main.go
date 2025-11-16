package main

import (
	"fmt"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
)

// Fill these with your real credentials before running.
const (
	appID    = 6              // your api_id from my.telegram.org
	appHash  = "YOUR_APP_HASH" // your api_hash from my.telegram.org
	botToken = "YOUR_BOT_TOKEN"
)

func main() {
	if appID == 0 || appHash == "YOUR_APP_HASH" || botToken == "YOUR_BOT_TOKEN" {
		panic("please set appID, appHash, and botToken in examples/pfs/main.go before running")
	}

	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:     appID,
		AppHash:   appHash,
		Session:   "pfs.session",
		LogLevel:  telegram.LogDebug,
		EnablePFS: true,
	})
	if err != nil {
		panic(fmt.Errorf("creating client: %w", err))
	}

	if err := client.LoginBot(botToken); err != nil {
		panic(fmt.Errorf("login bot: %w", err))
	}

	me, err := client.GetMe()
	if err != nil {
		panic(fmt.Errorf("get me: %w", err))
	}

	text := fmt.Sprintf("PFS test: %s (user=%s)", time.Now().Format(time.RFC3339), me.Username)
	client.SendMessage("me", text)

	fmt.Println("PFS test message sent. Check debug logs for PFS manager, temp auth key creation, and binding.")

	// Keep the client running so that PFS manager can rotate keys over time if desired.
	client.Idle()
}
