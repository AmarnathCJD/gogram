// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"fmt"
	"math"
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

func (i *InlineQuery) Answer(results []InputBotInlineResult, options ...*InlineSendOptions) (bool, error) {
	var opts InlineSendOptions
	if len(options) > 0 {
		opts = *options[0]
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
	MimeType             string                             `json:"mime_type,omitempty"`
	ExcludeMedia         bool                               `json:"exclude_media,omitempty"`
	ForceDocument        bool                               `json:"force_document,omitempty"`
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
	VoiceNote            bool                               `json:"voice_note,omitempty"`
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

func (i *InlineBuilder) Photo(photo any, options ...*ArticleOptions) InputBotInlineResult {
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

func (i *InlineBuilder) Document(document any, options ...*ArticleOptions) InputBotInlineResult {
	var opts = getVariadic(options, &ArticleOptions{})
	inputDoc, err := i.Client.getSendableMedia(document, &MediaMetadata{
		Inline:        true,
		ForceDocument: opts.ForceDocument,
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
		Type:        getInlineDocumentType(opts.MimeType, opts.VoiceNote),
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
	return strings.TrimSpace(strings.Join(Messages[1:], " "))
}

func (i *InlineSend) Edit(message any, options ...*SendOptions) (*NewMessage, error) {
	return i.Client.EditMessage(&i.MsgID, 0, message, options...)
}

func (i *InlineSend) ChatID() int64 {
	switch msg := i.MsgID.(type) {
	case *InputBotInlineMessageIDObj:
		return int64(math.Abs(float64(msg.ID >> 32)))
	case *InputBotInlineMessageID64:
		return int64(math.Abs(float64(msg.OwnerID)))
	default:
		return 0
	}
}

func (i *InlineSend) ChannelID() int64 {
	switch msg := i.MsgID.(type) {
	case *InputBotInlineMessageIDObj:
		return -100_000_000_0000 - int64(msg.ID>>32)
	default:
		return 0
	}
}

func (i *InlineSend) AccessHash() int64 {
	switch msg := i.MsgID.(type) {
	case *InputBotInlineMessageIDObj:
		return msg.AccessHash
	case *InputBotInlineMessageID64:
		return msg.AccessHash
	default:
		return 0
	}
}

func (i *InlineSend) MessageID() int32 {
	switch msg := i.MsgID.(type) {
	case *InputBotInlineMessageIDObj:
		return int32(uint32(msg.ID & 0xFFFFFFFF))
	case *InputBotInlineMessageID64:
		return msg.ID
	default:
		return 0
	}
}

func (i *InlineSend) GetPeer() (InputPeer, error) {
	switch msg := i.MsgID.(type) {
	case *InputBotInlineMessageIDObj:
		return &InputPeerChannel{
			ChannelID:  i.ChatID(),
			AccessHash: msg.AccessHash,
		}, nil
	case *InputBotInlineMessageID64:
		return &InputPeerChannel{
			ChannelID:  i.ChatID(),
			AccessHash: msg.AccessHash,
		}, nil
	}

	return nil, fmt.Errorf("unknown message type: %T", i.MsgID)
}

func (i *InlineSend) GetMessage() (*NewMessage, error) {
	peer, err := i.Client.ResolvePeer(i.ChatID())
	if err != nil {
		peer = &InputPeerChannel{
			ChannelID:  i.ChatID(),
			AccessHash: i.AccessHash(),
		}
	}

	messages, err := i.Client.GetMessages(peer, &SearchOption{IDs: &InputMessageID{ID: i.MessageID()}})
	if err != nil {
		return nil, err
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("message not found")
	}

	return &messages[0], nil
}

func (i *InlineSend) GetReplyMessage() (*NewMessage, error) {
	msg, err := i.GetMessage()
	if err != nil {
		return nil, err
	}

	if !msg.IsReply() {
		return nil, fmt.Errorf("message is not a reply")
	}

	return msg.GetReplyMessage()
}

func (i *InlineSend) Marshal(nointent ...bool) string {
	return i.Client.JSON(i.OriginalUpdate, nointent)
}
