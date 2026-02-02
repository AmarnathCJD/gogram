package telegram

import (
	"fmt"
	"slices"
	"time"
)

const defaultConversationTimeout = 60

// State Machine for conversation with users and in groups
type Conversation struct {
	Client          *Client
	Peer            InputPeer
	isPrivate       bool
	timeout         int32
	openHandlers    []Handle
	lastMsg         *NewMessage
	stopPropagation bool
}

func (c *Client) NewConversation(peer any, isPrivate bool, timeout ...int32) (*Conversation, error) {
	peerID, err := c.ResolvePeer(peer)
	if err != nil {
		return nil, err
	}
	return &Conversation{
		Client:          c,
		Peer:            peerID,
		isPrivate:       isPrivate,
		timeout:         getVariadic(timeout, defaultConversationTimeout),
		stopPropagation: false,
	}, nil
}

// NewConversation creates a new conversation with user
func NewConversation(client *Client, peer InputPeer, timeout ...int32) *Conversation {
	return &Conversation{
		Client:          client,
		Peer:            peer,
		timeout:         getVariadic(timeout, defaultConversationTimeout),
		stopPropagation: false,
	}
}

// SetTimeout sets the timeout for conversation
func (c *Conversation) SetTimeout(timeout int32) *Conversation {
	c.timeout = timeout
	return c
}

// when stopPropagation is set to true, the event handler blocks all other handlers
func (c *Conversation) SetStopPropagation(stop bool) *Conversation {
	c.stopPropagation = stop
	return c
}

func (c *Conversation) Respond(text any, opts ...*SendOptions) (*NewMessage, error) {
	return c.Client.SendMessage(c.Peer, text, opts...)
}

func (c *Conversation) RespondMedia(media InputMedia, opts ...*MediaOptions) (*NewMessage, error) {
	return c.Client.SendMedia(c.Peer, media, opts...)
}

func (c *Conversation) Reply(text any, opts ...*SendOptions) (*NewMessage, error) {
	var options = getVariadic(opts, &SendOptions{})
	if options.ReplyID == 0 {
		if c.lastMsg != nil {
			options.ReplyID = c.lastMsg.ID
		}
	}

	return c.Client.SendMessage(c.Peer, text, opts...)
}

func (c *Conversation) ReplyMedia(media InputMedia, opts ...*MediaOptions) (*NewMessage, error) {
	var options = getVariadic(opts, &MediaOptions{})
	if options.ReplyID == 0 {
		if c.lastMsg != nil {
			options.ReplyID = c.lastMsg.ID
		}
	}

	return c.Client.SendMedia(c.Peer, media, opts...)
}

func (c *Conversation) GetResponse() (*NewMessage, error) {
	resp := make(chan *NewMessage, 1)
	waitFunc := func(m *NewMessage) error {
		select {
		case resp <- m:
			c.lastMsg = m
		default:
		}

		if c.stopPropagation {
			return EndGroup
		}
		return nil
	}

	var filters []Filter
	switch c.Peer.(type) {
	case *InputPeerChannel, *InputPeerChat:
		filters = append(filters, FilterChats(c.Client.GetPeerID(c.Peer)))
	case *InputPeerUser, *InputPeerSelf:
		filters = append(filters, FilterUsers(c.Client.GetPeerID(c.Peer)))
	}

	if c.isPrivate {
		filters = append(filters, FilterPrivate)
	}

	h := c.Client.On(OnMessage, waitFunc, filters...)
	h.SetGroup("conversation")

	c.openHandlers = append(c.openHandlers, h)
	select {
	case <-time.After(time.Duration(c.timeout) * time.Second):
		go c.removeHandle(h)
		return nil, fmt.Errorf("conversation timeout: %d", c.timeout)
	case m := <-resp:
		go c.removeHandle(h)
		return m, nil
	}
}

