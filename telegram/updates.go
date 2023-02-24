// Copyright (c) 2022, amarnathcjd

package telegram

import (
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
)

const DEF_ALBUM_WAIT_TIME = 600 * time.Millisecond

var (
	UpdateHandleDispatcher = &UpdateDispatcher{}
)

type messageHandle struct {
	Pattern interface{}
	Handler func(m *NewMessage) error
	Filters *Filters
}

type albumBox struct {
	sync.Mutex
	waitExit  chan struct{}
	messages  []*NewMessage
	groupedID int64
}

func (a *albumBox) Wait() {
	time.Sleep(DEF_ALBUM_WAIT_TIME)
	a.waitExit <- struct{}{}
}

func (a *albumBox) Add(m *NewMessage) {
	a.Lock()
	defer a.Unlock()
	a.messages = append(a.messages, m)
}

func (h *messageHandle) Remove() {
	for i, handle := range UpdateHandleDispatcher.messageHandles {
		if reflect.DeepEqual(handle, h) {
			UpdateHandleDispatcher.messageHandles = append(UpdateHandleDispatcher.messageHandles[:i], UpdateHandleDispatcher.messageHandles[i+1:]...)
		}
	}
}

type albumHandle struct {
	Handler func(alb *Album) error
}

func (h *albumHandle) Remove() {
	for i, handle := range UpdateHandleDispatcher.albumHandles {
		if reflect.DeepEqual(handle, h) {
			UpdateHandleDispatcher.albumHandles = append(UpdateHandleDispatcher.albumHandles[:i], UpdateHandleDispatcher.albumHandles[i+1:]...)
		}
	}
}

type chatActionHandle struct {
	Handler func(m *NewMessage) error
}

func (h *chatActionHandle) Remove() {
	for i, handle := range UpdateHandleDispatcher.actionHandles {
		if reflect.DeepEqual(handle, h) {
			UpdateHandleDispatcher.actionHandles = append(UpdateHandleDispatcher.actionHandles[:i], UpdateHandleDispatcher.actionHandles[i+1:]...)
		}
	}
}

type messageEditHandle struct {
	Pattern interface{}
	Handler func(m *NewMessage) error
}

func (h *messageEditHandle) Remove() {
	for i, handle := range UpdateHandleDispatcher.messageEditHandles {
		if reflect.DeepEqual(handle, h) {
			UpdateHandleDispatcher.messageEditHandles = append(UpdateHandleDispatcher.messageEditHandles[:i], UpdateHandleDispatcher.messageEditHandles[i+1:]...)
		}
	}
}

type messageDeleteHandle struct {
	Pattern interface{}
	Handler func(m *UpdateDeleteMessages) error
}

func (h *messageDeleteHandle) Remove() {
	for i, handle := range UpdateHandleDispatcher.messageDeleteHandles {
		if reflect.DeepEqual(handle, h) {
			UpdateHandleDispatcher.messageDeleteHandles = append(UpdateHandleDispatcher.messageDeleteHandles[:i], UpdateHandleDispatcher.messageDeleteHandles[i+1:]...)
		}
	}
}

type inlineHandle struct {
	Pattern interface{}
	Handler func(m *InlineQuery) error
}

func (h *inlineHandle) Remove() {
	for i, handle := range UpdateHandleDispatcher.inlineHandles {
		if reflect.DeepEqual(handle, h) {
			UpdateHandleDispatcher.inlineHandles = append(UpdateHandleDispatcher.inlineHandles[:i], UpdateHandleDispatcher.inlineHandles[i+1:]...)
		}
	}
}

type callbackHandle struct {
	Pattern interface{}
	Handler func(m *CallbackQuery) error
}

func (h *callbackHandle) Remove() {
	for i, handle := range UpdateHandleDispatcher.callbackHandles {
		if reflect.DeepEqual(handle, h) {
			UpdateHandleDispatcher.callbackHandles = append(UpdateHandleDispatcher.callbackHandles[:i], UpdateHandleDispatcher.callbackHandles[i+1:]...)
		}
	}
}

