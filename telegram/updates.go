// Copyright (c) 2025, amarnathcjd

package telegram

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Middleware func(MessageHandler) MessageHandler

type LifecycleHooks struct {
	BeforeHandler func(*NewMessage)
	AfterHandler  func(*NewMessage, error)
	OnError       func(error, *NewMessage)
}

type HandlerMetrics struct {
	TotalCalls  atomic.Int64
	Errors      atomic.Int64
	TotalTimeNs atomic.Int64
}

func (m *HandlerMetrics) RecordCall(duration time.Duration, err error) {
	m.TotalCalls.Add(1)
	if err != nil {
		m.Errors.Add(1)
	}
	m.TotalTimeNs.Add(int64(duration))
}

func (m *HandlerMetrics) AvgDuration() time.Duration {
	calls := m.TotalCalls.Load()
	if calls == 0 {
		return 0
	}
	return time.Duration(m.TotalTimeNs.Load() / calls)
}

func (m *HandlerMetrics) ErrorRate() float64 {
	calls := m.TotalCalls.Load()
	if calls == 0 {
		return 0
	}
	return float64(m.Errors.Load()) / float64(calls)
}

type lruCache struct {
	sync.Mutex
	maxSize int
	items   map[int64]*list.Element
	list    *list.List
}

type lruEntry struct {
	key       int64
	timestamp time.Time
}

func newLRUCache(maxSize int) *lruCache {
	return &lruCache{
		maxSize: maxSize,
		items:   make(map[int64]*list.Element),
		list:    list.New(),
	}
}

func (c *lruCache) Add(key int64) {
	c.Lock()
	defer c.Unlock()

	if elem, exists := c.items[key]; exists {
		c.list.MoveToFront(elem)
		elem.Value.(*lruEntry).timestamp = time.Now()
		return
	}

	entry := &lruEntry{key: key, timestamp: time.Now()}
	elem := c.list.PushFront(entry)
	c.items[key] = elem

	if c.list.Len() > c.maxSize {
		oldest := c.list.Back()
		if oldest != nil {
			c.list.Remove(oldest)
			delete(c.items, oldest.Value.(*lruEntry).key)
		}
	}
}

func (c *lruCache) Contains(key int64) bool {
	c.Lock()
	defer c.Unlock()
	_, exists := c.items[key]
	return exists
}

type patternCache struct {
	sync.RWMutex
	patterns map[string]*regexp.Regexp
}

func newPatternCache() *patternCache {
	return &patternCache{
		patterns: make(map[string]*regexp.Regexp),
	}
}

func (c *patternCache) Get(pattern string) *regexp.Regexp {
	c.RLock()
	if regex, exists := c.patterns[pattern]; exists {
		c.RUnlock()
		return regex
	}
	c.RUnlock()

	c.Lock()
	defer c.Unlock()
	if regex, exists := c.patterns[pattern]; exists {
		return regex
	}
	regex := regexp.MustCompile(pattern)
	c.patterns[pattern] = regex
	return regex
}

type EventType string

const (
	EventMessage        EventType = "message"
	EventNewMessage     EventType = "newmessage"
	EventCommand        EventType = "command"
	EventEdit           EventType = "edit"
	EventEditMessage    EventType = "editmessage"
	EventDelete         EventType = "delete"
	EventDeleteMessage  EventType = "deletemessage"
	EventAlbum          EventType = "album"
	EventInline         EventType = "inline"
	EventInlineQuery    EventType = "inlinequery"
	EventCallback       EventType = "callback"
	EventCallbackQuery  EventType = "callbackquery"
	EventInlineCallback EventType = "inlinecallback"
	EventChosenInline   EventType = "choseninline"
	EventParticipant    EventType = "participant"
	EventJoinRequest    EventType = "joinrequest"
	EventAction         EventType = "action"
	EventRaw            EventType = "raw"
)

func applyMiddlewares(handler MessageHandler, middlewares []Middleware) MessageHandler {
	if len(middlewares) == 0 {
		return handler
	}
	final := handler
	for i := len(middlewares) - 1; i >= 0; i-- {
		final = middlewares[i](final)
	}
	return final
}

// WithMiddleware wraps a handler with the provided middlewares
func WithMiddleware(handler MessageHandler, middlewares ...Middleware) MessageHandler {
	return applyMiddlewares(handler, middlewares)
}

