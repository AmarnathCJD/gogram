package telegram

import (
	"html"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/amarnathcjd/gogram/internal/utils"
)

type Formatter struct {
	Log *utils.Logger
}

func NewFormatter() *Formatter {
	return &Formatter{
		Log: utils.NewLogger("gogram - formatter - "),
	}
}

var Fmt = NewFormatter()

func (c *Client) FormatMessage(message string, mode string) ([]MessageEntity, string) {
	if mode == HTML {
		return Fmt.parseHTML(message)
	} else if mode == MarkDown {
		return Fmt.parseMarkdown(message)
	} else {
		return []MessageEntity{}, message
	}
}

func (f *Formatter) parseEntities(text string, parseMode string) (entities []MessageEntity, newText string) {
	switch parseMode {
	case HTML:
		return f.parseHTML(text)
	case MarkDown:
		return f.parseMarkdown(text)
	}
	return []MessageEntity{}, text
}

func (f *Formatter) parseHTML(text string) ([]MessageEntity, string) {
	return f.parseHTMLRune([]rune(text), 0)
}

func (f *Formatter) parseHTMLRune(in []rune, prevO int32) ([]MessageEntity, string) {
	prev := 0
	var entities []MessageEntity
	out := &strings.Builder{}
	for i := 0; i < len(in); i++ {
		switch in[i] {
		case '<':
			c := getHTMLTagCloseIndex(in[i+1:])
			if c < 0 {
				return entities, string(in)
			}
			closeTag := i + c + 1
			tagContent := string(in[i+1 : closeTag])
			tagFields := strings.Fields(tagContent)
			if len(tagFields) < 1 {
				return entities, string(in)
			}
			tag := tagFields[0]

			co, cc := getClosingTag(in[closeTag+1:], tag)
			if co < 0 || cc < 0 {
				return entities, string(in)
			}
			closingOpen, closingClose := closeTag+1+co, closeTag+1+cc
			out.WriteString(html.UnescapeString(string(in[prev:i])))

			e, nested := f.parseHTMLRune(in[closeTag+1:closingOpen], int32(len(utf16.Encode([]rune(out.String())))))
			entities = append(entities, e...)
			offset := int32(len(utf16.Encode([]rune(out.String())))) + prevO
			length := int32(len(utf16.Encode([]rune(nested))))
			if length == 0 {
				prev = closingClose + 1
				i = closingClose
				continue
			}

			switch tag {
			case "b", "strong":
				entities = append(entities, &MessageEntityBold{
					Offset: offset,
					Length: length,
				})
				out.WriteString(nested)
			case "i", "em":
				entities = append(entities, &MessageEntityItalic{
					Offset: offset,
					Length: length,
				})
				out.WriteString(nested)
			case "u", "ins":
				entities = append(entities, &MessageEntityUnderline{
					Offset: offset,
					Length: length,
				})
				out.WriteString(nested)
			case "s", "strike", "del":
				entities = append(entities, &MessageEntityStrike{
					Offset: offset,
					Length: length,
				})
				out.WriteString(nested)
			case "code":
				entities = append(entities, &MessageEntityCode{
					Offset: offset,
					Length: length,
				})
				// code and pre don't look at nested values, because they're not parsed
				out.WriteString(html.UnescapeString(string(in[closeTag+1 : closingOpen])))
			case "pre":
				entities = append(entities, &MessageEntityPre{
					Offset: offset,
					Length: length,
				})
				// code and pre don't look at nested values, because they're not parsed
				out.WriteString(html.UnescapeString(string(in[closeTag+1 : closingOpen])))
			case "span":
				// NOTE: All span tags are currently spoiler tags. This may change in the future.
				if len(tagFields) < 2 {
					f.Log.Warn("no closing tag for HTML tag started at ", i)
					return entities, string(in)
				}

				switch spanType := tagFields[1]; spanType {
				case "class=\"tg-spoiler\"":
					out.WriteString(html.UnescapeString(string(in[closeTag+1 : closingOpen])))
				default:
					f.Log.Warn("unknown span type ", spanType)
					return entities, string(in)
				}
			case "a":
				if link.MatchString(tagContent) {
					out.WriteString(nested)
					entities = append(entities, &MessageEntityTextURL{
						Offset: offset,
						Length: int32(len(nested)),
						URL:    link.FindStringSubmatch(tagContent)[1],
					})
				} else if link2.MatchString(tagContent) {
					out.WriteString(nested)
					entities = append(entities, &MessageEntityTextURL{
						Offset: offset,
						Length: length,
						URL:    link2.FindStringSubmatch(tagContent)[1],
					})
				} else {
					f.Log.Warn("unknown link format ", tagContent)
				}
			default:
				f.Log.Warn("unknown HTML tag ", tag)
				return entities, string(in)
			}

			prev = closingClose + 1
			i = closingClose
			// escape newlines
		case '\r', '\n':
			out.WriteString(string(in[prev:i]))
			// out.WriteRune('\\')
			out.WriteRune(in[i])
			prev = i + 1

		case '\\', '_', '*', '~', '`', '[', ']', '(', ')': // these all need to be escaped to ensure we retain the same message
			out.WriteString(html.UnescapeString(string(in[prev:i])))
			// out.WriteRune('\\')
			out.WriteRune(in[i])
			prev = i + 1
		}
	}
	out.WriteString(html.UnescapeString(string(in[prev:])))
	str := out.String()
	// \\n -> \n
	str = strings.ReplaceAll(str, "\\\\", "\\")
	return entities, str
}

