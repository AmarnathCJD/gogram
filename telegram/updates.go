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

type Middleware = func(MessageHandler) MessageHandler

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
	global         []Middleware
	edit           []func(EditHandler) EditHandler
	delete         []func(DeleteHandler) DeleteHandler
	album          []func(AlbumHandler) AlbumHandler
	inline         []func(InlineHandler) InlineHandler
	inlineSend     []func(InlineSendHandler) InlineSendHandler
	guestChat      []func(GuestChatQueryHandler) GuestChatQueryHandler
	callback       []func(CallbackHandler) CallbackHandler
	inlineCallback []func(InlineCallbackHandler) InlineCallbackHandler
	participant    []func(ParticipantHandler) ParticipantHandler
	joinRequest    []func(PendingJoinHandler) PendingJoinHandler
	raw            []func(RawHandler) RawHandler
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

func (mm *middlewareManager) edits() []func(EditHandler) EditHandler {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.edit)
}

func (mm *middlewareManager) deletes() []func(DeleteHandler) DeleteHandler {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.delete)
}

func (mm *middlewareManager) albums() []func(AlbumHandler) AlbumHandler {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.album)
}

func (mm *middlewareManager) inlines() []func(InlineHandler) InlineHandler {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.inline)
}

func (mm *middlewareManager) inlineSends() []func(InlineSendHandler) InlineSendHandler {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.inlineSend)
}

func (mm *middlewareManager) guestChats() []func(GuestChatQueryHandler) GuestChatQueryHandler {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.guestChat)
}

func (mm *middlewareManager) callbacks() []func(CallbackHandler) CallbackHandler {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.callback)
}

func (mm *middlewareManager) inlineCallbacks() []func(InlineCallbackHandler) InlineCallbackHandler {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.inlineCallback)
}

func (mm *middlewareManager) participants() []func(ParticipantHandler) ParticipantHandler {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.participant)
}

func (mm *middlewareManager) joinRequests() []func(PendingJoinHandler) PendingJoinHandler {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.joinRequest)
}

func (mm *middlewareManager) raws() []func(RawHandler) RawHandler {
	mm.RLock()
	defer mm.RUnlock()
	return slices.Clone(mm.raw)
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
	return hb.Filter(IsPrivate)
}

func (hb *MessageHandleBuilder) Groups() *MessageHandleBuilder {
	return hb.Filter(IsGroup)
}

func (hb *MessageHandleBuilder) Channels() *MessageHandleBuilder {
	return hb.Filter(IsChannel)
}

func (hb *MessageHandleBuilder) From(userIDs ...int64) *MessageHandleBuilder {
	return hb.Filter(FromUsers(userIDs...))
}

func (hb *MessageHandleBuilder) In(chatIDs ...int64) *MessageHandleBuilder {
	return hb.Filter(FromChats(chatIDs...))
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
	return cb.Filter(IsPrivate)
}

func (cb *CallbackHandleBuilder) From(userIDs ...int64) *CallbackHandleBuilder {
	return cb.Filter(FromUsers(userIDs...))
}

func (cb *CallbackHandleBuilder) In(chatIDs ...int64) *CallbackHandleBuilder {
	return cb.Filter(FromChats(chatIDs...))
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

func (c *lruCache) TryAdd(key int64) bool {
	c.Lock()
	defer c.Unlock()

	if elem, exists := c.items[key]; exists && elem != nil {
		if _, ok := elem.Value.(*lruEntry); ok {
			return false
		}
		delete(c.items, key)
		c.list.Remove(elem)
	}

	entry := &lruEntry{key: key, timestamp: time.Now()}
	elem := c.list.PushFront(entry)
	c.items[key] = elem

	if c.list.Len() > c.maxSize {
		oldest := c.list.Back()
		if oldest != nil {
			if entry, ok := oldest.Value.(*lruEntry); ok && entry != nil {
				delete(c.items, entry.key)
			}
			c.list.Remove(oldest)
		}
	}
	return true
}

// shardedLRU reduces lock contention by hashing keys across shards.
type shardedLRU struct {
	shards []*lruCache
}

func newShardedLRU(totalSize int, shardCount int) *shardedLRU {
	if shardCount <= 0 {
		shardCount = 1
	}
	if shardCount > 256 {
		shardCount = 256
	}
	perShard := totalSize / shardCount
	if perShard < 1 {
		perShard = 1
	}
	shards := make([]*lruCache, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = newLRUCache(perShard)
	}
	return &shardedLRU{shards: shards}
}

func (s *shardedLRU) shard(key int64) *lruCache {
	if len(s.shards) == 1 {
		return s.shards[0]
	}
	idx := uint64(key)
	idx ^= idx >> 33
	idx *= 0xff51afd7ed558ccd
	idx ^= idx >> 33
	idx *= 0xc4ceb9fe1a85ec53
	idx ^= idx >> 33
	return s.shards[idx%uint64(len(s.shards))]
}

func (s *shardedLRU) TryAdd(key int64) bool {
	return s.shard(key).TryAdd(key)
}

type patternCache struct {
	cache sync.Map
}

func newPatternCache() *patternCache {
	return &patternCache{}
}

// counterBox keeps monotonically increasing counters (pts/qts) ordered and deduplicated.
// It buffers out-of-order updates, detects gaps, and optionally triggers a fetch to fill them.
type counterBox struct {
	sync.Mutex
	name           string
	current        int32
	pending        map[int32][]pendingCounter
	recovering     bool
	recoveryEpoch  uint64
	fetchGap       func(from, target int32)
	logger         Logger
	debounce       time.Duration
	lastGapAt      time.Time
	onAdvance      func(int32)
	gapDeadline    time.Time
	reorderWait    time.Duration
	gapTimer       *time.Timer
}

type pendingCounter struct {
	counter int32
	count   int32
	apply   func()
	arrived time.Time
}

func newCounterBox(name string, logger Logger, fetch func(from, target int32), onAdvance func(int32)) *counterBox {
	return &counterBox{
		name:        name,
		pending:     make(map[int32][]pendingCounter),
		fetchGap:    fetch,
		logger:      logger,
		debounce:    time.Second,
		onAdvance:   onAdvance,
		reorderWait: 500 * time.Millisecond,
	}
}

func (b *counterBox) beginGettingDiff() {
	b.Lock()
	b.recovering = true
	b.recoveryEpoch++
	b.pending = make(map[int32][]pendingCounter)
	b.Unlock()
}

func (b *counterBox) endGettingDiff() {
	b.Lock()
	b.recovering = false
	b.Unlock()
}

// process enforces ordering using Telegram semantics where counter represents the value *after* applying the update.
// If the counter is contiguous, apply() is executed immediately; otherwise it is buffered and a gap fetch is triggered.
func (b *counterBox) process(counter, count int32, apply func()) bool {
	if counter == 0 {
		apply()
		return true
	}

	b.Lock()
	defer b.Unlock()

	if b.recovering {
		return false
	}

	prev := counter - count
	if prev < 0 {
		prev = 0
	}

	if b.current == 0 {
		b.current = counter
		b.recordAdvance(counter)
		b.runUnlocked(apply)
		b.flushLocked()
		return true
	}

	if counter <= b.current {
		return false
	}

	if prev == b.current {
		b.current = counter
		b.recordAdvance(counter)
		b.runUnlocked(apply)
		b.flushLocked()
		return true
	}

	b.logger.Debug("counterBox=%s gap counter=%d count=%d boxCurrent=%d -> buffering", b.name, counter, count, b.current)
	if prev < 0 {
		prev = 0
	}
	b.pending[prev] = append(b.pending[prev], pendingCounter{counter: counter, count: count, apply: apply, arrived: time.Now()})
	if b.gapDeadline.IsZero() {
		b.gapDeadline = time.Now().Add(b.reorderWait)
	}
	b.scheduleGapCheckLocked()
	return false
}

func (b *counterBox) scheduleGapCheckLocked() {
	if b.fetchGap == nil || b.recovering {
		return
	}
	deadline := b.gapDeadline
	if deadline.IsZero() {
		return
	}
	wait := time.Until(deadline)
	if wait <= 0 {
		b.triggerGapLocked(b.current, b.current+1)
		return
	}
	if b.gapTimer != nil {
		return
	}
	b.gapTimer = time.AfterFunc(wait, func() {
		b.Lock()
		defer b.Unlock()
		b.gapTimer = nil
		if b.recovering || len(b.pending) == 0 {
			b.gapDeadline = time.Time{}
			return
		}
		if time.Now().Before(b.gapDeadline) {
			b.scheduleGapCheckLocked()
			return
		}
		b.gapDeadline = time.Time{}
		b.triggerGapLocked(b.current, b.current+1)
	})
}

func (b *counterBox) recordAdvance(value int32) {
	if b.onAdvance != nil {
		b.onAdvance(value)
	}
}

// forceSet updates the counter, clearing any older pending entries.
// Monotonic: refuses to rewind unless value is 0 (explicit reset).
func (b *counterBox) forceSet(value int32) {
	b.Lock()
	if value != 0 && value < b.current {
		b.Unlock()
		return
	}
	b.current = value
	b.recordAdvance(value)
	for prev := range b.pending {
		if prev < value {
			delete(b.pending, prev)
		}
	}
	b.flushLocked()
	b.Unlock()
}

func (b *counterBox) processCheckpoint(counter int32, apply func()) bool {
	if counter == 0 {
		apply()
		return true
	}
	b.Lock()
	if b.recovering {
		b.Unlock()
		return false
	}
	if b.current == 0 {
		seed := counter - 1
		if seed < 1 {
			seed = 1
		}
		b.current = seed
		b.recordAdvance(seed)
		b.runUnlocked(apply)
		b.Unlock()
		return true
	}
	if counter <= b.current {
		b.runUnlocked(apply)
		b.Unlock()
		return true
	}
	b.runUnlocked(apply)
	b.Unlock()
	return true
}

func (b *counterBox) currentValue() int32 {
	b.Lock()
	defer b.Unlock()
	return b.current
}

func (b *counterBox) flushLocked() {
	for {
		list, ok := b.pending[b.current]
		if !ok || len(list) == 0 {
			for prev := range b.pending {
				if prev < b.current {
					delete(b.pending, prev)
				}
			}
			if len(b.pending) == 0 {
				b.gapDeadline = time.Time{}
			}
			return
		}

		readyItem := list[0]
		if len(list) == 1 {
			delete(b.pending, b.current)
		} else {
			b.pending[b.current] = list[1:]
		}

		b.current = readyItem.counter
		b.recordAdvance(readyItem.counter)
		b.runUnlocked(readyItem.apply)
	}
}

func (b *counterBox) triggerGapLocked(prev, target int32) {
	if b.fetchGap == nil || b.recovering {
		return
	}
	if time.Since(b.lastGapAt) < b.debounce {
		return
	}
	b.recovering = true
	b.recoveryEpoch++
	epoch := b.recoveryEpoch
	b.lastGapAt = time.Now()

	go func(from, to int32, epoch uint64) {
		if b.logger != nil {
			b.logger.Debug("gap detected in %s (from=%d,target=%d)", b.name, from, to)
		}
		b.fetchGap(from, to)
		b.Lock()
		if b.recoveryEpoch == epoch {
			b.recovering = false
		}
		b.Unlock()
	}(prev, target, epoch)
}

func (b *counterBox) runUnlocked(apply func()) {
	b.Unlock()
	defer b.Lock()
	apply()
}

func (c *patternCache) Get(pattern string) (*regexp.Regexp, error) {
	if v, ok := c.cache.Load(pattern); ok {
		if reg, ok := v.(*regexp.Regexp); ok {
			return reg, nil
		}
	}

	reg, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern %q: %w", pattern, err)
	}

	c.cache.Store(pattern, reg)
	return reg, nil
}

