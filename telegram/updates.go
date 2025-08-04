// Copyright (c) 2024, amarnathcjd

package telegram

import (
	"context"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"slices"

	"github.com/amarnathcjd/gogram/internal/utils"
	"github.com/pkg/errors"
)

type MessageHandler func(m *NewMessage) error
type EditHandler func(m *NewMessage) error
type DeleteHandler func(m *DeleteMessage) error
type AlbumHandler func(m *Album) error
type InlineHandler func(m *InlineQuery) error
type InlineSendHandler func(m *InlineSend) error
type CallbackHandler func(m *CallbackQuery) error
type InlineCallbackHandler func(m *InlineCallbackQuery) error
type ParticipantHandler func(m *ParticipantUpdate) error
type PendingJoinHandler func(m *JoinRequestUpdate) error
type RawHandler func(m Update, c *Client) error

var EndGroup = errors.New("[END_GROUP_ERROR] handler propagation ended")

type Handle interface {
	SetGroup(group string) Handle
	GetGroup() string
	SetPriority(priority int) Handle
	GetPriority() int
}

type baseHandle struct {
	Group       string
	priority    int
	sortTrigger chan any
}

func (h *baseHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *baseHandle) GetGroup() string {
	return h.Group
}

func (h *baseHandle) SetPriority(priority int) Handle {
	h.priority = priority
	h.sortTrigger <- h
	return h
}

func (h *baseHandle) GetPriority() int {
	return h.priority
}

type messageHandle struct {
	baseHandle
	Pattern any
	Handler MessageHandler
	Filters []Filter
}

type albumHandle struct {
	baseHandle
	Handler func(alb *Album) error
}

type chatActionHandle struct {
	baseHandle
	Handler MessageHandler
}

type messageEditHandle struct {
	baseHandle
	Pattern any
	Handler MessageHandler
	Filters []Filter
}

type messageDeleteHandle struct {
	baseHandle
	Pattern any
	Handler func(m *DeleteMessage) error
}

type inlineHandle struct {
	baseHandle
	Pattern any
	Handler InlineHandler
}

type inlineSendHandle struct {
	baseHandle
	Handler InlineSendHandler
}

type callbackHandle struct {
	baseHandle
	Pattern any
	Handler CallbackHandler
	Filters []Filter
}

type inlineCallbackHandle struct {
	baseHandle
	Pattern any
	Handler InlineCallbackHandler
}

type participantHandle struct {
	baseHandle
	Handler ParticipantHandler
}

type joinRequestHandle struct {
	baseHandle
	Handler PendingJoinHandler
}

type rawHandle struct {
	baseHandle
	updateType Update
	Handler    RawHandler
}

type albumBox struct {
	sync.Mutex
	messages  []*NewMessage
	groupedId int64
}

func (a *albumBox) WaitAndTrigger(d *UpdateDispatcher, c *Client) {
	time.Sleep(time.Duration(c.clientData.albumWaitTime) * time.Millisecond)

	for gp, handlers := range d.albumHandles {
		for _, handler := range handlers {
			handle := func(h *albumHandle) error {
				sort.SliceStable(a.messages, func(i, j int) bool {
					return a.messages[i].ID < a.messages[j].ID
				})

				return h.Handler(&Album{
					GroupedID: a.groupedId,
					Messages:  a.messages,
					Client:    c,
				})
			}

			if strings.EqualFold(gp, "") || strings.EqualFold(strings.TrimSpace(gp), "default") {
				go func() {
					err := handle(handler)
					if err != nil {
						if errors.Is(err, EndGroup) {
							return
						}
						c.Log.Error(errors.Wrap(err, "[ALBUM_ERROR]"))
					}
				}()
			} else {
				if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
					break
				}
			}
		}
	}

	d.Lock()
	defer d.Unlock()
	delete(d.activeAlbums, a.groupedId)
}

func (a *albumBox) Add(m *NewMessage) {
	a.Lock()
	defer a.Unlock()
	a.messages = append(a.messages, m)
}

type openChat struct { // TODO: Implement this
	accessHash int64
	closeChan  chan struct{}
	lastPts    int32
}

type UpdateDispatcher struct {
	sync.RWMutex
	messageHandles        map[string][]*messageHandle
	inlineHandles         map[string][]*inlineHandle
	inlineSendHandles     map[string][]*inlineSendHandle
	callbackHandles       map[string][]*callbackHandle
	inlineCallbackHandles map[string][]*inlineCallbackHandle
	participantHandles    map[string][]*participantHandle
	joinRequestHandles    map[string][]*joinRequestHandle
	messageEditHandles    map[string][]*messageEditHandle
	actionHandles         map[string][]*chatActionHandle
	messageDeleteHandles  map[string][]*messageDeleteHandle
	albumHandles          map[string][]*albumHandle
	rawHandles            map[string][]*rawHandle
	activeAlbums          map[int64]*albumBox
	sortTrigger           chan any
	logger                *utils.Logger
	openChats             map[int64]*openChat
	nextUpdatesDeadline   time.Time
	currentPts            int32
}

func (d *UpdateDispatcher) SetPts(pts int32) {
	d.Lock()
	defer d.Unlock()
	d.currentPts = pts
}

func (d *UpdateDispatcher) GetPts() int32 {
	d.RLock()
	defer d.RUnlock()
	return d.currentPts
}

// NewUpdateDispatcher creates and populates a new UpdateDispatcher
func (c *Client) NewUpdateDispatcher(sessionName ...string) {
	c.dispatcher = &UpdateDispatcher{
		logger: utils.NewLogger("gogram " + getLogPrefix("dispatcher", getVariadic(sessionName, ""))).
			SetLevel(c.Log.Lev()).
			NoColor(!c.Log.Color()),
	}
	c.dispatcher.SortTrigger()
	//go c.fetchChannelGap()
	c.dispatcher.logger.Debug("dispatcher initialized")
}

// ---------------------------- Dispatcher Functions ----------------------------