// AnyFilter creates a filter that matches if any of the provided filters match
func AnyFilter(filters ...Filter) Filter {
	return Filter{orFilters: filters}
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
type PendingJoinHandler func(m *JoinRequestUpdate) error
type RawHandler func(m Update, c *Client) error
type E2EHandler func(update Update, c *Client) error

var ErrEndGroup = errors.New("[EndGroup] end of handler propagation")

const (
	ConversationGroup = -1
	DefaultGroup      = 0
)

type Handle interface {
	SetGroup(group int) Handle
	GetGroup() int
	SetPriority(priority int) Handle
	GetPriority() int
}

type baseHandle struct {
	Group             int
	priority          int
	name              string
	enabled           bool
	metrics           *HandlerMetrics
	onGroupChanged    func(int, int)
	onPriorityChanged func()
}

func (h *baseHandle) SetGroup(group int) Handle {
	oldGroup := h.Group
	h.Group = group
	if h.onGroupChanged != nil {
		h.onGroupChanged(oldGroup, group)
	}
	return h
}

func (h *baseHandle) GetGroup() int {
	return h.Group
}

func (h *baseHandle) SetPriority(priority int) Handle {
	h.priority = priority
	if h.onPriorityChanged != nil {
		h.onPriorityChanged()
	}
	return h
}

func (h *baseHandle) GetPriority() int {
	return h.priority
}

type messageHandle struct {
	baseHandle
	Pattern     any
	Handler     MessageHandler
	Filters     []Filter
	middlewares []Middleware
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

type e2eHandle struct {
	baseHandle
	Handler E2EHandler
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

			if gp == DefaultGroup {
				go func() {
					err := handle(handler)
					if err != nil {
						if errors.Is(err, ErrEndGroup) {
							return
						}

						c.Log.WithError(err).Error("[AlbumHandler]")
					}
				}()
			} else {
				if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
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

type openChat struct {
	sync.RWMutex
	accessHash int64
	closeChan  chan struct{}
	lastPts    int32
	timeout    int32
}

type channelState struct {
	pts        int32
	accessHash int64
	isOpen     bool
}

// UpdateState represents the current update state
type UpdateState struct {
	Pts  int32
	Qts  int32
	Seq  int32
	Date int32
}

type UpdateDispatcher struct {
	sync.RWMutex
	messageHandles        map[int][]*messageHandle
	inlineHandles         map[int][]*inlineHandle
	inlineSendHandles     map[int][]*inlineSendHandle
	callbackHandles       map[int][]*callbackHandle
	inlineCallbackHandles map[int][]*inlineCallbackHandle
	participantHandles    map[int][]*participantHandle
	joinRequestHandles    map[int][]*joinRequestHandle
	messageEditHandles    map[int][]*messageEditHandle
	actionHandles         map[int][]*chatActionHandle
	messageDeleteHandles  map[int][]*messageDeleteHandle
	albumHandles          map[int][]*albumHandle
	rawHandles            map[int][]*rawHandle
	e2eHandles            map[int][]*e2eHandle
	activeAlbums          map[int64]*albumBox
	logger                Logger
	openChats             map[int64]*openChat
	nextUpdatesDeadline   time.Time
	lastUpdateTimeNano    atomic.Int64
	state                 UpdateState
	channelStates         map[int64]*channelState
	pendingGaps           map[int32]time.Time
	processedUpdatesLRU   *lruCache
	recoveringDifference  bool
	recoveringChannels    map[int64]bool
	stopChan              chan struct{}
	patternCache          *patternCache
	lifecycleHooks        *LifecycleHooks
}

func (d *UpdateDispatcher) SetPts(pts int32) {
	d.Lock()
	defer d.Unlock()
	d.state.Pts = pts
}

func (d *UpdateDispatcher) GetPts() int32 {
	d.RLock()
	defer d.RUnlock()
	return d.state.Pts
}

func (d *UpdateDispatcher) SetQts(qts int32) {
	d.Lock()
	defer d.Unlock()
	d.state.Qts = qts
}

func (d *UpdateDispatcher) GetQts() int32 {
	d.RLock()
	defer d.RUnlock()
	return d.state.Qts
}

func (d *UpdateDispatcher) SetSeq(seq int32) {
	d.Lock()
	defer d.Unlock()
	d.state.Seq = seq
}

func (d *UpdateDispatcher) GetSeq() int32 {
	d.RLock()
	defer d.RUnlock()
	return d.state.Seq
}

func (d *UpdateDispatcher) SetDate(date int32) {
	d.Lock()
	defer d.Unlock()
	d.state.Date = date
}

func (d *UpdateDispatcher) GetDate() int32 {
	d.RLock()
	defer d.RUnlock()
	return d.state.Date
}

func (d *UpdateDispatcher) SetChannelPts(channelID int64, pts int32) {
	d.Lock()
	defer d.Unlock()
	if d.channelStates == nil {
		d.channelStates = make(map[int64]*channelState)
	}
	if state, ok := d.channelStates[channelID]; ok {
		state.pts = pts
	} else {
		d.channelStates[channelID] = &channelState{pts: pts}
	}
}

func (d *UpdateDispatcher) GetChannelPts(channelID int64) int32 {
	d.RLock()
	defer d.RUnlock()
	if d.channelStates == nil {
		return 0
	}
	if state, ok := d.channelStates[channelID]; ok {
		return state.pts
	}
	return 0
}

func (u *UpdateDispatcher) UpdateLastUpdateTime() {
	u.lastUpdateTimeNano.Store(time.Now().UnixNano())
}

func (u *UpdateDispatcher) getLastUpdateTime() time.Time {
	return time.Unix(0, u.lastUpdateTimeNano.Load())
}

// TryMarkUpdateProcessed atomically checks if an update was processed and marks it if not.
// Returns true if this call marked it (first processor), false if already processed.
func (d *UpdateDispatcher) TryMarkUpdateProcessed(updateID int64) bool {
	if d.processedUpdatesLRU == nil {
		return true
	}
	if d.processedUpdatesLRU.Contains(updateID) {
		return false
	}
	d.processedUpdatesLRU.Add(updateID)
	return true
}

func (c *Client) NewUpdateDispatcher(sessionName ...string) {
	c.dispatcher = &UpdateDispatcher{
		logger: c.Log.WithPrefix("gogram " +
			lp("updates", getVariadic(sessionName, ""))),
		channelStates:         make(map[int64]*channelState),
		pendingGaps:           make(map[int32]time.Time),
		processedUpdatesLRU:   newLRUCache(15000),
		stopChan:              make(chan struct{}),
		messageHandles:        make(map[int][]*messageHandle),
		inlineHandles:         make(map[int][]*inlineHandle),
		inlineSendHandles:     make(map[int][]*inlineSendHandle),
		callbackHandles:       make(map[int][]*callbackHandle),
		inlineCallbackHandles: make(map[int][]*inlineCallbackHandle),
		participantHandles:    make(map[int][]*participantHandle),
		joinRequestHandles:    make(map[int][]*joinRequestHandle),
		messageEditHandles:    make(map[int][]*messageEditHandle),
		actionHandles:         make(map[int][]*chatActionHandle),
		messageDeleteHandles:  make(map[int][]*messageDeleteHandle),
		albumHandles:          make(map[int][]*albumHandle),
		rawHandles:            make(map[int][]*rawHandle),
		e2eHandles:            make(map[int][]*e2eHandle),
		activeAlbums:          make(map[int64]*albumBox),
		patternCache:          newPatternCache(),
		lifecycleHooks:        &LifecycleHooks{},
	}
	c.dispatcher.lastUpdateTimeNano.Store(time.Now().UnixNano())
	c.dispatcher.logger.Debug("update dispatcher initialized")

	go c.monitorNoUpdatesTimeout()
}

func (c *Client) RemoveHandle(handle Handle) error {
	if c.dispatcher == nil || c == nil {
		return errors.New("[DispatcherNotInitialized] dispatcher is not initialized")
	}

	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()

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
	case *inlineSendHandle:
		removeHandleFromMap(h, c.dispatcher.inlineSendHandles)
	default:
		return errors.New("[InvalidHandlerType] handle type not supported")
	}

	return nil
}

func removeHandleFromMap[T any](handle T, handlesMap map[int][]T) {
	for key := range handlesMap {
		handles := handlesMap[key]
		for i := len(handles) - 1; i >= 0; i-- {
			if reflect.DeepEqual(handles[i], handle) {
				handlesMap[key] = slices.Delete(handles, i, i+1)
				return
			}
		}
	}
}

// ---------------------------- Handle Functions ----------------------------

func (c *Client) handleMessageUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		if msg.Out {
			return
		}
		updateID := int64(msg.ID)
		peerID := c.GetPeerID(msg.PeerID)
		if peerID == 0 {
			peerID = c.GetPeerID(msg.FromID)
		}
		if peerID != 0 {
			updateID = (peerID << 32) | int64(msg.ID)
		}

		if !c.dispatcher.TryMarkUpdateProcessed(updateID) {
			c.dispatcher.logger.Trace("duplicate message update skipped: %d", updateID)
			return
		}

		if msg.GroupedID != 0 {
			c.handleAlbum(*msg)
		}

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

		c.dispatcher.RLock()
		convHandlers := c.dispatcher.messageHandles[ConversationGroup]
		allMessageHandles := make(map[int][]*messageHandle)
		maps.Copy(allMessageHandles, c.dispatcher.messageHandles)
		c.dispatcher.RUnlock()

		if len(convHandlers) > 0 {
			for _, handler := range convHandlers {
				if handler.IsMatch(msg.Message, c) {
					if err := handle(handler); err != nil {
						if errors.Is(err, ErrEndGroup) {
							return
						}
						c.dispatcher.logger.WithError(err).Error("[NewMessageHandler]")
					}
				}
			}
		}

		type groupWithHandlers struct {
			group    int
			handlers []*messageHandle
		}

		var groupsToProcess []groupWithHandlers

		for group, handlers := range allMessageHandles {
			if group == ConversationGroup || group == DefaultGroup {
				continue
			}

			groupsToProcess = append(groupsToProcess, groupWithHandlers{
				group:    group,
				handlers: handlers,
			})
		}

		sort.Slice(groupsToProcess, func(i, j int) bool {
			return groupsToProcess[i].group < groupsToProcess[j].group
		})

		for _, gp := range groupsToProcess {
			for _, handler := range gp.handlers {
				if handler.IsMatch(msg.Message, c) {
					if err := handle(handler); err != nil {
						if errors.Is(err, ErrEndGroup) {
							break
						}
						c.dispatcher.logger.WithError(err).Error("[NewMessageHandler]")
					}
				}
			}
		}

		if defaultHandlers, ok := allMessageHandles[DefaultGroup]; ok {
			var wg sync.WaitGroup
			for _, handler := range defaultHandlers {
				if handler.IsMatch(msg.Message, c) {
					wg.Add(1)
					go func(h *messageHandle) {
						defer wg.Done()
						if err := handle(h); err != nil && !errors.Is(err, ErrEndGroup) {
							c.dispatcher.logger.WithError(err).Error("[NewMessageHandler]")
						}
					}(handler)
				}
			}
			wg.Wait()
		}

	case *MessageService:
		if msg.Out {
			return
		}
		packed := packMessage(c, msg)

		c.dispatcher.RLock()
		actionHandles := make(map[int][]*chatActionHandle)
		maps.Copy(actionHandles, c.dispatcher.actionHandles)
		c.dispatcher.RUnlock()

		for group, handler := range actionHandles {
			for _, h := range handler {
				handle := func(h *chatActionHandle) error {
					defer c.NewRecovery()()
					return h.Handler(packed)
				}

				if group == DefaultGroup {
					go func() {
						err := handle(h)
						if err != nil {
							if errors.Is(err, ErrEndGroup) {
								return
							}
							c.Log.WithError(err).Error("[ChatActionHandler]")
						}
					}()
				} else {
					if err := handle(h); err != nil && errors.Is(err, ErrEndGroup) {
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

func (c *Client) fetchPeersBeforeUpdate(m Message, pts int32) {
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

		updatedMessage, err := c.GetDifference(pts, 1)
		if err != nil {
			c.Log.WithError(err).Error("[GetDifference] failed to get difference")
		}
		if updatedMessage != nil {
			c.handleMessageUpdate(updatedMessage)
		}
	}
}

func (c *Client) handleEditUpdate(update Message) {
	if msg, ok := update.(*MessageObj); ok {
		packed := packMessage(c, msg)

		c.dispatcher.RLock()
		editHandles := make(map[int][]*messageEditHandle)
		maps.Copy(editHandles, c.dispatcher.messageEditHandles)
		c.dispatcher.RUnlock()

		for group, handlers := range editHandles {
			for _, handler := range handlers {
				if handler.IsMatch(msg.Message) {
					handle := func(h *messageEditHandle) error {
						if h.runFilterChain(packed, h.Filters) {
							defer c.NewRecovery()()
							return h.Handler(packed)
						}
						return nil
					}

					if group == DefaultGroup {
						go func() {
							err := handle(handler)
							if err != nil {
								if errors.Is(err, ErrEndGroup) {
									return
								}
								c.Log.WithError(err).Error("[EditMessageHandler]")
							}
						}()
					} else {
						if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
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

	c.dispatcher.RLock()
	callbackHandles := make(map[int][]*callbackHandle)
	maps.Copy(callbackHandles, c.dispatcher.callbackHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range callbackHandles {
		for _, handler := range handlers {
			if handler.IsMatch(update.Data) {
				handle := func(h *callbackHandle) error {
					if h.runFilterChain(packed, h.Filters) {
						defer c.NewRecovery()()
						return h.Handler(packed)
					}
					return nil
				}

				if group == DefaultGroup {
					go func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, ErrEndGroup) {
								return
							}
							c.Log.WithError(err).Error("[CallbackQueryHandler]")
						}
					}()
				} else {
					if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
						break
					}
				}
			}
		}
	}
}

func (c *Client) handleInlineCallbackUpdate(update *UpdateInlineBotCallbackQuery) {
	packed := packInlineCallbackQuery(c, update)

	c.dispatcher.RLock()
	inlineCallbackHandles := make(map[int][]*inlineCallbackHandle)
	maps.Copy(inlineCallbackHandles, c.dispatcher.inlineCallbackHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range inlineCallbackHandles {
		for _, handler := range handlers {
			if handler.IsMatch(update.Data) {
				handle := func(h *inlineCallbackHandle) error {
					defer c.NewRecovery()()
					return h.Handler(packed)
				}

				if group == DefaultGroup {
					go func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, ErrEndGroup) {
								return
							}
							c.Log.WithError(err).Error("[InlineCallbackHandler]")
						}
					}()
				} else {
					if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
						break
					}
				}
			}
		}
	}
}

func (c *Client) handleParticipantUpdate(update *UpdateChannelParticipant) {
	packed := packChannelParticipant(c, update)

	c.dispatcher.RLock()
	participantHandles := make(map[int][]*participantHandle)
	maps.Copy(participantHandles, c.dispatcher.participantHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range participantHandles {
		for _, handler := range handlers {
			handle := func(h *participantHandle) error {
				defer c.NewRecovery()()
				return h.Handler(packed)
			}

			if group == DefaultGroup {
				go func() {
					err := handle(handler)
					if err != nil {
						if errors.Is(err, ErrEndGroup) {
							return
						}
						c.Log.WithError(err).Error("[ParticipantUpdateHandler]")
					}
				}()
			} else {
				if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
					break
				}
			}
		}
	}
}

func (c *Client) handleInlineUpdate(update *UpdateBotInlineQuery) {
	packed := packInlineQuery(c, update)

	c.dispatcher.RLock()
	inlineHandles := make(map[int][]*inlineHandle)
	maps.Copy(inlineHandles, c.dispatcher.inlineHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range inlineHandles {
		for _, handler := range handlers {
			if handler.IsMatch(update.Query) {
				handle := func(h *inlineHandle) error {
					defer c.NewRecovery()()
					return h.Handler(packed)
				}

				if group == DefaultGroup {
					go func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, ErrEndGroup) {
								return
							}
							c.Log.WithError(err).Error("[InlineQueryHandler]")
						}
					}()
				} else {
					if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
						break
					}
				}
			}
		}
	}
}

func (c *Client) handleInlineSendUpdate(update *UpdateBotInlineSend) {
	packed := packInlineSend(c, update)

	c.dispatcher.RLock()
	inlineSendHandles := make(map[int][]*inlineSendHandle)
	maps.Copy(inlineSendHandles, c.dispatcher.inlineSendHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range inlineSendHandles {
		for _, handler := range handlers {
			handle := func(h *inlineSendHandle) error {
				defer c.NewRecovery()()
				return h.Handler(packed)
			}

			if group == DefaultGroup {
				go func() {
					err := handle(handler)
					if err != nil {
						if errors.Is(err, ErrEndGroup) {
							return
						}
						c.Log.WithError(err).Error("[InlineSendHandler]")
					}
				}()
			} else {
				if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
					break
				}
			}
		}
	}
}

func (c *Client) handleDeleteUpdate(update Update) {
	packed := packDeleteMessage(c, update)

	c.dispatcher.RLock()
	messageDeleteHandles := make(map[int][]*messageDeleteHandle)
	maps.Copy(messageDeleteHandles, c.dispatcher.messageDeleteHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range messageDeleteHandles {
		for _, handler := range handlers {
			handle := func(h *messageDeleteHandle) error {
				defer c.NewRecovery()()
				return h.Handler(packed)
			}

			if group == DefaultGroup {
				go func() {
					err := handle(handler)
					if err != nil {
						if errors.Is(err, ErrEndGroup) {
							return
						}
						c.Log.WithError(err).Error("[DeleteMessageHandler]")
					}
				}()
			} else {
				if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
					break
				}
			}
		}
	}
}

func (c *Client) handleJoinRequestUpdate(update Update) {
	var packed *JoinRequestUpdate
	switch u := update.(type) {
	case *UpdateBotChatInviteRequester:
		packed = packBotChatJoinRequest(c, u)
	case *UpdatePendingJoinRequests:
		packed = packJoinRequest(c, u)
	}

	c.dispatcher.RLock()
	joinRequestHandles := make(map[int][]*joinRequestHandle)
	maps.Copy(joinRequestHandles, c.dispatcher.joinRequestHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range joinRequestHandles {
		for _, handler := range handlers {
			handle := func(h *joinRequestHandle) error {
				defer c.NewRecovery()()
				return h.Handler(packed)
			}

			if group == DefaultGroup {
				go func() {
					err := handle(handler)
					if err != nil {
						if errors.Is(err, ErrEndGroup) {
							return
						}
						c.Log.WithError(err).Error("[JoinRequestHandler]")
					}
				}()
			} else {
				if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
					break
				}
			}
		}
	}
}

func (c *Client) handleRawUpdate(update Update) {
	c.dispatcher.RLock()
	rawHandles := make(map[int][]*rawHandle)
	maps.Copy(rawHandles, c.dispatcher.rawHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range rawHandles {
		for _, handler := range handlers {
			if handler == nil || handler.Handler == nil {
				continue
			}
			if reflect.TypeOf(update) == reflect.TypeOf(handler.updateType) || handler.updateType == nil {
				handle := func(h *rawHandle) error {
					defer c.NewRecovery()()
					return h.Handler(update, c)
				}

				if group == DefaultGroup {
					go func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, ErrEndGroup) {
								return
							}
							c.Log.WithError(err).Error("[RawUpdateHandler]")
						}
					}()
				} else {
					if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
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
	if h.Pattern == nil {
		return false
	}
	switch Pattern := h.Pattern.(type) {
	case string:
		if Pattern == OnNewMessage || Pattern == OnMessage {
			return true
		}

		if after, ok := strings.CutPrefix(Pattern, "cmd:"); ok {
			prefixes := c.CommandPrefixes()
			if prefixes == "" {
				prefixes = "/!"
			}
			escapedPrefixes := regexp.QuoteMeta(prefixes)
			Pattern = "(?i)^[" + escapedPrefixes + "]" + after
			if me := c.Me(); me != nil && me.Username != "" && me.Bot {
				Pattern += "(?: |$|@" + me.Username + ")(.*)"
			} else {
				Pattern += "(?: |$)(.*)"
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
	for _, f := range filters {
		if !f.check(m) {
			return false
		}
	}
	return true
}

func (e *messageEditHandle) runFilterChain(m *NewMessage, filters []Filter) bool {
	for _, f := range filters {
		if !f.check(m) {
			return false
		}
	}
	return true
}

func (h *callbackHandle) runFilterChain(c *CallbackQuery, filters []Filter) bool {
	for _, f := range filters {
		if !f.checkCallback(c) {
			return false
		}
	}
	return true
}

type Filter struct {
	flags        FilterFlag
	Users        []int64
	Chats        []int64
	Channels     []int64
	MinLength    int
	MaxLength    int
	MediaTypes   []string
	Func         func(m *NewMessage) bool
	FuncCallback func(c *CallbackQuery) bool
	orFilters    []Filter
}

type FilterFlag uint32

const (
	FPrivate FilterFlag = 1 << iota
	FGroup
	FChannel
	FMedia
	FCommand
	FReply
	FForward
	FFromBot
	FBlacklist
	FMention
	FOutgoing
	FIncoming
	FEdited
	FPhoto
	FVideo
	FDocument
	FAudio
	FSticker
	FAnimation
	FVoice
	FVideoNote
	FContact
	FLocation
	FVenue
	FPoll
	FText
)

func (f FilterFlag) Has(flag FilterFlag) bool { return f&flag != 0 }

func (f FilterFlag) check(m *NewMessage) bool {
	if f == 0 {
		return true
	}
	if f.Has(FPrivate) && !m.IsPrivate() {
		return false
	}
	if f.Has(FGroup) && !m.IsGroup() {
		return false
	}
	if f.Has(FChannel) && !m.IsChannel() {
		return false
	}
	if f.Has(FMedia) && !m.IsMedia() {
		return false
	}
	if f.Has(FCommand) && !m.IsCommand() {
		return false
	}
	if f.Has(FReply) && !m.IsReply() {
		return false
	}
	if f.Has(FForward) && !m.IsForward() {
		return false
	}
	if f.Has(FFromBot) && (m.Sender == nil || !m.Sender.Bot) {
		return false
	}
	if f.Has(FMention) && (m.Message == nil || !m.Message.Mentioned) {
		return false
	}
	if f.Has(FOutgoing) && (m.Message == nil || !m.Message.Out) {
		return false
	}
	if f.Has(FIncoming) && (m.Message == nil || m.Message.Out) {
		return false
	}
	if f.Has(FEdited) && (m.Message == nil || m.Message.EditDate == 0) {
		return false
	}
	if f.Has(FText) && m.Text() == "" {
		return false
	}
	if f.Has(FPhoto) && m.Photo() == nil {
		return false
	}
	if f.Has(FVideo) && m.Video() == nil {
		return false
	}
	if f.Has(FDocument) && m.Document() == nil {
		return false
	}
	if f.Has(FAudio) && m.Audio() == nil {
		return false
	}
	if f.Has(FSticker) && m.Sticker() == nil {
		return false
	}
	if f.Has(FVoice) && m.Voice() == nil {
		return false
	}
	if f.Has(FAnimation) {
		doc := m.Document()
		if doc == nil {
			return false
		}
		isAnim := false
		for _, attr := range doc.Attributes {
			if _, ok := attr.(*DocumentAttributeAnimated); ok {
				isAnim = true
				break
			}
		}
		if !isAnim {
			return false
		}
	}
	if f.Has(FVideoNote) {
		doc := m.Document()
		if doc == nil {
			return false
		}
		isVN := false
		for _, attr := range doc.Attributes {
			if v, ok := attr.(*DocumentAttributeVideo); ok && v.RoundMessage {
				isVN = true
				break
			}
		}
		if !isVN {
			return false
		}
	}
	if f.Has(FContact) && m.Contact() == nil {
		return false
	}
	if f.Has(FLocation) {
		if _, ok := m.Media().(*MessageMediaGeo); !ok {
			return false
		}
	}
	if f.Has(FVenue) {
		if _, ok := m.Media().(*MessageMediaVenue); !ok {
			return false
		}
	}
	if f.Has(FPoll) {
		if _, ok := m.Media().(*MessageMediaPoll); !ok {
			return false
		}
	}
	return true
}

var (
	IsPrivate  = Filter{flags: FPrivate}
	IsGroup    = Filter{flags: FGroup}
	IsChannel  = Filter{flags: FChannel}
	IsMedia    = Filter{flags: FMedia}
	IsCommand  = Filter{flags: FCommand}
	IsReply    = Filter{flags: FReply}
	IsForward  = Filter{flags: FForward}
	IsMention  = Filter{flags: FMention}
	IsOutgoing = Filter{flags: FOutgoing}
	IsIncoming = Filter{flags: FIncoming}
	IsEdited   = Filter{flags: FEdited}
	IsText     = Filter{flags: FText}
	IsPhoto    = Filter{flags: FPhoto}
	IsVideo    = Filter{flags: FVideo}
	IsAudio    = Filter{flags: FAudio}
	IsVoice    = Filter{flags: FVoice}
	IsBot      = Filter{flags: FFromBot}
)

func FromUser(ids ...int64) Filter { return Filter{Users: ids} }
func InChat(ids ...int64) Filter   { return Filter{Chats: ids} }
func TextMinLen(n int) Filter      { return Filter{MinLength: n} }
func TextMaxLen(n int) Filter      { return Filter{MaxLength: n} }

// Custom allows any func(m *NewMessage) bool as a filter
func Custom(fn func(*NewMessage) bool) Filter { return Filter{Func: fn} }

// CustomCallback allows any func(c *CallbackQuery) bool as a filter
func CustomCallback(fn func(*CallbackQuery) bool) Filter {
	return Filter{FuncCallback: fn}
}

func Not(f Filter) Filter {
	return Filter{Func: func(m *NewMessage) bool { return !f.check(m) }}
}

func Any(fs ...Filter) Filter {
	return Filter{Func: func(m *NewMessage) bool {
		for _, f := range fs {
			if f.check(m) {
				return true
			}
		}
		return false
	}}
}

func All(fs ...Filter) Filter {
	return Filter{Func: func(m *NewMessage) bool {
		for _, f := range fs {
			if !f.check(m) {
				return false
			}
		}
		return true
	}}
}

func (f Filter) check(m *NewMessage) bool {
	if len(f.orFilters) > 0 {
		for _, orFilter := range f.orFilters {
			if orFilter.check(m) {
				return true
			}
		}
		return false
	}

	if !f.flags.check(m) {
		return false
	}
	isBlacklist := f.flags.Has(FBlacklist)
	if len(f.Users) > 0 {
		found := false
		sid := m.SenderID()
		if slices.Contains(f.Users, sid) {
			found = true
		}
		if isBlacklist && found {
			return false
		}
		if !isBlacklist && !found {
			return false
		}
	}
	if len(f.Chats) > 0 {
		found := false
		cid := m.ChatID()
		if slices.Contains(f.Chats, cid) {
			found = true
		}
		if isBlacklist && found {
			return false
		}
		if !isBlacklist && !found {
			return false
		}
	}
	if len(f.Channels) > 0 {
		found := false
		cid := m.ChannelID()
		if slices.Contains(f.Channels, cid) {
			found = true
		}
		if isBlacklist && found {
			return false
		}
		if !isBlacklist && !found {
			return false
		}
	}
	if f.MinLength > 0 && len(m.Text()) < f.MinLength {
		return false
	}
	if f.MaxLength > 0 && len(m.Text()) > f.MaxLength {
		return false
	}
	if len(f.MediaTypes) > 0 {
		mt := strings.ToLower(m.MediaType())
		found := false
		for _, t := range f.MediaTypes {
			if strings.ToLower(t) == mt {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if f.Func != nil && !f.Func(m) {
		return false
	}
	return true
}

func (f Filter) checkCallback(c *CallbackQuery) bool {
	if f.flags.Has(FPrivate) && !c.IsPrivate() {
		return false
	}
	if f.flags.Has(FGroup) && !c.IsGroup() {
		return false
	}
	if f.flags.Has(FChannel) && !c.IsChannel() {
		return false
	}
	if f.flags.Has(FFromBot) && (c.Sender == nil || !c.Sender.Bot) {
		return false
	}
	if f.FuncCallback != nil && !f.FuncCallback(c) {
		return false
	}
	return true
}

func (f Filter) Or(other Filter) Filter {
	if len(f.orFilters) == 0 {
		f.orFilters = []Filter{f, other}
	} else {
		f.orFilters = append(f.orFilters, other)
	}
	return f
}

func F(flags FilterFlag) Filter                               { return Filter{flags: flags} }
func (f Filter) Flag(flag FilterFlag) Filter                  { f.flags |= flag; return f }
func (f Filter) FromUsers(ids ...int64) Filter                { f.Users = ids; return f }
func (f Filter) FromChats(ids ...int64) Filter                { f.Chats = ids; return f }
func (f Filter) FromChannels(ids ...int64) Filter             { f.Channels = ids; return f }
func (f Filter) MinLen(n int) Filter                          { f.MinLength = n; return f }
func (f Filter) MaxLen(n int) Filter                          { f.MaxLength = n; return f }
func (f Filter) WithMediaTypes(t ...string) Filter            { f.MediaTypes = t; return f }
func (f Filter) WithFunc(fn func(*NewMessage) bool) Filter    { f.Func = fn; return f }
func (f Filter) CustomCB(fn func(*CallbackQuery) bool) Filter { f.FuncCallback = fn; return f }
func (f Filter) Blacklist() Filter                            { f.flags |= FBlacklist; return f }

var (
	FilterPrivate   = IsPrivate
	FilterGroup     = IsGroup
	FilterChannel   = IsChannel
	FilterMedia     = IsMedia
	FilterCommand   = IsCommand
	FilterReply     = IsReply
	FilterForward   = IsForward
	FilterFromBot   = IsBot
	FilterMention   = IsMention
	FilterOutgoing  = IsOutgoing
	FilterIncoming  = IsIncoming
	FilterEdited    = IsEdited
	FilterPhoto     = IsPhoto
	FilterVideo     = IsVideo
	FilterDocument  = Filter{flags: FDocument}
	FilterAudio     = IsAudio
	FilterSticker   = Filter{flags: FSticker}
	FilterAnimation = Filter{flags: FAnimation}
	FilterVoice     = IsVoice
	FilterVideoNote = Filter{flags: FVideoNote}
	FilterContact   = Filter{flags: FContact}
	FilterLocation  = Filter{flags: FLocation}
	FilterVenue     = Filter{flags: FVenue}
	FilterPoll      = Filter{flags: FPoll}
	FilterText      = IsText
)

func addHandleToMap[T Handle](handleMap map[int][]T, handle T) T {
	group := handle.GetGroup()

	handlers := handleMap[group]
	inserted := false
	for i, h := range handlers {
		if handle.GetPriority() > h.GetPriority() {
			handleMap[group] = append(handlers[:i], append([]T{handle}, handlers[i:]...)...)
			inserted = true
			break
		}
	}

	if !inserted {
		handleMap[group] = append(handlers, handle)
	}

	return handleMap[group][len(handleMap[group])-1]
}

func makePriorityChangeCallback[T Handle](handleMap map[int][]T, handle T, mu *sync.RWMutex) func() {
	return func() {
		mu.Lock()
		defer mu.Unlock()
		group := handle.GetGroup()
		handlers := handleMap[group]

		for i := range handlers {
			if reflect.DeepEqual(handlers[i], handle) {
				handlers = append(handlers[:i], handlers[i+1:]...)
				handleMap[group] = handlers
				break
			}
		}

		handlers = handleMap[group]
		inserted := false
		for i, h := range handlers {
			if handle.GetPriority() > h.GetPriority() {
				handleMap[group] = append(handlers[:i], append([]T{handle}, handlers[i:]...)...)
				inserted = true
				break
			}
		}

		if !inserted {
			handleMap[group] = append(handlers, handle)
		}
	}
}

func makeGroupChangeCallback[T Handle](handleMap map[int][]T, handle T, mu *sync.RWMutex) func(int, int) {
	return func(oldGroup, newGroup int) {
		mu.Lock()
		defer mu.Unlock()
		if old, ok := handleMap[oldGroup]; ok {
			for i := range old {
				if reflect.DeepEqual(old[i], handle) {
					handleMap[oldGroup] = append(old[:i], old[i+1:]...)
					break
				}
			}
		}
		handleMap[newGroup] = append(handleMap[newGroup], handle)
	}
}

func (c *Client) AddMessageHandler(pattern any, handler MessageHandler, filters ...Filter) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}

	handle := &messageHandle{
		Pattern: pattern,
		Handler: handler,
		Filters: messageFilters,
		baseHandle: baseHandle{
			Group:   DefaultGroup,
			enabled: true,
			metrics: &HandlerMetrics{},
		},
	}

	handle.onGroupChanged = makeGroupChangeCallback(c.dispatcher.messageHandles, handle, &c.dispatcher.RWMutex)
	handle.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.messageHandles, handle, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.messageHandles, handle)
}

func (c *Client) AddCommandHandler(pattern string, handler MessageHandler, filters ...Filter) Handle {
	if !strings.HasPrefix(pattern, "cmd:") {
		pattern = "cmd:" + pattern
	}

	return c.AddMessageHandler(pattern, handler, filters...)
}

func (c *Client) AddDeleteHandler(pattern any, handler func(d *DeleteMessage) error) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	h := &messageDeleteHandle{
		Pattern:    pattern,
		Handler:    handler,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.messageDeleteHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.messageDeleteHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.messageDeleteHandles, h)
}

func (c *Client) AddAlbumHandler(handler func(m *Album) error) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	h := &albumHandle{
		Handler:    handler,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.albumHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.albumHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.albumHandles, h)
}

func (c *Client) AddActionHandler(handler MessageHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	h := &chatActionHandle{
		Handler:    handler,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.actionHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.actionHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.actionHandles, h)
}

func (c *Client) AddEditHandler(pattern any, handler MessageHandler, filters ...Filter) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}
	h := &messageEditHandle{
		Pattern:    pattern,
		Handler:    handler,
		Filters:    messageFilters,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.messageEditHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.messageEditHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.messageEditHandles, h)
}

func (c *Client) AddInlineHandler(pattern any, handler InlineHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	h := &inlineHandle{
		Pattern:    pattern,
		Handler:    handler,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.inlineHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.inlineHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.inlineHandles, h)
}

func (c *Client) AddInlineSendHandler(handler InlineSendHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	h := &inlineSendHandle{
		Handler:    handler,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.inlineSendHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.inlineSendHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.inlineSendHandles, h)
}

func (c *Client) AddCallbackHandler(pattern any, handler CallbackHandler, filters ...Filter) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}
	h := &callbackHandle{
		Pattern:    pattern,
		Handler:    handler,
		Filters:    messageFilters,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.callbackHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.callbackHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.callbackHandles, h)
}

func (c *Client) AddInlineCallbackHandler(pattern any, handler InlineCallbackHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	h := &inlineCallbackHandle{
		Pattern:    pattern,
		Handler:    handler,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.inlineCallbackHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.inlineCallbackHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.inlineCallbackHandles, h)
}

func (c *Client) AddJoinRequestHandler(handler PendingJoinHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	h := &joinRequestHandle{
		Handler:    handler,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.joinRequestHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.joinRequestHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.joinRequestHandles, h)
}

func (c *Client) AddParticipantHandler(handler ParticipantHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	h := &participantHandle{
		Handler:    handler,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.participantHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.participantHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.participantHandles, h)
}

func (c *Client) AddRawHandler(updateType Update, handler RawHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	h := &rawHandle{
		updateType: updateType,
		Handler:    handler,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.rawHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.rawHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.rawHandles, h)
}

func (c *Client) AddE2EHandler(handler func(update Update, c *Client) error) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	h := &e2eHandle{
		Handler:    handler,
		baseHandle: baseHandle{Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.e2eHandles, h, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.e2eHandles, h, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.e2eHandles, h)
}

// HandleIncomingUpdates processes incoming updates and dispatches them to the appropriate handlers.
func HandleIncomingUpdates(u any, c *Client) bool {
	// Update last update time for 15-minute timeout monitoring
	c.dispatcher.UpdateLastUpdateTime()
	c.dispatcher.nextUpdatesDeadline = time.Now().Add(time.Minute * 15)

UpdateTypeSwitching:
	switch upd := u.(type) {
	case *UpdatesObj:
		if !c.manageSeq(upd.Seq, upd.Seq) {
			return false
		}

		go c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
		for _, update := range upd.Updates {
			switch update := update.(type) {
			case *UpdateNewMessage:
				go c.handleMessageUpdate(update.Message)
				c.managePts(update.Pts, update.PtsCount)
			case *UpdateNewChannelMessage:
				channelID := getChannelIDFromMessage(update.Message)
				if channelID != 0 {
					go c.handleMessageUpdate(update.Message)
					c.manageChannelPts(channelID, update.Pts, update.PtsCount)
				} else {
					go c.handleMessageUpdate(update.Message)
					c.managePts(update.Pts, update.PtsCount)
				}
			case *UpdateNewScheduledMessage:
				go c.handleMessageUpdate(update.Message)
			case *UpdateEditMessage:
				go c.handleEditUpdate(update.Message)
				c.managePts(update.Pts, update.PtsCount)
			case *UpdateEditChannelMessage:
				channelID := getChannelIDFromMessage(update.Message)
				if channelID != 0 {
					go c.handleEditUpdate(update.Message)
					c.manageChannelPts(channelID, update.Pts, update.PtsCount)
				} else {
					go c.handleEditUpdate(update.Message)
					c.managePts(update.Pts, update.PtsCount)
				}
			case *UpdateDeleteMessages:
				go c.handleDeleteUpdate(update)
				c.managePts(update.Pts, update.PtsCount)
			case *UpdateDeleteChannelMessages:
				go c.handleDeleteUpdate(update)
				c.manageChannelPts(update.ChannelID, update.Pts, update.PtsCount)
			case *UpdateReadHistoryInbox:
				c.managePts(update.Pts, update.PtsCount)
			case *UpdateReadHistoryOutbox:
				c.managePts(update.Pts, update.PtsCount)
			case *UpdateWebPage:
				c.managePts(update.Pts, update.PtsCount)
			case *UpdateReadMessagesContents:
				c.managePts(update.Pts, update.PtsCount)
			case *UpdateReadChannelInbox:
				c.manageChannelPts(update.ChannelID, update.Pts, 0)
			case *UpdateChannelWebPage:
				c.manageChannelPts(update.ChannelID, update.Pts, update.PtsCount)
			case *UpdateFolderPeers:
				c.managePts(update.Pts, update.PtsCount)
			case *UpdatePinnedMessages:
				c.managePts(update.Pts, update.PtsCount)
			case *UpdatePinnedChannelMessages:
				c.manageChannelPts(update.ChannelID, update.Pts, update.PtsCount)
			case *UpdateBotInlineQuery:
				go c.handleInlineUpdate(update)
			case *UpdateBotCallbackQuery:
				go c.handleCallbackUpdate(update)
			case *UpdateInlineBotCallbackQuery:
				go c.handleInlineCallbackUpdate(update)
			case *UpdateChannelParticipant:
				go c.handleParticipantUpdate(update)
			case *UpdatePendingJoinRequests, *UpdateBotChatInviteRequester:
				go c.handleJoinRequestUpdate(update)
			case *UpdateBotInlineSend:
				go c.handleInlineSendUpdate(update)
			case *UpdateChannelTooLong:
				currentPts := c.dispatcher.GetChannelPts(update.ChannelID)
				if update.Pts != 0 {
					currentPts = update.Pts
				}
				go c.FetchChannelDifference(update.ChannelID, currentPts, 50)
			case *UpdateEncryption, *UpdateNewEncryptedMessage:
				go c.HandleSecretChatUpdate(update)
			}
			go c.handleRawUpdate(update)
		}
	case *UpdateShort:
		switch upd := upd.Update.(type) {
		case *UpdateNewMessage:
			go c.fetchPeersBeforeUpdate(upd.Message, upd.Pts)
		case *UpdateNewChannelMessage:
			channelID := getChannelIDFromMessage(upd.Message)
			if channelID != 0 {
				go c.handleMessageUpdate(upd.Message)
				c.manageChannelPts(channelID, upd.Pts, upd.PtsCount)
			} else {
				go c.fetchPeersBeforeUpdate(upd.Message, upd.Pts)
			}
		case *UpdateChannelTooLong:
			currentPts := c.dispatcher.GetChannelPts(upd.ChannelID)
			if upd.Pts != 0 {
				currentPts = upd.Pts
			}
			go c.FetchChannelDifference(upd.ChannelID, currentPts, 50)
		}
		go c.handleRawUpdate(upd.Update)
	case *UpdateShortMessage:
		update := &MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.UserID), PeerID: getPeerUser(upd.UserID), Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}
		go c.fetchPeersBeforeUpdate(update, upd.Pts)
		go c.handleRawUpdate(&UpdateNewMessage{Message: update, Pts: upd.Pts, PtsCount: 0})
	case *UpdateShortChatMessage:
		update := &MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.FromID), PeerID: &PeerChat{ChatID: upd.ChatID}, Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}
		go c.fetchPeersBeforeUpdate(update, upd.Pts)
		go c.handleRawUpdate(&UpdateNewMessage{Message: update, Pts: upd.Pts, PtsCount: 0})
	case *UpdateShortSentMessage:
		update := &MessageObj{ID: upd.ID, Out: upd.Out, Date: upd.Date, Media: upd.Media, Entities: upd.Entities, TtlPeriod: upd.TtlPeriod}
		go c.fetchPeersBeforeUpdate(update, upd.Pts)
		go c.handleRawUpdate(&UpdateNewMessage{Message: update, Pts: upd.Pts, PtsCount: 0})
	case *UpdatesCombined:
		if !c.manageSeq(upd.Seq, upd.SeqStart) {
			return false
		}

		u = upd.Updates
		go c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
		goto UpdateTypeSwitching
	case *UpdateChannelTooLong:
		currentPts := c.dispatcher.GetChannelPts(upd.ChannelID)
		if upd.Pts != 0 {
			currentPts = upd.Pts
		}
		go c.FetchChannelDifference(upd.ChannelID, currentPts, 50)
	case *UpdatesTooLong:
		go c.FetchDifference(c.dispatcher.GetPts(), 5000)
	default:
		c.Log.Debug("unhandled update type: %T", upd)
	}
	return true
}

