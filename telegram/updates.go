// Copyright (c) 2024, amarnathcjd

package telegram

import (
	"context"
	"fmt"
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

type channelState struct {
	pts        int32
	accessHash int64
	isOpen     bool
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
	lastUpdateTime        time.Time
	currentPts            int32
	currentQts            int32
	currentSeq            int32
	currentDate           int32
	channelStates         map[int64]*channelState
	pendingGaps           map[int32]time.Time
	pendingChannelGaps    map[int64]map[int32]time.Time
	processedUpdates      map[int64]time.Time
	recoveringDifference  bool
	recoveringChannels    map[int64]bool
	stopChan              chan struct{}
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

func (d *UpdateDispatcher) SetQts(qts int32) {
	d.Lock()
	defer d.Unlock()
	d.currentQts = qts
}

func (d *UpdateDispatcher) GetQts() int32 {
	d.RLock()
	defer d.RUnlock()
	return d.currentQts
}

func (d *UpdateDispatcher) SetSeq(seq int32) {
	d.Lock()
	defer d.Unlock()
	d.currentSeq = seq
}

func (d *UpdateDispatcher) GetSeq() int32 {
	d.RLock()
	defer d.RUnlock()
	return d.currentSeq
}

func (d *UpdateDispatcher) SetDate(date int32) {
	d.Lock()
	defer d.Unlock()
	d.currentDate = date
}

func (d *UpdateDispatcher) GetDate() int32 {
	d.RLock()
	defer d.RUnlock()
	return d.currentDate
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

func (d *UpdateDispatcher) UpdateLastUpdateTime() {
	d.Lock()
	defer d.Unlock()
	d.lastUpdateTime = time.Now()
}

func (d *UpdateDispatcher) IsUpdateProcessed(updateID int64) bool {
	d.RLock()
	defer d.RUnlock()
	if d.processedUpdates == nil {
		return false
	}
	_, exists := d.processedUpdates[updateID]
	return exists
}

func (d *UpdateDispatcher) MarkUpdateProcessed(updateID int64) {
	d.Lock()
	defer d.Unlock()
	if d.processedUpdates == nil {
		d.processedUpdates = make(map[int64]time.Time)
	}
	d.processedUpdates[updateID] = time.Now()

	if len(d.processedUpdates) > 1000 {
		oldest := time.Now()
		var oldestID int64
		for id, t := range d.processedUpdates {
			if t.Before(oldest) {
				oldest = t
				oldestID = id
			}
		}
		delete(d.processedUpdates, oldestID)
	}
}

func (d *UpdateDispatcher) CleanOldProcessedUpdates() {
	d.Lock()
	defer d.Unlock()
	if d.processedUpdates == nil {
		return
	}
	cutoff := time.Now().Add(-5 * time.Minute)
	for id, t := range d.processedUpdates {
		if t.Before(cutoff) {
			delete(d.processedUpdates, id)
		}
	}
}

func (c *Client) NewUpdateDispatcher(sessionName ...string) {
	c.dispatcher = &UpdateDispatcher{
		logger: utils.NewLogger("gogram " + getLogPrefix("dispatcher", getVariadic(sessionName, ""))).
			SetLevel(c.Log.Lev()).
			NoColor(!c.Log.Color()),
		channelStates:    make(map[int64]*channelState),
		pendingGaps:      make(map[int32]time.Time),
		processedUpdates: make(map[int64]time.Time),
		stopChan:         make(chan struct{}),
		lastUpdateTime:   time.Now(),
	}
	c.dispatcher.SortTrigger()
	c.dispatcher.logger.Debug("dispatcher initialized")

	go c.monitorNoUpdatesTimeout()
	go c.cleanupProcessedUpdates()
}

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

func (c *Client) handleMessageUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		updateID := int64(msg.ID)
		if msg.PeerID != nil {
			if peer, ok := msg.PeerID.(*PeerChannel); ok {
				updateID = (int64(peer.ChannelID) << 32) | int64(msg.ID)
			} else if peer, ok := msg.PeerID.(*PeerChat); ok {
				updateID = (int64(peer.ChatID) << 32) | int64(msg.ID)
			} else if peer, ok := msg.PeerID.(*PeerUser); ok {
				updateID = (int64(peer.UserID) << 32) | int64(msg.ID)
			}
		}

		if c.dispatcher.IsUpdateProcessed(updateID) {
			return
		}
		c.dispatcher.MarkUpdateProcessed(updateID)

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
	//if !c.managePtsFast(pts, 1) {
		//return
	//}

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

		updatedMessage, err := c.GetDifference(pts-1, 1)
		if err != nil {
			c.Log.Error(errors.Wrap(err, "[GET_DIFF] failed to get difference"))
		}
		if updatedMessage != nil {
			c.handleMessageUpdate(updatedMessage)
		}
	}
}

func (c *Client) handleEditUpdate(update Message) {
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

func (c *Client) handleDeleteUpdate(update Update) {
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

		if after, ok := strings.CutPrefix(Pattern, "cmd:"); ok {
			prefixes := c.CommandPrefixes()
			if prefixes == "" {
				prefixes = "/!"
			}
			escapedPrefixes := regexp.QuoteMeta(prefixes)
			Pattern = "(?i)^[" + escapedPrefixes + "]" + regexp.QuoteMeta(after)
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
			// Chat type filters
			if filter.Private && !m.IsPrivate() || filter.Group && !m.IsGroup() || filter.Channel && !m.IsChannel() {
				return false
			}

			// Message type filters
			if filter.Media && !m.IsMedia() || filter.Command && !m.IsCommand() || filter.Reply && !m.IsReply() || filter.Forward && !m.IsForward() {
				return false
			}

			// Direction filters
			if filter.Outgoing && (m.Message == nil || !m.Message.Out) {
				return false
			}
			if filter.Incoming && (m.Message == nil || m.Message.Out) {
				return false
			}

			// Sender filters
			if filter.FromBot {
				if m.Sender == nil || !m.Sender.Bot {
					return false
				}
			}

			// Length filters
			if filter.MinLength > 0 && len(m.Text()) < filter.MinLength {
				return false
			}
			if filter.MaxLength > 0 && len(m.Text()) > filter.MaxLength {
				return false
			}

			// Text content filter
			if filter.HasText && m.Text() == "" {
				return false
			}

			// Specific media type filters
			if filter.HasPhoto && m.Photo() == nil {
				return false
			}
			if filter.HasVideo && m.Video() == nil {
				return false
			}
			if filter.HasDocument && m.Document() == nil {
				return false
			}
			if filter.HasAudio && m.Audio() == nil {
				return false
			}
			if filter.HasSticker && m.Sticker() == nil {
				return false
			}
			if filter.HasAnimation {
				doc := m.Document()
				if doc == nil {
					return false
				}
				isAnimation := false
				for _, attr := range doc.Attributes {
					if _, ok := attr.(*DocumentAttributeAnimated); ok {
						isAnimation = true
						break
					}
				}
				if !isAnimation {
					return false
				}
			}
			if filter.HasVoice && m.Voice() == nil {
				return false
			}
			if filter.HasVideoNote {
				doc := m.Document()
				if doc == nil {
					return false
				}
				isVideoNote := false
				for _, attr := range doc.Attributes {
					if _, ok := attr.(*DocumentAttributeVideo); ok {
						if videoAttr, okVideo := attr.(*DocumentAttributeVideo); okVideo && videoAttr.RoundMessage {
							isVideoNote = true
							break
						}
					}
				}
				if !isVideoNote {
					return false
				}
			}
			if filter.HasContact && m.Contact() == nil {
				return false
			}
			if filter.HasLocation {
				if msgMedia, ok := m.Media().(*MessageMediaGeo); !ok || msgMedia == nil {
					return false
				}
			}
			if filter.HasVenue {
				if msgMedia, ok := m.Media().(*MessageMediaVenue); !ok || msgMedia == nil {
					return false
				}
			}
			if filter.HasPoll {
				if msgMedia, ok := m.Media().(*MessageMediaPoll); !ok || msgMedia == nil {
					return false
				}
			}

			// Edited filter
			if filter.Edited && (m.Message == nil || m.Message.EditDate == 0) {
				return false
			}

			// Custom media types filter
			if len(filter.MediaTypes) > 0 {
				hasMatchingMedia := false
				currentMediaType := m.MediaType()
				for _, mediaType := range filter.MediaTypes {
					mtLower := strings.ToLower(mediaType)
					switch mtLower {
					case "photo":
						if m.Photo() != nil {
							hasMatchingMedia = true
						}
					case "video":
						if m.Video() != nil {
							hasMatchingMedia = true
						}
					case "document":
						if m.Document() != nil {
							hasMatchingMedia = true
						}
					case "audio":
						if m.Audio() != nil {
							hasMatchingMedia = true
						}
					case "voice":
						if m.Voice() != nil {
							hasMatchingMedia = true
						}
					case "sticker":
						if m.Sticker() != nil {
							hasMatchingMedia = true
						}
					case "animation", "gif":
						doc := m.Document()
						if doc != nil {
							for _, attr := range doc.Attributes {
								if _, ok := attr.(*DocumentAttributeAnimated); ok {
									hasMatchingMedia = true
									break
								}
							}
						}
					case "videonote", "video_note":
						doc := m.Document()
						if doc != nil {
							for _, attr := range doc.Attributes {
								if videoAttr, ok := attr.(*DocumentAttributeVideo); ok && videoAttr.RoundMessage {
									hasMatchingMedia = true
									break
								}
							}
						}
					case "contact":
						if m.Contact() != nil {
							hasMatchingMedia = true
						}
					case "geo", "location":
						if currentMediaType == "geo" || currentMediaType == "geo_live" {
							hasMatchingMedia = true
						}
					case "venue":
						if currentMediaType == "venue" {
							hasMatchingMedia = true
						}
					case "poll":
						if _, ok := m.Media().(*MessageMediaPoll); ok {
							hasMatchingMedia = true
						}
					}
					if hasMatchingMedia {
						break
					}
				}
				if !hasMatchingMedia {
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
	// Advanced filters
	Outgoing, Incoming                                 bool     // Outgoing or incoming messages
	MinLength, MaxLength                               int      // Message text length constraints
	HasText, HasPhoto, HasVideo, HasDocument, HasAudio bool     // Specific media type filters
	HasSticker, HasAnimation, HasVoice, HasVideoNote   bool     // More media types
	HasContact, HasLocation, HasVenue, HasPoll         bool     // Service message types
	Edited                                             bool     // Only edited messages
	MediaTypes                                         []string // Custom media type list (e.g., ["photo", "video"])
}

func (f Filter) IsPrivate() Filter                                    { f.Private = true; return f }
func (f Filter) IsGroup() Filter                                      { f.Group = true; return f }
func (f Filter) IsChannel() Filter                                    { f.Channel = true; return f }
func (f Filter) IsMedia() Filter                                      { f.Media = true; return f }
func (f Filter) IsCommand() Filter                                    { f.Command = true; return f }
func (f Filter) IsReply() Filter                                      { f.Reply = true; return f }
func (f Filter) IsForward() Filter                                    { f.Forward = true; return f }
func (f Filter) IsFromBot() Filter                                    { f.FromBot = true; return f }
func (f Filter) IsMention() Filter                                    { f.Mention = true; return f }
func (f Filter) IsOutgoing() Filter                                   { f.Outgoing = true; return f }
func (f Filter) IsIncoming() Filter                                   { f.Incoming = true; return f }
func (f Filter) IsEdited() Filter                                     { f.Edited = true; return f }
func (f Filter) WithPhoto() Filter                                    { f.HasPhoto = true; return f }
func (f Filter) WithVideo() Filter                                    { f.HasVideo = true; return f }
func (f Filter) WithDocument() Filter                                 { f.HasDocument = true; return f }
func (f Filter) WithAudio() Filter                                    { f.HasAudio = true; return f }
func (f Filter) WithSticker() Filter                                  { f.HasSticker = true; return f }
func (f Filter) WithAnimation() Filter                                { f.HasAnimation = true; return f }
func (f Filter) WithVoice() Filter                                    { f.HasVoice = true; return f }
func (f Filter) WithVideoNote() Filter                                { f.HasVideoNote = true; return f }
func (f Filter) WithContact() Filter                                  { f.HasContact = true; return f }
func (f Filter) WithLocation() Filter                                 { f.HasLocation = true; return f }
func (f Filter) WithVenue() Filter                                    { f.HasVenue = true; return f }
func (f Filter) WithPoll() Filter                                     { f.HasPoll = true; return f }
func (f Filter) WithText() Filter                                     { f.HasText = true; return f }
func (f Filter) FromUsers(users ...int64) Filter                      { f.Users = users; return f }
func (f Filter) FromChats(chats ...int64) Filter                      { f.Chats = chats; return f }
func (f Filter) FromChannels(channels ...int64) Filter                { f.Channels = channels; return f }
func (f Filter) MinLen(length int) Filter                             { f.MinLength = length; return f }
func (f Filter) MaxLen(length int) Filter                             { f.MaxLength = length; return f }
func (f Filter) WithMediaTypes(types ...string) Filter                { f.MediaTypes = types; return f }
func (f Filter) AsBlacklist() Filter                                  { f.Blacklist = true; return f }
func (f Filter) Custom(fn func(m *NewMessage) bool) Filter            { f.Func = fn; return f }
func (f Filter) CustomCallback(fn func(c *CallbackQuery) bool) Filter { f.FuncCallback = fn; return f }

func NewFilter() Filter { return Filter{} }

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
	FilterOutgoing  = Filter{Outgoing: true}
	FilterIncoming  = Filter{Incoming: true}
	FilterEdited    = Filter{Edited: true}
	FilterPhoto     = Filter{HasPhoto: true}
	FilterVideo     = Filter{HasVideo: true}
	FilterDocument  = Filter{HasDocument: true}
	FilterAudio     = Filter{HasAudio: true}
	FilterSticker   = Filter{HasSticker: true}
	FilterAnimation = Filter{HasAnimation: true}
	FilterVoice     = Filter{HasVoice: true}
	FilterVideoNote = Filter{HasVideoNote: true}
	FilterContact   = Filter{HasContact: true}
	FilterLocation  = Filter{HasLocation: true}
	FilterVenue     = Filter{HasVenue: true}
	FilterPoll      = Filter{HasPoll: true}

	FilterUsers = func(users ...int64) Filter {
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
		return Filter{FuncCallback: f}
	}
	FilterFuncInline = func(f func(c *InlineQuery) bool) Filter {
		return Filter{Func: func(m *NewMessage) bool { return true }}
	}
	FilterMinLength = func(length int) Filter {
		return Filter{MinLength: length}
	}
	FilterMaxLength = func(length int) Filter {
		return Filter{MaxLength: length}
	}
	FilterMediaTypes = func(types ...string) Filter {
		return Filter{MediaTypes: types}
	}
	// Combine multiple filters with AND logic
	FilterAnd = func(filters ...Filter) Filter {
		return Filter{
			Func: func(m *NewMessage) bool {
				for _, f := range filters {
					handle := &messageHandle{Filters: []Filter{f}}
					if !handle.runFilterChain(m, []Filter{f}) {
						return false
					}
				}
				return true
			},
		}
	}
	// Combine multiple filters with OR logic
	FilterOr = func(filters ...Filter) Filter {
		return Filter{
			Func: func(m *NewMessage) bool {
				for _, f := range filters {
					handle := &messageHandle{Filters: []Filter{f}}
					if handle.runFilterChain(m, []Filter{f}) {
						return true
					}
				}
				return false
			},
		}
	}
	// Negate a filter
	FilterNot = func(filter Filter) Filter {
		return Filter{
			Func: func(m *NewMessage) bool {
				handle := &messageHandle{Filters: []Filter{filter}}
				return !handle.runFilterChain(m, []Filter{filter})
			},
		}
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
				if c.managePts(update.Pts, update.PtsCount) {
					go c.handleMessageUpdate(update.Message)
				}
			case *UpdateNewChannelMessage:
				channelID := getChannelIDFromMessage(update.Message)
				if channelID != 0 {
					go c.handleMessageUpdate(update.Message)
					c.manageChannelPts(channelID, update.Pts, update.PtsCount)
				} else {
					if c.managePts(update.Pts, update.PtsCount) {
						go c.handleMessageUpdate(update.Message)
					}
				}
			case *UpdateNewScheduledMessage:
				go c.handleMessageUpdate(update.Message)
			case *UpdateEditMessage:
				if c.managePts(update.Pts, update.PtsCount) {
					go c.handleEditUpdate(update.Message)
				}
			case *UpdateEditChannelMessage:
				channelID := getChannelIDFromMessage(update.Message)
				if channelID != 0 {
					go c.handleEditUpdate(update.Message)
					c.manageChannelPts(channelID, update.Pts, update.PtsCount)
				} else {
					if c.managePts(update.Pts, update.PtsCount) {
						go c.handleEditUpdate(update.Message)
					}
				}
			case *UpdateDeleteMessages:
				if c.managePts(update.Pts, update.PtsCount) {
					go c.handleDeleteUpdate(update)
				}
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
			case *UpdatePendingJoinRequests:
				go c.handleJoinRequestUpdate(update)
			case *UpdateBotInlineSend:
				go c.handleInlineSendUpdate(update)
			case *UpdateChannelTooLong:
				currentPts := c.dispatcher.GetChannelPts(update.ChannelID)
				if update.Pts != 0 {
					currentPts = update.Pts
				}
				go c.FetchChannelDifference(update.ChannelID, currentPts, 50)
			}
			go c.handleRawUpdate(update)
		}
	case *UpdateShort:
		switch upd := upd.Update.(type) {
		case *UpdateNewMessage:
			go c.handleMessageUpdateWith(upd.Message, upd.Pts)
		case *UpdateNewChannelMessage:
			channelID := getChannelIDFromMessage(upd.Message)
			if channelID != 0 {
				go c.handleMessageUpdate(upd.Message)
				c.manageChannelPts(channelID, upd.Pts, upd.PtsCount)
			} else {
				go c.handleMessageUpdateWith(upd.Message, upd.Pts)
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
		go c.handleMessageUpdateWith(&MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.UserID), PeerID: getPeerUser(upd.UserID), Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}, upd.Pts)
	case *UpdateShortChatMessage:
		go c.handleMessageUpdateWith(&MessageObj{ID: upd.ID, Out: upd.Out, Mentioned: upd.Mentioned, Message: upd.Message, MediaUnread: upd.MediaUnread, FromID: getPeerUser(upd.FromID), PeerID: &PeerChat{ChatID: upd.ChatID}, Date: upd.Date, Entities: upd.Entities, FwdFrom: upd.FwdFrom, ReplyTo: upd.ReplyTo, ViaBotID: upd.ViaBotID, TtlPeriod: upd.TtlPeriod, Silent: upd.Silent}, upd.Pts)
	case *UpdateShortSentMessage:
		go c.handleMessageUpdateWith(&MessageObj{ID: upd.ID, Out: upd.Out, Date: upd.Date, Media: upd.Media, Entities: upd.Entities, TtlPeriod: upd.TtlPeriod}, upd.Pts)
	case *UpdatesCombined:
		if !c.manageSeq(upd.Seq, upd.SeqStart) {
			return false
		}

		u = upd.Updates
		go c.Cache.UpdatePeersToCache(upd.Users, upd.Chats)
		goto UpdateTypeSwitching
	case *UpdatesTooLong:
		go c.FetchDifference(c.dispatcher.GetPts(), 5000)
	default:
		c.Log.Debug(fmt.Sprintf("unknown update skipped: %v", reflect.TypeOf(upd)))
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
			if ctx.Err() == context.DeadlineExceeded {
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
			c.Log.Debug(fmt.Sprintf("difference too long, refetching state (pts=%d), tried with limit=%d, fetched=%d, %v", u.Pts, limit, totalFetched, c.JSON(req)))
			c.dispatcher.SetPts(u.Pts)

			state, err := c.UpdatesGetState()
			if err != nil {
				c.Log.Error(fmt.Sprintf("get state failed: %v", err))
				return
			}

			c.dispatcher.SetPts(state.Pts)
			c.dispatcher.SetQts(state.Qts)
			c.dispatcher.SetSeq(state.Seq)
			c.dispatcher.SetDate(state.Date)
			return

		default:
			c.Log.Debug(fmt.Sprintf("unknown difference type: %v", reflect.TypeOf(updates)))
			return
		}
	}

	c.Log.Debug(fmt.Sprintf("fetch difference max iterations (iterations=%d, pts=%d, fetched=%d)", maxIterations, req.Pts, totalFetched))
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

func (c *Client) managePtsFast(pts int32, ptsCount int32) bool {
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
		}

		c.dispatcher.SetPts(pts)

		gapPts := expectedPts
		go func() {
			time.Sleep(10 * time.Millisecond)

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
			c.Log.Error(fmt.Sprintf("channel difference failed (channel=%d): no access hash", channelID))
			return
		}
	}

	totalFetched := 0
	maxIterations := 20
	iteration := 0

	defer func() {
		if totalFetched > 0 {
			// Fetch channel difference complete
		}
	}()

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
			if ctx.Err() == context.DeadlineExceeded {
				c.Log.Debug(fmt.Sprintf("channel difference timeout, retrying (channel=%d, iteration=%d, pts=%d)", channelID, iteration, req.Pts))
			}
			continue
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
				c.Log.Debug(fmt.Sprintf("channel difference too long, refreshing state (channel=%d, pts=%d, final=%v)", channelID, dialogChannel.Pts, d.Final))

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
			c.Log.Debug(fmt.Sprintf("unknown channel difference type (channel=%d): %v", channelID, reflect.TypeOf(diff)))
			return
		}
	}

	c.Log.Debug(fmt.Sprintf("channel difference max iterations (channel=%d, iterations=%d, pts=%d, fetched=%d)", channelID, maxIterations, req.Pts, totalFetched))
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