func (c *Conversation) GetEdit() (*NewMessage, error) {
	resp := make(chan *NewMessage)
	waitFunc := func(m *NewMessage) error {
		select {
		case resp <- m:
			c.lastMsg = m
		default:
		}

		if c.stopPropagation {
			return EndGroup
		}
		return nil
	}

	var filters []Filter
	switch c.Peer.(type) {
	case *InputPeerChannel, *InputPeerChat:
		filters = append(filters, FilterChats(c.Client.GetPeerID(c.Peer)))
	case *InputPeerUser, *InputPeerSelf:
		filters = append(filters, FilterUsers(c.Client.GetPeerID(c.Peer)))
	}

	if c.isPrivate {
		filters = append(filters, FilterPrivate)
	}

	h := c.Client.On(OnEdit, waitFunc, filters...)
	h.SetGroup("conversation")
	c.openHandlers = append(c.openHandlers, h)
	select {
	case <-time.After(time.Duration(c.timeout) * time.Second):
		go c.removeHandle(h)
		return nil, fmt.Errorf("conversation timeout: %d", c.timeout)
	case m := <-resp:
		go c.removeHandle(h)
		return m, nil
	}
}

func (c *Conversation) GetReply() (*NewMessage, error) {
	resp := make(chan *NewMessage)
	waitFunc := func(m *NewMessage) error {
		select {
		case resp <- m:
			c.lastMsg = m
		default:
		}

		if c.stopPropagation {
			return EndGroup
		}
		return nil
	}

	var filters []Filter
	switch c.Peer.(type) {
	case *InputPeerChannel, *InputPeerChat:
		filters = append(filters, FilterChats(c.Client.GetPeerID(c.Peer)))
	case *InputPeerUser, *InputPeerSelf:
		filters = append(filters, FilterUsers(c.Client.GetPeerID(c.Peer)))
	}

	if c.isPrivate {
		filters = append(filters, FilterPrivate)
	}

	filters = append(filters, FilterReply)

	h := c.Client.On(OnMessage, waitFunc, filters...)
	h.SetGroup("conversation")
	c.openHandlers = append(c.openHandlers, h)
	select {
	case <-time.After(time.Duration(c.timeout) * time.Second):
		go c.removeHandle(h)
		return nil, fmt.Errorf("conversation timeout: %d", c.timeout)
	case m := <-resp:
		go c.removeHandle(h)
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

func (c *Conversation) WaitEvent(ev Update) (Update, error) {
	resp := make(chan Update)
	waitFunc := func(u Update, c *Client) error {
		select {
		case resp <- u:
		default:
		}

		return nil
	}

	h := c.Client.On(ev, waitFunc)
	c.openHandlers = append(c.openHandlers, h)
	select {
	case <-time.After(time.Duration(c.timeout) * time.Second):
		go c.removeHandle(h)
		return nil, fmt.Errorf("conversation timeout: %d", c.timeout)
	case u := <-resp:
		go c.removeHandle(h)
		return u, nil
	}
}

func (c *Conversation) WaitRead() (*UpdateReadChannelInbox, error) {
	resp := make(chan *UpdateReadChannelInbox)
	waitFunc := func(u Update) error {
		switch v := u.(type) {
		case *UpdateReadChannelInbox:
			select {
			case resp <- v:
			default:
			}
		}

		return nil
	}

	h := c.Client.On(&UpdateReadChannelInbox{}, waitFunc)
	c.openHandlers = append(c.openHandlers, h)

	select {
	case <-time.After(time.Duration(c.timeout) * time.Second):
		go c.removeHandle(h)
		return nil, fmt.Errorf("conversation timeout: %d", c.timeout)
	case u := <-resp:
		go c.removeHandle(h)
		return u, nil
	}
}

func (c *Conversation) removeHandle(h Handle) {
	for i, v := range c.openHandlers {
		if v == h {
			c.openHandlers = slices.Delete(c.openHandlers, i, i+1)
			break
		}
	}
	c.Client.removeHandle(h)
}

// close closes the conversation, removing all open event handlers
func (c *Conversation) Close() {
	for _, h := range c.openHandlers {
		c.Client.removeHandle(h)
	}
}
