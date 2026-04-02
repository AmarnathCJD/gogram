package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	botToken = "YOUR_BOT_TOKEN"
	chatID   = "YOUR_CHAT_ID"
)

func main() {
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:   appID,
		AppHash: appHash,
	})
	client.LoginBot(botToken)

	chat, _ := client.ResolvePeer(chatID)

	// Upload from a file path (string)
	// Parallel upload — file is read at random offsets by multiple workers.
	client.SendMedia(chat, "video.mp4", &telegram.MediaOptions{
		Upload: &telegram.UploadOptions{
			ProgressCallback: logProgress,
			ProgressInterval: 3,
		},
	})

	// Upload from *os.File
	// Also parallel — *os.File satisfies io.ReaderAt.
	f, _ := os.Open("photo.jpg")
	defer f.Close()
	client.SendMedia(chat, f, &telegram.MediaOptions{
		Upload: &telegram.UploadOptions{
			ProgressCallback: logProgress,
		},
	})

	// Upload from []byte
	// Parallel — bytes.NewReader wraps the slice, supports ReaderAt.
	data, _ := os.ReadFile("document.pdf")
	client.SendMedia(chat, data, &telegram.MediaOptions{
		Upload: &telegram.UploadOptions{
			FileName: "document.pdf", // required: no filename is inferred from []byte
		},
	})

	// Upload from *bytes.Reader (parallel)
	// bytes.Reader implements io.ReaderAt so uploads are parallelised.
	br := bytes.NewReader(data)
	client.SendMedia(chat, br, &telegram.MediaOptions{
		Upload: &telegram.UploadOptions{
			FileName:         "document.pdf",
			ProgressCallback: logProgress,
		},
	})

	// Upload from io.ReadSeeker (sequential, size known)
	// ReadSeeker is NOT safe for concurrent reads, so gogram uses a single worker.
	// The seek is used upfront to measure the file size for accurate progress.
	var rs io.ReadSeeker = strings.NewReader("hello world")
	client.SendMedia(chat, rs, &telegram.MediaOptions{
		Upload: &telegram.UploadOptions{
			FileName: "hello.txt",
		},
	})

	// Upload from io.Reader (sequential, size unknown)
	// No parallelism, no progress percentage (size is unknown).
	// Use for streaming sources like network responses.
	var r io.Reader = strings.NewReader("streamed content")
	client.SendMedia(chat, r, &telegram.MediaOptions{
		Upload: &telegram.UploadOptions{
			FileName: "stream.txt",
		},
	})

	// Upload from ReaderAtSource (parallel, custom io.ReaderAt)
	// Use this when you have a custom source that supports random-access reads,
	// e.g. a memory-mapped file, a cloud storage reader, etc.
	// You must supply the size explicitly since there is no way to infer it.
	rawData := []byte("binary payload")
	src := &telegram.ReaderAtSource{
		Reader: bytes.NewReader(rawData),
		Size:   int64(len(rawData)),
		Name:   "payload.bin",
	}
	client.SendMedia(chat, src, &telegram.MediaOptions{
		Upload: &telegram.UploadOptions{
			ProgressCallback: logProgress,
		},
	})

	// Upload with ProgressManager (edits a live message)
	status, _ := client.SendMessage(chat, "Uploading...")
	pm := telegram.NewProgressManager(5).
		WithCallback(func(pi *telegram.ProgressInfo) {
			status.Edit(fmt.Sprintf("Uploading... %.1f%% (%.2f MB/s) ETA %.0fs",
				pi.Percentage, pi.CurrentSpeed/1024/1024, pi.ETA))
		})
	client.SendMedia(chat, "bigfile.zip", &telegram.MediaOptions{
		Upload: &telegram.UploadOptions{
			ProgressManager: pm,
		},
	})
	status.Edit("Upload complete!")
}

func logProgress(p *telegram.ProgressInfo) {
	fmt.Printf("\r%s  %.1f%%  %.2f MB/s  ETA %.0fs",
		p.FileName, p.Percentage, p.CurrentSpeed/1024/1024, p.ETA)
	if p.Percentage >= 100 {
		fmt.Println()
	}
}
