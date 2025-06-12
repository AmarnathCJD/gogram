package telegram

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"time"

	"github.com/amarnathcjd/gogram"
	"github.com/pkg/errors"
)

type SendOptions struct {
	Attributes      []DocumentAttribute // attributes of the file
	MimeType        string              // mime type of the file
	Caption         any                 // caption for the media (takes array of strings or a NewMessage)
	ClearDraft      bool                // to clear the draft after sending
	Entities        []MessageEntity     // message formatting entities
	FileName        string              // file name to be used
	ForceDocument   bool                // to force the file to be sent as a document
	InvertMedia     bool                // show media below the caption
	LinkPreview     bool                // to enable link preview
	Media           any                 // media to be sent (e.g., photo, video, document)
	NoForwards      bool                // to disable forwarding (restrict saving)
	ParseMode       string              // parse mode for the caption (markdown or html)
	ReplyID         int32               // reply to message ID
	TopicID         int32               // topic ID for the message to be sent
	ReplyMarkup     ReplyMarkup         // keyboard to send with the message
	ScheduleDate    int32               // schedule date for the message
	SendAs          any                 // to send the message as a different peer
	Silent          bool                // to send the message silently
	Thumb           any                 // thumbnail of the file
	TTL             int32               // time to live for the file (in seconds)
	Spoiler         bool                // to send the file as a spoiler message
	ProgressManager *ProgressManager    // progress manager for uploading
	UploadThreads   int                 // number of worker threads to use for uploading
	Effect          int64               // effect ID for the media (e.g., animations)
	Timeouts        int32               // timeouts for conversations (m.Ask())
}

// SendMessage sends a message to a specified peer using the Telegram API method messages.sendMessage.
//
// Parameters:
//   - peerID: ID of the peer to send the message to.
//   - message: The message to be sent. It can be a string, a media object, or a NewMessage.
//   - opts: Optional parameters that can be used to customize the message sending process.
//
// Returns:
//   - A pointer to a NewMessage object containing information about the sent message.
//   - An error if the message sending fails.
//
// Note: If the message parameter is a NewMessage or a pointer to a NewMessage, the function will extract the message text and entities from it.
// If the message parameter is a media object, the function will send the media as a separate message and return a pointer to a NewMessage object containing information about the sent media.
// If the message parameter is a string, the function will parse it for entities and send it as a text message.
func (c *Client) SendMessage(peerID, message any, opts ...*SendOptions) (*NewMessage, error) {
	opt := getVariadic(opts, &SendOptions{})
	opt.ParseMode = getValue(opt.ParseMode, c.ParseMode())
	var (
		entities    []MessageEntity
		textMessage string
		rawText     string
		media       any
	)
	switch message := message.(type) {
	case string:
		entities, textMessage = parseEntities(message, opt.ParseMode)
		rawText = message
	case MessageMedia, InputMedia, InputFile:
		media = message
	case NewMessage:
		entities = message.Message.Entities
		textMessage = message.MessageText()
		rawText = message.MessageText()
		media = message.Media()
		opt.ReplyMarkup = getValue(opt.ReplyMarkup, *message.ReplyMarkup())
	case *NewMessage:
		entities = message.Message.Entities
		textMessage = message.MessageText()
		rawText = message.MessageText()
		media = message.Media()
		opt.ReplyMarkup = getValue(opt.ReplyMarkup, *message.ReplyMarkup())
	default:
		return nil, fmt.Errorf("invalid message type: %s", reflect.TypeOf(message))
	}
	if opt.Entities != nil {
		entities = opt.Entities
	}
	media = getValue(media, opt.Media)
	if media != nil {
		opt.Caption = getValueAny(opt.Caption, rawText)
		if opt.Entities == nil {
			opt.Entities = entities
		}
		return c.SendMedia(peerID, media, convertOption(opt))
	}
	senderPeer, err := c.ResolvePeer(peerID)
	if err != nil {
		return nil, err
	}
	var sendAs InputPeer
	if opt.SendAs != nil {
		sendAs, err = c.ResolvePeer(opt.SendAs)
		if err != nil {
			return nil, err
		}
	}
	return c.sendMessage(senderPeer, textMessage, entities, sendAs, opt)
}

func (c *Client) sendMessage(Peer InputPeer, Message string, entities []MessageEntity, sendAs InputPeer, opt *SendOptions) (*NewMessage, error) {
	var replyTo *InputReplyToMessage = &InputReplyToMessage{ReplyToMsgID: opt.ReplyID}
	if opt.ReplyID != 0 {
		if opt.TopicID != 0 && opt.TopicID != opt.ReplyID && opt.TopicID != 1 {
			replyTo.TopMsgID = opt.TopicID
		}
	} else {
		if opt.TopicID != 0 && opt.TopicID != 1 {
			replyTo.TopMsgID = opt.TopicID
		}
	}

	updateResp, err := c.MessagesSendMessage(&MessagesSendMessageParams{
		NoWebpage:              !opt.LinkPreview,
		Silent:                 opt.Silent,
		Background:             false,
		ClearDraft:             opt.ClearDraft,
		Noforwards:             opt.NoForwards,
		UpdateStickersetsOrder: false,
		InvertMedia:            opt.InvertMedia,
		Peer:                   Peer,
		ReplyTo:                replyTo,
		Message:                Message,
		RandomID:               GenRandInt(),
		ReplyMarkup:            opt.ReplyMarkup,
		Entities:               entities,
		ScheduleDate:           opt.ScheduleDate,
		SendAs:                 sendAs,
		Effect:                 opt.Effect,
	})
	if err != nil {
		return nil, err
	}
	if updateResp != nil {
		processed := c.processUpdate(updateResp)
		processed.PeerID = c.getPeer(Peer)
		return packMessage(c, processed), nil
	}

	return nil, errors.New("no response for sendMessage")
}

// EditMessage edits a message. This method is a wrapper for messages.editMessage.
//
// Parameters:
//   - peerID: ID of the peer the message was sent to.
//   - id: ID of the message to be edited.
//   - message: New text of the message.
//   - opts: Optional parameters.
//
// Returns:
//   - NewMessage: Returns a NewMessage object containing the edited message on success.
//   - error: Returns an error on failure.
func (c *Client) EditMessage(peerID any, id int32, message any, opts ...*SendOptions) (*NewMessage, error) {
	opt := getVariadic(opts, &SendOptions{})
	opt.ParseMode = getValue(opt.ParseMode, c.ParseMode())
	var (
		entities    []MessageEntity
		textMessage string
		media       any
	)
	switch message := message.(type) {
	case string:
		entities, textMessage = parseEntities(message, opt.ParseMode)
	case MessageMedia, InputMedia, InputFile:
		media = message
	case *NewMessage:
		entities = message.Message.Entities
		textMessage = message.MessageText()
		media = message.Media()
	default:
		return nil, fmt.Errorf("invalid message type: %s", reflect.TypeOf(message))
	}
	if opt.Entities != nil {
		entities = opt.Entities
	}
	media = getValue(media, opt.Media)
	switch p := peerID.(type) {
	case *InputBotInlineMessageID:
		return c.editBotInlineMessage(*p, textMessage, entities, media, opt)
	}
	senderPeer, err := c.ResolvePeer(peerID)
	if err != nil {
		return nil, err
	}
	return c.editMessage(senderPeer, id, textMessage, entities, media, opt)
}

