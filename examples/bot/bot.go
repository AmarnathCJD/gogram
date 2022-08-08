// Example of using gogram for bot.

package main

import (
	"fmt"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 3138242
	appHash  = "9ff85074c961b349e6dad943e9b20f54"
	botToken = "5240997485:AAFI2yoHILM0VVlxXwc3RIkiCIq63QMBY9A"
)

func main() {
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:      appID,
		AppHash:    appHash,
		DataCenter: 5, // Working on a fix for this
		ParseMode:  "HTML",
	})
	if err != nil {
		panic(err)
	}
	me, _ := client.GetMe()
	fmt.Println("Logged in as @", me.Username)
	client.AddEventHandler("/start", Start)
	client.AddEventHandler("/ping", Ping)
	client.AddEventHandler("[/!?]js|json", Jsonify)
	client.AddEventHandler("/echo", Echo)
	client.AddEventHandler("/imdb", Imdb)
	client.Idle()
}

func Start(c *telegram.Client, m *telegram.NewMessage) error {
	_, err := c.SendMessage(m.ChatID(), "Hello, I'm a bot!")
	return err
}

func Ping(c *telegram.Client, m *telegram.NewMessage) error {
	CurrentTime := time.Now()
	msg, _ := m.Reply("Pinging...")
	fmt.Println(msg.ChatID())
	_, err := msg.Edit(fmt.Sprintf("Pong! %s", time.Since(CurrentTime)))
	fmt.Println(err)
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
