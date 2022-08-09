package telegram

import (
	"fmt"
)

type Handle struct {
	Pattern interface{}
	Handler func(c *Client, m *NewMessage) error
	Client  *Client
}

var (
	HANDLERS = []Handle{}
)

func (c *Client) AddEventHandler(pattern interface{}, handler func(c *Client, m *NewMessage) error) {
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
	switch upd := u.(type) {
	case *UpdatesObj:
		cache.UpdatePeersToCache(upd.Users, upd.Chats)
		for _, update := range upd.Updates {
			switch update := update.(type) {
			case *UpdateNewMessage:
				go func() { HandleMessageUpdate(update.Message) }()
			case *UpdateNewChannelMessage:
				go func() { HandleMessageUpdate(update.Message) }()
			}
		}
	case *UpdateShort:
		switch upd := upd.Update.(type) {
		case *UpdateNewMessage:
			go func() { HandleMessageUpdate(upd.Message) }()
		case *UpdateNewChannelMessage:
			go func() { HandleMessageUpdate(upd.Message) }()
		}
	default:
		fmt.Println("unknown update type")
	}

	return true
}
