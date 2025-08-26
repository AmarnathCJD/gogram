// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"strings"
	"unicode/utf16"

	"golang.org/x/net/html"
)

func (c *Client) FormatMessage(message, mode string) ([]MessageEntity, string) {
	return parseEntities(message, mode)
}

// parseEntities parses the message and returns a list of MessageEntities and the cleaned text string
func parseEntities(message, mode string) ([]MessageEntity, string) {
	if strings.EqualFold(mode, HTML) {
		return parseHTML(message)
	} else if strings.EqualFold(mode, MarkDown) {
		return parseMarkdown(message)
	}
	return []MessageEntity{}, message
}

// parseHTML parses HTML and returns a list of MessageEntities and the cleaned text string
func parseHTML(text string) ([]MessageEntity, string) {
	cleanedText, tags, err := parseHTMLToTags(text)
	if err != nil {
		return []MessageEntity{}, text
	}

	entities := parseTagsToEntity(tags)
	return entities, cleanedText
}

// parseMarkdown parses Markdown and returns a list of MessageEntities and the cleaned text string
func parseMarkdown(text string) ([]MessageEntity, string) {
	htmlStr := HTMLToMarkdownV2(text)
	return parseHTML(htmlStr)
}

// Tag represents a tag in the HTML string, including its type, length, and offset and whether it has nested tags, and its attrs
type Tag struct {
	Type      string `json:"type"`
	Length    int32  `json:"length"`
	Offset    int32  `json:"offset"`
	hasNested bool
	Attrs     map[string]string
}

// supportedTag returns true if the tag is supported by the parser
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

func parseHTMLToTags(htmlStr string) (string, []Tag, error) {
	// Parse the HTML string into a tree of nodes
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return "", nil, err
	}

	// Convert the tree of nodes into a string with no HTML tags
	var textBuf bytes.Buffer
	tagOffsets := []Tag{}
	var parseNode func(*html.Node, int32)
	var openTags []Tag
	parseNode = func(n *html.Node, offset int32) {
		switch n.Type {
		case html.ElementNode:
			// Only record tag information for non-body, non-html, non-head, non-p tags
			if supportedTag(n.Data) {
				tagType := n.Data
				tagLength := getTextLength(n)
				TagAttrs := make(map[string]string)
				for _, attr := range n.Attr {
					TagAttrs[attr.Key] = attr.Val
				}

				tagOffset := utf16RuneCountInString(textBuf.String())
				tagOffsets = append(tagOffsets, Tag{Type: tagType, Length: tagLength, Offset: tagOffset, Attrs: TagAttrs})

				// if tag not closed, add to open tags
				if n.FirstChild != nil && n.FirstChild.NextSibling == nil {
					openTags = append(openTags, Tag{Type: tagType, Length: tagLength, Offset: tagOffset})
				}

			}
		case html.TextNode:
			// Write the text content of this node to the buffer
			textBuf.WriteString(n.Data)
			offset += utf16RuneCountInString(n.Data)
		}

		// Recursively process child nodes
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			parseNode(c, offset)
		}

		// Check if any open tags are closed by this node
		for i := len(openTags) - 1; i >= 0; i-- {
			if openTags[i].Type == n.Data {
				openTags[i].Length = utf16RuneCountInString(textBuf.String()) - openTags[i].Offset
				openTags[i].hasNested = true
				openTags = openTags[:i]
			}
		}
	}

	parseNode(doc, 0)

	// Adjust the length of any unclosed tags at the end of the string
	lastOffset := utf16RuneCountInString(textBuf.String())
	for i := range openTags {
		openTags[i].Length = lastOffset - openTags[i].Offset
	}

	// Return the cleaned text string and tag offsets list
	cleanedText := strings.TrimSpace(textBuf.String())
	// for any tag if length is 0, remove it
	var newTagOffsets []Tag
	for _, tag := range tagOffsets {
		if tag.Length > 0 {
			newTagOffsets = append(newTagOffsets, tag)
		}
	}
	tagOffsets = newTagOffsets

	return cleanedText, tagOffsets, nil
}

func trimTrailing(input string) string {
	lastNewlineIndex := strings.LastIndex(input, "\n")
	if lastNewlineIndex != -1 && strings.TrimSpace(input[lastNewlineIndex:]) == "" {
		return input[:lastNewlineIndex]
	}

	return input
}

