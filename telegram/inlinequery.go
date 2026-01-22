// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"fmt"
	"strings"
)

type InlineQuery struct {
	QueryID        int64
	Query          string
	OriginalUpdate *UpdateBotInlineQuery
	Sender         *UserObj
	SenderID       int64
	Offset         string
	PeerType       InlineQueryPeerType
	Client         *Client
}

type InlineBuilder struct {
	Client        *Client
	QueryID       int64
	InlineResults []InputBotInlineResult
	lastResult    InputBotInlineResult
	maxResults    int32
	nextOffset    string
	cacheTime     int32
	isPersonal    bool
	switchPm      string
	switchPmText  string
	err           error
}

type InlineSend struct {
	OriginalUpdate *UpdateBotInlineSend
	Sender         *UserObj
	SenderID       int64
	ID             string
	MsgID          InputBotInlineMessageID
	Client         *Client
}

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

// Error returns the first error encountered during result building
func (i *InlineBuilder) Error() error {
	return i.err
}

// Answer sends the inline query results to Telegram
func (i *InlineBuilder) Answer(options ...*InlineSendOptions) (bool, error) {
	if i.err != nil {
		return false, i.err
	}
	var opts InlineSendOptions
	if len(options) > 0 {
		opts = *options[0]
	} else {
		opts = InlineSendOptions{}
	}

	if opts.CacheTime == 0 && i.cacheTime > 0 {
		opts.CacheTime = i.cacheTime
	}
	if opts.NextOffset == "" && i.nextOffset != "" {
		opts.NextOffset = i.nextOffset
	}
	if !opts.Private && i.isPersonal {
		opts.Private = i.isPersonal
	}
	if opts.SwitchPm == "" && i.switchPm != "" {
		opts.SwitchPm = i.switchPm
	}
	if opts.SwitchPmText == "" && i.switchPmText != "" {
		opts.SwitchPmText = i.switchPmText
	}

	return i.Client.AnswerInlineQuery(i.QueryID, i.InlineResults, &opts)
}

// MaxResults limits the number of results (Note: Telegram has a 50 result limit)
func (i *InlineBuilder) MaxResults(max int32) *InlineBuilder {
	i.maxResults = max
	if max > 0 && len(i.InlineResults) > int(max) {
		i.InlineResults = i.InlineResults[:max]
	}
	return i
}

// NextOffset sets the offset for pagination
func (i *InlineBuilder) NextOffset(offset string) *InlineBuilder {
	i.nextOffset = offset
	return i
}

// CacheTime sets how long Telegram should cache the results (in seconds)
func (i *InlineBuilder) CacheTime(seconds int32) *InlineBuilder {
	i.cacheTime = seconds
	return i
}

// IsPersonal marks results as personal (not cached for all users)
func (i *InlineBuilder) IsPersonal(personal bool) *InlineBuilder {
	i.isPersonal = personal
	return i
}

// SwitchPM adds a "Switch to PM" button
func (i *InlineBuilder) SwitchPM(text, startParam string) *InlineBuilder {
	i.switchPm = text
	i.switchPmText = startParam
	return i
}

func (i *InlineBuilder) getSendMessage() any {
	if i.lastResult == nil {
		return nil
	}
	switch r := i.lastResult.(type) {
	case *InputBotInlineResultObj:
		return r.SendMessage
	case *InputBotInlineResultPhoto:
		return r.SendMessage
	case *InputBotInlineResultDocument:
		return r.SendMessage
	case *InputBotInlineResultGame:
		return r.SendMessage
	default:
		return nil
	}
}

func (i *InlineBuilder) setMessageField(setter func(any)) *InlineBuilder {
	if msg := i.getSendMessage(); msg != nil {
		setter(msg)
	}
	return i
}

// WithID sets a custom ID for the result
func (i *InlineBuilder) WithID(id string) *InlineBuilder {
	if i.lastResult == nil {
		return i
	}
	switch r := i.lastResult.(type) {
	case *InputBotInlineResultObj:
		r.ID = id
	case *InputBotInlineResultPhoto:
		r.ID = id
	case *InputBotInlineResultDocument:
		r.ID = id
	case *InputBotInlineResultGame:
		r.ID = id
	}
	return i
}

