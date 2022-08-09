// Example of using gogram for bot.

package main

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
	client.AddEventHandler(telegram.Command("start", "/?."), Start)
	client.AddEventHandler("/ping", Ping)
	client.AddEventHandler("[/!?]js|json", Jsonify)
	client.AddEventHandler(telegram.OnNewMessage, Echo)
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
	_, err := msg.Edit(telegram.Ent().Bold("Pong!! ").Code(time.Since(CurrentTime).String()))
	return err
}

func Jsonify(c *telegram.Client, m *telegram.NewMessage) error {
	_, err := m.Reply(telegram.Ent().Code(m.Marshal()))
	return err
}

func Echo(c *telegram.Client, m *telegram.NewMessage) error {
	if (m.Text() == "" && !m.IsMedia()) || !m.IsPrivate() || m.IsCommand() {
		return nil
	}
	if m.IsMedia() {
		_, err := m.Respond(m.Media())
		return err
	}
	_, err := m.Respond(m.Text())
	return err
}
