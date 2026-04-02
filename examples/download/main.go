package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	botToken = "YOUR_BOT_TOKEN"
)

func main() {
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:   appID,
		AppHash: appHash,
	})
	client.LoginBot(botToken)

	// Download to disk (default)
	// File is saved to the current directory using the server-provided filename.
	// Workers write directly to the file in parallel — no intermediate buffer.
	client.On("message:/dl", func(m *telegram.NewMessage) error {
		if !m.IsMedia() {
			return nil
		}

		path, err := m.Download(&telegram.DownloadOptions{
			FileName:         "downloads/output.mp4", // optional: override save path
			ProgressCallback: logProgress,
			ProgressInterval: 3,
		})
		if err != nil {
			return err
		}
		m.Reply("saved to: " + path)
		return nil
	})

	// Download directly into an *os.File (io.WriterAt, fastest)
	// *os.File implements io.WriterAt so chunks are written in parallel with zero
	// intermediate allocation — you control the file handle yourself.
	client.On("message:/dl_file", func(m *telegram.NewMessage) error {
		if !m.IsMedia() {
			return nil
		}

		f, err := os.CreateTemp("", "tg-*.mp4")
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = m.Download(&telegram.DownloadOptions{
			Buffer:           f, // *os.File satisfies io.WriterAt — no intermediate buffer
			ProgressCallback: logProgress,
		})
		if err != nil {
			return err
		}
		m.Reply("written to temp file: " + f.Name())
		return nil
	})

	// Download into a bytes.Buffer (io.Writer, in-memory)
	// Chunks are assembled in an internal []byte then copied to the buffer at the end.
	// Useful for small files you want to process in-memory without touching disk.
	// Note: allocates the full file size in RAM — avoid for large files.
	client.On("message:/dl_mem", func(m *telegram.NewMessage) error {
		if !m.IsMedia() {
			return nil
		}

		var buf bytes.Buffer
		_, err := m.Download(&telegram.DownloadOptions{
			Buffer: &buf, // *bytes.Buffer satisfies io.Writer
		})
		if err != nil {
			return err
		}
		m.Reply(fmt.Sprintf("downloaded %d bytes into memory", buf.Len()))
		return nil
	})

	// Download with ProgressManager (edits a live message)
	client.On(telegram.OnMessage, func(m *telegram.NewMessage) error {
		if !m.IsMedia() {
			return nil
		}

		pm := telegram.NewProgressManager(5).SetMessage(m)
		_, err := m.Download(&telegram.DownloadOptions{
			ProgressManager: pm,
		})
		if err != nil {
			m.Reply("download error: " + err.Error())
			return err
		}
		m.Reply("done!")
		return nil
	}, telegram.IsPrivate)

	client.Idle()
}

func logProgress(p *telegram.ProgressInfo) {
	fmt.Printf("\r%s  %.1f%%  %.2f MB/s  ETA %.0fs",
		p.FileName, p.Percentage, p.CurrentSpeed/1024/1024, p.ETA)
	if p.Percentage >= 100 {
		fmt.Println()
	}
}
