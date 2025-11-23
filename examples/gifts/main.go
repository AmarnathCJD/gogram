package main

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
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:   appID,
		AppHash: appHash,
	})
	client.Conn()
	client.AuthPrompt()

	// get all your unique gifts and send the first one to a user
	gifts, _ := client.GetMyGifts(true)
	for _, gift := range gifts {
		gft := (*gift).(*telegram.StarGiftUnique)

		// transfer the gift to the user
		client.SendGift("roseloverx", gft.ID, "Here is a gift for you!")

		break
	}

	// buying new gifts and sending them to users
	availGifts, _ := client.PaymentsGetStarGifts(0)
	for _, gift := range availGifts.(*telegram.PaymentsStarGiftsObj).Gifts {
		gft := gift.(*telegram.StarGiftObj)

		// buy and send the gift
		client.SendNewGift("roseloverx", gft.ID, "Here is a gift for you!")

		break
	}

	// add a handler on receiving gifts
	client.AddActionHandler(func(m *telegram.NewMessage) error {
		if action, ok := m.Action.(*telegram.MessageActionStarGift); ok {
			fmt.Println("Gift received", action.Gift)
		} else if action, ok := m.Action.(*telegram.MessageActionStarGiftUnique); ok {
			fmt.Println("Unique gift received", action.Gift)
		}

		return nil
	})
}
