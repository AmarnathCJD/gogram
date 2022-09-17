package telegram

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/pkg/errors"
)

func (c *Client) GetMe() (*UserObj, error) {
	resp, err := c.UsersGetFullUser(&InputUserSelf{})
	if err != nil {
		return nil, errors.Wrap(err, "getting user")
	}
	user, ok := resp.Users[0].(*UserObj)
	if !ok {
		return nil, errors.New("got wrong response: " + reflect.TypeOf(resp).String())
	}
	return user, nil
}

func (c *Client) SendMessage(peerID interface{}, TextObj interface{}, Opts ...*SendOptions) (*NewMessage, error) {
	var options SendOptions
	if len(Opts) > 0 {
		options = *Opts[0]
	}
	if options.ParseMode == "" {
		options.ParseMode = c.ParseMode
	}
	var e []MessageEntity
	var Text string
	switch TextObj := TextObj.(type) {
	case string:
		e, Text = c.FormatMessage(TextObj, options.ParseMode)
	case MessageMedia, InputMedia:
		return c.SendMedia(peerID, TextObj, &MediaOptions{
			Caption:     options.Caption,
			ParseMode:   options.ParseMode,
			LinkPreview: options.LinkPreview,
			ReplyID:     options.ReplyID,
			ReplyMarkup: options.ReplyMarkup,
			NoForwards:  options.NoForwards,
			Silent:      options.Silent,
			ClearDraft:  options.ClearDraft,
		})
	case *NewMessage:
		if TextObj.Media() != nil {
			return c.SendMedia(peerID, TextObj.Media(), &MediaOptions{
				Caption:     getValue(options.Caption, TextObj.Text()).(string),
				ParseMode:   options.ParseMode,
				LinkPreview: options.LinkPreview,
				ReplyID:     options.ReplyID,
				ReplyMarkup: *getValue(&options.ReplyMarkup, TextObj.ReplyMarkup).(*ReplyMarkup),
				NoForwards:  options.NoForwards,
				Silent:      options.Silent,
				ClearDraft:  options.ClearDraft,
			})
		}
		Text = TextObj.Text()
		e, Text = c.FormatMessage(Text, options.ParseMode)
		options.ReplyMarkup = *getValue(&options.ReplyMarkup, TextObj.ReplyMarkup).(*ReplyMarkup)
	}
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	Update, err := c.MessagesSendMessage(&MessagesSendMessageParams{
		Peer:         PeerToSend,
		Message:      Text,
		RandomID:     GenRandInt(),
		ReplyToMsgID: options.ReplyID,
		Entities:     e,
		ReplyMarkup:  options.ReplyMarkup,
		NoWebpage:    options.LinkPreview,
		Silent:       options.Silent,
		ClearDraft:   options.ClearDraft,
	})
	if err != nil {
		c.Logger.Println("Error sending message: ", err)
		if strings.Contains(err.Error(), "ENTITY_BOUNDS_INVALID") {
			Update, err = c.MessagesSendMessage(&MessagesSendMessageParams{
				Peer:         PeerToSend,
				Message:      Text,
				RandomID:     GenRandInt(),
				ReplyToMsgID: options.ReplyID,
				Entities:     []MessageEntity{},
				ReplyMarkup:  options.ReplyMarkup,
				NoWebpage:    options.LinkPreview,
				Silent:       options.Silent,
				ClearDraft:   options.ClearDraft,
				ScheduleDate: options.ScheduleDate,
				SendAs:       options.SendAs,
			})
		} else {
			return nil, err
		}
	}
	return packMessage(c, processUpdate(Update)), err
}