// sort generic
func sortGeneric[T Handle](handles map[string][]T) map[string][]T {
	result := make(map[string][]T)

	for _, h := range handles {
		for _, handle := range h {
			group := handle.GetGroup()
			if group == "" {
				group = "default"
			}
			result[group] = append(result[group], handle)
		}
	}

	for group, h := range result {
		sort.SliceStable(h, func(i, j int) bool {
			return h[i].GetPriority() > h[j].GetPriority()
		})
		result[group] = h
	}

	return result
}

func (d *UpdateDispatcher) SortTrigger() {
	if d.sortTrigger == nil {
		d.sortTrigger = make(chan any)
	}

	go func() {
		for range d.sortTrigger {
			d.Lock()
			d.messageHandles = sortGeneric(d.messageHandles)
			d.albumHandles = sortGeneric(d.albumHandles)
			d.actionHandles = sortGeneric(d.actionHandles)
			d.messageEditHandles = sortGeneric(d.messageEditHandles)
			d.inlineHandles = sortGeneric(d.inlineHandles)
			d.inlineSendHandles = sortGeneric(d.inlineSendHandles)
			d.callbackHandles = sortGeneric(d.callbackHandles)
			d.inlineCallbackHandles = sortGeneric(d.inlineCallbackHandles)
			d.participantHandles = sortGeneric(d.participantHandles)
			d.joinRequestHandles = sortGeneric(d.joinRequestHandles)
			d.messageDeleteHandles = sortGeneric(d.messageDeleteHandles)
			d.rawHandles = sortGeneric(d.rawHandles)
			d.Unlock()
		}
	}()
}

func (c *Client) RemoveHandle(handle Handle) error {
	if c.dispatcher == nil {
		return errors.New("[DISPATCHER_NOT_INITIALIZED] dispatcher is not initialized")
	}

	if err := c.removeHandle(handle); err != nil {
		return err
	}

	return nil
}

func (c *Client) removeHandle(handle Handle) error {
	switch h := handle.(type) {
	case *messageHandle:
		removeHandleFromMap(h, c.dispatcher.messageHandles)
	case *inlineHandle:
		removeHandleFromMap(h, c.dispatcher.inlineHandles)
	case *callbackHandle:
		removeHandleFromMap(h, c.dispatcher.callbackHandles)
	case *inlineCallbackHandle:
		removeHandleFromMap(h, c.dispatcher.inlineCallbackHandles)
	case *participantHandle:
		removeHandleFromMap(h, c.dispatcher.participantHandles)
	case *joinRequestHandle:
		removeHandleFromMap(h, c.dispatcher.joinRequestHandles)
	case *messageEditHandle:
		removeHandleFromMap(h, c.dispatcher.messageEditHandles)
	case *chatActionHandle:
		removeHandleFromMap(h, c.dispatcher.actionHandles)
	case *messageDeleteHandle:
		removeHandleFromMap(h, c.dispatcher.messageDeleteHandles)
	case *albumHandle:
		removeHandleFromMap(h, c.dispatcher.albumHandles)
	case *rawHandle:
		removeHandleFromMap(h, c.dispatcher.rawHandles)
	default:
		return errors.New("[INVALID_HANDLE] handle type not supported")
	}

	return nil
}

func removeHandleFromMap[T any](handle T, handlesMap map[string][]T) {
	for key, handles := range handlesMap {
		for i, h := range handles {
			if reflect.DeepEqual(h, handle) {
				handlesMap[key] = slices.Delete(handles, i, i+1)
				return
			}
		}
	}
}

// ---------------------------- Handle Functions ----------------------------

func (c *Client) handleMessageUpdate(update Message, pts ...int32) {
	if len(pts) > 0 {
		if pts[0] == -1 {
			fetchUpdates(c)
			return
		} else {
			if !c.managePts(pts[0], pts[1]) {
				return
			}
		}
	}

	switch msg := update.(type) {
	case *MessageObj:
		if msg.GroupedID != 0 {
			c.handleAlbum(*msg)
		}

		wg := sync.WaitGroup{}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		packed := packMessage(c, msg)

		handle := func(h *messageHandle) error {
			if h.runFilterChain(packed, h.Filters) {
				defer c.NewRecovery()()
				if err := h.Handler(packed); err != nil {
					return err
				}
			}
			return nil
		}

		groupFunc := func(group string, handlers []*messageHandle) {
			defer wg.Done()
			if group == "" || strings.EqualFold(strings.TrimSpace(group), "default") {
				var localWg sync.WaitGroup
				for _, handler := range handlers {
					if handler.IsMatch(msg.Message, c) {
						localWg.Add(1)
						go func(h *messageHandle) {
							defer localWg.Done()
							if err := handle(h); err != nil && !errors.Is(err, EndGroup) {
								c.dispatcher.logger.Error(errors.Wrap(err, "[NEW_MESSAGE_HANDLER_ERROR]"))
							}
						}(handler)
					}
				}
				localWg.Wait()
			} else {
				for _, handler := range handlers {
					if handler.IsMatch(msg.Message, c) {
						if err := handle(handler); err != nil {
							if errors.Is(err, EndGroup) {
								if strings.EqualFold(group, "conversation") {
									cancel()
								}
								break
							}
							c.dispatcher.logger.Error(errors.Wrap(err, "[NEW_MESSAGE_HANDLER_ERROR]"))
						}
					}
				}
			}
		}

		if conv, ok := c.dispatcher.messageHandles["conversation"]; ok {
			wg.Add(1)
			groupFunc("conversation", conv)
		}

		for group, handlers := range c.dispatcher.messageHandles {
			if ctx.Err() != nil {
				break
			}
			if strings.EqualFold(group, "conversation") {
				continue
			}
			if group == "" || strings.EqualFold(strings.TrimSpace(group), "default") {
				wg.Add(1)
				go groupFunc(group, handlers)
			}
		}

		wg.Wait()

	case *MessageService:
		packed := packMessage(c, msg)

		for group, handler := range c.dispatcher.actionHandles {
			for _, h := range handler {
				handle := func(h *chatActionHandle) error {
					defer c.NewRecovery()()
					return h.Handler(packed)
				}

				if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
					go func() {
						err := handle(h)
						if err != nil {
							if errors.Is(err, EndGroup) {
								return
							}
							c.Log.Error(errors.Wrap(err, "[CHAT_ACTION_ERROR]"))
						}
					}()
				} else {
					if err := handle(h); err != nil && errors.Is(err, EndGroup) {
						break
					}
				}
			}
		}
	}
}

