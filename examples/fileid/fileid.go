package examples

import (
	"github.com/bs9/spread_service_gogram/telegram"
)

// https://gist.github.com/AmarnathCJD/824907f8ec9e7aa9cec8278937fc437b
// use this code to convert bot-api file id to gogram file id

// https://gist.github.com/AmarnathCJD/27626c8fc1b5d5234576d1eecb5d651f
// use this code to convert pyrogram file id to gogram file id

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	phoneNum = "YOUR_PHONE_NUMBER"
)

func main() {
	// Create a new client
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
	})

	client.Conn() // Connect to the telegram server

	// Authentication
	client.LoginBot("<BOT_TOKEN_HERE>")

	client.On(telegram.OnMessage, func(msg *telegram.NewMessage) error {
		if msg.Media() != nil {
			fileID := msg.File.FileID // or telegram.PackBotFileID(msg.Media())

			msg.Respond("File ID: " + fileID)

			fileRetained, err := telegram.ResolveBotFileID(fileID)
			if err != nil {
				return err
			}

			msg.RespondMedia(fileRetained)
		}

		return nil
	})
}
