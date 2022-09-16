package telegram

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

type (
	NewMessage struct {
		Client         *Client
		OriginalUpdate Message
		Chat           *ChatObj
		Sender         *UserObj
		SenderChat     *Channel
		Channel        *Channel
		ID             int32
		Action         MessageAction
		Message        *MessageObj
		Peer           InputPeer
	}
)

func (m *NewMessage) PeerChat() (*ChatObj, error) {
	switch Peer := m.Message.PeerID.(type) {
	case *PeerUser:
		return m.Client.GetPeerChat(Peer.UserID)
	case *PeerChat:
		return m.Client.GetPeerChat(Peer.ChatID)
	case *PeerChannel:
		return m.Client.GetPeerChat(Peer.ChannelID)
	}
	return nil, fmt.Errorf("failed to resolve peer")
}

func (m *NewMessage) MessageText() string {
	return m.Message.Message
}

func (m *NewMessage) ReplyToMsgID() int32 {
	if m.Message.ReplyTo != nil {
		return m.Message.ReplyTo.ReplyToMsgID
	}
	return 0
}

func (m *NewMessage) GetReplyMessage() (*NewMessage, error) {
	if !m.IsReply() {
		return nil, errors.New("message is not a reply")
	}
	messages, err := m.Client.GetMessages(m.ChatID(), &SearchOption{IDs: []int32{m.ReplyToMsgID()}})
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, errors.New("message not found")
	}
	return &messages[0], nil
}

func (m *NewMessage) ChatID() int64 {
	if m.Message.PeerID != nil {
		switch Peer := m.Message.PeerID.(type) {
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
	switch Peer := m.Message.FromID.(type) {
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
	if m.Message != nil && m.Message.PeerID != nil {
		switch m.Message.PeerID.(type) {
		case *PeerUser:
			return EntityUser
		case *PeerChat:
			return EntityChat
		case *PeerChannel:
			return EntityChannel
		}
	}
	return EntityUnknown
}

func (m *NewMessage) IsPrivate() bool {
	return m.ChatType() == EntityUser
}

func (m *NewMessage) IsGroup() bool {
	if m.Channel != nil {
		return m.ChatType() == EntityChat || (m.ChatType() == EntityChannel && !m.Channel.Broadcast)
	}
	return m.ChatType() == EntityChat
}

func (m *NewMessage) ReplyMarkup() *ReplyMarkup {
	return &m.Message.ReplyMarkup
}

func (m *NewMessage) IsChannel() bool {
	return m.ChatType() == EntityChannel
}

func (m *NewMessage) IsReply() bool {
	return m.Message.ReplyTo != nil && m.Message.ReplyTo.ReplyToMsgID != 0
}

func (m *NewMessage) Marshal() string {
	b, _ := json.MarshalIndent(m.Message, "", "  ")
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
	if m.Message.Media == nil {
		return nil
	}
	return m.Message.Media
}

// IsMedia returns true if message contains media
func (m *NewMessage) IsMedia() bool {
	return m.Media() != nil
}

func (m *NewMessage) Sticker() *DocumentObj {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaDocument); ok {
			if doc, ok := m.Document.(*DocumentObj); ok {
				for _, attr := range doc.Attributes {
					if _, ok := attr.(*DocumentAttributeSticker); ok {
						return doc
					}
					if f, ok := attr.(*DocumentAttributeFilename); ok {
						if strings.HasSuffix(f.FileName, ".tgs") || strings.HasSuffix(f.FileName, ".webp") {
							return doc
						}
					}
				}
			}
		}
	}
	return nil
}

func (m *NewMessage) Photo() *PhotoObj {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaPhoto); ok {
			if photo, ok := m.Photo.(*PhotoObj); ok {
				return photo
			}
		}
	}
	return nil
}

func (m *NewMessage) Document() *DocumentObj {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaDocument); ok {
			if doc, ok := m.Document.(*DocumentObj); ok {
				return doc
			}
		}
	}
	return nil
}

func (m *NewMessage) Video() *DocumentObj {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaDocument); ok {
			if doc, ok := m.Document.(*DocumentObj); ok {
				for _, attr := range doc.Attributes {
					if _, ok := attr.(*DocumentAttributeVideo); ok {
						return doc
					}
				}
			}
		}
	}
	return nil
}

func (m *NewMessage) Audio() *DocumentObj {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaDocument); ok {
			if doc, ok := m.Document.(*DocumentObj); ok {
				for _, attr := range doc.Attributes {
					if _, ok := attr.(*DocumentAttributeAudio); ok {
						return doc
					}
				}
			}
		}
	}
	return nil
}

