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

func (c *Client) ResolveUsername(username string) (interface{}, error) {
	resp, err := c.ContactsResolveUsername(username)
	if err != nil {
		return nil, errors.Wrap(err, "resolving username")
	}
	if len(resp.Users) != 0 {
		return resp.Users[0].(*UserObj), nil
	} else if len(resp.Chats) != 0 {
		switch Peer := resp.Chats[0].(type) {
		case *ChatObj:
			return Peer, nil
		case *Channel:
			return Peer, nil
		default:
			return nil, fmt.Errorf("got wrong response: %s", reflect.TypeOf(resp).String())
		}
	} else {
		return nil, fmt.Errorf("no user or chat has username %s", username)
	}
}

func (c *Client) GetPeerID(peer interface{}) (int64, error) {
	switch Peer := peer.(type) {
	case *InputPeerChat:
		return Peer.ChatID, nil
	case *InputPeerChannel:
		return Peer.ChannelID, nil
	case *InputPeerUser:
		return Peer.UserID, nil
	case *ChatObj:
		return Peer.ID, nil
	case *Channel:
		return Peer.ID, nil
	case *UserObj:
		return Peer.ID, nil
	case int64:
		return Peer, nil
	default:
		return 0, errors.New("unknown peer type")
	}
}

type SendOptions struct {
	ReplyID     int32
	ParseMode   string
	Silent      bool
	LinkPreview bool
}

func (c *Client) SendMessage(peerID interface{}, Text string, Opts ...*SendOptions) (*MessageObj, error) {
	var options SendOptions
	var update Updates
	if len(Opts) > 0 {
		options = *Opts[0]
	}
	if options.ParseMode == "" {
		options.ParseMode = c.ParseMode
	}
	var e []MessageEntity
	Text, e = c.ParseEntity(Text, options.ParseMode)
	var err error
	switch Peer := peerID.(type) {
	case *InputPeer:
		PeerID, errs := c.GetPeerID(Peer)
		if errs != nil {
			return nil, errors.Wrap(errs, "getting peer id")
		}
		if ResolvedPeer := c.GetInputPeer(PeerID); ResolvedPeer != nil {
			update, err = c.MessagesSendMessage(&MessagesSendMessageParams{
				Peer:         c.GetInputPeer(PeerID),
				Message:      Text,
				RandomID:     GenRandInt(),
				ReplyToMsgID: options.ReplyID,
				Entities:     e,
				ReplyMarkup:  nil,
				NoWebpage:    options.LinkPreview,
				Silent:       options.Silent,
			})
		} else {
			return nil, errors.New("peer not resolved")
		}
	case string:
		entity, errs := c.ResolveUsername(Peer)
		if errs != nil {
			return nil, errs
		}
		switch entity := entity.(type) {
		case *UserObj:
			update, err = c.MessagesSendMessage(&MessagesSendMessageParams{
				Peer:         &InputPeerUser{UserID: entity.ID, AccessHash: entity.AccessHash},
				Message:      Text,
				RandomID:     GenRandInt(),
				ReplyToMsgID: options.ReplyID,
				Entities:     e,
				ReplyMarkup:  nil,
				NoWebpage:    options.LinkPreview,
				Silent:       options.Silent,
			})
		case *ChatObj:
			update, err = c.MessagesSendMessage(&MessagesSendMessageParams{
				Peer:         &InputPeerChat{ChatID: entity.ID},
				Message:      Text,
				RandomID:     GenRandInt(),
				ReplyToMsgID: options.ReplyID,
				Entities:     e,
				ReplyMarkup:  nil,
				NoWebpage:    options.LinkPreview,
				Silent:       options.Silent,
			})
		case *Channel:
			update, err = c.MessagesSendMessage(&MessagesSendMessageParams{
				Peer:         &InputPeerChannel{ChannelID: entity.ID, AccessHash: entity.AccessHash},
				Message:      Text,
				RandomID:     GenRandInt(),
				ReplyToMsgID: options.ReplyID,
				Entities:     e,
				ReplyMarkup:  nil,
				NoWebpage:    options.LinkPreview,
				Silent:       options.Silent,
			})
		}
	case int64:
		if ResolvedPeer := c.GetInputPeer(Peer); ResolvedPeer != nil {
			update, err = c.MessagesSendMessage(&MessagesSendMessageParams{
				Peer:         ResolvedPeer,
				Message:      Text,
				RandomID:     GenRandInt(),
				ReplyToMsgID: options.ReplyID,
				Entities:     e,
				ReplyMarkup:  nil,
				NoWebpage:    options.LinkPreview,
				Silent:       options.Silent,
			})
		} else {
			return nil, errors.New("peer not resolved")
		}
	case int32:
		if ResolvedPeer := c.GetInputPeer(int64(Peer)); ResolvedPeer != nil {
			update, err = c.MessagesSendMessage(&MessagesSendMessageParams{
				Peer:         ResolvedPeer,
				Message:      Text,
				RandomID:     GenRandInt(),
				ReplyToMsgID: options.ReplyID,
				Entities:     e,
				ReplyMarkup:  nil,
				NoWebpage:    options.LinkPreview,
				Silent:       options.Silent,
			})
		} else {
			return nil, errors.New("peer not resolved")
		}
	case int:
		if ResolvedPeer := c.GetInputPeer(int64(Peer)); ResolvedPeer != nil {
			update, err = c.MessagesSendMessage(&MessagesSendMessageParams{
				Peer:         ResolvedPeer,
				Message:      Text,
				RandomID:     GenRandInt(),
				ReplyToMsgID: options.ReplyID,
				Entities:     e,
				ReplyMarkup:  nil,
				NoWebpage:    options.LinkPreview,
				Silent:       options.Silent,
			})
		} else {
			return nil, errors.New("peer not resolved")
		}
	}
	return ProcessMessageUpdate(update), err
}

