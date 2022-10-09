package telegram

import (
	"log"
	"sync"
)

type (
	Filters struct {
		IsPrivate      bool
		IsGroup        bool
		IsChannel      bool
		IsCommand      bool
		IsText         bool
		IsMedia        bool
		Func           func(*NewMessage) bool
		BlackListChats []int64
		WhiteListChats []int64
		Users          []int64
		Outgoing       bool
		Incoming       bool
	}

	MessageHandle struct {
		Pattern interface{}
		Handler func(m *NewMessage) error
		Client  *Client
		Filters *Filters
	}

	ChatActionHandle struct {
		Handler func(m *NewMessage) error
		Client  *Client
	}

	InlineHandle struct {
		Pattern interface{}
		Handler func(m *InlineQuery) error
		Client  *Client
	}

	CallbackHandle struct {
		Pattern interface{}
		Handler func(m *CallbackQuery) error
		Client  *Client
	}

	RawHandle struct {
		updateType Update
		Handler    func(m Update) error
		Client     *Client
	}

	MessageEditHandle struct {
		Pattern interface{}
		Handler func(m *NewMessage) error
		Client  *Client
	}

	Command struct {
		Cmd    string
		Prefix string
	}
)

type (
	Progress struct {
		Current int64
		Total   int64
		rwlock  sync.RWMutex
	}

	LoginOptions struct {
		Password     string        `json:"password,omitempty"`
		Code         string        `json:"code,omitempty"`
		CodeHash     string        `json:"code_hash,omitempty"`
		CodeCallback func() string `json:"-"`
		FirstName    string        `json:"first_name,omitempty"`
		LastName     string        `json:"last_name,omitempty"`
	}

	ActionResult struct {
		Peer   InputPeer `json:"peer,omitempty"`
		Client *Client   `json:"client,omitempty"`
	}

	ArticleOptions struct {
		ID           string                             `json:"id,omitempty"`
		ExcludeMedia bool                               `json:"exclude_media,omitempty"`
		Thumb        InputWebDocument                   `json:"thumb,omitempty"`
		Content      InputWebDocument                   `json:"content,omitempty"`
		LinkPreview  bool                               `json:"link_preview,omitempty"`
		ReplyMarkup  ReplyMarkup                        `json:"reply_markup,omitempty"`
		Entities     []MessageEntity                    `json:"entities,omitempty"`
		ParseMode    string                             `json:"parse_mode,omitempty"`
		Caption      string                             `json:"caption,omitempty"`
		Venue        *InputBotInlineMessageMediaVenue   `json:"venue,omitempty"`
		Location     *InputBotInlineMessageMediaGeo     `json:"location,omitempty"`
		Contact      *InputBotInlineMessageMediaContact `json:"contact,omitempty"`
		Invoice      *InputBotInlineMessageMediaInvoice `json:"invoice,omitempty"`
	}

	PasswordOptions struct {
		Hint              string        `json:"hint,omitempty"`
		Email             string        `json:"email,omitempty"`
		EmailCodeCallback func() string `json:"email_code_callback,omitempty"`
	}

	Log struct {
		Logger *log.Logger
	}
)

var (
	ParticipantsAdmins = &ParticipantOptions{
		Filter: &ChannelParticipantsAdmins{},
		Query:  "",
		Offset: 0,
		Limit:  50,
	}
)

func (p *Progress) Set(value int64) {
	p.rwlock.Lock()
	defer p.rwlock.Unlock()
	p.Current = value
}

func (p *Progress) Get() int64 {
	p.rwlock.RLock()
	defer p.rwlock.RUnlock()
	return p.Current
}

func (p *Progress) Data() (total int64, current int64) {
	p.rwlock.RLock()
	defer p.rwlock.RUnlock()
	return p.Total, p.Current
}

func (p *Progress) Percentage() float64 {
	p.rwlock.RLock()
	defer p.rwlock.RUnlock()
	return float64(p.Current) / float64(p.Total) * 100
}

func (p *Progress) SetTotal(total int64) {
	p.rwlock.Lock()
	defer p.rwlock.Unlock()
	p.Total = total
}

func (p *Progress) Init() {
	p.rwlock.Lock()
	defer p.rwlock.Unlock()
	p.Total = 0
	p.Current = 0
}

func (p *Progress) Add(value int64) {
	p.rwlock.Lock()
	defer p.rwlock.Unlock()
	p.Current += value
}

// Cancel the pointed Action,
// Returns true if the action was cancelled
func (a *ActionResult) Cancel() bool {
	if a.Peer == nil || a.Client == nil {
		return false // Avoid nil pointer dereference
	}
	b, err := a.Client.MessagesSetTyping(a.Peer, 0, &SendMessageCancelAction{})
	if err != nil {
		return false
	}
	return b
}

// Custom logger

func (l *Log) Error(err error) {
	if l.Logger != nil {
		l.Logger.Println("Client - Error: ", err)
	}
}

func (l *Log) Info(msg string) {
	if l.Logger != nil {
		l.Logger.Println("Client - Info: ", msg)
	}
}

func (l *Log) Debug(msg string) {
	if l.Logger != nil {
		l.Logger.Println("Client - Debug: ", msg)
	}
}
