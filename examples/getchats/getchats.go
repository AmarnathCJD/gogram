package examples

import (
	"fmt"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	phoneNum = "YOUR_PHONE_NUMBER"
)

func main() {
	// Create a new client
	client, _ := telegram.TelegramClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
		// StringSession: "", // Uncomment this line to use string session
	})

	// Authenticate the client using the bot token
	// This will send a code to the phone number if it is not already authenticated
	if _, err := client.Login(phoneNum); err != nil {
		panic(err)
	}

	dialogs, err := client.GetDialogs()
	if err != nil {
		panic(err)
	}
	for _, dialog := range dialogs {
		switch d := dialog.(type) {
		case *telegram.DialogObj:
			fmt.Println(d.TopMessage)
		}
	}
}
