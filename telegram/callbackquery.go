// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"fmt"
)

type (
	CallbackQuery struct {
		QueryID        int64
		Data           []byte
		OriginalUpdate *UpdateBotCallbackQuery
		Sender         *UserObj
		MessageID      int32
		SenderID       int64
		ChatID         int64
		Chat           *ChatObj
		Channel        *Channel
		Peer           Peer
		Client         *Client
	}
)

func (b *CallbackQuery) Answer(Text string, options ...*CallbackOptions) (bool, error) {
	var opts CallbackOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	return b.Client.AnswerCallbackQuery(b.QueryID, Text, &opts)
}

func (b *CallbackQuery) GetMessage() (*NewMessage, error) {
	m, err := b.Client.GetMessages(b.Peer, &SearchOption{IDs: []int32{b.MessageID}})
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("message not found")
	}
	return &m[0], nil
}

func (b *CallbackQuery) GetChat() (*ChatObj, error) {
	if b.Chat != nil {
		return b.Chat, nil
	}
	return nil, fmt.Errorf("chat not found")
}

func (b *CallbackQuery) GetChannel() (*Channel, error) {
	if b.Channel != nil {
		return b.Channel, nil
	}
	return nil, fmt.Errorf("channel not found")
}

func (b *CallbackQuery) GetSender() (*UserObj, error) {
	if b.Sender != nil {
		return b.Sender, nil
	}
	return nil, fmt.Errorf("sender not found")
}

func (b *CallbackQuery) GetSenderID() int64 {
	if b.Sender != nil {
		return b.Sender.ID
	}
	return b.SenderID
}

func (b *CallbackQuery) GetChatID() int64 {
	if b.Chat != nil {
		return b.Chat.ID
	}
	if b.Channel != nil {
		return b.Channel.ID
	}
	return b.ChatID
}

func (b *CallbackQuery) ShortName() string {
	return b.OriginalUpdate.GameShortName
}

func (b *CallbackQuery) DataString() string {
	return string(b.Data)
}