func applyChain[H any](handler H, middlewares []func(H) H) H {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

func applyMiddlewares(handler MessageHandler, middlewares []Middleware) MessageHandler {
	return applyChain(handler, middlewares)
}

func WithMiddleware(handler MessageHandler, middlewares ...Middleware) MessageHandler {
	return applyChain(handler, middlewares)
}

type MessageHandler func(m *NewMessage) error
type EditHandler func(m *NewMessage) error
type DeleteHandler func(m *DeleteMessage) error
type AlbumHandler func(m *Album) error
type InlineHandler func(m *InlineQuery) error
type InlineSendHandler func(m *InlineSend) error
type GuestChatQueryHandler func(m *GuestChatQuery) error
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
	Handler AlbumHandler
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
	Handler DeleteHandler
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

type guestChatHandle struct {
	baseHandle
	Handler GuestChatQueryHandler
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

	d.Lock()
	delete(d.activeAlbums, a.groupedId)
	albumHandles := make(map[int][]*albumHandle, len(d.albumHandles))
	for k, v := range d.albumHandles {
		albumHandles[k] = append([]*albumHandle(nil), v...)
	}
	d.Unlock()

	a.Lock()
	sortedMessages := append([]*NewMessage(nil), a.messages...)
	a.Unlock()
	sort.SliceStable(sortedMessages, func(i, j int) bool {
		return sortedMessages[i].ID < sortedMessages[j].ID
	})

	for gp, handlers := range albumHandles {
		endGroup := false
		for _, handler := range handlers {
			handle := func(h *albumHandle) error {
				msgsCopy := append([]*NewMessage(nil), sortedMessages...)
				hf := h.Handler
				if mm := c.dispatcher.middlewareManager; mm != nil {
					hf = applyChain(hf, mm.albums())
				}
				return hf(&Album{
					GroupedID: a.groupedId,
					Messages:  msgsCopy,
					Client:    c,
				})
			}

			if gp == DefaultGroup {
				go func(h *albumHandle) {
					defer c.NewRecovery()()
					err := handle(h)
					if err != nil {
						if errors.Is(err, ErrEndGroup) {
							return
						}
						c.Log.WithError(err).Error("[AlbumHandler]")
					}
				}(handler)
			} else {
				var err error
				func(h *albumHandle) {
					defer c.NewRecovery()()
					err = handle(h)
				}(handler)
				if err != nil && errors.Is(err, ErrEndGroup) {
					endGroup = true
					break
				}
			}
		}
		if endGroup {
			continue
		}
	}
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
	guestChatHandles      map[int][]*guestChatHandle
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
	nextUpdatesDeadlineNs atomic.Int64
	lastUpdateTimeNano    atomic.Int64
	state                 UpdateState
	channelStates         map[int64]*channelState
	processedMsgLRU       *shardedLRU
	recoveringDifference  bool
	recoveringChannels    map[int64]bool
	stopChan              chan struct{}
	stopMu                sync.Mutex
	patternCache          *patternCache
	middlewareManager     *middlewareManager
	globalPtsBox          *counterBox
	globalQtsBox          *counterBox
	channelPtsBoxes       sync.Map
	channelGapFetcher     func(channelID int64, from, target int32)
}

func isChannelAccessError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, marker := range []string{"CHANNEL_PRIVATE", "CHANNEL_INVALID", "CHANNEL_PUBLIC_GROUP_NA", "USER_BANNED_IN_CHANNEL", "USER_KICKED", "CHAT_ADMIN_REQUIRED"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func (d *UpdateDispatcher) cleanupChannel(channelID int64) {
	d.channelPtsBoxes.Delete(channelID)
	d.Lock()
	delete(d.channelStates, channelID)
	if d.openChats != nil {
		if oc, ok := d.openChats[channelID]; ok {
			if oc.closeChan != nil {
				select {
				case <-oc.closeChan:
				default:
					close(oc.closeChan)
				}
			}
			delete(d.openChats, channelID)
		}
	}
	if d.recoveringChannels != nil {
		delete(d.recoveringChannels, channelID)
	}
	d.Unlock()
}

func (d *UpdateDispatcher) SetPts(pts int32) {
	if d.globalPtsBox != nil {
		d.globalPtsBox.forceSet(pts)
		return
	}
	d.Lock()
	if pts >= d.state.Pts {
		d.state.Pts = pts
	}
	d.Unlock()
}

func (d *UpdateDispatcher) GetPts() int32 {
	if d.globalPtsBox != nil {
		return d.globalPtsBox.currentValue()
	}
	d.RLock()
	defer d.RUnlock()
	return d.state.Pts
}

func (d *UpdateDispatcher) SetQts(qts int32) {
	if d.globalQtsBox != nil {
		d.globalQtsBox.forceSet(qts)
		return
	}
	d.Lock()
	if qts >= d.state.Qts {
		d.state.Qts = qts
	}
	d.Unlock()
}

func (d *UpdateDispatcher) GetQts() int32 {
	if d.globalQtsBox != nil {
		return d.globalQtsBox.currentValue()
	}
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
	if date <= 0 {
		return
	}
	d.Lock()
	defer d.Unlock()
	if date > d.state.Date {
		d.state.Date = date
	}
}

func (d *UpdateDispatcher) GetDate() int32 {
	d.RLock()
	defer d.RUnlock()
	return d.state.Date
}

func (d *UpdateDispatcher) SetChannelPts(channelID int64, pts int32) {
	if box := d.getChannelBox(channelID); box != nil {
		box.forceSet(pts)
	}

	d.Lock()
	if d.channelStates == nil {
		d.channelStates = make(map[int64]*channelState)
	}
	if state, ok := d.channelStates[channelID]; ok {
		state.pts = pts
	} else {
		d.channelStates[channelID] = &channelState{pts: pts}
	}
	d.Unlock()
}

func (d *UpdateDispatcher) GetChannelPts(channelID int64) int32 {
	if box := d.getChannelBox(channelID); box != nil {
		return box.currentValue()
	}
	return 0
}

func (d *UpdateDispatcher) getChannelBox(channelID int64) *counterBox {
	if v, ok := d.channelPtsBoxes.Load(channelID); ok {
		return v.(*counterBox)
	}
	if d.channelGapFetcher == nil {
		return nil
	}

	box := newCounterBox(fmt.Sprintf("channel:%d", channelID), d.logger, func(from, target int32) {
		d.channelGapFetcher(channelID, from, target)
	}, func(val int32) {
		d.Lock()
		if d.channelStates == nil {
			d.channelStates = make(map[int64]*channelState)
		}
		if st, ok := d.channelStates[channelID]; ok {
			st.pts = val
		} else {
			d.channelStates[channelID] = &channelState{pts: val}
		}
		d.Unlock()
	})

	d.RLock()
	if state, ok := d.channelStates[channelID]; ok {
		box.current = state.pts
	}
	d.RUnlock()

	if actual, loaded := d.channelPtsBoxes.LoadOrStore(channelID, box); loaded {
		return actual.(*counterBox)
	}
	return box
}

func (u *UpdateDispatcher) UpdateLastUpdateTime() {
	u.lastUpdateTimeNano.Store(time.Now().UnixNano())
}

func (u *UpdateDispatcher) getLastUpdateTime() time.Time {
	return time.Unix(0, u.lastUpdateTimeNano.Load())
}

func (d *UpdateDispatcher) TryMarkMessageProcessed(key int64) bool {
	if d.processedMsgLRU == nil {
		return true
	}
	return d.processedMsgLRU.TryAdd(key)
}

// tryMarkUpdate deduplicates an Update at the dispatcher entry, before any
// handler fires. Returns false when the same update has already been
// dispatched recently. Uses the same processedMsgLRU for compactness.
func (d *UpdateDispatcher) tryMarkUpdate(update Update) bool {
	if d.processedMsgLRU == nil {
		return true
	}
	key := updateDedupeKey(update)
	if key == 0 {
		// No stable identity — cannot dedup safely, allow through.
		return true
	}
	return d.processedMsgLRU.TryAdd(key)
}

// updateDedupeKey returns a stable 64-bit identity for an Update, or 0 if
// no stable key can be computed. Keys are namespaced so the raw-handler
// LRU does not collide with the message-handler LRU (both share the same
// LRU pool but keys are drawn from disjoint ranges).
func updateDedupeKey(update Update) int64 {
	const rawTag = int64(0x7261775f << 32) // "raw_" prefix in the high 32 bits
	switch u := update.(type) {
	case *UpdateNewMessage:
		if m, ok := u.Message.(*MessageObj); ok {
			return rawTag ^ messageDedupeKey(m, false)
		}
		if m, ok := u.Message.(*MessageService); ok {
			return rawTag ^ serviceMessageDedupeKey(m)
		}
	case *UpdateNewChannelMessage:
		if m, ok := u.Message.(*MessageObj); ok {
			return rawTag ^ messageDedupeKey(m, false)
		}
		if m, ok := u.Message.(*MessageService); ok {
			return rawTag ^ serviceMessageDedupeKey(m)
		}
	case *UpdateEditMessage:
		if m, ok := u.Message.(*MessageObj); ok {
			return rawTag ^ messageDedupeKey(m, true)
		}
	case *UpdateEditChannelMessage:
		if m, ok := u.Message.(*MessageObj); ok {
			return rawTag ^ messageDedupeKey(m, true)
		}
	case *UpdateDeleteMessages:
		return rawTag ^ hashDedupeFields(0x100, int64(u.Pts), false)
	case *UpdateDeleteChannelMessages:
		return rawTag ^ hashDedupeFields(u.ChannelID, int64(u.Pts), false)
	case *UpdateBotCallbackQuery:
		return rawTag ^ hashDedupeFields(0x200, int64(u.QueryID), false)
	case *UpdateInlineBotCallbackQuery:
		return rawTag ^ hashDedupeFields(0x201, int64(u.QueryID), false)
	case *UpdateBotInlineQuery:
		return rawTag ^ hashDedupeFields(0x202, int64(u.QueryID), false)
	case *UpdateBotInlineSend:
		return rawTag ^ hashDedupeFields(0x203, int64(u.UserID)^int64(len(u.Query)), false)
	case *UpdateChannelParticipant:
		return rawTag ^ hashDedupeFields(u.ChannelID, int64(u.Qts), false)
	case *UpdateChatParticipant:
		return rawTag ^ hashDedupeFields(u.ChatID, int64(u.Qts), false)
	case *UpdatePendingJoinRequests:
		return rawTag ^ hashDedupeFields(messagePeerKey(u.Peer), int64(u.RequestsPending), false)
	case *UpdateBotStopped:
		return rawTag ^ (0x300 << 40) ^ hashDedupeFields(int64(u.UserID), int64(u.Qts), false)
	case *UpdateBotChatBoost:
		return rawTag ^ (0x301 << 40) ^ hashDedupeFields(messagePeerKey(u.Peer), int64(u.Qts), false)
	case *UpdateBotMessageReaction:
		return rawTag ^ (0x302 << 40) ^ hashDedupeFields(messagePeerKey(u.Peer)^int64(u.MsgID), int64(u.Qts), false)
	case *UpdateBotMessageReactions:
		return rawTag ^ (0x303 << 40) ^ hashDedupeFields(messagePeerKey(u.Peer)^int64(u.MsgID), int64(u.Qts), false)
	case *UpdateBotBusinessConnect:
		return rawTag ^ (0x304 << 40) ^ hashDedupeFields(0, int64(u.Qts), false)
	case *UpdateBotNewBusinessMessage:
		return rawTag ^ (0x305 << 40) ^ hashDedupeFields(0, int64(u.Qts), false)
	case *UpdateBotEditBusinessMessage:
		return rawTag ^ (0x306 << 40) ^ hashDedupeFields(0, int64(u.Qts), false)
	case *UpdateBotDeleteBusinessMessage:
		return rawTag ^ (0x307 << 40) ^ hashDedupeFields(0, int64(u.Qts), false)
	case *UpdateBotPurchasedPaidMedia:
		return rawTag ^ (0x308 << 40) ^ hashDedupeFields(u.UserID, int64(u.Qts), false)
	case *UpdateBotStarsSubscription:
		return rawTag ^ (0x309 << 40) ^ hashDedupeFields(u.UserID, int64(u.Qts), false)
	case *UpdateManagedBot:
		return rawTag ^ (0x30A << 40) ^ hashDedupeFields(u.BotID, int64(u.Qts), false)
	case *UpdateMessagePollVote:
		return rawTag ^ (0x30B << 40) ^ hashDedupeFields(messagePeerKey(u.Peer), int64(u.Qts), false)
	case *UpdateBusinessBotCallbackQuery:
		return rawTag ^ (0x30C << 40) ^ hashDedupeFields(0, int64(u.QueryID), false)
	case *UpdateMessageReactions:
		return rawTag ^ (0x30D << 40) ^ hashDedupeFields(messagePeerKey(u.Peer), int64(u.MsgID), false)
	case *UpdateStory:
		return rawTag ^ (0x30E << 40) ^ hashDedupeFields(messagePeerKey(u.Peer), 0, false)
	case *UpdateStoryID:
		return rawTag ^ (0x30F << 40) ^ hashDedupeFields(u.RandomID, int64(u.ID), false)
	case *UpdateReadStories:
		return rawTag ^ (0x310 << 40) ^ hashDedupeFields(messagePeerKey(u.Peer), int64(u.MaxID), false)
	case *UpdatePhoneCall:
		return rawTag ^ (0x311 << 40) ^ hashDedupeFields(phoneCallID(u.PhoneCall), 0, false)
	}
	return 0
}

func phoneCallID(pc PhoneCall) int64 {
	switch v := pc.(type) {
	case *PhoneCallObj:
		return v.ID
	case *PhoneCallAccepted:
		return v.ID
	case *PhoneCallWaiting:
		return v.ID
	case *PhoneCallRequested:
		return v.ID
	case *PhoneCallDiscarded:
		return v.ID
	case *PhoneCallEmpty:
		return v.ID
	}
	return 0
}

func (c *Client) NewUpdateDispatcher(sessionName ...string) {
	d := &UpdateDispatcher{
		logger:                c.Log.WithPrefix("gogram " + lp("updates", getVariadic(sessionName, ""))),
		channelStates:         make(map[int64]*channelState),
		processedMsgLRU:       newShardedLRU(200000, 32),
		stopChan:              make(chan struct{}),
		messageHandles:        make(map[int][]*messageHandle),
		inlineHandles:         make(map[int][]*inlineHandle),
		inlineSendHandles:     make(map[int][]*inlineSendHandle),
		guestChatHandles:      make(map[int][]*guestChatHandle),
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
		channelGapFetcher: func(channelID int64, from, target int32) {
			if c.clientData.disableGapFetch {
				return
			}
			c.FetchChannelDifference(channelID, from, 50)
		},
	}

	d.globalPtsBox = newCounterBox("pts", d.logger, func(from, target int32) {
		if c.clientData.disableGapFetch {
			return
		}
		c.FetchDifference(from, 5000)
	}, func(val int32) {
		d.Lock()
		d.state.Pts = val
		d.Unlock()
	})

	d.globalQtsBox = newCounterBox("qts", d.logger, func(from, target int32) {
		if c.clientData.disableGapFetch {
			return
		}
		c.FetchDifference(from, 5000)
	}, func(val int32) {
		d.Lock()
		d.state.Qts = val
		d.Unlock()
	})

	c.dispatcher = d
	c.dispatcher.lastUpdateTimeNano.Store(time.Now().UnixNano())
	c.dispatcher.logger.Debug("update dispatcher initialized")

	if c.MTProto != nil {
		c.MTProto.SetOnNewSessionCreated(func() {
			if c.clientData.disableGapFetch {
				return
			}
			pts := c.dispatcher.GetPts()
			c.dispatcher.logger.Debug("NewSessionCreated observed, re-syncing update state from pts=%d", pts)
			c.FetchDifference(pts, 5000)
		})
	}

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
	case *guestChatHandle:
		removeHandleFromMap(h, c.dispatcher.guestChatHandles)
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
				handlesMap[key] = removeHandleCoW(handles, targetID)
				return
			}
		}
	}
}

