package telegram

import (
	"fmt"
	"math/rand"
	"reflect"

	"github.com/pkg/errors"
)

type SendOptions struct {
	Attributes       []DocumentAttribute `json:"attributes,omitempty"`
	Caption          interface{}         `json:"caption,omitempty"`
	ClearDraft       bool                `json:"clear_draft,omitempty"`
	Entites          []MessageEntity     `json:"entites,omitempty"`
	FileName         string              `json:"file_name,omitempty"`
	ForceDocument    bool                `json:"force_document,omitempty"`
	LinkPreview      bool                `json:"link_preview,omitempty"`
	Media            interface{}         `json:"media,omitempty"`
	NoForwards       bool                `json:"no_forwards,omitempty"`
	ParseMode        string              `json:"parse_mode,omitempty"`
	ReplyID          int32               `json:"reply_id,omitempty"`
	ReplyMarkup      ReplyMarkup         `json:"reply_markup,omitempty"`
	ScheduleDate     int32               `json:"schedule_date,omitempty"`
	SendAs           interface{}         `json:"send_as,omitempty"`
	Silent           bool                `json:"silent,omitempty"`
	Thumb            interface{}         `json:"thumb,omitempty"`
	TTL              int32               `json:"ttl,omitempty"`
	Spoiler          bool                `json:"spoiler,omitempty"`
	ProgressCallback func(int32, int32)  `json:"-"`
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
func (c *Client) SendMessage(peerID, message interface{}, opts ...*SendOptions) (*NewMessage, error) {
	opt := getVariadic(opts, &SendOptions{}).(*SendOptions)
	opt.ParseMode = getStr(opt.ParseMode, c.ParseMode())
	var (
		entities    []MessageEntity
		textMessage string
		rawText     string
		media       interface{}
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
	case *NewMessage:
		entities = message.Message.Entities
		textMessage = message.MessageText()
		rawText = message.MessageText()
		media = message.Media()
	default:
		return nil, fmt.Errorf("invalid message type: %s", reflect.TypeOf(message))
	}
	if opt.Entites != nil {
		entities = opt.Entites
	}
	media = getValue(media, opt.Media)
	if media != nil {
		opt.Caption = getValue(opt.Caption, rawText)
		return c.SendMedia(peerID, media, convertOption(opt))
	}
	senderPeer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	var sendAs InputPeer
	if opt.SendAs != nil {
		sendAs, err = c.GetSendablePeer(opt.SendAs)
		if err != nil {
			return nil, err
		}
	}
	return c.sendMessage(senderPeer, textMessage, entities, sendAs, opt)
}

func (c *Client) sendMessage(Peer InputPeer, Message string, entities []MessageEntity, sendAs InputPeer, opt *SendOptions) (*NewMessage, error) {
	updateResp, err := c.MessagesSendMessage(&MessagesSendMessageParams{
		NoWebpage:              !opt.LinkPreview,
		Silent:                 opt.Silent,
		Background:             false,
		ClearDraft:             opt.ClearDraft,
		Noforwards:             opt.NoForwards,
		UpdateStickersetsOrder: false,
		Peer:                   Peer,
		ReplyTo: &InputReplyToMessage{
			ReplyToMsgID: opt.ReplyID,
		},
		Message:      Message,
		RandomID:     GenRandInt(),
		ReplyMarkup:  opt.ReplyMarkup,
		Entities:     entities,
		ScheduleDate: opt.ScheduleDate,
		SendAs:       sendAs,
	})
	if err != nil {
		return nil, err
	}
	if updateResp != nil {
		return packMessage(c, processUpdate(updateResp)), nil
	}
	return nil, errors.New("no response")
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
func (c *Client) EditMessage(peerID interface{}, id int32, message interface{}, opts ...*SendOptions) (*NewMessage, error) {
	opt := getVariadic(opts, &SendOptions{}).(*SendOptions)
	opt.ParseMode = getStr(opt.ParseMode, c.ParseMode())
	var (
		entities    []MessageEntity
		textMessage string
		media       interface{}
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
	if opt.Entites != nil {
		entities = opt.Entites
	}
	media = getValue(media, opt.Media)
	switch p := peerID.(type) {
	case *InputBotInlineMessageID:
		return c.editBotInlineMessage(*p, textMessage, entities, media, opt)
	}
	senderPeer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	return c.editMessage(senderPeer, id, textMessage, entities, media, opt)
}

func (c *Client) editMessage(Peer InputPeer, id int32, Message string, entities []MessageEntity, Media interface{}, options *SendOptions) (*NewMessage, error) {
	var (
		media InputMedia
		err   error
	)
	if Media != nil {
		media, err = c.getSendableMedia(Media, &MediaMetadata{
			Attributes:       options.Attributes,
			TTL:              options.TTL,
			ForceDocument:    options.ForceDocument,
			Thumb:            options.Thumb,
			FileName:         options.FileName,
			Spoiler:          options.Spoiler,
			ProgressCallback: options.ProgressCallback,
		})
		if err != nil {
			return nil, err
		}
	}
	updateResp, err := c.MessagesEditMessage(&MessagesEditMessageParams{
		Peer:         Peer,
		ID:           id,
		Message:      Message,
		NoWebpage:    !options.LinkPreview,
		ReplyMarkup:  options.ReplyMarkup,
		Entities:     entities,
		Media:        media,
		ScheduleDate: options.ScheduleDate,
	})
	if err != nil {
		return nil, err
	}
	if updateResp != nil {
		return packMessage(c, processUpdate(updateResp)), nil
	}
	return nil, errors.New("no response")
}

func (c *Client) editBotInlineMessage(ID InputBotInlineMessageID, Message string, entities []MessageEntity, Media interface{}, options *SendOptions) (*NewMessage, error) {
	var (
		media InputMedia
		err   error
	)
	if Media != nil {
		media, err = c.getSendableMedia(Media, &MediaMetadata{
			Attributes:       options.Attributes,
			TTL:              options.TTL,
			ForceDocument:    options.ForceDocument,
			Thumb:            options.Thumb,
			FileName:         options.FileName,
			Spoiler:          options.Spoiler,
			ProgressCallback: options.ProgressCallback,
		})
		if err != nil {
			return nil, err
		}
	}
	editRequest := &MessagesEditInlineBotMessageParams{
		ID:          ID,
		Message:     Message,
		NoWebpage:   !options.LinkPreview,
		ReplyMarkup: options.ReplyMarkup,
		Entities:    entities,
		Media:       media,
	}
	var (
		editTrue bool
		dcID     int32
	)
	switch id := ID.(type) {
	case *InputBotInlineMessageID64:
		dcID = id.DcID
	case *InputBotInlineMessageIDObj:
		dcID = id.DcID
	}
	if dcID != int32(c.GetDC()) {
		borrowedSender, borrowError := c.borrowSender(int(dcID))
		if borrowError != nil {
			return nil, borrowError
		}
		editTrue, err = borrowedSender.MessagesEditInlineBotMessage(editRequest)
	} else {
		editTrue, err = c.MessagesEditInlineBotMessage(editRequest)
	}
	if err != nil {
		return nil, err
	}
	if editTrue {
		return &NewMessage{ID: 0}, nil
	}
	return nil, errors.New("request failed")
}

type MediaOptions struct {
	Attributes       []DocumentAttribute `json:"attributes,omitempty"`
	Caption          interface{}         `json:"caption,omitempty"`
	ClearDraft       bool                `json:"clear_draft,omitempty"`
	Entites          []MessageEntity     `json:"entities,omitempty"`
	FileName         string              `json:"file_name,omitempty"`
	ForceDocument    bool                `json:"force_document,omitempty"`
	LinkPreview      bool                `json:"link_preview,omitempty"`
	NoForwards       bool                `json:"no_forwards,omitempty"`
	NoSoundVideo     bool                `json:"no_sound_video,omitempty"`
	ParseMode        string              `json:"parse_mode,omitempty"`
	ReplyID          int32               `json:"reply_id,omitempty"`
	ReplyMarkup      ReplyMarkup         `json:"reply_markup,omitempty"`
	ScheduleDate     int32               `json:"schedule_date,omitempty"`
	SendAs           interface{}         `json:"send_as,omitempty"`
	Silent           bool                `json:"silent,omitempty"`
	Thumb            interface{}         `json:"thumb,omitempty"`
	TTL              int32               `json:"ttl,omitempty"`
	Spoiler          bool                `json:"spoiler,omitempty"`
	ProgressCallback func(int32, int32)  `json:"-"`
}

type MediaMetadata struct {
	FileName              string              `json:"file_name,omitempty"`
	BuissnessConnectionId string              `json:"buissness_connection_id,omitempty"`
	Thumb                 interface{}         `json:"thumb,omitempty"`
	Attributes            []DocumentAttribute `json:"attributes,omitempty"`
	ForceDocument         bool                `json:"force_document,omitempty"`
	TTL                   int32               `json:"ttl,omitempty"`
	Spoiler               bool                `json:"spoiler,omitempty"`
	ProgressCallback      func(int32, int32)  `json:"-"`
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
//   - If the entites field in opts is not nil, it will override any entities parsed from the caption.
//   - If send_as in opts is not nil, the message will be sent from the specified peer, otherwise it will be sent from the sender peer.
func (c *Client) SendMedia(peerID, Media interface{}, opts ...*MediaOptions) (*NewMessage, error) {
	opt := getVariadic(opts, &MediaOptions{}).(*MediaOptions)
	opt.ParseMode = getStr(opt.ParseMode, c.ParseMode())

	var (
		entities    []MessageEntity
		textMessage string
	)

	sendMedia, err := c.getSendableMedia(Media, &MediaMetadata{
		FileName:         opt.FileName,
		Thumb:            opt.Thumb,
		ForceDocument:    opt.ForceDocument,
		Attributes:       opt.Attributes,
		TTL:              opt.TTL,
		Spoiler:          opt.Spoiler,
		ProgressCallback: opt.ProgressCallback,
	})

	if err != nil {
		return nil, err
	}
	switch cap := opt.Caption.(type) {
	case string:
		entities, textMessage = parseEntities(cap, opt.ParseMode)
	case *NewMessage:
		entities = cap.Message.Entities
		textMessage = cap.MessageText()
	}
	if opt.Entites != nil {
		entities = opt.Entites
	}
	senderPeer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	var sendAs InputPeer
	if opt.SendAs != nil {
		sendAs, err = c.GetSendablePeer(opt.SendAs)
		if err != nil {
			return nil, err
		}
	}
	return c.sendMedia(senderPeer, sendMedia, textMessage, entities, sendAs, opt)
}

func (c *Client) sendMedia(Peer InputPeer, Media InputMedia, Caption string, entities []MessageEntity, sendAs InputPeer, opt *MediaOptions) (*NewMessage, error) {
	updateResp, err := c.MessagesSendMedia(&MessagesSendMediaParams{
		Silent:                 opt.Silent,
		Background:             false,
		ClearDraft:             opt.ClearDraft,
		Noforwards:             opt.NoForwards,
		UpdateStickersetsOrder: false,
		Peer:                   Peer,
		ReplyTo: &InputReplyToMessage{
			ReplyToMsgID: opt.ReplyID,
		},
		Media:        Media,
		RandomID:     GenRandInt(),
		ReplyMarkup:  opt.ReplyMarkup,
		Message:      Caption,
		Entities:     entities,
		ScheduleDate: opt.ScheduleDate,
		SendAs:       sendAs,
	})
	if err != nil {
		return nil, err
	}
	if updateResp != nil {
		return packMessage(c, processUpdate(updateResp)), nil
	}
	return nil, errors.New("no response")
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
//   - If the entites field in opts is not nil, it will override any entities parsed from the caption.
//   - If send_as in opts is not nil, the messages will be sent from the specified peer, otherwise they will be sent from the sender peer.
func (c *Client) SendAlbum(peerID, Album interface{}, opts ...*MediaOptions) ([]*NewMessage, error) {
	opt := getVariadic(opts, &MediaOptions{}).(*MediaOptions)
	opt.ParseMode = getStr(opt.ParseMode, c.ParseMode())
	var (
		entities    []MessageEntity
		textMessage string
	)
	InputAlbum, multiErr := c.getMultiMedia(Album, &MediaMetadata{FileName: opt.FileName, Thumb: opt.Thumb, ForceDocument: opt.ForceDocument, Attributes: opt.Attributes, TTL: opt.TTL, Spoiler: opt.Spoiler})
	if multiErr != nil {
		return nil, multiErr
	}

	switch cap := opt.Caption.(type) {
	case string:
		entities, textMessage = parseEntities(cap, opt.ParseMode)
	case *NewMessage:
		entities = cap.Message.Entities
		textMessage = cap.MessageText()
	}
	if opt.Entites != nil {
		entities = opt.Entites
	}
	InputAlbum[len(InputAlbum)-1].Message = textMessage
	InputAlbum[len(InputAlbum)-1].Entities = entities
	senderPeer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	var sendAs InputPeer
	if opt.SendAs != nil {
		sendAs, err = c.GetSendablePeer(opt.SendAs)
		if err != nil {
			return nil, err
		}
	}
	return c.sendAlbum(senderPeer, InputAlbum, sendAs, opt)
}

func (c *Client) sendAlbum(Peer InputPeer, Album []*InputSingleMedia, sendAs InputPeer, opt *MediaOptions) ([]*NewMessage, error) {
	updateResp, err := c.MessagesSendMultiMedia(&MessagesSendMultiMediaParams{
		Silent:                 opt.Silent,
		Background:             false,
		ClearDraft:             opt.ClearDraft,
		Noforwards:             opt.NoForwards,
		UpdateStickersetsOrder: false,
		Peer:                   Peer,
		ReplyTo: &InputReplyToMessage{
			ReplyToMsgID: opt.ReplyID,
		},
		ScheduleDate: opt.ScheduleDate,
		SendAs:       sendAs,
		MultiMedia:   Album,
	})
	if err != nil {
		return nil, err
	}
	var m []*NewMessage
	if updateResp != nil {
		updates := processUpdates(updateResp)
		for _, update := range updates {
			m = append(m, packMessage(c, update))
		}
	} else {
		return nil, errors.New("no response")
	}
	return m, nil
}

// SendReaction sends a reaction to a message.
// This method is a wrapper for messages.sendReaction
//
//	Params:
//	 - peerID: ID of the peer to send the message to.
//	 - msgID: ID of the message to react to.
//	 - reaction: Reaction to send.
//	 - big: Whether to use big emoji.
func (c *Client) SendReaction(peerID interface{}, msgID int32, reaction interface{}, big ...bool) error {
	b := getVariadic(big, false).(bool)
	peer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return err
	}
	var r []Reaction
	switch reaction := reaction.(type) {
	case string:
		if reaction == "" {
			r = append(r, &ReactionEmpty{})
		}
		r = append(r, &ReactionEmoji{reaction})
	case []string:
		for _, v := range reaction {
			if v == "" {
				r = append(r, &ReactionEmpty{})
			}
			r = append(r, &ReactionEmoji{v})
		}
	case ReactionCustomEmoji:
		r = append(r, &reaction)
	case []ReactionCustomEmoji:
		for _, v := range reaction {
			r = append(r, &v)
		}
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

// SendDice sends a special dice message.
// This method calls messages.sendMedia with a dice media.
func (c *Client) SendDice(peerID interface{}, emoji string) (*NewMessage, error) {
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
func (c *Client) SendAction(PeerID, Action interface{}, topMsgID ...int32) (*ActionResult, error) {
	peerChat, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return nil, err
	}
	TopMsgID := getVariadic(topMsgID, int32(0)).(int32)
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
func (c *Client) SendReadAck(PeerID interface{}, MaxID ...int32) (*MessagesAffectedMessages, error) {
	peerChat, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return nil, err
	}
	maxID := getVariadic(MaxID, int32(0)).(int32)
	return c.MessagesReadHistory(peerChat, maxID)
}

// SendPoll sends a poll. TODO

type ForwardOptions struct {
	HideCaption  bool        `json:"hide_caption,omitempty"`
	HideAuthor   bool        `json:"hide_author,omitempty"`
	Silent       bool        `json:"silent,omitempty"`
	Protected    bool        `json:"protected,omitempty"`
	Background   bool        `json:"background,omitempty"`
	WithMyScore  bool        `json:"with_my_score,omitempty"`
	SendAs       interface{} `json:"send_as,omitempty"`
	ScheduleDate int32       `json:"schedule_date,omitempty"`
}

// Forward forwards a message.
// This method is a wrapper for messages.forwardMessages.
func (c *Client) Forward(peerID, fromPeerID interface{}, msgIDs []int32, opts ...*ForwardOptions) ([]NewMessage, error) {
	opt := getVariadic(opts, &ForwardOptions{}).(*ForwardOptions)
	toPeer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	fromPeer, err := c.GetSendablePeer(fromPeerID)
	if err != nil {
		return nil, err
	}
	randomIDs := make([]int64, len(msgIDs))
	for i := range randomIDs {
		randomIDs[i] = rand.Int63()
	}
	var sendAs InputPeer
	if opt.SendAs != nil {
		sendAs, err = c.GetSendablePeer(opt.SendAs)
		if err != nil {
			return nil, err
		}
	}
	updateResp, err := c.MessagesForwardMessages(&MessagesForwardMessagesParams{
		ToPeer:            toPeer,
		FromPeer:          fromPeer,
		ID:                msgIDs,
		RandomID:          randomIDs,
		Silent:            opt.Silent,
		Background:        false,
		Noforwards:        opt.Protected,
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
func (c *Client) DeleteMessages(peerID interface{}, msgIDs []int32, Revoke ...bool) (*MessagesAffectedMessages, error) {
	revoke := getVariadic(Revoke, false).(bool)
	peer, err := c.GetSendablePeer(peerID)
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
		return c.MessagesDeleteMessages(revoke, msgIDs)
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
	IDs      interface{}    `json:"ids,omitempty"`
	Query    string         `json:"query,omitempty"`
	FromUser interface{}    `json:"from_user,omitempty"`
	Offset   int32          `json:"offset,omitempty"`
	Limit    int32          `json:"limit,omitempty"`
	Filter   MessagesFilter `json:"filter,omitempty"`
	TopMsgID int32          `json:"top_msg_id,omitempty"`
	MaxID    int32          `json:"max_id,omitempty"`
	MinID    int32          `json:"min_id,omitempty"`
	MaxDate  int32          `json:"max_date,omitempty"`
	MinDate  int32          `json:"min_date,omitempty"`
}

func (c *Client) GetMessages(PeerID interface{}, Opts ...*SearchOption) ([]NewMessage, error) {
	opt := getVariadic(Opts, &SearchOption{
		Filter: &InputMessagesFilterEmpty{},
	}).(*SearchOption)
	peer, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return nil, err
	}
	var (
		m        []Message
		messages []NewMessage
		inputIDs []InputMessage
		result   MessagesMessages
	)
	switch i := opt.IDs.(type) {
	case []int32, []int64, []int:
		var ids []int32
		switch i := i.(type) {
		case []int32:
			ids = make([]int32, len(i))
			copy(ids, i)
		case []int64:
			ids = make([]int32, len(i))
			for j, id := range i {
				ids[j] = int32(id)
			}
		case []int:
			ids = make([]int32, len(i))
			for j, id := range i {
				ids[j] = int32(id)
			}
		}
		for _, id := range ids {
			inputIDs = append(inputIDs, &InputMessageID{ID: id})
		}
	case int, int64, int32:
		inputIDs = append(inputIDs, &InputMessageID{ID: int32(i.(int))})
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
		switch peer := peer.(type) {
		case *InputPeerChannel:
			result, err = c.ChannelsGetMessages(&InputChannelObj{ChannelID: peer.ChannelID, AccessHash: peer.AccessHash}, inputIDs)
		case *InputPeerChat, *InputPeerUser:
			result, err = c.MessagesGetMessages(inputIDs)
		default:
			return nil, errors.New("invalid peer type")
		}
		if err != nil {
			return nil, err
		}
		switch result := result.(type) {
		case *MessagesChannelMessages:
			c.Cache.UpdatePeersToCache(result.Users, result.Chats)
			m = append(m, result.Messages...)
		case *MessagesMessagesObj:
			c.Cache.UpdatePeersToCache(result.Users, result.Chats)
			m = append(m, result.Messages...)
		}
	} else {
		params := &MessagesSearchParams{
			Peer:      peer,
			Q:         opt.Query,
			OffsetID:  opt.Offset,
			AddOffset: opt.Limit,
			Filter:    opt.Filter,
			MinDate:   opt.MinDate,
			MaxDate:   opt.MaxDate,
			MinID:     opt.MinID,
			MaxID:     opt.MaxID,
			Limit:     opt.Limit,
			TopMsgID:  opt.TopMsgID,
		}
		if opt.FromUser != nil {
			fromUser, err := c.GetSendablePeer(opt.FromUser)
			if err != nil {
				return nil, err
			}
			params.FromID = fromUser
		}
		result, err = c.MessagesSearch(params)
		if err != nil {
			return nil, err
		}
		switch result := result.(type) {
		case *MessagesChannelMessages:
			c.Cache.UpdatePeersToCache(result.Users, result.Chats)
			m = append(m, result.Messages...)
		case *MessagesMessagesObj:
			c.Cache.UpdatePeersToCache(result.Users, result.Chats)
			m = append(m, result.Messages...)
		}
	}
	for _, msg := range m {
		messages = append(messages, *packMessage(c, msg))
	}
	return messages, nil
}

type PinOptions struct {
	Unpin     bool `json:"unpin,omitempty"`
	PmOneside bool `json:"pm_oneside,omitempty"`
	Silent    bool `json:"silent,omitempty"`
}

// Pin pins a message.
// This method is a wrapper for messages.pinMessage.
func (c *Client) PinMessage(PeerID interface{}, MsgID int32, Opts ...*PinOptions) (Updates, error) {
	opts := getVariadic(Opts, &PinOptions{}).(*PinOptions)
	peer, err := c.GetSendablePeer(PeerID)
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
func (c *Client) UnpinMessage(PeerID interface{}, MsgID int32, Opts ...*PinOptions) (Updates, error) {
	opts := getVariadic(Opts, &PinOptions{}).(*PinOptions)
	opts.Unpin = true
	return c.PinMessage(PeerID, MsgID, opts)
}

// Gets the current pinned message in a chat
func (c *Client) GetPinnedMessage(PeerID interface{}) (*NewMessage, error) {
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
	Dialog   interface{}
	Offset   int32
	Query    string
	GeoPoint InputGeoPoint
}

// InlineQuery performs an inline query and returns the results.
//
//	Params:
//	  - peerID: The ID of the Inline Bot.
//	  - Query: The query to send.
//	  - Offset: The offset to send.
//	  - Dialog: The chat or channel to send the query to.
//	  - GeoPoint: The location to send.
func (c *Client) InlineQuery(peerID interface{}, Options ...*InlineOptions) (*MessagesBotResults, error) {
	options := getVariadic(Options, &InlineOptions{}).(*InlineOptions)
	peer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	var dialog InputPeer = &InputPeerEmpty{}
	if options.Dialog != nil {
		dialog, err = c.GetSendablePeer(options.Dialog)
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
		Offset:   fmt.Sprintf("%d", options.Offset),
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
func (c *Client) GetMediaGroup(PeerID interface{}, MsgID int32) ([]NewMessage, error) {
	_, err := c.GetSendablePeer(PeerID)
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
		Caption:       s.Caption,
		ParseMode:     s.ParseMode,
		Silent:        s.Silent,
		LinkPreview:   s.LinkPreview,
		ReplyMarkup:   s.ReplyMarkup,
		ClearDraft:    s.ClearDraft,
		NoForwards:    s.NoForwards,
		ScheduleDate:  s.ScheduleDate,
		SendAs:        s.SendAs,
		Thumb:         s.Thumb,
		TTL:           s.TTL,
		ForceDocument: s.ForceDocument,
		FileName:      s.FileName,
		Attributes:    s.Attributes,
	}
}

func getVariadic(v, def interface{}) interface{} {
	if v == nil {
		return def
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice {
		return v
	}
	if rv.Len() == 0 {
		return def
	}
	return rv.Index(0).Interface()
}
