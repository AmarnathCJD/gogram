package telegram

import (
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
)

type NewMessage struct {
	Action         MessageAction
	Channel        *Channel
	Chat           *ChatObj
	Client         *Client
	File           *CustomFile
	ID             int32
	Message        *MessageObj
	OriginalUpdate Message
	Peer           InputPeer
	Sender         *UserObj
	SenderChat     *Channel
}

type DeleteMessage struct {
	Client    *Client
	ChannelID int64
	Messages  []int32
}

type CustomFile struct {
	Ext    string `json:"ext,omitempty"`
	FileID string `json:"file_id,omitempty"`
	Name   string `json:"name,omitempty"`
	Size   int64  `json:"size,omitempty"`
}

func (m *NewMessage) MessageText() string {
	return m.Message.Message
}

func (m *NewMessage) ReplyToMsgID() int32 {
	if m.Message.ReplyTo != nil {
		return m.Message.ReplyTo.(*MessageReplyHeaderObj).ReplyToMsgID
	}
	return 0
}

func (m *NewMessage) ReplyID() int32 {
	return m.ReplyToMsgID()
}

// return the topic id of the message if it is in a topic
// if it is a reply to a message, return the topic id of the message
func (m *NewMessage) TopicID() (int32, bool) {
	if m.Message.ReplyTo != nil {
		if reply, ok := m.Message.ReplyTo.(*MessageReplyHeaderObj); ok {
			if reply.ForumTopic {
				if reply.ReplyToTopID != 0 {
					return reply.ReplyToTopID, true
				}
				return reply.ReplyToMsgID, true
			}
		}
	}
	return 0, false
}

func (m *NewMessage) ReplySenderID() int64 {
	if m.Message.ReplyTo != nil {
		return m.Client.GetPeerID(m.Message.ReplyTo.(*MessageReplyHeaderObj).ReplyToPeerID)
	}
	return 0
}

func (m *NewMessage) MarkRead() (err error) {
	_, err = m.Client.SendReadAck(m.ChannelID(), m.ID)
	return
}

func (a *NewMessage) Pin(opts ...*PinOptions) (err error) {
	_, err = a.Client.PinMessage(a.ChatID(), a.ID, opts...)
	return
}

func (a *NewMessage) Unpin() (err error) {
	_, err = a.Client.UnpinMessage(a.ChatID(), a.ID)
	return err
}

func (m *NewMessage) GetReplyMessage() (*NewMessage, error) {
	if !m.IsReply() {
		return nil, errors.New("message is not a reply")
	}
	// switch rep := m.Message.ReplyTo.(type) {
	// case *MessageReplyHeaderObj:

	// } TODO: Menakked Ahh, So Pinne Nokam, Reply Thingies.

	// try to get message by reply id, cuz if reply is by bot other bots can't get it otherwise
	messages, err := m.Client.GetMessages(m.ChannelID(), &SearchOption{IDs: &InputMessageReplyTo{ID: m.ID}})
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		// if actual message got deleted, try again with actual reply id
		messages, err = m.Client.GetMessages(m.ChannelID(), &SearchOption{IDs: []int32{m.ReplyToMsgID()}})
		if err != nil {
			return nil, err
		}
		if len(messages) == 0 {
			return nil, errors.New("message not found")
		}
	}
	return &messages[0], nil
}

