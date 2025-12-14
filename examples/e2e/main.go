package main

import (
	"fmt"

	"github.com/amarnathcjd/gogram/telegram"
)

func main() {
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:   6,
		AppHash: "eb06d4abfb49dc3eeb1aeb98ae0f581e",
	})
	if err != nil {
		panic(err)
	}

	// Authenticate the client
	if _, err := client.Login(""); err != nil {
		panic(err)
	}

	// Request a secret chat with a user
	fmt.Println(client.RequestSecretChat("xloverrose"))

	// Handle E2E encrypted messages
	client.OnE2EMessage(func(update telegram.Update, c *telegram.Client) error {
		switch u := update.(type) {
		case *telegram.UpdateEncryption:
			switch chat := u.Chat.(type) {
			case *telegram.EncryptedChatRequested:
				// Accept the incoming secret chat request
				err := c.AcceptSecretChat(telegram.InputEncryptedChat{
					ChatID:     chat.ID,
					AccessHash: chat.AccessHash,
				}, chat.GA)
				if err != nil {
					fmt.Println("Error accepting secret chat:", err)
				} else {
					fmt.Println("Accepted secret chat with ID:", chat.ID)
					fmt.Println(c.SendSecretMessage(chat.ID, "Hello from responder!", 0))
				}
			case *telegram.EncryptedChatDiscarded:
				fmt.Println("Secret chat was discarded")
			case *telegram.EncryptedChatObj:
				fmt.Println("Secret chat is ready with ID:", chat.ID)
			}

		case *telegram.UpdateNewEncryptedMessage:
			switch msg := u.Message.(type) {
			case *telegram.EncryptedMessageObj:
				// Decrypt the incoming message
				obj, err := c.DecryptSecretMessage(msg.ChatID, msg.Bytes)
				if err != nil {
					fmt.Println("Error decrypting message:", err)
					return nil
				}

				fmt.Printf("Decrypted message in chat %d: %+v\n", msg.ChatID, c.JSON(obj.Message))

				// Echo back with a file
				err = c.SendSecretFile(msg.ChatID, "tl.json", &telegram.SecretFileOptions{
					Caption: "Echoing your message!",
				})
				if err != nil {
					fmt.Println("Error sending secret file:", err)
				}
			}
		}
		return nil
	})

	// Keep the client running
	client.Idle()
}
