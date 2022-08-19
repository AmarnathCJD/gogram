package telegram

import (
	"encoding/json"
	"fmt"
)

type (
	NewMessage struct {
		Client         *Client
		OriginalUpdate *MessageObj
		Chat           *ChatObj
		Sender         *UserObj
		SenderChat     *ChatObj
		Channel        *Channel
		ID             int32
	}
)

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

func (m *NewMessage) GetReplyMessage() (*NewMessage, error) {
	if !m.IsReply() {
		return nil, fmt.Errorf("message is not a reply")
	}
	if m.IsPrivate() {
		if m.OriginalUpdate.ReplyTo == nil || m.OriginalUpdate.ReplyTo.ReplyToMsgID == 0 {
			return nil, nil
		}
		IDs := []InputMessage{}
		IDs = append(IDs, &InputMessageID{ID: m.OriginalUpdate.ReplyTo.ReplyToMsgID})
		ReplyMsg, err := m.Client.MessagesGetMessages(IDs)
		if err != nil {
			return nil, err
		}
		Message := ReplyMsg.(*MessagesMessagesObj)
		go func() { m.Client.Cache.UpdatePeersToCache(Message.Users, Message.Chats) }()
		switch Msg := Message.Messages[0].(type) {
		case *MessageObj:
			return PackMessage(m.Client, Msg), nil
		case *MessageEmpty:
			return nil, nil
		default:
			return nil, fmt.Errorf("unknown message type")
		}
	} else if m.IsChannel() {
		IDs := []InputMessage{}
		IDs = append(IDs, &InputMessageID{ID: m.OriginalUpdate.ReplyTo.ReplyToMsgID})
		InputPeer, err := m.Client.GetSendablePeer(m.ChatID())
		if err != nil {
			return nil, err
		}
		PeerChannelObject, ok := InputPeer.(*InputPeerChannel)
		if !ok {
			return nil, fmt.Errorf("failed to convert peer to channel")
		}
		ReplyMsg, err := m.Client.ChannelsGetMessages(&InputChannelObj{ChannelID: PeerChannelObject.ChannelID, AccessHash: PeerChannelObject.AccessHash}, IDs)
		if err != nil {
			return nil, err
		}
		Msg := ReplyMsg.(*MessagesChannelMessages)
		switch Msg := Msg.Messages[0].(type) {
		case *MessageObj:
			return PackMessage(m.Client, Msg), nil
		case *MessageEmpty:
			return nil, nil
		}
	}
	return nil, fmt.Errorf("message is not a reply")
}

func (m *NewMessage) ChatID() int64 {
	if m.OriginalUpdate.PeerID != nil {
		switch Peer := m.OriginalUpdate.PeerID.(type) {
		case *PeerUser:
			return Peer.UserID
		case *PeerChat:
			return Peer.ChatID
		case *PeerChannel:
			return Peer.ChannelID
		}
	}
	return 0
}

func (m *NewMessage) SenderID() int64 {
	if m.IsPrivate() {
		return m.ChatID()
	}
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

// GetPeer returns the peer of the message
func (m *NewMessage) GetPeer() (int64, int64) {
	if m.IsPrivate() {
		User, _ := m.Client.GetPeerUser(m.ChatID())
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

// GetSender returns the sender of the message
func (m *NewMessage) GetSender() (*UserObj, error) {
	return m.Client.GetPeerUser(m.SenderID())
}

func (m *NewMessage) GetSenderChat() string {
	return "soon will be implemented"
}

// Media is a media object in a message
func (m *NewMessage) Media() MessageMedia {
	if m.OriginalUpdate.Media == nil {
		return nil
	}
	return m.OriginalUpdate.Media
}

// IsMedia returns true if message contains media
func (m *NewMessage) IsMedia() bool {
	return m.Media() != nil
}

// MediaType returns the type of the media in the message.
func (m *NewMessage) MediaType() string {
	Media := m.Media()
	if Media == nil {
		return ""
	}
	switch Media.(type) {
	case *MessageMediaPhoto:
		return "photo"
	case *MessageMediaDocument:
		return "document"
	case *MessageMediaVenue:
		return "venue"
	case *MessageMediaContact:
		return "contact"
	case *MessageMediaGeo:
		return "geo"
	case *MessageMediaGame:
		return "game"
	case *MessageMediaInvoice:
		return "invoice"
	case *MessageMediaGeoLive:
		return "geo_live"
	case *MessageMediaUnsupported:
		return "unsupported"
	case *MessageMediaWebPage:
		return "web_page"
	case *MessageMediaDice:
		return "dice"
	default:
		return "unknown"
	}
}

// IsCommand returns true if the message is a command
func (m *NewMessage) IsCommand() bool {
	for _, p := range m.OriginalUpdate.Entities {
		if _, ok := p.(*MessageEntityBotCommand); ok {
			return true
		}
	}
	return false
}

// GetCommand returns the command from the message.
// If the message is not a command, it returns an empty string.
func (m *NewMessage) GetCommand() string {
	for _, p := range m.OriginalUpdate.Entities {
		if _, ok := p.(*MessageEntityBotCommand); ok && m.Text() != "" {
			Text := m.Text()
			p := p.(*MessageEntityBotCommand)
			return Text[p.Offset : p.Offset+p.Length]
		}
	}
	return ""
}

// Client.SendMessage ReplyID set to messageID
func (m *NewMessage) Reply(Text interface{}, Opts ...SendOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, SendOptions{ReplyID: m.ID})
	} else {
		Opts[0].ReplyID = m.ID
	}
	resp, err := m.Client.SendMessage(m.ChatID(), Text, &Opts[0])
	if resp == nil {
		return nil, err
	}
	r := *resp
	r.PeerID = m.OriginalUpdate.PeerID
	return &NewMessage{Client: m.Client, OriginalUpdate: &r, ID: resp.ID}, err
}

func (m *NewMessage) Respond(Text interface{}, Opts ...SendOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, SendOptions{})
	}
	resp, err := m.Client.SendMessage(m.ChatID(), Text, &Opts[0])
	if resp == nil {
		return nil, err
	}
	r := *resp
	r.PeerID = m.OriginalUpdate.PeerID
	return &NewMessage{Client: m.Client, OriginalUpdate: &r, ID: resp.ID}, err
}

func (m *NewMessage) Edit(Text interface{}, Opts ...SendOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, SendOptions{})
	}
	resp, err := m.Client.EditMessage(m.ChatID(), m.ID, Text, &Opts[0])
	if resp == nil {
		return nil, err
	}
	r := *resp
	r.PeerID = m.OriginalUpdate.PeerID
	return &NewMessage{Client: m.Client, OriginalUpdate: &r, ID: resp.ID}, err
}

// Delete deletes the message
func (m *NewMessage) Delete() error {
	return m.Client.DeleteMessage(m.ChatID(), m.ID)
}

// React to a message
func (m *NewMessage) React(Reaction string) error {
	return m.Client.SendReaction(m.ChatID(), m.ID, Reaction)
}

// Forward forwards the message to a chat
func (m *NewMessage) ForwardTo(ChatID int64, Opts ...ForwardOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, ForwardOptions{})
	}
	resp, err := m.Client.ForwardMessage(m.ChatID(), ChatID, []int32{m.ID}, &Opts[0])
	if resp == nil {
		return nil, err
	}
	r := *resp
	r.PeerID = m.OriginalUpdate.PeerID
	return &NewMessage{Client: m.Client, OriginalUpdate: &r, ID: resp.ID}, err
}
