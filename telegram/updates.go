// Copyright (c) 2025, amarnathcjd

package telegram

import (
	"cmp"
	"container/list"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
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
	hb.handle.metaMu.Lock()
	defer hb.handle.metaMu.Unlock()
	hb.handle.Filters = append(hb.handle.Filters, filters...)
	return hb
}

func (hb *MessageHandleBuilder) Use(middlewares ...Middleware) *MessageHandleBuilder {
	hb.handle.metaMu.Lock()
	defer hb.handle.metaMu.Unlock()
	hb.middlewares = append(hb.middlewares, middlewares...)
	hb.handle.middlewares = append(hb.handle.middlewares, middlewares...)
	return hb
}

func (hb *MessageHandleBuilder) Name(name string) *MessageHandleBuilder {
	hb.handle.metaMu.Lock()
	defer hb.handle.metaMu.Unlock()
	hb.handle.name = name
	return hb
}

func (hb *MessageHandleBuilder) Description(desc string) *MessageHandleBuilder {
	hb.handle.metaMu.Lock()
	defer hb.handle.metaMu.Unlock()
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
	cb.handle.metaMu.Lock()
	defer cb.handle.metaMu.Unlock()
	cb.handle.Filters = append(cb.handle.Filters, filters...)
	return cb
}

func (cb *CallbackHandleBuilder) Name(name string) *CallbackHandleBuilder {
	cb.handle.metaMu.Lock()
	defer cb.handle.metaMu.Unlock()
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
	maxSize   int
	evictions int
	items     map[updateDedupeKey]*list.Element
	list      *list.List
}

type lruEntry struct {
	key updateDedupeKey
}

func newLRUCache(maxSize int) *lruCache {
	return &lruCache{
		maxSize: maxSize,
		items:   make(map[updateDedupeKey]*list.Element),
		list:    list.New(),
	}
}