// WithReplyMarkup sets reply markup for the result
func (i *InlineBuilder) WithReplyMarkup(markup ReplyMarkup) *InlineBuilder {
	return i.setMessageField(func(msg any) {
		switch m := msg.(type) {
		case *InputBotInlineMessageText:
			m.ReplyMarkup = markup
		case *InputBotInlineMessageMediaAuto:
			m.ReplyMarkup = markup
		case *InputBotInlineMessageGame:
			m.ReplyMarkup = markup
		case *InputBotInlineMessageMediaGeo:
			m.ReplyMarkup = markup
		case *InputBotInlineMessageMediaVenue:
			m.ReplyMarkup = markup
		case *InputBotInlineMessageMediaContact:
			m.ReplyMarkup = markup
		case *InputBotInlineMessageMediaInvoice:
			m.ReplyMarkup = markup
		case *InputBotInlineMessageMediaWebPage:
			m.ReplyMarkup = markup
		}
	})
}

// WithLinkPreview enables/disables link preview for the result
func (i *InlineBuilder) WithLinkPreview(enable bool) *InlineBuilder {
	return i.setMessageField(func(msg any) {
		switch m := msg.(type) {
		case *InputBotInlineMessageText:
			m.NoWebpage = !enable
		case *InputBotInlineMessageMediaWebPage:
			m.ForceLargeMedia = enable
		}
	})
}

// WithThumb sets a thumbnail for the result
func (i *InlineBuilder) WithThumb(thumb InputWebDocument) *InlineBuilder {
	if i.lastResult == nil {
		return i
	}
	if r, ok := i.lastResult.(*InputBotInlineResultObj); ok {
		r.Thumb = &thumb
	}
	return i
}

// WithThumbURL sets a thumbnail URL for the result
func (i *InlineBuilder) WithThumbURL(url string) *InlineBuilder {
	return i.WithThumb(InputWebDocument{URL: url})
}

// WithContent sets content for the result
func (i *InlineBuilder) WithContent(content InputWebDocument) *InlineBuilder {
	if i.lastResult == nil {
		return i
	}
	if r, ok := i.lastResult.(*InputBotInlineResultObj); ok {
		r.Content = &content
	}
	return i
}

// WithContentURL sets content URL for the result
func (i *InlineBuilder) WithContentURL(url string) *InlineBuilder {
	return i.WithContent(InputWebDocument{URL: url})
}

// WithDescription sets description for the result
func (i *InlineBuilder) WithDescription(desc string) *InlineBuilder {
	if i.lastResult == nil {
		return i
	}
	switch r := i.lastResult.(type) {
	case *InputBotInlineResultObj:
		r.Description = desc
	case *InputBotInlineResultDocument:
		r.Description = desc
	}
	return i
}

// WithInvertMedia inverts media position for the result
func (i *InlineBuilder) WithInvertMedia(invert bool) *InlineBuilder {
	return i.setMessageField(func(msg any) {
		switch m := msg.(type) {
		case *InputBotInlineMessageText:
			m.InvertMedia = invert
		case *InputBotInlineMessageMediaWebPage:
			m.InvertMedia = invert
		}
	})
}

func selectMessageType(opts *ArticleOptions, defaultMsg InputBotInlineMessage) InputBotInlineMessage {
	switch {
	case opts.Venue != nil:
		return opts.Venue
	case opts.Location != nil:
		return opts.Location
	case opts.Contact != nil:
		return opts.Contact
	case opts.Invoice != nil:
		return opts.Invoice
	case opts.WebPage != nil:
		return opts.WebPage
	default:
		return defaultMsg
	}
}

