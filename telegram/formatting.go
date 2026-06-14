// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

func (c *Client) FormatMessage(message, mode string) ([]MessageEntity, string) {
	return parseEntities(message, mode)
}

func parseEntities(message, mode string) ([]MessageEntity, string) {
	if strings.EqualFold(mode, HTML) {
		return parseHTML(message)
	} else if strings.EqualFold(mode, MarkDown) {
		return parseMarkdown(message)
	}
	return []MessageEntity{}, message
}

func parseHTML(text string) ([]MessageEntity, string) {
	cleanedText, tags, err := parseHTMLToTags(text)
	if err != nil {
		return []MessageEntity{}, text
	}
	return parseTagsToEntity(tags), cleanedText
}

func parseMarkdown(text string) ([]MessageEntity, string) {
	htmlStr := HTMLToMarkdownV2(text)
	return parseHTML(htmlStr)
}

// length in UTF-16 code units
type Tag struct {
	Type      string
	Length    int32
	Offset    int32
	hasNested bool
	Attrs     map[string]string
}

// supported tags by telegram, only parse these
func supportedTag(tag string) bool {
	switch tag {
	case "b", "strong", "i", "em", "u", "s", "a", "code", "pre", "ins", "del", "spoiler", "quote", "blockquote", "emoji", "mention":
		return true
	}
	return false
}

func ParseHTMLToTags(htmlStr string) (string, []Tag, error) {
	return parseHTMLToTags(htmlStr)
}

type htmlToken struct {
	isTag     bool
	isClosing bool
	tagName   string
	attrs     map[string]string
	text      string
}

func simpleHTMLTokenize(html string) []htmlToken {
	var tokens = make([]htmlToken, 0, strings.Count(html, "<")+1)
	i := 0

	for i < len(html) {
		if html[i] == '<' {
			tagEnd := i + 1
			for tagEnd < len(html) && html[tagEnd] != '>' {
				tagEnd++
			}

			if tagEnd >= len(html) {
				tokens = append(tokens, htmlToken{isTag: false, text: html[i:]})
				break
			}

			tagContent := html[i+1 : tagEnd]
			isClosing := strings.HasPrefix(tagContent, "/")

			if isClosing {
				tagContent = tagContent[1:]
			}

			parts := strings.Fields(tagContent)
			if len(parts) > 0 {
				tagName := parts[0]
				attrs := make(map[string]string)

				for _, part := range parts[1:] {
					if strings.Contains(part, "=") {
						kv := strings.SplitN(part, "=", 2)
						key := kv[0]
						value := strings.Trim(kv[1], "\"'")
						attrs[key] = value
					} else {
						attrs[part] = "true"
					}
				}

				tokens = append(tokens, htmlToken{
					isTag:     true,
					isClosing: isClosing,
					tagName:   tagName,
					attrs:     attrs,
					text:      html[i : tagEnd+1],
				})
			} else {
				tokens = append(tokens, htmlToken{
					isTag: false,
					text:  html[i : tagEnd+1],
				})
			}

			i = tagEnd + 1
		} else {
			textStart := i
			for i < len(html) && html[i] != '<' {
				i++
			}
			tokens = append(tokens, htmlToken{
				isTag: false,
				text:  html[textStart:i],
			})
		}
	}

	return tokens
}

func parseHTMLToTags(htmlStr string) (string, []Tag, error) {
	tokens := simpleHTMLTokenize(htmlStr)

	var textBuf strings.Builder
	var tagOffsets []Tag
	var openTags []struct {
		tag    Tag
		tagIdx int
	}

	for _, token := range tokens {
		if !token.isTag {
			textBuf.WriteString(htmlUnescape(token.text))
		} else if !token.isClosing && supportedTag(token.tagName) {
			currentOffset := utf16RuneCountInString(textBuf.String())
			tag := Tag{
				Type:   token.tagName,
				Offset: currentOffset,
				Attrs:  token.attrs,
			}

			tagIdx := len(tagOffsets)
			tagOffsets = append(tagOffsets, tag)
			openTags = append(openTags, struct {
				tag    Tag
				tagIdx int
			}{tag: tag, tagIdx: tagIdx})

		} else if token.isClosing {
			matched := false
			for i := len(openTags) - 1; i >= 0; i-- {
				if openTags[i].tag.Type == token.tagName {
					currentOffset := utf16RuneCountInString(textBuf.String())
					tagOffsets[openTags[i].tagIdx].Length = currentOffset - openTags[i].tag.Offset
					openTags = append(openTags[:i], openTags[i+1:]...)
					matched = true
					break
				}
			}
			if !matched {
				textBuf.WriteString(token.text)
			}
		} else {
			textBuf.WriteString(token.text)
		}
	}

	// close unclosed tags
	currentOffset := utf16RuneCountInString(textBuf.String())
	for _, openTag := range openTags {
		tagOffsets[openTag.tagIdx].Length = currentOffset - openTag.tag.Offset
	}

	originalText := textBuf.String()
	cleanedText := strings.TrimSpace(originalText)

	leadingTrimmed := utf16RuneCountInString(originalText) - utf16RuneCountInString(strings.TrimLeft(originalText, " \t\n\r"))
	cleanedTextLen := utf16RuneCountInString(cleanedText)

	var newTagOffsets []Tag
	for _, tag := range tagOffsets {
		newOffset := max(tag.Offset-leadingTrimmed, 0)

		endPos := min(tag.Offset+tag.Length-leadingTrimmed, cleanedTextLen)

		newLength := endPos - newOffset
		if newLength > 0 {
			newTagOffsets = append(newTagOffsets, Tag{
				Type:      tag.Type,
				Length:    newLength,
				Offset:    newOffset,
				hasNested: tag.hasNested,
				Attrs:     tag.Attrs,
			})
		}
	}

	return cleanedText, newTagOffsets, nil
}