func (c *lruCache) TryAdd(key updateDedupeKey) bool {
	c.Lock()
	defer c.Unlock()

	if elem, exists := c.items[key]; exists && elem != nil {
		if _, ok := elem.Value.(*lruEntry); ok {
			c.list.MoveToFront(elem)
			return false
		}
		delete(c.items, key)
		c.list.Remove(elem)
	}

	if c.maxSize > 0 && c.list.Len() >= c.maxSize {
		oldest := c.list.Back()
		entry := oldest.Value.(*lruEntry)
		delete(c.items, entry.key)
		entry.key = key
		c.list.MoveToFront(oldest)
		c.items[key] = oldest
		c.evictions++
		if c.evictions >= c.maxSize {
			items := make(map[updateDedupeKey]*list.Element, c.maxSize)
			for elem := c.list.Front(); elem != nil; elem = elem.Next() {
				items[elem.Value.(*lruEntry).key] = elem
			}
			c.items = items
			c.evictions = 0
		}
		return true
	}

	entry := &lruEntry{key: key}
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

func (s *shardedLRU) shard(key updateDedupeKey) *lruCache {
	if len(s.shards) == 1 {
		return s.shards[0]
	}
	idx := uint64(key.peerID) ^ uint64(key.id) ^ uint64(key.kind)
	idx ^= idx >> 33
	idx *= 0xff51afd7ed558ccd
	idx ^= idx >> 33
	idx *= 0xc4ceb9fe1a85ec53
	idx ^= idx >> 33
	return s.shards[idx%uint64(len(s.shards))]
}

func (s *shardedLRU) TryAdd(key updateDedupeKey) bool {
	return s.shard(key).TryAdd(key)
}

type patternCache struct {
	cache sync.Map
	mu    sync.Mutex
	keys  []string
	next  int
}

func newPatternCache() *patternCache {
	return &patternCache{}
}

// counterBox keeps monotonically increasing counters (pts/qts) ordered and deduplicated.
// It buffers out-of-order updates, detects gaps, and optionally triggers a fetch to fill them.
type counterBox struct {
	sync.Mutex
	name        string
	current     int32
	pending     map[int32][]pendingCounter
	recovering  bool
	fetchGap    func(from, target int32)
	logger      Logger
	debounce    time.Duration
	lastGapAt   time.Time
	gapAttempt  uint64
	gapFailures uint
	onAdvance   func(int32)
	schedule    func(func()) bool
}

type pendingCounter struct {
	counter int32
	count   int32
	apply   func()
	arrived time.Time
}

func newCounterBox(name string, logger Logger, fetch func(from, target int32), onAdvance func(int32)) *counterBox {
	return &counterBox{
		name:      name,
		pending:   make(map[int32][]pendingCounter),
		fetchGap:  fetch,
		logger:    logger,
		debounce:  time.Second,
		onAdvance: onAdvance,
	}
}

// process enforces ordering using Telegram semantics where counter represents the value *after* applying the update.
// If the counter is contiguous, apply() is executed immediately; otherwise it is buffered and a gap fetch is triggered.
func (b *counterBox) process(counter, count int32, recovered bool, apply func()) bool {
	if counter == 0 {
		apply()
		return true
	}

	b.Lock()
	defer b.Unlock()

	prev := max(counter-count, 0)

	if b.current == 0 && count == 0 && !recovered {
		// The first read-state update can precede the message at the same PTS.
		b.current = max(counter-1, 1)
		b.recordAdvance(b.current)
	}

	if b.current == 0 {
		b.current = counter
		b.recordAdvance(counter)
		b.runUnlocked(apply)
		b.flushLocked()
		return true
	}

	if counter == b.current {
		if count > 0 {
			return false
		}
		b.runUnlocked(apply)
		return true
	}

	if counter < b.current || (!recovered && prev < b.current) {
		return false
	}

	if recovered || prev == b.current {
		b.current = counter
		b.recordAdvance(counter)
		b.runUnlocked(apply)
		b.flushLocked()
		return true
	}

	if b.logger != nil {
		b.logger.Debug("counterBox=%s gap counter=%d count=%d boxCurrent=%d -> buffering", b.name, counter, count, b.current)
	}
	if len(b.pending[counter]) >= 64 {
		return false
	}
	if count > 0 {
		for _, pending := range b.pending[counter] {
			if pending.count > 0 {
				return false
			}
		}
	}
	pendingCount := 0
	for _, entries := range b.pending {
		pendingCount += len(entries)
	}
	if pendingCount >= 1024 {
		clear(b.pending)
		if b.logger != nil {
			b.logger.Warn("counterBox=%s pending limit reached; recovering from pts=%d", b.name, b.current)
		}
	}
	item := pendingCounter{counter: counter, count: count, apply: apply, arrived: time.Now()}
	if count > 0 {
		b.pending[counter] = append([]pendingCounter{item}, b.pending[counter]...)
	} else {
		b.pending[counter] = append(b.pending[counter], item)
	}
	b.triggerGapLocked(b.current, counter)
	return false
}

func (b *counterBox) recordAdvance(value int32) {
	if b.onAdvance != nil {
		b.onAdvance(value)
	}
}

// forceSet advances to a recovery checkpoint without rolling back newer updates.
func (b *counterBox) forceSet(value int32) {
	b.Lock()
	if value < b.current {
		b.Unlock()
		return
	}
	b.current = value
	b.recordAdvance(value)
	for k := range b.pending {
		if k <= value {
			delete(b.pending, k)
		}
	}
	// After externally forcing the counter, try to flush any buffered updates that now fit.
	b.flushLocked()
	b.Unlock()
}

func (b *counterBox) currentValue() int32 {
	b.Lock()
	defer b.Unlock()
	return b.current
}

func (b *counterBox) flushLocked() {
	for {
		var (
			readyKey   int32
			readyItem  pendingCounter
			foundReady bool
		)

		for key, list := range b.pending {
			if len(list) == 0 {
				delete(b.pending, key)
				continue
			}

			candidate := list[0]
			prev := candidate.counter - candidate.count
			if prev < 0 {
				prev = 0
			}

			if prev == b.current {
				readyKey = key
				readyItem = candidate
				foundReady = true
				break
			}

			if prev < b.current {
				// Stale buffered item; drop it.
				b.pending[key] = list[1:]
				if len(b.pending[key]) == 0 {
					delete(b.pending, key)
				}
			}
		}

		if !foundReady {
			return
		}

		// Consume the ready item before releasing the lock.
		if list := b.pending[readyKey]; len(list) > 0 {
			list = list[1:]
			if len(list) == 0 {
				delete(b.pending, readyKey)
			} else {
				b.pending[readyKey] = list
			}
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
	delay := min(b.debounce*time.Duration(1<<min(b.gapFailures, 6)), 60*time.Second)
	if time.Since(b.lastGapAt) < delay {
		return
	}
	b.recovering = true
	b.lastGapAt = time.Now()
	b.gapAttempt++
	attempt := b.gapAttempt

	work := func() {
		b.Lock()
		if b.gapAttempt != attempt {
			b.Unlock()
			return
		}
		if len(b.pending) == 0 {
			b.recovering = false
			b.gapFailures = 0
			b.Unlock()
			return
		}
		prev = b.current
		b.Unlock()
		defer func() {
			b.Lock()
			if b.gapAttempt == attempt {
				b.recovering = false
				if b.current > prev || len(b.pending) == 0 {
					b.gapFailures = 0
				} else {
					b.gapFailures = min(b.gapFailures+1, 6)
				}
			}
			b.Unlock()
		}()
		if b.logger != nil {
			b.logger.Debug("gap detected in %s (from=%d,target=%d)", b.name, prev, target)
		}
		b.fetchGap(prev, target)
	}
	if b.schedule != nil {
		if !b.schedule(work) {
			b.recovering = false
		}
	} else {
		go work()
	}
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

	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.cache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	if len(c.keys) == 1024 {
		c.cache.Delete(c.keys[c.next])
		c.keys[c.next] = pattern
		c.next = (c.next + 1) % len(c.keys)
	} else {
		c.keys = append(c.keys, pattern)
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
	metaMu            sync.RWMutex
	changeMu          sync.Mutex
	owner             Handle
	id                uint64
	Group             int
	priority          int
	name              string
	description       string
	enabled           bool
	onGroupChanged    func(int, int)
	onPriorityChanged func()
}

func (h *baseHandle) base() *baseHandle { return h }

func (h *baseHandle) SetGroup(group int) Handle {
	h.changeMu.Lock()
	defer h.changeMu.Unlock()
	h.metaMu.Lock()
	oldGroup := h.Group
	h.Group = group
	owner := h.owner
	h.metaMu.Unlock()
	if oldGroup != group && h.onGroupChanged != nil {
		h.onGroupChanged(oldGroup, group)
	}
	if owner != nil {
		return owner
	}
	return h
}

func (h *baseHandle) GetGroup() int {
	h.metaMu.RLock()
	defer h.metaMu.RUnlock()
	return h.Group
}

func (h *baseHandle) SetPriority(priority int) Handle {
	h.changeMu.Lock()
	defer h.changeMu.Unlock()
	h.metaMu.Lock()
	h.priority = priority
	owner := h.owner
	h.metaMu.Unlock()
	if h.onPriorityChanged != nil {
		h.onPriorityChanged()
	}
	if owner != nil {
		return owner
	}
	return h
}

func (h *baseHandle) GetPriority() int {
	h.metaMu.RLock()
	defer h.metaMu.RUnlock()
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
	messages  []*NewMessage
	groupedId int64
	deadline  time.Time
}

func (a *albumBox) dispatch(d *UpdateDispatcher, c *Client, stop <-chan struct{}) {
	sort.SliceStable(a.messages, func(i, j int) bool { return a.messages[i].ID < a.messages[j].ID })
	d.RLock()
	groups := make(map[int][]*albumHandle, len(d.albumHandles))
	copyHandlerGroups(groups, d.albumHandles)
	d.RUnlock()
	handle := func(h *albumHandle) error {
		select {
		case <-stop:
			return ErrEndGroup
		default:
		}
		hf := h.Handler
		if mm := d.middlewareManager; mm != nil {
			hf = applyChain(hf, mm.albums())
		}
		return hf(&Album{GroupedID: a.groupedId, Messages: slices.Clone(a.messages), Client: c})
	}
	for group, handlers := range groups {
		select {
		case <-stop:
			return
		default:
		}
		if group == DefaultGroup {
			for _, h := range handlers {
				if !c.submitUpdateTask(func() {
					if err := handle(h); err != nil && !errors.Is(err, ErrEndGroup) {
						c.Log.WithError(err).Error("[AlbumHandler]")
					}
				}, 0, stop) {
					return
				}
			}
			continue
		}
		if !c.submitUpdateTask(func() {
			for _, h := range handlers {
				if err := handle(h); errors.Is(err, ErrEndGroup) {
					return
				}
			}
		}, 0, stop) {
			return
		}
	}
}

// One short-lived scheduler covers all pending albums. Album arrival never
// sleeps in an update worker or creates a goroutine for every grouped ID.
func (c *Client) collectAlbums(d *UpdateDispatcher, stop <-chan struct{}) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			d.Lock()
			if d.albumStop == stop {
				clear(d.activeAlbums)
				d.albumRunning = false
			}
			d.Unlock()
			return
		case now := <-ticker.C:
			d.Lock()
			if d.albumStop != stop {
				d.Unlock()
				return
			}
			var ready []*albumBox
			for id, album := range d.activeAlbums {
				if !now.Before(album.deadline) {
					ready = append(ready, album)
					delete(d.activeAlbums, id)
				}
			}
			d.Unlock()
			for i, album := range ready {
				// Only the timer waits for callback capacity. The network reader
				// and preparation workers remain free to serve RPCs and updates.
				album.dispatch(d, c, stop)
				ready[i] = nil
			}
			d.Lock()
			done := d.albumStop != stop || len(d.activeAlbums) == 0
			if done && d.albumStop == stop {
				d.albumRunning = false
			}
			d.Unlock()
			if done {
				return
			}
		}
	}
}

type openChat struct {
	accessHash int64
	ctx        context.Context
	cancel     context.CancelFunc
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
	albumRunning          bool
	albumStop             chan struct{}
	albumDropped          uint64
	logger                Logger
	openChats             map[int64]*openChat
	lastUpdateTimeNano    atomic.Int64
	state                 UpdateState
	channelStates         map[int64]*channelState
	processedMsgLRU       *shardedLRU
	recoveringDifference  bool
	recoveringChannels    map[int64]bool
	stopChan              chan struct{}
	stopMu                sync.Mutex
	stopped               bool
	tasks                 *updateTaskPool
	preparations          *updateTaskPool
	secretUpdates         *updateTaskPool
	patternCache          *patternCache
	middlewareManager     *middlewareManager
	globalPtsBox          *counterBox
	globalQtsBox          *counterBox
	globalSeqBox          *counterBox
	channelPtsBoxes       map[int64]*counterBox
	channelGapFetcher     func(channelID int64, from, target int32)
	scheduleGap           func(func()) bool
}

func (d *UpdateDispatcher) SetPts(pts int32) {
	if d.globalPtsBox != nil {
		d.globalPtsBox.forceSet(pts)
		return
	}
	d.Lock()
	d.state.Pts = max(d.state.Pts, pts)
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
	d.state.Qts = max(d.state.Qts, qts)
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
	if d.globalSeqBox != nil {
		d.globalSeqBox.forceSet(seq)
		return
	}
	d.Lock()
	defer d.Unlock()
	d.state.Seq = max(d.state.Seq, seq)
}

func (d *UpdateDispatcher) GetSeq() int32 {
	if d.globalSeqBox != nil {
		return d.globalSeqBox.currentValue()
	}
	d.RLock()
	defer d.RUnlock()
	return d.state.Seq
}

func (d *UpdateDispatcher) SetDate(date int32) {
	d.Lock()
	defer d.Unlock()
	d.state.Date = max(d.state.Date, date)
}

func (d *UpdateDispatcher) GetDate() int32 {
	d.RLock()
	defer d.RUnlock()
	return d.state.Date
}

func (d *UpdateDispatcher) SetChannelPts(channelID int64, pts int32) {
	if box := d.getChannelBox(channelID); box != nil {
		box.forceSet(pts)
		return
	}

	d.Lock()
	if d.channelStates == nil {
		d.channelStates = make(map[int64]*channelState)
	}
	if state, ok := d.channelStates[channelID]; ok {
		state.pts = max(state.pts, pts)
	} else {
		d.channelStates[channelID] = &channelState{pts: pts}
	}
	d.Unlock()
}

func (d *UpdateDispatcher) GetChannelPts(channelID int64) int32 {
	d.RLock()
	box := d.channelPtsBoxes[channelID]
	var pts int32
	if state := d.channelStates[channelID]; state != nil {
		pts = state.pts
	}
	d.RUnlock()
	if box != nil {
		return box.currentValue()
	}
	return pts
}

func (d *UpdateDispatcher) getChannelBox(channelID int64) *counterBox {
	d.RLock()
	if box, ok := d.channelPtsBoxes[channelID]; ok {
		d.RUnlock()
		return box
	}
	d.RUnlock()

	if d.channelGapFetcher == nil {
		return nil
	}

	d.Lock()
	defer d.Unlock()

	// Double-check inside write lock in case another goroutine created it.
	if box, ok := d.channelPtsBoxes[channelID]; ok {
		return box
	}
	if d.channelPtsBoxes == nil {
		d.channelPtsBoxes = make(map[int64]*counterBox)
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
	box.schedule = d.scheduleGap

	if state, ok := d.channelStates[channelID]; ok {
		box.current = state.pts
	}

	d.channelPtsBoxes[channelID] = box
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
	return d.processedMsgLRU.TryAdd(updateDedupeKey{id: key})
}

// NewUpdateDispatcher initializes update handling once. Repeated calls preserve
// registered handlers, update state, and worker limits. Connect restarts a stopped
// dispatcher without replacing workers that are still running callbacks.
func (c *Client) NewUpdateDispatcher(sessionName ...string) {
	c.dispatcherOnce.Do(func() {
		if c.dispatcher != nil {
			return
		}
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
			albumStop:             make(chan struct{}),
			patternCache:          newPatternCache(),
			middlewareManager:     &middlewareManager{},
			channelPtsBoxes:       make(map[int64]*counterBox),
			scheduleGap:           c.dispatchInternal,
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
			c.FetchDifference(d.GetPts(), 5000)
		}, func(val int32) {
			d.Lock()
			d.state.Qts = val
			d.Unlock()
		})

		d.globalSeqBox = newCounterBox("seq", d.logger, func(from, target int32) {
			if !c.clientData.disableGapFetch {
				c.FetchDifference(d.GetPts(), 5000)
			}
		}, func(val int32) {
			d.Lock()
			d.state.Seq = val
			d.Unlock()
		})
		c.dispatcher = d
		d.globalPtsBox.schedule = c.dispatchInternal
		d.globalQtsBox.schedule = c.dispatchInternal
		d.globalSeqBox.schedule = c.dispatchInternal
		c.dispatcher.lastUpdateTimeNano.Store(time.Now().UnixNano())
		c.dispatcher.logger.Debug("update dispatcher initialized")

		go c.monitorNoUpdatesTimeout(d, d.stopChan)
	})
}

