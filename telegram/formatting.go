package telegram

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

func (c *Client) FormatMessage(message string, mode string) ([]MessageEntity, string) {
	var entities []MessageEntity
	message = strings.TrimSpace(message)
	message = strings.TrimFunc(message, func(r rune) bool {
		return !unicode.IsGraphic(r)
	})
	if mode == HTML {
		tok, _ := goquery.NewDocumentFromReader(strings.NewReader(message))
		var htmlFreeRegex = regexp.MustCompile(`<.*?>`)
		var Text = strings.TrimSpace(htmlFreeRegex.ReplaceAllString(message, ""))
		tok.Find("code").Each(func(i int, s *goquery.Selection) {
			if text := s.Text(); strings.TrimSpace(text) != "" {
				entities = append(entities, &MessageEntityCode{
					Offset: int32(GetOffSet(Text, text)),
					Length: int32(len(text)),
				})
			}
		})
		tok.Find("b").Each(func(i int, s *goquery.Selection) {
			if text := s.Text(); strings.TrimSpace(text) != "" {
				entities = append(entities, &MessageEntityBold{
					Offset: int32(GetOffSet(Text, text)),
					Length: int32(len([]rune(text))),
				})
			}
		})
		tok.Find("i").Each(func(i int, s *goquery.Selection) {
			if text := s.Text(); strings.TrimSpace(text) != "" {
				entities = append(entities, &MessageEntityItalic{
					Offset: int32(GetOffSet(Text, text)),
					Length: int32(len([]rune(text))),
				})
			}
		})
		tok.Find("s").Each(func(i int, s *goquery.Selection) {
			if text := s.Text(); strings.TrimSpace(text) != "" {
				entities = append(entities, &MessageEntityStrike{
					Offset: int32(GetOffSet(Text, text)),
					Length: int32(len([]rune(text))),
				})
			}
		})
		tok.Find("u").Each(func(i int, s *goquery.Selection) {
			if text := s.Text(); strings.TrimSpace(text) != "" {
				entities = append(entities, &MessageEntityUnderline{
					Offset: int32(GetOffSet(Text, text)),
					Length: int32(len([]rune(text))),
				})
			}
		})
		tok.Find("tgspoiler").Each(func(i int, s *goquery.Selection) {
			if text := s.Text(); strings.TrimSpace(text) != "" {
				entities = append(entities, &MessageEntitySpoiler{
					Offset: int32(GetOffSet(Text, text)),
					Length: int32(len([]rune(text))),
				})
			}
		})
		tok.Find("a").Each(func(i int, s *goquery.Selection) {
			if text := s.Text(); strings.TrimSpace(text) != "" {
				entities = append(entities, &MessageEntityTextURL{
					Offset: int32(GetOffSet(Text, text)),
					Length: int32(len([]rune(text))),
					URL:    s.AttrOr("href", ""),
				})
			}
		})
		return entities, Text
	} else {
		return entities, message
	}
}

func GetOffSet(str string, substr string) int32 {
	return int32(strings.Index(str, substr))
}

func (m *NewMessage) Text() string {
	text := m.MessageText()
	var correction int32
	for _, e := range m.Message.Entities {
		switch e := e.(type) {
		case *MessageEntityBold:
			offset := e.Offset + correction
			length := e.Length
			text = text[:offset] + "<b>" + text[offset:offset+length] + "</b>" + text[offset+length:]
			correction += 7
		case *MessageEntityItalic:
			offset := e.Offset + correction
			length := e.Length
			text = text[:offset] + "<i>" + text[offset:offset+length] + "</i>" + text[offset+length:]
			correction += 7
		case *MessageEntityCode:
			offset := e.Offset + correction
			length := e.Length
			text = text[:offset] + "<code>" + text[offset:offset+length] + "</code>" + text[offset+length:]
			correction += 13
		case *MessageEntityStrike:
			offset := e.Offset + correction
			length := e.Length
			text = text[:offset] + "<s>" + text[offset:offset+length] + "</s>" + text[offset+length:]
			correction += 7
		case *MessageEntityTextURL:
			offset := e.Offset + correction
			length := e.Length
			text = text[:offset] + "<a href=\"" + e.URL + "\">" + text[offset:offset+length] + "</a>" + text[offset+length:]
			correction += int32(len(e.URL)) + int32(len(text[offset:offset+length])) + 23
		}
	}
	return text
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