func (m *CallbackQuery) ChatType() string {
	if m.OriginalUpdate != nil && m.OriginalUpdate.Peer != nil {
		switch m.OriginalUpdate.Peer.(type) {
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

func (b *CallbackQuery) IsPrivate() bool {
	return b.ChatType() == EntityUser
}

func (b *CallbackQuery) IsGroup() bool {
	if b.Channel != nil {
		return b.ChatType() == EntityChat || (b.ChatType() == EntityChannel && !b.Channel.Broadcast)
	}
	return b.ChatType() == EntityChat
}

func (b *CallbackQuery) IsChannel() bool {
	return b.ChatType() == EntityChannel
}

func (b *CallbackQuery) Edit(Text any, options ...*SendOptions) (*NewMessage, error) {
	var opts SendOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	return b.Client.EditMessage(b.Peer, b.MessageID, Text, &opts)
}

func (b *CallbackQuery) Delete() (*MessagesAffectedMessages, error) {
	return b.Client.DeleteMessages(b.Peer, []int32{b.MessageID})
}

// Conv starts a new conversation with the user
func (b *CallbackQuery) Conv(timeout ...int32) (*Conversation, error) {
	return b.Client.NewConversation(b.Peer, b.IsPrivate(), timeout...)
}

// Ask starts new conversation with the user
func (b *CallbackQuery) Ask(Text any, Opts ...*SendOptions) (*NewMessage, error) {
	var opt = getVariadic(Opts, &SendOptions{})
	if opt.Timeouts == 0 {
		opt.Timeouts = 120 // default timeout
	}

	conv, err := b.Conv(opt.Timeouts)
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

func (b *CallbackQuery) WaitClick(timeout ...int32) (*CallbackQuery, error) {
	conv, err := b.Conv(getVariadic(timeout, 60))
	if err != nil {
		return nil, err
	}
	defer conv.Close()

	return conv.WaitClick()
}

func (b *CallbackQuery) Reply(Text any, options ...*SendOptions) (*NewMessage, error) {
	var opts SendOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	msg, err := b.GetMessage()
	if err != nil {
		return nil, err
	}
	opts.ReplyID = msg.ID
	return b.Client.SendMessage(b.Peer, Text, &opts)
}

func (b *CallbackQuery) Respond(Text any, options ...*SendOptions) (*NewMessage, error) {
	var opts SendOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	return b.Client.SendMessage(b.Peer, Text, &opts)
}

func (b *CallbackQuery) ReplyMedia(Media any, options ...*MediaOptions) (*NewMessage, error) {
	var opts MediaOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	msg, err := b.GetMessage()
	if err != nil {
		return nil, err
	}
	opts.ReplyID = msg.ReplyToMsgID()
	return b.Client.SendMedia(b.Peer, Media, &opts)
}

func (b *CallbackQuery) RespondMedia(Media any, options ...*MediaOptions) (*NewMessage, error) {
	var opts MediaOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	return b.Client.SendMedia(b.Peer, Media, &opts)
}

func (b *CallbackQuery) ForwardTo(ChatID int64, options ...*ForwardOptions) (*NewMessage, error) {
	var opts ForwardOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	m, err := b.Client.Forward(b.Peer, ChatID, []int32{b.MessageID}, &opts)
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("message not found")
	}
	return &m[0], nil
}

func (b *CallbackQuery) Marshal(nointent ...bool) string {
	return b.Client.JSON(b.OriginalUpdate, nointent)
}

type InlineCallbackQuery struct {
	QueryID        int64
	Data           []byte
	OriginalUpdate *UpdateInlineBotCallbackQuery
	Sender         *UserObj
	MsgID          InputBotInlineMessageID
	SenderID       int64
	ChatInstance   int64
	Client         *Client
	GameShortName  string
}

func (b *InlineCallbackQuery) Answer(Text string, options ...*CallbackOptions) (bool, error) {
	var opts CallbackOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	return b.Client.AnswerCallbackQuery(b.QueryID, Text, &opts)
}

func (b *InlineCallbackQuery) ShortName() string {
	return b.OriginalUpdate.GameShortName
}

func (b *InlineCallbackQuery) DataString() string {
	return string(b.Data)
}

func (b *InlineCallbackQuery) GetSender() (*UserObj, error) {
	if b.Sender != nil {
		return b.Sender, nil
	}
	return nil, fmt.Errorf("sender not found")
}

func (b *InlineCallbackQuery) GetSenderID() int64 {
	if b.Sender != nil {
		return b.Sender.ID
	}
	return b.SenderID
}

func (b *InlineCallbackQuery) Edit(Text any, options ...*SendOptions) (*NewMessage, error) {
	var opts SendOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	return b.Client.EditMessage(&b.MsgID, 0, Text, &opts)
}

func (b *InlineCallbackQuery) ChatType() string {
	if b.ChatInstance == int64(InlineQueryPeerTypePm) || b.ChatInstance == int64(InlineQueryPeerTypeSameBotPm) {
		return EntityUser
	} else if b.ChatInstance == int64(InlineQueryPeerTypeChat) {
		return EntityChat
	} else if b.ChatInstance == int64(InlineQueryPeerTypeBroadcast) || b.ChatInstance == int64(InlineQueryPeerTypeMegagroup) {
		return EntityChannel
	} else {
		return EntityUnknown
	}
}

func (b *InlineCallbackQuery) IsPrivate() bool {
	return b.ChatType() == EntityUser
}

func (b *InlineCallbackQuery) IsGroup() bool {
	return b.ChatType() == EntityChat
}

func (b *InlineCallbackQuery) IsChannel() bool {
	return b.ChatType() == EntityChannel
}

func (b *InlineCallbackQuery) Marshal(nointent ...bool) string {
	return b.Client.JSON(b.OriginalUpdate, nointent)
}