type participantHandle struct {
	Handler func(p *ParticipantUpdate) error
}

func (h *participantHandle) Remove() {
	for i, handle := range UpdateHandleDispatcher.participantHandles {
		if reflect.DeepEqual(handle, h) {
			UpdateHandleDispatcher.participantHandles = append(UpdateHandleDispatcher.participantHandles[:i], UpdateHandleDispatcher.participantHandles[i+1:]...)
		}
	}
}

type rawHandle struct {
	updateType Update
	Handler    func(m Update) error
}

func (h *rawHandle) Remove() {
	for i, handle := range UpdateHandleDispatcher.rawHandles {
		if reflect.DeepEqual(handle, h) {
			UpdateHandleDispatcher.rawHandles = append(UpdateHandleDispatcher.rawHandles[:i], UpdateHandleDispatcher.rawHandles[i+1:]...)
		}
	}
}

type UpdateDispatcher struct {
	client               *Client
	messageHandles       []messageHandle
	inlineHandles        []inlineHandle
	callbackHandles      []callbackHandle
	participantHandles   []participantHandle
	messageEditHandles   []messageEditHandle
	actionHandles        []chatActionHandle
	messageDeleteHandles []messageDeleteHandle
	albumHandles         []albumHandle
	rawHandles           []rawHandle
}

func (u *UpdateDispatcher) AddM(m messageHandle) messageHandle {
	u.messageHandles = append(u.messageHandles, m)
	return m
}

func (u *UpdateDispatcher) AddAL(a albumHandle) albumHandle {
	u.albumHandles = append(u.albumHandles, a)
	return a
}

func (u *UpdateDispatcher) AddI(i inlineHandle) inlineHandle {
	u.inlineHandles = append(u.inlineHandles, i)
	return i
}

func (u *UpdateDispatcher) AddC(c callbackHandle) callbackHandle {
	u.callbackHandles = append(u.callbackHandles, c)
	return c
}

func (u *UpdateDispatcher) AddA(a chatActionHandle) chatActionHandle {
	u.actionHandles = append(u.actionHandles, a)
	return a
}

func (u *UpdateDispatcher) AddME(m messageEditHandle) messageEditHandle {
	u.messageEditHandles = append(u.messageEditHandles, m)
	return m
}

func (u *UpdateDispatcher) AddMD(m messageDeleteHandle) messageDeleteHandle {
	u.messageDeleteHandles = append(u.messageDeleteHandles, m)
	return m
}

func (u *UpdateDispatcher) AddP(p participantHandle) participantHandle {
	u.participantHandles = append(u.participantHandles, p)
	return p
}

func (u *UpdateDispatcher) AddR(r rawHandle) rawHandle {
	u.rawHandles = append(u.rawHandles, r)
	return r
}

func (u *UpdateDispatcher) HandleMessageUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		if msg.GroupedID != 0 {
			u.HandleAlbum(*msg)
		}
		for _, handle := range u.messageHandles {
			if handle.IsMatch(msg.Message) {
				go func(h messageHandle) {
					m := packMessage(u.client, msg)
					if h.runFilterChain(m) {
						if err := h.Handler(m); err != nil {
							u.client.Log.Error(err)
						}
					}
				}(handle)
			}
		}
	case *MessageService:
		for _, handle := range u.actionHandles {
			go func(h chatActionHandle) {
				if err := h.Handler(packMessage(u.client, msg)); err != nil {
					u.client.Log.Error(err)
				}
			}(handle)
		}
	}
}

var (
	ErrInvalidUpdateType = errors.New("invalid update type")
	activeAlbums         = make(map[int64]*albumBox)
)

