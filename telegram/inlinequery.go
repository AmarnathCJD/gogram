package telegram

import (
	"encoding/json"
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
)

func (b *InlineQuery) Answer(results []InputBotInlineResult, options ...InlineSendOptions) (bool, error) {
	var opts InlineSendOptions
	if len(options) > 0 {
		opts = options[0]
	}
	return b.Client.AnswerInlineQuery(b.QueryID, results, &opts)
}

func (b *InlineQuery) Builder() *InlineBuilder {
	return &InlineBuilder{
		Client:        b.Client,
		QueryID:       b.QueryID,
		InlineResults: []InputBotInlineResult{},
	}
}

func (b *InlineBuilder) Results() []InputBotInlineResult {
	return b.InlineResults
}

type ArticleOptions struct {
	ID           string                             `json:"id,omitempty"`
	Title        string                             `json:"title,omitempty"`
	Description  string                             `json:"description,omitempty"`
	ExcludeMedia bool                               `json:"exclude_media,omitempty"`
	Thumb        InputWebDocument                   `json:"thumb,omitempty"`
	Content      InputWebDocument                   `json:"content,omitempty"`
	LinkPreview  bool                               `json:"link_preview,omitempty"`
	ReplyMarkup  ReplyMarkup                        `json:"reply_markup,omitempty"`
	Entities     []MessageEntity                    `json:"entities,omitempty"`
	ParseMode    string                             `json:"parse_mode,omitempty"`
	Caption      string                             `json:"caption,omitempty"`
	Venue        *InputBotInlineMessageMediaVenue   `json:"venue,omitempty"`
	Location     *InputBotInlineMessageMediaGeo     `json:"location,omitempty"`
	Contact      *InputBotInlineMessageMediaContact `json:"contact,omitempty"`
	Invoice      *InputBotInlineMessageMediaInvoice `json:"invoice,omitempty"`
}

func (b *InlineBuilder) Article(title, description, text string, options ...*ArticleOptions) InputBotInlineResult {
	var opts ArticleOptions
	if len(options) > 0 {
		opts = *options[0]
	} else {
		opts = ArticleOptions{}
	}
	e, text := b.Client.FormatMessage(text, getValue(opts.ParseMode, b.Client.ParseMode()).(string))
	result := &InputBotInlineResultObj{
		ID:          getValue(opts.ID, fmt.Sprint(GenerateRandomLong())).(string),
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
	b.InlineResults = append(b.InlineResults, result)
	return result
}

func (b *InlineBuilder) Photo(photo interface{}, options ...*ArticleOptions) InputBotInlineResult {
	var opts ArticleOptions
	if len(options) > 0 {
		opts = *options[0]
	} else {
		opts = ArticleOptions{}
	}
	Photo, _ := b.Client.getSendableMedia(photo, &MediaMetadata{})
	var Image InputPhoto
PhotoTypeSwitch:
	switch p := Photo.(type) {
	case *InputMediaPhoto:
		Image = p.ID
	case *InputMediaUploadedPhoto:
		media, err := b.Client.MessagesUploadMedia(&InputPeerSelf{}, p)
		if err != nil {
			Image = &InputPhotoEmpty{}
		}
		Photo, _ = b.Client.getSendableMedia(media, &MediaMetadata{})
		goto PhotoTypeSwitch
	default:
		b.Client.Logger.Warn("InlineBuilder.Photo: Photo is not a InputMediaPhoto, its a", reflect.TypeOf(Photo).String())
		Image = &InputPhotoEmpty{}
	}
	e, text := b.Client.FormatMessage(opts.Caption, getValue(opts.ParseMode, b.Client.ParseMode()).(string))
	result := &InputBotInlineResultPhoto{
		ID:    getValue(opts.ID, fmt.Sprint(GenerateRandomLong())).(string),
		Type:  "photo",
		Photo: Image,
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
	b.InlineResults = append(b.InlineResults, result)
	return result
}

func (b *InlineBuilder) Document(document interface{}, options ...*ArticleOptions) InputBotInlineResult {
	var opts ArticleOptions
	if len(options) > 0 {
		opts = *options[0]
	} else {
		opts = ArticleOptions{}
	}
	Document, _ := b.Client.getSendableMedia(document, &MediaMetadata{})
	var Doc InputDocument
DocTypeSwitch:
	switch p := Document.(type) {
	case *InputMediaDocument:
		Doc = p.ID
	case *InputMediaUploadedDocument:
		media, err := b.Client.MessagesUploadMedia(&InputPeerSelf{}, p)
		if err != nil {
			Doc = &InputDocumentEmpty{}
		}
		Document, _ = b.Client.getSendableMedia(media, &MediaMetadata{})
		goto DocTypeSwitch
	default:
		b.Client.Logger.Warn("InlineBuilder.Document: Document is not a InputMediaDocument")
		Doc = &InputDocumentEmpty{}
	}
	e, text := b.Client.FormatMessage(opts.Caption, getValue(opts.ParseMode, b.Client.ParseMode()).(string))
	result := &InputBotInlineResultDocument{
		ID:          getValue(opts.ID, fmt.Sprint(GenerateRandomLong())).(string),
		Type:        "document",
		Document:    Doc,
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
	b.InlineResults = append(b.InlineResults, result)
	return result
}

func (b *InlineBuilder) Game(ID, ShortName string, options ...*ArticleOptions) InputBotInlineResult {
	var opts ArticleOptions
	if len(options) > 0 {
		opts = *options[0]
	} else {
		opts = ArticleOptions{}
	}
	e, text := b.Client.FormatMessage(opts.Caption, getValue(opts.ParseMode, b.Client.ParseMode()).(string))
	result := &InputBotInlineResultGame{
		ID:        getValue(opts.ID, fmt.Sprint(GenerateRandomLong())).(string),
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
	b.InlineResults = append(b.InlineResults, result)
	return result
}

func (b *InlineQuery) IsChannel() bool {
	return b.PeerType == InlineQueryPeerTypeBroadcast
}

func (b *InlineQuery) IsGroup() bool {
	return b.PeerType == InlineQueryPeerTypeChat || b.PeerType == InlineQueryPeerTypeMegagroup
}

func (b *InlineQuery) IsPrivate() bool {
	return b.PeerType == InlineQueryPeerTypePm || b.PeerType == InlineQueryPeerTypeSameBotPm
}

func (b *InlineQuery) Marshal() string {
	bytes, _ := json.MarshalIndent(b, "", "  ")
	return string(bytes)
}

func (m *InlineQuery) Args() string {
	Messages := strings.Split(m.Query, " ")
	if len(Messages) < 2 {
		return ""
	}
	return strings.TrimSpace(strings.Join(Messages[1:], " ")) // Args()
}
