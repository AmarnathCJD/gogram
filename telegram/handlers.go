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

type NewMessage struct {
	Client         *Client
	OriginalUpdate *MessageObj
	Chat           *ChatObj
	Sender         *UserObj
	SenderChat     *ChatObj
	Channel        *Channel
}

func (m *NewMessage) PeerChat() (*ChatObj, error) {
	switch Peer := m.OriginalUpdate.PeerID.(type) {
	case *PeerUser:
		return m.Client.GetPeerChat(Peer.UserID)
	case *PeerChat:
		return m.Client.GetPeerChat(Peer.ChatID)
	case *PeerChannel:
		return m.Client.GetPeerChat(Peer.ChannelID)
	}
	return nil, fmt.Errorf("failed to resolve peer")
}

func (m *NewMessage) Message() string {
	return m.OriginalUpdate.Message
}

func (m *NewMessage) GetReplyMessage() (*MessageObj, error) {
	if m.OriginalUpdate.ReplyTo == nil || m.OriginalUpdate.ReplyTo.ReplyToMsgID == 0 {
		return nil, nil
	}
	IDs := []InputMessage{}
	IDs = append(IDs, &InputMessageID{ID: m.OriginalUpdate.ReplyTo.ReplyToMsgID})
	ReplyMsg, err := m.Client.MessagesGetMessages(IDs)
	if err != nil {
		return nil, err
	}
	return ReplyMsg.(*MessagesMessagesObj).Messages[0].(*MessageObj), nil
}

func (m *NewMessage) ChatID() int64 {
	switch Peer := m.OriginalUpdate.PeerID.(type) {
	case *PeerUser:
		return Peer.UserID
	case *PeerChat:
		return Peer.ChatID
	case *PeerChannel:
		return Peer.ChannelID
	}
	return 0
}

func (m *NewMessage) SenderID() int64 {
	switch Peer := m.OriginalUpdate.FromID.(type) {
	case *PeerUser:
		return Peer.UserID
	case *PeerChat:
		return Peer.ChatID
	case *PeerChannel:
		return Peer.ChannelID
	default:
		return 0
	}
}

func (m *NewMessage) ChatType() string {
	switch m.OriginalUpdate.PeerID.(type) {
	case *PeerUser:
		return "user"
	case *PeerChat:
		return "chat"
	case *PeerChannel:
		return "channel"
	}
	return ""
}

func (m *NewMessage) IsPrivate() bool {
	return m.ChatType() == "user"
}

func (m *NewMessage) IsGroup() bool {
	return m.ChatType() == "chat"
}

func (m *NewMessage) IsChannel() bool {
	return m.ChatType() == "channel"
}

func (m *NewMessage) IsReply() bool {
	return m.OriginalUpdate.ReplyTo != nil && m.OriginalUpdate.ReplyTo.ReplyToMsgID != 0
}

func (m *NewMessage) Marshal() string {
	b, _ := json.MarshalIndent(m.OriginalUpdate, "", "  ")
	return string(b)
}

func (m *NewMessage) GetChat() (*ChatObj, error) {
	return m.Client.GetPeerChat(m.ChatID())
}

func (m *NewMessage) GetPeer() (int64, int64) {
	if m.IsPrivate() {
		User, _ := m.Client.GetPeerUser(m.SenderID())
		return User.ID, User.AccessHash
	} else if m.IsGroup() {
		Chat, _ := m.Client.GetPeerChat(m.ChatID())
		return Chat.ID, 0
	} else if m.IsChannel() {
		Channel, _ := m.Client.GetPeerChannel(m.ChatID())
		return Channel.ID, Channel.AccessHash
	}
	return 0, 0
}

func (m *NewMessage) GetSender() (*UserObj, error) {
	return m.Client.GetPeerUser(m.SenderID())
}

func (m *NewMessage) GetSenderChat() string {
	return "soon will be implemented"
}

func (m *NewMessage) Reply(text string) error {
	if m.IsPrivate() {
		ID, AcessHash := m.GetPeer()
		fmt.Println(ID, AcessHash)
		return nil
	}
	return fmt.Errorf("soon will be implemented")
}

func packMessage(client *Client, message *MessageObj) *NewMessage {
	var Chat *ChatObj
	var Sender *UserObj
	var SenderChat *ChatObj
	var Channel *Channel
	switch Peer := message.PeerID.(type) {
	case *PeerUser:
		Chat = &ChatObj{
			ID: Peer.UserID,
		}
	case *PeerChat:
		Chat, _ = client.GetPeerChat(Peer.ChatID)
	case *PeerChannel:
		Channel, _ = client.GetPeerChannel(Peer.ChannelID)
		Chat = &ChatObj{
			ID:    Channel.ID,
			Title: Channel.Title,
		}
	}
	switch From := message.FromID.(type) {
	case *PeerUser:
		Sender, _ = client.GetPeerUser(From.UserID)
	case *PeerChat:
		SenderChat, _ = client.GetPeerChat(From.ChatID)
		Sender = &UserObj{
			ID: SenderChat.ID,
		}
	case *PeerChannel:
		Channel, _ = client.GetPeerChannel(From.ChannelID)
		Sender = &UserObj{
			ID: Channel.ID,
		}
	}
	return &NewMessage{
		Client:         client,
		OriginalUpdate: message,
		Chat:           Chat,
		Sender:         Sender,
		SenderChat:     SenderChat,
		Channel:        Channel,
	}
}

func (m *MessageObj) Reply(c *Client, Message string) (*MessageObj, error) {
	peer := m.PeerID
	var update *UpdateShortSentMessage
	var u Updates
	var message *MessageObj
	var err error
	var Entity []MessageEntity
	Message, Entity = c.ParseEntity(Message)
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
				Entities:     Entity,
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
				Entities:     Entity,
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
				Entities:     Entity,
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
	var Entity []MessageEntity
	Message, Entity = c.ParseEntity(Message)
	switch peer := peer.(type) {
	case *PeerUser:
		u, err = c.MessagesEditMessage(&MessagesEditMessageParams{
			NoWebpage:    false,
			Peer:         &InputPeerUser{UserID: peer.UserID},
			ID:           m.ID,
			Message:      Message,
			Media:        nil,
			ReplyMarkup:  nil,
			Entities:     Entity,
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
			Entities:     Entity,
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
			Entities:     Entity,
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
	pattern := regexp.MustCompile("^" + h.Pattern)
	return pattern.MatchString(text) || strings.HasPrefix(text, h.Pattern)
}