func htmlUnescape(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&#x27;", "'")
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func utf16RuneCountInString(s string) int32 {
	return int32(len(utf16.Encode([]rune(s))))
}

func parseTagsToEntity(tags []Tag) []MessageEntity {
	entities := make([]MessageEntity, 0, len(tags))
	for _, tag := range tags {
		switch tag.Type {
		case "a":
			switch {
			case tag.Attrs["href"] != "" && strings.HasPrefix(tag.Attrs["href"], "mailto:"):
				entities = append(entities, &MessageEntityEmail{tag.Offset, tag.Length})
			case tag.Attrs["href"] != "" && strings.HasPrefix(tag.Attrs["href"], "tg://emoji"):
				u, err := url.Parse(tag.Attrs["href"])
				if err == nil {
					id := u.Query().Get("id")
					if id != "" {
						emojiID, err := strconv.ParseInt(id, 10, 64)
						if err == nil {
							entities = append(entities, &MessageEntityCustomEmoji{
								Offset:     tag.Offset,
								Length:     tag.Length,
								DocumentID: emojiID,
							})
						}
					}
				}

			case tag.Attrs["href"] == "":
				entities = append(entities, &MessageEntityURL{tag.Offset, tag.Length})
			default:
				if after, ok := strings.CutPrefix(tag.Attrs["href"], "tg://user?id="); ok {
					idStr := after
					id, err := strconv.ParseInt(idStr, 10, 64)
					if err == nil {
						entities = append(entities, &InputMessageEntityMentionName{
							Offset: tag.Offset,
							Length: tag.Length,
							UserID: &InputUserObj{
								UserID:     id,
								AccessHash: 0,
							},
						})
						continue
					}
				}
				entities = append(entities, &MessageEntityTextURL{tag.Offset, tag.Length, tag.Attrs["href"]})
			}
		case "b", "strong":
			entities = append(entities, &MessageEntityBold{tag.Offset, tag.Length})
		case "code":
			entities = append(entities, &MessageEntityCode{tag.Offset, tag.Length})
		case "em", "i":
			entities = append(entities, &MessageEntityItalic{tag.Offset, tag.Length})
		case "pre":
			entities = append(entities, &MessageEntityPre{tag.Offset, tag.Length, tag.Attrs["language"]})
		case "s", "strike", "del":
			entities = append(entities, &MessageEntityStrike{tag.Offset, tag.Length})
		case "u":
			entities = append(entities, &MessageEntityUnderline{tag.Offset, tag.Length})
		case "mention":
			entities = append(entities, &MessageEntityMention{tag.Offset, tag.Length})
		case "spoiler":
			entities = append(entities, &MessageEntitySpoiler{tag.Offset, tag.Length})
		case "quote", "blockquote":
			isCollapsed := false
			if collapsed, err := strconv.ParseBool(tag.Attrs["collapsed"]); err == nil {
				isCollapsed = collapsed
			}
			if _, hasExpandable := tag.Attrs["expandable"]; hasExpandable {
				isCollapsed = true
			}
			entities = append(entities, &MessageEntityBlockquote{isCollapsed, tag.Offset, tag.Length})
		case "emoji":
			emojiID, err := strconv.ParseInt(tag.Attrs["id"], 10, 64)
			if err != nil {
				continue
			}
			entities = append(entities, &MessageEntityCustomEmoji{tag.Offset, tag.Length, emojiID})
		}
	}
	return entities
}

func ParseEntitiesToTags(entities []MessageEntity) []Tag {
	tags := make([]Tag, 0, len(entities))
	for _, entity := range entities {
		switch entity := entity.(type) {
		case *MessageEntityBold:
			tags = append(tags, Tag{Type: "b", Length: entity.Length, Offset: entity.Offset})
		case *MessageEntityItalic:
			tags = append(tags, Tag{Type: "i", Length: entity.Length, Offset: entity.Offset})
		case *MessageEntityUnderline:
			tags = append(tags, Tag{Type: "u", Length: entity.Length, Offset: entity.Offset})
		case *MessageEntityStrike:
			tags = append(tags, Tag{Type: "s", Length: entity.Length, Offset: entity.Offset})
		case *MessageEntitySpoiler:
			tags = append(tags, Tag{Type: "spoiler", Length: entity.Length, Offset: entity.Offset})
		case *MessageEntityCode:
			tags = append(tags, Tag{Type: "code", Length: entity.Length, Offset: entity.Offset})
		case *MessageEntityPre:
			tags = append(tags, Tag{Type: "pre", Length: entity.Length, Offset: entity.Offset, Attrs: map[string]string{"language": entity.Language}})
		case *MessageEntityURL:
			tags = append(tags, Tag{Type: "a", Length: entity.Length, Offset: entity.Offset, Attrs: map[string]string{"href": ""}})
		case *MessageEntityTextURL:
			tags = append(tags, Tag{Type: "a", Length: entity.Length, Offset: entity.Offset, Attrs: map[string]string{"href": entity.URL}})
		case *MessageEntityEmail:
			tags = append(tags, Tag{Type: "a", Length: entity.Length, Offset: entity.Offset, Attrs: map[string]string{"href": "mailto:"}})
		case *MessageEntityMention:
			tags = append(tags, Tag{Type: "mention", Length: entity.Length, Offset: entity.Offset})
		case *MessageEntityBlockquote:
			tags = append(tags, Tag{Type: "blockquote", Length: entity.Length, Offset: entity.Offset, Attrs: map[string]string{"collapsed": strconv.FormatBool(entity.Collapsed)}})
		case *MessageEntityCustomEmoji:
			tags = append(tags, Tag{Type: "emoji", Length: entity.Length, Offset: entity.Offset, Attrs: map[string]string{"id": strconv.FormatInt(entity.DocumentID, 10)}})
		}
	}
	return tags
}

func InsertTagsIntoText(text string, tags []Tag) string {
	utf16Text := utf16.Encode([]rune(text))
	openTags := make(map[int][]Tag)
	closeTags := make(map[int][]Tag)

	for _, tag := range tags {
		openTags[int(tag.Offset)] = append(openTags[int(tag.Offset)], tag)
		closeTags[int(tag.Offset+tag.Length)] = append(closeTags[int(tag.Offset+tag.Length)], tag)
	}

	result := make([]uint16, 0, len(utf16Text)*2)
	for i := range utf16Text {
		if opening, exists := openTags[i]; exists {
			for _, tag := range opening {
				var utf16tag []uint16

				if len(tag.Attrs) > 0 {
					attrStr := ""
					for k, v := range tag.Attrs {
						attrStr += fmt.Sprintf(" %s=\"%s\"", k, v)
					}

					utf16tag = utf16.Encode([]rune(fmt.Sprintf("<%s%s>", tag.Type, attrStr)))
				} else {
					utf16tag = utf16.Encode([]rune(fmt.Sprintf("<%s>", tag.Type)))
				}

				result = append(result, utf16tag...)
			}
		}
		result = append(result, utf16Text[i])

		if closing, exists := closeTags[i+1]; exists {
			for j := len(closing) - 1; j >= 0; j-- {
				utf16tag := utf16.Encode([]rune(fmt.Sprintf("</%s>", closing[j].Type)))
				result = append(result, utf16tag...)
			}
		}
	}
	return string(utf16.Decode(result))
}

var (
	mdLinkRe       = regexp.MustCompile(`<a\s+href="([^"]*)"[^>]*>([^<]*)</a>`)
	mdEmojiRe      = regexp.MustCompile(`<emoji\s+id="(\d+)"[^>]*>[^<]*</emoji>`)
	mdPreLangRe    = regexp.MustCompile(`<pre><code\s+class="language-([^"]+)">([^<]*)</code></pre>`)
	mdPreRe        = regexp.MustCompile(`<pre><code>([^<]*)</code></pre>`)
	mdBlockquoteRe = regexp.MustCompile(`<blockquote(?:\s+collapsed="(true|false)")?[^>]*>([^<]*)</blockquote>`)
)

func ToMarkdown(htmlStr string) string {
	if htmlStr == "" {
		return ""
	}
	htmlStr = mdLinkRe.ReplaceAllString(htmlStr, "[$2]($1)")
	htmlStr = mdEmojiRe.ReplaceAllString(htmlStr, "::$1::")
	htmlStr = mdPreLangRe.ReplaceAllString(htmlStr, "```$1\n$2```")
	htmlStr = mdPreRe.ReplaceAllString(htmlStr, "```$1```")

	htmlStr = mdBlockquoteRe.ReplaceAllStringFunc(htmlStr, func(m string) string {
		parts := mdBlockquoteRe.FindStringSubmatch(m)
		prefix := "> "
		if len(parts) > 1 && parts[1] == "true" {
			prefix = ">> "
		}
		lines := strings.Split(parts[2], "\n")
		for i, line := range lines {
			if line != "" {
				lines[i] = prefix + line
			}
		}
		return strings.Join(lines, "\n")
	})

	replacer := strings.NewReplacer(
		"<b>", "**", "</b>", "**",
		"<strong>", "**", "</strong>", "**",
		"<i>", "__", "</i>", "__",
		"<em>", "__", "</em>", "__",
		"<u>", "--", "</u>", "--",
		"<s>", "~~", "</s>", "~~",
		"<code>", "`", "</code>", "`",
		"<spoiler>", "||", "</spoiler>", "||",
	)
	return replacer.Replace(htmlStr)
}

func HTMLToMarkdownV2(markdown string) string {
	if markdown == "" {
		return ""
	}

	markdown, placeholders := handleEscapes(markdown)
	markdown = convertCodeBlockSyntax(markdown)
	markdown = convertCodeSyntax(markdown)

	for _, conv := range [][3]string{
		{"**", "<b>", "</b>"},
		{"__", "<i>", "</i>"},
		{"!!", "<u>", "</u>"},
		{"--", "<u>", "</u>"},
		{"~~", "<s>", "</s>"},
		{"||", "<spoiler>", "</spoiler>"},
	} {
		markdown = convertSyntax(markdown, conv[0], conv[1], conv[2])
	}

	markdown = convertLinksSyntax(markdown)
	markdown = convertEmojiSyntax(markdown)
	markdown = convertBlockquoteSyntax(markdown)
	markdown = restoreEscapes(markdown, placeholders)

	return strings.TrimSpace(markdown)
}

var escapeChars = []string{"*", "_", "~", "|", "`", "[", "]", "(", ")", "{", "}", "<", ">", "!"}

func handleEscapes(markdown string) (string, map[string]string) {
	placeholders := make(map[string]string, len(escapeChars))
	for i, ch := range escapeChars {
		esc := "\\" + ch
		placeholder := "\x00ESC" + strconv.Itoa(i) + "\x00"
		placeholders[placeholder] = ch
		markdown = strings.ReplaceAll(markdown, esc, placeholder)
	}
	return markdown, placeholders
}

func restoreEscapes(markdown string, placeholders map[string]string) string {
	for placeholder, ch := range placeholders {
		markdown = strings.ReplaceAll(markdown, placeholder, ch)
	}
	return markdown
}

func convertSyntax(markdown, delim, openTag, closeTag string) string {
	delimLen := len(delim)
	for {
		start := strings.Index(markdown, delim)
		if start == -1 {
			break
		}
		rest := markdown[start+delimLen:]
		end := strings.Index(rest, delim)
		if end == -1 {
			break
		}
		content := rest[:end]
		if content == "" { // skip empty: ****, etc.
			break
		}
		markdown = markdown[:start] + openTag + content + closeTag + rest[end+delimLen:]
	}
	return markdown
}

func convertCodeSyntax(markdown string) string {
	for {
		start := strings.Index(markdown, "`")
		if start == -1 {
			break
		}
		if start+2 < len(markdown) && markdown[start:start+3] == "```" {
			break
		}
		rest := markdown[start+1:]
		end := strings.Index(rest, "`")
		if end == -1 || end == 0 {
			break
		}
		content := htmlEscape(rest[:end])
		markdown = markdown[:start] + "<code>" + content + "</code>" + rest[end+1:]
	}
	return markdown
}

func convertCodeBlockSyntax(markdown string) string {
	const fence = "```"
	for {
		start := strings.Index(markdown, fence)
		if start == -1 {
			break
		}
		rest := markdown[start+3:]
		end := strings.Index(rest, fence)
		if end == -1 {
			break
		}

		block := rest[:end]
		var lang, code string
		if idx := strings.Index(block, "\n"); idx != -1 {
			lang = strings.TrimSpace(block[:idx])
			code = block[idx+1:]
		} else {
			code = block
		}

		escaped := htmlEscape(code)
		var replacement string
		if lang != "" {
			replacement = "<pre><code class=\"language-" + htmlEscape(lang) + "\">" + escaped + "</code></pre>"
		} else {
			replacement = "<pre><code>" + escaped + "</code></pre>"
		}
		markdown = markdown[:start] + replacement + rest[end+3:]
	}
	return markdown
}

func convertLinksSyntax(markdown string) string {
	var b strings.Builder
	b.Grow(len(markdown))
	i := 0
	for i < len(markdown) {
		if markdown[i] != '[' {
			b.WriteByte(markdown[i])
			i++
			continue
		}
		// find matching ']'
		end := strings.IndexByte(markdown[i+1:], ']')
		if end < 0 || i+1+end+1 >= len(markdown) || markdown[i+1+end+1] != '(' {
			b.WriteByte(markdown[i])
			i++
			continue
		}
		labelStart := i + 1
		labelEnd := i + 1 + end
		urlStart := labelEnd + 2
		// scan url with paren-depth tracking so URLs containing '(' / ')' work
		depth := 1
		j := urlStart
		for j < len(markdown) && depth > 0 {
			switch markdown[j] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 {
				break
			}
			j++
		}
		if depth != 0 {
			b.WriteByte(markdown[i])
			i++
			continue
		}
		label := markdown[labelStart:labelEnd]
		url := markdown[urlStart:j]
		fmt.Fprintf(&b, `<a href="%s">%s</a>`, htmlEscape(url), htmlEscape(label))
		i = j + 1
	}
	return b.String()
}

func convertEmojiSyntax(markdown string) string {
	const delim = "::"
	for {
		start := strings.Index(markdown, delim)
		if start == -1 {
			break
		}
		rest := markdown[start+2:]
		end := strings.Index(rest, delim)
		if end == -1 || end == 0 {
			break
		}
		emojiID := rest[:end]
		if _, err := strconv.ParseInt(emojiID, 10, 64); err != nil {
			break
		}
		markdown = markdown[:start] + "<emoji id=\"" + emojiID + "\"></emoji>" + rest[end+2:]
	}
	return markdown
}

func convertBlockquoteSyntax(markdown string) string {
	lines := strings.Split(markdown, "\n")
	if len(lines) == 0 {
		return markdown
	}

	var result strings.Builder
	result.Grow(len(markdown) + 100)

	var inBlockquote, isCollapsed bool

	closeBlockquote := func() {
		if inBlockquote {
			result.WriteString("</blockquote>\n")
			inBlockquote, isCollapsed = false, false
		}
	}

	openBlockquote := func(collapsed bool) {
		if inBlockquote && isCollapsed != collapsed {
			closeBlockquote()
		}
		if !inBlockquote {
			if collapsed {
				result.WriteString("<blockquote collapsed=\"true\">")
			} else {
				result.WriteString("<blockquote>")
			}
			inBlockquote, isCollapsed = true, collapsed
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, ">> "):
			openBlockquote(true)
			result.WriteString(trimmed[3:] + "\n")
		case strings.HasPrefix(trimmed, "> "):
			openBlockquote(false)
			result.WriteString(trimmed[2:] + "\n")
		default:
			closeBlockquote()
			result.WriteString(line + "\n")
		}
	}
	closeBlockquote()

	return result.String()
}