func (c *Client) EditMessage(peerID interface{}, MsgID int32, TextObj interface{}, Opts ...*SendOptions) (*NewMessage, error) {
	var options SendOptions
	if len(Opts) > 0 {
		options = *Opts[0]
	}
	if options.ParseMode == "" {
		options.ParseMode = c.ParseMode
	}
	var err error
	var e []MessageEntity
	var Text string
	var Media InputMedia
	if options.Media != nil {
		m, err := c.getSendableMedia(options.Media, &CustomAttrs{
			Attributes:    options.Attributes,
			TTL:           options.TTL,
			ForceDocument: options.ForceDocument,
			Thumb:         options.Thumb,
			FileName:      options.FileName,
		})
		if err == nil {
			Media = m
		} else {
			Media = &InputMediaEmpty{}
		}
	}
	switch TextObj := TextObj.(type) {
	case string:
		e, Text = c.FormatMessage(TextObj, options.ParseMode)
	}
	switch peerID := peerID.(type) {
	case InputBotInlineMessageID:
		sender := c
		switch p := peerID.(type) {
		case *InputBotInlineMessageIDObj:
			if int(p.DcID) != sender.GetDC() {
				sender, err = sender.ExportSender(int(p.DcID))
				if err != nil {
					return nil, err
				}
				defer sender.Terminate()
			}
		case *InputBotInlineMessageID64:
			if int(p.DcID) != sender.GetDC() {
				sender, err = sender.ExportSender(int(p.DcID))
				if err != nil {
					return nil, err
				}
				defer sender.Terminate()
			}
		}
		_, err := sender.MessagesEditInlineBotMessage(&MessagesEditInlineBotMessageParams{
			NoWebpage:   !options.LinkPreview,
			ID:          peerID,
			Message:     Text,
			Media:       Media,
			ReplyMarkup: options.ReplyMarkup,
			Entities:    e,
		})
		if err != nil {
			return nil, err
		}
		return &NewMessage{ID: 0}, nil
	case nil:
		return nil, errors.New("peerID cant be nil")
	default:
		PeerToSend, err := c.GetSendablePeer(peerID)
		if err != nil {
			return nil, err
		}
		Update, err := c.MessagesEditMessage(&MessagesEditMessageParams{
			Peer:         PeerToSend,
			Message:      Text,
			ID:           MsgID,
			Entities:     e,
			NoWebpage:    options.LinkPreview,
			Media:        Media,
			ReplyMarkup:  options.ReplyMarkup,
			ScheduleDate: options.ScheduleDate,
		})
		if err != nil {
			return nil, err
		}
		return packMessage(c, processUpdate(Update)), err
	}
}

func (c *Client) DeleteMessage(peerID interface{}, MsgIDs ...int32) error {
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return err
	}
	PeerChannel, ok := PeerToSend.(*InputPeerChannel)
	if !ok {
		_, err = c.MessagesDeleteMessages(true, MsgIDs)
		return err
	}
	_, err = c.ChannelsDeleteMessages(&InputChannelObj{ChannelID: PeerChannel.ChannelID, AccessHash: PeerChannel.AccessHash}, MsgIDs)
	return err
}

func (c *Client) ForwardMessage(fromID interface{}, toID interface{}, MsgIDs []int32, Opts ...*ForwardOptions) (*NewMessage, error) {
	var options ForwardOptions
	FromPeer, err := c.GetSendablePeer(fromID)
	if err != nil {
		return nil, err
	}
	ToPeer, err := c.GetSendablePeer(toID)
	if err != nil {
		return nil, err
	}
	Update, err := c.MessagesForwardMessages(&MessagesForwardMessagesParams{
		FromPeer:          FromPeer,
		ToPeer:            ToPeer,
		ID:                MsgIDs,
		RandomID:          []int64{GenRandInt()},
		DropMediaCaptions: options.HideCaption,
		DropAuthor:        options.HideAuthor,
		Noforwards:        options.Protected,
		Silent:            options.Silent,
	})
	if err != nil {
		return nil, err
	}
	return packMessage(c, processUpdate(Update)), nil
}

func (c *Client) SendDice(peerID interface{}, Emoji string) (*NewMessage, error) {
	media := &InputMediaDice{
		Emoticon: Emoji,
	}
	return c.SendMedia(peerID, media)
}

