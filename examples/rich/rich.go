package main

import (
	"log"
	"time"

	tg "github.com/amarnathcjd/gogram/telegram"
)

func main() {
	client, err := tg.NewClient(tg.ClientConfig{
		AppID:   12345,
		AppHash: "your-app-hash",
		Session: "session",
	})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := client.Conn(); err != nil {
		log.Fatal(err)
	}
	if err := client.LoginBot("YOUR:BOT_TOKEN"); err != nil {
		log.Fatal(err)
	}

	client.On("message:/demo", handleDemo)
	client.On("message:/blocks", handleBlocks)

	client.Idle()
}

// /demo — the original showcase: heading, paragraph, quote, table, details,
// divider, link.
func handleDemo(m *tg.NewMessage) error {
	rb := tg.NewRichMessage().
		AddBlock(&tg.PageBlockHeading1{
			Text: &tg.TextPlain{Text: "Inception (2010)"},
		}).
		Paragraph("A thief who steals corporate secrets through dream-sharing.").
		Quote("You mustn't be afraid to dream a little bigger, darling.").
		Table([][]any{
			{"Source", "Score"},
			{"IMDb", "8.8 / 10"},
			{"Metascore", "74 / 100"},
		}, &tg.TableOptions{
			Bordered: true,
			Striped:  true,
			Header:   true,
			Title:    "Ratings",
		}).
		Details(
			&tg.TextBold{Text: &tg.TextPlain{Text: "Plot — tap to reveal"}},
			&tg.PageBlockParagraph{
				Text: &tg.TextPlain{Text: "Cobb is offered a chance to have his criminal history erased..."},
			},
		).
		Divider().
		AddBlock(&tg.PageBlockParagraph{
			Text: &tg.TextURL{
				Text: &tg.TextPlain{Text: "→ Open on IMDb"},
				URL:  "https://www.imdb.com/title/tt1375666/",
			},
		})

	_, err := m.ReplyRich(rb, &tg.SendOptions{
		ReplyMarkup: tg.NewKeyboard().AddRow(
			tg.Button.URL("Watch trailer", "https://www.imdb.com/video/vi3877612057/"),
		).Build(),
	})
	return err
}

