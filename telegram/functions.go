package telegram

import (
	"fmt"
)

func (c *Client) GetUser(peer any) (*UserObj, error) {
	switch peer := peer.(type) {
	case string:
		resp, err := c.ContactsResolveUsername(peer)
		if err != nil {
			return nil, err
		}
		if len(resp.Users) != 0 {
			return resp.Users[0].(*UserObj), nil
		} else {
			return nil, fmt.Errorf("no user has username %s", peer)
		}
	case int64:
		resp, err := c.UsersGetUsers([]InputUser{&InputUserObj{UserID: int32(peer)}})
		if err != nil {
			return nil, err
		}
		if len(resp) != 0 {
			return resp[0].(*UserObj), nil
		} else {
			return nil, fmt.Errorf("no user has id %d", peer)
		}
	}
	return nil, fmt.Errorf("unknown peer type")
}

func (c *Client) GetChat(peer any) (*ChatObj, error, string) {
	switch peer := peer.(type) {
	case string:
		resp, err := c.ContactsResolveUsername(peer)
		if err != nil {
			return nil, err, ""
		}
		if len(resp.Chats) != 0 {
			return resp.Chats[0].(*ChatObj), nil, ""
		} else {
			return nil, fmt.Errorf("no chat has username %s", peer), ""
		}
	case int64:
		return nil, fmt.Errorf("soon to be implemented"), ""

	}
	return nil, fmt.Errorf("unknown peer type"), ""
}

func (c *Client) ResolvePeer(peer any) (*UserObj, *ChatObj, *Channel, error) {
	switch peer := peer.(type) {
	case string:
		resp, err := c.ContactsResolveUsername(peer)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(resp.Users) != 0 {
			return resp.Users[0].(*UserObj), nil, nil, nil
		} else if len(resp.Chats) != 0 {
			chat := resp.Chats[0]
			switch chat := chat.(type) {
			case *ChatObj:
				return nil, chat, nil, nil
			case *Channel:
				return nil, nil, chat, nil
			}
		} else {
			return nil, nil, nil, fmt.Errorf("no user or chat has username %s", peer)
		}
	case int:
		if peer < 0 {
			return nil, nil, nil, fmt.Errorf("soon to be implemented")
		} else {
			resp, err := c.UsersGetUsers([]InputUser{&InputUserObj{UserID: int32(peer)}})
			if err != nil {
				return nil, nil, nil, err
			}
			if len(resp) != 0 {
				return resp[0].(*UserObj), nil, nil, nil
			} else {
				return nil, nil, nil, fmt.Errorf("no user has id %d", peer)
			}
		}
	}
	return nil, nil, nil, fmt.Errorf("unknown peer type")
}

func (c *Client) Respond(Peer any, Message string, ReplyTo ...int32) (Updates, error) {
	user, chat, channel, err := c.ResolvePeer(Peer)
	var ReplyToMsg int32 = 0
	if len(ReplyTo) != 0 {
		ReplyToMsg = ReplyTo[0]
	}
	if user != nil {
		return c.MessagesSendMessage(
			&MessagesSendMessageParams{
				Peer:         &InputPeerUser{UserID: user.ID, AccessHash: user.AccessHash},
				ReplyToMsgID: ReplyToMsg,
				Message:      Message,
				RandomID:     GenRandInt(),
				ReplyMarkup:  nil,
				Entities:     []MessageEntity{},
				ScheduleDate: 0,
			},
		)
	} else if chat != nil {
		return c.MessagesSendMessage(
			&MessagesSendMessageParams{
				Peer:         &InputPeerChat{ChatID: chat.ID},
				ReplyToMsgID: ReplyToMsg,
				Message:      Message,
				RandomID:     GenRandInt(),
				ReplyMarkup:  nil,
				Entities:     []MessageEntity{},
				ScheduleDate: 0,
			},
		)
	} else if channel != nil {
		return c.MessagesSendMessage(
			&MessagesSendMessageParams{
				Peer:         &InputPeerChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash},
				ReplyToMsgID: ReplyToMsg,
				Message:      Message,
				RandomID:     GenRandInt(),
				ReplyMarkup:  nil,
				Entities:     []MessageEntity{},
				ScheduleDate: 0,
			},
		)
	} else if err != nil {
		return nil, err
	} else {
		return nil, fmt.Errorf("failed to resolve peer")
	}
}

type Handle struct {
	Pattern string
	Handler func(c *Client, m *MessageObj) error
	Client  *Client
}

var (
	HANDLERS = []Handle{}
)

func (c *Client) AddEventHandler(pattern string, handler func(c *Client, m *MessageObj) error) {
	MessageHandles = append(MessageHandles, Handle{pattern, handler, c})
}

func (c *Client) RemoveEventHandler(pattern string) {
	for i, p := range HANDLERS {
		if p.Pattern == pattern {
			HANDLERS = append(HANDLERS[:i], HANDLERS[i+1:]...)
			return
		}
	}
}

func HandleUpdate(u interface{}) bool {
	upd := u.(*UpdatesObj).Updates
	for _, update := range upd {
		switch update := update.(type) {
		case *UpdateNewMessage:
			go func() { HandleMessageUpdate(update.Message) }()
		}
	}
	return true
}
