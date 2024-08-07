// Copyright (c) 2024, amarnathcjd

package telegram

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/pkg/errors"
)

// TODO: confine sort to when new handler priorities are set.
// way to set prio on adding event handler (this solves todo-1)
// calling endGroup from inside the event handler.
// make it more efficient
// maybe add a channnel to the newMessage,... types as endGroup.

type Handle interface {
	IsMatch(text string) bool
	SetPriority(priority int) *Handle
	SetGroup(group string) *Handle
	GetPriority() int
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

func (c *Client) removeHandle(h interface{}) {
	removeHandleFromSlice := func(handles []interface{}, handle interface{}) []interface{} {
		for i, h := range handles {
			if reflect.DeepEqual(h, handle) {
				return append(handles[:i], handles[i+1:]...)
			}
		}
		return handles
	}

	handleMap := map[reflect.Type]*[]interface{}{
		reflect.TypeOf((*messageHandle)(nil)):        (*[]interface{})(unsafe.Pointer(&c.dispatcher.messageHandles)),
		reflect.TypeOf((*albumHandle)(nil)):          (*[]interface{})(unsafe.Pointer(&c.dispatcher.albumHandles)),
		reflect.TypeOf((*chatActionHandle)(nil)):     (*[]interface{})(unsafe.Pointer(&c.dispatcher.actionHandles)),
		reflect.TypeOf((*messageEditHandle)(nil)):    (*[]interface{})(unsafe.Pointer(&c.dispatcher.messageEditHandles)),
		reflect.TypeOf((*inlineHandle)(nil)):         (*[]interface{})(unsafe.Pointer(&c.dispatcher.inlineHandles)),
		reflect.TypeOf((*callbackHandle)(nil)):       (*[]interface{})(unsafe.Pointer(&c.dispatcher.callbackHandles)),
		reflect.TypeOf((*inlineCallbackHandle)(nil)): (*[]interface{})(unsafe.Pointer(&c.dispatcher.inlineCallbackHandles)),
		reflect.TypeOf((*participantHandle)(nil)):    (*[]interface{})(unsafe.Pointer(&c.dispatcher.participantHandles)),
		reflect.TypeOf((*rawHandle)(nil)):            (*[]interface{})(unsafe.Pointer(&c.dispatcher.rawHandles)),
	}

	handleType := reflect.TypeOf(h)
	if handles, ok := handleMap[handleType]; ok {
		*handles = removeHandleFromSlice(*handles, h)
	}
}

type messageHandle struct {
	Pattern  interface{}
	Handler  MessageHandler
	Filters  []Filter
	Priority int
	Group    string
}

func (h *messageHandle) SetPriority(priority int) *messageHandle {
	h.Priority = priority
	return h
}

func (h *messageHandle) GetPriority() int {
	return h.Priority
}

func (h *messageHandle) SetGroup(group string) *messageHandle {
	h.Group = group
	return h
}

func (h *messageHandle) GetGroup() string {
	return h.Group
}

type albumHandle struct {
	Handler  func(alb *Album) error
	Filters  []Filter
	Priority int
	Group    string
}

func (h *albumHandle) SetPriority(priority int) *albumHandle {
	h.Priority = priority
	return h
}

func (h *albumHandle) GetPriority() int {
	return h.Priority
}

func (h *albumHandle) SetGroup(group string) *albumHandle {
	h.Group = group
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
	Handler  MessageHandler
	Filters  []Filter
	Priority int
	Group    string
}

func (h *chatActionHandle) SetPriority(priority int) *chatActionHandle {
	h.Priority = priority
	return h
}

func (h *chatActionHandle) GetPriority() int {
	return h.Priority
}

func (h *chatActionHandle) SetGroup(group string) *chatActionHandle {
	h.Group = group
	return h
}

func (h *chatActionHandle) GetGroup() string {
	return h.Group
}

type messageEditHandle struct {
	Pattern  interface{}
	Handler  MessageHandler
	Filters  []Filter
	Priority int
	Group    string
}

func (h *messageEditHandle) SetPriority(priority int) *messageEditHandle {
	h.Priority = priority
	return h
}

func (h *messageEditHandle) GetPriority() int {
	return h.Priority
}

func (h *messageEditHandle) SetGroup(group string) *messageEditHandle {
	h.Group = group
	return h
}

func (h *messageEditHandle) GetGroup() string {
	return h.Group
}

type messageDeleteHandle struct {
	Pattern  interface{}
	Handler  func(m *DeleteMessage) error
	Filters  []Filter
	Priority int
	Group    string
}

func (h *messageDeleteHandle) SetPriority(priority int) *messageDeleteHandle {
	h.Priority = priority
	return h
}

func (h *messageDeleteHandle) GetPriority() int {
	return h.Priority
}

func (h *messageDeleteHandle) SetGroup(group string) *messageDeleteHandle {
	h.Group = group
	return h
}

func (h *messageDeleteHandle) GetGroup() string {
	return h.Group
}

type inlineHandle struct {
	Pattern  interface{}
	Handler  InlineHandler
	Filters  []Filter
	Priority int
	Group    string
}

func (h *inlineHandle) SetPriority(priority int) *inlineHandle {
	h.Priority = priority
	return h
}

func (h *inlineHandle) GetPriority() int {
	return h.Priority
}

func (h *inlineHandle) SetGroup(group string) *inlineHandle {
	h.Group = group
	return h
}

func (h *inlineHandle) GetGroup() string {
	return h.Group
}

type callbackHandle struct {
	Pattern  interface{}
	Handler  CallbackHandler
	Filters  []Filter
	Priority int
	Group    string
}

func (h *callbackHandle) SetPriority(priority int) *callbackHandle {
	h.Priority = priority
	return h
}

func (h *callbackHandle) GetPriority() int {
	return h.Priority
}

func (h *callbackHandle) SetGroup(group string) *callbackHandle {
	h.Group = group
	return h
}

func (h *callbackHandle) GetGroup() string {
	return h.Group
}

type inlineCallbackHandle struct {
	Pattern  interface{}
	Handler  InlineCallbackHandler
	Filters  []Filter
	Priority int
	Group    string
}

func (h *inlineCallbackHandle) SetPriority(priority int) *inlineCallbackHandle {
	h.Priority = priority
	return h
}

func (h *inlineCallbackHandle) GetPriority() int {
	return h.Priority
}

func (h *inlineCallbackHandle) SetGroup(group string) *inlineCallbackHandle {
	h.Group = group
	return h
}

func (h *inlineCallbackHandle) GetGroup() string {
	return h.Group
}

type participantHandle struct {
	Handler  func(p *ParticipantUpdate) error
	Filters  []Filter
	Priority int
	Group    string
}

func (h *participantHandle) SetPriority(priority int) *participantHandle {
	h.Priority = priority
	return h
}

func (h *participantHandle) GetPriority() int {
	return h.Priority
}

func (h *participantHandle) SetGroup(group string) *participantHandle {
	h.Group = group
	return h
}

func (h *participantHandle) GetGroup() string {
	return h.Group
}

type rawHandle struct {
	updateType Update
	Handler    RawHandler
	Filters    []Filter
	Priority   int
	Group      string
}

func (h *rawHandle) SetPriority(priority int) *rawHandle {
	h.Priority = priority
	return h
}

func (h *rawHandle) GetPriority() int {
	return h.Priority
}

func (h *rawHandle) SetGroup(group string) *rawHandle {
	h.Group = group
	return h
}

func (h *rawHandle) GetGroup() string {
	return h.Group
}

type UpdateDispatcher struct {
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
	activeAlbums          map[int64]*albumBox
}

// creates and populates a new UpdateDispatcher
func (c *Client) NewUpdateDispatcher() {
	c.dispatcher = &UpdateDispatcher{}
}

func hasEndGroup(f []Filter) bool {
	for _, filter := range f {
		if filter.EndGroup {
			return true
		}
	}
	return false
}

func (c *Client) handleMessageUpdate(update Message) {
	switch msg := update.(type) {
	case *MessageObj:
		if msg.GroupedID != 0 {
			c.handleAlbum(*msg)
		}

		sort.SliceStable(c.dispatcher.messageHandles, func(i, j int) bool {
			if c.dispatcher.messageHandles[i].Group == c.dispatcher.messageHandles[j].Group {
				return c.dispatcher.messageHandles[i].Priority < c.dispatcher.messageHandles[j].Priority
			}
			return c.dispatcher.messageHandles[i].Group < c.dispatcher.messageHandles[j].Group
		})

		processedGroups := make(map[string]bool)
		handlerChannel := make(chan messageHandle, len(c.dispatcher.messageHandles))

		for _, handler := range c.dispatcher.messageHandles {
			if handler.IsMatch(msg.Message) {
				// Check if the handler's group has already been processed
				if processedGroups[handler.GetGroup()] {
					continue
				}

				if hasEndGroup(handler.Filters) {
					processedGroups[handler.GetGroup()] = true
				}

				handlerChannel <- handler
			}
		}
		close(handlerChannel)

		var wg sync.WaitGroup
		for handler := range handlerChannel {
			wg.Add(1)
			go func(h messageHandle) {
				defer wg.Done()
				packedMessage := packMessage(c, msg)
				if h.runFilterChain(packedMessage) {
					defer c.NewRecovery()()
					if err := h.Handler(packedMessage); err != nil {
						c.Log.Error(errors.Wrap(err, "updates.dispatcher.message"))
					}
				}
			}(handler)
		}
		wg.Wait()

	case *MessageService:
		var wg sync.WaitGroup

		sort.SliceStable(c.dispatcher.actionHandles, func(i, j int) bool {
			if c.dispatcher.actionHandles[i].Group == c.dispatcher.actionHandles[j].Group {
				return c.dispatcher.actionHandles[i].Priority < c.dispatcher.actionHandles[j].Priority
			}
			return c.dispatcher.actionHandles[i].Group < c.dispatcher.actionHandles[j].Group
		})

		processedGroups := make(map[string]bool)

		for _, handler := range c.dispatcher.actionHandles {
			go func(h chatActionHandle) {
				if processedGroups[h.GetGroup()] {
					return
				}

				if hasEndGroup(handler.Filters) {
					processedGroups[handler.GetGroup()] = true
				}

				wg.Add(1)
				defer wg.Done()
				defer c.NewRecovery()()
				if err := h.Handler(packMessage(c, msg)); err != nil {
					c.Log.Error(errors.Wrap(err, "updates.dispatcher.action"))
				}
			}(handler)
		}
		wg.Wait()
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

			sort.SliceStable(c.dispatcher.albumHandles, func(i, j int) bool {
				if c.dispatcher.albumHandles[i].Group == c.dispatcher.albumHandles[j].Group {
					return c.dispatcher.albumHandles[i].Priority < c.dispatcher.albumHandles[j].Priority
				}
				return c.dispatcher.albumHandles[i].Group < c.dispatcher.albumHandles[j].Group
			})

			processedGroups := make(map[string]bool)

			for _, handle := range c.dispatcher.albumHandles {
				go func(h albumHandle) {
					if processedGroups[h.GetGroup()] {
						return
					}

					if hasEndGroup(handle.Filters) {
						processedGroups[handle.GetGroup()] = true
					}

					if err := h.Handler(&Album{
						GroupedID: abox.groupedId,
						Messages:  abox.messages,
						Client:    c,
					}); err != nil {
						c.Log.Error(errors.Wrap(err, "updates.dispatcher.album"))
					}
				}(handle)
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
		c.Log.Error("updates.Dispatcher.getDifference -", err)
	}
	if updatedMessage != nil {
		c.handleMessageUpdate(updatedMessage)
	}
}

func (c *Client) handleEditUpdate(update Message) {
	if msg, ok := update.(*MessageObj); ok {
		sort.SliceStable(c.dispatcher.messageEditHandles, func(i, j int) bool {
			if c.dispatcher.messageEditHandles[i].Group == c.dispatcher.messageEditHandles[j].Group {
				return c.dispatcher.messageEditHandles[i].Priority < c.dispatcher.messageEditHandles[j].Priority
			}
			return c.dispatcher.messageEditHandles[i].Group < c.dispatcher.messageEditHandles[j].Group
		})

		processedGroups := make(map[string]bool)
		handlerChannel := make(chan messageEditHandle, len(c.dispatcher.messageEditHandles))

		for _, handler := range c.dispatcher.messageEditHandles {
			if handler.IsMatch(msg.Message) {
				if processedGroups[handler.GetGroup()] {
					continue
				}

				if hasEndGroup(handler.Filters) {
					processedGroups[handler.GetGroup()] = true
				}

				handlerChannel <- handler
			}
		}
		close(handlerChannel)

		var wg sync.WaitGroup
		for handler := range handlerChannel {
			wg.Add(1)
			go func(h messageEditHandle) {
				defer wg.Done()
				defer c.NewRecovery()()
				if err := h.Handler(packMessage(c, msg)); err != nil {
					c.Log.Error(errors.Wrap(err, "updates.dispatcher.editMessage"))
				}
			}(handler)
		}
		wg.Wait()
	}
}

func (c *Client) handleCallbackUpdate(update *UpdateBotCallbackQuery) {
	sort.SliceStable(c.dispatcher.callbackHandles, func(i, j int) bool {
		if c.dispatcher.callbackHandles[i].Group == c.dispatcher.callbackHandles[j].Group {
			return c.dispatcher.callbackHandles[i].Priority < c.dispatcher.callbackHandles[j].Priority
		}
		return c.dispatcher.callbackHandles[i].Group < c.dispatcher.callbackHandles[j].Group
	})

	processedGroups := make(map[string]bool)
	handlerChannel := make(chan callbackHandle, len(c.dispatcher.callbackHandles))

	for _, handler := range c.dispatcher.callbackHandles {
		if handler.IsMatch(update.Data) {
			if processedGroups[handler.GetGroup()] {
				continue
			}

			if hasEndGroup(handler.Filters) {
				processedGroups[handler.GetGroup()] = true
			}

			handlerChannel <- handler
		}

	}
	close(handlerChannel)

	var wg sync.WaitGroup

	for handler := range handlerChannel {
		wg.Add(1)
		go func(h callbackHandle) {
			defer wg.Done()
			defer c.NewRecovery()()
			if err := h.Handler(packCallbackQuery(c, update)); err != nil {
				c.Log.Error(errors.Wrap(err, "updates.dispatcher.callbackQuery"))
			}
		}(handler)
	}
	wg.Wait()
}

func (c *Client) handleInlineCallbackUpdate(update *UpdateInlineBotCallbackQuery) {
	sort.SliceStable(c.dispatcher.inlineCallbackHandles, func(i, j int) bool {
		if c.dispatcher.inlineCallbackHandles[i].Group == c.dispatcher.inlineCallbackHandles[j].Group {
			return c.dispatcher.inlineCallbackHandles[i].Priority < c.dispatcher.inlineCallbackHandles[j].Priority
		}
		return c.dispatcher.inlineCallbackHandles[i].Group < c.dispatcher.inlineCallbackHandles[j].Group
	})

	processedGroups := make(map[string]bool)
	handlerChannel := make(chan inlineCallbackHandle, len(c.dispatcher.inlineCallbackHandles))

	for _, handler := range c.dispatcher.inlineCallbackHandles {
		if handler.IsMatch(update.Data) {
			if processedGroups[handler.GetGroup()] {
				continue
			}

			if hasEndGroup(handler.Filters) {
				processedGroups[handler.GetGroup()] = true
			}

			handlerChannel <- handler
		}
	}
	close(handlerChannel)

	var wg sync.WaitGroup

	for handler := range handlerChannel {
		wg.Add(1)
		go func(h inlineCallbackHandle) {
			defer wg.Done()
			defer c.NewRecovery()()
			if err := h.Handler(packInlineCallbackQuery(c, update)); err != nil {
				c.Log.Error(errors.Wrap(err, "updates.dispatcher.inlineCallbackQuery"))
			}
		}(handler)
	}
	wg.Wait()
}

func (c *Client) handleParticipantUpdate(update *UpdateChannelParticipant) {
	handlerChannel := make(chan participantHandle, len(c.dispatcher.participantHandles))

	for _, handler := range c.dispatcher.participantHandles {
		handlerChannel <- handler
	}
	close(handlerChannel)

	var wg sync.WaitGroup

	for handler := range handlerChannel {
		wg.Add(1)
		go func(h participantHandle) {
			defer wg.Done()
			defer c.NewRecovery()()
			if err := h.Handler(packChannelParticipant(c, update)); err != nil {
				c.Log.Error(errors.Wrap(err, "updates.dispatcher.participant"))
			}
		}(handler)
	}
	wg.Wait()
}

func (c *Client) handleInlineUpdate(update *UpdateBotInlineQuery) {
	handlerChannel := make(chan inlineHandle, len(c.dispatcher.inlineHandles))

	for _, handler := range c.dispatcher.inlineHandles {
		if handler.IsMatch(update.Query) {
			handlerChannel <- handler
		}
	}
	close(handlerChannel)

	var wg sync.WaitGroup

	for handler := range handlerChannel {
		wg.Add(1)
		go func(h inlineHandle) {
			defer wg.Done()
			defer c.NewRecovery()()
			if err := h.Handler(packInlineQuery(c, update)); err != nil {
				c.Log.Error(errors.Wrap(err, "updates.dispatcher.inlineQuery"))
			}
		}(handler)
	}
	wg.Wait()
}

func (c *Client) handleDeleteUpdate(update Update) {
	handlerChannel := make(chan messageDeleteHandle, len(c.dispatcher.messageDeleteHandles))

	for _, handler := range c.dispatcher.messageDeleteHandles {
		handlerChannel <- handler
	}
	close(handlerChannel)

	var wg sync.WaitGroup

	for handler := range handlerChannel {
		wg.Add(1)
		go func(h messageDeleteHandle) {
			defer wg.Done()
			defer c.NewRecovery()()
			if err := h.Handler(packDeleteMessage(c, update)); err != nil {
				c.Log.Error(errors.Wrap(err, "updates.dispatcher.deleteMessage"))
			}
		}(handler)
	}
	wg.Wait()
}

func (c *Client) handleRawUpdate(update Update) {
	handlerChannel := make(chan rawHandle, len(c.dispatcher.rawHandles))

	for _, handler := range c.dispatcher.rawHandles {
		if reflect.TypeOf(update) == reflect.TypeOf(handler.updateType) {
			handlerChannel <- handler
		}
	}
	close(handlerChannel)

	var wg sync.WaitGroup

	for handler := range handlerChannel {
		wg.Add(1)
		go func(h rawHandle) {
			defer wg.Done()
			defer c.NewRecovery()()
			if err := h.Handler(update, c); err != nil {
				c.Log.Error(errors.Wrap(err, "updates.dispatcher.rawUpdate"))
			}
		}(handler)
	}
	wg.Wait()
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
	Private, Group, Channel, Media, Command, Reply, Forward, FromBot, Blacklist, Mention, EndGroup bool
	Users, Chats                                                                                   []int64
	Func                                                                                           func(m *NewMessage) bool
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
	FilterEndGroup = func() Filter {
		return Filter{EndGroup: true}
	}
)

func (c *Client) AddMessageHandler(pattern interface{}, handler MessageHandler, filters ...Filter) *messageHandle {
	var messageFilters []Filter
	if len(filters) > 0 {
		messageFilters = filters
	}
	handle := messageHandle{Pattern: pattern, Handler: handler, Filters: messageFilters}
	c.dispatcher.messageHandles = append(c.dispatcher.messageHandles, handle)
	return &c.dispatcher.messageHandles[len(c.dispatcher.messageHandles)-1]
}

func (c *Client) AddDeleteHandler(pattern interface{}, handler func(d *DeleteMessage) error) messageDeleteHandle {
	handle := messageDeleteHandle{
		Pattern: pattern,
		Handler: handler,
	}
	c.dispatcher.messageDeleteHandles = append(c.dispatcher.messageDeleteHandles, handle)
	return handle
}

func (c *Client) AddAlbumHandler(handler func(m *Album) error) albumHandle {
	c.dispatcher.albumHandles = append(c.dispatcher.albumHandles, albumHandle{Handler: handler})
	return albumHandle{Handler: handler}
}

func (c *Client) AddActionHandler(handler MessageHandler) chatActionHandle {
	c.dispatcher.actionHandles = append(c.dispatcher.actionHandles, chatActionHandle{Handler: handler})
	return chatActionHandle{Handler: handler}
}

// Handle updates categorized as "UpdateMessageEdited"
//
// Included Updates:
//   - Message Edited
//   - Channel Post Edited
func (c *Client) AddEditHandler(pattern interface{}, handler MessageHandler) messageEditHandle {
	handle := messageEditHandle{Pattern: pattern, Handler: handler}
	c.dispatcher.messageEditHandles = append(c.dispatcher.messageEditHandles, handle)
	return handle
}

// Handle updates categorized as "UpdateBotInlineQuery"
//
// Included Updates:
//   - Inline Query
func (c *Client) AddInlineHandler(pattern interface{}, handler InlineHandler) inlineHandle {
	handle := inlineHandle{Pattern: pattern, Handler: handler}
	c.dispatcher.inlineHandles = append(c.dispatcher.inlineHandles, handle)
	return handle
}

// Handle updates categorized as "UpdateBotCallbackQuery"
//
// Included Updates:
//   - Callback Query
func (c *Client) AddCallbackHandler(pattern interface{}, handler CallbackHandler) callbackHandle {
	handle := callbackHandle{Pattern: pattern, Handler: handler}
	c.dispatcher.callbackHandles = append(c.dispatcher.callbackHandles, handle)
	return handle
}

// Handle updates categorized as "UpdateInlineBotCallbackQuery"
//
// Included Updates:
//   - Inline Callback Query
func (c *Client) AddInlineCallbackHandler(pattern interface{}, handler InlineCallbackHandler) inlineCallbackHandle {
	handle := inlineCallbackHandle{Pattern: pattern, Handler: handler}
	c.dispatcher.inlineCallbackHandles = append(c.dispatcher.inlineCallbackHandles, handle)
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
func (c *Client) AddParticipantHandler(handler ParticipantHandler) participantHandle {
	handle := participantHandle{Handler: handler}
	c.dispatcher.participantHandles = append(c.dispatcher.participantHandles, handle)
	return handle
}

func (c *Client) AddRawHandler(updateType Update, handler RawHandler) rawHandle {
	handle := rawHandle{updateType: updateType, Handler: handler}
	c.dispatcher.rawHandles = append(c.dispatcher.rawHandles, handle)
	return handle
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
	OnEdit           ev = "edit"
	OnDelete         ev = "delete"
	OnAlbum          ev = "album"
	OnInline         ev = "inline"
	OnCallback       ev = "callback"
	OnInlineCallback ev = "inlineCallback"
	OnParticipant    ev = "participant"
	OnRaw            ev = "raw"
)

type handleInterface interface{}

func (c *Client) On(pattern any, handler interface{}, filters ...Filter) handleInterface {
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

func (c *Client) OnGroup(group string, pattern any, handler interface{}, filters ...Filter) handleInterface {
	h := c.On(pattern, handler, filters...)
	if h != nil {
		h.(Handle).SetGroup(group)
	}
	return h
}
