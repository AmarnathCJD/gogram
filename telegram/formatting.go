// Copyright (c) 2025 RoseLoverX

package telegram

import (
	"bytes"
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
	var tokens []htmlToken
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
			for i := len(openTags) - 1; i >= 0; i-- {
				if openTags[i].tag.Type == token.tagName {
					currentOffset := utf16RuneCountInString(textBuf.String())
					tagOffsets[openTags[i].tagIdx].Length = currentOffset - openTags[i].tag.Offset
					openTags = append(openTags[:i], openTags[i+1:]...)
					break
				}
			}
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

		endPos := tag.Offset + tag.Length - leadingTrimmed
		if endPos > cleanedTextLen {
			endPos = cleanedTextLen
		}

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
	var entities []MessageEntity
	for _, tag := range tags {
		switch tag.Type {
		case "a":
			switch {
			case tag.Attrs["href"] != "" && strings.HasPrefix(tag.Attrs["href"], "mailto:"):
				entities = append(entities, &MessageEntityEmail{tag.Offset, tag.Length})
			case tag.Attrs["href"] != "" && strings.HasPrefix(tag.Attrs["href"], "tg://user"):
				u, err := url.Parse(tag.Attrs["href"])
				if err == nil {
					id := u.Query().Get("id")
					if id != "" {
						userID, err := strconv.ParseInt(id, 10, 64)
						if err == nil {
							entities = append(entities, &InputMessageEntityMentionName{
								Offset: tag.Offset,
								Length: tag.Length,
								UserID: &InputUserObj{UserID: userID, AccessHash: 0},
							})
						}
					}
				}
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
	var tags []Tag
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
	for i := 0; i < len(utf16Text); i++ {
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

func ToMarkdown(htmlStr string) string {
	replacer := strings.NewReplacer(
		"<b>", "**",
		"</b>", "**",
		"<strong>", "**",
		"</strong>", "**",
		"<i>", "__",
		"</i>", "__",
		"<em>", "__",
		"</em>", "__",
		"<u>", "--",
		"</u>", "--",
		"<s>", "~~",
		"</s>", "~~",
		"<code>", "`",
		"</code>", "`",
		"<pre>", "```",
		"</pre>", "```",
		"<a href=\"", "[",
		"\">", "](",
		"</a>", ")",
		"<spoiler>", "||",
		"</spoiler>", "||",
		"<blockquote>", "> ",
		"<blockquote collapsed=\"false\">", "> ",
		"</blockquote>", "",
		"<blockquote collapsed=\"true\">", ">> ",
		"</blockquote>", "",
		"<emoji id=\"", "::",
		"\">", "::",
	)
	return replacer.Replace(htmlStr)
}

func HTMLToMarkdownV2(markdown string) string {
	markdown, placeholders := handleEscapes(markdown)
	markdown = convertSyntax(markdown, "**", "<b>", "</b>")
	markdown = convertSyntax(markdown, "__", "<i>", "</i>")
	markdown = convertSyntax(markdown, "__", "<i>", "</i>")
	markdown = convertSyntax(markdown, "!!", "<u>", "</u>")
	markdown = convertSyntax(markdown, "--", "<u>", "</u>")
	markdown = convertSyntax(markdown, "~~", "<s>", "</s>")
	markdown = convertSyntax(markdown, "||", "<spoiler>", "</spoiler>")
	markdown = convertCodeSyntax(markdown)
	markdown = convertCodeBlockSyntax(markdown)
	markdown = convertLinksSyntax(markdown)
	markdown = convertEmojiSyntax(markdown)
	markdown = convertBlockquotesSyntax(markdown)
	markdown = restoreEscapes(markdown, placeholders)
	return string(bytes.TrimSpace([]byte(markdown)))
}

func handleEscapes(markdown string) (string, map[string]string) {
	escapes := []string{"\\*", "\\_", "\\~", "\\|", "\\`", "\\[", "\\]", "\\(", "\\)", "\\{", "\\}", "\\<", "\\>", "\\!"}
	placeholders := make(map[string]string)

	for i, esc := range escapes {
		placeholder := "??ESC??" + strconv.Itoa(i) + "??"
		placeholders[placeholder] = escapes[i]
		markdown = strings.ReplaceAll(markdown, esc, placeholder)
	}

	return markdown, placeholders
}

func restoreEscapes(markdown string, placeholders map[string]string) string {
	for placeholder, esc := range placeholders {
		markdown = strings.ReplaceAll(markdown, placeholder, strings.ReplaceAll(esc, "\\", ""))
	}
	return markdown
}

func convertSyntax(markdown, delimiter, openTag, closeTag string) string {
	for {
		start := strings.Index(markdown, delimiter)
		if start == -1 {
			break
		}
		end := strings.Index(markdown[start+len(delimiter):], delimiter)
		if end == -1 {
			break
		}
		end += start + len(delimiter)
		markdown = markdown[:start] + openTag + markdown[start+len(delimiter):end] + closeTag + markdown[end+len(delimiter):]
	}
	return markdown
}

func convertCodeSyntax(markdown string) string {
	for {
		start := strings.Index(markdown, "`")
		if start == -1 {
			break
		}
		end := strings.Index(markdown[start+1:], "`")
		if end == -1 {
			break
		}
		end += start + 1
		content := htmlEscape(markdown[start+1 : end])
		markdown = markdown[:start] + "<code>" + content + "</code>" + markdown[end+1:]
	}
	return markdown
}

func convertCodeBlockSyntax(markdown string) string {
	for {
		start := strings.Index(markdown, "```")
		if start == -1 {
			break
		}
		end := strings.Index(markdown[start+3:], "```")
		if end == -1 {
			break
		}
		end += start + 3

		codeBlock := markdown[start+3 : end]
		var lang string
		if idx := strings.Index(codeBlock, "\n"); idx != -1 {
			lang = strings.TrimSpace(codeBlock[:idx])
			codeBlock = codeBlock[idx+1:]
		}

		content := htmlEscape(codeBlock)
		if lang != "" {
			markdown = markdown[:start] + "<pre><code class=\"language-" + htmlEscape(lang) + "\">" + content + "</code></pre>" + markdown[end+3:]
		} else {
			markdown = markdown[:start] + "<pre><code>" + content + "</code></pre>" + markdown[end+3:]
		}
	}
	return markdown
}

func convertLinksSyntax(markdown string) string {
	re := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	return re.ReplaceAllStringFunc(markdown, func(m string) string {
		parts := re.FindStringSubmatch(m)
		text := htmlEscape(parts[1])
		url := htmlEscape(parts[2])
		return fmt.Sprintf(`<a href="%s">%s</a>`, url, text)
	})
}

func convertEmojiSyntax(markdown string) string {
	for {
		start := strings.Index(markdown, "::")
		if start == -1 {
			break
		}
		end := strings.Index(markdown[start+2:], "::")
		if end == -1 {
			break
		}
		end += start + 2
		emojiID := markdown[start+2 : end]
		markdown = markdown[:start] + "<emoji id=\"" + emojiID + "\"></emoji>" + markdown[end+2:]
	}
	return markdown
}

func convertBlockquotesSyntax(markdown string) string {
	var result strings.Builder
	lines := strings.Split(markdown, "\n")
	inBlockquote := false
	isCollapsed := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, ">> ") {
			if !inBlockquote || !isCollapsed {
				if inBlockquote {
					result.WriteString("</blockquote>\n")
				}
				result.WriteString("<blockquote collapsed=\"true\">")
				inBlockquote = true
				isCollapsed = true
			}
			result.WriteString(strings.TrimPrefix(trimmed, ">> ") + "\n")
		} else if strings.HasPrefix(trimmed, "> ") {
			if !inBlockquote || isCollapsed {
				if inBlockquote {
					result.WriteString("</blockquote>\n")
				}
				result.WriteString("<blockquote>")
				inBlockquote = true
				isCollapsed = false
			}
			result.WriteString(strings.TrimPrefix(trimmed, "> ") + "\n")
		} else {
			if inBlockquote {
				result.WriteString("</blockquote>\n")
				inBlockquote = false
				isCollapsed = false
			}
			result.WriteString(line + "\n")
		}
	}

	if inBlockquote {
		result.WriteString("</blockquote>\n")
	}

	return result.String()
}
