package main

import (
	"encoding/json"
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

func Imdb(c *telegram.Client, m *telegram.NewMessage) error {
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
	TitleInfo := telegram.Ent().Bold("Title: ").Plain(title.Title + "\n").Bold("ID: ").Code(title.ID + "\n").Bold("Actors: ").Code(title.Actors + "\n").Bold("Rank: ").Code(title.Rank + "\n")
	if title.Poster != "" {
		_, SendErr := m.Client.SendMedia(m.ChatID(), title.Poster, &telegram.SendOptions{Caption: TitleInfo, ReplyMarkup: btn.Keyboard(btn.Row(btn.URL("View on IMDb", title.Link))), LinkPreview: false})
		return SendErr
	}
	_, Senderr := m.Respond(TitleInfo)
	return Senderr
}