func (c *Client) RemoveHandle(handle Handle) error {
	if c == nil || c.dispatcher == nil {
		return errors.New("[DispatcherNotInitialized] dispatcher is not initialized")
	}

	if h, ok := handle.(interface{ base() *baseHandle }); ok {
		h.base().changeMu.Lock()
		defer h.base().changeMu.Unlock()
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
	case *e2eHandle:
		removeHandleFromMap(h, c.dispatcher.e2eHandles)
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
	return h.GetPriority()
}

func removeHandleFromMap[T handleWithID](handle T, handlesMap map[int][]T) {
	targetID := handle.getID()
	for key := range handlesMap {
		handles := handlesMap[key]
		for i := len(handles) - 1; i >= 0; i-- {
			if handles[i].getID() == targetID {
				handlesMap[key] = slices.Delete(handles, i, i+1)
				if len(handlesMap[key]) == 0 {
					delete(handlesMap, key)
				}
				return
			}
		}
	}
}

// ---------------------------- Handle Functions ----------------------------

func (c *Client) handleMessageUpdate(update Message, scheduled bool) {
	switch msg := update.(type) {
	case *MessageObj:
		if msg == nil {
			return
		}
		copy := *msg
		msg = &copy
		if msg.Out {
			if msg.FromID == nil {
				if me := c.Me(); me != nil {
					msg.FromID = &PeerUser{UserID: me.ID}
				}
			}
		}
		key := messageDedupeKey(msg, scheduled)
		if scheduled {
			// Scheduled messages have their own IDs and can be revised before sending.
			key.kind = 4
		}
		if cache := c.dispatcher.processedMsgLRU; cache != nil && !cache.TryAdd(key) {
			return
		}

		packed := packMessage(c, msg)
		if msg.GroupedID != 0 {
			c.handleAlbum(packed)
		}
		handle := func(h *messageHandle) error {
			h.metaMu.RLock()
			filters := slices.Clone(h.Filters)
			localMiddlewares := slices.Clone(h.middlewares)
			h.metaMu.RUnlock()
			if msg.Out && !containsFilter(filters, IsOutgoing) {
				return nil
			}
			if h.runFilterChain(packed, filters) {
				defer c.NewRecovery()()

				handler := h.Handler
				var mids []Middleware

				c.dispatcher.RLock()
				if c.dispatcher.middlewareManager != nil {
					mids = append(mids, c.dispatcher.middlewareManager.GetGlobal()...)
				}
				c.dispatcher.RUnlock()
				mids = append(mids, localMiddlewares...)

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
		convHandlers := slices.Clone(c.dispatcher.messageHandles[ConversationGroup])
		allMessageHandles := make(map[int][]*messageHandle)
		copyHandlerGroups(allMessageHandles, c.dispatcher.messageHandles)
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

		if len(groupsToProcess) > 0 {
			c.dispatchAsync(func() {
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
			})
		}

		if defaultHandlers, ok := allMessageHandles[DefaultGroup]; ok {
			for _, handler := range defaultHandlers {
				if handler.IsMatch(msg.Message, c) {
					h := handler
					c.dispatchAsync(func() {
						if err := handle(h); err != nil && !errors.Is(err, ErrEndGroup) {
							c.dispatcher.logger.WithError(err).Error("[NewMessageHandler]")
						}
					})
				}
			}
		}

	case *MessageService:
		if msg.Out {
			return
		}
		if cache := c.dispatcher.processedMsgLRU; cache != nil && !cache.TryAdd(serviceMessageDedupeKey(msg)) {
			return
		}
		packed := packMessage(c, msg)

		c.dispatcher.RLock()
		actionHandles := make(map[int][]*chatActionHandle)
		copyHandlerGroups(actionHandles, c.dispatcher.actionHandles)
		c.dispatcher.RUnlock()

		for group, handler := range actionHandles {
			c.dispatchHandlerGroup(group, func() {
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
						c.dispatchAsync(func() {
							err := handle(h)
							if err != nil {
								if errors.Is(err, ErrEndGroup) {
									return
								}
								c.Log.WithError(err).Error("[ChatActionHandler]")
							}
						})
					} else {
						if err := handle(h); err != nil && errors.Is(err, ErrEndGroup) {
							break
						}
					}
				}
			})
		}
	}
}

func (c *Client) handleAlbum(packed *NewMessage) {
	if packed == nil || packed.Message == nil {
		return
	}
	message := packed.Message
	d := c.dispatcher
	d.stopMu.Lock()
	stopped := d.stopped
	d.stopMu.Unlock()
	if stopped {
		return
	}
	d.Lock()
	stop := d.albumStop
	select {
	case <-stop:
		d.Unlock()
		return
	default:
	}
	if group := d.activeAlbums[message.GroupedID]; group != nil {
		// Telegram media groups contain at most ten items.
		if len(group.messages) < 10 {
			group.messages = append(group.messages, packed)
		}
		d.Unlock()
		return
	}
	if len(d.activeAlbums) >= 1024 {
		d.albumDropped++
		dropped := d.albumDropped
		d.Unlock()
		if dropped&(dropped-1) == 0 {
			c.Log.Warn("album queue full: dropped %d groups", dropped)
		}
		return
	}
	if d.activeAlbums == nil {
		d.activeAlbums = make(map[int64]*albumBox)
	}
	delay := time.Duration(max(c.clientData.albumWaitTime, 1)) * time.Millisecond
	d.activeAlbums[message.GroupedID] = &albumBox{messages: []*NewMessage{packed}, groupedId: message.GroupedID, deadline: time.Now().Add(delay)}
	start := !d.albumRunning
	d.albumRunning = true
	d.Unlock()
	if start {
		go c.collectAlbums(d, stop)
	}
}

func (c *Client) fetchPeersBeforeUpdate(m Message, pts int32) {
	if msg, ok := m.(*MessageObj); ok && msg != nil && pts > 0 {
		if (c.IdInCache(c.GetPeerID(msg.FromID)) || func() bool {
			_, ok := msg.FromID.(*PeerChat)
			return ok
		}()) && (c.IdInCache(c.GetPeerID(msg.PeerID)) || func() bool {
			_, ok := msg.PeerID.(*PeerChat)
			return ok
		}()) {
			c.handleMessageUpdate(msg, false)
			return
		}

		updatedMessage, err := c.GetDifference(pts, 1)
		if err != nil {
			c.Log.WithError(err).Error("[GetDifference] failed to get difference")
		}
		if updated, ok := updatedMessage.(*MessageObj); ok && updated != nil && messageDedupeKey(updated, false) == messageDedupeKey(msg, false) {
			m = updated
		}
	}
	c.handleMessageUpdate(m, false)
}

func (c *Client) fetchChannelPeersBeforeUpdate(m Message, pts int32) {
	msg, ok := m.(*MessageObj)
	if !ok {
		c.handleMessageUpdate(m, false)
		return
	}
	peerCached := c.IdInCache(c.GetPeerID(msg.PeerID))
	senderCached := msg.FromID == nil || c.IdInCache(c.GetPeerID(msg.FromID))
	if peerCached && senderCached {
		c.handleMessageUpdate(msg, false)
		return
	}
	if peer, ok := msg.PeerID.(*PeerChannel); ok && pts > 0 {
		currentPts := c.dispatcher.GetChannelPts(peer.ChannelID)
		if currentPts == 0 || pts-1 < currentPts {
			currentPts = pts - 1
		}
		c.FetchChannelDifference(peer.ChannelID, currentPts, 10)
	}
	c.handleMessageUpdate(msg, false)
}

func (c *Client) handleEditUpdate(update Message, pts int32) {
	if msg, ok := update.(*MessageObj); ok {
		if msg == nil {
			return
		}
		copy := *msg
		msg = &copy
		if msg.Out {
			if msg.FromID == nil {
				if me := c.Me(); me != nil {
					msg.FromID = &PeerUser{UserID: me.ID}
				}
			}
		}
		key := messageDedupeKey(msg, true)
		key.counter = pts
		if cache := c.dispatcher.processedMsgLRU; cache != nil && !cache.TryAdd(key) {
			return
		}
		packed := packMessage(c, msg)

		c.dispatcher.RLock()
		editHandles := make(map[int][]*messageEditHandle)
		copyHandlerGroups(editHandles, c.dispatcher.messageEditHandles)
		c.dispatcher.RUnlock()

		for group, handlers := range editHandles {
			c.dispatchHandlerGroup(group, func() {
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
							c.dispatchAsync(func() {
								err := handle(handler)
								if err != nil {
									if errors.Is(err, ErrEndGroup) {
										return
									}
									c.Log.WithError(err).Error("[EditMessageHandler]")
								}
							})
						} else {
							if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
								break
							}
						}
					}
				}
			})
		}
	}
}