type ev any

var (
	OnMessage        ev = "message"
	OnCommand        ev = "command"
	OnCommandShort   ev = "cmd"
	OnEdit           ev = "edit"
	OnDelete         ev = "delete"
	OnAlbum          ev = "album"
	OnInline         ev = "inline"
	OnCallback       ev = "callback"
	OnInlineCallback ev = "inlinecallback"
	OnChosenInline   ev = "choseninline"
	OnParticipant    ev = "participant"
	OnRaw            ev = "raw"
)

// .On is a helper function to add a handler for a specific event type (handler: func(m *NewMessage) error, etc.)
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
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m *NewMessage) error")
		}
	case OnCommand, OnCommandShort:
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if args != "" {
				return c.AddMessageHandler("cmd:"+args, h, filters...)
			}
			return c.AddMessageHandler(OnNewMessage, h, filters...)
		} else {
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m *NewMessage) error")
		}
	case OnEdit, OnEditMessage:
		if h, ok := handler.(func(m *NewMessage) error); ok {
			if args != "" {
				return c.AddEditHandler(args, h)
			}
			return c.AddEditHandler(OnEditMessage, h)
		} else {
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m *NewMessage) error")
		}
	case OnDelete, OnDeleteMessage:
		if h, ok := handler.(func(m *DeleteMessage) error); ok {
			if args != "" {
				return c.AddDeleteHandler(args, h)
			}
			return c.AddDeleteHandler(OnDeleteMessage, h)
		} else {
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m *DeleteMessage) error")
		}
	case OnAlbum:
		if h, ok := handler.(func(m *Album) error); ok {
			return c.AddAlbumHandler(h)
		} else {
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m *Album) error")
		}
	case OnInline, OnInlineQuery:
		if h, ok := handler.(func(m *InlineQuery) error); ok {
			if args != "" {
				return c.AddInlineHandler(args, h)
			}
			return c.AddInlineHandler(OnInlineQuery, h)
		} else {
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m *InlineQuery) error")
		}
	case OnChosenInline:
		if h, ok := handler.(func(m *InlineSend) error); ok {
			return c.AddInlineSendHandler(h)
		} else {
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m *InlineSend) error")
		}
	case OnCallback, OnCallbackQuery:
		if h, ok := handler.(func(m *CallbackQuery) error); ok {
			if args != "" {
				return c.AddCallbackHandler(args, h)
			}
			return c.AddCallbackHandler(OnCallbackQuery, h)
		} else {
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m *CallbackQuery) error")
		}
	case OnInlineCallback, OnInlineCallbackQuery, "inlineCallback":
		if h, ok := handler.(func(m *InlineCallbackQuery) error); ok {
			if args != "" {
				return c.AddInlineCallbackHandler(args, h)
			}
			return c.AddInlineCallbackHandler(OnInlineCallbackQuery, h)
		} else {
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m *InlineCallbackQuery) error")
		}
	case OnParticipant:
		if h, ok := handler.(func(m *ParticipantUpdate) error); ok {
			return c.AddParticipantHandler(h)
		} else {
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m *ParticipantUpdate) error")
		}
	case OnRaw:
		if h, ok := handler.(func(m Update, c *Client) error); ok {
			return c.AddRawHandler(nil, h)
		} else {
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m Update, c *Client) error")
		}
	default:
		if update, ok := pattern.(Update); ok {
			if h, ok := handler.(func(m Update, c *Client) error); ok {
				return c.AddRawHandler(update, h)
			}
		} else {
			c.Log.Error("invalid handler signature - ", reflect.TypeOf(handler), " Expected: func(m Update, c *Client) error")
		}
	}

	return nil
}

func (c *Client) monitorNoUpdatesTimeout() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if time.Since(c.dispatcher.lastUpdateTime) > 15*time.Minute {
				c.Log.Debug("no updates received for 15 minutes, getting difference...")
				c.FetchDifference(c.dispatcher.currentPts, 5000)
			}
		case <-c.dispatcher.stopChan:
			return
		}
	}
}

func (c *Client) cleanupProcessedUpdates() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.dispatcher.CleanOldProcessedUpdates()
		case <-c.dispatcher.stopChan:
			return
		}
	}
}

func (c *Client) FetchDifferenceOnStartup() error {
	// need to store last state locally on disconnect and compare on startup
	return errors.New("TODO: implement FetchDifferenceOnStartup")
}
