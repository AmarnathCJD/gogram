package telegram

import (
	"encoding/json"
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
	var message *MessageObj
	var err error
	b, _ := json.Marshal(m)
	fmt.Println("Reply:", string(b))
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
		message = &MessageObj{
			ID:     update.ID,
			PeerID: m.PeerID,
		}
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
		switch u := u.(type) {
		case *UpdateShortSentMessage:
			message = &MessageObj{
				ID:     u.ID,
				PeerID: m.PeerID,
			}
		case *UpdatesObj:
			message = u.Updates[0].(*UpdateNewChannelMessage).Message.(*MessageObj)
		}
	case *PeerChannel:
		var Peer, _ = c.GetPeerChannel(peer.ChannelID)
		u, err = c.MessagesSendMessage(
			&MessagesSendMessageParams{
				Peer:         &InputPeerChannel{ChannelID: Peer.ID, AccessHash: Peer.AccessHash},
				ReplyToMsgID: m.ID,
				Message:      Message,
				RandomID:     GenRandInt(),
				ReplyMarkup:  nil,
				Entities:     []MessageEntity{},
				ScheduleDate: 0,
			},
		)
		if err != nil {
			fmt.Println("error", err)
			return nil, err
		}
		switch u := u.(type) {
		case *UpdateShortSentMessage:
			message = &MessageObj{
				ID:     u.ID,
				PeerID: m.PeerID,
			}
		case *UpdatesObj:
			upd := u.Updates[0]
			switch upd := upd.(type) {
			case *UpdateNewChannelMessage:
				message = upd.Message.(*MessageObj)
			case *UpdateMessageID:
				message = &MessageObj{
					ID:     upd.ID,
					PeerID: m.PeerID,
				}
			}
		}
	default:
		return nil, fmt.Errorf("failed to resolve peer")
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
		var Peer, _ = c.GetPeerChannel(peer.ChannelID)
		u, err = c.MessagesEditMessage(&MessagesEditMessageParams{
			NoWebpage:    false,
			Peer:         &InputPeerChannel{ChannelID: Peer.ID, AccessHash: Peer.AccessHash},
			ID:           m.ID,
			Message:      Message,
			Media:        nil,
			ReplyMarkup:  nil,
			Entities:     []MessageEntity{},
			ScheduleDate: 0,
		})
		switch upd := u.(type) {
		case *UpdatesObj:
			upda := upd.Updates[0]
			switch upd := upda.(type) {
			case *UpdateEditMessage:
				update = upd.Message.(*MessageObj)
			case *UpdateEditChannelMessage:
				update = upd.Message.(*MessageObj)
			}
		}
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
