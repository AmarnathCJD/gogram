package telegram

import (
	"fmt"
	"time"
)

const ConvDefaultTimeOut = int32(60)

type handle interface{}

// Conversation is a struct for conversation with user.
type Conversation struct {
	Client    *Client
	Peer      InputPeer
	isPrivate bool
	timeOut   int32
	openH     []handle
	lastMsg   *NewMessage
}

func (c *Client) NewConversation(peer any, isPrivate bool, timeout ...int32) (*Conversation, error) {
	peerID, err := c.ResolvePeer(peer)
	if err != nil {
		return nil, err
	}
	return &Conversation{
		Client:    c,
		Peer:      peerID,
		isPrivate: isPrivate,
		timeOut:   getVariadic(timeout, ConvDefaultTimeOut).(int32),
	}, nil
}

// NewConversation creates a new conversation with user
func NewConversation(client *Client, peer InputPeer, timeout ...int32) *Conversation {
	return &Conversation{
		Client:  client,
		Peer:    peer,
		timeOut: getVariadic(timeout, ConvDefaultTimeOut).(int32),
	}
}

// SetTimeOut sets the timeout for conversation
func (c *Conversation) SetTimeOut(timeout int32) *Conversation {
	c.timeOut = timeout
	return c
}

func (c *Conversation) Respond(text any, opts ...*SendOptions) (*NewMessage, error) {
	return c.Client.SendMessage(c.Peer, text, opts...)
}

func (c *Conversation) RespondMedia(media InputMedia, opts ...*MediaOptions) (*NewMessage, error) {
	return c.Client.SendMedia(c.Peer, media, opts...)
}

func (c *Conversation) Reply(text any, opts ...*SendOptions) (*NewMessage, error) {
	var options = getVariadic(opts, &SendOptions{}).(*SendOptions)
	if options.ReplyID == 0 {
		if c.lastMsg != nil {
			options.ReplyID = c.lastMsg.ID
		}
	}

	return c.Client.SendMessage(c.Peer, text, opts...)
}

func (c *Conversation) ReplyMedia(media InputMedia, opts ...*MediaOptions) (*NewMessage, error) {
	var options = getVariadic(opts, &MediaOptions{}).(*MediaOptions)
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
		resp <- m
		c.lastMsg = m
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

	c.openH = append(c.openH, &h)
	select {
	case <-time.After(time.Duration(c.timeOut) * time.Second):
		go c.removeHandle(&h)
		return nil, fmt.Errorf("conversation timeout: %d", c.timeOut)
	case m := <-resp:
		go c.removeHandle(&h)
		return m, nil
	}
}

func (c *Conversation) GetEdit() (*NewMessage, error) {
	resp := make(chan *NewMessage)
	waitFunc := func(m *NewMessage) error {
		resp <- m
		c.lastMsg = m
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
	c.openH = append(c.openH, &h)
	select {
	case <-time.After(time.Duration(c.timeOut) * time.Second):
		go c.removeHandle(&h)
		return nil, fmt.Errorf("conversation timeout: %d", c.timeOut)
	case m := <-resp:
		go c.removeHandle(&h)
		return m, nil
	}
}

func (c *Conversation) GetReply() (*NewMessage, error) {
	resp := make(chan *NewMessage)
	waitFunc := func(m *NewMessage) error {
		resp <- m
		c.lastMsg = m
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
	c.openH = append(c.openH, &h)
	select {
	case <-time.After(time.Duration(c.timeOut) * time.Second):
		go c.removeHandle(&h)
		return nil, fmt.Errorf("conversation timeout: %d", c.timeOut)
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
	waitFunc := func(u Update, c *Client) error {
		resp <- u
		return nil
	}

	h := c.Client.On(*ev, waitFunc)
	c.openH = append(c.openH, &h)
	select {
	case <-time.After(time.Duration(c.timeOut) * time.Second):
		go c.removeHandle(&h)
		return nil, fmt.Errorf("conversation timeout: %d", c.timeOut)
	case u := <-resp:
		go c.removeHandle(&h)
		return u, nil
	}
}

func (c *Conversation) WaitRead() (*UpdateReadChannelInbox, error) {
	resp := make(chan *UpdateReadChannelInbox)
	waitFunc := func(u Update) error {
		switch v := u.(type) {
		case *UpdateReadChannelInbox:
			resp <- v
		}

		return nil
	}

	h := c.Client.On(&UpdateReadChannelInbox{}, waitFunc)
	c.openH = append(c.openH, &h)

	select {
	case <-time.After(time.Duration(c.timeOut) * time.Second):
		go c.removeHandle(&h)
		return nil, fmt.Errorf("conversation timeout: %d", c.timeOut)
	case u := <-resp:
		go c.removeHandle(&h)
		return u, nil
	}
}

func (c *Conversation) removeHandle(h handle) {
	for i, v := range c.openH {
		if v == h {
			c.openH = append(c.openH[:i], c.openH[i+1:]...)
			return
		}
	}
	c.Client.removeHandle(h)
}

// close closes the conversation, removing all open event handlers
func (c *Conversation) Close() {
	for _, h := range c.openH {
		c.Client.removeHandle(h)
	}
}