var (
	updateTypeIDsByReflect sync.Map
	updateTypeIDMu         sync.RWMutex
	nextTypeIDValue        uint32 = 1
)

func getUpdateTypeID(update Update) uint32 {
	if update == nil {
		return 0
	}
	rt := reflect.TypeOf(update)
	if v, ok := updateTypeIDsByReflect.Load(rt); ok {
		return v.(uint32)
	}
	updateTypeIDMu.Lock()
	defer updateTypeIDMu.Unlock()
	if v, ok := updateTypeIDsByReflect.Load(rt); ok {
		return v.(uint32)
	}
	id := nextTypeIDValue
	nextTypeIDValue++
	updateTypeIDsByReflect.Store(rt, id)
	return id
}

// ---------------------------- Handle Functions ----------------------------

func (c *Client) handleMessageUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		// Compute the dedup key BEFORE any mutation. FromID can be
		// enriched for outgoing messages below, but the identity of
		// the update is (peer, id, out) — enrichment must not change
		// what we consider "the same update."
		if !c.dispatcher.TryMarkMessageProcessed(messageDedupeKey(msg, false)) {
			return
		}
		if msg.Out {
			if msg.FromID == nil {
				if me := c.Me(); me != nil {
					msg.FromID = &PeerUser{UserID: me.ID}
				}
			}
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
		if msg.Out {
			return
		}
		if !c.dispatcher.TryMarkMessageProcessed(serviceMessageDedupeKey(msg)) {
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
					hf := h.Handler
					if mm := c.dispatcher.middlewareManager; mm != nil {
						hf = applyChain(hf, mm.GetGlobal())
					}
					return hf(packed)
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

	c.dispatcher.Lock()
	if c.dispatcher.activeAlbums == nil {
		c.dispatcher.activeAlbums = make(map[int64]*albumBox)
	}
	// Double-checked insert: another goroutine may have created the box
	// between our earlier RLock and this Lock.
	if group, ok := c.dispatcher.activeAlbums[message.GroupedID]; ok {
		c.dispatcher.Unlock()
		group.Add(packed)
		return
	}
	albBox := &albumBox{
		messages:  []*NewMessage{packed},
		groupedId: message.GroupedID,
	}
	c.dispatcher.activeAlbums[message.GroupedID] = albBox
	c.dispatcher.Unlock()
	albBox.WaitAndTrigger(c.dispatcher, c)
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
		} else {
			c.handleMessageUpdate(msg)
		}
	}
}