type RichBuilder struct {
	mode       string
	body       string
	rtl        bool
	noAutoLink bool
	photos     []InputPhoto
	docs       []InputDocument
	users      []InputUser
	blocks     []PageBlock
	pending    []*pendingAttach
	resolved   bool
}

type pendingAttach struct {
	kind     string
	source   any
	upload   *UploadOptions
	mimeType string
	fileName string
}

func NewRichMessage() *RichBuilder {
	return &RichBuilder{mode: "markdown"}
}

func (r *RichBuilder) Markdown(text string) *RichBuilder {
	r.mode = "markdown"
	r.body = text
	r.blocks = nil
	return r
}

func (r *RichBuilder) HTML(text string) *RichBuilder {
	r.mode = "html"
	r.body = text
	r.blocks = nil
	return r
}

func (r *RichBuilder) Blocks(blocks ...PageBlock) *RichBuilder {
	r.mode = "obj"
	r.body = ""
	r.blocks = append(r.blocks, blocks...)
	return r
}

func (r *RichBuilder) AddBlock(block PageBlock) *RichBuilder {
	r.mode = "obj"
	r.blocks = append(r.blocks, block)
	return r
}

func (r *RichBuilder) RTL() *RichBuilder {
	r.rtl = true
	return r
}

func (r *RichBuilder) NoAutoLink() *RichBuilder {
	r.noAutoLink = true
	return r
}