func (c *Client) handleCallbackUpdate(update *UpdateBotCallbackQuery) {
	packed := packCallbackQuery(c, update)

	c.dispatcher.RLock()
	callbackHandles := make(map[int][]*callbackHandle)
	copyHandlerGroups(callbackHandles, c.dispatcher.callbackHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range callbackHandles {
		c.dispatchHandlerGroup(group, func() {
			for _, handler := range handlers {
				if handler.IsMatch(update.Data, c) {
					handle := func(h *callbackHandle) error {
						h.metaMu.RLock()
						filters := slices.Clone(h.Filters)
						h.metaMu.RUnlock()
						if h.runFilterChain(packed, filters) {
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
						c.dispatchAsync(func() {
							err := handle(handler)
							if err != nil {
								if errors.Is(err, ErrEndGroup) {
									return
								}
								c.Log.WithError(err).Error("[CallbackQueryHandler]")
							}
						})
					} else {
						if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
							break
						}
					}
				}
			}
		})
	}
}

func (c *Client) handleInlineCallbackUpdate(update *UpdateInlineBotCallbackQuery) {
	packed := packInlineCallbackQuery(c, update)

	c.dispatcher.RLock()
	inlineCallbackHandles := make(map[int][]*inlineCallbackHandle)
	copyHandlerGroups(inlineCallbackHandles, c.dispatcher.inlineCallbackHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range inlineCallbackHandles {
		c.dispatchHandlerGroup(group, func() {
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
						c.dispatchAsync(func() {
							err := handle(handler)
							if err != nil {
								if errors.Is(err, ErrEndGroup) {
									return
								}
								c.Log.WithError(err).Error("[InlineCallbackHandler]")
							}
						})
					} else {
						if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
							break
						}
					}
				}
			}
		})
	}
}

func (c *Client) handleParticipantUpdate(update *UpdateChannelParticipant) {
	packed := packChannelParticipant(c, update)

	c.dispatcher.RLock()
	participantHandles := make(map[int][]*participantHandle)
	copyHandlerGroups(participantHandles, c.dispatcher.participantHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range participantHandles {
		c.dispatchHandlerGroup(group, func() {
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
					c.dispatchAsync(func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, ErrEndGroup) {
								return
							}
							c.Log.WithError(err).Error("[ParticipantUpdateHandler]")
						}
					})
				} else {
					if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
						break
					}
				}
			}
		})
	}
}

func (c *Client) handleInlineUpdate(update *UpdateBotInlineQuery) {
	packed := packInlineQuery(c, update)

	c.dispatcher.RLock()
	inlineHandles := make(map[int][]*inlineHandle)
	copyHandlerGroups(inlineHandles, c.dispatcher.inlineHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range inlineHandles {
		c.dispatchHandlerGroup(group, func() {
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
						c.dispatchAsync(func() {
							err := handle(handler)
							if err != nil {
								if errors.Is(err, ErrEndGroup) {
									return
								}
								c.Log.WithError(err).Error("[InlineQueryHandler]")
							}
						})
					} else {
						if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
							break
						}
					}
				}
			}
		})
	}
}

func (c *Client) handleInlineSendUpdate(update *UpdateBotInlineSend) {
	packed := packInlineSend(c, update)

	c.dispatcher.RLock()
	inlineSendHandles := make(map[int][]*inlineSendHandle)
	copyHandlerGroups(inlineSendHandles, c.dispatcher.inlineSendHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range inlineSendHandles {
		c.dispatchHandlerGroup(group, func() {
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
					c.dispatchAsync(func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, ErrEndGroup) {
								return
							}
							c.Log.WithError(err).Error("[InlineSendHandler]")
						}
					})
				} else {
					if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
						break
					}
				}
			}
		})
	}
}

func (c *Client) handleGuestChatUpdate(update *UpdateBotGuestChatQuery) {
	packed := packGuestChatQuery(c, update)

	c.dispatcher.RLock()
	guestChatHandles := make(map[int][]*guestChatHandle)
	copyHandlerGroups(guestChatHandles, c.dispatcher.guestChatHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range guestChatHandles {
		c.dispatchHandlerGroup(group, func() {
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
					c.dispatchAsync(func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, ErrEndGroup) {
								return
							}
							c.Log.WithError(err).Error("[GuestChatQueryHandler]")
						}
					})
				} else {
					if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
						break
					}
				}
			}
		})
	}
}

func (c *Client) handleDeleteUpdate(update Update) {
	packed := packDeleteMessage(c, update)

	c.dispatcher.RLock()
	messageDeleteHandles := make(map[int][]*messageDeleteHandle)
	copyHandlerGroups(messageDeleteHandles, c.dispatcher.messageDeleteHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range messageDeleteHandles {
		c.dispatchHandlerGroup(group, func() {
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
					c.dispatchAsync(func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, ErrEndGroup) {
								return
							}
							c.Log.WithError(err).Error("[DeleteMessageHandler]")
						}
					})
				} else {
					if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
						break
					}
				}
			}
		})
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
	copyHandlerGroups(joinRequestHandles, c.dispatcher.joinRequestHandles)
	c.dispatcher.RUnlock()

	for group, handlers := range joinRequestHandles {
		c.dispatchHandlerGroup(group, func() {
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
					c.dispatchAsync(func() {
						err := handle(handler)
						if err != nil {
							if errors.Is(err, ErrEndGroup) {
								return
							}
							c.Log.WithError(err).Error("[JoinRequestHandler]")
						}
					})
				} else {
					if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
						break
					}
				}
			}
		})
	}
}

