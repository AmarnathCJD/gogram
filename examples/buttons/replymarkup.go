package examples

import (
	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	botToken = "YOUR_BOT_TOKEN"
)

func main() {
	// create a new client object
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
	})

	client.LoginBot(botToken)

	client.SendMessage("username", "Hello, This is ReplyMarkup Example", &telegram.SendOptions{
		ReplyMarkup: telegram.NewKeyboard().AddRow( // adds a row of buttons
			telegram.Button.Data("Help", "help"),
			telegram.Button.URL("Google", "https://www.google.com"),
		).Build(), //.AddRow( // adds another row of buttons)
	})

	client.On("message:/start", func(message *telegram.NewMessage) error {
		message.Reply("Hello:>", telegram.SendOptions{
			ReplyMarkup: telegram.NewKeyboard().NewGrid(2, 3, // 2 rows, 3 columns format
				telegram.Button.URL("Google", "https://www.google.com"),
				telegram.Button.Data("Help", "help"),
				telegram.Button.Data("Main Menu", "main"),
				telegram.Button.Data("Source", "source"),
				telegram.Button.SwitchInline("goinline", true, "query"),
				telegram.Button.Data("Back", "back")).Build(),
		})
		return nil
	})

	telegram.NewKeyboard().NewColumn(2, telegram.Button.URL("Google", "https://www.google.com"),
		telegram.Button.URL("Yahoo", "https://www.yahoo.com"),
		telegram.Button.URL("Bing", "https://www.bing.com"),
		telegram.Button.URL("DuckDuckGo", "https://www.duckduckgo.com"),
	).Build() // 2 columns format, buttons equally divided into 2 columns

	telegram.NewKeyboard().NewRow(3,
		telegram.Button.URL("Google", "https://www.google.com"),
		telegram.Button.URL("Yahoo", "https://www.yahoo.com"),
		telegram.Button.URL("Bing", "https://www.bing.com"),
		telegram.Button.URL("DuckDuckGo", "https://www.duckduckgo.com"),
	).Build() // 3 rows format, buttons equally divided into 3 rows

	client.On("callback:help", func(callback *telegram.CallbackQuery) error {
		callback.Answer("Help is on the way!")
		return nil
	})

	client.Idle()
}