func (m *NewMessage) Voice() *DocumentObj {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaDocument); ok {
			if doc, ok := m.Document.(*DocumentObj); ok {
				for _, attr := range doc.Attributes {
					if _, ok := attr.(*DocumentAttributeAudio); ok {
						return doc
					}
				}
			}
		}
	}
	return nil
}

func (m *NewMessage) Animation() *DocumentObj {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaDocument); ok {
			if doc, ok := m.Document.(*DocumentObj); ok {
				for _, attr := range doc.Attributes {
					if _, ok := attr.(*DocumentAttributeAnimated); ok {
						return doc
					}
				}
			}
		}
	}
	return nil
}

func (m *NewMessage) Geo() *GeoPointObj {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaGeo); ok {
			if geo, ok := m.Geo.(*GeoPointObj); ok {
				return geo
			}
		}
	}
	return nil
}

func (m *NewMessage) Contact() *MessageMediaContact {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaContact); ok {
			return m
		}
	}
	return nil
}

func (m *NewMessage) Game() *MessageMediaGame {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaGame); ok {
			return m
		}
	}
	return nil
}

func (m *NewMessage) Invoice() *MessageMediaInvoice {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaInvoice); ok {
			return m
		}
	}
	return nil
}

func (m *NewMessage) WebPage() *WebPageObj {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaWebPage); ok {
			if page, ok := m.Webpage.(*WebPageObj); ok {
				return page
			}
		}
	}
	return nil
}

func (m *NewMessage) Poll() *MessageMediaPoll {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaPoll); ok {
			return m
		}
	}
	return nil
}

func (m *NewMessage) Venue() *MessageMediaVenue {
	if m.IsMedia() {
		if m, ok := m.Media().(*MessageMediaVenue); ok {
			return m
		}
	}
	return nil
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
	for _, p := range m.Message.Entities {
		if _, ok := p.(*MessageEntityBotCommand); ok {
			return true
		}
	}
	return false
}

// GetCommand returns the command from the message.
// If the message is not a command, it returns an empty string.
func (m *NewMessage) GetCommand() string {
	for _, p := range m.Message.Entities {
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
	response := *resp
	response.Message.PeerID = m.Message.PeerID
	return &response, err
}

func (m *NewMessage) Respond(Text interface{}, Opts ...SendOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, SendOptions{})
	}
	resp, err := m.Client.SendMessage(m.ChatID(), Text, &Opts[0])
	if resp == nil {
		return nil, err
	}
	response := *resp
	response.Message.PeerID = m.Message.PeerID
	return &response, err
}

func (m *NewMessage) SendDice(Emoticon string) (*NewMessage, error) {
	return m.Client.SendDice(m.ChatID(), Emoticon)
}

func (m *NewMessage) SendAction(Action interface{}) (*ActionResult, error) {
	return m.Client.SendAction(m.ChatID(), Action)
}

func (m *NewMessage) Edit(Text interface{}, Opts ...SendOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, SendOptions{})
	}
	resp, err := m.Client.EditMessage(m.ChatID(), m.ID, Text, &Opts[0])
	if resp == nil {
		return nil, err
	}
	response := *resp
	response.Message.PeerID = m.Message.PeerID
	return &response, err
}

func (m *NewMessage) ReplyMedia(Media interface{}, Opts ...MediaOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, MediaOptions{ReplyID: m.ID})
	} else {
		Opts[0].ReplyID = m.ID
	}
	resp, err := m.Client.SendMedia(m.ChatID(), Media, &Opts[0])
	if err != nil {
		return nil, err
	}
	response := *resp
	response.Message.PeerID = m.Message.PeerID
	return &response, err
}

func (m *NewMessage) RespondMedia(Media interface{}, Opts ...MediaOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, MediaOptions{})
	}
	resp, err := m.Client.SendMedia(m.ChatID(), Media, &Opts[0])
	if err != nil {
		return nil, err
	}
	response := *resp
	response.Message.PeerID = m.Message.PeerID
	return &response, err
}

// Delete deletes the message
func (m *NewMessage) Delete() error {
	return m.Client.DeleteMessage(m.ChatID(), m.ID)
}

// React to a message
func (m *NewMessage) React(Reaction ...string) error {
	var Reactions []string
	Reactions = append(Reactions, Reaction...)
	return m.Client.SendReaction(m.ChatID(), m.ID, Reactions, true)
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
	response := *resp
	response.Message.PeerID = m.Message.PeerID
	return &response, err
}

// Download Media to Disk,
// if path is empty, it will be downloaded to the current directory,
// returns the path to the downloaded file
func (m *NewMessage) Download(opts ...*DownloadOptions) (string, error) {
	return m.Client.DownloadMedia(m.Media(), opts...)
}