func (c *Client) fetchChannelPeersBeforeUpdate(m Message, pts int32) {
	msg, ok := m.(*MessageObj)
	if !ok {
		c.handleMessageUpdate(m)
		return
	}
	peerCached := c.IdInCache(c.GetPeerID(msg.PeerID))
	senderCached := msg.FromID == nil || c.IdInCache(c.GetPeerID(msg.FromID))
	if peerCached && senderCached {
		c.handleMessageUpdate(msg)
		return
	}
	if peer, ok := msg.PeerID.(*PeerChannel); ok && pts > 0 {
		currentPts := c.dispatcher.GetChannelPts(peer.ChannelID)
		if currentPts == 0 || pts-1 < currentPts {
			currentPts = pts - 1
		}
		c.FetchChannelDifference(peer.ChannelID, currentPts, 10)
	}
	c.handleMessageUpdate(msg)
}

func (c *Client) handleEditUpdate(update Message) {
	if msg, ok := update.(*MessageObj); ok {
		if msg.Out {
			if msg.FromID == nil {
				if me := c.Me(); me != nil {
					msg.FromID = &PeerUser{UserID: me.ID}
				}
			}
		}
		if !c.dispatcher.TryMarkMessageProcessed(messageDedupeKey(msg, true)) {
			return
		}
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

							hf := EditHandler(h.Handler)
							if mm := c.dispatcher.middlewareManager; mm != nil {
								hf = applyChain(hf, mm.edits())
							}

							err := hf(packed)
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
	// Dedup callbacks by QueryID — Telegram guarantees uniqueness.
	if !c.dispatcher.TryMarkMessageProcessed(hashDedupeFields(0x100200, int64(update.QueryID), false)) {
		return
	}
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
						hf := h.Handler
						if mm := c.dispatcher.middlewareManager; mm != nil {
							hf = applyChain(hf, mm.callbacks())
						}
						err := hf(packed)
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
	if !c.dispatcher.TryMarkMessageProcessed(hashDedupeFields(0x100201, int64(update.QueryID), false)) {
		return
	}
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
					hf := h.Handler
					if mm := c.dispatcher.middlewareManager; mm != nil {
						hf = applyChain(hf, mm.inlineCallbacks())
					}
					return hf(packed)
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
				hf := h.Handler
				if mm := c.dispatcher.middlewareManager; mm != nil {
					hf = applyChain(hf, mm.participants())
				}
				return hf(packed)
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
	if !c.dispatcher.TryMarkMessageProcessed(hashDedupeFields(0x100202, int64(update.QueryID), false)) {
		return
	}
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
					hf := h.Handler
					if mm := c.dispatcher.middlewareManager; mm != nil {
						hf = applyChain(hf, mm.inlines())
					}
					return hf(packed)
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
				hf := h.Handler
				if mm := c.dispatcher.middlewareManager; mm != nil {
					hf = applyChain(hf, mm.inlineSends())
				}
				return hf(packed)
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

func (c *Client) handleGuestChatUpdate(update *UpdateBotGuestChatQuery) {
	packed := packGuestChatQuery(c, update)

	c.dispatcher.RLock()
	guestChatHandles := make(map[int][]*guestChatHandle)
	maps.Copy(guestChatHandles, c.dispatcher.guestChatHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range guestChatHandles {
		for _, handler := range handlers {
			handle := func(h *guestChatHandle) error {
				defer c.NewRecovery()()
				hf := h.Handler
				if mm := c.dispatcher.middlewareManager; mm != nil {
					hf = applyChain(hf, mm.guestChats())
				}
				return hf(packed)
			}

			if group == DefaultGroup {
				go func() {
					err := handle(handler)
					if err != nil {
						if errors.Is(err, ErrEndGroup) {
							return
						}
						c.Log.WithError(err).Error("[GuestChatQueryHandler]")
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
				hf := h.Handler
				if mm := c.dispatcher.middlewareManager; mm != nil {
					hf = applyChain(hf, mm.deletes())
				}
				return hf(packed)
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
				hf := h.Handler
				if mm := c.dispatcher.middlewareManager; mm != nil {
					hf = applyChain(hf, mm.joinRequests())
				}
				return hf(packed)
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
	if len(c.dispatcher.rawHandles) == 0 {
		c.dispatcher.RUnlock()
		return
	}
	rawHandles := make(map[int][]*rawHandle, len(c.dispatcher.rawHandles))
	maps.Copy(rawHandles, c.dispatcher.rawHandles)
	c.dispatcher.RUnlock()

	if !c.dispatcher.tryMarkUpdate(update) {
		return
	}
	updateTypeID := getUpdateTypeID(update)

	for group, handlers := range rawHandles {
		for _, handler := range handlers {
			if handler == nil || handler.Handler == nil {
				continue
			}
			if handler.updateTypeID == updateTypeID || handler.updateTypeID == 0 {
				handle := func(h *rawHandle) error {
					defer c.NewRecovery()()
					hf := h.Handler
					if mm := c.dispatcher.middlewareManager; mm != nil {
						hf = applyChain(hf, mm.raws())
					}
					return hf(update, c)
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
	if h == nil || h.Pattern == nil {
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
	return containsFilter(h.Filters, IsOutgoing)
}

func containsFilter(filters []Filter, target Filter) bool {
	for _, f := range filters {
		if f == target {
			return true
		}
		switch ff := f.(type) {
		case anyFilter:
			if containsFilter([]Filter(ff), target) {
				return true
			}
		case allFilter:
			if containsFilter([]Filter(ff), target) {
				return true
			}
		case notFilter:
			if containsFilter([]Filter{ff.f}, target) {
				return true
			}
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

// Filter is an interface that checks whether a message or callback query matches certain criteria.
type Filter interface {
	Check(m *NewMessage) bool
	CheckCallback(c *CallbackQuery) bool
}

// funcFilter is a simple filter implementation using functions.
// We use a pointer type to make filter instances comparable by identity.
type funcFilter struct {
	check   func(*NewMessage) bool
	checkCb func(*CallbackQuery) bool
}

func (f *funcFilter) Check(m *NewMessage) bool {
	if f.check != nil {
		return f.check(m)
	}
	return true
}

func (f *funcFilter) CheckCallback(c *CallbackQuery) bool {
	if f.checkCb != nil {
		return f.checkCb(c)
	}
	return true
}

var (
	// IsPrivate matches messages from private (1-on-1) chats.
	IsPrivate Filter = &funcFilter{
		check:   func(m *NewMessage) bool { return m.IsPrivate() },
		checkCb: func(c *CallbackQuery) bool { return c.IsPrivate() },
	}
	// IsGroup matches messages from group chats.
	IsGroup Filter = &funcFilter{
		check:   func(m *NewMessage) bool { return m.IsGroup() },
		checkCb: func(c *CallbackQuery) bool { return c.IsGroup() },
	}
	// IsChannel matches messages from channels.
	IsChannel Filter = &funcFilter{
		check:   func(m *NewMessage) bool { return m.IsChannel() },
		checkCb: func(c *CallbackQuery) bool { return c.IsChannel() },
	}
)

var (
	// IsCommand matches messages that contain a bot command.
	IsCommand Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.IsCommand() },
	}
	// IsReply matches messages that are replies to another message.
	IsReply Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.IsReply() },
	}
	// IsForward matches forwarded messages.
	IsForward Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.IsForward() },
	}
	// IsOutgoing matches outgoing messages (sent by the current user).
	IsOutgoing Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Message != nil && m.Message.Out },
	}
	// IsIncoming matches incoming messages (not sent by the current user).
	IsIncoming Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Message != nil && !m.Message.Out },
	}
	// IsEdited matches messages that have been edited.
	IsEdited Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Message != nil && m.Message.EditDate != 0 },
	}
	// IsText matches messages that have non-empty text.
	IsText Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Text() != "" },
	}
)

var (
	// FromBot matches messages sent by a bot.
	FromBot Filter = &funcFilter{
		check:   func(m *NewMessage) bool { return m.Sender != nil && m.Sender.Bot },
		checkCb: func(c *CallbackQuery) bool { return c.Sender != nil && c.Sender.Bot },
	}
	// HasMention matches messages where the current user is mentioned.
	HasMention Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Message != nil && m.Message.Mentioned },
	}
)

var (
	// HasMedia matches messages that contain any media.
	HasMedia Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.IsMedia() },
	}
	// HasPhoto matches messages that contain a photo.
	HasPhoto Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Photo() != nil },
	}
	// HasVideo matches messages that contain a video.
	HasVideo Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Video() != nil },
	}
	// HasDocument matches messages that contain a document.
	HasDocument Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Document() != nil },
	}
	// HasAudio matches messages that contain an audio file.
	HasAudio Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Audio() != nil },
	}
	// HasSticker matches messages that contain a sticker.
	HasSticker Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Sticker() != nil },
	}
	// HasAnimation matches messages that contain a GIF/animation.
	HasAnimation Filter = &funcFilter{
		check: func(m *NewMessage) bool {
			if doc := m.Document(); doc != nil {
				for _, attr := range doc.Attributes {
					if _, ok := attr.(*DocumentAttributeAnimated); ok {
						return true
					}
				}
			}
			return false
		},
	}
	// HasVoice matches messages that contain a voice message.
	HasVoice Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Voice() != nil },
	}
	// HasVideoNote matches messages that contain a video note (round video).
	HasVideoNote Filter = &funcFilter{
		check: func(m *NewMessage) bool {
			if doc := m.Document(); doc != nil {
				for _, attr := range doc.Attributes {
					if v, ok := attr.(*DocumentAttributeVideo); ok && v.RoundMessage {
						return true
					}
				}
			}
			return false
		},
	}
	// HasContact matches messages that contain a contact.
	HasContact Filter = &funcFilter{
		check: func(m *NewMessage) bool { return m.Contact() != nil },
	}
	// HasLocation matches messages that contain a geo location.
	HasLocation Filter = &funcFilter{
		check: func(m *NewMessage) bool {
			_, ok := m.Media().(*MessageMediaGeo)
			return ok
		},
	}
	// HasVenue matches messages that contain a venue.
	HasVenue Filter = &funcFilter{
		check: func(m *NewMessage) bool {
			_, ok := m.Media().(*MessageMediaVenue)
			return ok
		},
	}
	// HasPoll matches messages that contain a poll.
	HasPoll Filter = &funcFilter{
		check: func(m *NewMessage) bool {
			_, ok := m.Media().(*MessageMediaPoll)
			return ok
		},
	}
)

