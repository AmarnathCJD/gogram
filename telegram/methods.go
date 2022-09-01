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
	switch TextObj := TextObj.(type) {
	case string:
		e, Text = c.FormatMessage(TextObj, options.ParseMode)
	}
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	Update, err := c.MessagesEditMessage(&MessagesEditMessageParams{
		Peer:        PeerToSend,
		Message:     Text,
		ID:          MsgID,
		Entities:    e,
		ReplyMarkup: nil,
		NoWebpage:   options.LinkPreview,
	})
	if err != nil {
		return nil, err
	}
	return packMessage(c, processUpdate(Update)), err
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
	fmt.Println(Opts)
	var options MediaOptions
	if len(Opts) > 0 {
		options = *Opts[0]
	}
	if options.ParseMode == "" {
		options.ParseMode = c.ParseMode
	}
	var Caption string
	var e []MessageEntity
	fmt.Println("name", options.FileName)
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
	fmt.Println(MediaFile, Caption)
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
	return packMessage(c, processUpdate(Update)), err
}

func (c *Client) SendReaction(peerID interface{}, MsgID int32, Reaction string, Big ...bool) error {
	var big bool
	if len(Big) > 0 {
		big = Big[0]
	}
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return err
	}
	_, err = c.MessagesSendReaction(
		big,
		PeerToSend,
		MsgID,
		Reaction,
	)
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

func (c *Client) GetParticipant(PeerID interface{}, UserID interface{}) (Participant, error) {
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

func (c *Client) GetMessages(peerID interface{}, IDs []int32) ([]Message, error) {
	// TODO: implement message filters
	return []Message{}, errors.New("not implemented, use raw method")
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
	} else {
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