func (f *Formatter) parseMarkdown(text string) ([]MessageEntity, string) {
	return f.parseHTML(f.markdownToHTML([]rune(text)))
}

func (f *Formatter) markdownToHTML(in []rune) string {
	out := strings.Builder{}

	for i := 0; i < len(in); i++ {
		c := in[i]
		if _, ok := chars[string(c)]; !ok {
			out.WriteRune(c)
			continue
		}

		if !validStart(i, in) {
			if c == '\\' && i+1 < len(in) {
				if _, ok := chars[string(in[i+1])]; ok {
					out.WriteRune(in[i+1])
					i++
					continue
				}
			}
			out.WriteRune(c)
			continue
		}

		switch c {
		case '`', '*', '~', '_', '|':
			item := string(c)
			if c == '|' {
				if i+1 >= len(in) || in[i+1] != '|' {
					out.WriteRune(c)
					continue
				}

				item = "||"
				i++
			} else if c == '_' && i+1 < len(in) && in[i+1] == '_' { // support __
				item = "__"
				i++
			} else if c == '`' && i+2 < len(in) && in[i+1] == '`' && in[i+2] == '`' { // support ```
				item = "```"
				i += 2
			}

			if i+1 >= len(in) {
				out.WriteString(item)
				continue
			}

			idx := getValidEnd(in[i+1:], item)
			if idx < 0 {
				out.WriteString(item)
				continue
			}

			nStart, nEnd := i+1, i+idx+1

			var nestedT string
			if c == '`' {
				nestedT = string(in[nStart:nEnd])
			} else {
				nestedT = f.markdownToHTML(in[nStart:nEnd])
			}
			followT := f.markdownToHTML(in[nEnd+len(item):])

			return out.String() + "<" + chars[item] + ">" + nestedT + "</" + closeSpans(chars[item]) + ">" + followT

		case '[':
			linkText, linkURL := findLinkSections(in[i:])
			if linkText < 0 || linkURL < 0 {
				out.WriteRune(c)
				continue
			}

			content := string(in[i+linkText+2 : i+linkURL])
			text := in[i+1 : i+linkText]
			end := i + linkURL + 1
			followT := f.markdownToHTML(in[end:])
			nestedT := f.markdownToHTML(text)
			return out.String() + `<a href="` + content + `">` + nestedT + "</a>" + followT

		case ']', '(', ')':
			out.WriteRune(c)

		case '\\':
			if i+1 < len(in) {
				if _, ok := chars[string(in[i+1])]; ok {
					out.WriteRune(in[i+1])
					i++
					continue
				}
			}
			out.WriteRune(c)
		}
	}

	return out.String()
}

var chars = map[string]string{
	"`":   "code",
	"```": "pre",
	"_":   "i",
	"*":   "b",
	"~":   "s",
	"__":  "u",
	"|":   "",
	"||":  "span class=\"tg-spoiler\"",
	"[":   "",
	"]":   "",
	"(":   "",
	")":   "",
	"\\":  "",
}

var AllMarkdownV2Chars = func() []rune {
	var outString []string
	for k := range chars {
		outString = append(outString, k)
	}
	sort.Strings(outString)
	var out []rune
	for _, x := range outString {
		out = append(out, []rune(x)[0])
	}
	return out
}()

func EscapeMarkdownV2(r []rune) string {
	out := strings.Builder{}
	for i, x := range r {
		if contains(x, AllMarkdownV2Chars) {
			if i == 0 || i == len(r)-1 || validEnd(i, r) || validStart(i, r) {
				out.WriteRune('\\')
			}
		}
		out.WriteRune(x)
	}
	return out.String()
}