type ArticleOptions struct {
	ID                   string                             // Unique result identifier
	Title                string                             // Result title
	Description          string                             // Short description of the result
	MimeType             string                             // MIME type for content
	ExcludeMedia         bool                               // Separate media from message text
	ForceDocument        bool                               // Force result as document
	Thumb                InputWebDocument                   // Thumbnail for the result
	Content              InputWebDocument                   // Content URL and attributes
	LinkPreview          bool                               // Enable link preview in message
	ReplyMarkup          ReplyMarkup                        // Inline keyboard for the result
	Entities             []MessageEntity                    // Text formatting entities
	ParseMode            string                             // Parse mode: "HTML" or "Markdown"
	Caption              string                             // Caption for media results
	InvertMedia          bool                               // Show media below text
	Venue                *InputBotInlineMessageMediaVenue   // Venue information
	Location             *InputBotInlineMessageMediaGeo     // Location information
	Contact              *InputBotInlineMessageMediaContact // Contact information
	Invoice              *InputBotInlineMessageMediaInvoice // Invoice for payments
	WebPage              *InputBotInlineMessageMediaWebPage // Web page preview
	BusinessConnectionId string                             // Business connection ID
	VoiceNote            bool                               // Send as voice note
}

func (i *InlineBuilder) Article(title, description, text string, options ...*ArticleOptions) *InlineBuilder {
	opts := getVariadic(options, &ArticleOptions{})

	entities, text := parseEntities(text, getValue(opts.ParseMode, i.Client.ParseMode()))
	defaultMsg := &InputBotInlineMessageText{
		Message:     text,
		Entities:    entities,
		ReplyMarkup: opts.ReplyMarkup,
		NoWebpage:   !opts.LinkPreview,
		InvertMedia: opts.InvertMedia,
	}

	result := &InputBotInlineResultObj{
		ID:          getValue(opts.ID, fmt.Sprint(GenerateRandomLong())),
		Type:        "article",
		Title:       title,
		Description: description,
		SendMessage: selectMessageType(opts, defaultMsg),
	}

	if opts.Thumb.URL != "" {
		result.Thumb = &opts.Thumb
	}
	if opts.Content.URL != "" {
		result.Content = &opts.Content
	}

	i.InlineResults = append(i.InlineResults, result)
	i.lastResult = result
	return i
}

func (i *InlineBuilder) Photo(photo any, options ...*ArticleOptions) *InlineBuilder {
	if i.err != nil {
		return i
	}

	opts := getVariadic(options, &ArticleOptions{})

	inputPhoto, err := i.Client.getSendableMedia(photo, &MediaMetadata{Inline: true})
	if err != nil {
		i.err = fmt.Errorf("inline photo: %w", err)
		return i
	}

	im, ok := inputPhoto.(*InputMediaPhoto)
	if !ok {
		i.err = fmt.Errorf("inline photo: expected InputMediaPhoto, got %T", inputPhoto)
		return i
	}

	entities, text := parseEntities(opts.Caption, getValue(opts.ParseMode, i.Client.ParseMode()))
	var defaultMsg InputBotInlineMessage = &InputBotInlineMessageMediaAuto{
		Message:     text,
		Entities:    entities,
		ReplyMarkup: opts.ReplyMarkup,
	}

	if opts.ExcludeMedia {
		defaultMsg = &InputBotInlineMessageText{
			Message:     text,
			Entities:    entities,
			ReplyMarkup: opts.ReplyMarkup,
			NoWebpage:   !opts.LinkPreview,
		}
	}

	result := &InputBotInlineResultPhoto{
		ID:          getValue(opts.ID, fmt.Sprint(GenerateRandomLong())),
		Type:        "photo",
		Photo:       im.ID,
		SendMessage: selectMessageType(opts, defaultMsg),
	}

	i.InlineResults = append(i.InlineResults, result)
	i.lastResult = result
	return i
}