func (r *RichBuilder) AddPhoto(source any, opts ...*UploadOptions) *RichBuilder {
	r.pending = append(r.pending, &pendingAttach{
		kind:   "photo",
		source: source,
		upload: getVariadic(opts, (*UploadOptions)(nil)),
	})
	return r
}

func (r *RichBuilder) AddDocument(source any, opts ...*UploadOptions) *RichBuilder {
	r.pending = append(r.pending, &pendingAttach{
		kind:   "document",
		source: source,
		upload: getVariadic(opts, (*UploadOptions)(nil)),
	})
	return r
}

func (r *RichBuilder) AddVideo(source any, opts ...*UploadOptions) *RichBuilder {
	r.pending = append(r.pending, &pendingAttach{
		kind:     "document",
		source:   source,
		upload:   getVariadic(opts, (*UploadOptions)(nil)),
		mimeType: "video/mp4",
	})
	return r
}

func (r *RichBuilder) AddAudio(source any, opts ...*UploadOptions) *RichBuilder {
	r.pending = append(r.pending, &pendingAttach{
		kind:     "document",
		source:   source,
		upload:   getVariadic(opts, (*UploadOptions)(nil)),
		mimeType: "audio/mpeg",
	})
	return r
}

func (r *RichBuilder) AttachPhoto(photo InputPhoto) *RichBuilder {
	r.photos = append(r.photos, photo)
	return r
}