func (c *Client) handleAlbum(message MessageObj) {
	packed := packMessage(c, &message)

	c.dispatcher.RLock()
	if group, ok := c.dispatcher.activeAlbums[message.GroupedID]; ok {
		c.dispatcher.RUnlock()
		group.Add(packed)
	} else {
		c.dispatcher.RUnlock()
		albBox := &albumBox{
			messages:  []*NewMessage{packed},
			groupedId: message.GroupedID,
		}
		c.dispatcher.Lock()
		if c.dispatcher.activeAlbums == nil {
			c.dispatcher.activeAlbums = make(map[int64]*albumBox)
		}
		c.dispatcher.activeAlbums[message.GroupedID] = albBox
		c.dispatcher.Unlock()
		albBox.WaitAndTrigger(c.dispatcher, c)
	}
}

func (c *Client) handleMessageUpdateWith(m Message, pts int32) {
	switch msg := m.(type) {
	case *MessageObj:
		if (c.IdInCache(c.GetPeerID(msg.FromID)) || func() bool {
			_, ok := msg.FromID.(*PeerChat)
			return ok
		}()) && (c.IdInCache(c.GetPeerID(msg.PeerID)) || func() bool {
			_, ok := msg.PeerID.(*PeerChat)
			return ok
		}()) {
			c.handleMessageUpdate(msg)
			return
		}
	}
	updatedMessage, err := c.GetDifference(pts, 1)
	if err != nil {
		c.Log.Error(errors.Wrap(err, "[GET_DIFF] failed to get difference"))
	}
	if updatedMessage != nil {
		c.handleMessageUpdate(updatedMessage)
	}
}

func (c *Client) handleEditUpdate(update Message, pts ...int32) {
	if len(pts) > 0 {
		if !c.managePts(pts[0], pts[1]) {
			return
		}
	}

	if msg, ok := update.(*MessageObj); ok {
		packed := packMessage(c, msg)

		for group, handlers := range c.dispatcher.messageEditHandles {
			for _, handler := range handlers {
				if handler.IsMatch(msg.Message) {
					handle := func(h *messageEditHandle) error {
						if handler.runFilterChain(packed, h.Filters) {
							defer c.NewRecovery()()
							return h.Handler(packed)
						}
						return nil
					}

					if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
						go func() {
							err := handle(handler)
							if err != nil {
								if errors.Is(err, EndGroup) {
									return
								}
								c.Log.Error(errors.Wrap(err, "[EDIT_MESSAGE_ERROR]"))
							}
						}()
					} else {
						if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
							break
						}
					}
				}
			}
		}
	}
}

func (c *Client) handleCallbackUpdate(update *UpdateBotCallbackQuery) {
	packed := packCallbackQuery(c, update)

	for group, handlers := range c.dispatcher.callbackHandles {
		for _, handler := range handlers {
			if handler.IsMatch(update.Data) {
				handle := func(h *callbackHandle) error {
					if handler.runFilterChain(packed, h.Filters) {
						defer c.NewRecovery()()
						return h.Handler(packed)
					}
					return nil
				}

				if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
					go func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, EndGroup) {
								return
							}
							c.Log.Error(errors.Wrap(err, "[CALLBACK_QUERY_ERROR]"))
						}
					}()
				} else {
					if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
						break
					}
				}
			}
		}
	}
}

func (c *Client) handleInlineCallbackUpdate(update *UpdateInlineBotCallbackQuery) {
	packed := packInlineCallbackQuery(c, update)

	for group, handlers := range c.dispatcher.inlineCallbackHandles {
		for _, handler := range handlers {
			if handler.IsMatch(update.Data) {
				handle := func(h *inlineCallbackHandle) error {
					defer c.NewRecovery()()
					return h.Handler(packed)
				}

				if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
					go func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, EndGroup) {
								return
							}
							c.Log.Error(errors.Wrap(err, "[INLINE_CALLBACK_ERROR]"))
						}
					}()
				} else {
					if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
						break
					}
				}
			}
		}
	}
}

func (c *Client) handleParticipantUpdate(update *UpdateChannelParticipant) {
	packed := packChannelParticipant(c, update)

	for group, handlers := range c.dispatcher.participantHandles {
		for _, handler := range handlers {
			handle := func(h *participantHandle) error {
				defer c.NewRecovery()()
				return h.Handler(packed)
			}

			if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
				go func() {
					err := handle(handler)
					if err != nil {
						if errors.Is(err, EndGroup) {
							return
						}
						c.Log.Error(errors.Wrap(err, "[PARTICIPANT_UPDATE_ERROR]"))
					}
				}()
			} else {
				if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
					break
				}
			}
		}
	}
}

func (c *Client) handleInlineUpdate(update *UpdateBotInlineQuery) {
	packed := packInlineQuery(c, update)

	for group, handlers := range c.dispatcher.inlineHandles {
		for _, handler := range handlers {
			if handler.IsMatch(update.Query) {
				handle := func(h *inlineHandle) error {
					defer c.NewRecovery()()
					return h.Handler(packed)
				}

				if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
					go func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, EndGroup) {
								return
							}
							c.Log.Error(errors.Wrap(err, "[INLINE_QUERY_ERROR]"))
						}
					}()
				} else {
					if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
						break
					}
				}
			}
		}
	}
}

