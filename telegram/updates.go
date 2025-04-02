// Copyright (c) 2024, amarnathcjd

package telegram

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/amarnathcjd/gogram/internal/utils"
	"github.com/pkg/errors"
)

type Handle interface {
	SetGroup(group string) Handle
	GetGroup() string
}

type MessageHandler func(m *NewMessage) error
type EditHandler func(m *NewMessage) error
type DeleteHandler func(m *DeleteMessage) error
type AlbumHandler func(m *Album) error
type InlineHandler func(m *InlineQuery) error
type InlineSendHandler func(m *InlineSend) error
type CallbackHandler func(m *CallbackQuery) error
type InlineCallbackHandler func(m *InlineCallbackQuery) error
type ParticipantHandler func(m *ParticipantUpdate) error
type RawHandler func(m Update, c *Client) error

var EndGroup = errors.New("end-group-trigger")

type messageHandle struct {
	Pattern     any
	Handler     MessageHandler
	Filters     []Filter
	Group       string
	sortTrigger chan any
}

func (h *messageHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *messageHandle) GetGroup() string {
	return h.Group
}

type albumHandle struct {
	Handler     func(alb *Album) error
	Group       string
	sortTrigger chan any
}

func (h *albumHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *albumHandle) GetGroup() string {
	return h.Group
}

type albumBox struct {
	sync.Mutex
	messages  []*NewMessage
	groupedId int64
}

func (a *albumBox) WaitAndTrigger(d *UpdateDispatcher, c *Client) {
	time.Sleep(600 * time.Millisecond)

	for gp, handlers := range d.albumHandles {
		for _, handler := range handlers {
			handle := func(h *albumHandle) error {
				if err := h.Handler(&Album{
					GroupedID: a.groupedId,
					Messages:  a.messages,
					Client:    c,
				}); err != nil {
					if errors.Is(err, EndGroup) {
						return err
					}
					c.Log.Error(errors.Wrap(err, "[newAlbum]"))
				}
				return nil
			}

			if strings.EqualFold(gp, "") || strings.EqualFold(strings.TrimSpace(gp), "default") {
				go handle(handler)
			} else {
				if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
					break
				}
			}
		}
	}

	// delete(d.activeAlbums, a.groupedId)
	d.Lock()
	defer d.Unlock()
	delete(d.activeAlbums, a.groupedId)
}

func (a *albumBox) Add(m *NewMessage) {
	a.Lock()
	defer a.Unlock()
	a.messages = append(a.messages, m)
}

type chatActionHandle struct {
	Handler     MessageHandler
	Group       string
	sortTrigger chan any
}

func (h *chatActionHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *chatActionHandle) GetGroup() string {
	return h.Group
}

type messageEditHandle struct {
	Pattern     any
	Handler     MessageHandler
	Filters     []Filter
	Group       string
	sortTrigger chan any
}

func (h *messageEditHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *messageEditHandle) GetGroup() string {
	return h.Group
}

type messageDeleteHandle struct {
	Pattern     any
	Handler     func(m *DeleteMessage) error
	Group       string
	sortTrigger chan any
}

func (h *messageDeleteHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *messageDeleteHandle) GetGroup() string {
	return h.Group
}

type inlineHandle struct {
	Pattern     any
	Handler     InlineHandler
	Group       string
	sortTrigger chan any
}

func (h *inlineHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *inlineHandle) GetGroup() string {
	return h.Group
}

type inlineSendHandle struct {
	Handler     InlineSendHandler
	Group       string
	sortTrigger chan any
}

func (h *inlineSendHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *inlineSendHandle) GetGroup() string {
	return h.Group
}

type callbackHandle struct {
	Pattern     any
	Handler     CallbackHandler
	Filters     []Filter
	Group       string
	sortTrigger chan any
}

func (h *callbackHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *callbackHandle) GetGroup() string {
	return h.Group
}