func (u *UpdateDispatcher) HandleAlbum(message MessageObj) {
	if group, ok := activeAlbums[message.GroupedID]; ok {
		group.Add(packMessage(u.client, &message))
	} else {
		abox := &albumBox{
			waitExit:  make(chan struct{}),
			messages:  []*NewMessage{packMessage(u.client, &message)},
			groupedID: message.GroupedID,
		}
		activeAlbums[message.GroupedID] = abox
		go func() {
			<-abox.waitExit
			for _, handle := range u.albumHandles {
				go func(h albumHandle) {
					if err := h.Handler(&Album{
						GroupedID: abox.groupedID,
						Messages:  abox.messages,
						Client:    u.client,
					}); err != nil {
						u.client.Log.Error(err)
					}
				}(handle)
			}
			delete(activeAlbums, message.GroupedID)
		}()
		go abox.Wait()
	}
}

func (u *UpdateDispatcher) HandleMessageUpdateW(_ Message, pts int32) {
	m, err := u.client.GetDiffrence(pts, 1)
	if err != nil {
		u.client.Log.Error(err)
	}
	if m != nil {
		u.HandleMessageUpdate(m)
	}
}

func (u *UpdateDispatcher) HandleEditUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		for _, handle := range u.messageEditHandles {
			if handle.IsMatch(msg.Message) {
				go func(h messageEditHandle) {
					if err := h.Handler(packMessage(u.client, msg)); err != nil {
						u.client.Log.Error(err)
					}
				}(handle)
			}
		}
	}
}

func (u *UpdateDispatcher) HandleCallbackUpdate(update *UpdateBotCallbackQuery) {
	for _, handle := range u.callbackHandles {
		if handle.IsMatch(update.Data) {
			go func(h callbackHandle) {
				if err := h.Handler(packCallbackQuery(u.client, update)); err != nil {
					u.client.Log.Error(err)
				}
			}(handle)
		}
	}
}

func (u *UpdateDispatcher) HandleParticipantUpdate(update *UpdateChannelParticipant) {
	for _, handle := range u.participantHandles {
		go func(h participantHandle) {
			if err := h.Handler(packChannelParticipant(u.client, update)); err != nil {
				u.client.Log.Error(err)
			}
		}(handle)
	}
}

func (u *UpdateDispatcher) HandleInlineUpdate(update *UpdateBotInlineQuery) {
	for _, handle := range u.inlineHandles {
		if handle.IsMatch(update.Query) {
			go func(h inlineHandle) {
				if err := h.Handler(packInlineQuery(u.client, update)); err != nil {
					u.client.Log.Error(err)
				}
			}(handle)
		}
	}
}

func (u *UpdateDispatcher) HandleDeleteUpdate(update *UpdateDeleteMessages) {
	for _, handle := range u.messageDeleteHandles {
		go func(h messageDeleteHandle) {
			if err := h.Handler(update); err != nil {
				u.client.Log.Error(err)
			}
		}(handle)
	}
}

func (u *UpdateDispatcher) HandleRawUpdate(update Update) {
	for _, handle := range u.rawHandles {
		if reflect.TypeOf(update) == reflect.TypeOf(handle.updateType) {
			go func(h rawHandle) {
				if err := h.Handler(update); err != nil {
					u.client.Log.Error(err)
				}
			}(handle)
		}
	}
}