type userFilter struct {
	users []int64
}

func (f userFilter) Check(m *NewMessage) bool {
	return slices.Contains(f.users, m.SenderID())
}

func (f userFilter) CheckCallback(c *CallbackQuery) bool {
	return slices.Contains(f.users, c.SenderID)
}

type chatFilter struct {
	chats []int64
}

func (f chatFilter) Check(m *NewMessage) bool {
	return slices.Contains(f.chats, m.ChatID())
}

func (f chatFilter) CheckCallback(c *CallbackQuery) bool {
	return slices.Contains(f.chats, c.ChatID)
}

type customFilter struct {
	fn func(*NewMessage) bool
}

func (f customFilter) Check(m *NewMessage) bool {
	return f.fn(m)
}

func (f customFilter) CheckCallback(c *CallbackQuery) bool { return true }

type customCallbackFilter struct {
	fn func(*CallbackQuery) bool
}

func (f customCallbackFilter) Check(m *NewMessage) bool { return true }

func (f customCallbackFilter) CheckCallback(c *CallbackQuery) bool {
	return f.fn(c)
}

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

type notFilter struct {
	f Filter
}

func (n notFilter) Check(m *NewMessage) bool {
	return !n.f.Check(m)
}

func (n notFilter) CheckCallback(c *CallbackQuery) bool {
	return !n.f.CheckCallback(c)
}

// Any creates a filter that matches if any of the provided filters match (OR logic).
func Any(fs ...Filter) Filter {
	return anyFilter(fs)
}

// All creates a filter that matches only if all provided filters match (AND logic).
func All(fs ...Filter) Filter {
	return allFilter(fs)
}

// Not creates a filter that negates the provided filter.
func Not(f Filter) Filter {
	return notFilter{f: f}
}

// FromUsers creates a filter that matches messages from specific user IDs.
func FromUsers(ids ...int64) Filter { return userFilter{users: ids} }

// FromChats creates a filter that matches messages from specific chat/channel IDs.
func FromChats(ids ...int64) Filter { return chatFilter{chats: ids} }

// TextMinLen creates a filter that matches messages with text length >= n.
func TextMinLen(n int) Filter { return lengthFilter{min: n} }

// TextMaxLen creates a filter that matches messages with text length <= n.
func TextMaxLen(n int) Filter { return lengthFilter{max: n} }

// Custom creates a filter with a custom check function for messages.
func CustomFilter(fn func(*NewMessage) bool) Filter { return customFilter{fn: fn} }

// CustomCallback creates a filter with a custom check function for callback queries.
func CustomCallback(fn func(*CallbackQuery) bool) Filter { return customCallbackFilter{fn: fn} }

var (
	FilterPrivate   = IsPrivate
	FilterGroup     = IsGroup
	FilterChannel   = IsChannel
	FilterMedia     = HasMedia
	FilterCommand   = IsCommand
	FilterReply     = IsReply
	FilterForward   = IsForward
	FilterFromBot   = FromBot
	FilterMention   = HasMention
	FilterOutgoing  = IsOutgoing
	FilterIncoming  = IsIncoming
	FilterEdited    = IsEdited
	FilterPhoto     = HasPhoto
	FilterVideo     = HasVideo
	FilterDocument  = HasDocument
	FilterAudio     = HasAudio
	FilterSticker   = HasSticker
	FilterAnimation = HasAnimation
	FilterVoice     = HasVoice
	FilterVideoNote = HasVideoNote
	FilterContact   = HasContact
	FilterLocation  = HasLocation
	FilterVenue     = HasVenue
	FilterPoll      = HasPoll
	FilterText      = IsText

	// Deprecated: Use FromUsers instead.
	FromUser = FromUsers
	// Deprecated: Use FromChats instead.
	InChat = FromChats
)

func insertHandleCoW[T any](handlers []T, at int, handle T) []T {
	out := make([]T, 0, len(handlers)+1)
	out = append(out, handlers[:at]...)
	out = append(out, handle)
	out = append(out, handlers[at:]...)
	return out
}

func removeHandleCoW[T handleWithID](handlers []T, id uint64) []T {
	for i := range handlers {
		if handlers[i].getID() == id {
			out := make([]T, 0, len(handlers)-1)
			out = append(out, handlers[:i]...)
			out = append(out, handlers[i+1:]...)
			return out
		}
	}
	return handlers
}

func addHandleToMap[T Handle](handleMap map[int][]T, handle T) T {
	group := handle.GetGroup()

	handlers := handleMap[group]
	inserted := false
	for i, h := range handlers {
		if handle.GetPriority() > h.GetPriority() {
			handleMap[group] = insertHandleCoW(handlers, i, handle)
			inserted = true
			break
		}
	}

	if !inserted {
		grown := make([]T, len(handlers)+1)
		copy(grown, handlers)
		grown[len(handlers)] = handle
		handleMap[group] = grown
	}

	return handleMap[group][len(handleMap[group])-1]
}

func makePriorityChangeCallback[T handleWithID](handleMap map[int][]T, handle T, handleID uint64, getGroup func() int, getPriority func() int, mu *sync.RWMutex) func() {
	return func() {
		mu.Lock()
		defer mu.Unlock()
		group := getGroup()
		handleMap[group] = removeHandleCoW(handleMap[group], handleID)

		handlers := handleMap[group]
		inserted := false
		myPriority := getPriority()
		for i := range handlers {
			if myPriority > handlers[i].getPriority() {
				handleMap[group] = insertHandleCoW(handlers, i, handle)
				inserted = true
				break
			}
		}

		if !inserted {
			grown := make([]T, len(handlers)+1)
			copy(grown, handlers)
			grown[len(handlers)] = handle
			handleMap[group] = grown
		}
	}
}

func makeGroupChangeCallback[T handleWithID](handleMap map[int][]T, handle T, handleID uint64, mu *sync.RWMutex) func(int, int) {
	return func(oldGroup, newGroup int) {
		mu.Lock()
		defer mu.Unlock()
		if _, ok := handleMap[oldGroup]; ok {
			handleMap[oldGroup] = removeHandleCoW(handleMap[oldGroup], handleID)
		}
		existing := handleMap[newGroup]
		grown := make([]T, len(existing)+1)
		copy(grown, existing)
		grown[len(existing)] = handle
		handleMap[newGroup] = grown
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

func (c *Client) AddGuestChatHandler(handler GuestChatQueryHandler) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	handleID := nextHandleID()
	h := &guestChatHandle{
		Handler:    handler,
		baseHandle: baseHandle{id: handleID, Group: DefaultGroup},
	}
	h.onGroupChanged = makeGroupChangeCallback(c.dispatcher.guestChatHandles, h, handleID, &c.dispatcher.RWMutex)
	h.onPriorityChanged = makePriorityChangeCallback(c.dispatcher.guestChatHandles, h, handleID, h.GetGroup, h.GetPriority, &c.dispatcher.RWMutex)
	return addHandleToMap(c.dispatcher.guestChatHandles, h)
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

// AddRawHandler registers a handler for raw Update objects.
//
// This does NOT deliver the raw MTProto container stream. Updates reach the
// handler only after passing gogram's internal pipeline:
//
//	HandleIncomingUpdates → applyIncomingUpdate → processWithState →
//	counterBox (pts/qts gap tracking) → dispatchUpdate → handleRawUpdate
//
// The gap-tracking layer buffers updates whose pts is non-contiguous with
// the last acknowledged pts until the gap resolves via updates.getDifference.
// For normal clients this is invisible and desirable — it guarantees ordered,
// gap-free updates. For proxy layers, Bot-API adapters, or callers building
// their own state tracking, it can introduce buffering delays or (rarely)
// stalls if a gap fetch cannot resolve while other RPC traffic is heavy.
//
// If you want raw updates without gap tracking, the recommended way is:
//
//	client, _ := telegram.NewClient(telegram.ClientConfig{
//	    ..., RawUpdates: true,
//	})
//	client.OnRaw(nil, func(upd telegram.Update, c *telegram.Client) error {
//	    // fires immediately, no pts/qts buffering, and also fires for
//	    // updates piggybacking on RPC responses (e.g. messages.sendMessage)
//	    return nil
//	})
//
// For the deeper escape hatch (skip the friendly dispatcher entirely and
// tap the raw MTProto container stream), see [UnpackContainer] and
// [MTProto.AddCustomServerRequestHandler] / [MTProto.AddRPCResponseHandler].
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

// UnpackContainer flattens a raw MTProto update container into a slice of
// individual Update objects. It handles all top-level container types the
// server sends on the update stream: UpdatesObj, UpdatesCombined,
// UpdateShort, UpdateShortMessage, UpdateShortChatMessage, and
// UpdateShortSentMessage. Unknown or non-container values yield nil.
//
// This is the helper for the escape-hatch pattern described on
// [Client.AddRawHandler]: register a custom server-request handler via
// c.MTProto.AddCustomServerRequestHandler and call UnpackContainer on the
// argument to get individual Updates without going through gogram's
// pts/qts gap-tracking dispatcher.
//
//	client.MTProto.AddCustomServerRequestHandler(func(u any) bool {
//	    for _, upd := range telegram.UnpackContainer(u) {
//	        // ... deliver upd directly, no gap tracking, no buffering
//	    }
//	    return false
//	})
//
// The short-message variants (UpdateShortMessage / UpdateShortChatMessage /
// UpdateShortSentMessage) are expanded into synthetic UpdateNewMessage
// objects mirroring what the server would have sent inside an UpdatesObj,
// so downstream code can treat every element uniformly.
func UnpackContainer(u any) []Update {
	switch upd := u.(type) {
	case *UpdatesObj:
		return upd.Updates
	case *UpdatesCombined:
		return upd.Updates
	case *UpdateShort:
		return []Update{upd.Update}
	case *UpdateShortMessage:
		msg := &MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.UserID), PeerID: getPeerUser(upd.UserID), Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}
		return []Update{&UpdateNewMessage{Message: msg, Pts: upd.Pts, PtsCount: upd.PtsCount}}
	case *UpdateShortChatMessage:
		msg := &MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.FromID), PeerID: &PeerChat{ChatID: upd.ChatID}, Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}
		return []Update{&UpdateNewMessage{Message: msg, Pts: upd.Pts, PtsCount: upd.PtsCount}}
	case *UpdateShortSentMessage:
		msg := &MessageObj{ID: upd.ID, Out: upd.Out, Date: upd.Date, Media: upd.Media, Entities: upd.Entities, TtlPeriod: upd.TtlPeriod}
		return []Update{&UpdateNewMessage{Message: msg, Pts: upd.Pts, PtsCount: upd.PtsCount}}
	default:
		return nil
	}
}

