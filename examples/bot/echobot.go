package examples

import (
	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "app_hash"
	botToken = "bot_token"
)

func main() {
	// Create a new client
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:   appID,
		AppHash: appHash,
	})

	// Establish connection with the telegram server
	if err := client.Connect(); err != nil {
		panic(err)
	}

	// Authenticate the client using the bot token
	if err := client.Start(); err != nil {
		panic(err)
	}

	// Add a message handler for echoing the messages back
	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Respond(m)
		return nil
	})

	// Add a message handler for "/start" command
	client.AddMessageHandler("/start", func(message *telegram.NewMessage) error {
		var buttonBuilder = telegram.Button{}
		message.Reply("Hello, I am a bot!", telegram.SendOptions{
			ReplyMarkup: buttonBuilder.Keyboard(
				buttonBuilder.Row(
					buttonBuilder.URL(
						"Gogram Repo", "github.com/amarnathcjd/gogram",
					),
				),
			),
			ParseMode:   telegram.HTML,
			LinkPreview: true,
			NoForwards:  true,
		})

		return nil
	})

	// Hold the current goroutine till client.Stop() is called.
	client.Idle()
}