type inlineCallbackHandle struct {
	Pattern     any
	Handler     InlineCallbackHandler
	Group       string
	sortTrigger chan any
}

func (h *inlineCallbackHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *inlineCallbackHandle) GetGroup() string {
	return h.Group
}

type participantHandle struct {
	Handler     func(p *ParticipantUpdate) error
	Group       string
	sortTrigger chan any
}

func (h *participantHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *participantHandle) GetGroup() string {
	return h.Group
}

type rawHandle struct {
	updateType  Update
	Handler     RawHandler
	Group       string
	sortTrigger chan any
}

func (h *rawHandle) SetGroup(group string) Handle {
	h.Group = group
	h.sortTrigger <- h
	return h
}

func (h *rawHandle) GetGroup() string {
	return h.Group
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

// creates and populates a new UpdateDispatcher
func (c *Client) NewUpdateDispatcher(sessionName ...string) {
	c.dispatcher = &UpdateDispatcher{
		logger: utils.NewLogger("gogram " + getLogPrefix("dispatcher", getVariadic(sessionName, ""))).
			SetLevel(c.Log.Lev()),
	}
	c.dispatcher.SortTrigger()
	//go c.fetchChannelGap()
	c.dispatcher.logger.Debug("dispatcher initialized")
}

// ---------------------------- Dispatcher Functions ----------------------------

// sortgeneric
func sortGeneric[T Handle](handles map[string][]T) map[string][]T {
	for group, h := range handles {
		correctHandles := h[:0]
		var toMove []T

		for _, handle := range h {
			if handle.GetGroup() == group {
				correctHandles = append(correctHandles, handle)
			} else {
				toMove = append(toMove, handle)
			}
		}

		handles[group] = correctHandles

		for _, handle := range toMove {
			handles[handle.GetGroup()] = append(handles[handle.GetGroup()], handle)
		}
	}

	return handles
}

func (d *UpdateDispatcher) SortTrigger() {
	if d.sortTrigger == nil {
		d.sortTrigger = make(chan any)
	}

	go func() {
		for handle := range d.sortTrigger {
			switch handle.(type) {
			case *messageHandle:
				sortGeneric(d.messageHandles)
			case *albumHandle:
				sortGeneric(d.albumHandles)
			case *chatActionHandle:
				sortGeneric(d.actionHandles)
			case *messageEditHandle:
				sortGeneric(d.messageEditHandles)
			case *inlineHandle:
				sortGeneric(d.inlineHandles)
			case *callbackHandle:
				sortGeneric(d.callbackHandles)
			case *inlineCallbackHandle:
				sortGeneric(d.inlineCallbackHandles)
			case *participantHandle:
				sortGeneric(d.participantHandles)
			case *messageDeleteHandle:
				sortGeneric(d.messageDeleteHandles)
			case *rawHandle:
				sortGeneric(d.rawHandles)
			}
		}
	}()
}

func (c *Client) RemoveHandle(handle Handle) error {
	if c.dispatcher == nil {
		return errors.New("dispatcher not initialized")
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
		return errors.New("invalid handle type")
	}

	return nil
}

func removeHandleFromMap[T any](handle T, handlesMap map[string][]T) {
	for key, handles := range handlesMap {
		for i, h := range handles {
			if reflect.DeepEqual(h, handle) {
				// Remove the handle by appending the slice before and after the index
				handlesMap[key] = append(handles[:i], handles[i+1:]...)
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

		packed := packMessage(c, msg)
		groupFunc := func(group string, handlers []*messageHandle) {
			defer wg.Done()
			for _, handler := range handlers {
				if handler.IsMatch(msg.Message, c) {
					handle := func(h *messageHandle) error {
						if handler.runFilterChain(packed, h.Filters) {
							defer c.NewRecovery()()
							if err := h.Handler(packed); err != nil {
								if errors.Is(err, EndGroup) {
									return err
								}
								c.dispatcher.logger.Error(errors.Wrap(err, "[newMessage]"))
							}
						}
						return nil
					}

					if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
						go handle(handler)
					} else {
						if err := handle(handler); err != nil && errors.Is(err, EndGroup) {
							if strings.EqualFold(group, "conversation") {
								cancel()
							}
							break
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

			wg.Add(1)
			go groupFunc(group, handlers)
		}

		wg.Wait()
		cancel()

	case *MessageService:
		packed := packMessage(c, msg)

		for group, handler := range c.dispatcher.actionHandles {
			for _, h := range handler {
				handle := func(h *chatActionHandle) error {
					defer c.NewRecovery()()
					if err := h.Handler(packed); err != nil {
						if errors.Is(err, EndGroup) {
							return err
						}
						c.Log.Error(errors.Wrap(err, "[chatAction]"))
					}

					return nil
				}

				if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
					go handle(h)
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
		c.Log.Error(errors.Wrap(err, "updates.dispatcher.getDifference"))
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
						defer c.NewRecovery()()
						if handler.runFilterChain(packed, h.Filters) {
							if err := h.Handler(packed); err != nil {
								if errors.Is(err, EndGroup) {
									return err
								}
								c.Log.Error(errors.Wrap(err, "[editMessage]"))
							}
						}
						return nil
					}

					if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
						go handle(handler)
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
						if err := h.Handler(packed); err != nil {
							if errors.Is(err, EndGroup) {
								return err
							}
							c.Log.Error(errors.Wrap(err, "[callbackQuery]"))
						}
					}
					return nil
				}

				if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
					go handle(handler)
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
					if err := h.Handler(packed); err != nil {
						if errors.Is(err, EndGroup) {
							return err
						}
						c.Log.Error(errors.Wrap(err, "[inlineCallbackQuery]"))
					}
					return nil
				}

				if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
					go handle(handler)
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
				if err := h.Handler(packed); err != nil {
					if errors.Is(err, EndGroup) {
						return err
					}
					c.Log.Error(errors.Wrap(err, "[participantUpdate]"))
				}
				return nil
			}

			if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
				go handle(handler)
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
					if err := h.Handler(packed); err != nil {
						if errors.Is(err, EndGroup) {
							return err
						}
						c.Log.Error(errors.Wrap(err, "[inlineQuery]"))
					}
					return nil
				}

				if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
					go handle(handler)
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
				if err := h.Handler(packed); err != nil {
					if errors.Is(err, EndGroup) {
						return err
					}
					c.Log.Error(errors.Wrap(err, "[inlineSend]"))
				}
				return nil
			}

			if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
				go handle(handler)
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
				if err := h.Handler(packed); err != nil {
					if errors.Is(err, EndGroup) {
						return err
					}
					c.Log.Error(errors.Wrap(err, "[deleteMessage]"))
				}
				return nil
			}

			if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
				go handle(handler)
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
					if err := h.Handler(update, c); err != nil {
						if errors.Is(err, EndGroup) {
							return err
						}
						c.Log.Error(errors.Wrap(err, "[rawUpdate]"))
					}
					return nil
				}

				if strings.EqualFold(group, "") || strings.EqualFold(strings.TrimSpace(group), "default") {
					go handle(handler)
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

	if !actAsBlacklist && (len(actUsers) > 0 || len(actGroups) > 0) && !(peerCheckUserPassed && peerCheckGroupPassed) {
		return false
	}

	return true
}

func (h *messageEditHandle) runFilterChain(m *NewMessage, filters []Filter) bool {
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
	Users, Chats                                                                         []int64
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
	FilterFunc = func(f func(m *NewMessage) bool) Filter {
		return Filter{Func: f}
	}
	FilterFuncCallback = func(f func(c *CallbackQuery) bool) Filter {
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

	handle := messageHandle{Pattern: pattern, Handler: handler, Filters: messageFilters, sortTrigger: c.dispatcher.sortTrigger}
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
		Pattern:     pattern,
		Handler:     handler,
		sortTrigger: c.dispatcher.sortTrigger,
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

	handle := albumHandle{Handler: handler, sortTrigger: c.dispatcher.sortTrigger}
	c.dispatcher.albumHandles["default"] = append(c.dispatcher.albumHandles["default"], &handle)
	return c.dispatcher.albumHandles["default"][len(c.dispatcher.albumHandles["default"])-1]
}

func (c *Client) AddActionHandler(handler MessageHandler) Handle {
	if c.dispatcher.actionHandles == nil {
		c.dispatcher.actionHandles = make(map[string][]*chatActionHandle)
	}

	handle := chatActionHandle{Handler: handler, sortTrigger: c.dispatcher.sortTrigger}
	c.dispatcher.actionHandles["default"] = append(c.dispatcher.actionHandles["default"], &handle)
	return c.dispatcher.actionHandles["default"][len(c.dispatcher.actionHandles["default"])-1]
}

// Handle updates categorized as "UpdateMessageEdited"
//
// Included Updates:
//   - Message Edited
//   - Channel Post Edited
func (c *Client) AddEditHandler(pattern any, handler MessageHandler, filters ...Filter) Handle {
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}

	if c.dispatcher.messageEditHandles == nil {
		c.dispatcher.messageEditHandles = make(map[string][]*messageEditHandle)
	}

	handle := messageEditHandle{Pattern: pattern, Handler: handler, sortTrigger: c.dispatcher.sortTrigger, Filters: messageFilters}
	c.dispatcher.messageEditHandles["default"] = append(c.dispatcher.messageEditHandles["default"], &handle)
	return c.dispatcher.messageEditHandles["default"][len(c.dispatcher.messageEditHandles["default"])-1]
}

// Handle updates categorized as "UpdateBotInlineQuery"
//
// Included Updates:
//   - Inline Query
func (c *Client) AddInlineHandler(pattern any, handler InlineHandler) Handle {
	if c.dispatcher.inlineHandles == nil {
		c.dispatcher.inlineHandles = make(map[string][]*inlineHandle)
	}

	handle := inlineHandle{Pattern: pattern, Handler: handler, sortTrigger: c.dispatcher.sortTrigger}
	c.dispatcher.inlineHandles["default"] = append(c.dispatcher.inlineHandles["default"], &handle)
	return c.dispatcher.inlineHandles["default"][len(c.dispatcher.inlineHandles["default"])-1]
}

// enable feedback updates from botfather, to recieve these updates.
func (c *Client) AddInlineSendHandler(handler InlineSendHandler) Handle {
	if c.dispatcher.inlineSendHandles == nil {
		c.dispatcher.inlineSendHandles = make(map[string][]*inlineSendHandle)
	}

	handle := inlineSendHandle{Handler: handler, sortTrigger: c.dispatcher.sortTrigger}
	c.dispatcher.inlineSendHandles["default"] = append(c.dispatcher.inlineSendHandles["default"], &handle)
	return c.dispatcher.inlineSendHandles["default"][len(c.dispatcher.inlineSendHandles["default"])-1]
}

// Handle updates categorized as "UpdateBotCallbackQuery"
//
// Included Updates:
//   - Callback Query
func (c *Client) AddCallbackHandler(pattern any, handler CallbackHandler, filters ...Filter) Handle {
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}

	if c.dispatcher.callbackHandles == nil {
		c.dispatcher.callbackHandles = make(map[string][]*callbackHandle)
	}

	handle := callbackHandle{Pattern: pattern, Handler: handler, sortTrigger: c.dispatcher.sortTrigger, Filters: messageFilters}
	c.dispatcher.callbackHandles["default"] = append(c.dispatcher.callbackHandles["default"], &handle)
	return c.dispatcher.callbackHandles["default"][len(c.dispatcher.callbackHandles["default"])-1]
}

