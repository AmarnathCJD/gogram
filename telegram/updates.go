package telegram

import (
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"
)

var (
	HANDLERS        = []Handle{}
	MessageHandles  = []Handle{}
	OnNewMessage    = "OnNewMessage"
	OnChatAction    = "OnChatAction"
	OnInlineQuery   = "OnInlineQuery"
	OnCallbackQuery = "OnCallbackQuery"
)

type (
	Handle struct {
		Pattern interface{}
		Handler func(m *NewMessage) error
		Client  *Client
	}

	ChatAction struct {
		Client *Client
		ID     int32
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
				if err := handle.Handler(PackMessage(handle.Client, msg)); err != nil {
					handle.Client.Logger.Println("RPC Error:", err)
				}
			}
		}
	case *MessageService:
		fmt.Println("MessageService")
	}
}

func (h *Handle) IsMatch(text string) bool {
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

func (c *Client) AddEventHandler(pattern interface{}, handler func(m *NewMessage) error) {
	MessageHandles = append(MessageHandles, Handle{pattern, handler, c})
}

func (c *Client) RemoveEventHandler(pattern string) {
	for i, p := range HANDLERS {
		if p.Pattern == pattern {
			HANDLERS = append(HANDLERS[:i], HANDLERS[i+1:]...)
			return
		}
	}
}

// Sort and Handle all the Incoming Updates
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
			case *UpdateBotInlineQuery:
			case *UpdateBotInlineSend:
			case *UpdateBotCallbackQuery:
			case *UpdateBotShippingQuery:
			case *UpdateBotPrecheckoutQuery:
			case *UpdateBotStopped:
			case *UpdateChannel:
			case *UpdateChannelAvailableMessages:
			case *UpdateChannelMessageForwards:
			case *UpdateChannelMessageViews:
			case *UpdateChannelParticipant:
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
