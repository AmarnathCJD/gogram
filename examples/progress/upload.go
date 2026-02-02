package examples

// Youtube DL Bot Example;
// https://gist.github.com/AmarnathCJD/bfceefe9efd1a079ab151da54ef3bba2

import (
	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	apiKey   = ""
	botToken = ""
)

func main() {
	// create a new client object
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:   appID,
		AppHash: apiKey,
	})

	client.LoginBot(botToken)

	chat, _ := client.ResolvePeer("chatId")
	m, _ := client.SendMessage(chat, "Starting File Upload...")

	var pm = telegram.NewProgressManager(5)
	pm.Edit(func(a, b int64) {
		client.EditMessage(chat, m.ID, pm.GetStats(a))
	})

	client.SendMedia(chat, "<file-name>", &telegram.MediaOptions{
		ProgressManager: pm,
	})
}
