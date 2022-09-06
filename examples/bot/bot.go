package examples

import (
	"fmt"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = ""
	botToken = ""
)

func main() {
	client, err := telegram.TelegramClient(telegram.ClientConfig{
		AppID:         appID,
		AppHash:       appHash,
		DataCenter:    5,  // Working on a fix for this, (This should be acurate)
		StringSession: "", // (if this value is specified, client.Login is not Necessary.)
	})

	if err != nil {
		panic(err)
	}

	// Login with bot token, if you have a string session, you can skip this step.
	err = client.LoginBot(botToken)
	if err != nil {
		panic(err)
	}

	stringSession, _ := client.ExportSession()
	fmt.Println("String Session: ", stringSession)

	me, _ := client.GetMe()
	fmt.Printf("Logged in as %s\n", me.Username)

	// Add handlers
	client.AddMessageHandler("/start", Start)
	client.AddMessageHandler("/download", DownloadFile)
	client.AddMessageHandler("/upload", UploadFile)
	client.AddInlineHandler("test", InlineQuery)

	client.Idle() // Blocks until client.Stop() is called
}

func Start(m *telegram.NewMessage) error {
	_, err := m.Reply("Hello World")
	return err
}

func InlineQuery(m *telegram.InlineQuery) error {
	var b = m.Builder()
	b.Article(
		"Test Article Title",
		"Test Article Description",
		"Test Article Text",
	)
	_, err := m.Answer(b.Results())
	return err
}

func DownloadFile(m *telegram.NewMessage) error {
	if !m.IsMedia() {
		m.Reply("Not Media!")
	}
	var p = telegram.Progress{}
	e, _ := m.Reply("Downloading...")
	go func() {
		for range time.Tick(time.Second * 5) {
			if p.Percentage() == 100 {
				e.Edit(fmt.Sprintf("Downloaded... %v%%", p.Percentage()))
				break
			}
			e.Edit(fmt.Sprintf("Download...\nProgress %v", p.Percentage()))
		}
	}()
	_, err := m.Download(&telegram.DownloadOptions{Progress: &p})
	return err
}

func UploadFile(m *telegram.NewMessage) error {
	if m.Args() == "" {
		m.Reply("Please specify a file path!")
	}
	message, _ := m.Reply("Uploading...")
	defer message.Delete()
	startTime := time.Now()
	file, err := message.Client.UploadFile(m.Args(), true) // MultiThread : bool
	if err != nil {
		return err
	}
	_, err = m.ReplyMedia(file, telegram.MediaOptions{Caption: fmt.Sprintf("Uploaded in %v", time.Since(startTime))})
	if err != nil {
		message.Edit(err.Error())
		return err
	}

	return nil
}