func (c *Client) editMessage(Peer InputPeer, id int32, Message string, entities []MessageEntity, Media any, options *SendOptions) (*NewMessage, error) {
	var (
		media InputMedia
		err   error
	)
	if Media != nil {
		media, err = c.getSendableMedia(Media, &MediaMetadata{
			FileName:        options.FileName,
			Thumb:           options.Thumb,
			Attributes:      options.Attributes,
			ForceDocument:   options.ForceDocument,
			TTL:             options.TTL,
			Spoiler:         options.Spoiler,
			DisableThumb:    false,
			MimeType:        options.MimeType,
			ProgressManager: options.ProgressManager,
			UploadThreads:   options.UploadThreads,
		})
		if err != nil {
			return nil, err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := c.MakeRequestCtx(ctx, &MessagesEditMessageParams{
		Peer:         Peer,
		ID:           id,
		Message:      Message,
		NoWebpage:    !options.LinkPreview,
		InvertMedia:  options.InvertMedia,
		ReplyMarkup:  options.ReplyMarkup,
		Entities:     entities,
		Media:        media,
		ScheduleDate: options.ScheduleDate,
	})
	if err != nil {
		return nil, err
	}
	if result != nil {
		processed := c.processUpdate(result.(Updates))
		processed.PeerID = c.getPeer(Peer)
		return packMessage(c, processed), nil
	}

	return packMessage(c, &MessageObj{
		ID:     id,
		PeerID: c.getPeer(Peer),
		FromID: &PeerUser{UserID: c.Me().ID},
	}), nil
}

func (c *Client) editBotInlineMessage(ID InputBotInlineMessageID, Message string, entities []MessageEntity, Media any, options *SendOptions) (*NewMessage, error) {
	var (
		media InputMedia
		err   error
	)
	if Media != nil {
		media, err = c.getSendableMedia(Media, &MediaMetadata{
			Attributes:      options.Attributes,
			TTL:             options.TTL,
			ForceDocument:   options.ForceDocument,
			Thumb:           options.Thumb,
			FileName:        options.FileName,
			Spoiler:         options.Spoiler,
			MimeType:        options.MimeType,
			ProgressManager: options.ProgressManager,
			Inline:          true,
		})
		if err != nil {
			return nil, err
		}
	}

	editRequest := &MessagesEditInlineBotMessageParams{
		ID:          ID,
		Message:     Message,
		NoWebpage:   !options.LinkPreview,
		InvertMedia: options.InvertMedia,
		ReplyMarkup: options.ReplyMarkup,
		Entities:    entities,
		Media:       media,
	}

	var (
		dcID int32
	)
	switch id := ID.(type) {
	case *InputBotInlineMessageID64:
		dcID = id.DcID
	case *InputBotInlineMessageIDObj:
		dcID = id.DcID
	}

	var sender *gogram.MTProto = c.MTProto
	if dcID != int32(c.GetDC()) {
		found := false
		for dcId, workers := range c.exSenders.senders {
			if int32(dcId) == int32(dcID) {
				for _, worker := range workers {
					sender = worker.MTProto
					found = true
				}
			}
		}

		if !found {
			senderNew, err := c.CreateExportedSender(int(dcID), false)
			if err != nil {
				return nil, err
			}

			c.exSenders.senders[int(dcID)] = append(c.exSenders.senders[int(dcID)], &ExSender{senderNew, time.Now()})
			sender = senderNew
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	editTrueAny, err := sender.MakeRequestCtx(ctx, editRequest)
	if err != nil {
		return nil, err
	}

	if editTrue, ok := editTrueAny.(bool); ok && editTrue {
		return &NewMessage{ID: 0, Message: &MessageObj{
			ID:          0,
			Message:     Message,
			ReplyMarkup: options.ReplyMarkup,
			Entities:    entities,
		}}, nil
	}

	return nil, errors.New("no response for editBotInlineMessage")
}

type MediaOptions struct {
	Attributes       []DocumentAttribute // attributes of the file
	MimeType         string              // mime type of the file
	Caption          any                 // caption for the media (takes array of strings or a NewMessage)
	ClearDraft       bool                // to clear the draft after sending
	Entities         []MessageEntity     // message formatting entities
	FileName         string              // file name to be used
	ForceDocument    bool                // to force the file to be sent as a document
	InvertMedia      bool                // show media below the caption
	LinkPreview      bool                // to enable link preview
	NoForwards       bool                // to disable forwarding (restrict saving)
	NoSoundVideo     bool                // to send the video without sound
	ParseMode        string              // parse mode for the caption (markdown or html)
	ReplyID          int32               //	reply to message ID
	TopicID          int32               // topic ID for the message to be sent
	ReplyMarkup      ReplyMarkup         // keyboard to send with the message
	ScheduleDate     int32               // schedule date for the message
	SendAs           any                 // to send the message as a different peer
	Silent           bool                // to send the message silently
	Thumb            any                 // thumbnail of the file
	TTL              int32               // time to live for the file (in seconds)
	Spoiler          bool                // to send the file as a spoiler message
	ProgressManager  *ProgressManager    // progress manager for uploading
	UploadThreads    int                 // number of worker threads to use for uploading
	SkipHash         bool                // to skip reusing duplicate files
	SleepThresholdMs int32               // sleep threshold in milliseconds (in-between chunked operations)
}

type MediaMetadata struct {
	FileName             string              // file name to be used.
	BusinessConnectionId string              // business connection id
	Thumb                any                 // thumbnail of the file
	Attributes           []DocumentAttribute // attributes of the file
	ForceDocument        bool                // to force the file to be sent as a document
	TTL                  int32               // time to live for the file
	Spoiler              bool                // to send the file as a spoiler message
	DisableThumb         bool                // disable thumbnail generation
	MimeType             string              // mime type of the file
	ProgressManager      *ProgressManager    // progress manager for uploading
	UploadThreads        int                 // number of worker threads to use for uploading
	FileAbsPath          string              // absolute path to the file
	Inline               bool                // to force calling media.uploadMedia (for inline and albums)
	SkipHash             bool                // to skip reusing duplicate files
}

// SendMedia sends a media message.
// This method is a wrapper for messages.sendMedia.
//
// Params:
//   - peerID: ID of the peer to send the message to.
//   - Media: Media to send.
//   - opts: Optional parameters.
//
// Returns:
//   - A pointer to a NewMessage object and an error if the message sending fails.
//   - If the message is sent successfully, the returned NewMessage object will contain information about the sent message.
//
// Note:
//   - If the caption in opts is a string, it will be parsed for entities based on the parse_mode in opts.
//   - If the caption in opts is a pointer to a NewMessage, its entities will be used instead.
//   - If the entities field in opts is not nil, it will override any entities parsed from the caption.
//   - If send_as in opts is not nil, the message will be sent from the specified peer, otherwise it will be sent from the sender peer.
func (c *Client) SendMedia(peerID, Media any, opts ...*MediaOptions) (*NewMessage, error) {
	opt := getVariadic(opts, &MediaOptions{})
	opt.ParseMode = getValue(opt.ParseMode, c.ParseMode())

	var (
		entities    []MessageEntity
		textMessage string
	)

	sendMedia, err := c.getSendableMedia(Media, &MediaMetadata{
		FileName:        opt.FileName,
		Thumb:           opt.Thumb,
		ForceDocument:   opt.ForceDocument,
		Attributes:      opt.Attributes,
		TTL:             opt.TTL,
		Spoiler:         opt.Spoiler,
		MimeType:        opt.MimeType,
		ProgressManager: opt.ProgressManager,
		UploadThreads:   opt.UploadThreads,
		SkipHash:        opt.SkipHash,
	})

	if err != nil {
		return nil, err
	}
	switch caption := opt.Caption.(type) {
	case string:
		entities, textMessage = parseEntities(caption, opt.ParseMode)
	case *NewMessage:
		entities = caption.Message.Entities
		textMessage = caption.MessageText()
	}
	if opt.Entities != nil {
		entities = opt.Entities
	}
	senderPeer, err := c.ResolvePeer(peerID)
	if err != nil {
		return nil, err
	}
	var sendAs InputPeer
	if opt.SendAs != nil {
		sendAs, err = c.ResolvePeer(opt.SendAs)
		if err != nil {
			return nil, err
		}
	}
	return c.sendMedia(senderPeer, sendMedia, textMessage, entities, sendAs, opt)
}

func (c *Client) sendMedia(Peer InputPeer, Media InputMedia, Caption string, entities []MessageEntity, sendAs InputPeer, opt *MediaOptions) (*NewMessage, error) {
	var replyTo *InputReplyToMessage = &InputReplyToMessage{ReplyToMsgID: opt.ReplyID}
	if opt.ReplyID != 0 {
		if opt.TopicID != 0 && opt.TopicID != opt.ReplyID && opt.TopicID != 1 {
			replyTo.TopMsgID = opt.TopicID
		}
	} else {
		if opt.TopicID != 0 && opt.TopicID != 1 {
			replyTo.TopMsgID = opt.TopicID
		}
	}

	result, err := c.MessagesSendMedia(&MessagesSendMediaParams{
		Silent:                 opt.Silent,
		Background:             false,
		ClearDraft:             opt.ClearDraft,
		Noforwards:             opt.NoForwards,
		UpdateStickersetsOrder: false,
		InvertMedia:            opt.InvertMedia,
		Peer:                   Peer,
		ReplyTo:                replyTo,
		Media:                  Media,
		RandomID:               GenRandInt(),
		ReplyMarkup:            opt.ReplyMarkup,
		Message:                Caption,
		Entities:               entities,
		ScheduleDate:           opt.ScheduleDate,
		SendAs:                 sendAs,
	})
	if err != nil {
		return nil, err
	}
	if result != nil {
		processed := c.processUpdate(result)
		processed.PeerID = c.getPeer(Peer)
		return packMessage(c, processed), nil
	}

	return nil, errors.New("no response for sendMedia")
}

// SendAlbum sends a media album.
// This method is a wrapper for messages.sendMultiMedia.
//
// Params:
//   - peerID: ID of the peer to send the message to.
//   - Album: List of media to send.
//   - opts: Optional parameters.
//
// Returns:
//   - A slice of pointers to NewMessage objects and an error if the message sending fails.
//   - If the messages are sent successfully, the returned NewMessage objects will contain information about the sent messages.
//
// Note:
//   - If the caption in opts is a string, it will be parsed for entities based on the parse_mode in opts.
//   - If the caption in opts is a pointer to a NewMessage, its entities will be used instead.
//   - If the entities field in opts is not nil, it will override any entities parsed from the caption.
//   - If send_as in opts is not nil, the messages will be sent from the specified peer, otherwise they will be sent from the sender peer.
func (c *Client) SendAlbum(peerID, Album any, opts ...*MediaOptions) ([]*NewMessage, error) {
	opt := getVariadic(opts, &MediaOptions{})
	opt.ParseMode = getValue(opt.ParseMode, c.ParseMode())

	if opt.SleepThresholdMs == 0 {
		opt.SleepThresholdMs = 5000
	}

	InputAlbum, multiErr := c.getMultiMedia(Album, &MediaMetadata{FileName: opt.FileName, Thumb: opt.Thumb, ForceDocument: opt.ForceDocument, Attributes: opt.Attributes, TTL: opt.TTL, Spoiler: opt.Spoiler, MimeType: opt.MimeType, ProgressManager: opt.ProgressManager, UploadThreads: opt.UploadThreads, SkipHash: opt.SkipHash})
	if multiErr != nil {
		return nil, multiErr
	}

	switch caption := opt.Caption.(type) {
	case string, *NewMessage:
		var (
			entities    []MessageEntity
			textMessage string
		)

		switch cap := caption.(type) {
		case string:
			entities, textMessage = parseEntities(cap, opt.ParseMode)
		case *NewMessage:
			entities = cap.Message.Entities
			textMessage = cap.MessageText()
		}

		if opt.Entities != nil {
			entities = opt.Entities
		}

		if len(InputAlbum) > 0 {
			InputAlbum[len(InputAlbum)-1].Message = textMessage
			InputAlbum[len(InputAlbum)-1].Entities = entities
		}

	case []string, []*NewMessage:
		if len(InputAlbum) > 0 {
			switch cap := caption.(type) {
			case []string:
				for i, cap := range cap {
					entities, textMessage := parseEntities(cap, opt.ParseMode)
					InputAlbum[i].Message = textMessage
					InputAlbum[i].Entities = entities
				}
			case []*NewMessage:
				for i, cap := range cap {
					InputAlbum[i].Message = cap.MessageText()
					InputAlbum[i].Entities = cap.Message.Entities
				}
			}
		}
	}

	senderPeer, err := c.ResolvePeer(peerID)
	if err != nil {
		return nil, err
	}
	var sendAs InputPeer
	if opt.SendAs != nil {
		sendAs, err = c.ResolvePeer(sendAs)
		if err != nil {
			return nil, err
		}
	}
	return c.sendAlbum(senderPeer, InputAlbum, sendAs, opt)
}

func (c *Client) sendAlbum(Peer InputPeer, Album []*InputSingleMedia, sendAs InputPeer, opt *MediaOptions) ([]*NewMessage, error) {
	var replyTo *InputReplyToMessage = &InputReplyToMessage{ReplyToMsgID: opt.ReplyID}
	if opt.ReplyID != 0 {
		if opt.TopicID != 0 && opt.TopicID != opt.ReplyID && opt.TopicID != 1 {
			replyTo.TopMsgID = opt.TopicID
		}
	} else {
		if opt.TopicID != 0 && opt.TopicID != 1 {
			replyTo.TopMsgID = opt.TopicID
		}
	}

	req := &MessagesSendMultiMediaParams{
		Silent:                 opt.Silent,
		Background:             false,
		ClearDraft:             opt.ClearDraft,
		Noforwards:             opt.NoForwards,
		UpdateStickersetsOrder: false,
		Peer:                   Peer,
		ReplyTo:                replyTo,
		ScheduleDate:           opt.ScheduleDate,
		SendAs:                 sendAs,
	}

	// split into chunks of 10
	var chunk []*InputSingleMedia
	var results []*NewMessage
	for i := 0; i < len(Album); i += 10 {
		end := i + 10
		if end > len(Album) {
			end = len(Album)
		}
		chunk = Album[i:end]

		req.MultiMedia = chunk
		result, err := c.MessagesSendMultiMedia(req)
		if err != nil {
			return nil, err
		}

		if result != nil {
			updates := processUpdates(result)
			for _, update := range updates {
				update.(*MessageObj).PeerID = c.getPeer(Peer)
			}

			results = append(results, PackMessages(c, updates)...)
		}

		time.Sleep(time.Duration(opt.SleepThresholdMs) * time.Millisecond)
	}

	return results, nil
}

type PollOptions struct {
	PublicVoters   bool   // Allow public voters
	MCQ            bool   // Multiple choice question type poll
	IsQuiz         bool   // Quiz type poll
	ClosePeriod    int32  // Close the poll after this period
	CloseDate      int32  // Close the poll at this date
	Solution       string // Solution for the poll
	CorrectAnswers []int  // Correct answers for the poll
	ReplyID        int32  // Reply to message ID
	TopicID        int32  // Topic ID for the message to be sent
	NoForwards     bool   // Disable forwarding
	ScheduleDate   int32  // Schedule date for the message
}

func (c *Client) SendPoll(peerID any, question string, options []string, opts ...*PollOptions) (*NewMessage, error) {
	opt := getVariadic(opts, &PollOptions{})
	senderPeer, err := c.ResolvePeer(peerID)
	if err != nil {
		return nil, err
	}
	return c.sendPoll(senderPeer, question, options, opt)
}

func (c *Client) sendPoll(Peer InputPeer, question string, options []string, opt *PollOptions) (*NewMessage, error) {
	questionEntities, actualQuestion := parseEntities(question, c.ParseMode())
	var actualOptions []*TextWithEntities
	for _, option := range options {
		entities, text := parseEntities(option, c.ParseMode())
		actualOptions = append(actualOptions, &TextWithEntities{
			Text:     text,
			Entities: entities,
		})
	}

	var answsers []*PollAnswer
	for i, option := range actualOptions {
		answsers = append(answsers, &PollAnswer{
			Text:   option,
			Option: []byte{byte(i)},
		})
	}

	correctAnswers := [][]byte{}
	if len(opt.CorrectAnswers) > 0 {
		for _, answer := range opt.CorrectAnswers {
			correctAnswers = append(correctAnswers, []byte{byte(answer)})
		}
	}

	var solnEntities []MessageEntity
	if opt.Solution != "" {
		solnEntities, opt.Solution = parseEntities(opt.Solution, c.ParseMode())
	}

	poll := &InputMediaPoll{
		Poll: &Poll{
			ID:             GenRandInt(),
			Closed:         false,
			PublicVoters:   opt.PublicVoters,
			MultipleChoice: opt.MCQ,
			Quiz:           opt.IsQuiz,
			Question: &TextWithEntities{
				Text:     actualQuestion,
				Entities: questionEntities,
			},
			Answers:     answsers,
			ClosePeriod: opt.ClosePeriod,
			CloseDate:   opt.CloseDate,
		},
		CorrectAnswers:   correctAnswers,
		Solution:         opt.Solution,
		SolutionEntities: solnEntities,
	}

	var replyTo *InputReplyToMessage = &InputReplyToMessage{ReplyToMsgID: opt.ReplyID}
	if opt.ReplyID != 0 {
		if opt.TopicID != 0 && opt.TopicID != opt.ReplyID && opt.TopicID != 1 {
			replyTo.TopMsgID = opt.TopicID
		}
	} else {
		if opt.TopicID != 0 && opt.TopicID != 1 {
			replyTo.TopMsgID = opt.TopicID
		}
	}

	updateResp, err := c.MessagesSendMedia(&MessagesSendMediaParams{
		ClearDraft:   false,
		Noforwards:   opt.NoForwards,
		Peer:         Peer,
		ReplyTo:      replyTo,
		Media:        poll,
		RandomID:     GenRandInt(),
		ScheduleDate: opt.ScheduleDate,
	})

	if err != nil {
		return nil, err
	}

	if updateResp != nil {
		processed := c.processUpdate(updateResp)
		processed.PeerID = c.getPeer(Peer)
		return packMessage(c, processed), nil
	}

	return nil, errors.New("no response for sendPoll")
}

// SendReaction sends a reaction to a message, which can be an emoji or a custom emoji.
func (c *Client) SendReaction(peerID any, msgID int32, reaction any, big ...bool) error {
	b := getVariadic(big, false)
	peer, err := c.ResolvePeer(peerID)
	if err != nil {
		return err
	}

	r, err := convertReaction(reaction)
	if err != nil {
		return err
	}

	_, err = c.MessagesSendReaction(&MessagesSendReactionParams{
		Peer:        peer,
		Big:         b,
		AddToRecent: false,
		MsgID:       msgID,
		Reaction:    r,
	})
	return err
}

func convertReaction(reaction any) ([]Reaction, error) {
	var r []Reaction
	switch v := reaction.(type) {
	case string:
		r = append(r, createReactionFromString(v))
	case []string:
		for _, s := range v {
			r = append(r, createReactionFromString(s))
		}
	case ReactionCustomEmoji:
		r = append(r, &v)
	case []ReactionCustomEmoji:
		for _, ce := range v {
			r = append(r, &ce)
		}
	case []any:
		for _, i := range v {
			switch iv := i.(type) {
			case string:
				r = append(r, createReactionFromString(iv))
			case ReactionCustomEmoji:
				r = append(r, &iv)
			default:
				return nil, errors.New("invalid reaction type in array")
			}
		}
	default:
		return nil, errors.New("invalid reaction type")
	}
	return r, nil
}

func createReactionFromString(s string) Reaction {
	if s == "" {
		return &ReactionEmpty{}
	}
	return &ReactionEmoji{s}
}

// SendDice sends a special dice message.
// This method calls messages.sendMedia with a dice media.
func (c *Client) SendDice(peerID any, emoji string) (*NewMessage, error) {
	return c.SendMedia(peerID, &InputMediaDice{Emoticon: emoji})
}

type ActionResult struct {
	Peer   InputPeer `json:"peer,omitempty"`
	Client *Client   `json:"client,omitempty"`
}

// Cancel the pointed Action,
// Returns true if the action was cancelled
func (a *ActionResult) Cancel() bool {
	if a.Peer == nil || a.Client == nil {
		return false // Avoid nil pointer dereference
	}
	b, err := a.Client.MessagesSetTyping(a.Peer, 0, &SendMessageCancelAction{})
	if err != nil {
		return false
	}
	return b
}

// SendAction sends a chat action.
// This method is a wrapper for messages.setTyping.
func (c *Client) SendAction(PeerID, Action any, topMsgID ...int32) (*ActionResult, error) {
	peerChat, err := c.ResolvePeer(PeerID)
	if err != nil {
		return nil, err
	}
	TopMsgID := getVariadic(topMsgID, int32(0))
	switch a := Action.(type) {
	case string:
		if action, ok := Actions[a]; ok {
			_, err = c.MessagesSetTyping(peerChat, TopMsgID, action)
		} else {
			return nil, errors.New("unknown action")
		}
	case *SendMessageAction:
		_, err = c.MessagesSetTyping(peerChat, TopMsgID, *a)
	default:
		return nil, errors.New("unknown action type")
	}
	return &ActionResult{Peer: peerChat, Client: c}, err
}

// SendReadAck sends a read acknowledgement.
// This method is a wrapper for messages.readHistory.
func (c *Client) SendReadAck(PeerID any, MaxID ...int32) (*MessagesAffectedMessages, error) {
	peerChat, err := c.ResolvePeer(PeerID)
	if err != nil {
		return nil, err
	}
	maxID := getVariadic(MaxID, int32(0))
	switch peer := peerChat.(type) {
	case *InputPeerChannel:
		done, err := c.ChannelsReadHistory(&InputChannelObj{
			ChannelID:  peer.ChannelID,
			AccessHash: peer.AccessHash,
		}, maxID)
		if err != nil {
			return nil, err
		} else if !done {
			return nil, errors.New("failed to read history")
		}

		return &MessagesAffectedMessages{Pts: 0, PtsCount: 0}, nil
	case *InputPeerChat, *InputPeerUser:
		return c.MessagesReadHistory(peerChat, maxID)
	default:
		return nil, errors.New("invalid peer type")
	}
}

// SendPoll sends a poll. TODO

type ForwardOptions struct {
	HideCaption  bool                 // whether to drop the original caption
	HideAuthor   bool                 // whether to drop the original author
	Silent       bool                 // whether to send the message silently
	Noforwards   bool                 // whether to disable forwarding
	Background   bool                 // send the message in the background
	WithMyScore  bool                 // whether to include the user's game score
	SendAs       any                  // send the message as a different peer
	ScheduleDate int32                // schedule date for the message
	ReplyTo      *InputReplyToMessage // reply to message
}

// Forward forwards a message.
// This method is a wrapper for messages.forwardMessages.
func (c *Client) Forward(peerID, fromPeerID any, msgIDs []int32, opts ...*ForwardOptions) ([]NewMessage, error) {
	opt := getVariadic(opts, &ForwardOptions{
		ReplyTo: &InputReplyToMessage{},
	})
	toPeer, err := c.ResolvePeer(peerID)
	if err != nil {
		return nil, err
	}
	fromPeer, err := c.ResolvePeer(fromPeerID)
	if err != nil {
		return nil, err
	}
	randomIDs := make([]int64, len(msgIDs))
	for i := range randomIDs {
		randomIDs[i] = rand.Int63()
	}
	var sendAs InputPeer
	if opt.SendAs != nil {
		sendAs, err = c.ResolvePeer(opt.SendAs)
		if err != nil {
			return nil, err
		}
	}
	updateResp, err := c.MessagesForwardMessages(&MessagesForwardMessagesParams{
		//ReplyTo:           opt.ReplyTo,
		ToPeer:            toPeer,
		FromPeer:          fromPeer,
		ID:                msgIDs,
		RandomID:          randomIDs,
		Silent:            opt.Silent,
		Background:        false,
		Noforwards:        opt.Noforwards,
		ScheduleDate:      opt.ScheduleDate,
		DropAuthor:        opt.HideAuthor,
		DropMediaCaptions: opt.HideCaption,
		SendAs:            sendAs,
	})
	if err != nil {
		return nil, err
	}
	var m []NewMessage
	if updateResp != nil {
		updates := processUpdates(updateResp)
		for _, update := range updates {
			m = append(m, *packMessage(c, update))
		}
	}
	return m, nil
}

// DeleteMessages deletes messages.
// This method is a wrapper for messages.deleteMessages.
func (c *Client) DeleteMessages(peerID any, msgIDs []int32, noRevoke ...bool) (*MessagesAffectedMessages, error) {
	shouldRevoke := getVariadic(noRevoke, false)
	peer, err := c.ResolvePeer(peerID)
	if err != nil {
		return nil, err
	}
	switch peer := peer.(type) {
	case *InputPeerChannel:
		return c.ChannelsDeleteMessages(&InputChannelObj{
			ChannelID:  peer.ChannelID,
			AccessHash: peer.AccessHash,
		}, msgIDs)
	case *InputPeerChat, *InputPeerUser:
		return c.MessagesDeleteMessages(!shouldRevoke, msgIDs)
	default:
		return nil, errors.New("invalid peer type")
	}
}

// GetCustomEmoji gets the document of a custom emoji
//
//	Params:
//	 - docIDs: the document id of the emoji
func (c *Client) GetCustomEmoji(docIDs ...int64) ([]Document, error) {
	var em []int64
	em = append(em, docIDs...)
	emojis, err := c.MessagesGetCustomEmojiDocuments(em)
	if err != nil {
		return nil, err
	}
	return emojis, nil
}

type SearchOption struct {
	IDs              any            // IDs of the messages to get (bots can use)
	Query            string         // query to search for
	FromUser         any            // ID of the user to search from
	AddOffset        int32          // Sequential number of the first message to be returned
	Offset           int32          // offset of the message to search from
	Limit            int32          // limit of the messages to get
	Filter           MessagesFilter // filter to use
	TopMsgID         int32          // ID of the top message
	MaxID            int32          // maximum ID of the message
	MinID            int32          // minimum ID of the message
	MaxDate          int32          // maximum date of the message
	MinDate          int32          // minimum date of the message
	SleepThresholdMs int32          // sleep threshold in milliseconds (in-between chunked operations)
}

func (c *Client) GetMessages(PeerID any, Opts ...*SearchOption) ([]NewMessage, error) {
	opt := getVariadic(Opts, &SearchOption{
		Filter:           &InputMessagesFilterEmpty{},
		SleepThresholdMs: 20,
	})
	peer, err := c.ResolvePeer(PeerID)
	if err != nil {
		return nil, err
	}

	var (
		messages []NewMessage
		inputIDs []InputMessage
		result   MessagesMessages
	)

	switch i := opt.IDs.(type) {
	case []int32, []int64, []int:
		var ids []int32
		switch i := i.(type) {
		case []int32:
			ids = convertSlice[int32](i)
		case []int64:
			ids = convertSlice[int32](i)
		case []int:
			ids = convertSlice[int32](i)
		}
		for _, id := range ids {
			inputIDs = append(inputIDs, &InputMessageID{ID: id})
		}
	case int, int64, int32:
		inputIDs = append(inputIDs, &InputMessageID{ID: parseInt32(i)})
	case *InputMessage:
		inputIDs = append(inputIDs, *i)
	case *InputMessagePinned:
		inputIDs = append(inputIDs, &InputMessagePinned{})
	case *InputMessageID:
		inputIDs = append(inputIDs, &InputMessageID{ID: i.ID})
	case *InputMessageReplyTo:
		inputIDs = append(inputIDs, &InputMessageReplyTo{ID: i.ID})
	case *InputMessageCallbackQuery:
		inputIDs = append(inputIDs, &InputMessageCallbackQuery{ID: i.ID})
	}

	if len(inputIDs) == 0 && opt.Query == "" && opt.Limit == 0 {
		opt.Limit = 1
	}

	if len(inputIDs) > 0 {
		var chunkedIds = splitIDsIntoChunks(inputIDs, 100)
		for _, ids := range chunkedIds {
			switch peer := peer.(type) {
			case *InputPeerChannel:
				result, err = c.ChannelsGetMessages(&InputChannelObj{ChannelID: peer.ChannelID, AccessHash: peer.AccessHash}, ids)
			case *InputPeerChat, *InputPeerUser, *InputPeerSelf:
				result, err = c.MessagesGetMessages(ids)
			default:
				return nil, errors.New("invalid peer type to get messages")
			}
			if err != nil {
				return messages, err
			}
			switch result := result.(type) {
			case *MessagesChannelMessages:
				c.Cache.UpdatePeersToCache(result.Users, result.Chats)
				for _, msg := range result.Messages {
					messages = append(messages, *packMessage(c, msg))
				}
			case *MessagesMessagesObj:
				c.Cache.UpdatePeersToCache(result.Users, result.Chats)
				for _, msg := range result.Messages {
					messages = append(messages, *packMessage(c, msg))
				}
			}
			if len(messages) >= int(opt.Limit) && opt.Limit != 0 {
				return messages[:opt.Limit], nil
			}
			time.Sleep(time.Duration(opt.SleepThresholdMs) * time.Millisecond)
		}

		return messages, nil
	} else {
		if opt.Filter == nil {
			opt.Filter = &InputMessagesFilterEmpty{}
		}

		params := &MessagesSearchParams{
			Peer:      peer,
			Q:         opt.Query,
			OffsetID:  opt.Offset,
			AddOffset: opt.AddOffset,
			Filter:    opt.Filter,
			MinDate:   opt.MinDate,
			MaxDate:   opt.MaxDate,
			MinID:     opt.MinID,
			MaxID:     opt.MaxID,
			Limit:     opt.Limit,
			TopMsgID:  opt.TopMsgID,
		}

		if opt.FromUser != nil {
			fromUser, err := c.ResolvePeer(opt.FromUser)
			if err != nil {
				return nil, err
			}
			params.FromID = fromUser
		}

		for {
			remaining := int(opt.Limit) - len(messages)
			if remaining <= 0 {
				break
			}

			perReqLimit := min(int32(remaining), 100)
			params.Limit = perReqLimit

			result, err = c.MessagesSearch(params)
			if err != nil {
				if handleIfFlood(err, c) {
					continue
				}
				return nil, err
			}

			var fetchedMessages []NewMessage
			switch result := result.(type) {
			case *MessagesChannelMessages:
				c.Cache.UpdatePeersToCache(result.Users, result.Chats)
				for _, msg := range result.Messages {
					fetchedMessages = append(fetchedMessages, *packMessage(c, msg))
				}
			case *MessagesMessagesObj:
				c.Cache.UpdatePeersToCache(result.Users, result.Chats)
				for _, msg := range result.Messages {
					fetchedMessages = append(fetchedMessages, *packMessage(c, msg))
				}
			case *MessagesMessagesSlice:
				c.Cache.UpdatePeersToCache(result.Users, result.Chats)
				for _, msg := range result.Messages {
					fetchedMessages = append(fetchedMessages, *packMessage(c, msg))
				}
			}

			if len(fetchedMessages) == 0 {
				break
			}

			messages = append(messages, fetchedMessages...)
			if len(messages) >= int(opt.Limit) && opt.Limit != 0 {
				messages = messages[:opt.Limit]
				break
			}

			params.OffsetID = fetchedMessages[len(fetchedMessages)-1].ID
			params.MaxDate = fetchedMessages[len(fetchedMessages)-1].Date()

			time.Sleep(time.Duration(opt.SleepThresholdMs) * time.Millisecond)
		}
	}

	return messages, nil
}

func (c *Client) IterMessages(PeerID any, Opts ...*SearchOption) (<-chan NewMessage, <-chan error) {
	ch := make(chan NewMessage)
	errCh := make(chan error)

	go func() {
		defer close(ch)
		defer close(errCh)

		opt := getVariadic(Opts, &SearchOption{
			Filter:           &InputMessagesFilterEmpty{},
			SleepThresholdMs: 20,
		})

		peer, err := c.ResolvePeer(PeerID)
		if err != nil {
			errCh <- err
			return
		}

		var (
			messages []NewMessage
			inputIDs []InputMessage
			result   MessagesMessages
		)

		switch i := opt.IDs.(type) {
		case []int32, []int64, []int:
			var ids []int32
			switch i := i.(type) {
			case []int32:
				ids = convertSlice[int32](i)
			case []int64:
				ids = convertSlice[int32](i)
			case []int:
				ids = convertSlice[int32](i)
			}
			for _, id := range ids {
				inputIDs = append(inputIDs, &InputMessageID{ID: id})
			}
		case int, int64, int32:
			inputIDs = append(inputIDs, &InputMessageID{ID: parseInt32(i)})
		case *InputMessage:
			inputIDs = append(inputIDs, *i)
		case *InputMessagePinned:
			inputIDs = append(inputIDs, &InputMessagePinned{})
		case *InputMessageID:
			inputIDs = append(inputIDs, &InputMessageID{ID: i.ID})
		case *InputMessageReplyTo:
			inputIDs = append(inputIDs, &InputMessageReplyTo{ID: i.ID})
		case *InputMessageCallbackQuery:
			inputIDs = append(inputIDs, &InputMessageCallbackQuery{ID: i.ID})
		}

		if len(inputIDs) == 0 && opt.Query == "" && opt.Limit == 0 {
			opt.Limit = 1
		}

		if len(inputIDs) > 0 {
			var chunkedIds = splitIDsIntoChunks(inputIDs, 100)
			for _, ids := range chunkedIds {
				switch peer := peer.(type) {
				case *InputPeerChannel:
					result, err = c.ChannelsGetMessages(&InputChannelObj{ChannelID: peer.ChannelID, AccessHash: peer.AccessHash}, ids)
				case *InputPeerChat, *InputPeerUser, *InputPeerSelf:
					result, err = c.MessagesGetMessages(ids)
				default:
					errCh <- errors.New("invalid peer type to get messages")
					return
				}
				if err != nil {
					errCh <- err
					return
				}
				switch result := result.(type) {
				case *MessagesChannelMessages:
					c.Cache.UpdatePeersToCache(result.Users, result.Chats)
					for _, msg := range result.Messages {
						ch <- *packMessage(c, msg)
					}
				case *MessagesMessagesObj:
					c.Cache.UpdatePeersToCache(result.Users, result.Chats)
					for _, msg := range result.Messages {
						ch <- *packMessage(c, msg)
					}
				case *MessagesMessagesSlice:
					c.Cache.UpdatePeersToCache(result.Users, result.Chats)
					for _, msg := range result.Messages {
						ch <- *packMessage(c, msg)
					}
				}

				time.Sleep(time.Duration(opt.SleepThresholdMs) * time.Millisecond)
			}
		} else {
			if opt.Filter == nil {
				opt.Filter = &InputMessagesFilterEmpty{}
			}

			params := &MessagesSearchParams{
				Peer:     peer,
				Q:        opt.Query,
				OffsetID: opt.Offset,
				Filter:   opt.Filter,
				MinDate:  opt.MinDate,
				MaxDate:  opt.MaxDate,
				MinID:    opt.MinID,
				MaxID:    opt.MaxID,
				Limit:    opt.Limit,
				TopMsgID: opt.TopMsgID,
			}

			if opt.FromUser != nil {
				fromUser, err := c.ResolvePeer(opt.FromUser)
				if err != nil {
					errCh <- err
					return
				}
				params.FromID = fromUser
			}

			for {
				remaining := opt.Limit - int32(len(messages))
				perReqLimit := min(remaining, int32(100))
				params.Limit = perReqLimit

				result, err = c.MessagesSearch(params)
				if err != nil {
					if handleIfFlood(err, c) {
						continue
					}
					errCh <- err
					return
				}
				switch result := result.(type) {
				case *MessagesChannelMessages:
					if result.Count == 0 {
						return
					}

					c.Cache.UpdatePeersToCache(result.Users, result.Chats)
					for _, msg := range result.Messages {
						messages = append(messages, *packMessage(c, msg))
					}
				case *MessagesMessagesObj:
					if len(result.Messages) == 0 {
						return
					}

					c.Cache.UpdatePeersToCache(result.Users, result.Chats)
					for _, msg := range result.Messages {
						messages = append(messages, *packMessage(c, msg))
					}
				case *MessagesMessagesSlice:
					if result.Count == 0 {
						return
					}

					c.Cache.UpdatePeersToCache(result.Users, result.Chats)
					for _, msg := range result.Messages {
						messages = append(messages, *packMessage(c, msg))
					}
				}

				for _, msg := range messages {
					ch <- msg
				}

				if (len(messages) >= int(opt.Limit) || len(messages) == 0) && opt.Limit > 0 {
					break
				}

				params.OffsetID = messages[len(messages)-1].ID
				params.MaxDate = messages[len(messages)-1].Date()

				time.Sleep(time.Duration(opt.SleepThresholdMs) * time.Millisecond)
			}
		}
	}()

	return ch, errCh
}

func (c *Client) GetMessageByID(PeerID any, MsgID int32) (*NewMessage, error) {
	resp, err := c.GetMessages(PeerID, &SearchOption{
		IDs: MsgID,
	})
	if err != nil {
		return nil, err
	}
	if len(resp) == 0 {
		return nil, errors.New("no messages found")
	}
	return &resp[0], nil
}

type HistoryOption struct {
	Limit            int32 // limit of the messages to get
	Offset           int32 // offset of the message to search from
	OffsetDate       int32 // offset date of the message to search from
	MaxID            int32 // maximum ID of the message
	MinID            int32 // minimum ID of the message
	SleepThresholdMs int32 // sleep threshold in milliseconds (in-between chunked operations)
}

func (c *Client) GetHistory(PeerID any, opts ...*HistoryOption) ([]NewMessage, error) {
	peerToAct, err := c.ResolvePeer(PeerID)
	if err != nil {
		return nil, err
	}

	var opt = getVariadic(opts, &HistoryOption{
		Limit:            1,
		SleepThresholdMs: 20,
	})

	var messages []NewMessage
	var fetched int

	req := &MessagesGetHistoryParams{
		Peer:       peerToAct,
		OffsetID:   opt.Offset,
		OffsetDate: 0,
		MaxID:      opt.MaxID,
		MinID:      opt.MinID,
	}

	for {
		remaining := opt.Limit - int32(fetched)
		perReqLimit := int32(100)
		if remaining < perReqLimit {
			perReqLimit = remaining
		}
		req.Limit = perReqLimit

		resp, err := c.MessagesGetHistory(req)
		if err != nil {
			if handleIfFlood(err, c) {
				continue
			}
			return nil, err
		}

		switch resp := resp.(type) {
		case *MessagesMessagesObj:
			c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
			for _, msg := range resp.Messages {
				messages = append(messages, *packMessage(c, msg))
			}
			fetched += len(resp.Messages)
			if len(resp.Messages) < int(perReqLimit) || fetched >= int(opt.Limit) && opt.Limit > 0 {
				return messages, nil
			}

			req.OffsetID = messages[len(messages)-1].ID
			req.OffsetDate = messages[len(messages)-1].Date()
		case *MessagesMessagesSlice:
			c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
			for _, msg := range resp.Messages {
				messages = append(messages, *packMessage(c, msg))
			}
			fetched += len(resp.Messages)
			if len(resp.Messages) < int(perReqLimit) || fetched >= int(opt.Limit) && opt.Limit > 0 {
				return messages, nil
			}

			req.OffsetID = messages[len(messages)-1].ID
			req.OffsetDate = messages[len(messages)-1].Date()
		case *MessagesChannelMessages:
			c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
			for _, msg := range resp.Messages {
				messages = append(messages, *packMessage(c, msg))
			}
			fetched += len(resp.Messages)
			if len(resp.Messages) < int(perReqLimit) || fetched >= int(opt.Limit) && opt.Limit > 0 {
				return messages, nil
			}

			req.OffsetID = messages[len(messages)-1].ID
			req.OffsetDate = messages[len(messages)-1].Date()
		default:
			return nil, errors.New("unexpected response: " + reflect.TypeOf(resp).String())
		}

		time.Sleep(time.Duration(opt.SleepThresholdMs) * time.Millisecond)
	}
}

func (c *Client) IterHistory(PeerID any, opts ...*HistoryOption) (<-chan NewMessage, <-chan error) {
	ch := make(chan NewMessage)
	errCh := make(chan error)

	go func() {
		defer close(ch)
		defer close(errCh)

		var opt = getVariadic(opts, &HistoryOption{
			Limit:            1,
			SleepThresholdMs: 20,
		})

		var fetched int

		var peerToAct, err = c.ResolvePeer(PeerID)
		if err != nil {
			errCh <- err
			return
		}

		req := &MessagesGetHistoryParams{
			Peer:       peerToAct,
			OffsetID:   opt.Offset,
			OffsetDate: 0,
			MaxID:      opt.MaxID,
			MinID:      opt.MinID,
		}

		for {
			var messages []NewMessage
			remaining := opt.Limit - int32(fetched)
			perReqLimit := int32(100)
			if remaining < perReqLimit {
				perReqLimit = remaining
			}
			req.Limit = perReqLimit

			resp, err := c.MessagesGetHistory(req)
			if err != nil {
				if handleIfFlood(err, c) {
					continue
				}
				errCh <- err
				return
			}

			switch resp := resp.(type) {
			case *MessagesMessagesObj:
				c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
				for _, msg := range resp.Messages {
					messages = append(messages, *packMessage(c, msg))
				}
				fetched += len(resp.Messages)

				for _, msg := range messages {
					ch <- msg
				}
				if len(resp.Messages) < int(perReqLimit) || fetched >= int(opt.Limit) && opt.Limit > 0 {
					return
				}

				req.OffsetID = messages[len(messages)-1].ID
				req.OffsetDate = messages[len(messages)-1].Date()

			case *MessagesMessagesSlice:
				c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
				for _, msg := range resp.Messages {
					messages = append(messages, *packMessage(c, msg))
				}
				fetched += len(resp.Messages)

				for _, msg := range messages {
					ch <- msg
				}
				if len(resp.Messages) < int(perReqLimit) || fetched >= int(opt.Limit) && opt.Limit > 0 {
					return
				}

				req.OffsetID = messages[len(messages)-1].ID
				req.OffsetDate = messages[len(messages)-1].Date()
			case *MessagesChannelMessages:
				c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
				for _, msg := range resp.Messages {
					messages = append(messages, *packMessage(c, msg))
				}
				fetched += len(resp.Messages)

				for _, msg := range messages {
					ch <- msg
				}
				if len(resp.Messages) < int(perReqLimit) || fetched >= int(opt.Limit) && opt.Limit > 0 {
					return
				}

				req.OffsetID = messages[len(messages)-1].ID
				req.OffsetDate = messages[len(messages)-1].Date()
			default:
				errCh <- errors.New("unexpected response: " + reflect.TypeOf(resp).String())
				return
			}

			time.Sleep(time.Duration(opt.SleepThresholdMs) * time.Millisecond)
		}
	}()

	return ch, errCh
}

type PinOptions struct {
	Unpin     bool // whether to unpin the message
	PmOneside bool // whether to pin the message on one side
	Silent    bool // whether to pin the message silently
}

// Pin pins a message.
// This method is a wrapper for messages.pinMessage.
func (c *Client) PinMessage(PeerID any, MsgID int32, Opts ...*PinOptions) (Updates, error) {
	opts := getVariadic(Opts, &PinOptions{})
	peer, err := c.ResolvePeer(PeerID)
	if err != nil {
		return nil, err
	}
	resp, err := c.MessagesUpdatePinnedMessage(&MessagesUpdatePinnedMessageParams{
		Peer:      peer,
		ID:        MsgID,
		Unpin:     opts.Unpin,
		PmOneside: opts.PmOneside,
		Silent:    opts.Silent,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// UnpinMessage unpins a message.
func (c *Client) UnpinMessage(PeerID any, MsgID int32, Opts ...*PinOptions) (Updates, error) {
	opts := getVariadic(Opts, &PinOptions{})
	opts.Unpin = true
	return c.PinMessage(PeerID, MsgID, opts)
}

// Gets the current pinned message in a chat
func (c *Client) GetPinnedMessage(PeerID any) (*NewMessage, error) {
	resp, err := c.GetMessages(PeerID, &SearchOption{
		IDs: &InputMessagePinned{},
	})
	if err != nil {
		return nil, err
	}
	if len(resp) == 0 {
		return nil, errors.New("no pinned message")
	}
	return &resp[0], nil
}

type InlineOptions struct {
	Dialog   any           // The chat or channel to send the query to.
	Offset   string        // The offset to send.
	Query    string        // The query to send.
	GeoPoint InputGeoPoint // The location to send.
}

// InlineQuery performs an inline query and returns the results.
//
//	Params:
//	  - peerID: The ID of the Inline Bot.
//	  - Query: The query to send.
//	  - Offset: The offset to send.
//	  - Dialog: The chat or channel to send the query to.
//	  - GeoPoint: The location to send.
func (c *Client) InlineQuery(peerID any, Options ...*InlineOptions) (*MessagesBotResults, error) {
	options := getVariadic(Options, &InlineOptions{})
	peer, err := c.ResolvePeer(peerID)
	if err != nil {
		return nil, err
	}
	var dialog InputPeer = &InputPeerEmpty{}
	if options.Dialog != nil {
		dialog, err = c.ResolvePeer(options.Dialog)
		if err != nil {
			return nil, err
		}
	}
	bot, ok := peer.(*InputPeerUser)
	if !ok {
		return nil, errors.New("peer is not a bot")
	}
	resp, err := c.MessagesGetInlineBotResults(&MessagesGetInlineBotResultsParams{
		Bot:      &InputUserObj{UserID: bot.UserID, AccessHash: bot.AccessHash},
		Peer:     dialog,
		Query:    options.Query,
		Offset:   options.Offset,
		GeoPoint: options.GeoPoint,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// GetMediaGroup gets all the messages in a media group.
//
//	Params:
//	  - PeerID: The ID of the chat or channel.
//	  - MsgID: The ID of the message.
func (c *Client) GetMediaGroup(PeerID any, MsgID int32) ([]NewMessage, error) {
	_, err := c.ResolvePeer(PeerID)
	if err != nil {
		return nil, err
	}
	if MsgID <= 0 {
		return nil, errors.New("invalid message ID")
	}
	fetchIDs := func(id int32) []int32 {
		pref := id - 9
		later := id + 10
		var ids []int32
		for i := pref; i < later; i++ {
			ids = append(ids, i)
		}
		return ids
	}
	resp, err := c.GetMessages(PeerID, &SearchOption{
		IDs: fetchIDs(MsgID),
	})
	if err != nil {
		return nil, err
	}
	getMediaGroupID := func(ms []NewMessage) int64 {
		if len(ms) == 19 {
			return ms[9].Message.GroupedID
		}
		for _, m := range ms {
			if m.ID == MsgID-1 {
				return m.Message.GroupedID
			}
		}
		return 0
	}
	groupID := getMediaGroupID(resp)
	if groupID == 0 {
		return nil, errors.New("The message is not part of a media group")
	}
	sameGroup := func(m []NewMessage, groupID int64) []NewMessage {
		var msgs []NewMessage
		for _, msg := range m {
			if msg.Message.GroupedID == groupID {
				msgs = append(msgs, msg)
			}
		}
		return msgs
	}
	return sameGroup(resp, groupID), nil
}

// Internal functions

func convertOption(s *SendOptions) *MediaOptions {
	return &MediaOptions{
		ReplyID:       s.ReplyID,
		MimeType:      s.MimeType,
		Caption:       s.Caption,
		ParseMode:     s.ParseMode,
		Silent:        s.Silent,
		LinkPreview:   s.LinkPreview,
		InvertMedia:   s.InvertMedia,
		ReplyMarkup:   s.ReplyMarkup,
		ClearDraft:    s.ClearDraft,
		NoForwards:    s.NoForwards,
		ScheduleDate:  s.ScheduleDate,
		SendAs:        s.SendAs,
		Thumb:         s.Thumb,
		TTL:           s.TTL,
		Entities:      s.Entities,
		ForceDocument: s.ForceDocument,
		FileName:      s.FileName,
		Attributes:    s.Attributes,
	}
}

func getVariadic[T comparable](opts []T, def T) T {
	if len(opts) == 0 {
		return def
	}
	first := opts[0]
	var zero T
	if first == zero {
		return def
	}
	return first
}

func splitIDsIntoChunks(ids []InputMessage, chunkSize int) [][]InputMessage {
	var chunks [][]InputMessage
	for i := 0; i < len(ids); i += chunkSize {
		end := min(i+chunkSize, len(ids))
		chunks = append(chunks, ids[i:end])
	}
	return chunks
}
