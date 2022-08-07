package examples

// Example of using gogram for bot.

import (
	"fmt"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = ""
	botToken = ""
)

func main() {
	// Create a new client
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:      appID,
		AppHash:    appHash,
		DataCenter: 5, // Working on a fix for this
		ParseMode:  "HTML",
	})
	if err != nil {
		panic(err)
	}
	// login with bot token
	client.AuthImportBotAuthorization(1, appID, appHash, botToken)
	me, _ := client.GetMe()
	fmt.Printf("Logged in as @%s\n", me.Username)
	// add handlers
	client.AddEventHandler("/start", Start)
	client.AddEventHandler("/ping", Ping)
	client.AddEventHandler("[/!?]js|json", Jsonify)
	client.AddEventHandler("/echo", Echo)
	// infinite loop
	client.Idle()
}

func Start(c *telegram.Client, m *telegram.NewMessage) error {
	_, err := c.SendMessage(m.ChatID(), "Hello, I'm a bot!")
	return err
}

func Ping(c *telegram.Client, m *telegram.NewMessage) error {
	CurrentTime := time.Now()
	m, _ = m.Reply("Pinging...")
	_, err := m.Edit(fmt.Sprintf("Pong! %s", time.Since(CurrentTime)))
	return err
}

func Jsonify(c *telegram.Client, m *telegram.NewMessage) error {
	_, err := m.Reply(fmt.Sprintf("<code>%s</code>", m.Marshal()))
	return err
}

func Echo(c *telegram.Client, m *telegram.NewMessage) error {
	if m.Args() == "" {
		m.Reply("Please specify a message to echo")
		return nil
	}
	_, err := m.Reply(m.Args())
	return err
}