func (r *RichBuilder) AttachDocument(doc InputDocument) *RichBuilder {
	r.docs = append(r.docs, doc)
	return r
}

func (r *RichBuilder) AddUser(user any) *RichBuilder {
	r.pending = append(r.pending, &pendingAttach{kind: "user", source: user})
	return r
}

func (r *RichBuilder) AttachUser(user InputUser) *RichBuilder {
	r.users = append(r.users, user)
	return r
}

func (r *RichBuilder) Carousel(items ...PageBlock) *RichBuilder {
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockCollage{Items: items})
	return r
}

func (r *RichBuilder) Slideshow(items ...PageBlock) *RichBuilder {
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockSlideshow{Items: items})
	return r
}

func (r *RichBuilder) Paragraph(text string) *RichBuilder {
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockParagraph{Text: &TextPlain{Text: text}})
	return r
}

func (r *RichBuilder) Heading(text string) *RichBuilder {
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockHeader{Text: &TextPlain{Text: text}})
	return r
}

func (r *RichBuilder) Divider() *RichBuilder {
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockDivider{})
	return r
}

func (r *RichBuilder) PhotoBlock(photo InputPhoto, caption string) *RichBuilder {
	id := photoID(photo)
	r.photos = append(r.photos, photo)
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockPhoto{
		PhotoID: id,
		Caption: &PageCaption{Text: &TextPlain{Text: caption}, Credit: &TextEmpty{}},
	})
	return r
}