// HandleIncomingUpdates processes incoming updates and dispatches them to the appropriate handlers.
func HandleIncomingUpdates(u any, c *Client) bool {
	if c == nil {
		return false
	}

	d := c.dispatcher
	if d == nil {
		return false
	}

	// Update last update time for 15-minute timeout monitoring
	d.UpdateLastUpdateTime()
	d.nextUpdatesDeadlineNs.Store(time.Now().Add(time.Minute * 15).UnixNano())

	switch upd := u.(type) {
	case *UpdatesObj:
		if !c.manageSeq(upd.Seq, upd.Seq) {
			return false
		}
		c.dispatcher.SetDate(upd.Date)
		c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
		sortUpdatesByPtsAscending(upd.Updates)
		for _, update := range upd.Updates {
			c.applyIncomingUpdate(update)
		}
		return true
	case *UpdatesCombined:
		if !c.manageSeq(upd.Seq, upd.SeqStart) {
			return false
		}
		c.dispatcher.SetDate(upd.Date)
		c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
		sortUpdatesByPtsAscending(upd.Updates)
		for _, update := range upd.Updates {
			c.applyIncomingUpdate(update)
		}
		return true
	case *UpdateShort:
		c.dispatcher.SetDate(upd.Date)
		c.applyIncomingUpdate(upd.Update)
		return true
	case *UpdateShortMessage:
		c.dispatcher.SetDate(upd.Date)
		other := getPeerUser(upd.UserID)
		var fromID Peer
		if !upd.Out {
			fromID = other
		}
		msg := &MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: fromID, PeerID: other, Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}
		c.applyIncomingUpdate(&UpdateNewMessage{Message: msg, Pts: upd.Pts, PtsCount: upd.PtsCount})
		return true
	case *UpdateShortChatMessage:
		c.dispatcher.SetDate(upd.Date)
		msg := &MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.FromID), PeerID: &PeerChat{ChatID: upd.ChatID}, Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}
		c.applyIncomingUpdate(&UpdateNewMessage{Message: msg, Pts: upd.Pts, PtsCount: upd.PtsCount})
		return true
	case *UpdateShortSentMessage:
		c.dispatcher.SetDate(upd.Date)
		if d.globalPtsBox != nil && upd.Pts != 0 {
			d.globalPtsBox.processCheckpoint(upd.Pts, func() {})
		}
		return true
	case *UpdateChannelTooLong:
		currentPts := d.GetChannelPts(upd.ChannelID)
		if upd.Pts != 0 {
			currentPts = upd.Pts
		}
		go c.FetchChannelDifference(upd.ChannelID, currentPts, 50)
		return true
	case *UpdatesTooLong:
		go c.FetchDifference(d.GetPts(), 5000)
		return true
	default:
		c.Log.Debug("unhandled update type: %T", upd)
		return true
	}
}

func (c *Client) applyIncomingUpdate(update Update) {
	if update == nil {
		return
	}

	meta := extractUpdateMeta(update)
	c.processWithState(meta, func() {
		c.dispatchUpdate(update)
	})
}

func (c *Client) dispatchUpdate(update Update) {
	switch upd := update.(type) {
	case *UpdateNewMessage:
		go c.fetchPeersBeforeUpdate(upd.Message, upd.Pts)
	case *UpdateNewChannelMessage:
		go c.fetchChannelPeersBeforeUpdate(upd.Message, upd.Pts)
	case *UpdateNewScheduledMessage:
		go c.handleMessageUpdate(upd.Message)
	case *UpdateEditMessage:
		go c.handleEditUpdate(upd.Message)
	case *UpdateEditChannelMessage:
		go c.handleEditUpdate(upd.Message)
	case *UpdateDeleteMessages:
		go c.handleDeleteUpdate(upd)
	case *UpdateDeleteChannelMessages:
		go c.handleDeleteUpdate(upd)
	case *UpdateReadHistoryInbox:
	case *UpdateReadHistoryOutbox:
	case *UpdateWebPage:
	case *UpdateReadMessagesContents:
	case *UpdateReadChannelInbox:
	case *UpdateChannelWebPage:
	case *UpdateFolderPeers:
	case *UpdatePinnedMessages:
	case *UpdatePinnedChannelMessages:
		// State-only updates are acknowledged by counter boxes; no handler needed here.
	case *UpdateBotInlineQuery:
		go c.handleInlineUpdate(upd)
	case *UpdateBotCallbackQuery:
		go c.handleCallbackUpdate(upd)
	case *UpdateInlineBotCallbackQuery:
		go c.handleInlineCallbackUpdate(upd)
	case *UpdateChannelParticipant:
		go c.handleParticipantUpdate(upd)
	case *UpdatePendingJoinRequests:
		go c.handleJoinRequestUpdate(upd)
	case *UpdateBotChatInviteRequester:
		go c.handleJoinRequestUpdate(upd)
	case *UpdateBotInlineSend:
		go c.handleInlineSendUpdate(upd)
	case *UpdateBotGuestChatQuery:
		go c.handleGuestChatUpdate(upd)
	case *UpdateChannelTooLong:
		currentPts := c.dispatcher.GetChannelPts(upd.ChannelID)
		if upd.Pts != 0 {
			currentPts = upd.Pts
		}
		go c.FetchChannelDifference(upd.ChannelID, currentPts, 50)
	case *UpdateNewEncryptedMessage:
		go c.HandleSecretChatUpdate(upd)
	case *UpdateEncryption:
		go c.HandleSecretChatUpdate(upd)
	}

	go c.handleRawUpdate(update)
}

func getChannelIDFromMessage(msg Message) int64 {
	if m, ok := msg.(*MessageObj); ok {
		if peer, ok := m.PeerID.(*PeerChannel); ok {
			return peer.ChannelID
		}
	}
	return 0
}

func messagePeerKey(peer Peer) int64 {
	switch p := peer.(type) {
	case *PeerUser:
		// Namespace users into a distinct range so they cannot collide
		// with chats or channels regardless of ID magnitude.
		return p.UserID
	case *PeerChat:
		// Chats: unique tag in the sign+high-bit space.
		return -(p.ChatID | (1 << 62))
	case *PeerChannel:
		// Channels: different tag to avoid ChatID = 2×ChannelID collisions.
		return -(p.ChannelID | (1 << 61))
	default:
		return 0
	}
}

func messageDedupeKey(msg *MessageObj, isEdit bool) int64 {
	if msg == nil {
		return 0
	}
	key := dedupeKeyFromFields(msg.ID, msg.FromID, msg.PeerID, msg.Out)
	if isEdit {
		key ^= int64(msg.EditDate) << 1
		key ^= 0x5f356495
	}
	return key
}

func serviceMessageDedupeKey(msg *MessageService) int64 {
	if msg == nil {
		return 0
	}
	key := dedupeKeyFromFields(msg.ID, msg.FromID, msg.PeerID, msg.Out)
	key ^= 0x73767063
	return key
}

func dedupeKeyFromFields(id int32, fromID Peer, peerID Peer, out bool) int64 {
	// Anchor identity on PeerID (where the message lives) so the key is
	// stable across arrivals that differ in FromID enrichment — the
	// server sometimes omits FromID for outgoing messages, and we later
	// fill it in with Me. Two arrivals of the same event must produce
	// the same dedup key regardless.
	peerKey := messagePeerKey(peerID)
	if peerKey == 0 {
		peerKey = messagePeerKey(fromID)
	}
	return hashDedupeFields(peerKey, int64(id), out)
}

// hashDedupeFields mixes (peerKey, id, out) into a full 64-bit hash so
// user IDs above 2^32 and adjacent numeric IDs cannot collide.
// FNV-1a 64-bit; small, fast, no cryptographic requirement.
func hashDedupeFields(peerKey, id int64, out bool) int64 {
	const (
		offset64 = uint64(14695981039346656037)
		prime64  = uint64(1099511628211)
	)
	h := offset64
	mix := func(v uint64) {
		for i := 0; i < 8; i++ {
			h ^= v & 0xff
			h *= prime64
			v >>= 8
		}
	}
	mix(uint64(peerKey))
	mix(uint64(id))
	if out {
		mix(1)
	} else {
		mix(0)
	}
	return int64(h)
}

type updateMeta struct {
	pts      int32
	ptsCount int32
	qts      int32
	channel  int64
}