// getTextLength returns the length of the text content of a node, including its children
func getTextLength(n *html.Node) int32 {
	var tagLength int32 = 0
	currentNode := n.FirstChild
	for currentNode != nil {
		switch currentNode.Type {
		case html.TextNode:
			tagLength += utf16RuneCountInString(trimTrailing(currentNode.Data))
		case html.ElementNode:
			tagLength += getTextLength(currentNode)
		}
		currentNode = currentNode.NextSibling
	}
	return tagLength
}

// utf16RuneCountInString returns the number of UTF-16 code units in a string
func utf16RuneCountInString(s string) int32 {
	return int32(len(utf16.Encode([]rune(s))))
}

// parseTagsToEntity converts a list of tags to a list of MessageEntities
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
						userID, _ := strconv.ParseInt(id, 10, 64)
						if userID != 0 {
							entities = append(entities, &MessageEntityMentionName{
								Offset: tag.Offset,
								Length: tag.Length,
								UserID: userID,
							})
						}
					}
				}
			case tag.Attrs["href"] != "" && strings.HasPrefix(tag.Attrs["href"], "tg://emoji"):
				u, err := url.Parse(tag.Attrs["href"])
				if err == nil {
					id := u.Query().Get("id")
					if id != "" {
						emojiID, _ := strconv.ParseInt(id, 10, 64)
						if emojiID != 0 {
							entities = append(entities, &MessageEntityCustomEmoji{
								Offset: tag.Offset,
								Length: tag.Length,
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
			if parsed, err := strconv.ParseBool(tag.Attrs["collapsed"]); err == nil {
				isCollapsed = parsed
			}
			entities = append(entities, &MessageEntityBlockquote{isCollapsed, tag.Offset, tag.Length})
		case "emoji":
			emoijiId, err := strconv.ParseInt(tag.Attrs["id"], 10, 64)
			if err != nil {
				continue
			}
			entities = append(entities, &MessageEntityCustomEmoji{tag.Offset, tag.Length, emoijiId})
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
		content := html.EscapeString(markdown[start+1 : end])
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
		// Extract the language if specified
		codeBlock := markdown[start+3 : end]
		var lang string
		if strings.Contains(codeBlock, "\n") {
			langEnd := strings.Index(codeBlock, "\n")
			lang = codeBlock[:langEnd]
			codeBlock = codeBlock[langEnd+1:]
		}
		content := html.EscapeString(codeBlock)
		if lang != "" {
			markdown = markdown[:start] + "<pre><code class=\"language-" + html.EscapeString(lang) + "\">" + content + "</code></pre>" + markdown[end+3:]
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
		text := html.EscapeString(parts[1])
		url := html.EscapeString(parts[2])
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
		markdown = markdown[:start] + "<emoji id=\"" + markdown[start+2:end] + "\">" + "</emoji>" + markdown[end+2:]
	}
	return markdown
}

func convertBlockquotesSyntax(markdown string) string {
	var result strings.Builder
	lines := strings.Split(markdown, "\n")
	inBlockquote := false
	collapsedBlockquote := false

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if inBlockquote && !strings.HasPrefix(trimmedLine, "> ") && !strings.HasPrefix(trimmedLine, ">> ") {
			result.WriteString("</blockquote>\n")
			inBlockquote = false
			collapsedBlockquote = false
		}

		if strings.HasPrefix(trimmedLine, "> ") {
			if !inBlockquote || collapsedBlockquote {
				if i > 0 {
					result.WriteString("")
				}
				if collapsedBlockquote {
					result.WriteString("</blockquote>\n")
					collapsedBlockquote = false
				}
				result.WriteString("<blockquote>")
				inBlockquote = true
			}
			result.WriteString(strings.TrimPrefix(trimmedLine, "> ") + "\n")
		} else if strings.HasPrefix(trimmedLine, ">> ") {
			if !inBlockquote || !collapsedBlockquote {
				if i > 0 {
					result.WriteString("")
				}
				if inBlockquote && !collapsedBlockquote {
					result.WriteString("</blockquote>\n")
				}
				result.WriteString("<blockquote collapsed=\"true\">")
				inBlockquote = true
				collapsedBlockquote = true
			}
			result.WriteString(strings.TrimPrefix(trimmedLine, ">> ") + "\n")
		} else {
			if inBlockquote {
				result.WriteString("</blockquote>\n")
				inBlockquote = false
				collapsedBlockquote = false
			}
			result.WriteString(line + "\n")
		}
	}

	if inBlockquote {
		result.WriteString("</blockquote>\n")
	}

	return result.String()
}