func (c *Client) handleRawUpdate(update Update) {
	c.dispatcher.RLock()
	rawHandles := make(map[int][]*rawHandle)
	copyHandlerGroups(rawHandles, c.dispatcher.rawHandles)
	c.dispatcher.RUnlock()

	updateTypeID := update.CRC()

	for group, handlers := range rawHandles {
		c.dispatchHandlerGroup(group, func() {
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
						c.dispatchAsync(func() {
							err := handle(handler)
							if err != nil {
								if errors.Is(err, ErrEndGroup) {
									return
								}
								c.Log.WithError(err).Error("[RawUpdateHandler]")
							}
						})
					} else {
						if err := handle(handler); err != nil && errors.Is(err, ErrEndGroup) {
							break
						}
					}
				}
			}
		})
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

func addHandleToMap[T Handle](handleMap map[int][]T, handle T) T {
	if h, ok := any(handle).(interface{ base() *baseHandle }); ok {
		h.base().metaMu.Lock()
		h.base().owner = handle
		h.base().metaMu.Unlock()
	}
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

	return handle
}

func makePriorityChangeCallback[T handleWithID](handleMap map[int][]T, handle T, handleID uint64, getGroup func() int, getPriority func() int, mu *sync.RWMutex) func() {
	return func() {
		mu.Lock()
		defer mu.Unlock()
		group := getGroup()
		handlers := handleMap[group]
		found := false
		for i := range handlers {
			if handlers[i].getID() == handleID {
				handlers = slices.Delete(handlers, i, i+1)
				found = true
				break
			}
		}
		if !found {
			return
		}
		index := len(handlers)
		priority := getPriority()
		for i, existing := range handlers {
			if priority > existing.getPriority() {
				index = i
				break
			}
		}
		handleMap[group] = slices.Insert(handlers, index, handle)
	}
}

func makeGroupChangeCallback[T handleWithID](handleMap map[int][]T, handle T, handleID uint64, mu *sync.RWMutex) func(int, int) {
	return func(oldGroup, newGroup int) {
		mu.Lock()
		defer mu.Unlock()
		old := handleMap[oldGroup]
		found := false
		for i := range old {
			if old[i].getID() == handleID {
				handleMap[oldGroup] = slices.Delete(old, i, i+1)
				if len(handleMap[oldGroup]) == 0 {
					delete(handleMap, oldGroup)
				}
				found = true
				break
			}
		}
		if !found {
			return
		}
		handlers := handleMap[newGroup]
		index := len(handlers)
		for i, existing := range handlers {
			if handle.getPriority() > existing.getPriority() {
				index = i
				break
			}
		}
		handleMap[newGroup] = slices.Insert(handlers, index, handle)
	}
}

func (c *Client) AddMessageHandler(pattern any, handler MessageHandler, filters ...Filter) Handle {
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = slices.Clone(filters)
	}

	handleID := nextHandleID()
	handle := &messageHandle{
		Pattern: normalizePattern(pattern, EventNewMessage),
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
		messageFilters = slices.Clone(filters)
	}
	handleID := nextHandleID()
	h := &messageEditHandle{
		Pattern:    normalizePattern(pattern, EventEditMessage),
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
		Pattern:    normalizePattern(pattern, EventInlineQuery),
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
		messageFilters = slices.Clone(filters)
	}
	handleID := nextHandleID()
	h := &callbackHandle{
		Pattern:    normalizePattern(pattern, EventCallbackQuery),
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
		Pattern:    normalizePattern(pattern, EventInlineCallback),
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
// Recovery can introduce buffering delays when a gap cannot yet be filled.
// Handler execution is concurrent and the queues are bounded; saturated queues
// can discard callbacks. This is not a persistent exactly-once delivery log.
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
		typeID = updateType.CRC()
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

// UnpackContainer flattens a raw MTProto update container into a slice of individual updates.
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

	switch upd := u.(type) {
	case *UpdatesObj:
		return c.manageSeq(upd.Seq, upd.Seq, func() {
			d.SetDate(upd.Date)
			c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
			for _, update := range upd.Updates {
				c.applyIncomingUpdate(update)
			}
		})
	case *UpdatesCombined:
		return c.manageSeq(upd.Seq, upd.SeqStart, func() {
			d.SetDate(upd.Date)
			c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
			for _, update := range upd.Updates {
				c.applyIncomingUpdate(update)
			}
		})
	case *UpdateShort:
		c.dispatcher.SetDate(upd.Date)
		c.applyIncomingUpdate(upd.Update)
		return true
	case *UpdateShortMessage:
		msg := &MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.UserID), PeerID: getPeerUser(upd.UserID), Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}
		c.applyIncomingUpdate(&UpdateNewMessage{Message: msg, Pts: upd.Pts, PtsCount: upd.PtsCount})
		return true
	case *UpdateShortChatMessage:
		msg := &MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.FromID), PeerID: &PeerChat{ChatID: upd.ChatID}, Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}
		c.applyIncomingUpdate(&UpdateNewMessage{Message: msg, Pts: upd.Pts, PtsCount: upd.PtsCount})
		return true
	case *UpdateShortSentMessage:
		msg := &MessageObj{ID: upd.ID, Out: upd.Out, Date: upd.Date, Media: upd.Media, Entities: upd.Entities, TtlPeriod: upd.TtlPeriod}
		c.applyIncomingUpdate(&UpdateNewMessage{Message: msg, Pts: upd.Pts, PtsCount: upd.PtsCount})
		return true
	case *UpdateChannelTooLong:
		c.applyIncomingUpdate(upd)
		return true
	case *UpdatesTooLong:
		c.dispatchInternal(func() { c.FetchDifference(d.GetPts(), 5000) })
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
	if update == nil {
		return
	}
	// PTS/QTS handle ordered events. These identities also cover RPC echoes,
	// recovered messages without counters, and queries without PTS/QTS.
	var key updateDedupeKey
	var message Message
	var editPts int32
	var edit bool
	var scheduled bool
	switch upd := update.(type) {
	case *UpdateNewMessage:
		message = upd.Message
	case *UpdateNewChannelMessage:
		message = upd.Message
	case *UpdateNewScheduledMessage:
		message, scheduled = upd.Message, true
	case *UpdateEditMessage:
		message, editPts, edit = upd.Message, upd.Pts, true
	case *UpdateEditChannelMessage:
		message, editPts, edit = upd.Message, upd.Pts, true
	case *UpdateBotCallbackQuery:
		key = updateDedupeKey{kind: upd.CRC(), id: upd.QueryID}
	case *UpdateInlineBotCallbackQuery:
		key = updateDedupeKey{kind: upd.CRC(), id: upd.QueryID}
	case *UpdateBotInlineQuery:
		key = updateDedupeKey{kind: upd.CRC(), id: upd.QueryID}
	case *UpdateNewEncryptedMessage:
		switch msg := upd.Message.(type) {
		case *EncryptedMessageObj:
			key = updateDedupeKey{kind: upd.CRC(), peerID: int64(msg.ChatID), id: msg.RandomID}
		case *EncryptedMessageService:
			key = updateDedupeKey{kind: upd.CRC(), peerID: int64(msg.ChatID), id: msg.RandomID}
		}
	}
	if message != nil {
		switch msg := message.(type) {
		case *MessageObj:
			key = messageDedupeKey(msg, edit || scheduled)
		case *MessageService:
			key = serviceMessageDedupeKey(msg)
		}
		key.kind = (&UpdateNewMessage{}).CRC()
		if edit {
			key.kind = (&UpdateEditMessage{}).CRC()
			key.counter = editPts
		} else if scheduled {
			key.kind = (&UpdateNewScheduledMessage{}).CRC()
		}
	}
	if key.kind == 0 {
		meta := extractUpdateMeta(update)
		if meta.pts != 0 && meta.ptsCount == 0 {
			if data, err := tl.Marshal(update); err == nil {
				key = updateDedupeKey{kind: update.CRC(), peerID: meta.channel, counter: meta.pts, revision: sha256.Sum256(data)}
			}
		}
	}
	if key.kind != 0 {
		if cache := c.dispatcher.processedMsgLRU; cache != nil && !cache.TryAdd(key) {
			return
		}
	}

	switch upd := update.(type) {
	case *UpdateNewMessage:
		c.dispatchInternal(func() { c.fetchPeersBeforeUpdate(upd.Message, upd.Pts) })
	case *UpdateNewChannelMessage:
		c.dispatchInternal(func() { c.fetchChannelPeersBeforeUpdate(upd.Message, upd.Pts) })
	case *UpdateNewScheduledMessage:
		c.dispatchInternal(func() { c.handleMessageUpdate(upd.Message, true) })
	case *UpdateEditMessage:
		c.dispatchInternal(func() { c.handleEditUpdate(upd.Message, upd.Pts) })
	case *UpdateEditChannelMessage:
		c.dispatchInternal(func() { c.handleEditUpdate(upd.Message, upd.Pts) })
	case *UpdateDeleteMessages:
		c.dispatchInternal(func() { c.handleDeleteUpdate(upd) })
	case *UpdateDeleteChannelMessages:
		c.dispatchInternal(func() { c.handleDeleteUpdate(upd) })
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
		c.dispatchInternal(func() { c.handleInlineUpdate(upd) })
	case *UpdateBotCallbackQuery:
		c.dispatchInternal(func() { c.handleCallbackUpdate(upd) })
	case *UpdateInlineBotCallbackQuery:
		c.dispatchInternal(func() { c.handleInlineCallbackUpdate(upd) })
	case *UpdateChannelParticipant:
		c.dispatchInternal(func() { c.handleParticipantUpdate(upd) })
	case *UpdatePendingJoinRequests:
		c.dispatchInternal(func() { c.handleJoinRequestUpdate(upd) })
	case *UpdateBotChatInviteRequester:
		c.dispatchInternal(func() { c.handleJoinRequestUpdate(upd) })
	case *UpdateBotInlineSend:
		c.dispatchInternal(func() { c.handleInlineSendUpdate(upd) })
	case *UpdateBotGuestChatQuery:
		c.dispatchInternal(func() { c.handleGuestChatUpdate(upd) })
	case *UpdateChannelTooLong:
		currentPts := c.dispatcher.GetChannelPts(upd.ChannelID)
		if currentPts == 0 {
			currentPts = upd.Pts
		}
		c.dispatchInternal(func() { c.FetchChannelDifference(upd.ChannelID, currentPts, 50) })
	case *UpdateNewEncryptedMessage, *UpdateEncryption:
		// Key exchange and encrypted messages must keep their wire order.
		c.submitUpdateTask(func() {
			if err := c.HandleSecretChatUpdate(upd); err != nil {
				c.Log.Error("secret chat update: %v", err)
			}
		}, 2, nil)
	}

	c.dispatchInternal(func() { c.handleRawUpdate(update) })
}

