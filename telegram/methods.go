package telegram

import (
	"fmt"
	"reflect"

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

func (c *Client) SendMessage(peerID interface{}, TextObj interface{}, Opts ...*SendOptions) (*MessageObj, error) {
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
		Text, e = c.ParseEntity(TextObj, options.ParseMode)
	case *Entity:
		Text = TextObj.GetText()
		e = TextObj.Entities()
	case MessageMedia:
		return c.SendMedia(peerID, TextObj, Opts[0])
	case InputMedia:
		return c.SendMedia(peerID, TextObj, Opts[0])
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
		return nil, err
	}
	return ProcessMessageUpdate(Update), err
}

func (c *Client) EditMessage(peerID interface{}, MsgID int32, TextObj interface{}, Opts ...*SendOptions) (*MessageObj, error) {
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
		Text, e = c.ParseEntity(TextObj, options.ParseMode)
	case *Entity:
		Text = TextObj.GetText()
		e = TextObj.Entities()
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
	return ProcessMessageUpdate(Update), err
}

func (c *Client) DeleteMessage(peerID interface{}, MsgIDs ...int32) error {
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return err
	}
	PeerChannel, ok := PeerToSend.(*InputPeerChannel)
	if !ok {
		return errors.New("peer is not a channel")
	}
	_, err = c.ChannelsDeleteMessages(&InputChannelObj{ChannelID: PeerChannel.ChannelID, AccessHash: PeerChannel.AccessHash}, MsgIDs)
	return err
}

func (c *Client) ForwardMessage(fromID interface{}, toID interface{}, MsgIDs []int32, Opts ...*ForwardOptions) (*MessageObj, error) {
	var options ForwardOptions
	FromPeer, err := c.GetSendablePeer(fromID)
	if err != nil {
		return nil, err
	}
	ToPeer, err := c.GetSendablePeer(toID)
	if err != nil {
		return nil, err
	}
	resp, err := c.MessagesForwardMessages(&MessagesForwardMessagesParams{
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
	return ProcessMessageUpdate(resp), nil
}

func (c *Client) SendMedia(peerID interface{}, Media interface{}, Opts ...*SendOptions) (*MessageObj, error) {
	var options SendOptions
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
		Caption, e = c.ParseEntity(Capt, options.ParseMode)
	case *Entity:
		Caption = Capt.GetText()
		e = Capt.Entities()
	}
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	MediaFile, err := c.GetSendableMedia(Media)
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
	fmt.Println("type", reflect.TypeOf(Update))
	return nil, err
}

func (c *Client) SendReaction(peerID interface{}, MsgID int32, Reaction string) error {
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return err
	}
	_, err = c.MessagesSendReaction(
		true,
		PeerToSend,
		MsgID,
		Reaction,
	)
	return err
}

func (c *Client) GetParticipants(PeerID interface{}, limit int32) ([]Participant, int32, error) {
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
		&ChannelParticipantsSearch{},
		0,
		limit,
		0,
	)
	if err != nil {
		return nil, 0, err
	}
	ParticipantsResponse := p.(*ChannelsChannelParticipantsObj)
	c.Cache.UpdatePeersToCache(ParticipantsResponse.Users, ParticipantsResponse.Chats)
	var Participants []Participant
	for _, u := range ParticipantsResponse.Participants {
		switch u := u.(type) {
		case *ChannelParticipantAdmin:
			User, _ := c.Cache.GetUser(u.UserID)
			Participants = append(Participants, Participant{
				User:        User,
				Participant: u,
				Admin:       true,
				Banned:      false,
				Creator:     false,
				Left:        false,
			})
		case *ChannelParticipantBanned:
			User, _ := c.Cache.GetUser(c.GetPeerID(u.Peer))
			Participants = append(Participants, Participant{
				User:        User,
				Participant: u,
				Admin:       false,
				Banned:      true,
				Creator:     false,
				Left:        u.Left,
			})
		case *ChannelParticipantCreator:
			User, _ := c.Cache.GetUser(u.UserID)
			Participants = append(Participants, Participant{
				User:        User,
				Participant: u,
				Admin:       true,
				Banned:      false,
				Creator:     true,
				Left:        false,
			})
		case *ChannelParticipantLeft:
			User, _ := c.Cache.GetUser(c.GetPeerID(u.Peer))
			Participants = append(Participants, Participant{
				User:        User,
				Participant: u,
				Admin:       false,
				Banned:      false,
				Creator:     false,
				Left:        true,
			})
		case *ChannelParticipantObj:
			User, _ := c.Cache.GetUser(u.UserID)
			Participants = append(Participants, Participant{
				User:        User,
				Participant: u,
				Admin:       false,
				Banned:      false,
				Creator:     false,
				Left:        false,
			})
		default:
			fmt.Println("unknown participant type", reflect.TypeOf(u).String())
		}

	}
	return Participants, ParticipantsResponse.Count, nil
}
