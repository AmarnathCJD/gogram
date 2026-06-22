package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	peerUsername     = "roseloverx"
	stickerShortName = "fErHs_1960469142_by_YujiItadoriBot"
)

func main() {
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:   2040,
		AppHash: "b18441a1ff607e10a989891a5462e627",
		Session: "secret-chat.session",
	})
	if err != nil {
		panic(err)
	}

	if err := client.Connect(); err != nil {
		panic(fmt.Errorf("Connect: %w", err))
	}
	if authed, _ := client.IsAuthorized(); !authed {
		fmt.Print("Enter phone number (e.g. +14155551234): ")
		var phone string
		fmt.Scanln(&phone)
		if _, err := client.Login(phone); err != nil {
			panic(fmt.Errorf("Login: %w", err))
		}
	}

	ready := make(chan int32, 1)

	client.OnE2EMessage(func(update telegram.Update, c *telegram.Client) error {
		if u, ok := update.(*telegram.UpdateEncryption); ok {
			if chat, ok := u.Chat.(*telegram.EncryptedChatObj); ok {
				fmt.Println("Secret chat ready with ID:", chat.ID)
				select {
				case ready <- chat.ID:
				default:
				}
			}
		}
		return nil
	})

	chat, err := client.RequestSecretChat(peerUsername)
	if err != nil {
		panic(fmt.Errorf("RequestSecretChat: %w", err))
	}
	fmt.Println("Requested secret chat:", chat.ID, "- waiting for peer to accept...")

	chatID := <-ready

	if _, err := client.SendSecretMessage(chatID, "hello", 0); err != nil {
		fmt.Println("SendSecretMessage error:", err)
	} else {
		fmt.Println("Sent: hello")
	}

	fmt.Print("Press Enter to send the first sticker from the set... ")
	bufio.NewReader(os.Stdin).ReadString('\n')

	stickerSet, err := client.MessagesGetStickerSet(
		&telegram.InputStickerSetShortName{ShortName: stickerShortName},
		0,
	)
	if err != nil {
		panic(fmt.Errorf("MessagesGetStickerSet: %w", err))
	}

	setObj, ok := stickerSet.(*telegram.MessagesStickerSetObj)
	if !ok {
		panic(fmt.Errorf("unexpected sticker set type: %T", stickerSet))
	}
	if len(setObj.Documents) == 0 {
		panic("sticker set has no documents")
	}

	first := setObj.Documents[0]

	if err := client.SendSecretFile(chatID, first, &telegram.SecretFileOptions{}); err != nil {
		fmt.Println("SendSecretFile error:", err)
	} else {
		fmt.Println("Sent sticker to secret chat.")
	}

	client.Idle()
}