func getChannelIDFromMessage(msg Message) int64 {
	if m, ok := msg.(*MessageObj); ok {
		if peer, ok := m.PeerID.(*PeerChannel); ok {
			return peer.ChannelID
		}
	}
	return 0
}

func (c *Client) FetchDifference(fromPts int32, limit int32) {
	c.dispatcher.Lock()
	if c.dispatcher.recoveringDifference {
		c.dispatcher.Unlock()
		return
	}
	c.dispatcher.recoveringDifference = true
	c.dispatcher.Unlock()

	defer func() {
		c.dispatcher.Lock()
		c.dispatcher.recoveringDifference = false
		c.dispatcher.Unlock()
	}()

	if limit == 0 {
		limit = 5000
	}
	if limit > 10000 {
		limit = 10000
	}

	totalFetched := 0

	req := &UpdatesGetDifferenceParams{
		Pts:           fromPts,
		PtsLimit:      limit,
		PtsTotalLimit: limit,
		Date:          c.dispatcher.GetDate(),
		Qts:           c.dispatcher.GetQts(),
		QtsLimit:      limit,
	}

	if req.Date == 0 {
		req.Date = int32(time.Now().Unix())
	}

	maxIterations := 10
	iteration := 0

	for iteration < maxIterations {
		iteration++
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		updates, err := c.MTProto.MakeRequestCtx(ctx, req)
		cancel()

		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				continue
			}
			return
		}

		switch u := updates.(type) {
		case *UpdatesDifferenceEmpty:
			c.dispatcher.SetDate(u.Date)
			c.dispatcher.SetSeq(u.Seq)
			return

		case *UpdatesDifferenceObj:
			c.Cache.UpdatePeersToCache(u.Users, u.Chats)

			for _, message := range u.NewMessages {
				if msg, ok := message.(*MessageObj); ok {
					go c.handleMessageUpdate(msg)
					totalFetched++
				}
			}

			if len(u.OtherUpdates) > 0 {
				totalFetched += len(u.OtherUpdates)
				HandleIncomingUpdates(&UpdatesObj{Updates: u.OtherUpdates, Users: u.Users, Chats: u.Chats}, c)
			}

			c.dispatcher.SetPts(u.State.Pts)
			c.dispatcher.SetQts(u.State.Qts)
			c.dispatcher.SetSeq(u.State.Seq)
			c.dispatcher.SetDate(u.State.Date)
			return

		case *UpdatesDifferenceSlice:
			c.Cache.UpdatePeersToCache(u.Users, u.Chats)

			for _, message := range u.NewMessages {
				if msg, ok := message.(*MessageObj); ok {
					go c.handleMessageUpdate(msg)
					totalFetched++
				}
			}

			if len(u.OtherUpdates) > 0 {
				totalFetched += len(u.OtherUpdates)
				HandleIncomingUpdates(&UpdatesObj{Updates: u.OtherUpdates, Users: u.Users, Chats: u.Chats}, c)
			}

			c.dispatcher.SetPts(u.IntermediateState.Pts)
			c.dispatcher.SetQts(u.IntermediateState.Qts)
			c.dispatcher.SetSeq(u.IntermediateState.Seq)
			c.dispatcher.SetDate(u.IntermediateState.Date)

			req.Pts = u.IntermediateState.Pts
			req.Qts = u.IntermediateState.Qts
			req.Date = u.IntermediateState.Date

		case *UpdatesDifferenceTooLong:
			c.Log.Debug("difference too long, refetching state (pts=%d, limit=%d, fetched=%d)", u.Pts, limit, totalFetched)
			c.dispatcher.SetPts(u.Pts)

			state, err := c.UpdatesGetState()
			if err != nil {
				c.Log.Error("failed to get update state: %v", err)
				return
			}

			c.dispatcher.SetPts(state.Pts)
			c.dispatcher.SetQts(state.Qts)
			c.dispatcher.SetSeq(state.Seq)
			c.dispatcher.SetDate(state.Date)
			return

		default:
			c.Log.Debug("unhandled difference type: %v", reflect.TypeOf(updates))
			return
		}
	}

	c.Log.Debug("difference fetch limit reached (iterations=%d, pts=%d, fetched=%d)", maxIterations, req.Pts, totalFetched)
}

