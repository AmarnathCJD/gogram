// Copyright (c) 2025, amarnathcjd

package telegram

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type EventType string

const (
	EventMessage        EventType = "message"
	EventNewMessage     EventType = "newmessage"
	EventCommand        EventType = "command"
	EventCommandShort   EventType = "cmd"
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

	OnMessage        = EventMessage
	OnCommand        = EventCommand
	OnCommandShort   = EventCommandShort
	OnAction         = EventAction
	OnEdit           = EventEdit
	OnDelete         = EventDelete
	OnAlbum          = EventAlbum
	OnInline         = EventInline
	OnCallback       = EventCallback
	OnInlineCallback = EventInlineCallback
	OnChosenInline   = EventChosenInline
	OnParticipant    = EventParticipant
	OnJoinRequest    = EventJoinRequest
	OnRaw            = EventRaw

	OnNewMessage          = EventNewMessage
	OnEditMessage         = EventEditMessage
	OnDeleteMessage       = EventDeleteMessage
	OnInlineQuery         = EventInlineQuery
	OnCallbackQuery       = EventCallbackQuery
	OnInlineCallbackQuery = EventInlineCallback
)

// Middleware wraps a handler to add cross-cutting concerns
type Middleware func(MessageHandler) MessageHandler

type MiddlewareChain struct {
	middlewares []Middleware
}

// NewMiddlewareChain creates a new middleware chain
func NewMiddlewareChain(middlewares ...Middleware) *MiddlewareChain {
	return &MiddlewareChain{middlewares: middlewares}
}

func (mc *MiddlewareChain) Apply(handler MessageHandler) MessageHandler {
	if len(mc.middlewares) == 0 {
		return handler
	}
	final := handler
	for i := len(mc.middlewares) - 1; i >= 0; i-- {
		final = mc.middlewares[i](final)
	}
	return final
}

func (mc *MiddlewareChain) Add(m Middleware) *MiddlewareChain {
	mc.middlewares = append(mc.middlewares, m)
	return mc
}

type middlewareManager struct {
	sync.RWMutex
	global []Middleware
}

func (mm *middlewareManager) Use(middleware Middleware) {
	mm.Lock()
	defer mm.Unlock()
	mm.global = append(mm.global, middleware)
}

func (mm *middlewareManager) GetGlobal() []Middleware {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.global)
}

// HandlerGroup represents a group of handlers with shared configuration
type HandlerGroup struct {
	client      *Client
	groupID     int
	priority    int
	middlewares []Middleware
	filters     []Filter
}

// Use adds middleware to this group
func (hg *HandlerGroup) Use(m Middleware) *HandlerGroup {
	hg.middlewares = append(hg.middlewares, m)
	return hg
}

// Filter adds filter to this group
func (hg *HandlerGroup) Filter(f Filter) *HandlerGroup {
	hg.filters = append(hg.filters, f)
	return hg
}

// Priority sets the priority for handlers in this group
func (hg *HandlerGroup) Priority(p int) *HandlerGroup {
	hg.priority = p
	return hg
}

// OnMessage registers a message handler in this group
func (hg *HandlerGroup) OnMessage(pattern string, handler MessageHandler) *MessageHandleBuilder {
	if pattern == "" {
		pattern = string(OnMessage)
	}
	return hg.client.OnMessage(pattern, handler).
		Group(hg.groupID).
		Priority(hg.priority).
		Use(hg.middlewares...).
		Filter(hg.filters...)
}

// OnCommand registers a command handler in this group
func (hg *HandlerGroup) OnCommand(command string, handler MessageHandler) *MessageHandleBuilder {
	return hg.client.OnCommand(command, handler).
		Group(hg.groupID).
		Priority(hg.priority).
		Use(hg.middlewares...).
		Filter(hg.filters...)
}

// OnCallback registers a callback handler in this group
func (hg *HandlerGroup) OnCallback(pattern string, handler CallbackHandler) *CallbackHandleBuilder {
	return hg.client.OnCallback(pattern, handler).
		Group(hg.groupID).
		Priority(hg.priority)
}

// MessageHandleBuilder provides fluent API for configuring message handlers
type MessageHandleBuilder struct {
	handle      *messageHandle
	client      *Client
	registered  bool
	middlewares []Middleware
}

func (hb *MessageHandleBuilder) Group(group int) *MessageHandleBuilder {
	if hb.registered {
		hb.handle.SetGroup(group)
	} else {
		hb.handle.Group = group
	}
	return hb
}

func (hb *MessageHandleBuilder) Priority(priority int) *MessageHandleBuilder {
	if hb.registered {
		hb.handle.SetPriority(priority)
	} else {
		hb.handle.priority = priority
	}
	return hb
}

func (hb *MessageHandleBuilder) Filter(filters ...Filter) *MessageHandleBuilder {
	hb.handle.Filters = append(hb.handle.Filters, filters...)
	return hb
}

func (hb *MessageHandleBuilder) Use(middlewares ...Middleware) *MessageHandleBuilder {
	hb.middlewares = append(hb.middlewares, middlewares...)
	hb.handle.middlewares = append(hb.handle.middlewares, middlewares...)
	return hb
}

func (hb *MessageHandleBuilder) Name(name string) *MessageHandleBuilder {
	hb.handle.name = name
	return hb
}

func (hb *MessageHandleBuilder) Description(desc string) *MessageHandleBuilder {
	hb.handle.description = desc
	return hb
}

func (hb *MessageHandleBuilder) Private() *MessageHandleBuilder {
	return hb.Filter(FilterPrivate)
}

func (hb *MessageHandleBuilder) Groups() *MessageHandleBuilder {
	return hb.Filter(FilterGroup)
}

func (hb *MessageHandleBuilder) Channels() *MessageHandleBuilder {
	return hb.Filter(FilterChannel)
}

func (hb *MessageHandleBuilder) From(userIDs ...int64) *MessageHandleBuilder {
	return hb.Filter(FromUser(userIDs...))
}

func (hb *MessageHandleBuilder) In(chatIDs ...int64) *MessageHandleBuilder {
	return hb.Filter(InChat(chatIDs...))
}

func (hb *MessageHandleBuilder) Register() Handle {
	if hb.registered {
		return hb.handle
	}
	hb.client.dispatcher.Lock()
	defer hb.client.dispatcher.Unlock()
	hb.registered = true
	return addHandleToMap(hb.client.dispatcher.messageHandles, hb.handle)
}

func (hb *MessageHandleBuilder) Handle() Handle {
	return hb.handle
}

type CallbackHandleBuilder struct {
	handle     *callbackHandle
	client     *Client
	registered bool
}

func (cb *CallbackHandleBuilder) Group(group int) *CallbackHandleBuilder {
	if cb.registered {
		cb.handle.SetGroup(group)
	} else {
		cb.handle.Group = group
	}
	return cb
}

func (cb *CallbackHandleBuilder) Priority(priority int) *CallbackHandleBuilder {
	if cb.registered {
		cb.handle.SetPriority(priority)
	} else {
		cb.handle.priority = priority
	}
	return cb
}

func (cb *CallbackHandleBuilder) Filter(filters ...Filter) *CallbackHandleBuilder {
	cb.handle.Filters = append(cb.handle.Filters, filters...)
	return cb
}

func (cb *CallbackHandleBuilder) Name(name string) *CallbackHandleBuilder {
	cb.handle.name = name
	return cb
}

func (cb *CallbackHandleBuilder) Private() *CallbackHandleBuilder {
	return cb.Filter(FilterPrivate)
}

