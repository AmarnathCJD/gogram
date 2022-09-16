package telegram

import (
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/PuerkitoBio/goquery"
)

func (c *Client) FormatMessage(message string, mode string) ([]MessageEntity, string) {
	var entities []MessageEntity
	if mode == HTML {
		return c.ParseHtml(message)
	} else {
		// TODO: Add markdown formatting
		return entities, message
	}
}

func (c *Client) ParseHtml(t string) ([]MessageEntity, string) {
	var entities []MessageEntity
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(strings.TrimSpace(t)))
	if err != nil {
		return entities, t
	}
	finalText := doc.Text()
	doc.Find("*").Each(func(i int, s *goquery.Selection) {
		var entity MessageEntity
		Offset := Index(finalText, s.Text())
		Length := int32(len(utf16.Encode([]rune(s.Text()))))
		switch s.Nodes[0].Data {
		case "b", "strong":
			entity = &MessageEntityBold{Offset, Length}
		case "i", "em":
			entity = &MessageEntityItalic{Offset, Length}
		case "a":
			entity = &MessageEntityTextURL{Offset, Length, s.AttrOr("href", "")}
		case "code":
			entity = &MessageEntityCode{Offset, Length}
		case "pre":
			entity = &MessageEntityPre{Offset, Length, s.AttrOr("language", "")}
		case "tgspoiler", "spoiler":
			entity = &MessageEntitySpoiler{Offset, Length}
		case "u":
			entity = &MessageEntityUnderline{Offset, Length}
		case "s", "strike":
			entity = &MessageEntityStrike{Offset, Length}
		case "blockquote":
			entity = &MessageEntityBlockquote{Offset, Length}
		case "emoji", "document":
			document_id, err := strconv.ParseInt(s.AttrOr("document_id", s.AttrOr("document-id", s.AttrOr("document_id", ""))), 10, 64)
			if err != nil {
				return
			}
			entity = &MessageEntityCustomEmoji{Offset, Length, document_id}
		default:
			return
		}
		entities = append(entities, entity)
	})

	return entities, finalText
}

func Index(r string, s string) int32 {
	if i := strings.Index(r, s); i >= 0 {
		return int32(len(utf16.Encode([]rune(r[:i]))))
	}
	return -1
}

func (m *NewMessage) Text() string {
	return m.MessageText() // TODO: Add formatting
}

func (m *NewMessage) RawText() string {
	return m.MessageText()
}

func (m *NewMessage) Args() string {
	Messages := strings.Split(m.Text(), " ")
	if len(Messages) < 2 {
		return ""
	}
	return strings.TrimSpace(strings.Join(Messages[1:], " "))
}

func (c *Client) SetParseMode(mode string) {
	if mode == "" {
		mode = "Markdown"
	}
	for _, m := range []string{"Markdown", "HTML"} {
		if m == mode {
			c.ParseMode = mode
			return
		}
	}
}