func (r *RichBuilder) VideoBlock(doc InputDocument, caption string, autoplay, loop bool) *RichBuilder {
	id := documentID(doc)
	r.docs = append(r.docs, doc)
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockVideo{
		Autoplay: autoplay,
		Loop:     loop,
		VideoID:  id,
		Caption:  &PageCaption{Text: &TextPlain{Text: caption}, Credit: &TextEmpty{}},
	})
	return r
}

func (r *RichBuilder) SlideshowPhotos(photos ...InputPhoto) *RichBuilder {
	items := make([]PageBlock, 0, len(photos))
	for _, p := range photos {
		r.photos = append(r.photos, p)
		items = append(items, &PageBlockPhoto{PhotoID: photoID(p), Caption: EmptyCaption()})
	}
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockSlideshow{Items: items, Caption: EmptyCaption()})
	return r
}

func (r *RichBuilder) CollagePhotos(photos ...InputPhoto) *RichBuilder {
	items := make([]PageBlock, 0, len(photos))
	for _, p := range photos {
		r.photos = append(r.photos, p)
		items = append(items, &PageBlockPhoto{PhotoID: photoID(p), Caption: EmptyCaption()})
	}
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockCollage{Items: items, Caption: EmptyCaption()})
	return r
}

type TableOptions struct {
	Title    any
	Bordered bool
	Striped  bool
	Header   bool // first row is a header row
}

func (r *RichBuilder) Table(rows [][]any, opts ...*TableOptions) *RichBuilder {
	opt := getVariadic(opts, &TableOptions{})
	tableRows := make([]*PageTableRow, 0, len(rows))
	for i, row := range rows {
		cells := make([]*PageTableCell, 0, len(row))
		header := opt.Header && i == 0
		for _, cell := range row {
			cells = append(cells, &PageTableCell{Header: header, Text: toRich(cell)})
		}
		tableRows = append(tableRows, &PageTableRow{Cells: cells})
	}
	title := RichText(&TextEmpty{})
	if opt.Title != nil {
		title = toRich(opt.Title)
	}
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockTable{
		Bordered: opt.Bordered,
		Striped:  opt.Striped,
		Title:    title,
		Rows:     tableRows,
	})
	return r
}

func (r *RichBuilder) Details(title any, blocks ...PageBlock) *RichBuilder {
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockDetails{Title: toRich(title), Blocks: blocks})
	return r
}

func (r *RichBuilder) Quote(text any) *RichBuilder {
	r.mode = "obj"
	r.blocks = append(r.blocks, &PageBlockPullquote{Text: toRich(text), Caption: &TextEmpty{}})
	return r
}

func photoID(p InputPhoto) int64 {
	if v, ok := p.(*InputPhotoObj); ok {
		return v.ID
	}
	return 0
}

func documentID(d InputDocument) int64 {
	if v, ok := d.(*InputDocumentObj); ok {
		return v.ID
	}
	return 0
}

func (r *RichBuilder) Latex(s string) *RichBuilder {
	r.body += ConvertLatex(s)
	if r.mode == "" {
		r.mode = "markdown"
	}
	return r
}

func (r *RichBuilder) resolve(c *Client) error {
	if r.resolved {
		return nil
	}
	for _, p := range r.pending {
		switch p.kind {
		case "photo":
			if p.source == nil {
				continue
			}
			if ph, ok := p.source.(InputPhoto); ok {
				r.photos = append(r.photos, ph)
				continue
			}
			media, err := c.GetSendableMedia(p.source, &MediaMetadata{Upload: p.upload, Inline: true})
			if err != nil {
				return fmt.Errorf("attach photo: %w", err)
			}
			if mp, ok := media.(*InputMediaPhoto); ok {
				r.photos = append(r.photos, mp.ID)
			} else {
				return fmt.Errorf("attach photo: not a photo (got %T)", media)
			}
		case "document":
			if p.source == nil {
				continue
			}
			if dc, ok := p.source.(InputDocument); ok {
				r.docs = append(r.docs, dc)
				continue
			}
			media, err := c.GetSendableMedia(p.source, &MediaMetadata{
				Upload:   p.upload,
				MimeType: p.mimeType,
				FileName: p.fileName,
				Inline:   true,
			})
			if err != nil {
				return fmt.Errorf("attach document: %w", err)
			}
			if md, ok := media.(*InputMediaDocument); ok {
				r.docs = append(r.docs, md.ID)
			} else {
				return fmt.Errorf("attach document: not a document (got %T)", media)
			}
		case "user":
			user, err := c.GetSendableUser(p.source)
			if err != nil {
				return fmt.Errorf("resolve user mention: %w", err)
			}
			r.users = append(r.users, user)
		}
	}
	r.pending = nil
	r.resolved = true
	if r.mode == "markdown" && containsLatex(r.body) {
		r.body = ConvertLatex(r.body)
	}
	return nil
}