func (c *Client) managePts(pts int32, ptsCount int32) bool {
	var currentPts = c.dispatcher.GetPts()

	if currentPts == 0 {
		c.dispatcher.SetPts(pts)
		return true
	}

	expectedPts := currentPts + ptsCount

	if expectedPts == pts {
		c.dispatcher.SetPts(pts)
		return true
	}

	if expectedPts > pts {
		return false
	}

	if expectedPts < pts {
		gap := pts - expectedPts

		if gap <= 5 {
			c.dispatcher.SetPts(pts)
			return true
		} else if gap > 2000 {
			c.dispatcher.SetPts(pts)
			return true
		}

		c.dispatcher.SetPts(pts)

		gapPts := expectedPts
		go func() {
			time.Sleep(500 * time.Millisecond)

			newCurrentPts := c.dispatcher.GetPts()
			if newCurrentPts >= pts {
				return
			}

			c.FetchDifference(gapPts, gap+10)
		}()

		return true
	}

	return true
}

func (c *Client) manageSeq(seq int32, seqStart int32) bool {
	if seq == 0 && seqStart == 0 {
		return true
	}

	currentSeq := c.dispatcher.GetSeq()

	if currentSeq == 0 {
		c.dispatcher.SetSeq(seq)
		return true
	}

	expectedSeqStart := currentSeq + 1

	if expectedSeqStart == seqStart {
		c.dispatcher.SetSeq(seq)
		return true
	}

	if expectedSeqStart > seqStart {
		return false
	}

	if expectedSeqStart < seqStart {
		go c.FetchDifference(c.dispatcher.GetPts(), 5000)
		return false
	}

	return true
}

