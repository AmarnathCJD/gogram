// Copyright (c) 2022, amarnathcjd

package telegram

import (
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

const DEF_ALBUM_WAIT_TIME = 600 * time.Millisecond

var (
	UpdateHandleDispatcher = &UpdateDispatcher{}
	activeAlbums           = make(map[int64]*albumBox)
)

type messageHandle struct {
	Pattern interface{}
	Handler func(m *NewMessage) error
	Filters []Filter
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

type albumBox struct {
	sync.Mutex
	waitExit  chan struct{}
	messages  []*NewMessage
	groupedId int64
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

type inlineCallbackHandle struct {
	Pattern interface{}
	Handler func(m *InlineCallbackQuery) error
}

func (h *inlineCallbackHandle) Remove() {
	for i, handle := range UpdateHandleDispatcher.inlineCallbackHandles {
		if reflect.DeepEqual(handle, h) {
			UpdateHandleDispatcher.inlineCallbackHandles = append(UpdateHandleDispatcher.inlineCallbackHandles[:i], UpdateHandleDispatcher.inlineCallbackHandles[i+1:]...)
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
	client                *Client
	messageHandles        []messageHandle
	inlineHandles         []inlineHandle
	callbackHandles       []callbackHandle
	inlineCallbackHandles []inlineCallbackHandle
	participantHandles    []participantHandle
	messageEditHandles    []messageEditHandle
	actionHandles         []chatActionHandle
	messageDeleteHandles  []messageDeleteHandle
	albumHandles          []albumHandle
	rawHandles            []rawHandle
}

func (u *UpdateDispatcher) HandleMessageUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		if msg.GroupedID != 0 {
			u.HandleAlbum(*msg)
		}
		for _, handler := range u.messageHandles {
			if handler.IsMatch(msg.Message) {
				go func(h messageHandle) {
					m := packMessage(u.client, msg)
					if h.runFilterChain(m) {
						defer u.client.NewRecovery()()
						if err := h.Handler(m); err != nil {
							u.client.Log.Error(err)
						}
					}
				}(handler)
			}
		}
	case *MessageService:
		for _, handler := range u.actionHandles {
			go func(h chatActionHandle) {
				defer u.client.NewRecovery()()
				if err := h.Handler(packMessage(u.client, msg)); err != nil {
					u.client.Log.Error(err)
				}
			}(handler)
		}
	}
}

func (u *UpdateDispatcher) HandleAlbum(message MessageObj) {
	if group, ok := activeAlbums[message.GroupedID]; ok {
		group.Add(packMessage(u.client, &message))
	} else {
		abox := &albumBox{
			waitExit:  make(chan struct{}),
			messages:  []*NewMessage{packMessage(u.client, &message)},
			groupedId: message.GroupedID,
		}
		activeAlbums[message.GroupedID] = abox
		go func() {
			<-abox.waitExit
			for _, handle := range u.albumHandles {
				go func(h albumHandle) {
					if err := h.Handler(&Album{
						GroupedID: abox.groupedId,
						Messages:  abox.messages,
						Client:    u.client,
					}); err != nil {
						u.client.Log.Error("updates.Dispatcher.Album - ", err)
					}
				}(handle)
			}
			delete(activeAlbums, message.GroupedID)
		}()
		go abox.Wait()
	}
}

func (u *UpdateDispatcher) HandleMessageUpdateW(_ Message, pts int32) {
	updatedMessage, err := u.client.GetDifference(pts, 1)
	if err != nil {
		u.client.Log.Error("updates.Dispatcher.GetDifference -", err)
	}
	if updatedMessage != nil {
		u.HandleMessageUpdate(updatedMessage)
	}
}

func (u *UpdateDispatcher) HandleEditUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		for _, handle := range u.messageEditHandles {
			if handle.IsMatch(msg.Message) {
				go func(h messageEditHandle) {
					defer u.client.NewRecovery()()
					if err := h.Handler(packMessage(u.client, msg)); err != nil {
						u.client.Log.Error("updates.dispatcher.EditMessage -", err)
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
				defer u.client.NewRecovery()()
				if err := h.Handler(packCallbackQuery(u.client, update)); err != nil {
					u.client.Log.Error("updates.dispatcher.CallbackQuery -", err)
				}
			}(handle)
		}
	}
}

func (u *UpdateDispatcher) HandleInlineCallbackUpdate(update *UpdateInlineBotCallbackQuery) {
	for _, handle := range u.inlineCallbackHandles {
		if handle.IsMatch(update.Data) {
			go func(h inlineCallbackHandle) {
				defer u.client.NewRecovery()()
				if err := h.Handler(packInlineCallbackQuery(u.client, update)); err != nil {
					u.client.Log.Error("updates.dispatcher.InlineCallbackQuery -", err)
				}
			}(handle)
		}
	}
}

func (u *UpdateDispatcher) HandleParticipantUpdate(update *UpdateChannelParticipant) {
	for _, handle := range u.participantHandles {
		go func(h participantHandle) {
			defer u.client.NewRecovery()()
			if err := h.Handler(packChannelParticipant(u.client, update)); err != nil {
				u.client.Log.Error("updates.dispatcher.ParticipantUpdate -", err)
			}
		}(handle)
	}
}

func (u *UpdateDispatcher) HandleInlineUpdate(update *UpdateBotInlineQuery) {
	for _, handle := range u.inlineHandles {
		if handle.IsMatch(update.Query) {
			go func(h inlineHandle) {
				defer u.client.NewRecovery()()
				if err := h.Handler(packInlineQuery(u.client, update)); err != nil {
					u.client.Log.Error("updates.dispatcher.InlineQuery -", err)
				}
			}(handle)
		}
	}
}

func (u *UpdateDispatcher) HandleDeleteUpdate(update *UpdateDeleteMessages) {
	for _, handle := range u.messageDeleteHandles {
		go func(h messageDeleteHandle) {
			defer u.client.NewRecovery()()
			if err := h.Handler(update); err != nil {
				u.client.Log.Error("updates.dispatcher.DeleteUpdate -", err)
			}
		}(handle)
	}
}

func (u *UpdateDispatcher) HandleRawUpdate(update Update) {
	for _, handle := range u.rawHandles {
		if reflect.TypeOf(update) == reflect.TypeOf(handle.updateType) {
			go func(h rawHandle) {
				defer u.client.NewRecovery()()
				if err := h.Handler(update); err != nil {
					u.client.Log.Error("updates.dispatcher.RawUpdate -", err)
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
		if !strings.HasPrefix(pattern, "^") {
			pattern = "^" + pattern
		}
		return regexp.MustCompile(pattern).MatchString(text) || strings.HasPrefix(text, pattern)
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

func (h *inlineCallbackHandle) IsMatch(data []byte) bool {
	switch pattern := h.Pattern.(type) {
	case string:
		if pattern == OnInlineCallbackQuery {
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
	var (
		actAsBlacklist      bool
		actUsers, actGroups []int64
		inSlice             = func(e int64, s []int64) bool {
			for _, a := range s {
				if a == e {
					return true
				}
			}
			return false
		}
	)

	if h.Filters != nil && len(h.Filters) > 0 {
		for _, filter := range h.Filters {
			if filter.Private && !m.IsPrivate() || filter.Group && !m.IsGroup() || filter.Channel && !m.IsChannel() {
				return false
			}
			if filter.Media && !m.IsMedia() || filter.Command && !m.IsCommand() || filter.Reply && !m.IsReply() || filter.Forward && !m.IsForward() {
				return false
			}
			if filter.FromBot {
				if m.Sender == nil || !m.Sender.Bot {
					return false
				}
			}
			if filter.Users != nil && len(filter.Users) > 0 {
				actUsers = filter.Users
			}
			if filter.Chats != nil && len(filter.Chats) > 0 {
				actGroups = filter.Chats
			}
			if filter.Blacklist {
				actAsBlacklist = true
			}
		}
	}

	var peerCheckPassed bool

	if inSlice(m.SenderID(), actUsers) {
		if actAsBlacklist {
			return false
		}
		peerCheckPassed = true
	}
	if inSlice(m.ChatID(), actGroups) {
		if actAsBlacklist {
			return false
		}
		peerCheckPassed = true
	}

	if !actAsBlacklist && (len(actUsers) > 0 || len(actGroups) > 0) && !peerCheckPassed {
		return false
	}

	return true
}

type Filter struct {
	Private, Group, Channel, Media, Command, Reply, Forward, FromBot, Blacklist bool
	Users, Chats                                                                []int64
}

var (
	FilterPrivate   = Filter{Private: true}
	FilterGroup     = Filter{Group: true}
	FilterChannel   = Filter{Channel: true}
	FilterMedia     = Filter{Media: true}
	FilterCommand   = Filter{Command: true}
	FilterReply     = Filter{Reply: true}
	FilterForward   = Filter{Forward: true}
	FilterFromBot   = Filter{FromBot: true}
	FilterBlacklist = Filter{Blacklist: true}
	FilterUsers     = func(users ...int64) Filter {
		return Filter{Users: users}
	}
	FilterChats = func(chats ...int64) Filter {
		return Filter{Chats: chats}
	}
)

func (*Client) AddMessageHandler(pattern interface{}, handler func(m *NewMessage) error, filters ...Filter) messageHandle {
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}
	handle := messageHandle{Pattern: pattern, Handler: handler, Filters: messageFilters}
	UpdateHandleDispatcher.messageHandles = append(UpdateHandleDispatcher.messageHandles, handle)
	return handle
}

func (*Client) AddAlbumHandler(handler func(m *Album) error) albumHandle {
	UpdateHandleDispatcher.albumHandles = append(UpdateHandleDispatcher.albumHandles, albumHandle{Handler: handler})
	return albumHandle{Handler: handler}
}

func (*Client) AddActionHandler(handler func(m *NewMessage) error) chatActionHandle {
	UpdateHandleDispatcher.actionHandles = append(UpdateHandleDispatcher.actionHandles, chatActionHandle{Handler: handler})
	return chatActionHandle{Handler: handler}
}

// Handle updates categorized as "UpdateMessageEdited"
//
// Included Updates:
//   - Message Edited
//   - Channel Post Edited
func (*Client) AddEditHandler(pattern interface{}, handler func(m *NewMessage) error) messageEditHandle {
	handle := messageEditHandle{Pattern: pattern, Handler: handler}
	UpdateHandleDispatcher.messageEditHandles = append(UpdateHandleDispatcher.messageEditHandles, handle)
	return handle
}

// Handle updates categorized as "UpdateBotInlineQuery"
//
// Included Updates:
//   - Inline Query
func (*Client) AddInlineHandler(pattern interface{}, handler func(m *InlineQuery) error) inlineHandle {
	handle := inlineHandle{Pattern: pattern, Handler: handler}
	UpdateHandleDispatcher.inlineHandles = append(UpdateHandleDispatcher.inlineHandles, handle)
	return handle
}

// Handle updates categorized as "UpdateBotCallbackQuery"
//
// Included Updates:
//   - Callback Query
func (*Client) AddCallbackHandler(pattern interface{}, handler func(m *CallbackQuery) error) callbackHandle {
	handle := callbackHandle{Pattern: pattern, Handler: handler}
	UpdateHandleDispatcher.callbackHandles = append(UpdateHandleDispatcher.callbackHandles, handle)
	return handle
}

// Handle updates categorized as "UpdateInlineBotCallbackQuery"
//
// Included Updates:
//   - Inline Callback Query
func (c *Client) AddInlineCallbackHandler(pattern interface{}, handler func(m *InlineCallbackQuery) error) inlineCallbackHandle {
	handle := inlineCallbackHandle{Pattern: pattern, Handler: handler}
	UpdateHandleDispatcher.inlineCallbackHandles = append(UpdateHandleDispatcher.inlineCallbackHandles, handle)
	return handle
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
func (*Client) AddParticipantHandler(handler func(m *ParticipantUpdate) error) participantHandle {
	handle := participantHandle{Handler: handler}
	UpdateHandleDispatcher.participantHandles = append(UpdateHandleDispatcher.participantHandles, handle)
	return handle
}

func (*Client) AddRawHandler(updateType Update, handler func(m Update) error) rawHandle {
	handle := rawHandle{updateType: updateType, Handler: handler}
	UpdateHandleDispatcher.rawHandles = append(UpdateHandleDispatcher.rawHandles, handle)
	return handle
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
			case *UpdateInlineBotCallbackQuery:
				go UpdateHandleDispatcher.HandleInlineCallbackUpdate(update)
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
		default:
			go UpdateHandleDispatcher.HandleRawUpdate(upd)
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
		UpdateHandleDispatcher.client.Log.Warn("Ignoring Unknown Update Type: ", u)
	}
	return true
}

func (c *Client) GetDifference(Pts int32, Limit int32) (Message, error) {
	c.Logger.Debug("updates.getDifference: [pts: ", Pts, " limit: ", Limit, "]")

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
