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
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:   apiKey,
		AppHash: apiHash,
		Session: "session",
	})

	if err := client.ConnectBot(botToken); err != nil {
		panic(err)
	}

	client.AddMessageHandler(telegram.OnNewMessage, func(message *telegram.NewMessage) error {
		f, err := client.DownloadMedia(message.Message.Media, &telegram.DownloadOptions{
			CallbackFunc: progressCb,
		})
		if err != nil {
			panic(err)
		}
 
		fmt.Println(f)

		return nil
	})

	fmt.Println("Bot is running...")
	client.Idle()
}

func progressCb(current, total int32) {
	fmt.Printf("Downloaded %s of %s\n", humanBytes(current), humanBytes(total))
}

func humanBytes(bytes int32) string {
	const (
		KB = 1 << 10
		MB = 1 << 20
		GB = 1 << 30
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2fGB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2fMB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2fKB", float64(bytes)/KB)
	}

	return fmt.Sprintf("%dB", bytes)
}
