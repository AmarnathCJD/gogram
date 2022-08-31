package telegram

import "fmt"

type (
	InlineQuery struct {
		QueryID        int64
		Query          string
		OriginalUpdate *UpdateBotInlineQuery
		Sender         *UserObj
		Offset         string
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

func (b *InlineBuilder) Article(title, description, text string, options ...*ArticleOptions) InputBotInlineResult {
	var opts ArticleOptions
	if len(options) > 0 {
		opts = *options[0]
	} else {
		opts = ArticleOptions{}
	}
	e, text := b.Client.FormatMessage(text, getValue(opts.ParseMode, b.Client.ParseMode).(string))
	result := &InputBotInlineResultObj{
		ID:          fmt.Sprint(GenerateRandomLong()),
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
	Photo, _ := b.Client.getSendableMedia(photo, &CustomAttrs{})
	var Image InputPhoto
	switch p := Photo.(type) {
	case *InputMediaPhoto:
		Image = p.ID
	default:
		b.Client.Logger.Println("InlineBuilder.Photo: Photo is not a InputMediaPhoto")
		return nil
	}
	e, text := b.Client.FormatMessage(opts.Caption, getValue(opts.ParseMode, b.Client.ParseMode).(string))
	result := &InputBotInlineResultPhoto{
		ID:    fmt.Sprint(GenerateRandomLong()),
		Type:  "photo",
		Photo: Image,
		SendMessage: &InputBotInlineMessageText{
			Message:     text,
			Entities:    e,
			ReplyMarkup: opts.ReplyMarkup,
			NoWebpage:   !opts.LinkPreview,
		},
	}
	return result
}

// Document is not implemented yet
//