func (c *Client) SendMedia(peerID interface{}, Media interface{}, Opts ...*MediaOptions) (*NewMessage, error) {
	var options MediaOptions
	if len(Opts) > 0 {
		options = *Opts[0]
	}
	if options.ParseMode == "" {
		options.ParseMode = c.ParseMode
	}
	var Caption string
	var e []MessageEntity
	switch Capt := options.Caption.(type) {
	case string:
		e, Caption = c.FormatMessage(Capt, options.ParseMode)
	}
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	MediaFile, err := c.getSendableMedia(Media, &CustomAttrs{
		FileName:      options.FileName,
		Thumb:         options.Thumb,
		ForceDocument: options.ForceDocument,
		Attributes:    options.Attributes,
		TTL:           options.TTL,
	})
	if err != nil {
		return nil, err
	}
	Update, err := c.MessagesSendMedia(&MessagesSendMediaParams{
		Peer:         PeerToSend,
		Media:        MediaFile,
		Message:      Caption,
		RandomID:     GenRandInt(),
		ReplyToMsgID: options.ReplyID,
		Entities:     e,
		ReplyMarkup:  options.ReplyMarkup,
		Silent:       options.Silent,
		ClearDraft:   options.ClearDraft,
		Noforwards:   options.NoForwards,
	})
	if err != nil {
		return nil, err
	}
	return packMessage(c, processUpdate(Update)), err
}

func (c *Client) SendAlbum(peerID interface{}, Media interface{}, Opts ...*MediaOptions) ([]*NewMessage, error) {
	var options MediaOptions
	if len(Opts) > 0 {
		options = *Opts[0]
	}
	if options.ParseMode == "" {
		options.ParseMode = c.ParseMode
	}
	var Caption string
	var e []MessageEntity
	switch Capt := options.Caption.(type) {
	case string:
		e, Caption = c.FormatMessage(Capt, options.ParseMode)
	}
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	MediaFiles, multiErr := c.getMultiMedia(Media, &CustomAttrs{
		FileName:      options.FileName,
		Thumb:         options.Thumb,
		ForceDocument: options.ForceDocument,
		Attributes:    options.Attributes,
		TTL:           options.TTL,
	})
	if err != nil {
		return nil, multiErr
	}
	MediaFiles[len(MediaFiles)-1].Message = Caption
	MediaFiles[len(MediaFiles)-1].Entities = e
	Update, err := c.MessagesSendMultiMedia(&MessagesSendMultiMediaParams{
		Peer:         PeerToSend,
		Silent:       options.Silent,
		ClearDraft:   options.ClearDraft,
		Noforwards:   options.NoForwards,
		ReplyToMsgID: options.ReplyID,
		MultiMedia:   MediaFiles,
		ScheduleDate: options.ScheduleDate,
		SendAs:       options.SendAs,
	})
	if err != nil {
		return nil, err
	}
	var m []*NewMessage
	updates := processUpdates(Update)
	for _, update := range updates {
		m = append(m, packMessage(c, update))
	}
	return m, nil
}

func (c *Client) SendReaction(peerID interface{}, MsgID int32, reactionEmoji interface{}, Big ...bool) error {
	var big bool
	if len(Big) > 0 {
		big = Big[0]
	}
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return err
	}
	var r []Reaction
	switch reaction := reactionEmoji.(type) {
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
		Peer:        PeerToSend,
		Big:         big,
		AddToRecent: true,
		MsgID:       MsgID,
		Reaction:    r,
	})
	return err
}