// Handle updates categorized as "UpdateInlineBotCallbackQuery"
//
// Included Updates:
//   - Inline Callback Query
func (c *Client) AddInlineCallbackHandler(pattern any, handler InlineCallbackHandler) Handle {
	if c.dispatcher.inlineCallbackHandles == nil {
		c.dispatcher.inlineCallbackHandles = make(map[string][]*inlineCallbackHandle)
	}

	handle := inlineCallbackHandle{Pattern: pattern, Handler: handler, sortTrigger: c.dispatcher.sortTrigger}
	c.dispatcher.inlineCallbackHandles["default"] = append(c.dispatcher.inlineCallbackHandles["default"], &handle)
	return c.dispatcher.inlineCallbackHandles["default"][len(c.dispatcher.inlineCallbackHandles["default"])-1]
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
func (c *Client) AddParticipantHandler(handler ParticipantHandler) Handle {
	if c.dispatcher.participantHandles == nil {
		c.dispatcher.participantHandles = make(map[string][]*participantHandle)
	}

	handle := participantHandle{Handler: handler, sortTrigger: c.dispatcher.sortTrigger}
	c.dispatcher.participantHandles["default"] = append(c.dispatcher.participantHandles["default"], &handle)
	return c.dispatcher.participantHandles["default"][len(c.dispatcher.participantHandles["default"])-1]
}

func (c *Client) AddRawHandler(updateType Update, handler RawHandler) Handle {
	if c.dispatcher.rawHandles == nil {
		c.dispatcher.rawHandles = make(map[string][]*rawHandle)
	}

	handle := rawHandle{updateType: updateType, Handler: handler, sortTrigger: c.dispatcher.sortTrigger}
	c.dispatcher.rawHandles["default"] = append(c.dispatcher.rawHandles["default"], &handle)
	return c.dispatcher.rawHandles["default"][len(c.dispatcher.rawHandles["default"])-1]
}

// Sort and Handle all the Incoming Updates
// Many more types to be added
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
		c.Log.Debug("too many updates, forcing getDifference")
		c.handleMessageUpdate(&MessageEmpty{}, -1) // updatesTooLong, too many updates, shall fetch manually
	default:
		c.Log.Debug("skipping unhanded update type: ", reflect.TypeOf(u), " with value: ", c.JSON(u))
	}
	return true
}