func (c *Client) handleInlineSendUpdate(update *UpdateBotInlineSend) {
	packed := packInlineSend(c, update)

	for group, handlers := range c.dispatcher.inlineSendHandles {
		for _, handler := range handlers {
			handle := func(h *inlineSendHandle) error {
				defer c.NewRecovery()()
				return h.Handler(packed)
			}

			if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
				go func() {
					err := handle(handler)
					if err != nil {
						if errors.Is(err, EndGroup) {
							return
						}
						c.Log.Error(errors.Wrap(err, "[INLINE_SEND_ERROR]"))
					}
				}()
			} else {
				if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
					break
				}
			}
		}
	}
}

func (c *Client) handleDeleteUpdate(update Update, pts ...int32) {
	if len(pts) > 0 {
		if !c.managePts(pts[0], pts[1]) {
			return
		}
	}

	packed := packDeleteMessage(c, update)

	for group, handlers := range c.dispatcher.messageDeleteHandles {
		for _, handler := range handlers {
			handle := func(h *messageDeleteHandle) error {
				defer c.NewRecovery()()
				return h.Handler(packed)
			}

			if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
				go func() {
					err := handle(handler)
					if err != nil {
						if errors.Is(err, EndGroup) {
							return
						}
						c.Log.Error(errors.Wrap(err, "[DELETE_MESSAGE_ERROR]"))
					}
				}()
			} else {
				if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
					break
				}
			}
		}
	}
}

func (c *Client) handleJoinRequestUpdate(update *UpdatePendingJoinRequests) {
	packed := packJoinRequest(c, update)

	for group, handlers := range c.dispatcher.joinRequestHandles {
		for _, handler := range handlers {
			handle := func(h *joinRequestHandle) error {
				defer c.NewRecovery()()
				return h.Handler(packed)
			}

			if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
				go func() {
					err := handle(handler)
					if err != nil {
						if errors.Is(err, EndGroup) {
							return
						}
						c.Log.Error(errors.Wrap(err, "[JOIN_REQUEST_ERROR]"))
					}
				}()
			} else {
				if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
					break
				}
			}
		}
	}
}

func (c *Client) handleRawUpdate(update Update) {
	for group, handlers := range c.dispatcher.rawHandles {
		for _, handler := range handlers {
			if reflect.TypeOf(update) == reflect.TypeOf(handler.updateType) || handler.updateType == nil {
				handle := func(h *rawHandle) error {
					defer c.NewRecovery()()
					return h.Handler(update, c)
				}

				if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
					go func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, EndGroup) {
								return
							}
							c.Log.Error(errors.Wrap(err, "[RAW_UPDATE_ERROR]"))
						}
					}()
				} else {
					if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
						break
					}
				}
			}
		}
	}
}