func (c *Client) GetParticipants(PeerID interface{}, opts ...*ParticipantOptions) ([]Participant, int32, error) {
	var options = &ParticipantOptions{}
	if len(opts) > 0 {
		options = opts[0]
	} else {
		options = &ParticipantOptions{
			Filter: &ChannelParticipantsSearch{},
			Offset: 0,
			Limit:  1,
			Query:  "",
		}
	}
	if options.Query != "" {
		options.Filter = &ChannelParticipantsSearch{
			Q: options.Query,
		}
	}
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return nil, 0, err
	}
	Channel, ok := PeerToSend.(*InputPeerChannel)
	if !ok {
		return nil, 0, errors.New("peer is not a channel")
	}
	p, err := c.ChannelsGetParticipants(
		&InputChannelObj{ChannelID: Channel.ChannelID, AccessHash: Channel.AccessHash},
		options.Filter,
		options.Offset,
		options.Limit,
		0,
	)
	if err != nil {
		return nil, 0, err
	}
	ParticipantsResponse := p.(*ChannelsChannelParticipantsObj)
	c.Cache.UpdatePeersToCache(ParticipantsResponse.Users, ParticipantsResponse.Chats)
	var Participants []Participant
	for _, u := range ParticipantsResponse.Participants {
		var p = &Participant{}
		p.Participant = u
		switch u := u.(type) {
		case *ChannelParticipantObj:
			p.User, _ = c.Cache.GetUser(u.UserID)
			p.Rights = &ChatAdminRights{}
		case *ChannelParticipantLeft:
			peerID, _ := c.GetSendablePeer(u.Peer)
			if u, ok := peerID.(*InputPeerUser); ok {
				p.User, _ = c.Cache.GetUser(u.UserID)
			}
			p.Left = true
			p.Rights = &ChatAdminRights{}
		case *ChannelParticipantBanned:
			peerID, _ := c.GetSendablePeer(u.Peer)
			if u, ok := peerID.(*InputPeerUser); ok {
				p.User, _ = c.Cache.GetUser(u.UserID)
			}
			p.Left = true
			p.Banned = true
			p.Rights = &ChatAdminRights{}
		case *ChannelParticipantAdmin:
			p.User, _ = c.Cache.GetUser(u.UserID)
			p.Admin = true
			p.Rights = u.AdminRights
		case *ChannelParticipantCreator:
			p.User, _ = c.Cache.GetUser(u.UserID)
			p.Creator = true
			p.Admin = true
			p.Rights = u.AdminRights
		default:
			fmt.Println("unknown participant type", reflect.TypeOf(u).String())
		}
		Participants = append(Participants, *p)

	}
	return Participants, ParticipantsResponse.Count, nil
}

func (c *Client) GetChatMember(PeerID interface{}, UserID interface{}) (Participant, error) {
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return Participant{}, err
	}
	Channel, ok := PeerToSend.(*InputPeerChannel)
	if !ok {
		return Participant{}, errors.New("peer is not a channel")
	}
	PeerPart, err := c.GetSendablePeer(UserID)
	if err != nil {
		return Participant{}, err
	}
	ParticipantResponse, err := c.ChannelsGetParticipant(
		&InputChannelObj{ChannelID: Channel.ChannelID, AccessHash: Channel.AccessHash},
		PeerPart,
	)
	if err != nil {
		return Participant{}, err
	}
	c.Cache.UpdatePeersToCache(ParticipantResponse.Users, ParticipantResponse.Chats)
	var Participant = &Participant{}
	Participant.Participant = ParticipantResponse.Participant
	switch P := ParticipantResponse.Participant.(type) {
	case *ChannelParticipantAdmin:
		Participant.Admin = true
		Participant.Rights = P.AdminRights
	case *ChannelParticipantCreator:
		Participant.Creator = true
		Participant.Rights = P.AdminRights
	case *ChannelParticipantLeft:
		Participant.Left = true
		Participant.Rights = &ChatAdminRights{}
	case *ChannelParticipantBanned:
		Participant.Banned = true
		Participant.Rights = &ChatAdminRights{}
	case *ChannelParticipantObj:
		Participant.Rights = &ChatAdminRights{}
	}
	return *Participant, nil
}

func (c *Client) SendAction(PeerID interface{}, Action interface{}) (*ActionResult, error) {
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return nil, err
	}
	switch a := Action.(type) {
	case string:
		if action, ok := Actions[a]; ok {
			_, err = c.MessagesSetTyping(PeerToSend, 0, action)
		} else {
			return nil, errors.New("unknown action")
		}
	case *SendMessageAction:
		_, err = c.MessagesSetTyping(PeerToSend, 0, *a)
	default:
		return nil, errors.New("unknown action type")
	}
	return &ActionResult{PeerToSend, c}, err
}

func (c *Client) SendReadAcknowledge(PeerID interface{}, MaxID ...int32) (*MessagesAffectedMessages, error) {
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return nil, err
	}
	return c.MessagesReadHistory(PeerToSend, MaxID[0])
}

