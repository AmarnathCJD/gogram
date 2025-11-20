package main

import (
	"fmt"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	apiKey   = 6
	apiHash  = ""
	botToken = ""
)

func main() {
	// create a new client object
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:   apiKey,
		AppHash: apiHash,
	})

	if err := client.LoginBot(botToken); err != nil {
		panic(err)
	}

	client.AddRawHandler(&telegram.UpdateBotMessageReaction{}, func(m telegram.Update, c *telegram.Client) error {
		fmt.Println(m.(*telegram.UpdateBotMessageReaction).NewReactions)
		return nil
	})

	client.AddMessageHandler(telegram.OnNewMessage, func(msg *telegram.NewMessage) error {
		client.MessagesSendReaction(&telegram.MessagesSendReactionParams{
			Big:         true,
			AddToRecent: false,
			Peer:        msg.Peer,
			MsgID:       msg.ID,
			Reaction: []telegram.Reaction{
				&telegram.ReactionEmoji{
					Emoticon: "👍",
				},
			},
		})

		// or

		// msg.React("👍")

		return nil
	})

	client.Idle()
}