func validStart(pos int, input []rune) bool {
	return (pos == 0 || !(unicode.IsLetter(input[pos-1]) || unicode.IsDigit(input[pos-1]))) && !(pos == len(input)-1 || unicode.IsSpace(input[pos+1]))
}

func validEnd(pos int, input []rune) bool {
	return !(pos == 0 || unicode.IsSpace(input[pos-1])) && (pos == len(input)-1 || !(unicode.IsLetter(input[pos+1]) || unicode.IsDigit(input[pos+1])))
}

func contains(r rune, rr []rune) bool {
	for _, x := range rr {
		if r == x {
			return true
		}
	}

	return false
}

func closeSpans(s string) string {
	if !strings.HasPrefix(s, "span") {
		return s
	}

	return "span"
}

func findLinkSections(in []rune) (int, int) {
	var textEnd, linkEnd int
	var offset int
	foundTextEnd := false
	for offset < len(in) {
		idx := stringIndex(in[offset:], "](")
		if idx < 0 {
			return -1, -1
		}
		textEnd = offset + idx
		if !IsEscaped(in, textEnd) {
			foundTextEnd = true
			break
		}
		offset = textEnd + 1
	}
	if !foundTextEnd {
		return -1, -1
	}

	offset = textEnd
	for offset < len(in) {
		idx := getValidLinkEnd(in[offset:])
		if idx < 0 {
			return -1, -1
		}
		linkEnd = offset + idx
		if !IsEscaped(in, linkEnd) {
			return textEnd, linkEnd
		}
		offset = linkEnd + 1
	}
	return -1, -1

}

func getValidEnd(in []rune, s string) int {
	offset := 0
	for offset < len(in) {
		idx := stringIndex(in[offset:], s)
		if idx < 0 {
			return -1
		}

		end := offset + idx
		if validEnd(end, in) && validEnd(end+len(s)-1, in) && !IsEscaped(in, end) {
			idx = stringIndex(in[end+1:], s)
			for idx == 0 {
				end++
				idx = stringIndex(in[end+1:], s)
			}
			return end
		}
		offset = end + 1
	}
	return -1
}

func stringIndex(in []rune, s string) int {
	r := []rune(s)
	for idx := range in {
		if startsWith(in[idx:], r) {
			return idx
		}
	}
	return -1
}

func startsWith(i []rune, p []rune) bool {
	for idx, x := range p {
		if idx >= len(i) || i[idx] != x {
			return false
		}
	}
	return true
}

func IsEscaped(input []rune, pos int) bool {
	if pos == 0 {
		return false
	}

	i := pos - 1
	for ; i >= 0; i-- {
		if input[i] == '\\' {
			continue
		}
		break
	}

	return (pos-i)%2 == 0
}

func getValidLinkEnd(in []rune) int {
	offset := 0
	for offset < len(in) {
		idx := stringIndex(in[offset:], ")")
		if idx < 0 {
			return -1
		}

		end := offset + idx
		if validEnd(end, in) && !IsEscaped(in, end) {
			return end
		}
		offset = end + 1
	}
	return -1
}

var link = regexp.MustCompile(`a href="(.*)"`)
var link2 = regexp.MustCompile(`a href='(.*)'`)

func getHTMLTagOpenIndex(in []rune) int {
	for idx, c := range in {
		if c == '<' {
			return idx
		}
	}
	return -1
}

func getHTMLTagCloseIndex(in []rune) int {
	for idx, c := range in {
		if c == '>' {
			return idx
		}
	}
	return -1
}

func isClosingTag(in []rune, pos int) bool {
	if in[pos] == '<' && pos+1 < len(in) && in[pos+1] == '/' {
		return true
	}
	return false
}

func getClosingTag(in []rune, tag string) (int, int) {
	offset := 0
	subtags := 0
	for offset < len(in) {
		o := getHTMLTagOpenIndex(in[offset:])
		if o < 0 {
			return -1, -1
		}
		openingTagIdx := offset + o

		c := getHTMLTagCloseIndex(in[openingTagIdx+2:])
		if c < 0 {
			return -1, -1
		}

		closingTagIdx := openingTagIdx + 2 + c
		if string(in[openingTagIdx+1:closingTagIdx]) == tag { // found a nested tag, this is annoying
			subtags++
		} else if isClosingTag(in, openingTagIdx) && string(in[openingTagIdx+2:closingTagIdx]) == tag {
			if subtags == 0 {
				return openingTagIdx, closingTagIdx
			}
			subtags--
		}
		offset = openingTagIdx + 1
	}
	return -1, -1
}
