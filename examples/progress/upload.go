package examples

// Youtube DL Bot Example;
// https://gist.github.com/AmarnathCJD/bfceefe9efd1a079ab151da54ef3bba2

import (
	"fmt"
	"strings"

	"github.com/bs9/spread_service_gogram/telegram"
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

	client.SendMedia(chat, "<file-name>", &telegram.MediaOptions{
		ProgressManager: telegram.NewProgressManager(5).SetMessage(m),
	})

	// to use custom progress manager
	// pm := telegram.NewProgressManager(5)
	// pm.EditFunc(MediaDownloadProgress("<file-name>", m, pm))
	// client.SendMedia(chat, "<file-name>", &telegram.MediaOptions{
	// 	ProgressManager: pm,
	// })

	// same goes for download
	// &DownloadOptions{ProgressManager: NewProgressManager(5).SetMessage(m)}
	// &SendOptions{ProgressManager: NewProgressManager(5).SetMessage(m)}
	// &MediaOptions{ProgressManager: NewProgressManager(5).SetMessage(m)}
}

func MediaDownloadProgress(fname string, editMsg *telegram.NewMessage, pm *telegram.ProgressManager) func(atotalBytes, currentBytes int64) {
	return func(totalBytes int64, currentBytes int64) {
		text := ""
		text += "<b>📄 Name:</b> <code>%s</code>\n"
		text += "<b>💾 File Size:</b> <code>%.2f MiB</code>\n"
		text += "<b>⌛️ ETA:</b> <code>%s</code>\n"
		text += "<b>⏱ Speed:</b> <code>%s</code>\n"
		text += "<b>⚙️ Progress:</b> %s <code>%.2f%%</code>"

		size := float64(totalBytes) / 1024 / 1024
		eta := pm.GetETA(currentBytes)
		speed := pm.GetSpeed(currentBytes)
		percent := pm.GetProgress(currentBytes)

		progressbar := strings.Repeat("■", int(percent/10)) + strings.Repeat("□", 10-int(percent/10))

		message := fmt.Sprintf(text, fname, size, eta, speed, progressbar, percent)
		editMsg.Edit(message)
	}
}
