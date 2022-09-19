package telegram

import (
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/PuerkitoBio/goquery"
)

func (c *Client) FormatMessage(message string, mode string) ([]MessageEntity, string) {
	if mode == HTML {
		return c.ParseHtml(message)
	} else {
		return c.ParseHtml(MarkdownToHTML(message)) // temporary fix
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

func MarkdownToHTML(text string) string {
	var (
		inCode      bool
		inPre       bool
		inSpoiler   bool
		inBold      bool
		inItalic    bool
		inUnderline bool
		inStrike    bool
	)
	var result string
	for _, c := range text {
		if inCode {
			if c == '`' {
				inCode = false
				result += "</code>"
			} else {
				result += string(c)
			}
			continue
		}
		if inPre {
			if c == '`' {
				inPre = false
				result += "</pre>"
			} else {
				result += string(c)
			}
			continue
		}
		if inSpoiler {
			if c == '|' {
				inSpoiler = false
				result += "</span>"
			} else {
				result += string(c)
			}
			continue
		}
		if inBold {
			if c == '*' {
				inBold = false
				result += "</b>"
			} else {
				result += string(c)
			}
			continue
		}
		if inItalic {
			if c == '_' {
				inItalic = false
				result += "</i>"
			} else {
				result += string(c)
			}
			continue
		}
		if inUnderline {
			if c == '_' {
				inUnderline = false
				result += "</u>"
			} else {
				result += string(c)
			}
			continue
		}
		if inStrike {
			if c == '~' {
				inStrike = false
				result += "</s>"
			} else {
				result += string(c)
			}
			continue
		}
		if c == '`' {
			inCode = true
			result += "<code>"
			continue
		}
		if c == '~' {
			inStrike = true
			result += "<s>"
			continue
		}
		if c == '_' {
			inUnderline = true
			result += "<u>"
			continue
		}
		if c == '*' {
			inBold = true
			result += "<b>"
			continue
		}
		if c == '_' {
			inItalic = true
			result += "<i>"
			continue
		}
		if c == '|' {
			inSpoiler = true
			result += "<span class=\"tg-spoiler\">"
			continue
		}
		result += string(c)
	}
	return result
}