func (c *Client) AnswerInlineQuery(QueryID int64, Results []InputBotInlineResult, Options ...*InlineSendOptions) (bool, error) {
	var options *InlineSendOptions
	if len(Options) > 0 {
		options = Options[0]
	} else {
		options = &InlineSendOptions{}
	}
	options.CacheTime = getValue(options.CacheTime, 60).(int32)
	request := &MessagesSetInlineBotResultsParams{
		Gallery:    options.Gallery,
		Private:    options.Private,
		QueryID:    QueryID,
		Results:    Results,
		CacheTime:  options.CacheTime,
		NextOffset: options.NextOffset,
	}
	if options.SwitchPm != "" {
		request.SwitchPm = &InlineBotSwitchPm{
			Text:       options.SwitchPm,
			StartParam: getValue(options.SwitchPmText, "start").(string),
		}
	}
	resp, err := c.MessagesSetInlineBotResults(request)
	if err != nil {
		return false, err
	}
	return resp, nil
}

func (c *Client) AnswerCallbackQuery(QueryID int64, Text string, Opts ...*CallbackOptions) (bool, error) {
	var options *CallbackOptions
	if len(Opts) > 0 {
		options = Opts[0]
	} else {
		options = &CallbackOptions{}
	}
	request := &MessagesSetBotCallbackAnswerParams{
		QueryID: QueryID,
		Message: Text,
		Alert:   options.Alert,
	}
	if options.URL != "" {
		request.URL = options.URL
	}
	if options.CacheTime != 0 {
		request.CacheTime = options.CacheTime
	}
	resp, err := c.MessagesSetBotCallbackAnswer(request)
	if err != nil {
		return false, err
	}
	return resp, nil
}

