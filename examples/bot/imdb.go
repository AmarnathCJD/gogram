package examples

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/amarnathcjd/gogram/telegram"
)

var (
	btn = telegram.Button{}
)

type (
	Title struct {
		Title  string `json:"title"`
		ID     string `json:"id"`
		Poster string `json:"poster"`
		Actors string `json:"actors"`
		Rank   string `json:"rank"`
		Link   string `json:"link"`
	}
)

// Examples of sending button & media messages:

func Imdb(m *telegram.NewMessage) error {
	Args := m.Args()
	resp, _ := http.Get("https://watch-series-go.vercel.app/api/imdb?query=" + url.QueryEscape(Args))
	var t []Title
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&t)
	if len(t) == 0 {
		_, err := m.Reply("No results found")
		return err
	}
	title := t[0]
	TitleInfo := telegram.Ent().Bold("Title: ").Plain(title.Title + "\n").Bold("Actors: ").Code(title.Actors + "\n").Bold("Rank: ").Code(title.Rank + "\n")
	fmt.Println(TitleInfo.GetText())
	if title.Poster != "" {
		_, err := m.Reply(title.Poster, telegram.SendOptions{Caption: TitleInfo, ReplyMarkup: btn.Keyboard(btn.Row(btn.URL("View on IMDb", title.Link))), LinkPreview: false})
		fmt.Println(err)
		return err
	}
	_, Senderr := m.Respond(TitleInfo)
	return Senderr
}
