package main

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
	MsgTitle := fmt.Sprintf("Title: %s\nActors: %s\nRank: %s", title.Title, title.Actors, title.Rank)
	if title.Poster != "" {
		_, SendErr := m.Client.SendMedia(m.ChatID(), title.Poster, &telegram.SendOptions{Caption: MsgTitle, ReplyMarkup: btn.Keyboard(btn.Row(btn.URL("View on IMDb", title.Link))), LinkPreview: false})
		return SendErr
	}
	_, Senderr := m.Respond(MsgTitle)
	return Senderr
}