func (m *NewMessage) ChatID() int64 {
	if m.Message != nil && m.Message.PeerID != nil {
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

func (m *NewMessage) ChannelID() int64 {
	if m.Message != nil && m.Message.PeerID != nil {
		switch Peer := m.Message.PeerID.(type) {
		case *PeerChannel:
			return -100_000_000_0000 - Peer.ChannelID
		case *PeerChat:
			return -Peer.ChatID
		case *PeerUser:
			return Peer.UserID
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
			if m.Channel != nil && !m.Channel.Broadcast {
				return EntityChat
			}
			return EntityChannel
		}
	}
	return EntityUnknown
}

func (m *NewMessage) IsPrivate() bool {
	return m.ChatType() == EntityUser
}

// returns the error only, of a method
func (m *NewMessage) E(obj any, err error) error {
	return err
}

func (m *NewMessage) IsEmpty() bool {
	_, isEmpty := m.OriginalUpdate.(*MessageEmpty)
	return isEmpty
}

func (m *NewMessage) IsGroup() bool {
	return m.ChatType() == EntityChat
}

func (m *NewMessage) ReplyMarkup() *ReplyMarkup {
	return &m.Message.ReplyMarkup
}

func (m *NewMessage) IsChannel() bool {
	return m.ChatType() == EntityChannel
}

func (m *NewMessage) IsReply() bool {
	return m.Message.ReplyTo != nil
}

func (m *NewMessage) Marshal(nointent ...bool) string {
	return m.Client.JSON(m.OriginalUpdate, nointent)
}

func (m *NewMessage) Unmarshal(data []byte) (*NewMessage, error) {
	if err := json.Unmarshal(data, m.Message); err != nil {
		return m, err
	}
	m = packMessage(m.Client, m.Message)
	return m, nil
}

func (m *NewMessage) GetChat() (*ChatObj, error) {
	return m.Client.GetChat(m.ChatID())
}

func (m *NewMessage) GetChannel() (*Channel, error) {
	return m.Client.GetChannel(m.ChannelID())
}

func (m *NewMessage) GetSender() (*UserObj, error) {
	return m.Client.GetUser(m.SenderID())
}

func (m *NewMessage) GetSenderChat() *Channel {
	return m.SenderChat
}

func (m *NewMessage) Date() int32 {
	return m.Message.Date
}

// GetPeer returns the peer of the message
func (m *NewMessage) GetPeer() (int64, int64) {
	if m.IsPrivate() {
		User, _ := m.Client.GetPeerUser(m.ChatID())
		return User.UserID, User.AccessHash
	} else if m.IsGroup() {
		Chat, _ := m.Client.GetChat(m.ChannelID())
		return Chat.ID, 0
	} else if m.IsChannel() {
		Channel, _ := m.Client.GetPeerChannel(m.ChannelID())
		return Channel.ChannelID, Channel.AccessHash
	}
	return 0, 0
}

func (m *NewMessage) IsForward() bool {
	return m.Message.FwdFrom != nil
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

func (m *NewMessage) Text() string {
	return m.MessageText()
}

func (m *NewMessage) RawText(markdown ...bool) string {
	var md = getVariadic(markdown, false)
	if parsedText := InsertTagsIntoText(m.Text(), ParseEntitiesToTags(m.Message.Entities)); md {
		return ToMarkdown(parsedText)
	} else {
		return parsedText
	}
}

func (m *NewMessage) Args() string {
	Messages := strings.Split(m.Text(), " ")
	if len(Messages) < 2 {
		return ""
	}
	return strings.TrimSpace(strings.Join(Messages[1:], " "))
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

// Conv starts a new conversation with the user
func (m *NewMessage) Conv(timeout ...int32) (*Conversation, error) {
	return m.Client.NewConversation(m.Peer, m.IsPrivate(), timeout...)
}

// Ask starts new conversation with the user
func (m *NewMessage) Ask(Text any, Opts ...*SendOptions) (*NewMessage, error) {
	var opt = getVariadic(Opts, &SendOptions{})
	if opt.Timeouts == 0 {
		opt.Timeouts = 120 // default timeout
	}

	conv, err := m.Conv(opt.Timeouts)
	if err != nil {
		return nil, err
	}

	defer conv.Close()

	_, err = conv.Respond(Text, Opts...)
	if err != nil {
		return nil, err
	}

	return conv.GetResponse()
}

// Client.SendMessage ReplyID set to messageID
func (m *NewMessage) Reply(Text any, Opts ...SendOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, SendOptions{ReplyID: m.ID})
	} else {
		Opts[0].ReplyID = m.ID
	}
	resp, err := m.Client.SendMessage(m.ChannelID(), Text, &Opts[0])
	if resp == nil {
		return nil, err
	}
	response := *resp
	response.Message.PeerID = m.Message.PeerID
	return &response, err
}

// ReplyWithoutError calls message.Reply and wraps the error to error channel of the client
func (m *NewMessage) ReplyWithoutError(Text any, Opts ...SendOptions) *NewMessage {
	resp, err := m.Reply(Text, Opts...)
	if err != nil {
		m.Client.WrapError(err)
	}
	return resp
}

func (m *NewMessage) Respond(Text any, Opts ...SendOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, SendOptions{})
	}
	resp, err := m.Client.SendMessage(m.ChannelID(), Text, &Opts[0])
	if resp == nil {
		return nil, err
	}
	response := *resp
	response.Message.PeerID = m.Message.PeerID
	return &response, err
}

func (m *NewMessage) SendDice(Emoticon string) (*NewMessage, error) {
	return m.Client.SendDice(m.ChannelID(), Emoticon)
}

func (m *NewMessage) SendAction(Action any) (*ActionResult, error) {
	return m.Client.SendAction(m.ChannelID(), Action)
}

func (m *NewMessage) Edit(Text any, Opts ...SendOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, SendOptions{})
	}
	resp, err := m.Client.EditMessage(m.ChannelID(), m.ID, Text, &Opts[0])
	if resp == nil {
		return nil, err
	}
	response := *resp
	response.Message.PeerID = m.Message.PeerID
	return &response, err
}

func (m *NewMessage) ReplyMedia(Media any, Opts ...MediaOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, MediaOptions{ReplyID: m.ID})
	} else {
		Opts[0].ReplyID = m.ID
	}

	resp, err := m.Client.SendMedia(m.ChannelID(), Media, &Opts[0])
	if err != nil {
		return nil, err
	}
	response := *resp
	response.Message.PeerID = m.Message.PeerID
	return &response, err
}