// Edit Admin rights of a user in a chat,
// returns true if successfull
func (c *Client) EditAdmin(PeerID interface{}, UserID interface{}, opts ...*AdminOptions) (bool, error) {
	var (
		IsAdmin     bool
		Rank        string
		AdminRights *ChatAdminRights
		err         error
	)
	if len(opts) > 0 {
		IsAdmin = opts[0].IsAdmin
		AdminRights = opts[0].Rights
		Rank = opts[0].Rank
	} else {
		IsAdmin = true
		AdminRights = &ChatAdminRights{}
	}
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return false, err
	}
	PeerPart, err := c.GetSendablePeer(UserID)
	if err != nil {
		return false, err
	}
	if user, ok := PeerPart.(*InputPeerUser); !ok {
		return false, errors.New("peer is not a user")
	} else {
		switch p := PeerToSend.(type) {
		case *InputPeerChannel:
			_, err = c.ChannelsEditAdmin(
				&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
				&InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash},
				AdminRights,
				Rank,
			)
		case *InputPeerChat:
			_, err = c.MessagesEditChatAdmin(p.ChatID, &InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash}, IsAdmin)
		default:
			return false, errors.New("peer is not a chat or channel")
		}
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) EditBanned(PeerID interface{}, UserID interface{}, opts ...*BannedOptions) (bool, error) {
	var (
		BannedOptions *BannedOptions
		err           error
	)
	if len(opts) > 0 {
		BannedOptions = opts[0]
	}
	if BannedOptions.Rights == nil {
		BannedOptions.Rights = &ChatBannedRights{}
	}
	if BannedOptions.Ban {
		BannedOptions.Rights.ViewMessages = true
	} else if BannedOptions.Unban {
		BannedOptions.Rights.ViewMessages = false
	} else if BannedOptions.Mute {
		BannedOptions.Rights.SendMessages = true
	} else if BannedOptions.Unmute {
		BannedOptions.Rights.SendMessages = false
	}
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return false, err
	}
	PeerPart, err := c.GetSendablePeer(UserID)
	if err != nil {
		return false, err
	}
	switch p := PeerToSend.(type) {
	case *InputPeerChannel:
		_, err = c.ChannelsEditBanned(
			&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
			PeerPart,
			BannedOptions.Rights,
		)
	case *InputPeerChat:
		return false, errors.New("method not found")
	default:
		return false, errors.New("peer is not a chat or channel")
	}

	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) KickParticipant(PeerID interface{}, UserID interface{}) (bool, error) {
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return false, err
	}
	PeerPart, err := c.GetSendablePeer(UserID)
	if err != nil {
		return false, err
	}
	switch p := PeerToSend.(type) {
	case *InputPeerChannel:
		_, err := c.EditBanned(p, PeerPart, &BannedOptions{Ban: true})
		if err != nil {
			return false, err
		}
		return c.EditBanned(p, PeerPart, &BannedOptions{Unban: true})
	case *InputPeerChat:
		u, ok := PeerPart.(*InputPeerUser)
		if !ok {
			return false, errors.New("peer is not a user")
		}
		_, err = c.MessagesDeleteChatUser(true, p.ChatID, &InputUserObj{UserID: u.UserID, AccessHash: u.AccessHash})
	default:
		return false, errors.New("peer is not a chat or channel")
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) EditTitle(PeerID interface{}, Title string, Opts ...TitleOptions) (bool, error) {
	var options TitleOptions
	if len(Opts) > 0 {
		options = Opts[0]
	}
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return false, err
	}
	switch p := PeerToSend.(type) {
	case *InputPeerChannel:
		_, err = c.ChannelsEditTitle(&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash}, Title)
	case *InputPeerChat:
		_, err = c.MessagesEditChatTitle(p.ChatID, Title)
	case *InputPeerSelf:
		_, err = c.AccountUpdateProfile(Title, options.LastName, options.About)
		if err != nil {
			return false, err
		}
	default:
		return false, errors.New("peer is not a chat or channel or self")
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetMessages returns a slice of messages from a chat,
// if IDs are not specifed - MessagesSearch is used.
func (c *Client) GetMessages(PeerID interface{}, Options ...*SearchOption) ([]NewMessage, error) {
	var (
		Opts *SearchOption
		err  error
	)
	if len(Options) > 0 {
		Opts = Options[0]
	} else {
		Opts = &SearchOption{}
	}
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return nil, err
	}
	var Messages []NewMessage
	var MessagesSlice []Message
	var MsgIDs []InputMessage
	for _, ID := range Opts.IDs {
		MsgIDs = append(MsgIDs, &InputMessageID{ID: ID})
	}
	if len(MsgIDs) == 0 && (Opts.Query == "" && Opts.Limit == 0) {
		Opts.Limit = 1
	}
	if Opts.Filter == nil {
		Opts.Filter = &InputMessagesFilterEmpty{}
	}
	switch p := PeerToSend.(type) {
	case *InputPeerChannel:
		if len(MsgIDs) > 0 {
			resp, err := c.ChannelsGetMessages(&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash}, MsgIDs)
			if err != nil {
				return nil, err
			}
			Messages, ok := resp.(*MessagesChannelMessages)
			if !ok {
				return nil, errors.New("could not convert messages: " + reflect.TypeOf(resp).String())
			}
			MessagesSlice = Messages.Messages
		} else {
			resp, err := c.MessagesSearch(&MessagesSearchParams{
				Peer: &InputPeerChannel{
					ChannelID:  p.ChannelID,
					AccessHash: p.AccessHash,
				},
				Q:        Opts.Query,
				OffsetID: Opts.Offset,
				Limit:    Opts.Limit,
				Filter:   Opts.Filter,
				MaxDate:  Opts.MaxDate,
				MinDate:  Opts.MinDate,
				MaxID:    Opts.MaxID,
				MinID:    Opts.MinID,
				TopMsgID: Opts.TopMsgID,
			})
			if err != nil {
				return nil, err
			}
			Messages, ok := resp.(*MessagesChannelMessages)
			if !ok {
				return nil, errors.New("could not convert messages: " + reflect.TypeOf(resp).String())
			}
			MessagesSlice = Messages.Messages
		}
	case *InputPeerChat:
		if len(MsgIDs) > 0 {
			resp, err := c.MessagesGetMessages(MsgIDs)
			if err != nil {
				return nil, err
			}
			Messages, ok := resp.(*MessagesMessagesObj)
			if !ok {
				return nil, errors.New("could not convert messages: " + reflect.TypeOf(resp).String())
			}
			MessagesSlice = Messages.Messages
		} else {
			resp, err := c.MessagesSearch(&MessagesSearchParams{
				Peer: &InputPeerChat{
					ChatID: p.ChatID,
				},
				Q:        Opts.Query,
				OffsetID: Opts.Offset,
				Limit:    Opts.Limit,
				Filter:   Opts.Filter,
				MaxDate:  Opts.MaxDate,
				MinDate:  Opts.MinDate,
				MaxID:    Opts.MaxID,
				MinID:    Opts.MinID,
				TopMsgID: Opts.TopMsgID,
			})
			if err != nil {
				return nil, err
			}
			Messages, ok := resp.(*MessagesChannelMessages)
			if !ok {
				return nil, errors.New("could not convert messages: " + reflect.TypeOf(resp).String())
			}
			MessagesSlice = Messages.Messages
		}
	case *InputPeerUser:
		if len(MsgIDs) > 0 {
			resp, err := c.MessagesGetMessages(MsgIDs)
			if err != nil {
				return nil, err
			}
			Messages, ok := resp.(*MessagesMessagesObj)
			if !ok {
				return nil, errors.New("could not convert messages: " + reflect.TypeOf(resp).String())
			}
			MessagesSlice = Messages.Messages
		} else {
			resp, err := c.MessagesSearch(&MessagesSearchParams{
				Peer: &InputPeerUser{
					UserID:     p.UserID,
					AccessHash: p.AccessHash,
				},
				Q:        Opts.Query,
				OffsetID: Opts.Offset,
				Limit:    Opts.Limit,
				Filter:   Opts.Filter,
				MaxDate:  Opts.MaxDate,
				MinDate:  Opts.MinDate,
				MaxID:    Opts.MaxID,
				MinID:    Opts.MinID,
				TopMsgID: Opts.TopMsgID,
			})
			if err != nil {
				return nil, err
			}
			Messages, ok := resp.(*MessagesMessagesObj)
			if !ok {
				return nil, errors.New("could not convert messages: " + reflect.TypeOf(resp).String())
			}
			MessagesSlice = Messages.Messages
		}
	}
	for _, Message := range MessagesSlice {
		Messages = append(Messages, *packMessage(c, Message))
	}
	return Messages, nil
}

