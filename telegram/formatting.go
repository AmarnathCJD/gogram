package telegram

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func (c *Client) FormatMessage(message string, mode string) ([]MessageEntity, string) {
	var entities []MessageEntity
	message = AddSurrogate(strings.TrimSpace(message))
	if mode == HTML {
		return c.FormatHTML(message)
	} else {
		return entities, message
	}
}

func ReplaceNonASCII(message string) string {
	return regexp.MustCompile("[^[:ascii:]]").ReplaceAllString(message, "_")
}

func (c *Client) FormatHTML(t string) ([]MessageEntity, string) {
	var entities []MessageEntity
	text := ReplaceNonASCII(t)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(text))
	if err != nil {
		return entities, text
	}
	text = doc.Text()
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if href != "" {
			entities = append(entities, &MessageEntityTextURL{
				Offset: int32(strings.Index(text, s.Text())),
				Length: int32(len(s.Text())),
				URL:    href,
			})
		}
	})
	doc.Find("b").Each(func(i int, s *goquery.Selection) {
		entities = append(entities, &MessageEntityBold{
			Offset: int32(strings.Index(text, s.Text())),
			Length: int32(len(s.Text())),
		})
	})
	doc.Find("i").Each(func(i int, s *goquery.Selection) {
		entities = append(entities, &MessageEntityItalic{
			Offset: int32(strings.Index(text, s.Text())),
			Length: int32(len(s.Text())),
		})
	})
	doc.Find("code").Each(func(i int, s *goquery.Selection) {
		entities = append(entities, &MessageEntityCode{
			Offset: int32(strings.Index(text, s.Text())),
			Length: int32(len(s.Text())),
		})
	})
	doc.Find("s").Each(func(i int, s *goquery.Selection) {
		entities = append(entities, &MessageEntityStrike{
			Offset: int32(strings.Index(text, s.Text())),
			Length: int32(len(s.Text())),
		})
	})
	doc.Find("emoji").Each(func(i int, s *goquery.Selection) {
		document_id := s.AttrOr("document_id", "")
		document_id = s.AttrOr("id", document_id)
		doc_id, err := strconv.ParseInt(document_id, 10, 64)
		if err != nil {
			return
		}
		entities = append(entities, &MessageEntityCustomEmoji{
			Offset:     int32(strings.Index(text, s.Text())),
			Length:     int32(len(s.Text())),
			DocumentID: doc_id,
		})
	})
	doc.Find("tgspoiler").Each(func(i int, s *goquery.Selection) {
		entities = append(entities, &MessageEntitySpoiler{
			Offset: int32(strings.Index(text, s.Text())),
			Length: int32(len(s.Text())),
		})
	})
	doc.Find("pre").Each(func(i int, s *goquery.Selection) {
		lang := s.AttrOr("lang", "")
		entities = append(entities, &MessageEntityPre{
			Offset:   int32(strings.Index(text, s.Text())),
			Length:   int32(len(s.Text())),
			Language: lang,
		})
	})
	tt, _ := goquery.NewDocumentFromReader(strings.NewReader(t))
	return entities, DeleteSurrogate(tt.Text())
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

func AddSurrogate(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0xD800 && r <= 0xDFFF {
			b.WriteRune(0xFFFD)
		} else if r >= 0x10000 {
			b.WriteRune(0xD800 + (r-0x10000)>>10)
			b.WriteRune(0xDC00 + (r-0x10000)&0x3FF)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func DeleteSurrogate(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0xD800 || r > 0xDFFF {
			b.WriteRune(r)
		}
	}
	return b.String()
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