func (c *Client) manageChannelPts(channelID int64, pts int32, ptsCount int32) bool {
	var currentPts = c.dispatcher.GetChannelPts(channelID)

	if currentPts == 0 {
		c.dispatcher.SetChannelPts(channelID, pts)
		return true
	}

	expectedPts := currentPts + ptsCount

	if expectedPts == pts {
		c.dispatcher.SetChannelPts(channelID, pts)
		return true
	}

	if expectedPts > pts {
		return false
	}

	if expectedPts < pts {
		gap := pts - expectedPts

		if gap <= 5 {
			c.dispatcher.SetChannelPts(channelID, pts)
			return true
		} else if gap > 1000 {
			c.dispatcher.SetChannelPts(channelID, pts)
			return true
		}

		c.dispatcher.SetChannelPts(channelID, pts)

		gapPts := expectedPts
		go func() {
			time.Sleep(500 * time.Millisecond)

			newCurrentPts := c.dispatcher.GetChannelPts(channelID)
			if newCurrentPts >= pts {
				return
			}

			c.FetchChannelDifference(channelID, gapPts, 100)
		}()

		return true
	}

	return true
}

func (c *Client) GetDifference(Pts, Limit int32) (Message, error) {
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

// FetchChannelDifference fetches updates difference for a specific channel
// Use limit 10-100 as recommended for channels
func (c *Client) FetchChannelDifference(channelID int64, fromPts int32, limit int32) {
	c.dispatcher.Lock()
	if c.dispatcher.recoveringChannels == nil {
		c.dispatcher.recoveringChannels = make(map[int64]bool)
	}
	if c.dispatcher.recoveringChannels[channelID] {
		c.dispatcher.Unlock()
		return
	}
	c.dispatcher.recoveringChannels[channelID] = true
	c.dispatcher.Unlock()

	defer func() {
		c.dispatcher.Lock()
		delete(c.dispatcher.recoveringChannels, channelID)
		c.dispatcher.Unlock()
	}()

	if limit == 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	c.dispatcher.RLock()
	channelState, hasState := c.dispatcher.channelStates[channelID]
	c.dispatcher.RUnlock()

	var accessHash int64
	if hasState {
		accessHash = channelState.accessHash
	}

	if accessHash == 0 {
		channel := c.getChannel(&PeerChannel{ChannelID: channelID})
		if channel != nil {
			accessHash = channel.AccessHash
		} else {
			c.Log.Error("channel difference failed: no access hash (channel=%d)", channelID)
			return
		}
	}

	totalFetched := 0
	maxIterations := 20
	iteration := 0

	req := &UpdatesGetChannelDifferenceParams{
		Force:   false,
		Channel: &InputChannelObj{ChannelID: channelID, AccessHash: accessHash},
		Filter:  &ChannelMessagesFilterEmpty{},
		Pts:     fromPts,
		Limit:   limit,
	}

	for iteration < maxIterations {
		iteration++
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		diff, err := c.MTProto.MakeRequestCtx(ctx, req)
		cancel()

		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				continue
			}
			return
		}

		switch d := diff.(type) {
		case *UpdatesChannelDifferenceEmpty:
			c.dispatcher.SetChannelPts(channelID, d.Pts)
			return

		case *UpdatesChannelDifferenceObj:
			c.Cache.UpdatePeersToCache(d.Users, d.Chats)

			for _, message := range d.NewMessages {
				if msg, ok := message.(*MessageObj); ok {
					go c.handleMessageUpdate(msg)
					totalFetched++
				}
			}

			if len(d.OtherUpdates) > 0 {
				totalFetched += len(d.OtherUpdates)
				HandleIncomingUpdates(&UpdatesObj{Updates: d.OtherUpdates, Users: d.Users, Chats: d.Chats}, c)
			}

			c.dispatcher.SetChannelPts(channelID, d.Pts)

			if d.Final {
				return
			}

			c.dispatcher.RLock()
			isOpen := channelState != nil && channelState.isOpen
			c.dispatcher.RUnlock()

			if !isOpen {
				return
			}

			req.Pts = d.Pts

		case *UpdatesChannelDifferenceTooLong:
			c.Cache.UpdatePeersToCache(d.Users, d.Chats)
			for _, message := range d.Messages {
				if msg, ok := message.(*MessageObj); ok {
					go c.handleMessageUpdate(msg)
					totalFetched++
				}
			}

			if dialogChannel, ok := d.Dialog.(*DialogObj); ok {
				c.dispatcher.SetChannelPts(channelID, dialogChannel.Pts)
				c.Log.Debug("channel difference too long, refreshing state (channel=%d, pts=%d)", channelID, dialogChannel.Pts)

				if !d.Final {
					c.dispatcher.RLock()
					isOpen := channelState != nil && channelState.isOpen
					c.dispatcher.RUnlock()

					if isOpen {
						req.Pts = dialogChannel.Pts
						continue
					}
				}
			}

			return

		default:
			c.Log.Debug("unhandled channel difference type: %v (channel=%d)", reflect.TypeOf(diff), channelID)
			return
		}
	}

	c.Log.Debug("channel difference fetch limit reached (channel=%d, iterations=%d, pts=%d, fetched=%d)", channelID, maxIterations, req.Pts, totalFetched)
}

