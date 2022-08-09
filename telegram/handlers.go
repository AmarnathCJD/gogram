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

var (
	OnNewMessage = "OnNewMessage"
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
	Command struct {
		Cmd    string
		Prefix string
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
	if m.OriginalUpdate.ReplyTo == nil || m.OriginalUpdate.ReplyTo.ReplyToMsgID == 0 {
		return nil, nil
	}
	IDs := []InputMessage{}
	IDs = append(IDs, &InputMessageID{ID: m.OriginalUpdate.ReplyTo.ReplyToMsgID})
	ReplyMsg, err := m.Client.MessagesGetMessages(IDs)
	if err != nil {
		return nil, err
	}
	return PackMessage(m.Client, ReplyMsg.(*MessagesMessagesObj).Messages[0].(*MessageObj)), nil
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

func (m *NewMessage) GetSender() (*UserObj, error) {
	return m.Client.GetPeerUser(m.SenderID())
}

func (m *NewMessage) GetSenderChat() string {
	return "soon will be implemented"
}

func (m *NewMessage) Media() MessageMedia {
	if m.OriginalUpdate.Media == nil {
		return nil
	}
	return m.OriginalUpdate.Media
}

func (m *NewMessage) IsMedia() bool {
	return m.Media() != nil
}

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

func (m *NewMessage) IsCommand() bool {
	for _, p := range m.OriginalUpdate.Entities {
		if _, ok := p.(*MessageEntityBotCommand); ok {
			return true
		}
	}
	return false
}

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

func (m *NewMessage) Delete() error {
	return m.Client.DeleteMessage(m.ChatID(), m.ID)
}

func PackMessage(client *Client, message *MessageObj) *NewMessage {
	var Chat *ChatObj
	var Sender *UserObj
	var SenderChat *ChatObj
	var Channel *Channel
	if message.PeerID != nil {
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
	}
	if message.FromID != nil {
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
	}
	return &NewMessage{
		Client:         client,
		OriginalUpdate: message,
		Chat:           Chat,
		Sender:         Sender,
		SenderChat:     SenderChat,
		Channel:        Channel,
		ID:             message.ID,
	}
}

func HandleMessageUpdate(update Message) {
	if len(MessageHandles) == 0 {
		return
	}
	var msg = update.(*MessageObj)
	for _, handle := range MessageHandles {
		if handle.IsMatch(msg.Message) {
			handle.Handler(handle.Client, PackMessage(handle.Client, msg))
		}
	}
}

func (h *Handle) IsMatch(text string) bool {
	switch Pattern := h.Pattern.(type) {
	case string:
		if Pattern == OnNewMessage {
			return true
		}
		pattern := regexp.MustCompile("^" + Pattern)
		return pattern.MatchString(text) || strings.HasPrefix(text, Pattern)
	case Command:
		Patt := fmt.Sprintf("^[%s]%s", Pattern.Prefix, Pattern.Cmd)
		pattern, err := regexp.Compile(Patt)
		if err != nil {
			return false
		}
		return pattern.MatchString(text)
	default:
		panic("unknown handler type")
	}
}