func (c *Client) EditMessage(peerID interface{}, MsgID int32, Text string, Opts ...*SendOptions) (*MessageObj, error) {
	var options SendOptions
	fmt.Println("edit message to", peerID)
	var update Updates
	if len(Opts) > 0 {
		options = *Opts[0]
	}
	if options.ParseMode == "" {
		options.ParseMode = c.ParseMode
	}
	var err error
	var e []MessageEntity
	Text, e = c.ParseEntity(Text, options.ParseMode)
	switch Peer := peerID.(type) {
	case *InputPeer:
		PeerID, errs := c.GetPeerID(Peer)
		if errs != nil {
			return nil, errors.Wrap(errs, "getting peer id")
		}
		if ResolvedPeer := c.GetInputPeer(PeerID); ResolvedPeer != nil {
			update, err = c.MessagesEditMessage(&MessagesEditMessageParams{
				Peer:        c.GetInputPeer(PeerID),
				Message:     Text,
				ID:          MsgID,
				Entities:    e,
				ReplyMarkup: nil,
				NoWebpage:   options.LinkPreview,
			})
		} else {
			return nil, errors.New("peer not resolved")
		}
	case string:
		entity, errs := c.ResolveUsername(Peer)
		if errs != nil {
			return nil, errs
		}
		switch entity := entity.(type) {
		case *UserObj:
			update, err = c.MessagesEditMessage(&MessagesEditMessageParams{
				Peer:        &InputPeerUser{UserID: entity.ID, AccessHash: entity.AccessHash},
				Message:     Text,
				ID:          MsgID,
				Entities:    e,
				ReplyMarkup: nil,
				NoWebpage:   options.LinkPreview,
			})
		case *ChatObj:
			update, err = c.MessagesEditMessage(&MessagesEditMessageParams{
				Peer:        &InputPeerChat{ChatID: entity.ID},
				Message:     Text,
				ID:          MsgID,
				Entities:    e,
				ReplyMarkup: nil,
				NoWebpage:   options.LinkPreview,
			})
		case *Channel:
			update, err = c.MessagesEditMessage(&MessagesEditMessageParams{
				Peer:        &InputPeerChannel{ChannelID: entity.ID, AccessHash: entity.AccessHash},
				Message:     Text,
				ID:          MsgID,
				Entities:    e,
				ReplyMarkup: nil,
				NoWebpage:   options.LinkPreview,
			})
		}
	case int64:
		if ResolvedPeer := c.GetInputPeer(Peer); ResolvedPeer != nil {
			update, err = c.MessagesEditMessage(&MessagesEditMessageParams{
				Peer:        ResolvedPeer,
				Message:     Text,
				ID:          MsgID,
				Entities:    e,
				ReplyMarkup: nil,
				NoWebpage:   options.LinkPreview,
			})
		} else {
			return nil, errors.New("peer not resolved")
		}
	case int32:
		if ResolvedPeer := c.GetInputPeer(int64(Peer)); ResolvedPeer != nil {
			update, err = c.MessagesEditMessage(&MessagesEditMessageParams{
				Peer:        ResolvedPeer,
				Message:     Text,
				ID:          MsgID,
				Entities:    e,
				ReplyMarkup: nil,
				NoWebpage:   options.LinkPreview,
			})
		} else {
			return nil, errors.New("peer not resolved")
		}
	case int:
		if ResolvedPeer := c.GetInputPeer(int64(Peer)); ResolvedPeer != nil {
			update, err = c.MessagesEditMessage(&MessagesEditMessageParams{
				Peer:        ResolvedPeer,
				Message:     Text,
				ID:          MsgID,
				Entities:    e,
				ReplyMarkup: nil,
				NoWebpage:   options.LinkPreview,
			})
		} else {
			return nil, errors.New("peer not resolved")
		}
	}
	return ProcessMessageUpdate(update), err
}

func ProcessMessageUpdate(update Updates) *MessageObj {
	if update == nil {
		return nil
	}
	switch update := update.(type) {
	case *UpdateShortSentMessage:
		return &MessageObj{
			ID:        update.ID,
			PeerID:    &PeerUser{},
			Date:      update.Date,
			Out:       update.Out,
			Media:     update.Media,
			Entities:  update.Entities,
			TtlPeriod: update.TtlPeriod,
		}
	case *UpdatesObj:
		upd := update.Updates[0]
		if len(update.Updates) == 2 {
			upd = update.Updates[1]
		}
		switch upd := upd.(type) {
		case *UpdateNewMessage:
			return upd.Message.(*MessageObj)
		case *UpdateNewChannelMessage:
			return upd.Message.(*MessageObj)
		case *UpdateEditMessage:
			return upd.Message.(*MessageObj)
		case *UpdateEditChannelMessage:
			return upd.Message.(*MessageObj)
		case *UpdateMessageID:
			return &MessageObj{
				ID: upd.ID,
			}
		default:
			fmt.Println("unknown update typex", reflect.TypeOf(upd).String())
		}
	}
	return nil
}
