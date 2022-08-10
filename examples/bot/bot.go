// Example of using gogram for bot.

package examples

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 3
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
	client.LoginBot(botToken)
	me, _ := client.GetMe()
	fmt.Println("Logged in as @", me.Username)
	client.AddEventHandler(telegram.Command{Cmd: "start", Prefix: "/?."}, Start)
	client.AddEventHandler("/ping", Ping)
	client.AddEventHandler("[/!?]js|json", Jsonify)
	client.AddEventHandler(telegram.OnNewMessage, Echo)
	client.AddEventHandler("/imdb", Imdb)
	client.AddEventHandler("/info", Info)
	client.Idle()
}

func Start(m *telegram.NewMessage) error {
	_, err := m.Client.SendMessage(m.ChatID(), "Hello, I'm a bot!")
	return err
}

func Ping(m *telegram.NewMessage) error {
	CurrentTime := time.Now()
	msg, _ := m.Reply("Pinging...")
	_, err := msg.Edit(telegram.Ent().Bold("Pong!! ").Code(time.Since(CurrentTime).String()))
	return err
}

func Jsonify(m *telegram.NewMessage) error {
	if m.IsReply() {
		r, errr := m.GetReplyMessage()
		if errr != nil {
			return errr
		}
		_, err := m.Reply(telegram.Ent().Code(r.Marshal()))
		return err
	}
	_, err := m.Reply(telegram.Ent().Code(m.Marshal()))
	return err
}

func Echo(m *telegram.NewMessage) error {
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

func Info(m *telegram.NewMessage) error {
	var peer telegram.InputPeer
	if m.IsReply() {
		r, err := m.GetReplyMessage()
		if err != nil {
			return err
		}
		peer, err = m.Client.GetSendablePeer(r.SenderID())
		if err != nil {
			return err
		}
	} else {
		if m.Args() == "" {
			peer, _ = m.Client.GetSendablePeer(m.SenderID())
		} else {
			peer, _ = m.Client.GetSendablePeer(m.Args())
		}
	}
	switch peer := peer.(type) {
	case *telegram.InputPeerUser:
		user, err := m.Client.Cache.GetUser(peer.UserID)
		if err != nil {
			_, err := m.Reply("User not found")
			return err
		}
		b, err := json.MarshalIndent(user, "", "    ")
		if err != nil {
			_, err := m.Reply("Error")
			return err
		}
		_, err = m.Reply(string(b))
		return err
	default:
		_, err := m.Reply(fmt.Sprintf("Peer not found, peer type: %T", peer))
		return err
	}
}