// OpenChat starts active polling for a channel to receive updates faster.
// timeoutSeconds specifies the polling interval in seconds.
func (c *Client) OpenChat(channel *InputChannelObj, timeoutSeconds int32) {
	c.dispatcher.Lock()
	if c.dispatcher.openChats == nil {
		c.dispatcher.openChats = make(map[int64]*openChat)
	}
	if _, ok := c.dispatcher.openChats[channel.ChannelID]; ok {
		c.dispatcher.Unlock()
		return
	}
	c.dispatcher.Unlock()

	currentPts := c.dispatcher.GetChannelPts(channel.ChannelID)
	if currentPts == 0 {
		diff, err := c.UpdatesGetChannelDifference(&UpdatesGetChannelDifferenceParams{
			Channel: channel,
			Filter:  &ChannelMessagesFilterEmpty{},
			Pts:     1,
			Limit:   1,
		})
		if err != nil {
			c.Log.Error("failed to get channel pts (channel=%d): %v", channel.ChannelID, err)
			return
		}
		switch d := diff.(type) {
		case *UpdatesChannelDifferenceEmpty:
			currentPts = d.Pts
		case *UpdatesChannelDifferenceObj:
			currentPts = d.Pts
		case *UpdatesChannelDifferenceTooLong:
			if dialog, ok := d.Dialog.(*DialogObj); ok {
				currentPts = dialog.Pts
			}
		}
		if currentPts == 0 {
			currentPts = 1
		}
	}

	chat := &openChat{
		accessHash: channel.AccessHash,
		closeChan:  make(chan struct{}),
		lastPts:    currentPts,
		timeout:    timeoutSeconds,
	}

	c.dispatcher.Lock()
	if _, ok := c.dispatcher.openChats[channel.ChannelID]; ok {
		c.dispatcher.Unlock()
		return
	}
	c.dispatcher.openChats[channel.ChannelID] = chat
	// Mark channel as open in channelState for FetchChannelDifference checks
	if c.dispatcher.channelStates == nil {
		c.dispatcher.channelStates = make(map[int64]*channelState)
	}
	if state, ok := c.dispatcher.channelStates[channel.ChannelID]; ok {
		state.isOpen = true
	} else {
		c.dispatcher.channelStates[channel.ChannelID] = &channelState{
			pts:        currentPts,
			accessHash: channel.AccessHash,
			isOpen:     true,
		}
	}
	c.dispatcher.Unlock()

	go c.pollOpenChat(channel.ChannelID, chat)
}

