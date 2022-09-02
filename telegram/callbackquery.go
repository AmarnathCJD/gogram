package telegram

import (
	"encoding/json"
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
	fmt.Println("GetMessage:", b.MessageID, b.Peer)
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

func (b *CallbackQuery) Edit(Text interface{}, options ...*SendOptions) (*NewMessage, error) {
	var opts SendOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	return b.Client.EditMessage(b.Peer, b.MessageID, Text, &opts)
}

func (b *CallbackQuery) Delete() error {
	return b.Client.DeleteMessage(b.Peer, b.MessageID)
}

func (b *CallbackQuery) Reply(Text interface{}, options ...*SendOptions) (*NewMessage, error) {
	var opts SendOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	opts.ReplyID = b.MessageID
	return b.Client.SendMessage(b.Peer, Text, &opts)
}

func (b *CallbackQuery) Respond(Text interface{}, options ...*SendOptions) (*NewMessage, error) {
	var opts SendOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	return b.Client.SendMessage(b.Peer, Text, &opts)
}

func (b *CallbackQuery) ReplyMedia(Media interface{}, options ...*MediaOptions) (*NewMessage, error) {
	var opts MediaOptions
	if len(options) > 0 {
		opts = *options[0]
	}
	opts.ReplyID = b.MessageID
	return b.Client.SendMedia(b.Peer, Media, &opts)
}

func (b *CallbackQuery) RespondMedia(Media interface{}, options ...*MediaOptions) (*NewMessage, error) {
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
	return b.Client.ForwardMessage(b.Peer, ChatID, []int32{b.MessageID}, &opts)
}

func (b *CallbackQuery) Marshal() string {
	bytes, _ := json.MarshalIndent(b, "", "  ")
	return string(bytes)
}