// /blocks — every PageBlock variant the builder + helpers expose, end to end.
func handleBlocks(m *tg.NewMessage) error {
	rb := tg.NewRichMessage().
		// Title / subtitle / kicker — masthead-style headers.
		AddBlock(&tg.PageBlockKicker{Text: tg.RichPlain("FEATURED")}).
		AddBlock(&tg.PageBlockTitle{Text: tg.RichPlain("Field guide to RichBuilder")}).
		AddBlock(&tg.PageBlockSubtitle{Text: tg.RichPlain("Every block, in one message")}).
		AddBlock(&tg.PageBlockAuthorDate{
			Author:        tg.RichConcat(tg.RichPlain("by "), tg.RichBold("gogram")),
			PublishedDate: int32(time.Now().Unix()),
		}).
		Divider().

		// Headings 1–4 (5/6 also exist but they're vanishingly small).
		AddBlock(&tg.PageBlockHeading1{Text: tg.RichPlain("Heading 1")}).
		AddBlock(&tg.PageBlockHeading2{Text: tg.RichPlain("Heading 2")}).
		AddBlock(&tg.PageBlockHeading3{Text: tg.RichPlain("Heading 3")}).
		AddBlock(&tg.PageBlockHeading4{Text: tg.RichPlain("Heading 4")}).
		AddBlock(&tg.PageBlockSubheader{Text: tg.RichPlain("Subheader")}).
		AddBlock(&tg.PageBlockHeader{Text: tg.RichPlain("Header")}).

		// Paragraph with mixed rich-text styling — every public Rich* helper.
		AddBlock(&tg.PageBlockParagraph{
			Text: tg.RichConcat(
				tg.RichBold("Bold"), tg.RichPlain(", "),
				tg.RichItalic("italic"), tg.RichPlain(", "),
				tg.RichUnderline("underline"), tg.RichPlain(", "),
				tg.RichStrike("strike"), tg.RichPlain(", "),
				tg.RichFixed("monospace"), tg.RichPlain(", "),
				tg.RichMarked("highlight"), tg.RichPlain(", "),
				tg.RichLink("link", "https://core.telegram.org"), tg.RichPlain("."),
			),
		}).

		// Block- and pull-quotes.
		AddBlock(&tg.PageBlockBlockquote{
			Text:    tg.RichPlain("A blockquote: the contents of a <blockquote> tag."),
			Caption: tg.RichItalic("— attribution caption"),
		}).
		Quote("A pullquote — visually larger, no caption.").

		// Preformatted code with a language hint.
		AddBlock(&tg.PageBlockPreformatted{
			Text: tg.RichPlain(`func main() {
    fmt.Println("hello, rich messages")
}`),
			Language: "go",
		}).

		// Math block (LaTeX source).
		AddBlock(&tg.PageBlockMath{
			Source: `e^{i\pi} + 1 = 0`,
		}).

		// Unordered list.
		AddBlock(&tg.PageBlockList{Items: []tg.PageListItem{
			&tg.PageListItemText{Text: tg.RichPlain("First item")},
			&tg.PageListItemText{Text: tg.RichConcat(
				tg.RichPlain("Second item with a "),
				tg.RichLink("link", "https://example.com"),
			)},
			&tg.PageListItemText{Text: tg.RichPlain("Third item")},
		}}).

		// Ordered list, with numeric markers ("1.", "2.", "3.").
		AddBlock(&tg.PageBlockOrderedList{Items: []tg.PageListOrderedItem{
			&tg.PageListOrderedItemText{Num: "1", Text: tg.RichPlain("Wake up")},
			&tg.PageListOrderedItemText{Num: "2", Text: tg.RichPlain("Build a rich message")},
			&tg.PageListOrderedItemText{Num: "3", Text: tg.RichPlain("Send it")},
		}}).

		// Table — same helper as /demo but without a header row this time.
		Table([][]any{
			{tg.RichBold("Speaker"), tg.RichBold("Topic")},
			{"Alice", "Building rich messages"},
			{"Bob", "Edge cases & quirks"},
		}, &tg.TableOptions{Bordered: true, Title: "Schedule"}).

		// Details — collapsible section that can wrap any blocks.
		Details(
			tg.RichBold("Advanced (tap to expand)"),
			&tg.PageBlockParagraph{Text: tg.RichPlain("Nested content goes here.")},
			&tg.PageBlockBlockquote{
				Text:    tg.RichPlain("You can put any block inside Details — including more blocks."),
				Caption: tg.RichEmpty(),
			},
		).

		// Embed — points to an external URL with optional dimensions.
		AddBlock(&tg.PageBlockEmbed{
			URL:            "https://core.telegram.org",
			FullWidth:      true,
			AllowScrolling: true,
			W:              640,
			H:              360,
			Caption:        tg.EmptyCaption(),
		}).

		// Related-articles strip — link cards at the bottom of an article.
		AddBlock(&tg.PageBlockRelatedArticles{
			Title: tg.RichBold("Related reading"),
			Articles: []*tg.PageRelatedArticle{
				{
					URL:         "https://core.telegram.org/api/rich-messages",
					Title:       "Rich messages",
					Description: "Telegram's reference for rich-message payloads.",
					Author:      "Telegram",
				},
				{
					URL:         "https://github.com/amarnathcjd/gogram",
					Title:       "gogram",
					Description: "Go MTProto client used by this bot.",
					Author:      "amarnathcjd",
				},
			},
		}).

		// Divider + footer to close out.
		Divider().
		AddBlock(&tg.PageBlockFooter{
			Text: tg.RichItalic("End of the field guide."),
		})

	_, err := m.ReplyRich(rb)
	return err
}