func (h *inlineHandle) IsMatch(text string) bool {
	switch pattern := h.Pattern.(type) {
	case string:
		if pattern == OnInlineQuery || pattern == OnInline {
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
		if pattern == OnEditMessage || pattern == OnEdit {
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
		if pattern == OnCallbackQuery || pattern == OnCallback {
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
		if pattern == OnInlineCallbackQuery || pattern == OnInlineCallback {
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

func (h *messageHandle) IsMatch(text string, c *Client) bool {
	switch Pattern := h.Pattern.(type) {
	case string:
		if Pattern == OnNewMessage || Pattern == OnMessage {
			return true
		}

		if strings.HasPrefix(Pattern, "cmd:") {
			//(?i)^[!/-?]ping(?: |$|@botusername)(.*)$
			Pattern = "(?i)^[!\\/?]" + strings.TrimPrefix(Pattern, "cmd:")
			if me := c.Me(); me != nil && me.Username != "" && me.Bot {
				Pattern += "(?: |$|@" + me.Username + ")(.*)"
			} else {
				Pattern += "(?: |$)(.*)"
			}
		} else {
			if !strings.HasPrefix(Pattern, "^") {
				Pattern = "^" + Pattern
			}
		}

		pattern := regexp.MustCompile(Pattern)
		return pattern.MatchString(text) || strings.HasPrefix(text, Pattern)
	case *regexp.Regexp:
		return Pattern.MatchString(text)
	}
	return false
}

func (h *messageHandle) runFilterChain(m *NewMessage, filters []Filter) bool {
	var (
		actAsBlacklist                   bool
		actUsers, actGroups, actChannels []int64
		inSlice                          = func(e int64, s []int64) bool {
			for _, a := range s {
				if a == e {
					return true
				}
			}
			return false
		}
	)

	if len(filters) > 0 {
		for _, filter := range filters {
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
			if len(filter.Users) > 0 {
				actUsers = filter.Users
			}
			if len(filter.Chats) > 0 {
				actGroups = filter.Chats
			}
			if len(filter.Channels) > 0 {
				actChannels = filter.Channels
			}
			if filter.Blacklist {
				actAsBlacklist = true
			}
			if filter.Mention && m.Message != nil && !m.Message.Mentioned {
				return false
			}

			if filter.Func != nil {
				if !filter.Func(m) {
					return false
				}
			}
		}
	}

	var peerCheckUserPassed bool
	var peerCheckGroupPassed bool
	var peerCheckChannelPassed bool

	if len(actUsers) > 0 && m.SenderID() != 0 {
		if inSlice(m.SenderID(), actUsers) {
			if actAsBlacklist {
				return false
			}
			peerCheckUserPassed = true
		}
	} else {
		peerCheckUserPassed = true
	}

	if len(actGroups) > 0 && m.ChatID() != 0 {
		if inSlice(m.ChatID(), actGroups) {
			if actAsBlacklist {
				return false
			}
			peerCheckGroupPassed = true
		}
	} else {
		peerCheckGroupPassed = true
	}

	if len(actChannels) > 0 && m.ChannelID() != 0 {
		if inSlice(m.ChannelID(), actChannels) {
			if actAsBlacklist {
				return false
			}
			// if the channel is in the blacklist, we should not pass the check
			peerCheckChannelPassed = true
		}
	} else {
		// if there are no channels in the filter, we should pass the check
		peerCheckChannelPassed = true
	}

	if !actAsBlacklist && (len(actUsers) > 0 || len(actGroups) > 0 || len(actChannels) > 0) && !(peerCheckUserPassed && peerCheckGroupPassed && peerCheckChannelPassed) {
		return false
	}

	return true
}

func (e *messageEditHandle) runFilterChain(m *NewMessage, filters []Filter) bool {
	var message = &messageHandle{}
	return message.runFilterChain(m, filters)
}

func (h *callbackHandle) runFilterChain(c *CallbackQuery, filters []Filter) bool {
	if len(filters) > 0 {
		for _, filter := range filters {
			if filter.Private && !c.IsPrivate() || filter.Group && !c.IsGroup() || filter.Channel && !c.IsChannel() {
				return false
			}
			if filter.FromBot {
				if c.Sender == nil || !c.Sender.Bot {
					return false
				}
			}

			if filter.Func != nil {
				if !filter.FuncCallback(c) {
					return false
				}
			}
		}
	}

	return true
}

type Filter struct {
	Private, Group, Channel, Media, Command, Reply, Forward, FromBot, Blacklist, Mention bool
	Users, Chats, Channels                                                               []int64
	Func                                                                                 func(m *NewMessage) bool
	FuncCallback                                                                         func(c *CallbackQuery) bool
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
	FilterMention   = Filter{Mention: true}
	FilterUsers     = func(users ...int64) Filter {
		return Filter{Users: users}
	}
	FilterChats = func(chats ...int64) Filter {
		return Filter{Chats: chats}
	}
	FilterChannels = func(channels ...int64) Filter {
		return Filter{Channels: channels}
	}
	FilterFunc = func(f func(m *NewMessage) bool) Filter {
		return Filter{Func: f}
	}
	FilterFuncCallback = func(f func(c *CallbackQuery) bool) Filter {
		return Filter{Func: func(m *NewMessage) bool { return true }}
	}
	FilterFuncInline = func(f func(c *InlineQuery) bool) Filter {
		return Filter{Func: func(m *NewMessage) bool { return true }}
	}
)

func (c *Client) AddMessageHandler(pattern any, handler MessageHandler, filters ...Filter) Handle {
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}

	if c.dispatcher.messageHandles == nil {
		c.dispatcher.messageHandles = make(map[string][]*messageHandle)
	}

	handle := messageHandle{Pattern: pattern, Handler: handler, Filters: messageFilters, baseHandle: baseHandle{
		sortTrigger: c.dispatcher.sortTrigger,
	}}
	c.dispatcher.messageHandles["default"] = append(c.dispatcher.messageHandles["default"], &handle)
	return c.dispatcher.messageHandles["default"][len(c.dispatcher.messageHandles["default"])-1]
}

func (c *Client) AddCommandHandler(pattern string, handler MessageHandler, filters ...Filter) Handle {
	if !strings.HasPrefix(pattern, "cmd:") {
		pattern = "cmd:" + pattern
	}

	return c.AddMessageHandler(pattern, handler, filters...)
}

func (c *Client) AddDeleteHandler(pattern any, handler func(d *DeleteMessage) error) Handle {
	handle := messageDeleteHandle{
		Pattern: pattern,
		Handler: handler,
		baseHandle: baseHandle{
			sortTrigger: c.dispatcher.sortTrigger,
		},
	}

	if c.dispatcher.messageDeleteHandles == nil {
		c.dispatcher.messageDeleteHandles = make(map[string][]*messageDeleteHandle)
	}

	c.dispatcher.messageDeleteHandles["default"] = append(c.dispatcher.messageDeleteHandles["default"], &handle)
	return c.dispatcher.messageDeleteHandles["default"][len(c.dispatcher.messageDeleteHandles["default"])-1]
}

func (c *Client) AddAlbumHandler(handler func(m *Album) error) Handle {
	if c.dispatcher.albumHandles == nil {
		c.dispatcher.albumHandles = make(map[string][]*albumHandle)
	}

	handle := albumHandle{Handler: handler, baseHandle: baseHandle{
		sortTrigger: c.dispatcher.sortTrigger,
	}}
	c.dispatcher.albumHandles["default"] = append(c.dispatcher.albumHandles["default"], &handle)
	return c.dispatcher.albumHandles["default"][len(c.dispatcher.albumHandles["default"])-1]
}

func (c *Client) AddActionHandler(handler MessageHandler) Handle {
	if c.dispatcher.actionHandles == nil {
		c.dispatcher.actionHandles = make(map[string][]*chatActionHandle)
	}

	handle := chatActionHandle{Handler: handler, baseHandle: baseHandle{
		sortTrigger: c.dispatcher.sortTrigger,
	}}
	c.dispatcher.actionHandles["default"] = append(c.dispatcher.actionHandles["default"], &handle)
	return c.dispatcher.actionHandles["default"][len(c.dispatcher.actionHandles["default"])-1]
}

func (c *Client) AddEditHandler(pattern any, handler MessageHandler, filters ...Filter) Handle {
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}

	if c.dispatcher.messageEditHandles == nil {
		c.dispatcher.messageEditHandles = make(map[string][]*messageEditHandle)
	}

	handle := messageEditHandle{Pattern: pattern, Handler: handler, Filters: messageFilters, baseHandle: baseHandle{
		sortTrigger: c.dispatcher.sortTrigger,
	}}
	c.dispatcher.messageEditHandles["default"] = append(c.dispatcher.messageEditHandles["default"], &handle)
	return c.dispatcher.messageEditHandles["default"][len(c.dispatcher.messageEditHandles["default"])-1]
}

func (c *Client) AddInlineHandler(pattern any, handler InlineHandler) Handle {
	if c.dispatcher.inlineHandles == nil {
		c.dispatcher.inlineHandles = make(map[string][]*inlineHandle)
	}

	handle := inlineHandle{Pattern: pattern, Handler: handler, baseHandle: baseHandle{
		sortTrigger: c.dispatcher.sortTrigger,
	}}
	c.dispatcher.inlineHandles["default"] = append(c.dispatcher.inlineHandles["default"], &handle)
	return c.dispatcher.inlineHandles["default"][len(c.dispatcher.inlineHandles["default"])-1]
}

// AddInlineSendHandler enable feedback updates from bot father, to receive these updates.
func (c *Client) AddInlineSendHandler(handler InlineSendHandler) Handle {
	if c.dispatcher.inlineSendHandles == nil {
		c.dispatcher.inlineSendHandles = make(map[string][]*inlineSendHandle)
	}

	handle := inlineSendHandle{Handler: handler, baseHandle: baseHandle{
		sortTrigger: c.dispatcher.sortTrigger,
	}}
	c.dispatcher.inlineSendHandles["default"] = append(c.dispatcher.inlineSendHandles["default"], &handle)
	return c.dispatcher.inlineSendHandles["default"][len(c.dispatcher.inlineSendHandles["default"])-1]
}

// AddCallbackHandler Handle updates categorized as "UpdateBotCallbackQuery"
func (c *Client) AddCallbackHandler(pattern any, handler CallbackHandler, filters ...Filter) Handle {
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}

	if c.dispatcher.callbackHandles == nil {
		c.dispatcher.callbackHandles = make(map[string][]*callbackHandle)
	}

	handle := callbackHandle{Pattern: pattern, Handler: handler, baseHandle: baseHandle{
		sortTrigger: c.dispatcher.sortTrigger,
	}, Filters: messageFilters}
	c.dispatcher.callbackHandles["default"] = append(c.dispatcher.callbackHandles["default"], &handle)
	return c.dispatcher.callbackHandles["default"][len(c.dispatcher.callbackHandles["default"])-1]
}

// AddInlineCallbackHandler Handle updates categorized as "UpdateInlineBotCallbackQuery"
func (c *Client) AddInlineCallbackHandler(pattern any, handler InlineCallbackHandler) Handle {
	if c.dispatcher.inlineCallbackHandles == nil {
		c.dispatcher.inlineCallbackHandles = make(map[string][]*inlineCallbackHandle)
	}

	handle := inlineCallbackHandle{Pattern: pattern, Handler: handler, baseHandle: baseHandle{
		sortTrigger: c.dispatcher.sortTrigger,
	}}
	c.dispatcher.inlineCallbackHandles["default"] = append(c.dispatcher.inlineCallbackHandles["default"], &handle)
	return c.dispatcher.inlineCallbackHandles["default"][len(c.dispatcher.inlineCallbackHandles["default"])-1]
}

// AddJoinRequestHandler Handle updates categorized as "UpdatePendingJoinRequests"
func (c *Client) AddJoinRequestHandler(handler PendingJoinHandler) Handle {
	if c.dispatcher.joinRequestHandles == nil {
		c.dispatcher.joinRequestHandles = make(map[string][]*joinRequestHandle)
	}

	handle := joinRequestHandle{Handler: handler, baseHandle: baseHandle{
		sortTrigger: c.dispatcher.sortTrigger,
	}}
	c.dispatcher.joinRequestHandles["default"] = append(c.dispatcher.joinRequestHandles["default"], &handle)
	return c.dispatcher.joinRequestHandles["default"][len(c.dispatcher.joinRequestHandles["default"])-1]
}

// AddParticipantHandler Handle updates categorized as "UpdateChannelParticipant"
func (c *Client) AddParticipantHandler(handler ParticipantHandler) Handle {
	if c.dispatcher.participantHandles == nil {
		c.dispatcher.participantHandles = make(map[string][]*participantHandle)
	}

	handle := participantHandle{Handler: handler, baseHandle: baseHandle{
		sortTrigger: c.dispatcher.sortTrigger,
	}}
	c.dispatcher.participantHandles["default"] = append(c.dispatcher.participantHandles["default"], &handle)
	return c.dispatcher.participantHandles["default"][len(c.dispatcher.participantHandles["default"])-1]
}

func (c *Client) AddRawHandler(updateType Update, handler RawHandler) Handle {
	if c.dispatcher.rawHandles == nil {
		c.dispatcher.rawHandles = make(map[string][]*rawHandle)
	}

	handle := rawHandle{updateType: updateType, Handler: handler, baseHandle: baseHandle{
		sortTrigger: c.dispatcher.sortTrigger,
	}}
	c.dispatcher.rawHandles["default"] = append(c.dispatcher.rawHandles["default"], &handle)
	return c.dispatcher.rawHandles["default"][len(c.dispatcher.rawHandles["default"])-1]
}

// HandleIncomingUpdates processes incoming updates and dispatches them to the appropriate handlers.
func HandleIncomingUpdates(u any, c *Client) bool {
	c.dispatcher.nextUpdatesDeadline = time.Now().Add(time.Minute * 15)

UpdateTypeSwitching:
	switch upd := u.(type) {
	case *UpdatesObj:
		go c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
		for _, update := range upd.Updates {
			switch update := update.(type) {
			case *UpdateNewMessage:
				go c.handleMessageUpdate(update.Message, update.Pts, update.PtsCount)
			case *UpdateNewChannelMessage:
				go c.handleMessageUpdate(update.Message, update.Pts, update.PtsCount)
			case *UpdateNewScheduledMessage:
				go c.handleMessageUpdate(update.Message)
			case *UpdateEditMessage:
				go c.handleEditUpdate(update.Message, update.Pts, update.PtsCount)
			case *UpdateEditChannelMessage:
				go c.handleEditUpdate(update.Message, update.Pts, update.PtsCount)
			case *UpdateBotInlineQuery:
				go c.handleInlineUpdate(update)
			case *UpdateBotCallbackQuery:
				go c.handleCallbackUpdate(update)
			case *UpdateInlineBotCallbackQuery:
				go c.handleInlineCallbackUpdate(update)
			case *UpdateChannelParticipant:
				go c.handleParticipantUpdate(update)
			case *UpdatePendingJoinRequests:
				go c.handleJoinRequestUpdate(update)
			case *UpdateDeleteChannelMessages:
				go c.handleDeleteUpdate(update, update.Pts, update.PtsCount)
			case *UpdateDeleteMessages:
				go c.handleDeleteUpdate(update, update.Pts, update.PtsCount)
			case *UpdateBotInlineSend:
				go c.handleInlineSendUpdate(update)
			}
			go c.handleRawUpdate(update)
		}
	case *UpdateShort:
		switch upd := upd.Update.(type) {
		case *UpdateNewMessage:
			go c.handleMessageUpdateWith(upd.Message, upd.Pts)
		case *UpdateNewChannelMessage:
			go c.handleMessageUpdateWith(upd.Message, upd.Pts)
		}
		go c.handleRawUpdate(upd.Update)
	case *UpdateShortMessage:
		go c.handleMessageUpdateWith(&MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.UserID), PeerID: getPeerUser(upd.UserID), Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}, upd.Pts)
	case *UpdateShortChatMessage:
		go c.handleMessageUpdateWith(&MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.FromID), PeerID: &PeerChat{ChatID: upd.ChatID}, Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}, upd.Pts)
	case *UpdateShortSentMessage:
		go c.handleMessageUpdateWith(&MessageObj{ID: upd.ID, Out: upd.Out, Date: upd.Date, Media: upd.Media, Entities: upd.Entities, TtlPeriod: upd.TtlPeriod}, upd.Pts)
	case *UpdatesCombined:
		u = upd.Updates
		go c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
		goto UpdateTypeSwitching
	case *UpdatesTooLong:
		c.Log.Debug("[TOO_MANY_UPDATES] - ", c.JSON(upd))
		c.handleMessageUpdate(&MessageEmpty{}, -1) // updatesTooLong, too many updates, shall fetch manually
	default:
		c.Log.Debug("[UNKNOWN_UPDATE_SKIPPED] - ", c.JSON(upd), " - ", reflect.TypeOf(upd))
	}
	return true
}