func (r *RichBuilder) build() InputRichMessage {
	switch r.mode {
	case "html":
		return &InputRichMessageHtml{
			Rtl:        r.rtl,
			Noautolink: r.noAutoLink,
			Html:       r.body,
			Files:      r.packFiles(),
		}
	case "obj":
		return &InputRichMessageObj{
			Rtl:        r.rtl,
			Noautolink: r.noAutoLink,
			Blocks:     r.blocks,
			Photos:     r.photos,
			Documents:  r.docs,
			Users:      r.users,
		}
	default:
		return &InputRichMessageMarkdown{
			Rtl:        r.rtl,
			Noautolink: r.noAutoLink,
			Markdown:   r.body,
			Files:      r.packFiles(),
		}
	}
}

func (r *RichBuilder) packFiles() []InputRichFile {
	if len(r.photos) == 0 && len(r.docs) == 0 {
		return nil
	}
	files := make([]InputRichFile, 0, len(r.photos)+len(r.docs))
	for i, p := range r.photos {
		files = append(files, &InputRichFilePhoto{
			ID:    fmt.Sprintf("photo_%d", i),
			Photo: p,
		})
	}
	for i, d := range r.docs {
		files = append(files, &InputRichFileDocument{
			ID:       fmt.Sprintf("doc_%d", i),
			Document: d,
		})
	}
	return files
}

var latexSymbols = map[string]string{
	`\alpha`: "α", `\beta`: "β", `\gamma`: "γ", `\delta`: "δ", `\epsilon`: "ε",
	`\varepsilon`: "ε", `\zeta`: "ζ", `\eta`: "η", `\theta`: "θ", `\vartheta`: "ϑ",
	`\iota`: "ι", `\kappa`: "κ", `\lambda`: "λ", `\mu`: "μ", `\nu`: "ν",
	`\xi`: "ξ", `\pi`: "π", `\varpi`: "ϖ", `\rho`: "ρ", `\varrho`: "ϱ",
	`\sigma`: "σ", `\varsigma`: "ς", `\tau`: "τ", `\upsilon`: "υ", `\phi`: "φ",
	`\varphi`: "ϕ", `\chi`: "χ", `\psi`: "ψ", `\omega`: "ω",
	`\Gamma`: "Γ", `\Delta`: "Δ", `\Theta`: "Θ", `\Lambda`: "Λ", `\Xi`: "Ξ",
	`\Pi`: "Π", `\Sigma`: "Σ", `\Upsilon`: "Υ", `\Phi`: "Φ", `\Psi`: "Ψ", `\Omega`: "Ω",

	`\pm`: "±", `\mp`: "∓", `\times`: "×", `\div`: "÷", `\cdot`: "·",
	`\ast`: "∗", `\star`: "⋆", `\circ`: "∘", `\bullet`: "•",
	`\neq`: "≠", `\ne`: "≠", `\leq`: "≤", `\le`: "≤", `\geq`: "≥", `\ge`: "≥",
	`\ll`: "≪", `\gg`: "≫", `\approx`: "≈", `\equiv`: "≡", `\sim`: "∼",
	`\simeq`: "≃", `\cong`: "≅", `\propto`: "∝",
	`\infty`: "∞", `\partial`: "∂", `\nabla`: "∇", `\sqrt`: "√",

	`\in`: "∈", `\notin`: "∉", `\ni`: "∋", `\subset`: "⊂", `\supset`: "⊃",
	`\subseteq`: "⊆", `\supseteq`: "⊇", `\cup`: "∪", `\cap`: "∩",
	`\setminus`: "∖", `\emptyset`: "∅", `\varnothing`: "∅",

	`\forall`: "∀", `\exists`: "∃", `\nexists`: "∄", `\neg`: "¬", `\lnot`: "¬",
	`\land`: "∧", `\wedge`: "∧", `\lor`: "∨", `\vee`: "∨",
	`\implies`: "⇒", `\Rightarrow`: "⇒", `\iff`: "⇔", `\Leftrightarrow`: "⇔",
	`\to`: "→", `\rightarrow`: "→", `\leftarrow`: "←", `\Leftarrow`: "⇐",
	`\mapsto`: "↦", `\uparrow`: "↑", `\downarrow`: "↓",

	`\sum`: "∑", `\prod`: "∏", `\int`: "∫", `\iint`: "∬", `\iiint`: "∭",
	`\oint`: "∮", `\coprod`: "∐", `\bigcup`: "⋃", `\bigcap`: "⋂",
	`\bigvee`: "⋁", `\bigwedge`: "⋀", `\bigoplus`: "⊕", `\bigotimes`: "⊗",

	`\hbar`: "ℏ", `\ell`: "ℓ", `\Re`: "ℜ", `\Im`: "ℑ", `\aleph`: "ℵ",
	`\degree`: "°", `\angle`: "∠", `\perp`: "⊥", `\parallel`: "∥",
	`\dots`: "…", `\ldots`: "…", `\cdots`: "⋯", `\vdots`: "⋮", `\ddots`: "⋱",
	`\prime`: "′", `\dagger`: "†", `\ddagger`: "‡",
	`\copyright`: "©", `\pounds`: "£", `\euro`: "€",

	`\quad`: " ", `\qquad`: "  ", `\,`: " ", `\;`: " ", `\:`: " ", `\!`: "",
	`\left`: "", `\right`: "", `\big`: "", `\Big`: "", `\bigg`: "", `\Bigg`: "",
	`\displaystyle`: "", `\textstyle`: "", `\mathrm`: "", `\mathit`: "",
	`\mathbf`: "", `\mathsf`: "", `\mathtt`: "", `\mathcal`: "", `\mathbb`: "",
}

