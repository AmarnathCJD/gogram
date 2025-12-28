package main

import (
	"fmt"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	botToken = "YOUR_BOT_TOKEN"
)

func main() {
	// Create a new client
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
	})

	// Connect to the server
	if err := client.Connect(); err != nil {
		panic(err)
	}

	// Authenticate the client using the bot token
	if err := client.LoginBot(botToken); err != nil {
		panic(err)
	}

	// Add a inline query handler
	client.OnInlineQuery("hello", HelloWorld)
	client.OnInlineQuery("multiple", SendMultipleResults)

	// Start polling
	client.Idle()
}

func HelloWorld(i *telegram.InlineQuery) error {
	builder := i.Builder()
	builder.Article("Hello World", "Hello World", "This is a test article")
	_, err := i.Answer(builder.Results())
	return err
}

func SendMultipleResults(i *telegram.InlineQuery) error {
	builder := i.Builder()
	for j := 1; j <= 10; j++ {
		builder.Article(fmt.Sprintf("Article %d", j), "description", "This is a test article").
			WithLinkPreview(false)
	}

	builder.CacheTime(60)                  // Cache results for 1 minute
	builder.SwitchPM("Start Bot", "start") // Add a button to start the bot in PM
	if err := builder.Error(); err != nil {
		return err
	}
	builder.Answer()

	return nil
}
