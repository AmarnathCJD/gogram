package main

// Youtube DL Bot Example;
// https://gist.github.com/AmarnathCJD/bfceefe9efd1a079ab151da54ef3bba2

import (
	"fmt"

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

	client.SendMedia(chat, "<file-name>", &telegram.MediaOptions{
		Upload: &telegram.UploadOptions{
			ProgressCallback: func(pi *telegram.ProgressInfo) {
				m.Edit(fmt.Sprintf("Uploading... %.2f%% complete (%.2f MB/s), ETA: %.2f seconds",
					pi.Percentage,
					pi.CurrentSpeed/1024/1024,
					pi.ETA,
				))
			},
			ProgressInterval: 5, // update every 5 seconds
		},
	})

	// to use custom progress manager
	// pm := telegram.NewProgressManager(5)
	// pm.EditFunc(MediaDownloadProgress("<file-name>", m, pm))
	// client.SendMedia(chat, "<file-name>", &telegram.MediaOptions{
	// 	ProgressManager: pm,
	// })

	// same goes for download
	// &DownloadOptions{ProgressManager: NewProgressManager(5).SetMessage(m)}
	// or
	// &DownloadOptions{ProgressCallback: func(pi *ProgressInfo) {
	//     m.Edit(fmt.Sprintf("Downloading... %.2f%% complete (%.2f MB/s), ETA: %.2f seconds",
	//         pi.Percentage,
	//         pi.CurrentSpeed/1024/1024,
	//         pi.ETA,
	//     ))
	// }}
	// &SendOptions{ProgressManager: ...}
	// &MediaOptions{ProgressManager: ...}
}
