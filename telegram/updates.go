package telegram

import (
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"
)

var (
	MessageHandles  = []MessageHandle{}
	InlineHandles   = []InlineHandle{}
	CallbackHandles = []CallbackHandle{}
	MessageEdit     = []MessageEditHandle{}
	RawHandles      = []RawHandle{}
	OnNewMessage    = "OnNewMessage"
	OnEditMessage   = "OnEditMessage"
	OnChatAction    = "OnChatAction"
	OnInlineQuery   = "OnInlineQuery"
	OnCallbackQuery = "OnCallbackQuery"
)

type (
	MessageHandle struct {
		Pattern interface{}
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

func HandleMessageUpdate(update Message) {
	if len(MessageHandles) == 0 {
		return
	}
	switch msg := update.(type) {
	case *MessageObj:
		for _, handle := range MessageHandles {
			if handle.IsMatch(msg.Message) {
				if err := handle.Handler(packMessage(handle.Client, msg)); err != nil {
					handle.Client.Logger.Println("RPC Error:", err)
				}
			}
		}
	case *MessageService:
		fmt.Println("MessageService")
	}
}

func HandleEditUpdate(update Message) {
	if len(MessageEdit) == 0 {
		return
	}
	switch msg := update.(type) {
	case *MessageObj:
		for _, handle := range MessageEdit {
			if handle.IsMatch(msg.Message) {
				if err := handle.Handler(packMessage(handle.Client, msg)); err != nil {
					handle.Client.Logger.Println("RPC Error:", err)
				}
			}
		}
	}
}

func HandleInlineUpdate(update *UpdateBotInlineQuery) {
	if len(InlineHandles) == 0 {
		return
	}
	for _, handle := range InlineHandles {
		if handle.IsMatch(update.Query) {
			if err := handle.Handler(packInlineQuery(handle.Client, update)); err != nil {
				handle.Client.Logger.Println("Unhandled Error:", err)
			}
		}
	}
}

func HandleCallbackUpdate(update *UpdateBotCallbackQuery) {
	if len(CallbackHandles) == 0 {
		return
	}
	for _, handle := range CallbackHandles {
		if handle.IsMatch(update.Data) {
			if err := handle.Handler(packCallbackQuery(handle.Client, update)); err != nil {
				handle.Client.Logger.Println("Unhandled Error:", err)
			}
		}
	}
}

func HandleRawUpdate(update Update) {
	if len(RawHandles) == 0 {
		return
	}
	for _, handle := range RawHandles {
		if reflect.TypeOf(handle.updateType) == reflect.TypeOf(update) {
			if err := handle.Handler(update); err != nil {
				handle.Client.Logger.Println("Unhandled Error:", err)
			}
		}
	}
}

func (h *InlineHandle) IsMatch(text string) bool {
	switch pattern := h.Pattern.(type) {
	case string:
		p := regexp.MustCompile("^" + pattern)
		return p.MatchString(text) || strings.HasPrefix(text, pattern)
	case *regexp.Regexp:
		return pattern.MatchString(text)
	default:
		return false
	}
}

func (e *MessageEditHandle) IsMatch(text string) bool {
	switch pattern := e.Pattern.(type) {
	case string:
		if pattern == OnEditMessage {
			return true
		}
		p := regexp.MustCompile("^" + pattern)
		return p.MatchString(text) || strings.HasPrefix(text, pattern)
	case *regexp.Regexp:
		return pattern.MatchString(text)
	default:
		return false
	}
}

func (h *CallbackHandle) IsMatch(data []byte) bool {
	switch pattern := h.Pattern.(type) {
	case string:
		p := regexp.MustCompile("^" + pattern)
		return p.Match(data) || strings.HasPrefix(string(data), pattern)
	case *regexp.Regexp:
		return pattern.Match(data)
	default:
		return false
	}
}

func (h *MessageHandle) IsMatch(text string) bool {
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
		panic(fmt.Sprintf("unknown handler type %s", reflect.TypeOf(Pattern).String()))
	}
}

func (c *Client) AddMessageHandler(pattern interface{}, handler func(m *NewMessage) error) {
	MessageHandles = append(MessageHandles, MessageHandle{pattern, handler, c})
}

func (c *Client) AddEditHandler(pattern interface{}, handler func(m *NewMessage) error) {
	MessageEdit = append(MessageEdit, MessageEditHandle{pattern, handler, c})
}

func (c *Client) AddInlineHandler(pattern interface{}, handler func(m *InlineQuery) error) {
	InlineHandles = append(InlineHandles, InlineHandle{pattern, handler, c})
}

func (c *Client) AddCallbackHandler(pattern interface{}, handler func(m *CallbackQuery) error) {
	CallbackHandles = append(CallbackHandles, CallbackHandle{pattern, handler, c})
}

func (c *Client) AddRawHandler(updateType Update, handler func(m Update) error) {
	RawHandles = append(RawHandles, RawHandle{updateType, handler, c})
}

func (c *Client) RemoveEventHandler(pattern string) {
	for i, p := range MessageHandles {
		if p.Pattern == pattern {
			MessageHandles = append(MessageHandles[:i], MessageHandles[i+1:]...)
			return
		}
	}
}

// Sort and Handle all the Incoming Updates
// Many more types to be added
func HandleUpdate(u interface{}) bool {
UpdateTypeSwitching:
	switch upd := u.(type) {
	case *UpdatesObj:
		cache.UpdatePeersToCache(upd.Users, upd.Chats)
		for _, update := range upd.Updates {
			switch update := update.(type) {
			case *UpdateNewMessage:
				go func() { HandleMessageUpdate(update.Message) }()
			case *UpdateNewChannelMessage:
				go func() { HandleMessageUpdate(update.Message) }()
			case *UpdateEditMessage:
				go func() { HandleEditUpdate(update.Message) }()
			case *UpdateEditChannelMessage:
				go func() { HandleEditUpdate(update.Message) }()
			case *UpdateBotInlineQuery:
				go func() { HandleInlineUpdate(update) }()
			case *UpdateBotCallbackQuery:
				go func() { HandleCallbackUpdate(update) }()
			default:
				go func() { HandleRawUpdate(update) }()
			}
		}
	case *UpdateShort:
		switch upd := upd.Update.(type) {
		case *UpdateNewMessage:
			go func() { HandleMessageUpdate(upd.Message) }()
		case *UpdateNewChannelMessage:
			go func() { HandleMessageUpdate(upd.Message) }()
		}
	case *UpdateShortMessage:
		go func() {
			HandleMessageUpdate(&MessageObj{Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.UserID), PeerID: getPeerUser(upd.UserID), Date: upd.Date, Entities: upd.Entities})
		}()
	case *UpdateShortChatMessage:
		go func() {
			HandleMessageUpdate(&MessageObj{Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.FromID), PeerID: getPeerUser(upd.ChatID), Date: upd.Date, Entities: upd.Entities})
		}()
	case *UpdateShortSentMessage:
		go func() {
			HandleMessageUpdate(&MessageObj{Out: upd.Out, Date: upd.Date, Media: upd.Media, Entities: upd.Entities})
		}()
	case *UpdatesCombined:
		u = upd.Updates
		cache.UpdatePeersToCache(upd.Users, upd.Chats)
		goto UpdateTypeSwitching
	case *UpdatesTooLong:
	default:
		log.Println("unknown update type", reflect.TypeOf(u).String())
	}
	return true
}
