// Group-call streaming example using gortc as the WebRTC backend on top of
// gogram's MTProto signalling.
//
// Run:
//
//	go run examples/tgcalls/main.go @yourchatusername /path/to/audio.mp3
//
// ffmpeg must be on PATH (gortc invokes it for transcoding to Opus/IVF).

package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/amarnathcjd/gogram/telegram"
	"github.com/amarnathcjd/gortc"
)

const (
	appID   = 6
	appHash = "YOUR_APP_HASH"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: tgcalls <chat> <media-source>")
		fmt.Println("  <chat>          @username, invite link or chat id")
		fmt.Println("  <media-source>  local file path or remote URL")
		os.Exit(1)
	}
	chat, source := os.Args[1], os.Args[2]

	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		Session:  "session.dat",
		LogLevel: telegram.LogInfo,
	})
	if err != nil {
		log.Fatalf("client: %v", err)
	}
	if err := client.Connect(); err != nil {
		log.Fatalf("connect: %v", err)
	}
	if err := client.AuthPrompt(); err != nil {
		log.Fatalf("auth: %v", err)
	}

	call := gortc.NewCall(client, gortc.WithLogLevel(slog.LevelInfo))

	call.OnConnected(func() {
		go func() {
			log.Printf("streaming %s", source)
			if err := call.Stream(context.Background(), gortc.FromFile(source)); err != nil {
				log.Printf("stream finished: %v", err)
			}
		}()
	})

	if err := call.Join(chat); err != nil {
		log.Fatalf("join: %v", err)
	}
	defer call.Leave()

	log.Printf("joined %s — press Ctrl+C to leave", chat)
	client.Idle()
}