func (h *inlineHandle) IsMatch(text string) bool {
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

func (e *messageEditHandle) IsMatch(text string) bool {
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

func (h *callbackHandle) IsMatch(data []byte) bool {
	switch pattern := h.Pattern.(type) {
	case string:
		if pattern == OnCallbackQuery {
			return true
		}
		p := regexp.MustCompile(pattern)
		return p.Match(data) || strings.HasPrefix(string(data), pattern)
	case *regexp.Regexp:
		return pattern.Match(data)
	default:
		return false
	}
}

func (h *messageHandle) IsMatch(text string) bool {
	switch Pattern := h.Pattern.(type) {
	case string:
		if Pattern == OnNewMessage {
			return true
		}
		pattern := regexp.MustCompile("^" + Pattern)
		return pattern.MatchString(text) || strings.HasPrefix(text, Pattern)
	case *regexp.Regexp:
		return Pattern.MatchString(text)
	}
	return false
}

func (h *messageHandle) runFilterChain(m *NewMessage) bool {
	if h.Filters == nil {
		return true
	}
	if h.Filters.Outgoing && !m.Message.Out || h.Filters.Incoming && !m.Message.Out || h.Filters.IsPrivate && !m.IsPrivate() || h.Filters.IsGroup && !m.IsGroup() || h.Filters.IsChannel && !m.IsChannel() || h.Filters.IsMedia && !m.IsMedia() || h.Filters.IsCommand && !m.IsCommand() || h.Filters.IsReply && !m.IsReply() || h.Filters.IsForward && !m.IsForward() {
		return false
	}
	if h.Filters.Func != nil && !h.Filters.Func(m) {
		return false
	}
	if len(h.Filters.Chats) > 0 {
		for _, chat := range h.Filters.Chats {
			if !h.Filters.Blacklist && chat != m.ChatID() || h.Filters.Blacklist && chat == m.ChatID() {
				return false
			}
		}
	}
	if len(h.Filters.Users) > 0 {
		for _, user := range h.Filters.Users {
			if !h.Filters.Blacklist && user != m.SenderID() || h.Filters.Blacklist && user == m.SenderID() {
				return false
			}
		}
	}
	return true
}

type Filters struct {
	IsPrivate bool                   `json:"is_private,omitempty"`
	IsGroup   bool                   `json:"is_group,omitempty"`
	IsChannel bool                   `json:"is_channel,omitempty"`
	IsCommand bool                   `json:"is_command,omitempty"`
	IsReply   bool                   `json:"is_reply,omitempty"`
	IsForward bool                   `json:"is_forward,omitempty"`
	IsText    bool                   `json:"is_text,omitempty"`
	IsMedia   bool                   `json:"is_media,omitempty"`
	Func      func(*NewMessage) bool `json:"func,omitempty"`
	Chats     []int64                `json:"chats,omitempty"`
	Users     []int64                `json:"users,omitempty"`
	Blacklist bool                   `json:"blacklist,omitempty"`
	Outgoing  bool                   `json:"outgoing,omitempty"`
	Incoming  bool                   `json:"incoming,omitempty"`
}

func (c *Client) AddMessageHandler(pattern interface{}, handler func(m *NewMessage) error, filters ...*Filters) messageHandle {
	return UpdateHandleDispatcher.AddM(messageHandle{Pattern: pattern, Handler: handler, Filters: getVariadic(filters, &Filters{}).(*Filters)})
}

func (c *Client) AddAlbumHandler(handler func(m *Album) error) albumHandle {
	return UpdateHandleDispatcher.AddAL(albumHandle{Handler: handler})
}

func (c *Client) AddActionHandler(handler func(m *NewMessage) error) chatActionHandle {
	return UpdateHandleDispatcher.AddA(chatActionHandle{Handler: handler})
}

// Handle updates categorized as "UpdateMessageEdited"
//
// Included Updates:
//   - Message Edited
//   - Channel Post Edited
func (c *Client) AddEditHandler(pattern interface{}, handler func(m *NewMessage) error) messageEditHandle {
	return UpdateHandleDispatcher.AddME(messageEditHandle{Pattern: pattern, Handler: handler})
}

// Handle updates categorized as "UpdateBotInlineQuery"
//
// Included Updates:
//   - Inline Query
func (c *Client) AddInlineHandler(pattern interface{}, handler func(m *InlineQuery) error) inlineHandle {
	return UpdateHandleDispatcher.AddI(inlineHandle{Pattern: pattern, Handler: handler})
}

// Handle updates categorized as "UpdateBotCallbackQuery"
//
// Included Updates:
//   - Callback Query
func (c *Client) AddCallbackHandler(pattern interface{}, handler func(m *CallbackQuery) error) callbackHandle {
	return UpdateHandleDispatcher.AddC(callbackHandle{Pattern: pattern, Handler: handler})
}

// Handle updates categorized as "UpdateChannelParticipant"
//
// Included Updates:
//   - New Channel Participant
//   - Banned Channel Participant
//   - Left Channel Participant
//   - Kicked Channel Participant
//   - Channel Participant Admin
//   - Channel Participant Creator
func (c *Client) AddParticipantHandler(handler func(m *ParticipantUpdate) error) participantHandle {
	return UpdateHandleDispatcher.AddP(participantHandle{Handler: handler})
}

func (c *Client) AddRawHandler(updateType Update, handler func(m Update) error) rawHandle {
	return UpdateHandleDispatcher.AddR(rawHandle{updateType: updateType, Handler: handler})
}

// Sort and Handle all the Incoming Updates
// Many more types to be added
func HandleIncomingUpdates(u interface{}) bool {
UpdateTypeSwitching:
	switch upd := u.(type) {
	case *UpdatesObj:
		go cache.UpdatePeersToCache(upd.Users, upd.Chats)
		for _, update := range upd.Updates {
			switch update := update.(type) {
			case *UpdateNewMessage:
				go UpdateHandleDispatcher.HandleMessageUpdate(update.Message)
			case *UpdateNewChannelMessage:
				go UpdateHandleDispatcher.HandleMessageUpdate(update.Message)
			case *UpdateNewScheduledMessage:
				go UpdateHandleDispatcher.HandleMessageUpdate(update.Message)
			case *UpdateEditMessage:
				go UpdateHandleDispatcher.HandleEditUpdate(update.Message)
			case *UpdateEditChannelMessage:
				go UpdateHandleDispatcher.HandleEditUpdate(update.Message)
			case *UpdateBotInlineQuery:
				go UpdateHandleDispatcher.HandleInlineUpdate(update)
			case *UpdateBotCallbackQuery:
				go UpdateHandleDispatcher.HandleCallbackUpdate(update)
			case *UpdateChannelParticipant:
				go UpdateHandleDispatcher.HandleParticipantUpdate(update)
			default:
				go UpdateHandleDispatcher.HandleRawUpdate(update)
			}
		}
	case *UpdateShort:
		switch upd := upd.Update.(type) {
		case *UpdateNewMessage:
			go UpdateHandleDispatcher.HandleMessageUpdateW(upd.Message, upd.Pts)
		case *UpdateNewChannelMessage:
			go UpdateHandleDispatcher.HandleMessageUpdateW(upd.Message, upd.Pts)
		}
	case *UpdateShortMessage:
		go UpdateHandleDispatcher.HandleMessageUpdateW(&MessageObj{Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.UserID), PeerID: getPeerUser(upd.UserID), Date: upd.Date, Entities: upd.Entities}, upd.Pts)
	case *UpdateShortChatMessage:
		go UpdateHandleDispatcher.HandleMessageUpdateW(&MessageObj{Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.FromID), PeerID: getPeerUser(upd.ChatID), Date: upd.Date, Entities: upd.Entities}, upd.Pts)
	case *UpdateShortSentMessage:
		go UpdateHandleDispatcher.HandleMessageUpdateW(&MessageObj{Out: upd.Out, Date: upd.Date, Media: upd.Media, Entities: upd.Entities}, upd.Pts)
	case *UpdatesCombined:
		u = upd.Updates
		go cache.UpdatePeersToCache(upd.Users, upd.Chats)
		goto UpdateTypeSwitching
	case *UpdatesTooLong:
	default:
		UpdateHandleDispatcher.client.Log.Warn(ErrInvalidUpdateType, reflect.TypeOf(u))
	}
	return true
}

func (c *Client) GetDiffrence(Pts int32, Limit int32) (Message, error) {
	c.Logger.Debug("Getting diffrence for", Pts)
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
