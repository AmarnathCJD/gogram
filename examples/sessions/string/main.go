package examples

import (
	"fmt"

	"github.com/bs9/spread_service_gogram/telegram"
)

func main() {
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:   6,               // <App_ID>
		AppHash: "YOUR_APP_HASH", // <App_Hash>
		// StringSession: "<GOGRAM_STRING_SESSION>",
		MemorySession: true,
	})

	if err != nil {
		panic(err)
	}

	client.Conn()       // <Initiate the connection to Telegram Servers>
	client.AuthPrompt() // <Auth Workflow>

	// Print the StringSession
	// This can be used to login without the need of reauthenticating again
	// <Beware: This is sensitive information, do not share it with anyone>
	// Doings so will allow Full Access to your Telegram Account
	fmt.Println("StringSession:", client.ExportSession())

	me, err := client.GetMe()
	if err != nil {
		panic(err)
	}

	fmt.Println(client.JSON(me, true))
}
