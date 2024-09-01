// Copyright (c) 2024, amarnathcjd

package telegram

import (
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

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
type CallbackHandler func(m *CallbackQuery) error
type InlineCallbackHandler func(m *InlineCallbackQuery) error
type ParticipantHandler func(m *ParticipantUpdate) error
type RawHandler func(m Update, c *Client) error

var EndGroup = errors.New("end-group-trigger")

type messageHandle struct {
	Pattern     interface{}
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
	waitExit  chan struct{}
	messages  []*NewMessage
	groupedId int64
}

func (a *albumBox) Wait() {
	time.Sleep(600 * time.Millisecond)
	a.waitExit <- struct{}{}
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
	Pattern     interface{}
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
	Pattern     interface{}
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
	Pattern     interface{}
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

type callbackHandle struct {
	Pattern     interface{}
	Handler     CallbackHandler
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
	Pattern     interface{}
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

type UpdateDispatcher struct {
	messageHandles        map[string][]*messageHandle
	inlineHandles         map[string][]*inlineHandle
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
}

// creates and populates a new UpdateDispatcher
func (c *Client) NewUpdateDispatcher() {
	c.dispatcher = &UpdateDispatcher{}
	c.dispatcher.SortTrigger()
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

func (c *Client) handleMessageUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		if msg.GroupedID != 0 {
			c.handleAlbum(*msg)
		}

		for group, handlers := range c.dispatcher.messageHandles {
			go func(group string, handlers []*messageHandle) {
				for _, handler := range handlers {
					if handler.IsMatch(msg.Message, c) {
						handle := func(h *messageHandle) error {
							packed := packMessage(c, msg)
							if c.runFilterChain(packed, h.Filters) {
								defer c.NewRecovery()()
								if err := h.Handler(packed); err != nil {
									if errors.Is(err, EndGroup) {
										return err
									}
									c.Log.Error(errors.Wrap(err, "updates.dispatcher.message"))
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

			}(group, handlers)
		}

	case *MessageService:
		for group, handler := range c.dispatcher.actionHandles {
			for _, h := range handler {
				handle := func(h *chatActionHandle) error {
					defer c.NewRecovery()()
					if err := h.Handler(packMessage(c, msg)); err != nil {
						if errors.Is(err, EndGroup) {
							return err
						}
						c.Log.Error(errors.Wrap(err, "updates.dispatcher.action"))
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
	if group, ok := c.dispatcher.activeAlbums[message.GroupedID]; ok {
		group.Add(packMessage(c, &message))
	} else {
		abox := &albumBox{
			waitExit:  make(chan struct{}),
			messages:  []*NewMessage{packMessage(c, &message)},
			groupedId: message.GroupedID,
		}
		if c.dispatcher.activeAlbums == nil {
			c.dispatcher.activeAlbums = make(map[int64]*albumBox)
		}
		c.dispatcher.activeAlbums[message.GroupedID] = abox
		go func() {
			<-abox.waitExit

			for gp, handlers := range c.dispatcher.albumHandles {
				for _, handler := range handlers {
					handle := func(h *albumHandle) error {
						if err := h.Handler(&Album{
							GroupedID: abox.groupedId,
							Messages:  abox.messages,
							Client:    c,
						}); err != nil {
							if errors.Is(err, EndGroup) {
								return err
							}
							c.Log.Error(errors.Wrap(err, "updates.dispatcher.album"))
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

			delete(c.dispatcher.activeAlbums, message.GroupedID)
		}()
		go abox.Wait()
	}
}

func (c *Client) handleMessageUpdateWith(m Message, pts int32) {
	switch msg := m.(type) {
	case *MessageObj:
		if c.IdInCache(c.GetPeerID(msg.FromID)) && c.IdInCache(c.GetPeerID(msg.PeerID)) {
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

func (c *Client) handleEditUpdate(update Message) {
	if msg, ok := update.(*MessageObj); ok {
		for group, handlers := range c.dispatcher.messageEditHandles {
			for _, handler := range handlers {
				if handler.IsMatch(msg.Message) {
					handle := func(h *messageEditHandle) error {
						packed := packMessage(c, msg)
						defer c.NewRecovery()()
						if c.runFilterChain(packed, h.Filters) {
							if err := h.Handler(packed); err != nil {
								if errors.Is(err, EndGroup) {
									return err
								}
								c.Log.Error(errors.Wrap(err, "updates.dispatcher.editMessage"))
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
	for group, handlers := range c.dispatcher.callbackHandles {
		for _, handler := range handlers {
			if handler.IsMatch(update.Data) {
				handle := func(h *callbackHandle) error {
					packed := packCallbackQuery(c, update)
					defer c.NewRecovery()()
					if err := h.Handler(packed); err != nil {
						if errors.Is(err, EndGroup) {
							return err
						}
						c.Log.Error(errors.Wrap(err, "updates.dispatcher.callbackQuery"))
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
	for group, handlers := range c.dispatcher.inlineCallbackHandles {
		for _, handler := range handlers {
			if handler.IsMatch(update.Data) {
				handle := func(h *inlineCallbackHandle) error {
					packed := packInlineCallbackQuery(c, update)
					defer c.NewRecovery()()
					if err := h.Handler(packed); err != nil {
						if errors.Is(err, EndGroup) {
							return err
						}
						c.Log.Error(errors.Wrap(err, "updates.dispatcher.inlineCallbackQuery"))
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
	for group, handlers := range c.dispatcher.participantHandles {
		for _, handler := range handlers {
			handle := func(h *participantHandle) error {
				packed := packChannelParticipant(c, update)
				defer c.NewRecovery()()
				if err := h.Handler(packed); err != nil {
					if errors.Is(err, EndGroup) {
						return err
					}
					c.Log.Error(errors.Wrap(err, "updates.dispatcher.participant"))
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
	for group, handlers := range c.dispatcher.inlineHandles {
		for _, handler := range handlers {
			if handler.IsMatch(update.Query) {
				handle := func(h *inlineHandle) error {
					packed := packInlineQuery(c, update)
					defer c.NewRecovery()()
					if err := h.Handler(packed); err != nil {
						if errors.Is(err, EndGroup) {
							return err
						}
						c.Log.Error(errors.Wrap(err, "updates.dispatcher.inlineQuery"))
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

func (c *Client) handleDeleteUpdate(update Update) {
	for group, handlers := range c.dispatcher.messageDeleteHandles {
		for _, handler := range handlers {
			handle := func(h *messageDeleteHandle) error {
				packed := packDeleteMessage(c, update)
				defer c.NewRecovery()()
				if err := h.Handler(packed); err != nil {
					if errors.Is(err, EndGroup) {
						return err
					}
					c.Log.Error(errors.Wrap(err, "updates.dispatcher.deleteMessage"))
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
			if reflect.TypeOf(update) == reflect.TypeOf(handler.updateType) {
				handle := func(h *rawHandle) error {
					defer c.NewRecovery()()
					if err := h.Handler(update, c); err != nil {
						if errors.Is(err, EndGroup) {
							return err
						}
						c.Log.Error(errors.Wrap(err, "updates.dispatcher.rawUpdate"))
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

func (h *messageHandle) IsMatch(text string, c *Client) bool {
	switch Pattern := h.Pattern.(type) {
	case string:
		if Pattern == OnNewMessage {
			return true
		}

		if strings.HasPrefix(Pattern, "cmd:") {
			//(?i)^[!/-]ping(?: |$|@botusername)(.*)$
			Pattern = "(?i)^[!/]" + strings.TrimPrefix(Pattern, "cmd:")
			if me := c.Me(); me != nil && me.Username != "" && me.Bot {
				Pattern += "(?: |$|@" + me.Username + ")(.*)"
			} else {
				Pattern += "(.*)"
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

func (c *Client) runFilterChain(m *NewMessage, filters []Filter) bool {
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
	Private, Group, Channel, Media, Command, Reply, Forward, FromBot, Blacklist, Mention bool
	Users, Chats                                                                         []int64
	Func                                                                                 func(m *NewMessage) bool
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
)

func (c *Client) AddMessageHandler(pattern interface{}, handler MessageHandler, filters ...Filter) Handle {
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

func (c *Client) AddDeleteHandler(pattern interface{}, handler func(d *DeleteMessage) error) Handle {
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
func (c *Client) AddEditHandler(pattern interface{}, handler MessageHandler, filters ...Filter) Handle {
	if c.dispatcher.messageEditHandles == nil {
		c.dispatcher.messageEditHandles = make(map[string][]*messageEditHandle)
	}

	handle := messageEditHandle{Pattern: pattern, Handler: handler, sortTrigger: c.dispatcher.sortTrigger}
	c.dispatcher.messageEditHandles["default"] = append(c.dispatcher.messageEditHandles["default"], &handle)
	return c.dispatcher.messageEditHandles["default"][len(c.dispatcher.messageEditHandles["default"])-1]
}

// Handle updates categorized as "UpdateBotInlineQuery"
//
// Included Updates:
//   - Inline Query
func (c *Client) AddInlineHandler(pattern interface{}, handler InlineHandler) Handle {
	if c.dispatcher.inlineHandles == nil {
		c.dispatcher.inlineHandles = make(map[string][]*inlineHandle)
	}

	handle := inlineHandle{Pattern: pattern, Handler: handler, sortTrigger: c.dispatcher.sortTrigger}
	c.dispatcher.inlineHandles["default"] = append(c.dispatcher.inlineHandles["default"], &handle)
	return c.dispatcher.inlineHandles["default"][len(c.dispatcher.inlineHandles["default"])-1]
}

// Handle updates categorized as "UpdateBotCallbackQuery"
//
// Included Updates:
//   - Callback Query
func (c *Client) AddCallbackHandler(pattern interface{}, handler CallbackHandler) Handle {
	if c.dispatcher.callbackHandles == nil {
		c.dispatcher.callbackHandles = make(map[string][]*callbackHandle)
	}

	handle := callbackHandle{Pattern: pattern, Handler: handler, sortTrigger: c.dispatcher.sortTrigger}
	c.dispatcher.callbackHandles["default"] = append(c.dispatcher.callbackHandles["default"], &handle)
	return c.dispatcher.callbackHandles["default"][len(c.dispatcher.callbackHandles["default"])-1]
}

// Handle updates categorized as "UpdateInlineBotCallbackQuery"
//
// Included Updates:
//   - Inline Callback Query
func (c *Client) AddInlineCallbackHandler(pattern interface{}, handler InlineCallbackHandler) Handle {
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
func HandleIncomingUpdates(u interface{}, c *Client) bool {
UpdateTypeSwitching:
	switch upd := u.(type) {
	case *UpdatesObj:
		go c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
		for _, update := range upd.Updates {
			switch update := update.(type) {
			case *UpdateNewMessage:
				go c.handleMessageUpdate(update.Message)
			case *UpdateNewChannelMessage:
				go c.handleMessageUpdate(update.Message)
			case *UpdateNewScheduledMessage:
				go c.handleMessageUpdate(update.Message)
			case *UpdateEditMessage:
				go c.handleEditUpdate(update.Message)
			case *UpdateEditChannelMessage:
				go c.handleEditUpdate(update.Message)
			case *UpdateBotInlineQuery:
				go c.handleInlineUpdate(update)
			case *UpdateBotCallbackQuery:
				go c.handleCallbackUpdate(update)
			case *UpdateInlineBotCallbackQuery:
				go c.handleInlineCallbackUpdate(update)
			case *UpdateChannelParticipant:
				go c.handleParticipantUpdate(update)
			case *UpdateDeleteChannelMessages:
				go c.handleDeleteUpdate(update)
			case *UpdateDeleteMessages:
				go c.handleDeleteUpdate(update)
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
		c.Log.Debug("update gap is too long, requesting getState")
		c.UpdatesGetState()
	default:
		c.Log.Debug("skipping unhanded update (", reflect.TypeOf(u), "): ", c.JSON(u))
	}
	return true
}

func (c *Client) GetDifference(Pts, Limit int32) (Message, error) {
	c.Logger.Debug("updates.getDifference: (pts: ", Pts, " limit: ", Limit, ")")

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

type ev interface{}

var (
	OnMessage        ev = "message"
	OnCommand        ev = "command"
	OnEdit           ev = "edit"
	OnDelete         ev = "delete"
	OnAlbum          ev = "album"
	OnInline         ev = "inline"
	OnCallback       ev = "callback"
	OnInlineCallback ev = "inlineCallback"
	OnParticipant    ev = "participant"
	OnRaw            ev = "raw"
)

func (c *Client) On(pattern any, handler interface{}, filters ...Filter) Handle {
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
	case OnMessage:
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if args != "" {
				return c.AddMessageHandler(args, h, filters...)
			}
			return c.AddMessageHandler(OnNewMessage, h, filters...)
		}
	case OnCommand:
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if args != "" {
				return c.AddMessageHandler("cmd:"+args, h, append(filters, FilterCommand)...)
			}
			return c.AddMessageHandler(OnNewMessage, h, append(filters, FilterCommand)...)
		}
	case OnEdit:
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if args != "" {
				return c.AddEditHandler(args, h)
			}
			return c.AddEditHandler(OnEditMessage, h)
		}
	case OnDelete:
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
	case OnInline:
		if h, ok := handler.(func(m *InlineQuery) error); ok {
			if args != "" {
				return c.AddInlineHandler(args, h)
			}
			return c.AddInlineHandler(OnInlineQuery, h)
		}
	case OnCallback:
		if h, ok := handler.(func(m *CallbackQuery) error); ok {
			if args != "" {
				return c.AddCallbackHandler(args, h)
			}
			return c.AddCallbackHandler(OnCallbackQuery, h)
		}
	case OnInlineCallback:
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
