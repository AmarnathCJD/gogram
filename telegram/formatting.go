package telegram

import (
	"regexp"
	"strings"
)

var (
	EntityCodeRegex          = regexp.MustCompile("`([\\s\\S]*)`")
	EntityBoldRegex          = regexp.MustCompile(`\*\*([\s\S]*)\*\*`)
	EntityItalicRegex        = regexp.MustCompile(`__([\s\S]*)__`)
	EntityStrikeRegex        = regexp.MustCompile(`\~\~([\s\S]*)\~\~`)
	EntityUnderlineRegex     = regexp.MustCompile(`\--([\s\S]*)\--`)
	EntitySpoilerRegex       = regexp.MustCompile(`||([\s\S]*)||`)
	EntityCodeHTMLRegex      = regexp.MustCompile(`<code>([\s\S]*)</code>`)
	EntityBoldHTMLRegex      = regexp.MustCompile(`<b>([\s\S]*)</b>`)
	EntityItalicHTMLRegex    = regexp.MustCompile(`<i>([\s\S]*)</i>`)
	EntityStrikeHTMLRegex    = regexp.MustCompile(`<s>([\s\S]*)</s>`)
	EntityUnderlineHTMLRegex = regexp.MustCompile(`<u>([\s\S]*)</u>`)
	EntitySpoilerHTMLRegex   = regexp.MustCompile(`<tgspoiler>([\s\S]*)</tgspoiler>`)
)

func (c *Client) ParseEntity(text string, ParseMode string) (string, []MessageEntity) {
	var e []MessageEntity
	if ParseMode == "Markdown" {
		for _, m := range EntityCodeRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "`", "", 2)
			e = append(e, &MessageEntityCode{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
		for _, m := range EntityBoldRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "**", "", 2)
			e = append(e, &MessageEntityBold{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
		for _, m := range EntityItalicRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "__", "", 2)
			e = append(e, &MessageEntityItalic{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
		for _, m := range EntityStrikeRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "~~", "", 2)
			e = append(e, &MessageEntityStrike{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
		for _, m := range EntityUnderlineRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "--", "", 2)
			e = append(e, &MessageEntityUnderline{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
		for _, m := range EntitySpoilerRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "||", "", 2)
			e = append(e, &MessageEntitySpoiler{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
	} else {
		for _, m := range EntityCodeHTMLRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "<code>", "", 1)
			text = strings.Replace(text, "</code>", "", 1)
			e = append(e, &MessageEntityCode{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
		for _, m := range EntityBoldHTMLRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "<b>", "", 1)
			text = strings.Replace(text, "</b>", "", 1)
			e = append(e, &MessageEntityBold{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
		for _, m := range EntityItalicHTMLRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "<i>", "", 1)
			text = strings.Replace(text, "</i>", "", 1)
			e = append(e, &MessageEntityItalic{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
		for _, m := range EntityStrikeHTMLRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "<s>", "", 1)
			text = strings.Replace(text, "</s>", "", 1)
			e = append(e, &MessageEntityStrike{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
		for _, m := range EntityUnderlineHTMLRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "<u>", "", 1)
			text = strings.Replace(text, "</u>", "", 1)
			e = append(e, &MessageEntityUnderline{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
		for _, m := range EntitySpoilerHTMLRegex.FindAllStringSubmatch(text, -1) {
			text = strings.Replace(text, "<tgspoiler>", "", 1)
			text = strings.Replace(text, "</tgspoiler>", "", 1)
			e = append(e, &MessageEntitySpoiler{
				Offset: GetOffSet(text, m[1]),
				Length: int32(len(m[1])),
			})
		}
	}
	return text, e
}

func GetOffSet(str string, substr string) int32 {
	return int32(strings.Index(str, substr))
}

func (m *NewMessage) Text() string {
	text := m.Message()
	var correction int32
	for _, e := range m.OriginalUpdate.Entities {
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
	return m.Message()
}

func (m *NewMessage) Args() string {
	Messages := strings.Split(m.Text(), " ")
	if len(Messages) < 2 {
		return ""
	}
	return strings.Join(Messages[1:], " ")
}
