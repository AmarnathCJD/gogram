// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"fmt"
	"reflect"
	"strings"
)

type (
	InlineQuery struct {
		QueryID        int64
		Query          string
		OriginalUpdate *UpdateBotInlineQuery
		Sender         *UserObj
		SenderID       int64
		Offset         string
		PeerType       InlineQueryPeerType
		Client         *Client
	}

	InlineBuilder struct {
		Client        *Client
		QueryID       int64
		InlineResults []InputBotInlineResult
	}

	InlineSend struct {
		OriginalUpdate *UpdateBotInlineSend
		Sender         *UserObj
		SenderID       int64
		ID             string
		MsgID          InputBotInlineMessageID
		Client         *Client
	}
)

func (i *InlineQuery) Answer(results []InputBotInlineResult, options ...InlineSendOptions) (bool, error) {
	var opts InlineSendOptions
	if len(options) > 0 {
		opts = options[0]
	}
	return i.Client.AnswerInlineQuery(i.QueryID, results, &opts)
}

func (i *InlineQuery) Builder() *InlineBuilder {
	return &InlineBuilder{
		Client:        i.Client,
		QueryID:       i.QueryID,
		InlineResults: []InputBotInlineResult{},
	}
}

func (i *InlineBuilder) Results() []InputBotInlineResult {
	return i.InlineResults
}

type ArticleOptions struct {
	ID                   string                             `json:"id,omitempty"`
	Title                string                             `json:"title,omitempty"`
	Description          string                             `json:"description,omitempty"`
	ExcludeMedia         bool                               `json:"exclude_media,omitempty"`
	Thumb                InputWebDocument                   `json:"thumb,omitempty"`
	Content              InputWebDocument                   `json:"content,omitempty"`
	LinkPreview          bool                               `json:"link_preview,omitempty"`
	ReplyMarkup          ReplyMarkup                        `json:"reply_markup,omitempty"`
	Entities             []MessageEntity                    `json:"entities,omitempty"`
	ParseMode            string                             `json:"parse_mode,omitempty"`
	Caption              string                             `json:"caption,omitempty"`
	Venue                *InputBotInlineMessageMediaVenue   `json:"venue,omitempty"`
	Location             *InputBotInlineMessageMediaGeo     `json:"location,omitempty"`
	Contact              *InputBotInlineMessageMediaContact `json:"contact,omitempty"`
	Invoice              *InputBotInlineMessageMediaInvoice `json:"invoice,omitempty"`
	BusinessConnectionId string                             `json:"business_connection_id,omitempty"`
}

func (i *InlineBuilder) Article(title, description, text string, options ...*ArticleOptions) InputBotInlineResult {
	var opts ArticleOptions
	if len(options) > 0 {
		opts = *options[0]
	} else {
		opts = ArticleOptions{}
	}
	e, text := i.Client.FormatMessage(text, getValue(opts.ParseMode, i.Client.ParseMode()))
	result := &InputBotInlineResultObj{
		ID:          getValue(opts.ID, fmt.Sprint(GenerateRandomLong())),
		Type:        "article",
		Title:       title,
		Description: description,
		URL:         "",
		SendMessage: &InputBotInlineMessageText{
			Message:     text,
			Entities:    e,
			ReplyMarkup: opts.ReplyMarkup,
			NoWebpage:   !opts.LinkPreview,
		},
	}
	if opts.Venue != nil {
		result.SendMessage = opts.Venue
	} else if opts.Location != nil {
		result.SendMessage = opts.Location
	} else if opts.Contact != nil {
		result.SendMessage = opts.Contact
	} else if opts.Invoice != nil {
		result.SendMessage = opts.Invoice
	}
	if opts.Thumb.URL != "" {
		result.Thumb = &opts.Thumb
	}
	if opts.Content.URL != "" {
		result.Content = &opts.Content
	}
	i.InlineResults = append(i.InlineResults, result)
	return result
}

func (i *InlineBuilder) Photo(photo interface{}, options ...*ArticleOptions) InputBotInlineResult {
	var opts = getVariadic(options, &ArticleOptions{})
	inputPhoto, err := i.Client.getSendableMedia(photo, &MediaMetadata{
		Inline: true,
	})
	if err != nil {
		i.Client.Logger.Debug("InlineBuilder.Photo: Error getting sendable media:", err)
		return nil
	}

	var image InputPhoto
	if im, ok := inputPhoto.(*InputMediaPhoto); !ok {
		i.Client.Logger.Error("InlineBuilder.Photo: Photo is not a InputMediaPhoto")
		return nil
	} else {
		image = im.ID
	}

	e, text := parseEntities(opts.Caption, getValue(opts.ParseMode, i.Client.ParseMode()))

	result := &InputBotInlineResultPhoto{
		ID:    getValue(opts.ID, fmt.Sprint(GenerateRandomLong())),
		Type:  "photo",
		Photo: image,
		SendMessage: &InputBotInlineMessageMediaAuto{
			Message:     text,
			Entities:    e,
			ReplyMarkup: opts.ReplyMarkup,
		},
	}

	if opts.ExcludeMedia {
		result.SendMessage = &InputBotInlineMessageText{
			Message:     text,
			Entities:    e,
			ReplyMarkup: opts.ReplyMarkup,
			NoWebpage:   !opts.LinkPreview,
		}
	}
	if opts.Venue != nil {
		result.SendMessage = opts.Venue
	} else if opts.Location != nil {
		result.SendMessage = opts.Location
	} else if opts.Contact != nil {
		result.SendMessage = opts.Contact
	} else if opts.Invoice != nil {
		result.SendMessage = opts.Invoice
	}

	i.InlineResults = append(i.InlineResults, result)
	return result
}