const GetdiffLimit = 1000

func fetchUpdates(c *Client) {
	totalFetched := 0

	req := &UpdatesGetDifferenceParams{
		Pts:           c.dispatcher.GetPts(),
		PtsLimit:      GetdiffLimit,
		PtsTotalLimit: GetdiffLimit,
		Date:          int32(time.Now().Unix()),
		Qts:           0,
		QtsLimit:      0,
	}

	defer func() {
		c.Log.Debug("[FORCE_GETDIFF] - ", totalFetched, " updates fetched")
	}()

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		updates, err := c.MTProto.MakeRequestCtx(ctx, req)
		cancel()
		if err != nil {
			c.Log.Error(errors.Wrap(err, "[UPDATES_DISPATCHER_GET_DIFFERENCE]"))
			return
		}

		switch u := updates.(type) {
		case *UpdatesDifferenceObj:
			c.Cache.UpdatePeersToCache(u.Users, u.Chats)
			for _, update := range u.NewMessages {
				switch update.(type) {
				case *MessageObj:
					go c.handleMessageUpdate(update)
					totalFetched++
				}
			}

			if len(u.OtherUpdates) > 0 {
				totalFetched += len(u.OtherUpdates)
				HandleIncomingUpdates(UpdatesObj{Updates: u.OtherUpdates}, c)
			}

			c.dispatcher.SetPts(u.State.Pts)
			return
		case *UpdatesDifferenceSlice:
			c.Cache.UpdatePeersToCache(u.Users, u.Chats)
			for _, update := range u.NewMessages {
				switch update.(type) {
				case *MessageObj:
					go c.handleMessageUpdate(update)
					totalFetched++
				}
			}

			if len(u.OtherUpdates) > 0 {
				totalFetched += len(u.OtherUpdates)
				HandleIncomingUpdates(UpdatesObj{Updates: u.OtherUpdates}, c)
			}

			c.dispatcher.SetPts(u.IntermediateState.Pts)
			req.Pts = u.IntermediateState.Pts
		case *UpdatesDifferenceTooLong:
			c.dispatcher.SetPts(u.Pts)
			req.Pts = u.Pts
		case *UpdatesDifferenceEmpty:
			return
		default:
			c.Log.Debug("[UNKNOWN_UPDATE_SKIPPED] - ", c.JSON(updates), " - ", reflect.TypeOf(updates))
			return
		}
	}
}