func (c *Client) GetDialogs(Opts ...*DialogOptions) ([]Dialog, error) {
	var Options *DialogOptions
	if len(Opts) > 0 {
		Options = Opts[0]
	}
	if Options == nil {
		Options = &DialogOptions{}
	}
	if Options.Limit > 1000 {
		Options.Limit = 1000
	} else if Options.Limit < 1 {
		Options.Limit = 1
	}
	resp, err := c.MessagesGetDialogs(&MessagesGetDialogsParams{
		OffsetDate:    Options.OffsetDate,
		OffsetID:      Options.OffsetID,
		OffsetPeer:    Options.OffsetPeer,
		Limit:         Options.Limit,
		FolderID:      Options.FolderID,
		ExcludePinned: Options.ExcludePinned,
	})
	if err != nil {
		return nil, err
	}
	switch p := resp.(type) {
	case *MessagesDialogsObj:
		go func() { c.Cache.UpdatePeersToCache(p.Users, p.Chats) }()
		return p.Dialogs, nil
	case *MessagesDialogsSlice:
		go func() { c.Cache.UpdatePeersToCache(p.Users, p.Chats) }()
		return p.Dialogs, nil
	default:
		return nil, errors.New("could not convert dialogs: " + reflect.TypeOf(resp).String())
	}
}