func sortUpdatesByPtsAscending(updates []Update) {
	n := len(updates)
	if n < 2 {
		return
	}
	metas := make([]updateMeta, n)
	needsSort := false
	for i := range updates {
		metas[i] = extractUpdateMeta(updates[i])
		if i > 0 && !metaOrderedAscending(metas[i-1], metas[i]) {
			needsSort = true
		}
	}
	if !needsSort {
		return
	}
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return metaLess(metas[idx[a]], metas[idx[b]])
	})
	tmp := make([]Update, n)
	for i, k := range idx {
		tmp[i] = updates[k]
	}
	copy(updates, tmp)
}

func metaLess(mi, mj updateMeta) bool {
	if mi.pts == 0 && mj.pts == 0 {
		return false
	}
	if mi.pts == 0 {
		return true
	}
	if mj.pts == 0 {
		return false
	}
	return (mi.pts - mi.ptsCount) < (mj.pts - mj.ptsCount)
}

func metaOrderedAscending(a, b updateMeta) bool {
	return !metaLess(b, a)
}

func extractUpdateMeta(update Update) updateMeta {
	meta := updateMeta{}

	switch upd := update.(type) {
	case *UpdateNewMessage:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
		meta.channel = getChannelIDFromMessage(upd.Message)
	case *UpdateNewChannelMessage:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
		meta.channel = getChannelIDFromMessage(upd.Message)
	case *UpdateNewScheduledMessage:
		meta.channel = getChannelIDFromMessage(upd.Message)
	case *UpdateEditMessage:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
		meta.channel = getChannelIDFromMessage(upd.Message)
	case *UpdateEditChannelMessage:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
		meta.channel = getChannelIDFromMessage(upd.Message)
	case *UpdateDeleteMessages:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
	case *UpdateDeleteChannelMessages:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
		meta.channel = upd.ChannelID
	case *UpdateReadHistoryInbox:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
	case *UpdateReadHistoryOutbox:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
	case *UpdateWebPage:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
	case *UpdateReadMessagesContents:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
	case *UpdateReadChannelInbox:
		meta.pts = upd.Pts
		meta.channel = upd.ChannelID
	case *UpdateChannelWebPage:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
		meta.channel = upd.ChannelID
	case *UpdateFolderPeers:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
	case *UpdatePinnedMessages:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
	case *UpdatePinnedChannelMessages:
		meta.pts = upd.Pts
		meta.ptsCount = upd.PtsCount
		meta.channel = upd.ChannelID
	case *UpdateChannelTooLong:
		meta.pts = upd.Pts
		meta.channel = upd.ChannelID
	case *UpdateChannelParticipant:
		meta.qts = upd.Qts
		meta.channel = upd.ChannelID
	case *UpdateBotChatInviteRequester:
		meta.qts = upd.Qts
		meta.channel = getChannelIDFromPeer(upd.Peer)
	case *UpdateNewEncryptedMessage:
		meta.qts = upd.Qts
	case *UpdateBotGuestChatQuery:
		meta.qts = upd.Qts
	case *UpdateChatParticipant:
		meta.qts = upd.Qts
	case *UpdateBotStopped:
		meta.qts = upd.Qts
	case *UpdateBotChatBoost:
		meta.qts = upd.Qts
		meta.channel = getChannelIDFromPeer(upd.Peer)
	case *UpdateBotMessageReaction:
		meta.qts = upd.Qts
		meta.channel = getChannelIDFromPeer(upd.Peer)
	case *UpdateBotMessageReactions:
		meta.qts = upd.Qts
		meta.channel = getChannelIDFromPeer(upd.Peer)
	case *UpdateBotBusinessConnect:
		meta.qts = upd.Qts
	case *UpdateBotNewBusinessMessage:
		meta.qts = upd.Qts
	case *UpdateBotEditBusinessMessage:
		meta.qts = upd.Qts
	case *UpdateBotDeleteBusinessMessage:
		meta.qts = upd.Qts
	case *UpdateBotPurchasedPaidMedia:
		meta.qts = upd.Qts
	case *UpdateBotStarsSubscription:
		meta.qts = upd.Qts
	case *UpdateManagedBot:
		meta.qts = upd.Qts
	case *UpdateMessagePollVote:
		meta.qts = upd.Qts
	case *UpdateBotInlineSend:
	}

	return meta
}

func getChannelIDFromPeer(p Peer) int64 {
	switch peer := p.(type) {
	case *PeerChannel:
		return peer.ChannelID
	default:
		return 0
	}
}

func (c *Client) processWithState(meta updateMeta, apply func()) bool {
	d := c.dispatcher
	if d == nil {
		apply()
		return true
	}

	if meta.pts != 0 && meta.ptsCount == 0 {
		if meta.channel != 0 {
			if box := d.getChannelBox(meta.channel); box != nil {
				return box.processCheckpoint(meta.pts, apply)
			}
			apply()
			return true
		}
		if d.globalPtsBox != nil {
			return d.globalPtsBox.processCheckpoint(meta.pts, apply)
		}
		apply()
		return true
	}

	if meta.qts != 0 && d.globalQtsBox != nil {
		return d.globalQtsBox.process(meta.qts, 1, apply)
	}

	if meta.channel != 0 && meta.pts != 0 {
		if box := d.getChannelBox(meta.channel); box != nil {
			return box.process(meta.pts, meta.ptsCount, apply)
		}
	}

	if meta.pts != 0 && d.globalPtsBox != nil {
		return d.globalPtsBox.process(meta.pts, meta.ptsCount, apply)
	}

	apply()
	return true
}