func (cb *CallbackHandleBuilder) From(userIDs ...int64) *CallbackHandleBuilder {
	return cb.Filter(FromUser(userIDs...))
}

func (cb *CallbackHandleBuilder) In(chatIDs ...int64) *CallbackHandleBuilder {
	return cb.Filter(InChat(chatIDs...))
}

func (cb *CallbackHandleBuilder) Register() Handle {
	if cb.registered {
		return cb.handle
	}
	cb.client.dispatcher.Lock()
	defer cb.client.dispatcher.Unlock()
	cb.registered = true
	return addHandleToMap(cb.client.dispatcher.callbackHandles, cb.handle)
}

func (cb *CallbackHandleBuilder) Handle() Handle {
	return cb.handle
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

func (c *patternCache) Get(pattern string) (*regexp.Regexp, error) {
	c.RLock()
	if regex, exists := c.patterns[pattern]; exists {
		c.RUnlock()
		return regex, nil
	}
	c.RUnlock()

	c.Lock()
	defer c.Unlock()

	if regex, exists := c.patterns[pattern]; exists {
		return regex, nil
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern %q: %w", pattern, err)
	}

	c.patterns[pattern] = regex
	return regex, nil
}

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
	return Any(filters...)
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

var handleIDCounter atomic.Uint64

func nextHandleID() uint64 {
	return handleIDCounter.Add(1)
}

type baseHandle struct {
	id                uint64
	Group             int
	priority          int
	name              string
	description       string
	enabled           bool
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
	updateType   Update
	updateTypeID uint32
	Handler      RawHandler
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

	d.RLock()
	albumHandles := make(map[int][]*albumHandle, len(d.albumHandles))
	maps.Copy(albumHandles, d.albumHandles)
	d.RUnlock()

	for gp, handlers := range albumHandles {
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

type channelState struct {
	pts        int32
	qts        int32
	accessHash int64
}

// UpdateState represents the current update state
type UpdateState struct {
	Pts  int32
	Qts  int32
	Seq  int32
	Date int32
}

type pendingGap struct {
	timestamp time.Time
	start     int32
	end       int32
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
	nextUpdatesDeadline   time.Time
	lastUpdateTimeNano    atomic.Int64
	state                 UpdateState
	channelStates         map[int64]*channelState
	pendingGaps           []pendingGap
	stopChan              chan struct{}
	patternCache          *patternCache
	middlewareManager     *middlewareManager
	channelDiffAbort      map[int64]chan struct{}
	channelDiffAbortMu    sync.Mutex
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

func (c *Client) NewUpdateDispatcher(sessionName ...string) {
	c.dispatcher = &UpdateDispatcher{
		logger: c.Log.WithPrefix("gogram " +
			lp("updates", getVariadic(sessionName, ""))),
		channelStates:         make(map[int64]*channelState),
		pendingGaps:           []pendingGap{},
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
		middlewareManager:     &middlewareManager{},
		channelDiffAbort:      make(map[int64]chan struct{}),
	}
	c.dispatcher.lastUpdateTimeNano.Store(time.Now().UnixNano())
	c.dispatcher.logger.Debug("update dispatcher initialized")
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

type handleWithID interface {
	getID() uint64
	getPriority() int
}

func (h *baseHandle) getID() uint64 {
	return h.id
}

func (h *baseHandle) getPriority() int {
	return h.priority
}

func removeHandleFromMap[T handleWithID](handle T, handlesMap map[int][]T) {
	targetID := handle.getID()
	for key := range handlesMap {
		handles := handlesMap[key]
		for i := len(handles) - 1; i >= 0; i-- {
			if handles[i].getID() == targetID {
				handlesMap[key] = slices.Delete(handles, i, i+1)
				return
			}
		}
	}
}

var (
	updateTypeIDs   = make(map[string]uint32)
	updateTypeIDMu  sync.RWMutex
	nextTypeIDValue uint32 = 1
)

func getUpdateTypeID(update Update) uint32 {
	if update == nil {
		return 0
	}
	typeName := fmt.Sprintf("%T", update)
	updateTypeIDMu.RLock()
	if id, ok := updateTypeIDs[typeName]; ok {
		updateTypeIDMu.RUnlock()
		return id
	}
	updateTypeIDMu.RUnlock()

	updateTypeIDMu.Lock()
	defer updateTypeIDMu.Unlock()
	if id, ok := updateTypeIDs[typeName]; ok {
		return id
	}
	id := nextTypeIDValue
	nextTypeIDValue++
	updateTypeIDs[typeName] = id
	return id
}

// ---------------------------- Handle Functions ----------------------------

func (c *Client) handleMessageUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		if msg.Out {
			msg.FromID = &PeerUser{UserID: c.Me().ID}
		}

		if msg.GroupedID != 0 {
			c.handleAlbum(*msg)
		}

		packed := packMessage(c, msg)
		handle := func(h *messageHandle) error {
			if msg.Out && !h.hasOutgoingFilter() {
				return nil
			}
			if h.runFilterChain(packed, h.Filters) {
				defer c.NewRecovery()()

				handler := h.Handler
				var mids []Middleware

				c.dispatcher.RLock()
				if c.dispatcher.middlewareManager != nil {
					mids = append(mids, c.dispatcher.middlewareManager.global...)
				}
				c.dispatcher.RUnlock()
				mids = append(mids, h.middlewares...)

				if len(mids) > 0 {
					handler = applyMiddlewares(handler, mids)
				}

				err := handler(packed)

				if err != nil {
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

		groupsToProcess := make([]groupWithHandlers, 0, len(allMessageHandles))

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
			for _, handler := range defaultHandlers {
				if handler.IsMatch(msg.Message, c) {
					h := handler
					go func() {
						if err := handle(h); err != nil && !errors.Is(err, ErrEndGroup) {
							c.dispatcher.logger.WithError(err).Error("[NewMessageHandler]")
						}
					}()
				}
			}
		}

	case *MessageService:

		packed := packMessage(c, msg)
		if msg.Out {
			return
		}

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
				if handler.IsMatch(msg.Message, c) {
					handle := func(h *messageEditHandle) error {
						if h.runFilterChain(packed, h.Filters) {
							defer c.NewRecovery()()

							handler := h.Handler
							var mids []Middleware

							c.dispatcher.RLock()
							if c.dispatcher.middlewareManager != nil {
								mids = append(mids, c.dispatcher.middlewareManager.global...)
							}
							c.dispatcher.RUnlock()
							if len(mids) > 0 {
								handler = applyMiddlewares(handler, mids)
							}

							err := handler(packed)

							if err != nil {
								return err
							}
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
			if handler.IsMatch(update.Data, c) {
				handle := func(h *callbackHandle) error {
					if h.runFilterChain(packed, h.Filters) {
						defer c.NewRecovery()()
						err := h.Handler(packed)
						if err != nil {
							return err
						}
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
			if handler.IsMatch(update.Data, c) {
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
			if handler.IsMatch(update.Query, c) {
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

	updateTypeID := getUpdateTypeID(update)

	for group, handlers := range rawHandles {
		for _, handler := range handlers {
			if handler == nil || handler.Handler == nil {
				continue
			}
			if handler.updateTypeID == updateTypeID || handler.updateTypeID == 0 {
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

func (h *inlineHandle) IsMatch(text string, c *Client) bool {
	switch pattern := h.Pattern.(type) {
	case string:
		if pattern == string(OnInlineQuery) || pattern == string(OnInline) {
			return true
		}
		if !strings.HasPrefix(pattern, "^") {
			pattern = "^" + pattern
		}

		reg, err := c.dispatcher.patternCache.Get(pattern)
		if err != nil {
			return strings.HasPrefix(text, pattern)
		}
		return reg.MatchString(text)
	case *regexp.Regexp:
		return pattern.MatchString(text)
	default:
		return false
	}
}

func (e *messageEditHandle) IsMatch(text string, c *Client) bool {
	switch pattern := e.Pattern.(type) {
	case string:
		if pattern == string(OnEditMessage) || pattern == string(OnEdit) {
			return true
		}
		p := "^" + pattern
		reg, err := c.dispatcher.patternCache.Get(p)
		if err != nil {
			return strings.HasPrefix(text, pattern)
		}
		return reg.MatchString(text)
	case *regexp.Regexp:
		return pattern.MatchString(text)
	default:
		return false
	}
}

func (h *callbackHandle) IsMatch(data []byte, c *Client) bool {
	switch pattern := h.Pattern.(type) {
	case string:
		if pattern == string(OnCallbackQuery) || pattern == string(OnCallback) {
			return true
		}
		if !strings.HasPrefix(pattern, "^") {
			pattern = "^" + pattern
		}
		reg, err := c.dispatcher.patternCache.Get(pattern)
		if err != nil {
			return strings.HasPrefix(string(data), pattern)
		}
		return reg.Match(data)
	case *regexp.Regexp:
		return pattern.Match(data)
	default:
		return false
	}
}

func (h *inlineCallbackHandle) IsMatch(data []byte, c *Client) bool {
	switch pattern := h.Pattern.(type) {
	case string:
		if pattern == string(OnInlineCallbackQuery) || pattern == string(OnInlineCallback) {
			return true
		}
		if !strings.HasPrefix(pattern, "^") {
			pattern = "^" + pattern
		}
		reg, err := c.dispatcher.patternCache.Get(pattern)
		if err != nil {
			return strings.HasPrefix(string(data), pattern)
		}
		return reg.Match(data)
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
		if Pattern == string(OnNewMessage) || Pattern == string(OnMessage) {
			return true
		}

		if after, ok := strings.CutPrefix(Pattern, "cmd:"); ok {
			prefixes := c.clientData.commandPrefixes
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

		reg, err := c.dispatcher.patternCache.Get(Pattern)
		if err != nil {
			return strings.HasPrefix(text, Pattern)
		}
		return reg.MatchString(text)
	case *regexp.Regexp:
		return Pattern.MatchString(text)
	}
	return false
}

func (h *messageHandle) runFilterChain(m *NewMessage, filters []Filter) bool {
	for _, f := range filters {
		if !f.Check(m) {
			return false
		}
	}
	return true
}

func (h *messageHandle) hasOutgoingFilter() bool {
	for _, f := range h.Filters {
		if f.HasFlag(FOutgoing) {
			return true
		}
	}
	return false
}

func (e *messageEditHandle) runFilterChain(m *NewMessage, filters []Filter) bool {
	for _, f := range filters {
		if !f.Check(m) {
			return false
		}
	}
	return true
}

func (h *callbackHandle) runFilterChain(c *CallbackQuery, filters []Filter) bool {
	for _, f := range filters {
		if !f.CheckCallback(c) {
			return false
		}
	}
	return true
}

type Filter interface {
	Check(m *NewMessage) bool
	CheckCallback(c *CallbackQuery) bool
	HasFlag(flag FilterFlag) bool
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

type flagFilter FilterFlag

func (f flagFilter) Check(m *NewMessage) bool {
	return f.checkFlags(m)
}

func (f flagFilter) CheckCallback(c *CallbackQuery) bool {
	flags := FilterFlag(f)
	if flags&FPrivate != 0 && !c.IsPrivate() {
		return false
	}
	if flags&FGroup != 0 && !c.IsGroup() {
		return false
	}
	if flags&FChannel != 0 && !c.IsChannel() {
		return false
	}
	if flags&FFromBot != 0 && (c.Sender == nil || !c.Sender.Bot) {
		return false
	}
	return true
}

func (f flagFilter) HasFlag(flag FilterFlag) bool {
	return FilterFlag(f)&flag != 0
}

func (f flagFilter) checkFlags(m *NewMessage) bool {
	flags := FilterFlag(f)
	if flags&FPrivate != 0 && !m.IsPrivate() {
		return false
	}
	if flags&FGroup != 0 && !m.IsGroup() {
		return false
	}
	if flags&FChannel != 0 && !m.IsChannel() {
		return false
	}
	if flags&FMedia != 0 && !m.IsMedia() {
		return false
	}
	if flags&FCommand != 0 && !m.IsCommand() {
		return false
	}
	if flags&FReply != 0 && !m.IsReply() {
		return false
	}
	if flags&FForward != 0 && !m.IsForward() {
		return false
	}
	if flags&FFromBot != 0 && (m.Sender == nil || !m.Sender.Bot) {
		return false
	}
	if flags&FMention != 0 && (m.Message == nil || !m.Message.Mentioned) {
		return false
	}
	if flags&FOutgoing != 0 && (m.Message == nil || !m.Message.Out) {
		return false
	}
	if flags&FIncoming != 0 && (m.Message == nil || m.Message.Out) {
		return false
	}
	if flags&FEdited != 0 && (m.Message == nil || m.Message.EditDate == 0) {
		return false
	}
	if flags&FText != 0 && m.Text() == "" {
		return false
	}

	if flags&FPhoto != 0 && m.Photo() == nil {
		return false
	}
	if flags&FVideo != 0 && m.Video() == nil {
		return false
	}
	if flags&FAudio != 0 && m.Audio() == nil {
		return false
	}
	if flags&FVoice != 0 && m.Voice() == nil {
		return false
	}
	if flags&FDocument != 0 && m.Document() == nil {
		return false
	}
	if flags&FContact != 0 && m.Contact() == nil {
		return false
	}
	if flags&FLocation != 0 {
		if _, ok := m.Media().(*MessageMediaGeo); !ok {
			return false
		}
	}
	if flags&FVenue != 0 {
		if _, ok := m.Media().(*MessageMediaVenue); !ok {
			return false
		}
	}
	if flags&FPoll != 0 {
		if _, ok := m.Media().(*MessageMediaPoll); !ok {
			return false
		}
	}

	if flags&FAnimation != 0 {
		if doc := m.Document(); doc != nil {
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
		} else {
			return false
		}
	}
	if flags&FVideoNote != 0 {
		if doc := m.Document(); doc != nil {
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
		} else {
			return false
		}
	}

	return true
}

type userFilter struct {
	users []int64
}

func (f userFilter) Check(m *NewMessage) bool {
	return slices.Contains(f.users, m.SenderID())
}

func (f userFilter) CheckCallback(c *CallbackQuery) bool {
	return slices.Contains(f.users, c.SenderID)
}

func (f userFilter) HasFlag(flag FilterFlag) bool { return false }

type chatFilter struct {
	chats []int64
}

func (f chatFilter) Check(m *NewMessage) bool {
	return slices.Contains(f.chats, m.ChatID())
}

func (f chatFilter) CheckCallback(c *CallbackQuery) bool {
	// Callbacks like GameShortName don't always have ChatID easily accessible or consistent?
	// But usually c.ChatID() is available if message is present.
	// For simple callbacks, it should work.
	return slices.Contains(f.chats, c.ChatID)
}

func (f chatFilter) HasFlag(flag FilterFlag) bool { return false }

type customFilter struct {
	fn func(*NewMessage) bool
}

func (f customFilter) Check(m *NewMessage) bool {
	return f.fn(m)
}

func (f customFilter) CheckCallback(c *CallbackQuery) bool { return true }

func (f customFilter) HasFlag(flag FilterFlag) bool { return false }

type customCallbackFilter struct {
	fn func(*CallbackQuery) bool
}

func (f customCallbackFilter) Check(m *NewMessage) bool { return true }

func (f customCallbackFilter) CheckCallback(c *CallbackQuery) bool {
	return f.fn(c)
}

func (f customCallbackFilter) HasFlag(flag FilterFlag) bool { return false }

type lengthFilter struct {
	min int
	max int
}

func (f lengthFilter) Check(m *NewMessage) bool {
	l := len(m.Text())
	if f.min > 0 && l < f.min {
		return false
	}
	if f.max > 0 && l > f.max {
		return false
	}
	return true
}

func (f lengthFilter) CheckCallback(c *CallbackQuery) bool { return true }
func (f lengthFilter) HasFlag(flag FilterFlag) bool        { return false }

type anyFilter []Filter

func (fs anyFilter) Check(m *NewMessage) bool {
	for _, f := range fs {
		if f.Check(m) {
			return true
		}
	}
	return false
}

func (fs anyFilter) CheckCallback(c *CallbackQuery) bool {
	for _, f := range fs {
		if f.CheckCallback(c) {
			return true
		}
	}
	return false
}

func (fs anyFilter) HasFlag(flag FilterFlag) bool {
	for _, f := range fs {
		if f.HasFlag(flag) {
			return true
		}
	}
	return false
}

type allFilter []Filter

func (fs allFilter) Check(m *NewMessage) bool {
	for _, f := range fs {
		if !f.Check(m) {
			return false
		}
	}
	return true
}

func (fs allFilter) CheckCallback(c *CallbackQuery) bool {
	for _, f := range fs {
		if !f.CheckCallback(c) {
			return false
		}
	}
	return true
}

func (fs allFilter) HasFlag(flag FilterFlag) bool {
	for _, f := range fs {
		if f.HasFlag(flag) {
			return true
		}
	}
	return false
}

type notFilter struct {
	f Filter
}

func (n notFilter) Check(m *NewMessage) bool {
	return !n.f.Check(m)
}

func (n notFilter) CheckCallback(c *CallbackQuery) bool {
	return !n.f.CheckCallback(c)
}

func (n notFilter) HasFlag(flag FilterFlag) bool {
	return n.f.HasFlag(flag)
}

func FromUser(ids ...int64) Filter { return userFilter{users: ids} }
func InChat(ids ...int64) Filter   { return chatFilter{chats: ids} }
func TextMinLen(n int) Filter      { return lengthFilter{min: n} }
func TextMaxLen(n int) Filter      { return lengthFilter{max: n} }

func Custom(fn func(*NewMessage) bool) Filter            { return customFilter{fn: fn} }
func CustomCallback(fn func(*CallbackQuery) bool) Filter { return customCallbackFilter{fn: fn} }

func Not(f Filter) Filter {
	return notFilter{f: f}
}

func Any(fs ...Filter) Filter {
	return anyFilter(fs)
}

func All(fs ...Filter) Filter {
	return allFilter(fs)
}

var (
	FilterPrivate   Filter = flagFilter(FPrivate)
	FilterGroup     Filter = flagFilter(FGroup)
	FilterChannel   Filter = flagFilter(FChannel)
	FilterMedia     Filter = flagFilter(FMedia)
	FilterCommand   Filter = flagFilter(FCommand)
	FilterReply     Filter = flagFilter(FReply)
	FilterForward   Filter = flagFilter(FForward)
	FilterFromBot   Filter = flagFilter(FFromBot)
	FilterMention   Filter = flagFilter(FMention)
	FilterOutgoing  Filter = flagFilter(FOutgoing)
	FilterIncoming  Filter = flagFilter(FIncoming)
	FilterEdited    Filter = flagFilter(FEdited)
	FilterPhoto     Filter = flagFilter(FPhoto)
	FilterVideo     Filter = flagFilter(FVideo)
	FilterDocument  Filter = flagFilter(FDocument)
	FilterAudio     Filter = flagFilter(FAudio)
	FilterSticker   Filter = flagFilter(FSticker)
	FilterAnimation Filter = flagFilter(FAnimation)
	FilterVoice     Filter = flagFilter(FVoice)
	FilterVideoNote Filter = flagFilter(FVideoNote)
	FilterContact   Filter = flagFilter(FContact)
	FilterLocation  Filter = flagFilter(FLocation)
	FilterVenue     Filter = flagFilter(FVenue)
	FilterPoll      Filter = flagFilter(FPoll)
	FilterText      Filter = flagFilter(FText)
)

var (
	IsPrivate  = FilterPrivate
	IsGroup    = FilterGroup
	IsChannel  = FilterChannel
	IsMedia    = FilterMedia
	IsCommand  = FilterCommand
	IsReply    = FilterReply
	IsForward  = FilterForward
	IsMention  = FilterMention
	IsOutgoing = FilterOutgoing
	IsIncoming = FilterIncoming
	IsEdited   = FilterEdited
	IsText     = FilterText
	IsPhoto    = FilterPhoto
	IsVideo    = FilterVideo
	IsAudio    = FilterAudio
	IsVoice    = FilterVoice
	IsBot      = FilterFromBot
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

func makePriorityChangeCallback[T handleWithID](handleMap map[int][]T, handle T, handleID uint64, getGroup func() int, getPriority func() int, mu *sync.RWMutex) func() {
	return func() {
		mu.Lock()
		defer mu.Unlock()
		group := getGroup()
		handlers := handleMap[group]

		for i := range handlers {
			if handlers[i].getID() == handleID {
				handlers = append(handlers[:i], handlers[i+1:]...)
				handleMap[group] = handlers
				break
			}
		}

		handlers = handleMap[group]
		inserted := false
		myPriority := getPriority()
		for i := range handlers {
			if myPriority > handlers[i].getPriority() {
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

func makeGroupChangeCallback[T handleWithID](handleMap map[int][]T, handle T, handleID uint64, mu *sync.RWMutex) func(int, int) {
	return func(oldGroup, newGroup int) {
		mu.Lock()
		defer mu.Unlock()
		if old, ok := handleMap[oldGroup]; ok {
			for i := range old {
				if old[i].getID() == handleID {
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

	handleID := nextHandleID()
	handle := &messageHandle{
		Pattern: pattern,
		Handler: handler,
		Filters: messageFilters,
		baseHandle: baseHandle{
			id:      handleID,
			Group:   DefaultGroup,
			enabled: true,
		},
	}

	handle.onGroupChanged = makeGroupChangeCallback(c.dispatcher.messageHandles, handle, handleID, &c.dispatcher.RWMutex)
	handle.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.messageHandles, handle, handleID, handle.GetGroup, handle.GetPriority, &c.dispatcher.RWMutex)
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
	handleID := nextHandleID()
	h := &messageDeleteHandle{
		Pattern:    pattern,
		Handler:    handler,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.messageDeleteHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.messageDeleteHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.messageDeleteHandles, h)
}

func (c *Client) AddAlbumHandler(handler func(m *Album) error) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	handleID := nextHandleID()
	h := &albumHandle{
		Handler:    handler,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.albumHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.albumHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.albumHandles, h)
}

func (c *Client) AddActionHandler(handler MessageHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	handleID := nextHandleID()
	h := &chatActionHandle{
		Handler:    handler,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.actionHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.actionHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.actionHandles, h)
}

func (c *Client) AddEditHandler(pattern any, handler MessageHandler, filters ...Filter) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}
	handleID := nextHandleID()
	h := &messageEditHandle{
		Pattern:    pattern,
		Handler:    handler,
		Filters:    messageFilters,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.messageEditHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.messageEditHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.messageEditHandles, h)
}

func (c *Client) AddInlineHandler(pattern any, handler InlineHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	handleID := nextHandleID()
	h := &inlineHandle{
		Pattern:    pattern,
		Handler:    handler,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.inlineHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.inlineHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.inlineHandles, h)
}

func (c *Client) AddInlineSendHandler(handler InlineSendHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	handleID := nextHandleID()
	h := &inlineSendHandle{
		Handler:    handler,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.inlineSendHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.inlineSendHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.inlineSendHandles, h)
}

func (c *Client) AddCallbackHandler(pattern any, handler CallbackHandler, filters ...Filter) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}
	handleID := nextHandleID()
	h := &callbackHandle{
		Pattern:    pattern,
		Handler:    handler,
		Filters:    messageFilters,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.callbackHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.callbackHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.callbackHandles, h)
}

func (c *Client) AddInlineCallbackHandler(pattern any, handler InlineCallbackHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	handleID := nextHandleID()
	h := &inlineCallbackHandle{
		Pattern:    pattern,
		Handler:    handler,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.inlineCallbackHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.inlineCallbackHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.inlineCallbackHandles, h)
}

func (c *Client) AddJoinRequestHandler(handler PendingJoinHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	handleID := nextHandleID()
	h := &joinRequestHandle{
		Handler:    handler,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.joinRequestHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.joinRequestHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.joinRequestHandles, h)
}

func (c *Client) AddParticipantHandler(handler ParticipantHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	handleID := nextHandleID()
	h := &participantHandle{
		Handler:    handler,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.participantHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.participantHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.participantHandles, h)
}

func (c *Client) AddRawHandler(updateType Update, handler RawHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	handleID := nextHandleID()
	var typeID uint32
	if updateType != nil {
		typeID = getUpdateTypeID(updateType)
	}
	h := &rawHandle{
		updateType:   updateType,
		updateTypeID: typeID,
		Handler:      handler,
		baseHandle:   baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.rawHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.rawHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.rawHandles, h)
}

func (c *Client) AddE2EHandler(handler func(update Update, c *Client) error) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	handleID := nextHandleID()
	h := &e2eHandle{
		Handler:    handler,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.e2eHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.e2eHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.e2eHandles, h)
}

func commonPtsFromUpdate(u any) (pts int32, ptsCount int32, ok bool) {
	if u == nil {
		return 0, 0, false
	}

	switch up := u.(type) {
	case *UpdateNewMessage:
		return normalizePts(up.Pts, up.PtsCount)
	case *UpdateEditMessage:
		return normalizePts(up.Pts, up.PtsCount)
	case *UpdateDeleteMessages:
		return normalizePts(up.Pts, up.PtsCount)
	case *UpdateReadHistoryInbox:
		return normalizePts(up.Pts, up.PtsCount)
	case *UpdateReadHistoryOutbox:
		return normalizePts(up.Pts, up.PtsCount)
	case *UpdateReadMessagesContents:
		return normalizePts(up.Pts, up.PtsCount)
	case *UpdatePinnedMessages:
		return normalizePts(up.Pts, up.PtsCount)
	case *UpdateWebPage:
		return normalizePts(up.Pts, up.PtsCount)
	case *UpdateFolderPeers:
		return normalizePts(up.Pts, up.PtsCount)
	case *UpdateShortMessage:
		return normalizePts(up.Pts, up.PtsCount)
	case *UpdateShortChatMessage:
		return normalizePts(up.Pts, up.PtsCount)
	case *UpdateShortSentMessage:
		return normalizePts(up.Pts, up.PtsCount)
	default:
		return 0, 0, false
	}
}

// qtsFromUpdate extracts Qts from an update-like object (if present).
func qtsFromUpdate(u any) (qts int32, ok bool) {
	if u == nil {
		return 0, false
	}

	switch up := u.(type) {
	case *UpdateNewEncryptedMessage:
		return normalizeQts(up.Qts)
	case *UpdateBotBusinessConnect:
		return normalizeQts(up.Qts)
	case *UpdateBotChatBoost:
		return normalizeQts(up.Qts)
	case *UpdateBotChatInviteRequester:
		return normalizeQts(up.Qts)
	case *UpdateBotDeleteBusinessMessage:
		return normalizeQts(up.Qts)
	case *UpdateBotEditBusinessMessage:
		return normalizeQts(up.Qts)
	case *UpdateBotMessageReaction:
		return normalizeQts(up.Qts)
	case *UpdateBotMessageReactions:
		return normalizeQts(up.Qts)
	case *UpdateBotNewBusinessMessage:
		return normalizeQts(up.Qts)
	case *UpdateBotPurchasedPaidMedia:
		return normalizeQts(up.Qts)
	case *UpdateBotStopped:
		return normalizeQts(up.Qts)
	case *UpdateBotSubscriptionExpire:
		return normalizeQts(up.Qts)
	case *UpdateChannelParticipant:
		return normalizeQts(up.Qts)
	case *UpdateChatParticipant:
		return normalizeQts(up.Qts)
	case *UpdateMessagePollVote:
		return normalizeQts(up.Qts)
	default:
		return 0, false
	}
}

// channelPtsFromUpdate extracts channel-scoped (ChannelID, Pts, PtsCount) if present.
func channelPtsFromUpdate(u any) (channelID int64, pts int32, ptsCount int32, ok bool) {
	if u == nil {
		return 0, 0, 0, false
	}

	switch up := u.(type) {
	case *UpdateDeleteChannelMessages:
		return normalizeChannelPts(up.ChannelID, up.Pts, up.PtsCount)
	case *UpdatePinnedChannelMessages:
		return normalizeChannelPts(up.ChannelID, up.Pts, up.PtsCount)
	case *UpdateReadChannelInbox:
		return normalizeChannelPts(up.ChannelID, up.Pts, 1)
	case *UpdateChannelWebPage:
		return normalizeChannelPts(up.ChannelID, up.Pts, up.PtsCount)
	case *UpdateChannelTooLong:
		return normalizeChannelPts(up.ChannelID, up.Pts, 1)
	case *UpdateNewChannelMessage:
		if channelID, ok := channelIDFromMessage(up.Message); ok {
			return normalizeChannelPts(channelID, up.Pts, up.PtsCount)
		}
		return 0, 0, 0, false
	case *UpdateEditChannelMessage:
		if channelID, ok := channelIDFromMessage(up.Message); ok {
			return normalizeChannelPts(channelID, up.Pts, up.PtsCount)
		}
		return 0, 0, 0, false
	default:
		return 0, 0, 0, false
	}
}

func (c *Client) applyUpdateState(update Update) bool {
	if c == nil || c.dispatcher == nil {
		return true
	}
	if qts, ok := qtsFromUpdate(update); ok {
		if !c.dispatcher.manageQts(c, qts) {
			return false
		}
	}
	if channelID, pts, ptsCount, ok := channelPtsFromUpdate(update); ok {
		if !c.dispatcher.manageChannelPts(c, channelID, pts, ptsCount) {
			return false
		}
	}
	if pts, ptsCount, ok := commonPtsFromUpdate(update); ok {
		if !c.dispatcher.managePts(c, pts, ptsCount) {
			return false
		}
	}
	return true
}

func (c *Client) dispatchUpdateToHandlers(update Update) {
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
	case *UpdateDeleteMessages:
		go c.handleDeleteUpdate(update)
	case *UpdateDeleteChannelMessages:
		go c.handleDeleteUpdate(update)
	case *UpdateReadHistoryInbox:
		//
	case *UpdateReadHistoryOutbox:
		//
	case *UpdateWebPage:
		//
	case *UpdateReadMessagesContents:
		//
	case *UpdateReadChannelInbox:
		//
	case *UpdateChannelWebPage:
		//
	case *UpdateFolderPeers:
		//
	case *UpdatePinnedMessages:
		//
	case *UpdatePinnedChannelMessages:
		//
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
		if update.Pts != 0 && c.dispatcher.GetChannelPts(update.ChannelID) == 0 {
			c.dispatcher.SetChannelPts(update.ChannelID, update.Pts)
		}
		c.Log.Trace("[updates] updateChannelTooLong -> triggering getChannelDifference (channel=%d)", update.ChannelID)
		go c.fetchChannelDifference(update.ChannelID, 5000)
	case *UpdateEncryption, *UpdateNewEncryptedMessage:
		go c.HandleSecretChatUpdate(update)
	}
}

// HandleIncomingUpdates processes incoming updates and dispatches them to the appropriate handlers.
func HandleIncomingUpdates(u any, c *Client) bool {
	c.dispatcher.UpdateLastUpdateTime()
	c.dispatcher.nextUpdatesDeadline = time.Now().Add(time.Minute * 15)

	switch upd := u.(type) {
	case *UpdatesObj:
		if !c.dispatcher.manageSeq(c, upd.Seq, upd.Seq) {
			return true
		}

		c.dispatcher.SetDate(upd.Date)
		go c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
		for _, update := range upd.Updates {
			if !c.applyUpdateState(update) {
				continue
			}
			c.dispatchUpdateToHandlers(update)
			go c.handleRawUpdate(update)
		}
	case *UpdateShort:
		switch upd := upd.Update.(type) {
		case *UpdateNewMessage:
			if !c.applyUpdateState(upd) {
				return true
			}
			go c.fetchPeersBeforeUpdate(upd.Message, upd.Pts)
		case *UpdateNewChannelMessage:
			if !c.applyUpdateState(upd) {
				return true
			}
			go c.fetchPeersBeforeUpdate(upd.Message, upd.Pts)
		case *UpdateChannelTooLong:
			if upd.Pts != 0 && c.dispatcher.GetChannelPts(upd.ChannelID) == 0 {
				c.dispatcher.SetChannelPts(upd.ChannelID, upd.Pts)
			}
			c.Log.Trace("[updates] updateChannelTooLong -> triggering getChannelDifference (channel=%d)", upd.ChannelID)
			go c.fetchChannelDifference(upd.ChannelID, 5000)
		}
		go c.handleRawUpdate(upd.Update)
	case *UpdateShortMessage:
		update := &MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.UserID), PeerID: getPeerUser(upd.UserID), Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}
		if !c.dispatcher.managePts(c, upd.Pts, upd.PtsCount) {
			return true
		}
		go c.fetchPeersBeforeUpdate(update, upd.Pts)
		go c.handleRawUpdate(&UpdateNewMessage{Message: update, Pts: upd.Pts, PtsCount: 0})
	case *UpdateShortChatMessage:
		update := &MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.FromID), PeerID: &PeerChat{ChatID: upd.ChatID}, Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}
		if pts, ptsCount, ok := commonPtsFromUpdate(upd); ok {
			if !c.dispatcher.managePts(c, pts, ptsCount) {
				return true
			}
		}
		go c.fetchPeersBeforeUpdate(update, upd.Pts)
		go c.handleRawUpdate(&UpdateNewMessage{Message: update, Pts: upd.Pts, PtsCount: 0})
	case *UpdateShortSentMessage:
		update := &MessageObj{ID: upd.ID, Out: upd.Out, Date: upd.Date, Media: upd.Media, Entities: upd.Entities, TtlPeriod: upd.TtlPeriod}
		if pts, ptsCount, ok := commonPtsFromUpdate(upd); ok {
			if !c.dispatcher.managePts(c, pts, ptsCount) {
				return true
			}
		}
		go c.fetchPeersBeforeUpdate(update, upd.Pts)
		go c.handleRawUpdate(&UpdateNewMessage{Message: update, Pts: upd.Pts, PtsCount: 0})
	case *UpdatesCombined:
		if !c.dispatcher.manageSeq(c, upd.SeqStart, upd.Seq) {
			return true
		}
		c.dispatcher.SetDate(upd.Date)
		go c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
		for _, update := range upd.Updates {
			if !c.applyUpdateState(update) {
				continue
			}
			c.dispatchUpdateToHandlers(update)
			go c.handleRawUpdate(update)
		}
	case *UpdateChannelTooLong:
		if upd.Pts != 0 && c.dispatcher.GetChannelPts(upd.ChannelID) == 0 {
			c.dispatcher.SetChannelPts(upd.ChannelID, upd.Pts)
		}
		c.Log.Trace("[updates] updateChannelTooLong -> triggering getChannelDifference (channel=%d)", upd.ChannelID)
		c.fetchChannelDifference(upd.ChannelID, 5000)
	case *UpdatesTooLong:
		go c.fetchCommonDifference(5000)
	default:
		c.Log.Debug("unhandled update type: %T", upd)
	}
	return true
}

func (d *UpdateDispatcher) manageSeq(c *Client, seqStart, seqEnd int32) bool {
	if seqStart == 0 {
		if seqEnd != 0 {
			d.Lock()
			if d.state.Seq < seqEnd {
				d.state.Seq = seqEnd
			}
			d.Unlock()
		}
		return true
	}
	if seqEnd == 0 {
		return true
	}

	d.Lock()
	localSeq := d.state.Seq
	if localSeq == 0 {
		d.state.Seq = seqEnd
		d.Unlock()
		return true
	}

	expectedStart := localSeq + 1
	if expectedStart == seqStart {
		d.state.Seq = seqEnd
		d.Unlock()
		return true
	}
	if expectedStart > seqStart {
		d.Unlock()
		return false
	}
	d.Unlock()

	if c != nil {
		c.Log.Debug("[updates] seq gap: got=%d..%d expectedStart=%d gap=%d (localSeq=%d) -> triggering getDifference", seqStart, seqEnd, expectedStart, seqStart-expectedStart, localSeq)
		go func(client *Client) {
			if err := client.fetchCommonDifference(5000); err != nil {
				client.Log.WithError(err).Debug("[getDifference] seq gap")
			}
		}(c)
	}
	return false
}

func (c *Client) dispatchCommonUpdate(update Update) {
	if !c.applyUpdateState(update) {
		return
	}
	c.dispatchUpdateToHandlers(update)
	go c.handleRawUpdate(update)
}

func (d *UpdateDispatcher) applyStateFrom(state *UpdatesState) {
	if state == nil {
		return
	}

	d.SetSeq(state.Seq)
	d.SetDate(state.Date)
	d.SetQts(state.Qts)
	d.SetPts(state.Pts)
}

// fetchCommonDifference calls updates.getDifference using the current dispatcher state
func (c *Client) fetchCommonDifference(limit int32) error {
	pts := c.dispatcher.GetPts()
	qts := c.dispatcher.GetQts()
	date := c.dispatcher.GetDate()
	if date == 0 {
		date = int32(time.Now().Unix())
	}
	if pts == 0 {
		if qts != 0 {
			c.Log.Debug("[updates] getDifference baseline missing (pts=0, qts=%d, date=%d): using pts=1", qts, date)
		}
		pts = 1
	}

	params := &UpdatesGetDifferenceParams{
		Pts:           pts,
		PtsLimit:      limit,
		PtsTotalLimit: limit,
		Date:          date,
		Qts:           qts,
	}
	if qts != 0 {
		params.QtsLimit = limit
	}

	resp, err := c.UpdatesGetDifference(params)
	if err != nil {
		if strings.Contains(err.Error(), "PERSISTENT_TIMESTAMP_EMPTY") {
			c.Log.WithError(err).Debug("[updates] getDifference rejected (PERSISTENT_TIMESTAMP_EMPTY); seeding state via updates.getState and retrying")
			st, stErr := c.UpdatesGetState()
			if stErr != nil {
				return err
			}
			c.dispatcher.applyStateFrom(st)
			pts = c.dispatcher.GetPts()
			qts = c.dispatcher.GetQts()
			date = c.dispatcher.GetDate()
			if date == 0 {
				date = int32(time.Now().Unix())
			}
			if pts == 0 {
				pts = 1
			}
			params = &UpdatesGetDifferenceParams{
				Pts:           pts,
				PtsLimit:      limit,
				PtsTotalLimit: limit,
				Date:          date,
				Qts:           qts,
			}
			if qts != 0 {
				params.QtsLimit = limit
			}
			resp, err = c.UpdatesGetDifference(params)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	switch resp := resp.(type) {
	case *UpdatesDifferenceEmpty:
		// No updates; nothing to do.
	case *UpdatesDifferenceObj:
		go c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
		if resp.State != nil {
			c.dispatcher.applyStateFrom(resp.State)
		}
		for _, update := range resp.NewMessages {
			go c.handleMessageUpdate(update)
		}

		for _, update := range resp.OtherUpdates {
			c.dispatchCommonUpdate(update)
		}
	case *UpdatesDifferenceSlice:
		if resp.IntermediateState != nil {
			c.dispatcher.applyStateFrom(resp.IntermediateState)
		}
		for _, update := range resp.NewMessages {
			go c.handleMessageUpdate(update)
		}

		for _, update := range resp.OtherUpdates {
			c.dispatchCommonUpdate(update)
		}
	}

	return nil
}

func (c *Client) inputChannelForID(channelID int64) InputChannel {
	if c != nil && c.Cache != nil {
		if peer, err := c.Cache.getChannelPeer(channelID); err == nil && peer != nil {
			return peer
		}
		if ch, err := c.getChannelFromCache(channelID); err == nil && ch != nil {
			return &InputChannelObj{ChannelID: channelID, AccessHash: ch.AccessHash}
		}
	}
	return &InputChannelObj{ChannelID: channelID, AccessHash: 0}
}

// fetchChannelDifference calls updates.getChannelDifference for a single channel using the current dispatcher channel pts.
func (c *Client) fetchChannelDifference(channelID int64, limit int32) error {
	if channelID == 0 {
		return errors.New("channelID is zero")
	}

	pts := c.dispatcher.GetChannelPts(channelID)
	resp, err := c.UpdatesGetChannelDifference(&UpdatesGetChannelDifferenceParams{
		Channel: c.inputChannelForID(channelID),
		Filter:  &ChannelMessagesFilterEmpty{},
		Pts:     pts,
		Limit:   limit,
	})
	if err != nil {
		return err
	}

	switch resp := resp.(type) {
	case *UpdatesChannelDifferenceEmpty:
		// No updates; nothing to do.
	case *UpdatesChannelDifferenceObj:
		go c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
		c.dispatcher.SetChannelPts(channelID, resp.Pts)
		for _, m := range resp.NewMessages {
			go c.handleMessageUpdate(m)
		}

		for _, up := range resp.OtherUpdates {
			c.dispatchCommonUpdate(up)
		}
	case *UpdatesChannelDifferenceTooLong:
		// Full resync required; nothing to do here.
	}

	return nil
}

func (d *UpdateDispatcher) managePts(c *Client, pts, ptsCount int32) bool {
	if pts == 0 {
		return true
	}
	if ptsCount <= 0 {
		ptsCount = 1
	}

	d.Lock()
	localPts := d.state.Pts
	if localPts == 0 {
		d.state.Pts = pts
		d.Unlock()
		return true
	}

	localNext := localPts + ptsCount
	if localNext == pts {
		d.state.Pts = pts
		d.Unlock()
		return true
	}
	if localNext > pts {
		d.Unlock()
		return false
	}

	diff := pts - localNext
	if diff <= 1 {
		d.state.Pts = pts
		d.Unlock()
		return true
	}
	d.Unlock()

	if c != nil {
		c.Log.Debug("[updates] pts gap: got=%d expected=%d diff=%d (localPts=%d ptsCount=%d) -> triggering getDifference", pts, localNext, diff, localPts, ptsCount)
		if c.MTProto != nil {
			go func(client *Client) {
				if err := client.fetchCommonDifference(5000); err != nil {
					client.Log.WithError(err).Debug("[getDifference] pts gap")
				}
			}(c)
		}
	}
	return false
}

func (d *UpdateDispatcher) manageQts(c *Client, qts int32) bool {
	if qts == 0 {
		return true
	}

	d.Lock()
	localQts := d.state.Qts
	if localQts == 0 {
		d.state.Qts = qts
		d.Unlock()
		return true
	}

	expected := localQts + 1
	if expected == qts {
		d.state.Qts = qts
		d.Unlock()
		return true
	}
	if expected > qts {
		d.Unlock()
		return false
	}
	d.Unlock()

	// Gap: expected < qts.
	if c != nil {
		c.Log.Debug("[updates] qts gap: got=%d expected=%d (localQts=%d) -> triggering getDifference", qts, expected, localQts)
		go func(client *Client) {
			if err := client.fetchCommonDifference(5000); err != nil {
				client.Log.WithError(err).Debug("[getDifference] qts gap")
			}
		}(c)
	}
	return false
}

func (d *UpdateDispatcher) manageChannelPts(c *Client, channelID int64, pts, ptsCount int32) bool {
	if channelID == 0 || pts == 0 {
		return true
	}
	if ptsCount <= 0 {
		ptsCount = 1
	}

	d.Lock()
	if d.channelStates == nil {
		d.channelStates = make(map[int64]*channelState)
	}
	st, ok := d.channelStates[channelID]
	if !ok {
		st = &channelState{}
		d.channelStates[channelID] = st
	}
	localPts := st.pts
	if localPts == 0 {
		st.pts = pts
		d.Unlock()
		return true
	}

	localNext := localPts + ptsCount
	if localNext == pts {
		st.pts = pts
		d.Unlock()
		return true
	}
	if localNext > pts {
		d.Unlock()
		return false
	}

	diff := pts - localNext
	if diff <= 1 {
		st.pts = pts
		d.Unlock()
		return true
	}
	d.Unlock()

	if c != nil {
		c.Log.Debug("[updates] channel pts gap: channel=%d got=%d expected=%d diff=%d (localPts=%d ptsCount=%d) -> triggering getChannelDifference", channelID, pts, localNext, diff, localPts, ptsCount)
		go func(client *Client, chID int64) {
			if err := client.fetchChannelDifference(chID, 5000); err != nil {
				client.Log.WithError(err).Debug("[getChannelDifference] channel pts gap (channel=%d)", chID)
			}
		}(c, channelID)
	}
	return false
}

func (c *Client) GetDifference(Pts, Limit int32) (Message, error) {
	updates, err := c.UpdatesGetDifference(&UpdatesGetDifferenceParams{
		Pts:           Pts - 1,
		PtsLimit:      Limit,
		PtsTotalLimit: Limit,
		Date:          int32(time.Now().Unix()),
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

	case EventType:
		return eventInfo{eventType: string(p)}

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
func (c *Client) On(args ...any) Handle {
	if len(args) == 0 {
		c.Log.Error("On: missing event type argument")
		return nil
	}

	var pattern any
	var handler any
	var filters []Filter

	switch len(args) {
	case 1:
		handler = args[0]
	case 2:
		if _, ok := args[1].(Filter); ok {
			handler = args[0]
			filters = append(filters, args[1].(Filter))
		} else {
			pattern = args[0]
			handler = args[1]
		}
	default:
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
			p := info.pattern
			if p == "" {
				p = string(OnNewMessage)
			}
			return c.AddMessageHandler(p, h, filters...)
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
			p := info.pattern
			if p == "" {
				p = string(OnEditMessage)
			}
			return c.AddEditHandler(p, h, filters...)
		}
		c.Log.Error("On(edit): invalid handler type %T, expected func(*NewMessage) error", handler)

	case "delete", "deletemessage":
		if h, ok := handler.(func(m *DeleteMessage) error); ok {
			p := info.pattern
			if p == "" {
				p = string(OnDeleteMessage)
			}
			return c.AddDeleteHandler(p, h)
		}
		c.Log.Error("On(delete): invalid handler type %T, expected func(*DeleteMessage) error", handler)

	case "album":
		if h, ok := handler.(func(m *Album) error); ok {
			return c.AddAlbumHandler(h)
		}
		c.Log.Error("On(album): invalid handler type %T, expected func(*Album) error", handler)

	case "inline", "inlinequery":
		if h, ok := handler.(func(m *InlineQuery) error); ok {
			p := info.pattern
			if p == "" {
				p = string(OnInlineQuery)
			}
			return c.AddInlineHandler(p, h)
		}
		c.Log.Error("On(inline): invalid handler type %T, expected func(*InlineQuery) error", handler)

	case "choseninline", "inlinesend":
		if h, ok := handler.(func(m *InlineSend) error); ok {
			return c.AddInlineSendHandler(h)
		}
		c.Log.Error("On(choseninline): invalid handler type %T, expected func(*InlineSend) error", handler)

	case "callback", "callbackquery":
		if h, ok := handler.(func(m *CallbackQuery) error); ok {
			p := info.pattern
			if p == "" {
				p = string(OnCallbackQuery)
			}
			return c.AddCallbackHandler(p, h, filters...)
		}
		c.Log.Error("On(callback): invalid handler type %T, expected func(*CallbackQuery) error", handler)

	case "inlinecallback", "inlinecallbackquery":
		if h, ok := handler.(func(m *InlineCallbackQuery) error); ok {
			p := info.pattern
			if p == "" {
				p = string(OnInlineCallbackQuery)
			}
			return c.AddInlineCallbackHandler(p, h)
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
			return c.AddMessageHandler(string(OnNewMessage), h, filters...)
		case func(m *DeleteMessage) error:
			return c.AddDeleteHandler(string(OnDeleteMessage), h)
		case func(m *Album) error:
			return c.AddAlbumHandler(h)
		case func(m *InlineQuery) error:
			return c.AddInlineHandler(string(OnInlineQuery), h)
		case func(m *InlineSend) error:
			return c.AddInlineSendHandler(h)
		case func(m *CallbackQuery) error:
			return c.AddCallbackHandler(string(OnCallbackQuery), h, filters...)
		case func(m *InlineCallbackQuery) error:
			return c.AddInlineCallbackHandler(string(OnInlineCallbackQuery), h)
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

// Use adds global middleware to the client
func (c *Client) Use(middlewares ...Middleware) {
	if c.dispatcher.middlewareManager == nil {
		c.dispatcher.middlewareManager = &middlewareManager{}
	}
	for _, m := range middlewares {
		c.dispatcher.middlewareManager.Use(m)
	}
}

// Group creates a new handler group
func (c *Client) Group(groupID int) *HandlerGroup {
	return &HandlerGroup{client: c, groupID: groupID}
}

// OnMessage registers a message handler and returns a builder
func (c *Client) OnMessage(pattern string, handler MessageHandler, filters ...Filter) *MessageHandleBuilder {
	if pattern == "" {
		pattern = string(EventNewMessage)
	}
	h := c.AddMessageHandler(pattern, handler, filters...)

	if mh, ok := h.(*messageHandle); ok {
		return &MessageHandleBuilder{
			handle:     mh,
			client:     c,
			registered: true,
		}
	}
	return nil
}

// OnCommand registers a command handler and returns a builder
func (c *Client) OnCommand(command string, handler MessageHandler, filters ...Filter) *MessageHandleBuilder {
	h := c.AddMessageHandler("cmd:"+command, handler, filters...)
	if mh, ok := h.(*messageHandle); ok {
		return &MessageHandleBuilder{
			handle:     mh,
			client:     c,
			registered: true,
		}
	}
	return nil
}

// OnCallback registers a callback handler and returns a builder
func (c *Client) OnCallback(pattern string, handler CallbackHandler, filters ...Filter) *CallbackHandleBuilder {
	if pattern == "" {
		pattern = string(EventCallbackQuery)
	}
	h := c.AddCallbackHandler(pattern, handler, filters...)
	if cb, ok := h.(*callbackHandle); ok {
		return &CallbackHandleBuilder{
			handle:     cb,
			client:     c,
			registered: true,
		}
	}
	return nil
}

// OnInlineQuery registers an inline query handler and returns a handle
func (c *Client) OnInlineQuery(pattern string, handler func(m *InlineQuery) error) Handle {
	if pattern == "" {
		pattern = string(EventInlineQuery)
	}
	return c.AddInlineHandler(pattern, handler)
}

// OnInlineCallback registers an inline callback handler and returns a handle
func (c *Client) OnInlineCallback(pattern string, handler func(m *InlineCallbackQuery) error) Handle {
	if pattern == "" {
		pattern = string(EventInlineCallback)
	}
	return c.AddInlineCallbackHandler(pattern, handler)
}

// OnEdit registers an edit handler and returns a handle
func (c *Client) OnEdit(pattern string, handler func(m *NewMessage) error, filters ...Filter) Handle {
	if pattern == "" {
		pattern = string(EventEditMessage)
	}
	return c.AddEditHandler(pattern, handler, filters...)
}

// OnDelete registers a delete handler and returns a handle
func (c *Client) OnDelete(pattern string, handler func(m *DeleteMessage) error) Handle {
	if pattern == "" {
		pattern = string(EventDeleteMessage)
	}
	return c.AddDeleteHandler(pattern, handler)
}

// OnAlbum registers an album handler and returns a handle
func (c *Client) OnAlbum(handler func(m *Album) error) Handle {
	return c.AddAlbumHandler(handler)
}

// OnChosenInline registers a chosen inline handler and returns a handle
func (c *Client) OnChosenInline(handler func(m *InlineSend) error) Handle {
	return c.AddInlineSendHandler(handler)
}

// OnParticipant registers a participant handler and returns a handle
func (c *Client) OnParticipant(handler func(m *ParticipantUpdate) error) Handle {
	return c.AddParticipantHandler(handler)
}

// OnJoinRequest registers a join request handler and returns a handle
func (c *Client) OnJoinRequest(handler func(m *JoinRequestUpdate) error) Handle {
	return c.AddJoinRequestHandler(handler)
}

// OnRaw registers a raw handler and returns a handle
func (c *Client) OnRaw(updateType Update, handler func(m Update, c *Client) error) Handle {
	return c.AddRawHandler(updateType, handler)
}

// OnE2EMessage registers an E2E message handler and returns a handle
func (c *Client) OnE2EMessage(handler func(update Update, c *Client) error) Handle {
	return c.AddE2EHandler(handler)
}
