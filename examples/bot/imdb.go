package examples

import (
	"net/http"

	"github.com/StalkR/imdb"
	"github.com/amarnathcjd/gogram/telegram"
)

var (
	btn = telegram.Button{}
)

// Examples of sending button & media messages:

func Imdb(c *telegram.Client, m *telegram.NewMessage) error {
	Args := m.Args()
	results, err := imdb.SearchTitle(http.DefaultClient, Args)
	if err != nil {
		m.Reply("Error: " + err.Error())
		return nil
	}
	if len(results) == 0 {
		m.Reply("No results found")
		return nil
	}
	Title, err := imdb.NewTitle(http.DefaultClient, results[0].ID)
	if err != nil {
		m.Reply("Error: " + err.Error())
	}
	var Movie string
	if Title.Name != "" {
		Movie = "<b>" + Title.Name + "</b>\n"
	} else {
		Movie = "No title found\n"
	}
	if Title.Poster.URL != "" {
		_, SendErr := m.Client.SendMedia(m.ChatID(), "https://m.media-amazon.com/images/M/MV5BYTRiNDQwYzAtMzVlZS00NTI5LWJjYjUtMzkwNTUzMWMxZTllXkEyXkFqcGdeQXVyNDIzMzcwNjc@._V1_FMjpg_UX1000_.jpg", &telegram.SendOptions{Caption: Movie, ReplyMarkup: btn.Keyboard(btn.Row(btn.URL("View on IMDb", Title.URL))), LinkPreview: false})
		return SendErr
	}
	_, Senderr := m.Respond(Movie)
	return Senderr
}