func getChannelIDFromMessage(msg Message) int64 {
	switch m := msg.(type) {
	case *MessageObj:
		return getChannelIDFromPeer(m.PeerID)
	case *MessageService:
		return getChannelIDFromPeer(m.PeerID)
	}
	return 0
}

// Keep the complete identity: channel IDs are 64-bit, and messages sent by
// the same user in different channels can have the same message ID.
type updateDedupeKey struct {
	kind     uint32
	peerKind uint32
	peerID   int64
	id       int64
	counter  int32
	revision [32]byte
}

func messageDedupeKey(msg *MessageObj, isEdit bool) updateDedupeKey {
	if msg == nil {
		return updateDedupeKey{}
	}
	key := dedupeKeyFromFields(msg.ID, msg.FromID, msg.PeerID)
	key.kind = 1
	if isEdit {
		key.kind = 3
		// Telegram timestamps have one-second precision; distinct edits can
		// share EditDate. PTS and content distinguish those revisions.
		data, _ := tl.Marshal(msg)
		key.revision = sha256.Sum256(data)
	}
	return key
}

func serviceMessageDedupeKey(msg *MessageService) updateDedupeKey {
	if msg == nil {
		return updateDedupeKey{}
	}
	key := dedupeKeyFromFields(msg.ID, msg.FromID, msg.PeerID)
	key.kind = 2
	return key
}

func dedupeKeyFromFields(id int32, fromID Peer, peerID Peer) updateDedupeKey {
	if peerID == nil {
		peerID = fromID
	}
	key := updateDedupeKey{id: int64(id)}
	// Private chats and basic groups share the account's message ID sequence.
	// Channels each have their own sequence. This also matches short sent
	// message acknowledgments, which omit the private-chat peer entirely.
	switch peer := peerID.(type) {
	case *PeerChannel:
		key.peerKind = peer.CRC()
		key.peerID = peer.ChannelID
	}
	return key
}

type updateMeta struct {
	pts       int32
	ptsCount  int32
	qts       int32
	channel   int64
	recovered bool
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
	case *UpdateBotBusinessConnect:
		meta.qts = upd.Qts
	case *UpdateBotChatBoost:
		meta.qts = upd.Qts
	case *UpdateBotDeleteBusinessMessage:
		meta.qts = upd.Qts
	case *UpdateBotEditBusinessMessage:
		meta.qts = upd.Qts
	case *UpdateBotMessageReaction:
		meta.qts = upd.Qts
	case *UpdateBotMessageReactions:
		meta.qts = upd.Qts
	case *UpdateBotNewBusinessMessage:
		meta.qts = upd.Qts
	case *UpdateBotPurchasedPaidMedia:
		meta.qts = upd.Qts
	case *UpdateBotStarsSubscription:
		meta.qts = upd.Qts
	case *UpdateBotStopped:
		meta.qts = upd.Qts
	case *UpdateChatParticipant:
		meta.qts = upd.Qts
	case *UpdateManagedBot:
		meta.qts = upd.Qts
	case *UpdateMessagePollVote:
		meta.qts = upd.Qts
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

	if meta.qts != 0 && d.globalQtsBox != nil {
		return d.globalQtsBox.process(meta.qts, 1, meta.recovered, apply)
	}

	if meta.channel != 0 && meta.pts != 0 {
		if box := d.getChannelBox(meta.channel); box != nil {
			return box.process(meta.pts, meta.ptsCount, meta.recovered, apply)
		}
	}

	if meta.pts != 0 && d.globalPtsBox != nil {
		return d.globalPtsBox.process(meta.pts, meta.ptsCount, meta.recovered, apply)
	}

	apply()
	return true
}

func (c *Client) applyRecoveredUpdates(updates []Update, channelID int64) {
	updates = slices.Clone(updates)
	slices.SortStableFunc(updates, func(a, b Update) int {
		left, right := extractUpdateMeta(a), extractUpdateMeta(b)
		if left.qts != 0 || right.qts != 0 {
			return cmp.Compare(left.qts, right.qts)
		}
		if left.channel != right.channel {
			return cmp.Compare(left.channel, right.channel)
		}
		if left.pts != right.pts {
			return cmp.Compare(left.pts, right.pts)
		}
		return cmp.Compare(right.ptsCount, left.ptsCount)
	})
	for _, update := range updates {
		if update == nil {
			continue
		}
		meta := extractUpdateMeta(update)
		meta.recovered = meta.channel == channelID && meta.qts == 0 || channelID == 0 && meta.qts != 0
		c.processWithState(meta, func() { c.dispatchUpdate(update) })
	}
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
		Pts:      fromPts,
		PtsLimit: limit,
		Date:     c.dispatcher.GetDate(),
		Qts:      c.dispatcher.GetQts(),
		QtsLimit: limit,
	}

	if req.Date == 0 {
		req.Date = int32(time.Now().Unix())
	}

	c.dispatcher.stopMu.Lock()
	stop := c.dispatcher.stopChan
	c.dispatcher.stopMu.Unlock()
	for {
		select {
		case <-stop:
			return
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		updates, err := c.MTProto.MakeRequest(ctx, req)
		cancel()

		if err != nil {
			c.Log.Debug("difference request failed: %v", err)
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
				switch msg := message.(type) {
				case *MessageObj, *MessageService:
					c.dispatchUpdate(&UpdateNewMessage{Message: msg})
					totalFetched++
				}
			}

			c.applyRecoveredUpdates(u.OtherUpdates, 0)
			totalFetched += len(u.OtherUpdates)

			for _, message := range u.NewEncryptedMessages {
				c.dispatchUpdate(&UpdateNewEncryptedMessage{Message: message})
				totalFetched++
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
					c.dispatchUpdate(&UpdateNewMessage{Message: msg})
					totalFetched++
				}
			}

			c.applyRecoveredUpdates(u.OtherUpdates, 0)
			totalFetched += len(u.OtherUpdates)

			for _, message := range u.NewEncryptedMessages {
				c.dispatchUpdate(&UpdateNewEncryptedMessage{Message: message})
				totalFetched++
			}

			c.dispatcher.SetPts(u.IntermediateState.Pts)
			c.dispatcher.SetQts(u.IntermediateState.Qts)
			c.dispatcher.SetSeq(u.IntermediateState.Seq)
			c.dispatcher.SetDate(u.IntermediateState.Date)

			if u.IntermediateState.Pts <= req.Pts && u.IntermediateState.Qts <= req.Qts && u.IntermediateState.Date <= req.Date {
				c.Log.Debug("difference slice did not advance state (pts=%d, qts=%d)", req.Pts, req.Qts)
				return
			}
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
}