func (c *Client) PinMessage(PeerID interface{}, MsgID int32, Opts ...*PinOptions) (Updates, error) {
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return nil, err
	}
	var Options *PinOptions
	if len(Opts) > 0 {
		Options = Opts[0]
	}
	if Options == nil {
		Options = &PinOptions{}
	}
	resp, err := c.MessagesUpdatePinnedMessage(&MessagesUpdatePinnedMessageParams{
		Peer:      PeerToSend,
		ID:        MsgID,
		Unpin:     Options.Unpin,
		PmOneside: Options.PmOneside,
		Silent:    Options.Silent,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) UnPinAll(PeerID interface{}) error {
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return err
	}
	_, err = c.MessagesUnpinAllMessages(PeerToSend)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) GetProfilePhotos(PeerID interface{}, Opts ...*PhotosOptions) ([]Photo, error) {
	PeerToSend, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return nil, err
	}
	var Options *PhotosOptions
	if len(Opts) > 0 {
		Options = Opts[0]
	}
	if Options == nil {
		Options = &PhotosOptions{}
	}
	if Options.Limit > 80 {
		Options.Limit = 80
	} else if Options.Limit < 1 {
		Options.Limit = 1
	}
	User, ok := PeerToSend.(*InputPeerUser)
	if !ok {
		return nil, errors.New("peer is not a user")
	}
	resp, err := c.PhotosGetUserPhotos(
		&InputUserObj{UserID: User.UserID, AccessHash: User.AccessHash},
		Options.Offset,
		Options.MaxID,
		Options.Limit,
	)
	if err != nil {
		return nil, err
	}
	switch p := resp.(type) {
	case *PhotosPhotosObj:
		c.Cache.UpdatePeersToCache(p.Users, []Chat{})
		return p.Photos, nil
	case *PhotosPhotosSlice:
		c.Cache.UpdatePeersToCache(p.Users, []Chat{})
		return p.Photos, nil
	default:
		return nil, errors.New("could not convert photos: " + reflect.TypeOf(resp).String())
	}
}

func (c *Client) GetChatPhotos(ChatID interface{}, limit ...int32) ([]Photo, error) {
	if limit == nil {
		limit = []int32{1}
	}
	messages, err := c.GetMessages(ChatID, &SearchOption{Limit: limit[0],
		Filter: &InputMessagesFilterChatPhotos{}})
	if err != nil {
		return nil, err
	}
	var photos []Photo
	for _, message := range messages {
		if message.IsMedia() {
			switch p := message.Media().(type) {
			case *MessageMediaPhoto:
				photos = append(photos, p.Photo)
			}
		}
	}
	return photos, nil
}

func (c *Client) GetChatPhoto(ChatID interface{}) (Photo, error) {
	photos, err := c.GetChatPhotos(ChatID)
	if err != nil {
		return &PhotoObj{}, err
	}
	if len(photos) > 0 {
		return photos[0], nil
	}
	return &PhotoObj{}, nil
}

func (c *Client) InlineQuery(PeerID interface{}, Options ...*InlineOptions) (*MessagesBotResults, error) {
	var Opts *InlineOptions
	if len(Options) > 0 {
		Opts = Options[0]
	}
	if Opts == nil {
		Opts = &InlineOptions{}
	}
	PeerBot, err := c.GetSendablePeer(PeerID)
	var Peer InputPeer
	if Opts.Dialog != nil {
		Peer, err = c.GetSendablePeer(Opts.Dialog)

	} else {
		Peer = &InputPeerEmpty{}
	}
	if err != nil {
		return nil, err
	}
	var m *MessagesBotResults
	if u, ok := PeerBot.(*InputPeerUser); ok {
		m, err = c.MessagesGetInlineBotResults(&MessagesGetInlineBotResultsParams{
			Bot:      &InputUserObj{UserID: u.UserID, AccessHash: u.AccessHash},
			Peer:     Peer,
			Query:    Opts.Query,
			Offset:   fmt.Sprint(Opts.Offset),
			GeoPoint: Opts.GeoPoint,
		})
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("peer is not a bot")
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (c *Client) JoinChannel(Channel interface{}) error {
	switch p := Channel.(type) {
	case string:
		if TG_JOIN_RE.MatchString(p) {
			_, err := c.MessagesImportChatInvite(TG_JOIN_RE.FindStringSubmatch(p)[2])
			if err != nil {
				return err
			}
		}
	default:
		Channel, err := c.GetSendablePeer(Channel)
		if err != nil {
			return err
		}
		if channel, ok := Channel.(*InputPeerChannel); ok {
			_, err = c.ChannelsJoinChannel(&InputChannelObj{ChannelID: channel.ChannelID, AccessHash: channel.AccessHash})
			if err != nil {
				return err
			}
		} else if channel, ok := Channel.(*InputPeerChat); ok {
			_, err = c.MessagesAddChatUser(channel.ChatID, &InputUserEmpty{}, 0)
			if err != nil {
				return err
			}
		} else {
			return errors.New("peer is not a channel or chat")
		}
	}
	return nil
}
