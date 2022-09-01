package examples

import (
	"fmt"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID   = 6
	appHash = ""
)

func main() {
	client, err := telegram.TelegramClient(telegram.ClientConfig{
		AppID:         appID,
		AppHash:       appHash,
		DataCenter:    5,  // Working on a fix for this, (This should be acurate)
		StringSession: "", // (if this value is specified, client.Login is not Necessary.)
	})

	if err != nil {
		panic(err)
	}

	// Login with phone number, if you have a string session, you can skip this step.
	// Code AuthFlow implemented
	phoneNumber := "+1234567890"
	if authed, err := client.Login(phoneNumber); !authed {
		panic(err)
	}

	stringSession, _ := client.ExportSession()
	fmt.Println("String Session: ", stringSession)

	me, _ := client.GetMe()
	fmt.Printf("Logged in as %s\n", me.Username)

	// Add handlers
	client.AddMessageHandler("/start", Start)
	client.Idle() // Blocks until client.Stop() is called
}

func Start(m *telegram.NewMessage) error {
	_, err := m.Reply("Hello World")
	return err
}