func (c *Client) manageSeq(seq int32, seqStart int32, apply func()) bool {
	if seqStart == 0 {
		apply()
		if seq != 0 {
			c.dispatcher.SetSeq(seq)
		}
		return true
	}
	if seqStart < 0 || seq < seqStart {
		return false
	}
	return c.dispatcher.globalSeqBox.process(seq, seq-seqStart+1, false, apply)
}

func (c *Client) GetDifference(Pts, Limit int32) (Message, error) {
	updates, err := c.UpdatesGetDifference(&UpdatesGetDifferenceParams{
		Pts:      Pts - 1,
		PtsLimit: Limit,
		Date:     int32(time.Now().Unix()),
		Qts:      0,
		QtsLimit: Limit,
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
		if len(u.NewMessages) > 0 {
			return u.NewMessages[0], nil
		}

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

	var accessHash int64
	c.dispatcher.RLock()
	if state := c.dispatcher.channelStates[channelID]; state != nil {
		accessHash = state.accessHash
	}
	c.dispatcher.RUnlock()

	if accessHash == 0 {
		channel := c.getChannel(&PeerChannel{ChannelID: channelID})
		if channel != nil {
			accessHash = channel.AccessHash
		} else {
			c.Log.Error("channel difference failed: no access hash (channel=%d)", channelID)
			return
		}
	}

	req := &UpdatesGetChannelDifferenceParams{
		Force:   false,
		Channel: &InputChannelObj{ChannelID: channelID, AccessHash: accessHash},
		Filter:  &ChannelMessagesFilterEmpty{},
		Pts:     fromPts,
		Limit:   limit,
	}

	c.dispatcher.stopMu.Lock()
	stop := c.dispatcher.stopChan
	c.dispatcher.stopMu.Unlock()
	for {
		select {
		case <-stop:
			return
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		diff, err := c.MTProto.MakeRequest(ctx, req)
		cancel()

		if err != nil {
			c.Log.Debug("channel difference request failed (channel=%d): %v", channelID, err)
			return
		}

		switch d := diff.(type) {
		case *UpdatesChannelDifferenceEmpty:
			c.dispatcher.SetChannelPts(channelID, d.Pts)
			if d.Final || d.Pts <= req.Pts {
				return
			}
			req.Pts = d.Pts

		case *UpdatesChannelDifferenceObj:
			c.Cache.UpdatePeersToCache(d.Users, d.Chats)

			for _, message := range d.NewMessages {
				switch msg := message.(type) {
				case *MessageObj:
					c.dispatchUpdate(&UpdateNewChannelMessage{Message: msg})
				case *MessageService:
					c.dispatchUpdate(&UpdateNewChannelMessage{Message: msg})
				}
			}

			c.applyRecoveredUpdates(d.OtherUpdates, channelID)

			c.dispatcher.SetChannelPts(channelID, d.Pts)

			if d.Final {
				return
			}
			if d.Pts <= req.Pts {
				c.Log.Debug("channel difference did not advance pts (channel=%d, pts=%d)", channelID, req.Pts)
				return
			}

			req.Pts = d.Pts

		case *UpdatesChannelDifferenceTooLong:
			c.Cache.UpdatePeersToCache(d.Users, d.Chats)
			for _, message := range d.Messages {
				switch msg := message.(type) {
				case *MessageObj, *MessageService:
					c.dispatchUpdate(&UpdateNewChannelMessage{Message: msg})
				}
			}

			if dialogChannel, ok := d.Dialog.(*DialogObj); ok {
				c.dispatcher.SetChannelPts(channelID, dialogChannel.Pts)
				c.Log.Debug("channel difference too long, refreshing state (channel=%d, pts=%d)", channelID, dialogChannel.Pts)

				if !d.Final && dialogChannel.Pts > req.Pts {
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
}

// OpenChat starts active polling for a channel to receive updates faster.
// timeoutSeconds is the delay before the first poll when channel state is known.
// Subsequent polls follow the server's timeout, or one second if it is absent.
func (c *Client) OpenChat(channel *InputChannelObj, timeoutSeconds int32) {
	if c == nil || c.dispatcher == nil || channel == nil {
		return
	}
	c.backgroundMu.Lock()
	defer c.backgroundMu.Unlock()
	select {
	case <-c.stopCh:
		return
	default:
	}

	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()
	if c.dispatcher.openChats == nil {
		c.dispatcher.openChats = make(map[int64]*openChat)
	}
	if _, ok := c.dispatcher.openChats[channel.ChannelID]; ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	chat := &openChat{accessHash: channel.AccessHash, ctx: ctx, cancel: cancel}
	c.dispatcher.openChats[channel.ChannelID] = chat
	if c.dispatcher.channelStates == nil {
		c.dispatcher.channelStates = make(map[int64]*channelState)
	}
	if state, ok := c.dispatcher.channelStates[channel.ChannelID]; ok {
		state.isOpen = true
		state.accessHash = channel.AccessHash
	} else {
		c.dispatcher.channelStates[channel.ChannelID] = &channelState{
			accessHash: channel.AccessHash,
			isOpen:     true,
		}
	}

	go c.pollOpenChat(channel.ChannelID, chat, timeoutSeconds)
}

// pollOpenChat periodically fetches channel difference for an open chat
func (c *Client) pollOpenChat(channelID int64, chat *openChat, timeoutSeconds int32) {
	d := c.dispatcher
	lastPts := d.GetChannelPts(channelID)
	delay := time.Duration(max(timeoutSeconds, 1)) * time.Second
	if lastPts == 0 {
		lastPts = 1
		delay = 0
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	var errorCount int
	const maxBackoff = 60 // max 60 seconds between retries on error

	for {
		select {
		case <-chat.ctx.Done():
			return
		case <-timer.C:
		}

		lastPts = max(lastPts, d.GetChannelPts(channelID))
		ctx, cancel := context.WithTimeout(chat.ctx, 30*time.Second)
		diff, err := c.MTProto.MakeRequest(ctx, &UpdatesGetChannelDifferenceParams{
			Channel: &InputChannelObj{ChannelID: channelID, AccessHash: chat.accessHash},
			Filter:  &ChannelMessagesFilterEmpty{},
			Pts:     lastPts,
			Limit:   100,
		})
		cancel()
		if chat.ctx.Err() != nil {
			return
		}
		if err != nil {
			errorCount++
			c.Log.Debug("channel poll error (channel=%d, attempt=%d): %v", channelID, errorCount, err)
			timer.Reset(time.Duration(min(1<<min(errorCount, 6), maxBackoff)) * time.Second)
			continue
		}
		errorCount = 0

		var messages []Message
		var updates []Update
		var pts, timeout int32
		var final bool
		switch result := diff.(type) {
		case *UpdatesChannelDifferenceEmpty:
			pts, timeout, final = result.Pts, result.Timeout, result.Final
		case *UpdatesChannelDifferenceObj:
			c.Cache.UpdatePeersToCache(result.Users, result.Chats)
			messages, updates = result.NewMessages, result.OtherUpdates
			pts, timeout, final = result.Pts, result.Timeout, result.Final
		case *UpdatesChannelDifferenceTooLong:
			c.Cache.UpdatePeersToCache(result.Users, result.Chats)
			messages = result.Messages
			timeout, final = result.Timeout, result.Final
			if dialog, ok := result.Dialog.(*DialogObj); ok {
				pts = dialog.Pts
			}
		default:
			c.Log.Debug("unhandled channel poll difference: %T (channel=%d)", diff, channelID)
			return
		}
		for _, msg := range messages {
			switch msg.(type) {
			case *MessageObj, *MessageService:
				c.dispatchUpdate(&UpdateNewChannelMessage{Message: msg})
			}
		}
		c.applyRecoveredUpdates(updates, channelID)
		d.SetChannelPts(channelID, pts)

		delay = 0
		if final {
			delay = time.Duration(max(timeout, 1)) * time.Second
		} else if pts <= lastPts {
			delay = time.Second
		}
		lastPts = max(lastPts, pts)
		timer.Reset(delay)
	}
}

// CloseChat stops active polling for a channel when user leaves it.
func (c *Client) CloseChat(channel *InputChannelObj) {
	if c == nil || c.dispatcher == nil || channel == nil {
		return
	}
	c.dispatcher.Lock()
	defer c.dispatcher.Unlock()

	if c.dispatcher.openChats == nil {
		return
	}
	chat, ok := c.dispatcher.openChats[channel.ChannelID]
	if !ok {
		return
	}
	chat.cancel()
	delete(c.dispatcher.openChats, channel.ChannelID)
	// Mark channel as closed
	if state, ok := c.dispatcher.channelStates[channel.ChannelID]; ok {
		state.isOpen = false
	}
}

func (c *Client) monitorNoUpdatesTimeout(d *UpdateDispatcher, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastDifference := time.Now()
	var boxes []*counterBox
	for {
		select {
		case now := <-ticker.C:
			select {
			case <-stop:
				return
			default:
			}
			if !c.clientData.disableGapFetch {
				d.RLock()
				boxes = append(boxes, d.globalPtsBox, d.globalQtsBox, d.globalSeqBox)
				for _, box := range d.channelPtsBoxes {
					boxes = append(boxes, box)
				}
				d.RUnlock()
				for _, box := range boxes {
					if box == nil {
						continue
					}
					box.Lock()
					if len(box.pending) > 0 {
						var target int32
						for counter := range box.pending {
							target = max(target, counter)
						}
						box.triggerGapLocked(box.current, target)
					}
					box.Unlock()
				}
				clear(boxes)
				boxes = boxes[:0]
			}
			if now.Sub(d.getLastUpdateTime()) > 15*time.Minute && now.Sub(lastDifference) >= 15*time.Minute {
				c.Log.Debug("no updates for 15 minutes, fetching difference")
				if c.dispatchInternal(func() { c.FetchDifference(d.GetPts(), 5000) }) {
					lastDifference = now
				}
			}
		case <-stop:
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
func (c *Client) OnCallback(pattern any, handler CallbackHandler, filters ...Filter) *CallbackHandleBuilder {
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
func (c *Client) OnInlineQuery(pattern any, handler func(m *InlineQuery) error) Handle {
	return c.AddInlineHandler(pattern, handler)
}

// OnInlineCallback registers an inline callback handler and returns a handle
func (c *Client) OnInlineCallback(pattern any, handler func(m *InlineCallbackQuery) error) Handle {
	return c.AddInlineCallbackHandler(pattern, handler)
}

// OnEdit registers an edit handler and returns a handle
func (c *Client) OnEdit(pattern any, handler func(m *NewMessage) error, filters ...Filter) Handle {
	return c.AddEditHandler(pattern, handler, filters...)
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

// updateTaskPool bounds both runnable callbacks and retained updates. Idle workers
// are reused until the pool drains or stops. Restart reuses the worker count, so a callback that blocks
// forever cannot create another worker on every disconnect/reconnect cycle.
type updateTaskPool struct {
	mu         sync.Mutex
	ready      *sync.Cond
	space      chan struct{}
	queue      []func()
	head       int
	active     int
	workers    int
	limit      int
	capacity   int
	closed     bool
	draining   bool
	dropped    uint64
	generation uint64
}

func newUpdateTaskPool(workers, capacity int) *updateTaskPool {
	if workers <= 0 {
		workers = 64
	}
	if capacity <= 0 {
		capacity = 10000
	}
	p := &updateTaskPool{limit: workers, capacity: capacity}
	p.ready = sync.NewCond(&p.mu)

	return p
}

// Waiting is reserved for independent producers; pool workers and the network
// reader must remain nonblocking so callbacks can make RPCs and schedule work.
func (p *updateTaskPool) submit(stop <-chan struct{}, fn func()) (accepted bool, dropped uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	generation := p.generation
	for {
		select {
		case <-stop:
			return false, 0
		default:
		}
		if p.closed || generation != p.generation {
			return false, 0
		}
		if p.workers < p.limit && p.active+len(p.queue)-p.head >= p.workers {
			p.workers++
			p.active++
			go p.run(fn)
			return true, 0
		}
		if len(p.queue)-p.head < p.capacity {
			break
		}
		if stop == nil {
			p.dropped++
			return false, p.dropped
		}
		if p.space == nil {
			p.space = make(chan struct{})
		}
		space := p.space
		p.mu.Unlock()
		select {
		case <-space:
		case <-stop:
		}
		p.mu.Lock()
	}
	if p.head > 0 && len(p.queue) >= p.capacity {
		n := copy(p.queue, p.queue[p.head:])
		clear(p.queue[n:])
		p.queue = p.queue[:n]
		p.head = 0
	}
	p.queue = append(p.queue, fn)
	p.ready.Signal()
	return true, 0
}

func (p *updateTaskPool) run(fn func()) {
	active := true
	defer func() {
		p.mu.Lock()
		if active {
			p.active--
		}
		p.workers--
		if !p.closed && p.head < len(p.queue) {
			next := p.queue[p.head]
			p.queue[p.head] = nil
			p.head++
			p.workers++
			p.active++
			go p.run(next)
		}
		if p.space != nil {
			close(p.space)
			p.space = nil
		}
		p.mu.Unlock()
	}()
	for {
		fn()
		fn = nil
		p.mu.Lock()
		p.active--
		active = false
		for !p.closed && !p.draining && p.head == len(p.queue) {
			p.queue = nil
			p.head = 0
			p.ready.Wait()
		}
		if p.closed || p.head == len(p.queue) {
			if p.head == len(p.queue) {
				p.queue = nil
				p.head = 0
			}
			p.mu.Unlock()
			return
		}
		fn = p.queue[p.head]
		p.queue[p.head] = nil
		p.head++
		if p.space != nil {
			close(p.space)
			p.space = nil
		}
		p.active++
		active = true
		p.mu.Unlock()
	}
}

func (p *updateTaskPool) stop(discard bool) {
	p.mu.Lock()
	p.draining = true
	if discard {
		p.closed = true
		p.generation++
		p.queue = nil
		p.head = 0
	}
	p.ready.Broadcast()
	if p.space != nil {
		close(p.space)
		p.space = nil
	}
	p.mu.Unlock()
}

func (p *updateTaskPool) restart() {
	p.mu.Lock()
	p.closed = false
	p.draining = false
	p.ready.Broadcast()
	if p.space != nil {
		close(p.space)
		p.space = nil
	}
	p.mu.Unlock()
}

func (c *Client) dispatchAsync(fn func()) bool {
	return c.submitUpdateTask(fn, 0, nil)
}

func (c *Client) dispatchInternal(fn func()) bool {
	return c.submitUpdateTask(fn, 1, nil)
}

func (c *Client) submitUpdateTask(fn func(), kind int, stop <-chan struct{}) bool {
	if c == nil || c.dispatcher == nil {
		return false
	}
	d := c.dispatcher
	d.stopMu.Lock()
	if d.stopped {
		d.stopMu.Unlock()
		return false
	}
	pool := &d.tasks
	workers := c.clientData.updateWorkers
	switch kind {
	case 1:
		pool = &d.preparations
		workers = 4
	case 2:
		pool = &d.secretUpdates
		workers = 1
	}
	if *pool == nil {
		*pool = newUpdateTaskPool(workers, c.clientData.updateQueueSize)
		select {
		case <-d.stopChan:
			(*pool).draining = true
		default:
		}
	}
	p := *pool
	d.stopMu.Unlock()
	accepted, dropped := p.submit(stop, func() {
		defer c.NewRecovery()()
		fn()
	})
	if dropped != 0 && dropped&(dropped-1) == 0 {
		c.Log.Warn("update queue full: dropped %d tasks; tune UpdateWorkers/UpdateQueueSize or reduce handler latency", dropped)
	}
	return accepted
}

func copyHandlerGroups[T any](dst, src map[int][]T) {
	for group, handlers := range src {
		dst[group] = slices.Clone(handlers)
	}
}

func (c *Client) dispatchHandlerGroup(group int, run func()) {
	if group == ConversationGroup || group == DefaultGroup {
		run()
	} else {
		c.dispatchAsync(run)
	}
}