func (i *InlineBuilder) Document(document any, options ...*ArticleOptions) *InlineBuilder {
	if i.err != nil {
		return i
	}

	opts := getVariadic(options, &ArticleOptions{})

	inputDoc, err := i.Client.getSendableMedia(document, &MediaMetadata{
		Inline:        true,
		ForceDocument: opts.ForceDocument,
	})
	if err != nil {
		i.err = fmt.Errorf("inline document: %w", err)
		return i
	}

	dc, ok := inputDoc.(*InputMediaDocument)
	if !ok {
		i.err = fmt.Errorf("inline document: expected InputMediaDocument, got %T", inputDoc)
		return i
	}

	entities, text := parseEntities(opts.Caption, getValue(opts.ParseMode, i.Client.ParseMode()))
	var defaultMsg InputBotInlineMessage = &InputBotInlineMessageMediaAuto{
		Message:     text,
		Entities:    entities,
		ReplyMarkup: opts.ReplyMarkup,
	}

	if opts.ExcludeMedia {
		defaultMsg = &InputBotInlineMessageText{
			Message:     text,
			Entities:    entities,
			ReplyMarkup: opts.ReplyMarkup,
			NoWebpage:   !opts.LinkPreview,
		}
	}

	result := &InputBotInlineResultDocument{
		ID:          getValue(opts.ID, fmt.Sprint(GenerateRandomLong())),
		Type:        MimeTypes.GetInlineType(opts.MimeType, opts.VoiceNote),
		Document:    dc.ID,
		Title:       opts.Title,
		Description: opts.Description,
		SendMessage: selectMessageType(opts, defaultMsg),
	}

	i.InlineResults = append(i.InlineResults, result)
	i.lastResult = result
	return i
}

func (i *InlineBuilder) Game(ID, ShortName string, options ...*ArticleOptions) *InlineBuilder {
	opts := getVariadic(options, &ArticleOptions{})

	e, text := parseEntities(opts.Caption, getValue(opts.ParseMode, i.Client.ParseMode()))
	var defaultMsg InputBotInlineMessage = &InputBotInlineMessageMediaAuto{
		Message:     text,
		Entities:    e,
		ReplyMarkup: opts.ReplyMarkup,
	}

	if opts.ExcludeMedia {
		defaultMsg = &InputBotInlineMessageText{
			Message:     text,
			Entities:    e,
			ReplyMarkup: opts.ReplyMarkup,
			NoWebpage:   !opts.LinkPreview,
		}
	}

	result := &InputBotInlineResultGame{
		ID:          getValue(opts.ID, fmt.Sprint(GenerateRandomLong())),
		ShortName:   ShortName,
		SendMessage: selectMessageType(opts, defaultMsg),
	}

	i.InlineResults = append(i.InlineResults, result)
	i.lastResult = result
	return i
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

func (i *InlineQuery) Marshal(noindent ...bool) string {
	return MarshalWithTypeName(i.OriginalUpdate, noindent...)
}

func (m *InlineQuery) Args() string {
	Messages := strings.Split(m.Query, " ")
	if len(Messages) < 2 {
		return ""
	}
	return strings.TrimSpace(strings.Join(Messages[1:], " "))
}

func (m *InlineQuery) ArgsList() []string {
	Messages := strings.Split(m.Query, " ")
	if len(Messages) < 2 {
		return []string{}
	}
	return Messages[1:]
}

func (i *InlineSend) Edit(message any, options ...*SendOptions) (*NewMessage, error) {
	return i.Client.EditMessage(&i.MsgID, 0, message, options...)
}

func (i *InlineSend) ChatID() int64 {
	switch msg := i.MsgID.(type) {
	case *InputBotInlineMessageIDObj:
		return int64(uint64(msg.ID) >> 32)
	case *InputBotInlineMessageID64:
		if msg.OwnerID < 0 {
			return -msg.OwnerID
		}
		return msg.OwnerID
	default:
		return 0
	}
}

func (i *InlineSend) ChannelID() int64 {
	switch msg := i.MsgID.(type) {
	case *InputBotInlineMessageIDObj:
		part := int64(uint64(msg.ID) >> 32)
		return -1000000000000 - part
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
		return int32(uint32(uint64(msg.ID) & 0xFFFFFFFF))
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

func (i *InlineSend) Marshal(noindent ...bool) string {
	return MarshalWithTypeName(i.OriginalUpdate, noindent...)
}