// pollOpenChat periodically fetches channel difference for an open chat
func (c *Client) pollOpenChat(channelID int64, chat *openChat) {
	var errorCount int
	const maxBackoff = 60 // max 60 seconds between retries on error

	for {
		chat.RLock()
		timeout := time.Duration(chat.timeout) * time.Second
		lastPts := chat.lastPts
		chat.RUnlock()

		if timeout < time.Second {
			timeout = 15 * time.Second
		}

		// Add exponential backoff on consecutive errors
		if errorCount > 0 {
			backoff := min(1<<errorCount, maxBackoff)
			timeout = time.Duration(backoff) * time.Second
		}

		select {
		case <-chat.closeChan:
			return
		case <-time.After(timeout):
		}

		diff, err := c.UpdatesGetChannelDifference(&UpdatesGetChannelDifferenceParams{
			Channel: &InputChannelObj{ChannelID: channelID, AccessHash: chat.accessHash},
			Filter:  &ChannelMessagesFilterEmpty{},
			Pts:     lastPts,
			Limit:   100,
		})
		if err != nil {
			errorCount++
			c.Log.Debug("channel poll error (channel=%d, attempt=%d): %v", channelID, errorCount, err)
			continue
		}
		errorCount = 0

		switch d := diff.(type) {
		case *UpdatesChannelDifferenceEmpty:
			chat.Lock()
			chat.timeout = d.Timeout
			chat.Unlock()

		case *UpdatesChannelDifferenceObj:
			c.Cache.UpdatePeersToCache(d.Users, d.Chats)
			for _, msg := range d.NewMessages {
				if msgObj, ok := msg.(*MessageObj); ok {
					go c.handleMessageUpdate(msgObj)
				}
			}
			if len(d.OtherUpdates) > 0 {
				HandleIncomingUpdates(&UpdatesObj{Updates: d.OtherUpdates, Users: d.Users, Chats: d.Chats}, c)
			}
			chat.Lock()
			chat.lastPts = d.Pts
			chat.timeout = d.Timeout
			chat.Unlock()
			c.dispatcher.SetChannelPts(channelID, d.Pts)

		case *UpdatesChannelDifferenceTooLong:
			c.Cache.UpdatePeersToCache(d.Users, d.Chats)
			for _, msg := range d.Messages {
				if msgObj, ok := msg.(*MessageObj); ok {
					go c.handleMessageUpdate(msgObj)
				}
			}
			chat.Lock()
			chat.timeout = d.Timeout
			if dialog, ok := d.Dialog.(*DialogObj); ok {
				chat.lastPts = dialog.Pts
				c.dispatcher.SetChannelPts(channelID, dialog.Pts)
			}
			chat.Unlock()
		}
	}
}

// CloseChat stops active polling for a channel when user leaves it.
func (c *Client) CloseChat(channel *InputChannelObj) {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()

	if c.dispatcher.openChats == nil {
		return
	}
	chat, ok := c.dispatcher.openChats[channel.ChannelID]
	if !ok {
		return
	}
	close(chat.closeChan)
	delete(c.dispatcher.openChats, channel.ChannelID)
	// Mark channel as closed
	if state, ok := c.dispatcher.channelStates[channel.ChannelID]; ok {
		state.isOpen = false
	}
}

func (c *Client) monitorNoUpdatesTimeout() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if time.Since(c.dispatcher.getLastUpdateTime()) > 15*time.Minute {
				c.Log.Debug("no updates for 15 minutes, fetching difference")
				c.FetchDifference(c.dispatcher.GetPts(), 5000)
			}
		case <-c.dispatcher.stopChan:
			return
		}
	}
}

// ExportPts exports the current pts value from the dispatcher.
func (c *Client) ExportPts() int32 {
	if c.dispatcher == nil {
		return 0
	}
	return c.dispatcher.GetPts()
}

// FetchDifferenceOnStartup fetches any missed updates since last disconnect.
// Should be called on startup after logging in to catch up on missed events.
func (c *Client) FetchDifferenceOnStartup(pts int32) {
	c.Log.Debug("fetching missed updates (pts=%d)", pts)
	c.FetchDifference(pts, 5000)
}

type ev any

var (
	OnMessage        ev = "message"
	OnCommand        ev = "command"
	OnCommandShort   ev = "cmd"
	OnAction         ev = "action"
	OnEdit           ev = "edit"
	OnDelete         ev = "delete"
	OnAlbum          ev = "album"
	OnInline         ev = "inline"
	OnCallback       ev = "callback"
	OnInlineCallback ev = "inlinecallback"
	OnChosenInline   ev = "choseninline"
	OnParticipant    ev = "participant"
	OnJoinRequest    ev = "joinrequest"
	OnRaw            ev = "raw"
)

type eventInfo struct {
	eventType string
	pattern   string
}

func parsePattern(pattern any) eventInfo {
	switch p := pattern.(type) {
	case string:
		p = strings.TrimSpace(p)

		if len(p) > 0 && (p[0] == '/' || p[0] == '!') {
			return eventInfo{eventType: "command", pattern: p[1:]}
		}
		if idx := strings.Index(p, ":"); idx > 0 {
			return eventInfo{
				eventType: strings.ToLower(strings.TrimSpace(p[:idx])),
				pattern:   strings.TrimSpace(p[idx+1:]),
			}
		}

		return eventInfo{eventType: strings.ToLower(p)}

	case ev:
		if s, ok := p.(string); ok {
			return eventInfo{eventType: s}
		}
		return eventInfo{}

	default:
		return eventInfo{}
	}
}

var handlerTypes = map[string]string{
	"func(*telegram.NewMessage) error":              "message",
	"func(*telegram.DeleteMessage) error":           "delete",
	"func(*telegram.Album) error":                   "album",
	"func(*telegram.InlineQuery) error":             "inline",
	"func(*telegram.InlineSend) error":              "choseninline",
	"func(*telegram.CallbackQuery) error":           "callback",
	"func(*telegram.InlineCallbackQuery) error":     "inlinecallback",
	"func(*telegram.ParticipantUpdate) error":       "participant",
	"func(*telegram.JoinRequestUpdate) error":       "joinrequest",
	"func(telegram.Update, *telegram.Client) error": "raw",
}

