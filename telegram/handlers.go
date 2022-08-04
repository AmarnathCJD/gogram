package telegram

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	MessageHandles = []Handle{}
)

func (m *MessageObj) Reply(c *Client, Message string) (*MessageObj, error) {
	peer := m.PeerID
	var update *UpdateShortSentMessage
	var u Updates
	var err error
	switch peer := peer.(type) {
	case *PeerUser:
		fmt.Println("replying to user", peer)
		u, err = c.MessagesSendMessage(
			&MessagesSendMessageParams{
				Peer:         &InputPeerUser{UserID: peer.UserID},
				ReplyToMsgID: m.ID,
				Message:      Message,
				RandomID:     GenRandInt(),
				ReplyMarkup:  nil,
				Entities:     []MessageEntity{},
				ScheduleDate: 0,
			},
		)
		update = u.(*UpdateShortSentMessage)
	case *PeerChat:
		u, err = c.MessagesSendMessage(
			&MessagesSendMessageParams{
				Peer:         &InputPeerUser{UserID: peer.ChatID},
				ReplyToMsgID: m.ID,
				Message:      Message,
				RandomID:     GenRandInt(),
				ReplyMarkup:  nil,
				Entities:     []MessageEntity{},
				ScheduleDate: 0,
			},
		)
		update = u.(*UpdateShortSentMessage)
	case *PeerChannel:
		fmt.Println("channel msg", peer)
		u, err = c.MessagesSendMessage(
			&MessagesSendMessageParams{
				Peer:         &InputPeerUser{UserID: peer.ChannelID},
				ReplyToMsgID: m.ID,
				Message:      Message,
				RandomID:     GenRandInt(),
				ReplyMarkup:  nil,
				Entities:     []MessageEntity{},
				ScheduleDate: 0,
			},
		)
		update = u.(*UpdateShortSentMessage)
	default:
		return nil, fmt.Errorf("failed to resolve peer")
	}
	message := &MessageObj{
		ID:     update.ID,
		PeerID: m.PeerID,
	}
	return message, err
}

func (m *MessageObj) Edit(c *Client, Message string) (*MessageObj, error) {
	peer := m.PeerID
	var update *MessageObj
	var u Updates
	var err error
	switch peer := peer.(type) {
	case *PeerUser:
		fmt.Println("replying to user", peer)
		u, err = c.MessagesEditMessage(&MessagesEditMessageParams{
			NoWebpage:    false,
			Peer:         &InputPeerUser{UserID: peer.UserID},
			ID:           m.ID,
			Message:      Message,
			Media:        nil,
			ReplyMarkup:  nil,
			Entities:     []MessageEntity{},
			ScheduleDate: 0,
		})
		update = u.(*UpdatesObj).Updates[0].(*UpdateEditMessage).Message.(*MessageObj)
	case *PeerChat:
		u, err = c.MessagesEditMessage(&MessagesEditMessageParams{
			NoWebpage:    false,
			Peer:         &InputPeerChat{ChatID: peer.ChatID},
			ID:           m.ID,
			Message:      Message,
			Media:        nil,
			ReplyMarkup:  nil,
			Entities:     []MessageEntity{},
			ScheduleDate: 0,
		})
		update = u.(*UpdatesObj).Updates[0].(*UpdateEditMessage).Message.(*MessageObj)
	case *PeerChannel:
		fmt.Println("channel msg", peer)
		u, err = c.MessagesEditMessage(&MessagesEditMessageParams{
			NoWebpage:    false,
			Peer:         &InputPeerChannel{ChannelID: peer.ChannelID},
			ID:           m.ID,
			Message:      Message,
			Media:        nil,
			ReplyMarkup:  nil,
			Entities:     []MessageEntity{},
			ScheduleDate: 0,
		})
		update = u.(*UpdatesObj).Updates[0].(*UpdateEditMessage).Message.(*MessageObj)
	default:
		return nil, fmt.Errorf("failed to resolve peer")
	}
	return update, err

}

func HandleMessageUpdate(update Message) {
	if len(MessageHandles) == 0 {
		return
	}
	var msg = update.(*MessageObj)
	for _, handle := range MessageHandles {
		if handle.IsMatch(msg.Message) {
			fmt.Println("HandleMessageUpdate:", msg.Message)
			handle.Handler(handle.Client, msg)
		}
	}
}

func (h *Handle) IsMatch(text string) bool {
	pattern := regexp.MustCompile(h.Pattern)
	return pattern.MatchString(text) || strings.HasPrefix(text, h.Pattern)
}
