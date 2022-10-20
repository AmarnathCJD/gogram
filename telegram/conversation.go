package telegram

import (
	"time"

	"github.com/pkg/errors"
)

// Conversation is a struct for conversation with user.

const (
	// DefaultTimeOut is the default timeout for conversation
	DefaultTimeOut = 30
)

var (
	ErrTimeOut = errors.New("conversation timeout")
)

type handle interface {
	Remove()
}

// Conversation is a struct for conversation with user.
type Conversation struct {
	Client  *Client
	Peer    InputPeer
	timeOut int
	openH   []handle
	lastMsg *NewMessage
}

func (c *Client) NewConversation(peer any, timeout ...int) (*Conversation, error) {
	peerID, err := c.GetSendablePeer(peer)
	if err != nil {
		return nil, err
	}
	return &Conversation{
		Client:  c,
		Peer:    peerID,
		timeOut: getVariadic(timeout, DefaultTimeOut).(int),
	}, nil
}

// NewConversation creates a new conversation with user
func NewConversation(client *Client, peer InputPeer, timeout ...int) *Conversation {
	c := &Conversation{
		Client:  client,
		Peer:    peer,
		timeOut: DefaultTimeOut,
	}
	if len(timeout) > 0 {
		c.timeOut = timeout[0]
	}
	return c
}

// SetTimeOut sets the timeout for conversation
func (c *Conversation) SetTimeOut(timeout int) *Conversation {
	c.timeOut = timeout
	return c
}

func (c *Conversation) SendMessage(text string, opts ...*SendOptions) (*NewMessage, error) {
	return c.Client.SendMessage(c.Peer, text, opts...)
}

func (c *Conversation) SendMedia(media InputMedia, opts ...*MediaOptions) (*NewMessage, error) {
	return c.Client.SendMedia(c.Peer, media, opts...)
}

func (c *Conversation) GetResponse() (*NewMessage, error) {
	peerID := c.Client.GetPeerID(c.Peer)
	resp := make(chan *NewMessage, 1)
	waitFunc := func(m *NewMessage) error {
		resp <- m
		c.lastMsg = m
		return nil
	}
	var filter = &Filters{}
	switch c.Peer.(type) {
	case *InputPeerChannel, *InputPeerChat:
		filter.Chats = append(filter.Chats, peerID)
	default:
		filter.Users = append(filter.Users, peerID)
	}
	h := c.Client.AddMessageHandler(OnNewMessage, waitFunc, filter)
	c.openH = append(c.openH, &h)
	select {
	case <-time.After(time.Duration(c.timeOut) * time.Second):
		go c.removeHandle(&h)
		return nil, ErrTimeOut
	case m := <-resp:
		go c.removeHandle(&h)
		return m, nil
	}
}

func (c *Conversation) GetEdit() (*NewMessage, error) {
	peerID := c.Client.GetPeerID(c.Peer)
	resp := make(chan *NewMessage)
	waitFunc := func(m *NewMessage) error {
		IsTrigger := false
		switch c.Peer.(type) {
		case *InputPeerChannel, *InputPeerChat:
			if m.ChatID() == peerID {
				IsTrigger = true
			}
		default:
			if m.SenderID() == peerID {
				IsTrigger = true
			}
		}
		if IsTrigger {
			resp <- m
			c.lastMsg = m
		}
		return nil
	}
	h := c.Client.AddEditHandler(OnEditMessage, waitFunc)
	c.openH = append(c.openH, &h)
	select {
	case <-time.After(time.Duration(c.timeOut) * time.Second):
		go c.removeHandle(&h)
		return nil, ErrTimeOut
	case m := <-resp:
		go c.removeHandle(&h)
		return m, nil
	}
}

func (c *Conversation) GetReply() (*NewMessage, error) {
	peerID := c.Client.GetPeerID(c.Peer)
	resp := make(chan *NewMessage)
	waitFunc := func(m *NewMessage) error {
		if m.IsReply() {
			IsTrigger := false
			switch c.Peer.(type) {
			case *InputPeerChannel, *InputPeerChat:
				if m.ChatID() == peerID {
					IsTrigger = true
				}
			default:
				if m.SenderID() == peerID {
					IsTrigger = true
				}
			}
			if IsTrigger {
				resp <- m
				c.lastMsg = m
			}
		}
		return nil
	}
	h := c.Client.AddMessageHandler(OnNewMessage, waitFunc)
	c.openH = append(c.openH, &h)
	select {
	case <-time.After(time.Duration(c.timeOut) * time.Second):
		go c.removeHandle(&h)
		return nil, ErrTimeOut
	case m := <-resp:
		go c.removeHandle(&h)
		return m, nil
	}
}

func (c *Conversation) MarkRead() (*MessagesAffectedMessages, error) {
	if c.lastMsg != nil {
		return c.Client.SendReadAck(c.Peer, c.lastMsg.ID)
	} else {
		return c.Client.SendReadAck(c.Peer)
	}
}

func (c *Conversation) WaitEvent(ev *Update) (Update, error) {
	resp := make(chan Update)
	waitFunc := func(u Update) error {
		resp <- u
		return nil
	}
	h := c.Client.AddRawHandler(*ev, waitFunc)
	c.openH = append(c.openH, &h)
	select {
	case <-time.After(time.Duration(c.timeOut) * time.Second):
		go c.removeHandle(&h)
		return nil, ErrTimeOut
	case u := <-resp:
		go c.removeHandle(&h)
		return u, nil
	}
}

func (c *Conversation) WaitRead() {
	// TODO: implement this
}

func (c *Conversation) removeHandle(h handle) {
	for i, v := range c.openH {
		if v == h {
			c.openH = append(c.openH[:i], c.openH[i+1:]...)
			return
		}
	}
	h.Remove()
}

// Close closes the conversation
func (c *Conversation) Close() {
	for _, h := range c.openH {
		h.Remove()
	}
}