func (m *NewMessage) ReplyAlbum(Album any, Opts ...*MediaOptions) ([]*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, &MediaOptions{})
	}
	Opts[0].ReplyID = m.ID
	return m.Client.SendAlbum(m.ChannelID(), Album, Opts...)
}

func (m *NewMessage) RespondMedia(Media any, Opts ...MediaOptions) (*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, MediaOptions{})
	}
	resp, err := m.Client.SendMedia(m.ChannelID(), Media, &Opts[0])
	if err != nil {
		return nil, err
	}
	response := *resp
	response.Message.PeerID = m.Message.PeerID
	return &response, err
}

func (m *NewMessage) RespondAlbum(Album any, Opts ...*MediaOptions) ([]*NewMessage, error) {
	if len(Opts) == 0 {
		Opts = append(Opts, &MediaOptions{})
	}
	return m.Client.SendAlbum(m.ChannelID(), Album, Opts...)
}

// Delete deletes the message
func (m *NewMessage) Delete() (*MessagesAffectedMessages, error) {
	return m.Client.DeleteMessages(m.Peer, []int32{m.ID})
}

// React to a message
func (m *NewMessage) React(Reactions ...any) error {
	return m.Client.SendReaction(m.ChannelID(), m.ID, Reactions, true)
}

// Forward forwards the message to a chat
func (m *NewMessage) ForwardTo(PeerID any, Opts ...*ForwardOptions) (*NewMessage, error) {
	resps, err := m.Client.Forward(PeerID, m.Peer, []int32{m.ID}, Opts...)
	if resps == nil {
		return nil, err
	}
	resps[0].Message.PeerID = m.Message.PeerID
	return &resps[0], err
}

// Fact checks the message for facts
func (m *NewMessage) Fact() ([]*FactCheck, error) {
	peer, err := m.Client.ResolvePeer(m.ChannelID())
	if err != nil {
		return nil, err
	}

	return m.Client.MessagesGetFactCheck(peer, []int32{m.ID})
}

// GetMediaGroup returns the media group of the message
func (m *NewMessage) GetMediaGroup() ([]NewMessage, error) {
	return m.Client.GetMediaGroup(m.ChannelID(), m.ID)
}

// Download Media to Disk,
// if path is empty, it will be downloaded to the current directory,
// returns the path to the downloaded file
func (m *NewMessage) Download(opts ...*DownloadOptions) (string, error) {
	return m.Client.DownloadMedia(m.Media(), opts...)
}

// Album Type for MediaGroup
type Album struct {
	Client    *Client
	GroupedID int64
	Messages  []*NewMessage
}

func (a *Album) Marshal(nointent ...bool) string {
	var messages []Message
	for _, m := range a.Messages {
		messages = append(messages, m.OriginalUpdate)
	}

	return a.Client.JSON(messages, nointent)
}

func (a *Album) Download(opts ...*DownloadOptions) ([]string, error) {
	var paths []string
	for _, m := range a.Messages {
		path, err := m.Download(opts...)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (a *Album) Delete() (*MessagesAffectedMessages, error) {
	var ids []int32
	for _, m := range a.Messages {
		ids = append(ids, m.ID)
	}
	return a.Client.DeleteMessages(a.Messages[0].ChatID(), ids)
}

func (a *Album) ForwardTo(PeerID any, Opts ...*ForwardOptions) ([]NewMessage, error) {
	var ids []int32
	for _, m := range a.Messages {
		ids = append(ids, m.ID)
	}
	return a.Client.Forward(a.Messages[0].ChatID(), PeerID, ids, Opts...)
}

func (a *Album) IsReply() bool {
	return a.Messages[0].IsReply()
}

func (a *Album) IsForward() bool {
	return a.Messages[0].IsForward()
}

func (a *Album) GetReplyMessage() (*NewMessage, error) {
	return a.Messages[0].GetReplyMessage()
}

func (a *Album) Respond(Text any, Opts ...SendOptions) (*NewMessage, error) {
	return a.Messages[0].Respond(Text, Opts...)
}

func (a *Album) RespondMedia(Media any, Opts ...MediaOptions) (*NewMessage, error) {
	return a.Messages[0].RespondMedia(Media, Opts...)
}

func (a *Album) Reply(Text any, Opts ...SendOptions) (*NewMessage, error) {
	return a.Messages[0].Reply(Text, Opts...)
}

func (a *Album) ReplyMedia(Media any, Opts ...MediaOptions) (*NewMessage, error) {
	return a.Messages[0].ReplyMedia(Media, Opts...)
}

func (a *Album) Edit(Text any, Opts ...SendOptions) (*NewMessage, error) {
	return a.Messages[0].Edit(Text, Opts...)
}

func (a *Album) MarkRead() error {
	return a.Messages[0].MarkRead()
}

func (a *Album) Pin(Opts ...*PinOptions) error {
	return a.Messages[0].Pin(Opts...)
}

func (a *Album) Unpin() error {
	return a.Messages[0].Unpin()
}