const GETDIFF_LIMIT = 1000

func fetchUpdates(c *Client) {
	totalFetched := 0

	req := &UpdatesGetDifferenceParams{
		Pts:           c.dispatcher.GetPts(),
		PtsLimit:      GETDIFF_LIMIT,
		PtsTotalLimit: GETDIFF_LIMIT,
		Date:          int32(time.Now().Unix()),
		Qts:           0,
		QtsLimit:      0,
	}

	defer func() {
		c.Log.Debug("force fetched ", totalFetched, " updates")
	}()

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		updates, err := c.MTProto.MakeRequestCtx(ctx, req)
		if err != nil {
			c.Log.Error(errors.Wrap(err, "updates.dispatcher.getDifference"))
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
			c.Log.Debug("skipping unknown update type: ", reflect.TypeOf(u), " with value: ", c.JSON(u))
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
		c.Log.Debug("update gap detected - filling - pts (", currentPts, ") - ptsCount (", ptsCount, ") - pts (", pts, ")")
		c.dispatcher.SetPts(pts)
		return true // remaining parts have some issues it seems, I'll address later

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		updates, err := c.MTProto.MakeRequestCtx(ctx, &UpdatesGetDifferenceParams{
			Pts:           currentPts,
			PtsLimit:      pts - currentPts,
			PtsTotalLimit: pts - currentPts,
			Date:          int32(time.Now().Unix()),
			Qts:           0,
			QtsLimit:      0,
		})

		if err != nil {
			c.Log.Error(errors.Wrap(err, "updates.dispatcher.getDifference"))
			return true
		}

		switch u := updates.(type) {
		case *UpdatesDifferenceObj:
			c.Cache.UpdatePeersToCache(u.Users, u.Chats)
			for _, update := range u.NewMessages {
				switch update.(type) {
				case *MessageObj:
					c.handleMessageUpdate(update)
				}
			}

		case *UpdatesDifferenceSlice:
			c.Cache.UpdatePeersToCache(u.Users, u.Chats)
			for _, update := range u.NewMessages {
				switch update.(type) {
				case *MessageObj:
					c.handleMessageUpdate(update)
				}
			}
		}

		c.dispatcher.SetPts(pts)
	} else {
		return true
	}

	return true
}