func (i *InlineBuilder) Document(document interface{}, options ...*ArticleOptions) InputBotInlineResult {
	var opts = getVariadic(options, &ArticleOptions{})
	inputDoc, err := i.Client.getSendableMedia(document, &MediaMetadata{
		Inline: true,
	})
	if err != nil {
		i.Client.Logger.Debug("InlineBuilder.Document: Error getting sendable media:", err)
		return nil
	}

	var doc InputDocument
	if dc, ok := inputDoc.(*InputMediaDocument); !ok {
		i.Client.Logger.Warn("inlineBuilder.Document: (skip) Document is not a InputMediaDocument but a", reflect.TypeOf(inputDoc))
		return nil
	} else {
		doc = dc.ID
	}

	e, text := parseEntities(opts.Caption, getValue(opts.ParseMode, i.Client.ParseMode()))

	result := &InputBotInlineResultDocument{
		ID:          getValue(opts.ID, fmt.Sprint(GenerateRandomLong())),
		Type:        "document",
		Document:    doc,
		Title:       opts.Title,
		Description: opts.Description,
		SendMessage: &InputBotInlineMessageMediaAuto{
			Message:     text,
			Entities:    e,
			ReplyMarkup: opts.ReplyMarkup,
		},
	}

	if opts.ExcludeMedia {
		result.SendMessage = &InputBotInlineMessageText{
			Message:     text,
			Entities:    e,
			ReplyMarkup: opts.ReplyMarkup,
			NoWebpage:   !opts.LinkPreview,
		}
	}
	if opts.Venue != nil {
		result.SendMessage = opts.Venue
	} else if opts.Location != nil {
		result.SendMessage = opts.Location
	} else if opts.Contact != nil {
		result.SendMessage = opts.Contact
	} else if opts.Invoice != nil {
		result.SendMessage = opts.Invoice
	}

	i.InlineResults = append(i.InlineResults, result)
	return result
}

func (i *InlineBuilder) Game(ID, ShortName string, options ...*ArticleOptions) InputBotInlineResult {
	var opts ArticleOptions
	if len(options) > 0 {
		opts = *options[0]
	} else {
		opts = ArticleOptions{}
	}
	e, text := parseEntities(opts.Caption, getValue(opts.ParseMode, i.Client.ParseMode()))
	result := &InputBotInlineResultGame{
		ID:        getValue(opts.ID, fmt.Sprint(GenerateRandomLong())),
		ShortName: ShortName,
		SendMessage: &InputBotInlineMessageMediaAuto{
			Message:     text,
			Entities:    e,
			ReplyMarkup: opts.ReplyMarkup,
		},
	}
	if opts.ExcludeMedia {
		result.SendMessage = &InputBotInlineMessageText{
			Message:     text,
			Entities:    e,
			ReplyMarkup: opts.ReplyMarkup,
			NoWebpage:   !opts.LinkPreview,
		}
	}
	if opts.Venue != nil {
		result.SendMessage = opts.Venue
	} else if opts.Location != nil {
		result.SendMessage = opts.Location
	} else if opts.Contact != nil {
		result.SendMessage = opts.Contact
	} else if opts.Invoice != nil {
		result.SendMessage = opts.Invoice
	}
	i.InlineResults = append(i.InlineResults, result)
	return result
}

func (i *InlineQuery) IsChannel() bool {
	return i.PeerType == InlineQueryPeerTypeBroadcast
}

func (i *InlineQuery) IsGroup() bool {
	return i.PeerType == InlineQueryPeerTypeChat || i.PeerType == InlineQueryPeerTypeMegagroup
}

func (i *InlineQuery) IsPrivate() bool {
	return i.PeerType == InlineQueryPeerTypePm || i.PeerType == InlineQueryPeerTypeSameBotPm
}

func (i *InlineQuery) Marshal(nointent ...bool) string {
	return i.Client.JSON(i.OriginalUpdate, nointent)
}

func (m *InlineQuery) Args() string {
	Messages := strings.Split(m.Query, " ")
	if len(Messages) < 2 {
		return ""
	}
	return strings.TrimSpace(strings.Join(Messages[1:], " ")) // Args()
}

func (i *InlineSend) Edit(message any, options ...SendOptions) (*NewMessage, error) {
	var opts SendOptions
	if len(options) > 0 {
		opts = options[0]
	}
	return i.Client.EditMessage(i.MsgID, 0, message, &opts)
}

// TODO: Complete Implementation of InlineSend