func (c *Client) FetchDifference(fromPts int32, limit int32) {
	c.dispatcher.Lock()
	if c.dispatcher.recoveringDifference {
		c.dispatcher.Unlock()
		return
	}
	c.dispatcher.recoveringDifference = true
	c.dispatcher.Unlock()

	if c.dispatcher.globalPtsBox != nil {
		c.dispatcher.globalPtsBox.beginGettingDiff()
	}
	if c.dispatcher.globalQtsBox != nil {
		c.dispatcher.globalQtsBox.beginGettingDiff()
	}

	defer func() {
		if c.dispatcher.globalPtsBox != nil {
			c.dispatcher.globalPtsBox.endGettingDiff()
		}
		if c.dispatcher.globalQtsBox != nil {
			c.dispatcher.globalQtsBox.endGettingDiff()
		}
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

	const maxIterations = 10
	const baseBackoff = 200 * time.Millisecond
	const maxBackoff = 30 * time.Second
	backoff := baseBackoff
	consecutiveErrors := 0
	iteration := 0

	for iteration < maxIterations {
		iteration++
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		updates, err := c.MTProto.MakeRequestCtx(ctx, req)
		cancel()

		if err != nil {
			consecutiveErrors++
			if consecutiveErrors >= 5 {
				c.Log.Error("FetchDifference giving up after %d errors: %v", consecutiveErrors, err)
				return
			}
			select {
			case <-c.dispatcher.stopChan:
				return
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		consecutiveErrors = 0
		backoff = baseBackoff

		switch u := updates.(type) {
		case *UpdatesDifferenceEmpty:
			c.dispatcher.SetDate(u.Date)
			c.dispatcher.SetSeq(u.Seq)
			return

		case *UpdatesDifferenceObj:
			c.Cache.UpdatePeersToCache(u.Users, u.Chats)

			for _, message := range u.NewMessages {
				switch msg := message.(type) {
				case *MessageObj, *MessageService:
					go c.handleMessageUpdate(msg)
					totalFetched++
				}
			}

			for _, update := range u.OtherUpdates {
				totalFetched++
				c.dispatchUpdate(update)
			}

			c.dispatcher.SetPts(u.State.Pts)
			c.dispatcher.SetQts(u.State.Qts)
			c.dispatcher.SetSeq(u.State.Seq)
			c.dispatcher.SetDate(u.State.Date)
			return

		case *UpdatesDifferenceSlice:
			c.Cache.UpdatePeersToCache(u.Users, u.Chats)

			for _, message := range u.NewMessages {
				switch msg := message.(type) {
				case *MessageObj, *MessageService:
					go c.handleMessageUpdate(msg)
					totalFetched++
				}
			}

			for _, update := range u.OtherUpdates {
				totalFetched++
				c.dispatchUpdate(update)
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
			c.Log.Debug("unhandled difference type: %T", updates)
			return
		}
	}

	c.Log.Debug("difference fetch iteration cap reached (iterations=%d, pts=%d, fetched=%d) - scheduling follow-up", maxIterations, req.Pts, totalFetched)
	followupPts := req.Pts
	go func() {
		select {
		case <-c.dispatcher.stopChan:
			return
		case <-time.After(500 * time.Millisecond):
		}
		c.FetchDifference(followupPts, 5000)
	}()
}

func (c *Client) manageSeq(seq int32, seqStart int32) bool {
	if seqStart == 0 {
		if seq == 0 {
			return true
		}
		d := c.dispatcher
		d.Lock()
		if seq > d.state.Seq {
			d.state.Seq = seq
		}
		d.Unlock()
		return true
	}

	d := c.dispatcher
	d.Lock()
	currentSeq := d.state.Seq

	if currentSeq == 0 {
		d.state.Seq = seq
		d.Unlock()
		return true
	}

	expectedSeqStart := currentSeq + 1

	if expectedSeqStart == seqStart {
		d.state.Seq = seq
		d.Unlock()
		return true
	}

	if expectedSeqStart > seqStart {
		d.Unlock()
		c.Log.Debug("manageSeq stale seq=%d seqStart=%d currentSeq=%d -> drop", seq, seqStart, currentSeq)
		return false
	}

	if c.clientData.disableGapFetch || d.recoveringDifference {
		d.Unlock()
		return false
	}

	currentPts := d.state.Pts
	d.Unlock()

	go func(targetSeqStart int32) {
		select {
		case <-time.After(300 * time.Millisecond):
		case <-c.dispatcher.stopChan:
			return
		}
		if c.dispatcher.GetSeq()+1 >= targetSeqStart {
			return
		}
		select {
		case <-c.dispatcher.stopChan:
			return
		default:
		}
		freshPts := c.dispatcher.GetPts()
		if freshPts == 0 {
			freshPts = currentPts
		}
		c.FetchDifference(freshPts, 5000)
	}(seqStart)

	return false
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

	if box := c.dispatcher.getChannelBox(channelID); box != nil {
		box.beginGettingDiff()
	}

	defer func() {
		if box := c.dispatcher.getChannelBox(channelID); box != nil {
			box.endGettingDiff()
		}
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
	const maxIterations = 20
	const baseBackoff = 200 * time.Millisecond
	const maxBackoff = 30 * time.Second
	backoff := baseBackoff
	consecutiveErrors := 0
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
			if isChannelAccessError(err) {
				c.Log.Debug("channel %d access lost (%v); dropping state", channelID, err)
				c.dispatcher.cleanupChannel(channelID)
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= 5 {
				c.Log.Error("FetchChannelDifference channel=%d giving up after %d errors: %v", channelID, consecutiveErrors, err)
				return
			}
			select {
			case <-c.dispatcher.stopChan:
				return
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		consecutiveErrors = 0
		backoff = baseBackoff

		switch d := diff.(type) {
		case *UpdatesChannelDifferenceEmpty:
			c.dispatcher.SetChannelPts(channelID, d.Pts)
			return

		case *UpdatesChannelDifferenceObj:
			c.Cache.UpdatePeersToCache(d.Users, d.Chats)

			for _, message := range d.NewMessages {
				switch msg := message.(type) {
				case *MessageObj:
					go c.handleMessageUpdate(msg)
					totalFetched++
				case *MessageService:
					go c.handleMessageUpdate(msg)
					totalFetched++
				}
			}

			for _, update := range d.OtherUpdates {
				totalFetched++
				c.dispatchUpdate(update)
			}

			c.dispatcher.SetChannelPts(channelID, d.Pts)

			if d.Final {
				return
			}

			req.Pts = d.Pts

		case *UpdatesChannelDifferenceTooLong:
			c.Cache.UpdatePeersToCache(d.Users, d.Chats)

			if dialogChannel, ok := d.Dialog.(*DialogObj); ok {
				c.dispatcher.SetChannelPts(channelID, dialogChannel.Pts)
				c.Log.Debug("channel difference too long, resetting state (channel=%d, pts=%d, dropped partial messages=%d)", channelID, dialogChannel.Pts, len(d.Messages))

				if !d.Final {
					req.Pts = dialogChannel.Pts
					continue
				}
			}

			return

		default:
			c.Log.Debug("unhandled channel difference type: %T (channel=%d)", diff, channelID)
			return
		}
	}

	c.Log.Debug("channel difference fetch iteration cap reached (channel=%d, iterations=%d, pts=%d, fetched=%d) - scheduling follow-up", channelID, maxIterations, req.Pts, totalFetched)
	followupPts := req.Pts
	followupLimit := limit
	go func() {
		select {
		case <-c.dispatcher.stopChan:
			return
		case <-time.After(500 * time.Millisecond):
		}
		c.FetchChannelDifference(channelID, followupPts, followupLimit)
	}()
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
		case <-c.dispatcher.stopChan:
			return
		case <-time.After(timeout):
		}

		if c.dispatcher != nil {
			select {
			case <-c.dispatcher.stopChan:
				return
			default:
			}
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
				switch m := msg.(type) {
				case *MessageObj, *MessageService:
					go c.handleMessageUpdate(m)
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
				switch m := msg.(type) {
				case *MessageObj, *MessageService:
					go c.handleMessageUpdate(m)
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

type eventInfo struct {
	eventType string
	pattern   string
}

func normalizePattern(pattern any, defaultEvent EventType) any {
	switch p := pattern.(type) {
	case nil:
		return string(defaultEvent)
	case string:
		if p == "" {
			return string(defaultEvent)
		}
		return p
	case EventType:
		if p == "" {
			return string(defaultEvent)
		}
		return string(p)
	default:
		return pattern
	}
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
	"func(*telegram.GuestChatQuery) error":          "guestchat",
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

	case "guestchat", "botguestchat":
		if h, ok := handler.(func(m *GuestChatQuery) error); ok {
			return c.AddGuestChatHandler(h)
		}
		c.Log.Error("On(guestchat): invalid handler type %T, expected func(*GuestChatQuery) error", handler)

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
		case func(m *GuestChatQuery) error:
			return c.AddGuestChatHandler(h)
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

func Use[H any](c *Client, middlewares ...func(H) H) {
	if len(middlewares) == 0 {
		return
	}
	if c.dispatcher.middlewareManager == nil {
		c.dispatcher.middlewareManager = &middlewareManager{}
	}
	mm := c.dispatcher.middlewareManager
	mm.Lock()
	defer mm.Unlock()

	var zero H
	switch any(zero).(type) {
	case MessageHandler:
		for _, mw := range middlewares {
			mm.global = append(mm.global, any(mw).(func(MessageHandler) MessageHandler))
		}
	case EditHandler:
		for _, mw := range middlewares {
			mm.edit = append(mm.edit, any(mw).(func(EditHandler) EditHandler))
		}
	case DeleteHandler:
		for _, mw := range middlewares {
			mm.delete = append(mm.delete, any(mw).(func(DeleteHandler) DeleteHandler))
		}
	case AlbumHandler:
		for _, mw := range middlewares {
			mm.album = append(mm.album, any(mw).(func(AlbumHandler) AlbumHandler))
		}
	case InlineHandler:
		for _, mw := range middlewares {
			mm.inline = append(mm.inline, any(mw).(func(InlineHandler) InlineHandler))
		}
	case InlineSendHandler:
		for _, mw := range middlewares {
			mm.inlineSend = append(mm.inlineSend, any(mw).(func(InlineSendHandler) InlineSendHandler))
		}
	case GuestChatQueryHandler:
		for _, mw := range middlewares {
			mm.guestChat = append(mm.guestChat, any(mw).(func(GuestChatQueryHandler) GuestChatQueryHandler))
		}
	case CallbackHandler:
		for _, mw := range middlewares {
			mm.callback = append(mm.callback, any(mw).(func(CallbackHandler) CallbackHandler))
		}
	case InlineCallbackHandler:
		for _, mw := range middlewares {
			mm.inlineCallback = append(mm.inlineCallback, any(mw).(func(InlineCallbackHandler) InlineCallbackHandler))
		}
	case ParticipantHandler:
		for _, mw := range middlewares {
			mm.participant = append(mm.participant, any(mw).(func(ParticipantHandler) ParticipantHandler))
		}
	case PendingJoinHandler:
		for _, mw := range middlewares {
			mm.joinRequest = append(mm.joinRequest, any(mw).(func(PendingJoinHandler) PendingJoinHandler))
		}
	case RawHandler:
		for _, mw := range middlewares {
			mm.raw = append(mm.raw, any(mw).(func(RawHandler) RawHandler))
		}
	default:
		panic(fmt.Sprintf("telegram.Use: unsupported handler type %T", zero))
	}
}

// Group creates a new handler group
func (c *Client) Group(groupID int) *HandlerGroup {
	return &HandlerGroup{client: c, groupID: groupID}
}

// OnMessage registers a message handler and returns a builder
func (c *Client) OnMessage(pattern any, handler MessageHandler, filters ...Filter) *MessageHandleBuilder {
	h := c.AddMessageHandler(normalizePattern(pattern, EventNewMessage), handler, filters...)

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
func (c *Client) OnCallback(pattern any, handler CallbackHandler, filters ...Filter) *CallbackHandleBuilder {
	h := c.AddCallbackHandler(normalizePattern(pattern, EventCallbackQuery), handler, filters...)
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
func (c *Client) OnInlineQuery(pattern any, handler func(m *InlineQuery) error) Handle {
	return c.AddInlineHandler(normalizePattern(pattern, EventInlineQuery), handler)
}

// OnInlineCallback registers an inline callback handler and returns a handle
func (c *Client) OnInlineCallback(pattern any, handler func(m *InlineCallbackQuery) error) Handle {
	return c.AddInlineCallbackHandler(normalizePattern(pattern, EventInlineCallback), handler)
}

// OnEdit registers an edit handler and returns a handle
func (c *Client) OnEdit(pattern any, handler func(m *NewMessage) error, filters ...Filter) Handle {
	return c.AddEditHandler(normalizePattern(pattern, EventEditMessage), handler, filters...)
}

// OnDelete registers a delete handler and returns a handle
func (c *Client) OnDelete(pattern any, handler func(m *DeleteMessage) error) Handle {
	return c.AddDeleteHandler(normalizePattern(pattern, EventDeleteMessage), handler)
}

// OnAlbum registers an album handler and returns a handle
func (c *Client) OnAlbum(handler func(m *Album) error) Handle {
	return c.AddAlbumHandler(handler)
}

// OnChosenInline registers a chosen inline handler and returns a handle
func (c *Client) OnChosenInline(handler func(m *InlineSend) error) Handle {
	return c.AddInlineSendHandler(handler)
}

// OnGuestChat registers a bot guest-chat query handler and returns a handle
func (c *Client) OnGuestChat(handler func(m *GuestChatQuery) error) Handle {
	return c.AddGuestChatHandler(handler)
}

// OnParticipant registers a participant handler and returns a handle
func (c *Client) OnParticipant(handler func(m *ParticipantUpdate) error) Handle {
	return c.AddParticipantHandler(handler)
}

// OnJoinRequest registers a join request handler and returns a handle
func (c *Client) OnJoinRequest(handler func(m *JoinRequestUpdate) error) Handle {
	return c.AddJoinRequestHandler(handler)
}

// OnRaw registers a raw handler and returns a handle.
//
// See [Client.AddRawHandler] for the exact delivery semantics — updates
// pass through gogram's pts/qts gap-tracking dispatcher before reaching
// this handler. Callers who need the un-gapped MTProto container stream
// should use the escape hatch described there.
func (c *Client) OnRaw(updateType Update, handler func(m Update, c *Client) error) Handle {
	return c.AddRawHandler(updateType, handler)
}

// OnE2EMessage registers an E2E message handler and returns a handle
func (c *Client) OnE2EMessage(handler func(update Update, c *Client) error) Handle {
	return c.AddE2EHandler(handler)
}
