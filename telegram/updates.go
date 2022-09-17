package telegram

import (
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"
	"time"
)

var (
	MessageHandles  = []MessageHandle{}
	InlineHandles   = []InlineHandle{}
	CallbackHandles = []CallbackHandle{}
	MessageEdit     = []MessageEditHandle{}
	ActionHandles   = []ChatActionHandle{}
	RawHandles      = []RawHandle{}
)

func HandleMessageUpdateWithDiffrence(update Message, Pts int32, Limit int32) {
	if m, ok := update.(*MessageObj); ok {

		for _, handle := range MessageHandles {
			if handle.IsMatch(m.Message) {
				msg, err := handle.Client.getDiffrence(Pts, Limit)

				if err != nil {
					handle.Client.Logger.Println(err)
					return
				} else if msg == nil {
					return
				}

				handleMessage(msg, handle)
			}
		}
	}
}

func (c *Client) getDiffrence(Pts int32, Limit int32) (Message, error) {
	updates, err := c.UpdatesGetDifference(Pts-1, Limit, int32(time.Now().Unix()), 0)
	if err != nil {
		return nil, err
	}
	switch u := updates.(type) {
	case *UpdatesDifferenceObj:
		c.Cache.UpdatePeersToCache(u.Users, u.Chats)
		for _, update := range u.NewMessages {
			switch update.(type) {
			case *MessageObj:
				return update, nil
			}
		}
	case *UpdatesDifferenceSlice:
		c.Cache.UpdatePeersToCache(u.Users, u.Chats)
		return u.NewMessages[0], nil
	default:
		return nil, nil
	}
	return nil, nil
}

func handleMessage(message Message, h MessageHandle) error {
	m := packMessage(h.Client, message)
	if h.Filters != nil && h.Filter(m) {
		if err := h.Handler(m); err != nil {
			h.Client.L.Error(err)
		}
	} else if h.Filters == nil {
		if err := h.Handler(m); err != nil {
			h.Client.L.Error(err)
		}
	}
	return nil
}

func HandleMessageUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		for _, handle := range MessageHandles {
			if handle.IsMatch(msg.Message) {
				handleMessage(msg, handle)
			}
		}
	case *MessageService:
		for _, handle := range ActionHandles {
			if err := handle.Handler(packMessage(handle.Client, msg)); err != nil {
				handle.Client.L.Error(err)
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
					handle.Client.L.Error(err)
				}
			}
		}
	}
}

func HandleInlineUpdate(update *UpdateBotInlineQuery) {
	for _, handle := range InlineHandles {
		if handle.IsMatch(update.Query) {
			if err := handle.Handler(packInlineQuery(handle.Client, update)); err != nil {
				handle.Client.L.Error(err)
			}
		}
	}
}

func HandleCallbackUpdate(update *UpdateBotCallbackQuery) {
	for _, handle := range CallbackHandles {
		if handle.IsMatch(update.Data) {
			if err := handle.Handler(packCallbackQuery(handle.Client, update)); err != nil {
				handle.Client.L.Error(err)
			}
		}
	}
}

func HandleRawUpdate(update Update) {
	for _, handle := range RawHandles {
		if reflect.TypeOf(handle.updateType) == reflect.TypeOf(update) {
			if err := handle.Handler(update); err != nil {
				handle.Client.L.Error(err)
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
			go HandleMessageUpdateWithDiffrence(upd.Message, upd.Pts, upd.PtsCount)
		case *UpdateNewChannelMessage:
			go HandleMessageUpdateWithDiffrence(upd.Message, upd.Pts, upd.PtsCount)
		}
	case *UpdateShortMessage:
		go HandleMessageUpdateWithDiffrence(&MessageObj{Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.UserID), PeerID: getPeerUser(upd.UserID), Date: upd.Date, Entities: upd.Entities}, upd.Pts, upd.PtsCount)
	case *UpdateShortChatMessage:
		go HandleMessageUpdate(&MessageObj{Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.FromID), PeerID: getPeerUser(upd.ChatID), Date: upd.Date, Entities: upd.Entities})
	case *UpdateShortSentMessage:
		go HandleMessageUpdate(&MessageObj{Out: upd.Out, Date: upd.Date, Media: upd.Media, Entities: upd.Entities})
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
