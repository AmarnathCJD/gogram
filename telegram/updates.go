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
	ActionHandles   = []ChatActionHandle{}
	RawHandles      = []RawHandle{}
)

func HandleMessageUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		for _, handle := range MessageHandles {
			if handle.IsMatch(msg.Message) {
				m := packMessage(handle.Client, msg)
				if handle.Filters != nil && handle.Filter(m) {
					if err := handle.Handler(m); err != nil {
						handle.Client.Logger.Println(err)
					}
				} else if handle.Filters == nil {
					if err := handle.Handler(m); err != nil {
						handle.Client.Logger.Println(err)
					}
				}
			}
		}
	case *MessageService:
		for _, handle := range ActionHandles {
			if err := handle.Handler(packMessage(handle.Client, msg)); err != nil {
				handle.Client.Logger.Println(err)
			}
		}
	}
}

func HandleEditUpdate(update Message) {
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
	for _, handle := range InlineHandles {
		if handle.IsMatch(update.Query) {
			if err := handle.Handler(packInlineQuery(handle.Client, update)); err != nil {
				handle.Client.Logger.Println("Unhandled Error:", err)
			}
		}
	}
}

func HandleCallbackUpdate(update *UpdateBotCallbackQuery) {
	for _, handle := range CallbackHandles {
		if handle.IsMatch(update.Data) {
			if err := handle.Handler(packCallbackQuery(handle.Client, update)); err != nil {
				handle.Client.Logger.Println("Unhandled Error:", err)
			}
		}
	}
}

func HandleRawUpdate(update Update) {
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
		if pattern == OnInlineQuery {
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
	case *regexp.Regexp:
		return Pattern.MatchString(text)
	default:
		panic(fmt.Sprintf("unknown handler type %s", reflect.TypeOf(Pattern).String()))
	}
}

func (h *MessageHandle) Filter(msg *NewMessage) bool {
	if h.Filters.IsPrivate && !msg.IsPrivate() {
		return false
	}
	if h.Filters.IsGroup && !msg.IsGroup() {
		return false
	}
	if h.Filters.IsChannel && !msg.IsChannel() {
		return false
	}
	if h.Filters.IsCommand && !msg.IsCommand() {
		return false
	}
	if h.Filters.Outgoing && !msg.Message.Out {
		return false
	}
	if h.Filters.Func != nil {
		if !h.Filters.Func(msg) {
			return false
		}
	}
	if len(h.Filters.BlackListChats) > 0 {
		for _, chat := range h.Filters.BlackListChats {
			if msg.ChatID() == chat {
				return false
			}
		}
	}
	if len(h.Filters.WhiteListChats) > 0 {
		for _, chat := range h.Filters.WhiteListChats {
			if msg.ChatID() == chat {
				return true
			}
		}
		return false
	}
	if len(h.Filters.Users) > 0 {
		for _, user := range h.Filters.Users {
			if msg.SenderID() == user {
				return false
			}
		}
	}
	return true
}

func (c *Client) AddMessageHandler(pattern interface{}, handler func(m *NewMessage) error, filters ...*Filters) {
	var FILTER *Filters
	if len(filters) > 0 {
		FILTER = filters[0]
	}
	MessageHandles = append(MessageHandles, MessageHandle{
		Pattern: pattern,
		Handler: handler,
		Filters: FILTER,
		Client:  c,
	})
}

func (c *Client) AddActionHandler(handler func(m *NewMessage) error) {
	ActionHandles = append(ActionHandles, ChatActionHandle{
		Handler: handler,
		Client:  c,
	})
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

func (c *Client) RemoveEventHandler(pattern interface{}) {
	// TODO: implement
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
				go HandleMessageUpdate(update.Message)
			case *UpdateNewChannelMessage:
				go HandleMessageUpdate(update.Message)
			case *UpdateNewScheduledMessage:
				go HandleMessageUpdate(update.Message)
			case *UpdateEditMessage:
				go HandleEditUpdate(update.Message)
			case *UpdateEditChannelMessage:
				go HandleEditUpdate(update.Message)
			case *UpdateBotInlineQuery:
				go HandleInlineUpdate(update)
			case *UpdateBotCallbackQuery:
				go HandleCallbackUpdate(update)
			default:
				go HandleRawUpdate(update)
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