// On registers an event handler with flexible pattern matching.
//
// Usage patterns:
//
//	// Message handlers
//	client.On("message", handler)              // All messages
//	client.On("message:hello", handler)        // Messages matching "hello"
//	client.On(OnMessage, handler)              // Using constant
//
//	// Command handlers (multiple formats)
//	client.On("/start", handler)               // Shortcut for cmd:start
//	client.On("!help", handler)                // Shortcut for cmd:help
//	client.On("cmd:start", handler)            // Explicit command
//	client.On("command:start", handler)        // Full form
//
//	// Other events
//	client.On("callback:data", handler)        // Callback with data pattern
//	client.On("inline:query", handler)         // Inline query pattern
//	client.On("edit", handler)                 // Message edits
//	client.On("delete", handler)               // Message deletions
//	client.On("album", handler)                // Media albums
//	client.On("participant", handler)          // Member updates
//	client.On("joinrequest", handler)          // Join requests
//
//	// Raw updates
//	client.On("raw", handler)                  // All raw updates
//	client.On("*", handler)                    // Alias for raw
//	client.On(&UpdateNewMessage{}, handler)   // Specific update type
//
//	// Auto-detect from handler signature
//	client.On(func(m *NewMessage) error {...}) // Detects as message handler
func (c *Client) On(args ...any) Handle {
	if len(args) == 0 {
		c.Log.Error("On: missing event type argument")
		return nil
	}

	var pattern any
	var handler any
	var filters []Filter

	// Parse arguments based on count and types
	switch len(args) {
	case 1:
		// Single arg must be a handler - auto-detect event type
		handler = args[0]
	case 2:
		// pattern + handler, or handler + filter
		if _, ok := args[1].(Filter); ok {
			handler = args[0]
			filters = append(filters, args[1].(Filter))
		} else {
			pattern = args[0]
			handler = args[1]
		}
	default:
		// pattern + handler + filters...
		pattern = args[0]
		handler = args[1]
		for _, f := range args[2:] {
			if filter, ok := f.(Filter); ok {
				filters = append(filters, filter)
			}
		}
	}

	info := parsePattern(pattern)
	if info.eventType == "" && handler != nil {
		handlerType := fmt.Sprintf("%T", handler)
		if detected, ok := handlerTypes[handlerType]; ok {
			info.eventType = detected
		}
	}

	switch info.eventType {
	case "message", "newmessage", "msg":
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if info.pattern != "" {
				return c.AddMessageHandler(info.pattern, h, filters...)
			}
			return c.AddMessageHandler(OnNewMessage, h, filters...)
		}
		c.Log.Error("On(%s): invalid handler type %T, expected func(*NewMessage) error", info.eventType, handler)

	case "command", "cmd":
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if info.pattern != "" {
				return c.AddMessageHandler("cmd:"+info.pattern, h, filters...)
			}
			c.Log.Error("On(command): pattern required, use 'cmd:name' or '/name'")
			return nil
		}
		c.Log.Error("On(%s): invalid handler type %T, expected func(*NewMessage) error", info.eventType, handler)

	case "action":
		if h, ok := handler.(func(m *NewMessage) error); ok {
			return c.AddActionHandler(h)
		}
		c.Log.Error("On(action): invalid handler type %T, expected func(*NewMessage) error", handler)

	case "edit", "editmessage":
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if info.pattern != "" {
				return c.AddEditHandler(info.pattern, h, filters...)
			}
			return c.AddEditHandler(OnEditMessage, h, filters...)
		}
		c.Log.Error("On(edit): invalid handler type %T, expected func(*NewMessage) error", handler)

	case "delete", "deletemessage":
		if h, ok := handler.(func(m *DeleteMessage) error); ok {
			if info.pattern != "" {
				return c.AddDeleteHandler(info.pattern, h)
			}
			return c.AddDeleteHandler(OnDeleteMessage, h)
		}
		c.Log.Error("On(delete): invalid handler type %T, expected func(*DeleteMessage) error", handler)

	case "album":
		if h, ok := handler.(func(m *Album) error); ok {
			return c.AddAlbumHandler(h)
		}
		c.Log.Error("On(album): invalid handler type %T, expected func(*Album) error", handler)

	case "inline", "inlinequery":
		if h, ok := handler.(func(m *InlineQuery) error); ok {
			if info.pattern != "" {
				return c.AddInlineHandler(info.pattern, h)
			}
			return c.AddInlineHandler(OnInlineQuery, h)
		}
		c.Log.Error("On(inline): invalid handler type %T, expected func(*InlineQuery) error", handler)

	case "choseninline", "inlinesend":
		if h, ok := handler.(func(m *InlineSend) error); ok {
			return c.AddInlineSendHandler(h)
		}
		c.Log.Error("On(choseninline): invalid handler type %T, expected func(*InlineSend) error", handler)

	case "callback", "callbackquery":
		if h, ok := handler.(func(m *CallbackQuery) error); ok {
			if info.pattern != "" {
				return c.AddCallbackHandler(info.pattern, h, filters...)
			}
			return c.AddCallbackHandler(OnCallbackQuery, h, filters...)
		}
		c.Log.Error("On(callback): invalid handler type %T, expected func(*CallbackQuery) error", handler)

	case "inlinecallback", "inlinecallbackquery":
		if h, ok := handler.(func(m *InlineCallbackQuery) error); ok {
			if info.pattern != "" {
				return c.AddInlineCallbackHandler(info.pattern, h)
			}
			return c.AddInlineCallbackHandler(OnInlineCallbackQuery, h)
		}
		c.Log.Error("On(inlinecallback): invalid handler type %T, expected func(*InlineCallbackQuery) error", handler)

	case "participant":
		if h, ok := handler.(func(m *ParticipantUpdate) error); ok {
			return c.AddParticipantHandler(h)
		}
		c.Log.Error("On(participant): invalid handler type %T, expected func(*ParticipantUpdate) error", handler)

	case "joinrequest":
		if h, ok := handler.(func(m *JoinRequestUpdate) error); ok {
			return c.AddJoinRequestHandler(h)
		}
		c.Log.Error("On(joinrequest): invalid handler type %T, expected func(*JoinRequestUpdate) error", handler)

	case "raw", "*":
		if h, ok := handler.(func(m Update, c *Client) error); ok {
			return c.AddRawHandler(nil, h)
		}
		c.Log.Error("On(raw): invalid handler type %T, expected func(Update, *Client) error", handler)

	default:
		if update, ok := pattern.(Update); ok {
			if h, ok := handler.(func(m Update, c *Client) error); ok {
				return c.AddRawHandler(update, h)
			}
			c.Log.Error("On(Update): invalid handler type %T, expected func(Update, *Client) error", handler)
			return nil
		}

		switch h := handler.(type) {
		case func(m *NewMessage) error:
			return c.AddMessageHandler(OnNewMessage, h, filters...)
		case func(m *DeleteMessage) error:
			return c.AddDeleteHandler(OnDeleteMessage, h)
		case func(m *Album) error:
			return c.AddAlbumHandler(h)
		case func(m *InlineQuery) error:
			return c.AddInlineHandler(OnInlineQuery, h)
		case func(m *InlineSend) error:
			return c.AddInlineSendHandler(h)
		case func(m *CallbackQuery) error:
			return c.AddCallbackHandler(OnCallbackQuery, h, filters...)
		case func(m *InlineCallbackQuery) error:
			return c.AddInlineCallbackHandler(OnInlineCallbackQuery, h)
		case func(m *ParticipantUpdate) error:
			return c.AddParticipantHandler(h)
		case func(m *JoinRequestUpdate) error:
			return c.AddJoinRequestHandler(h)
		case func(m Update, c *Client) error:
			return c.AddRawHandler(nil, h)
		default:
			c.Log.Error("On: unknown pattern %q or handler type %T", pattern, handler)
		}
	}

	return nil
}

func (c *Client) OnMessage(pattern string, handler func(m *NewMessage) error, filters ...Filter) Handle {
	if pattern == "" {
		return c.AddMessageHandler(OnNewMessage, handler, filters...)
	}
	return c.AddMessageHandler(pattern, handler, filters...)
}

func (c *Client) OnCommand(command string, handler func(m *NewMessage) error, filters ...Filter) Handle {
	return c.AddMessageHandler("cmd:"+command, handler, filters...)
}

func (c *Client) OnCallback(pattern string, handler func(m *CallbackQuery) error) Handle {
	if pattern == "" {
		return c.AddCallbackHandler(OnCallbackQuery, handler)
	}
	return c.AddCallbackHandler(pattern, handler)
}

func (c *Client) OnInlineQuery(pattern string, handler func(m *InlineQuery) error) Handle {
	if pattern == "" {
		return c.AddInlineHandler(OnInlineQuery, handler)
	}
	return c.AddInlineHandler(pattern, handler)
}

func (c *Client) OnEdit(pattern string, handler func(m *NewMessage) error) Handle {
	if pattern == "" {
		return c.AddEditHandler(OnEditMessage, handler)
	}
	return c.AddEditHandler(pattern, handler)
}

func (c *Client) OnDelete(handler func(m *DeleteMessage) error) Handle {
	return c.AddDeleteHandler(OnDeleteMessage, handler)
}

func (c *Client) OnAlbum(handler func(m *Album) error) Handle {
	return c.AddAlbumHandler(handler)
}

// SetLifecycleHooks sets the lifecycle hooks for all handlers
func (c *Client) SetLifecycleHooks(hooks *LifecycleHooks) {
	if c.dispatcher != nil {
		c.dispatcher.lifecycleHooks = hooks
	}
}

// GetHandlerMetrics returns metrics for a specific handler
func (c *Client) GetHandlerMetrics(handle Handle) *HandlerMetrics {
	if bh, ok := handle.(*messageHandle); ok {
		return bh.metrics
	}
	return nil
}

// AddMessageHandlerWithMiddleware adds a message handler with middleware support
func (c *Client) AddMessageHandlerWithMiddleware(pattern any, handler MessageHandler, middlewares []Middleware, filters ...Filter) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()

	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}

	handle := &messageHandle{
		Pattern:     pattern,
		Handler:     handler,
		Filters:     messageFilters,
		middlewares: middlewares,
		baseHandle: baseHandle{
			Group:   DefaultGroup,
			enabled: true,
			metrics: &HandlerMetrics{},
		},
	}

	handle.onGroupChanged = makeGroupChangeCallback(c.dispatcher.messageHandles, handle, &c.dispatcher.RWMutex)
	handle.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.messageHandles, handle, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.messageHandles, handle)
}

func (c *Client) OnParticipant(handler func(m *ParticipantUpdate) error) Handle {
	return c.AddParticipantHandler(handler)
}

func (c *Client) OnJoinRequest(handler func(m *JoinRequestUpdate) error) Handle {
	return c.AddJoinRequestHandler(handler)
}

func (c *Client) OnRaw(updateType Update, handler func(m Update, c *Client) error) Handle {
	return c.AddRawHandler(updateType, handler)
}

func (c *Client) OnE2EMessage(handler func(update Update, c *Client) error) Handle {
	return c.AddE2EHandler(handler)
}
