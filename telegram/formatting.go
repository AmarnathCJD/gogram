package telegram

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/net/html"
)

func (c *Client) FormatMessage(message string, mode string) ([]MessageEntity, string) {
	return parseEntities(message, mode)
}

// parseEntities parses the message and returns a list of MessageEntities and the cleaned text string
func parseEntities(message string, mode string) ([]MessageEntity, string) {
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
	htmlStr := MarkdownToHTML(text)
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
	case "b", "strong", "i", "em", "u", "s", "a", "code", "pre", "ins", "del", "spoiler":
		return true
	}
	return false
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
		if n.Type == html.ElementNode {
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
		} else if n.Type == html.TextNode {
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
	return cleanedText, tagOffsets, nil
}

// getTextLength returns the length of the text content of a node, including its children
func getTextLength(n *html.Node) int32 {
	var tagLength int32 = 0
	currentNode := n.FirstChild
	for currentNode != nil {
		if currentNode.Type == html.TextNode {
			tagLength += utf16RuneCountInString(currentNode.Data)
		} else if currentNode.Type == html.ElementNode {
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
			case tag.Attrs["href"] != "" && strings.HasPrefix(tag.Attrs["href"], "tg:"):
				userID, err := strconv.ParseInt(strings.TrimPrefix(tag.Attrs["href"], "tg://user?id="), 10, 64)
				if err == nil {
					entities = append(entities, &MessageEntityMentionName{tag.Offset, tag.Length, userID})
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
		}
	}
	return entities
}

// parseEntitiesToHTML converts a list of MessageEntities to HTML, given the original text
func parseEntitiesToHTML(entities []MessageEntity, text string) string {
	var htmlBuf bytes.Buffer
	var openTags []string
	var openTagOffsets []int32
	var openTagLengths []int32

	getOffset := func(e MessageEntity) int32 {
		switch e := e.(type) {
		case *MessageEntityBold:
			return e.Offset
		}
		return 0
	}

	getLength := func(e MessageEntity) int32 {
		switch e := e.(type) {
		case *MessageEntityBold:
			return e.Length
		}
		return 0
	}

	getType := func(e MessageEntity) string {
		switch e.(type) {
		case *MessageEntityBold:
			return "bold"
		}
		return ""
	}

	// Sort the entities by offset
	sort.Slice(entities, func(i, j int) bool {
		return getOffset(entities[i]) < getOffset(entities[j])
	})

	// Iterate through the entities and add the appropriate HTML tags
	for _, entity := range entities {
		// Write the text between the last entity and this one
		htmlBuf.WriteString(text[getOffset(entity) : getOffset(entity)+getLength(entity)])

		// Check if this entity is already open
		for i := range openTags {
			if openTags[i] == getType(entity) {
				// Close the tag
				htmlBuf.WriteString(fmt.Sprintf("</%s>", getType(entity)))
				openTags = append(openTags[:i], openTags[i+1:]...)
				openTagOffsets = append(openTagOffsets[:i], openTagOffsets[i+1:]...)
				openTagLengths = append(openTagLengths[:i], openTagLengths[i+1:]...)
				break
			}
		}

		// Open the tag
		switch getType(entity) {
		case "email":
			htmlBuf.WriteString(fmt.Sprintf("<a href=\"mailto:%s\">", text[getOffset(entity):getOffset(entity)+getLength(entity)]))
		case "mention_name":
			htmlBuf.WriteString(fmt.Sprintf("<a href=\"tg://user?id=%d\">", entity.(*MessageEntityMentionName).UserID))
		case "text_link":
			htmlBuf.WriteString(fmt.Sprintf("<a href=\"%s\">", entity.(*MessageEntityTextURL).URL))
		case "url":
			htmlBuf.WriteString("<a>")
		case "bold":
			htmlBuf.WriteString("<b>")
		case "code":
			htmlBuf.WriteString("<code>")
		case "italic":
			htmlBuf.WriteString("<em>")
		case "pre":
			htmlBuf.WriteString(fmt.Sprintf("<pre><code class=\"language-%s\">", entity.(*MessageEntityPre).Language))
		case "strike":
			htmlBuf.WriteString("<s>")
		case "underline":
			htmlBuf.WriteString("<u>")
		case "mention":
			htmlBuf.WriteString("<mention>")
		case "spoiler":
			htmlBuf.WriteString("<spoiler>")
		}
		openTags = append(openTags, getType(entity))
		openTagOffsets = append(openTagOffsets, getOffset(entity))
		openTagLengths = append(openTagLengths, getLength(entity))
	}

	// Write the text after the last entity
	htmlBuf.WriteString(text[getOffset(entities[len(entities)-1]) : getOffset(entities[len(entities)-1])+getLength(entities[len(entities)-1])])

	// Close any remaining open tags
	for i := len(openTags) - 1; i >= 0; i-- {
		htmlBuf.WriteString(fmt.Sprintf("</%s>", openTags[i]))
	}

	return htmlBuf.String()
}

func MarkdownToHTML(markdown string) string {
	// Convert bold syntax (**text**) to <strong> tags
	boldRe := regexp.MustCompile(`\*\*(.*?)\*\*`)
	markdown = boldRe.ReplaceAllStringFunc(markdown, func(match string) string {
		innerText := boldRe.FindStringSubmatch(match)[1]
		return "<b>" + innerText + "</b>"
	})

	// Convert preformatted syntax (```text```) to <pre> tags
	preRe := regexp.MustCompile("```(.*?)```")
	markdown = preRe.ReplaceAllStringFunc(markdown, func(match string) string {
		innerText := preRe.FindStringSubmatch(match)[1]
		return "<pre>" + html.EscapeString(innerText) + "</pre>"
	})

	// Convert italic syntax (__text__) to <em> tags
	italicRe := regexp.MustCompile(`__(.*?)__`)
	markdown = italicRe.ReplaceAllStringFunc(markdown, func(match string) string {
		innerText := italicRe.FindStringSubmatch(match)[1]
		return "<i>" + innerText + "</i>"
	})

	// Convert strikethrough syntax (~~text~~) to <del> tags
	strikeRe := regexp.MustCompile(`~~(.*?)~~`)
	markdown = strikeRe.ReplaceAllStringFunc(markdown, func(match string) string {
		innerText := strikeRe.FindStringSubmatch(match)[1]
		return "<s>" + innerText + "</s>"
	})

	// Convert inline code syntax (`code`) to <code> tags
	codeRe := regexp.MustCompile("`([^`\n]+)`")
	markdown = codeRe.ReplaceAllStringFunc(markdown, func(match string) string {
		innerText := codeRe.FindStringSubmatch(match)[1]
		return "<code>" + html.EscapeString(innerText) + "</code>"
	})

	// Convert links syntax ([text](url)) to <a> tags
	linkRe := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	markdown = linkRe.ReplaceAllStringFunc(markdown, func(match string) string {
		innerText := linkRe.FindStringSubmatch(match)[1]
		href := linkRe.FindStringSubmatch(match)[2]
		return "<a href=\"" + html.EscapeString(href) + "\">" + innerText + "</a>"
	})

	// Convert spoilers syntax (||text||) to <spoiler> tags
	spoilerRe := regexp.MustCompile(`\|\|([^|]+)\|\|`)
	markdown = spoilerRe.ReplaceAllStringFunc(markdown, func(match string) string {
		innerText := spoilerRe.FindStringSubmatch(match)[1]
		return "<spoiler>" + innerText + "</spoiler>"
	})

	// Convert underline syntax (!!text!!) to <u> tags
	underlineRe := regexp.MustCompile(`!!([^!]+)!!`)
	markdown = underlineRe.ReplaceAllStringFunc(markdown, func(match string) string {
		innerText := underlineRe.FindStringSubmatch(match)[1]
		return "<u>" + innerText + "</u>"
	})

	// Return the resulting HTML
	return string(bytes.TrimSpace([]byte(markdown)))
}