func (c *Client) managePts(pts int32, ptsCount int32) bool {
	var currentPts = c.dispatcher.GetPts()

	if currentPts+ptsCount == pts || currentPts == 0 {
		c.dispatcher.SetPts(pts)
		return true
	}

	if currentPts+ptsCount < pts {
		c.Log.Debug("[PTS_MISMATCH] - ", currentPts, " + ", ptsCount, " != ", pts)
		c.dispatcher.SetPts(pts)
		return true
	}

	return true
}

func (c *Client) GetDifference(Pts, Limit int32) (Message, error) {
	c.Log.Debug("[GET_DIFFERENCE] - getting difference with pts: ", Pts, " and limit: ", Limit)

	updates, err := c.UpdatesGetDifference(&UpdatesGetDifferenceParams{
		Pts:           Pts - 1,
		PtsLimit:      Limit,
		PtsTotalLimit: Limit,
		Date:          int32(time.Now().Unix()),
		Qts:           0,
		QtsLimit:      Limit,
	})

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

func (c *Client) OpenChat(channel *InputChannelObj) {
	if c.dispatcher.openChats == nil {
		c.dispatcher.openChats = make(map[int64]*openChat)
	}

	if _, ok := c.dispatcher.openChats[channel.ChannelID]; ok {
		return
	}

	c.dispatcher.openChats[channel.ChannelID] = &openChat{
		accessHash: channel.AccessHash,
		closeChan:  make(chan struct{}),
		lastPts:    0,
	}
}

func (c *Client) CloseChat(channel *InputChannelObj) {
	if c.dispatcher.openChats == nil {
		return
	}

	if _, ok := c.dispatcher.openChats[channel.ChannelID]; !ok {
		return
	}

	close(c.dispatcher.openChats[channel.ChannelID].closeChan)
	delete(c.dispatcher.openChats, channel.ChannelID)
}

const (
	GetChannelDiffInterval = 2000 * time.Millisecond
)

// FetchGap TODO Implement a better way to fetch channel differences
func (c *Client) FetchGap() {
	for {
		for channelID, chat := range c.dispatcher.openChats {
			chDiff, _ := c.UpdatesGetChannelDifference(&UpdatesGetChannelDifferenceParams{
				Force:   false,
				Channel: &InputChannelObj{ChannelID: channelID, AccessHash: chat.accessHash},
				Pts:     chat.lastPts,
				Limit:   100,
				Filter:  &ChannelMessagesFilterEmpty{},
			})

			if c.dispatcher.openChats[channelID].lastPts != chat.lastPts {
				c.Log.Debug("[CHANNEL_PTS_CHANGED] - skipping fetch")
			}

			if chDiff != nil {
				switch d := chDiff.(type) {
				case *UpdatesChannelDifferenceEmpty:
					c.Log.Info("[CHANNEL_DIFFERENCE_EMPTY] - channel difference is empty")
				case *UpdatesChannelDifferenceObj:
					c.Cache.UpdatePeersToCache(d.Users, d.Chats)
					for _, update := range d.NewMessages {
						switch u := update.(type) {
						case *MessageObj:
							c.handleMessageUpdate(u)
						}
					}
				case *UpdatesChannelDifferenceTooLong:
					c.Log.Debug("[CHANNEL_DIFFERENCE_TOO_LONG] - channel difference is too long, requesting getState")
				default:
					c.Log.Error("[UNKNOWN_CHANNEL_DIFFERENCE_TYPE] - ", reflect.TypeOf(d))
				}
			}
		}

		time.Sleep(GetChannelDiffInterval)
	}
}

type ev any

var (
	OnMessage        ev = "message"
	OnCommand        ev = "command"
	OnEdit           ev = "edit"
	OnDelete         ev = "delete"
	OnAlbum          ev = "album"
	OnInline         ev = "inline"
	OnCallback       ev = "callback"
	OnInlineCallback ev = "inlineCallback"
	OnChoosenInline  ev = "choosenInline"
	OnParticipant    ev = "participant"
	OnRaw            ev = "raw"
)

// On is a helper function to add a handler for a specific event type (handler: func(m *NewMessage) error, etc.)
func (c *Client) On(pattern any, handler any, filters ...Filter) Handle {
	var patternKey string
	var args string

	if patternStr, ok := pattern.(string); ok {
		if strings.Contains(patternStr, ":") {
			parts := strings.SplitN(patternStr, ":", 2)
			patternKey = strings.TrimSpace(parts[0])
			args = strings.TrimSpace(parts[1])
		} else {
			patternKey = strings.TrimSpace(patternStr)
		}
	}

	switch ev(patternKey) {
	case OnMessage, OnNewMessage:
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if args != "" {
				return c.AddMessageHandler(args, h, filters...)
			}
			return c.AddMessageHandler(OnNewMessage, h, filters...)
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m *NewMessage) error")
		}
	case OnCommand:
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if args != "" {
				return c.AddMessageHandler("cmd:"+args, h, filters...)
			}
			return c.AddMessageHandler(OnNewMessage, h, filters...)
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m *NewMessage) error")
		}
	case OnEdit, OnEditMessage:
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if args != "" {
				return c.AddEditHandler(args, h)
			}
			return c.AddEditHandler(OnEditMessage, h)
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m *NewMessage) error")
		}
	case OnDelete, OnDeleteMessage:
		if h, ok := handler.(func(m *DeleteMessage) error); ok {
			if args != "" {
				return c.AddDeleteHandler(args, h)
			}
			return c.AddDeleteHandler(OnDeleteMessage, h)
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m *DeleteMessage) error")
		}
	case OnAlbum:
		if h, ok := handler.(func(m *Album) error); ok {
			return c.AddAlbumHandler(h)
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m *Album) error")
		}
	case OnInline, OnInlineQuery:
		if h, ok := handler.(func(m *InlineQuery) error); ok {
			if args != "" {
				return c.AddInlineHandler(args, h)
			}
			return c.AddInlineHandler(OnInlineQuery, h)
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m *InlineQuery) error")
		}
	case OnChoosenInline:
		if h, ok := handler.(func(m *InlineSend) error); ok {
			return c.AddInlineSendHandler(h)
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m *InlineSend) error")
		}
	case OnCallback, OnCallbackQuery:
		if h, ok := handler.(func(m *CallbackQuery) error); ok {
			if args != "" {
				return c.AddCallbackHandler(args, h)
			}
			return c.AddCallbackHandler(OnCallbackQuery, h)
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m *CallbackQuery) error")
		}
	case OnInlineCallback, OnInlineCallbackQuery, "inlinecallback":
		if h, ok := handler.(func(m *InlineCallbackQuery) error); ok {
			if args != "" {
				return c.AddInlineCallbackHandler(args, h)
			}
			return c.AddInlineCallbackHandler(OnInlineCallbackQuery, h)
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m *InlineCallbackQuery) error")
		}
	case OnParticipant:
		if h, ok := handler.(func(m *ParticipantUpdate) error); ok {
			return c.AddParticipantHandler(h)
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m *ParticipantUpdate) error")
		}
	case OnRaw:
		if h, ok := handler.(func(m Update, c *Client) error); ok {
			return c.AddRawHandler(nil, h)
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m Update, c *Client) error")
		}
	default:
		if update, ok := pattern.(Update); ok {
			if h, ok := handler.(func(m Update, c *Client) error); ok {
				return c.AddRawHandler(update, h)
			}
		} else {
			c.Log.Error("[INVALID_HANDLER_SIGNATURE] - ", reflect.TypeOf(handler), " Expected: func(m Update, c *Client) error")
		}
	}

	return nil
}