var (
	supDigitMap = map[rune]rune{
		'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴',
		'5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
		'+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾',
		'n': 'ⁿ', 'i': 'ⁱ',
	}
	subDigitMap = map[rune]rune{
		'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄',
		'5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
		'+': '₊', '-': '₋', '=': '₌', '(': '₍', ')': '₎',
		'a': 'ₐ', 'e': 'ₑ', 'i': 'ᵢ', 'o': 'ₒ', 'x': 'ₓ',
	}
	latexCmdRe   = regexp.MustCompile(`\\[a-zA-Z]+`)
	supScriptRe  = regexp.MustCompile(`\^(\{([^{}]+)\}|(\w))`)
	subScriptRe  = regexp.MustCompile(`_(\{([^{}]+)\}|(\w))`)
	fracRe       = regexp.MustCompile(`\\frac\s*\{([^{}]+)\}\s*\{([^{}]+)\}`)
	sqrtRe       = regexp.MustCompile(`\\sqrt\s*\{([^{}]+)\}`)
	mathInlineRe = regexp.MustCompile(`\$([^$\n]+)\$`)
	mathBlockRe  = regexp.MustCompile(`\$\$([\s\S]+?)\$\$`)
)

func containsLatex(s string) bool {
	return strings.ContainsAny(s, "$\\") && (strings.Contains(s, "$") || latexCmdRe.MatchString(s))
}

func ConvertLatex(s string) string {
	s = mathBlockRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.TrimPrefix(strings.TrimSuffix(m, "$$"), "$$")
		return "\n" + convertLatexInner(inner) + "\n"
	})
	s = mathInlineRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.TrimPrefix(strings.TrimSuffix(m, "$"), "$")
		return convertLatexInner(inner)
	})
	return s
}

func convertLatexInner(s string) string {
	s = fracRe.ReplaceAllString(s, "($1)/($2)")
	s = sqrtRe.ReplaceAllString(s, "√($1)")

	s = latexCmdRe.ReplaceAllStringFunc(s, func(cmd string) string {
		if rep, ok := latexSymbols[cmd]; ok {
			return rep
		}
		return cmd
	})

	s = supScriptRe.ReplaceAllStringFunc(s, func(m string) string {
		content := stripGroup(strings.TrimPrefix(m, "^"))
		return mapScript(content, supDigitMap)
	})
	s = subScriptRe.ReplaceAllStringFunc(s, func(m string) string {
		content := stripGroup(strings.TrimPrefix(m, "_"))
		return mapScript(content, subDigitMap)
	})

	s = strings.ReplaceAll(s, "{", "")
	s = strings.ReplaceAll(s, "}", "")
	return s
}

func stripGroup(s string) string {
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		return s[1 : len(s)-1]
	}
	return s
}

func mapScript(s string, table map[rune]rune) string {
	var b strings.Builder
	for _, r := range s {
		if mapped, ok := table[r]; ok {
			b.WriteRune(mapped)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func RichPlain(text string) RichText { return &TextPlain{Text: text} }
func RichEmpty() RichText            { return &TextEmpty{} }

func RichBold(text any) RichText      { return &TextBold{Text: toRich(text)} }
func RichItalic(text any) RichText    { return &TextItalic{Text: toRich(text)} }
func RichFixed(text any) RichText     { return &TextFixed{Text: toRich(text)} }
func RichMarked(text any) RichText    { return &TextMarked{Text: toRich(text)} }
func RichUnderline(text any) RichText { return &TextUnderline{Text: toRich(text)} }
func RichStrike(text any) RichText    { return &TextStrike{Text: toRich(text)} }

func RichLink(text any, url string) RichText {
	return &TextURL{Text: toRich(text), URL: url}
}

func RichConcat(parts ...RichText) RichText {
	if len(parts) == 1 {
		return parts[0]
	}
	return &TextConcat{Texts: parts}
}

func toRich(v any) RichText {
	switch t := v.(type) {
	case nil:
		return &TextEmpty{}
	case RichText:
		return t
	case string:
		return &TextPlain{Text: t}
	default:
		return &TextPlain{Text: fmt.Sprint(t)}
	}
}

func EmptyCaption() *PageCaption {
	return &PageCaption{Text: &TextEmpty{}, Credit: &TextEmpty{}}
}

func Caption(text any) *PageCaption {
	return &PageCaption{Text: toRich(text), Credit: &TextEmpty{}}
}