func (c *Client) GetDifference(Pts, Limit int32) (Message, error) {
	c.Log.Debug("getting difference with pts: ", Pts, " and limit: ", Limit)

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
	GET_CHANNEL_DIFF_INTERVAL = 2000 * time.Millisecond
)

// TODO Implement a better way to fetch channel differences
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
				c.Log.Debug("channel pts changed, skipping fetch")
			}

			if chDiff != nil {
				switch d := chDiff.(type) {
				case *UpdatesChannelDifferenceEmpty:
					c.Log.Info("channel difference is empty")
				case *UpdatesChannelDifferenceObj:
					c.Cache.UpdatePeersToCache(d.Users, d.Chats)
					for _, update := range d.NewMessages {
						switch u := update.(type) {
						case *MessageObj:
							c.handleMessageUpdate(u)
						}
					}
				case *UpdatesChannelDifferenceTooLong:
					c.Log.Debug("channel difference is too long, requesting getState")
				default:
					c.Log.Error("unknown channel difference type: ", reflect.TypeOf(d))
				}
			}
		}

		time.Sleep(GET_CHANNEL_DIFF_INTERVAL)
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
		}
	case OnCommand:
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if args != "" {
				return c.AddMessageHandler("cmd:"+args, h, filters...)
			}
			return c.AddMessageHandler(OnNewMessage, h, filters...)
		}
	case OnEdit, OnEditMessage:
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if args != "" {
				return c.AddEditHandler(args, h)
			}
			return c.AddEditHandler(OnEditMessage, h)
		}
	case OnDelete, OnDeleteMessage:
		if h, ok := handler.(func(m *DeleteMessage) error); ok {
			if args != "" {
				return c.AddDeleteHandler(args, h)
			}
			return c.AddDeleteHandler(OnDeleteMessage, h)
		}
	case OnAlbum:
		if h, ok := handler.(func(m *Album) error); ok {
			return c.AddAlbumHandler(h)
		}
	case OnInline, OnInlineQuery:
		if h, ok := handler.(func(m *InlineQuery) error); ok {
			if args != "" {
				return c.AddInlineHandler(args, h)
			}
			return c.AddInlineHandler(OnInlineQuery, h)
		}
	case OnChoosenInline:
		if h, ok := handler.(func(m *InlineSend) error); ok {
			return c.AddInlineSendHandler(h)
		}
	case OnCallback, OnCallbackQuery:
		if h, ok := handler.(func(m *CallbackQuery) error); ok {
			if args != "" {
				return c.AddCallbackHandler(args, h)
			}
			return c.AddCallbackHandler(OnCallbackQuery, h)
		}
	case OnInlineCallback, OnInlineCallbackQuery, "inlinecallback":
		if h, ok := handler.(func(m *InlineCallbackQuery) error); ok {
			if args != "" {
				return c.AddInlineCallbackHandler(args, h)
			}
			return c.AddInlineCallbackHandler(OnInlineCallbackQuery, h)
		}
	case OnParticipant:
		if h, ok := handler.(func(m *ParticipantUpdate) error); ok {
			return c.AddParticipantHandler(h)
		}
	case OnRaw:
		if h, ok := handler.(func(m Update, c *Client) error); ok {
			return c.AddRawHandler(nil, h)
		}
	default:
		if update, ok := pattern.(Update); ok {
			if h, ok := handler.(func(m Update, c *Client) error); ok {
				return c.AddRawHandler(update, h)
			}
		}
	}

	return nil
}
