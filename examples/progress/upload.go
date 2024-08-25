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

	var pm *telegram.ProgressManager
	chat, _ := client.ResolvePeer("chatId")
	m, _ := client.SendMessage(chat, "Starting File Upload...")

	client.SendMedia(chat, "<file-name>", &telegram.MediaOptions{
		ProgressCallback: func(total, curr int64) {
			if pm == nil {
				pm = telegram.NewProgressManager(total, 5) // 5 seconds edit interval
			}
			if pm.ShouldEdit() {
				client.EditMessage(chat, m.ID, pm.GetStats(curr))
			}
		},
	})
}
