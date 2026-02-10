package telegram

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	ErrConversationTimeout = errors.New("conversation timeout")
	ErrConversationClosed  = errors.New("conversation closed")
	ErrConversationAborted = errors.New("conversation aborted by user")
	ErrValidationFailed    = errors.New("validation failed after max retries")
)

// Conversation is a state machine for interactive conversations with users
type Conversation struct {
	Client          *Client
	Peer            InputPeer
	isPrivate       bool
	timeout         int32
	lastMsg         *NewMessage
	stopPropagation bool
	ctx             context.Context
	cancel          context.CancelFunc
	closed          bool
	abortKeywords   []string
	fromUser        int64
	mu              sync.RWMutex
}

// ConversationOptions for configuring a conversation
type ConversationOptions struct {
	Private         bool            // Restrict conversation to private chats
	Timeout         int32           // Response timeout in seconds (default: 60)
	StopPropagation bool            // Stop update propagation to other handlers
	Context         context.Context // Parent context for cancellation
	AbortKeywords   []string        // Words that abort the conversation (e.g., "cancel")
	FromUser        int64           // Only accept messages from this user ID, useful in groups
}

func (c *Client) NewConversation(peer any, options ...*ConversationOptions) (*Conversation, error) {
	peerID, err := c.ResolvePeer(peer)
	if err != nil {
		return nil, err
	}

	opts := getVariadic(options, &ConversationOptions{
		Timeout:         60,
		StopPropagation: true,
	})

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	return &Conversation{
		Client:          c,
		Peer:            peerID,
		isPrivate:       opts.Private,
		timeout:         opts.Timeout,
		stopPropagation: opts.StopPropagation,
		ctx:             ctx,
		cancel:          cancel,
		abortKeywords:   opts.AbortKeywords,
		fromUser:        opts.FromUser,
	}, nil
}

func NewConversation(client *Client, peer InputPeer, options ...*ConversationOptions) *Conversation {
	opts := getVariadic(options, &ConversationOptions{
		Timeout: 60,
	})

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	return &Conversation{
		Client:          client,
		Peer:            peer,
		isPrivate:       opts.Private,
		timeout:         opts.Timeout,
		stopPropagation: opts.StopPropagation,
		ctx:             ctx,
		cancel:          cancel,
		abortKeywords:   opts.AbortKeywords,
		fromUser:        opts.FromUser,
	}
}

func (c *Conversation) WithTimeout(timeout int32) *Conversation {
	c.timeout = timeout
	return c
}

func (c *Conversation) WithPrivate(private bool) *Conversation {
	c.isPrivate = private
	return c
}

func (c *Conversation) WithStopPropagation(stop bool) *Conversation {
	c.stopPropagation = stop
	return c
}

func (c *Conversation) WithContext(ctx context.Context) *Conversation {
	c.ctx, c.cancel = context.WithCancel(ctx)
	return c
}

func (c *Conversation) SetTimeout(timeout int32) *Conversation {
	c.timeout = timeout
	return c
}

func (c *Conversation) SetStopPropagation(stop bool) *Conversation {
	c.stopPropagation = stop
	return c
}

func (c *Conversation) LastMessage() *NewMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastMsg
}

func (c *Conversation) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

func (c *Conversation) SetAbortKeywords(keywords ...string) *Conversation {
	c.abortKeywords = keywords
	return c
}

func (c *Conversation) SetFromUser(userID int64) *Conversation {
	c.fromUser = userID
	return c
}

func (c *Conversation) WithFromUser(userID int64) *Conversation {
	c.fromUser = userID
	return c
}

func (c *Conversation) checkAbort(msg *NewMessage) bool {
	if len(c.abortKeywords) == 0 {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(msg.Text()))
	for _, keyword := range c.abortKeywords {
		if strings.ToLower(keyword) == text {
			c.Close()
			return true
		}
	}
	return false
}

func (c *Conversation) Respond(text any, opts ...*SendOptions) (*NewMessage, error) {
	return c.Client.SendMessage(c.Peer, text, opts...)
}

func (c *Conversation) RespondMedia(media InputMedia, opts ...*MediaOptions) (*NewMessage, error) {
	return c.Client.SendMedia(c.Peer, media, opts...)
}

func (c *Conversation) Reply(text any, opts ...*SendOptions) (*NewMessage, error) {
	var options = getVariadic(opts, &SendOptions{})
	if options.ReplyID == 0 {
		c.mu.RLock()
		if c.lastMsg != nil {
			options.ReplyID = c.lastMsg.ID
		}
		c.mu.RUnlock()
	}

	return c.Client.SendMessage(c.Peer, text, opts...)
}

func (c *Conversation) ReplyMedia(media InputMedia, opts ...*MediaOptions) (*NewMessage, error) {
	var options = getVariadic(opts, &MediaOptions{})
	if options.ReplyID == 0 {
		c.mu.RLock()
		if c.lastMsg != nil {
			options.ReplyID = c.lastMsg.ID
		}
		c.mu.RUnlock()
	}

	return c.Client.SendMedia(c.Peer, media, opts...)
}

func (c *Conversation) GetResponse() (*NewMessage, error) {
	return c.waitForMessage(nil)
}

func (c *Conversation) waitForMessage(check func(*NewMessage) bool) (*NewMessage, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrConversationClosed
	}
	c.mu.RUnlock()

	resp := make(chan *NewMessage, 1)
	done := make(chan struct{})

	waitFunc := func(m *NewMessage) error {
		if check != nil && !check(m) {
			return nil
		}
		select {
		case resp <- m:
		case <-done:
		}
		if c.stopPropagation {
			return ErrEndGroup
		}
		return nil
	}

	filters := c.buildFilters()
	args := make([]any, 0, 2+len(filters))
	args = append(args, OnMessage, waitFunc)
	for _, f := range filters {
		args = append(args, f)
	}
	h := c.Client.On(args...)
	h.SetGroup(ConversationGroup)

	timeout := time.Duration(c.timeout) * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-c.ctx.Done():
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationClosed
	case <-timer.C:
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationTimeout
	case m := <-resp:
		close(done)
		c.Client.RemoveHandle(h)
		c.mu.Lock()
		c.lastMsg = m
		c.mu.Unlock()
		return m, nil
	}
}

func (c *Conversation) GetEdit() (*NewMessage, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrConversationClosed
	}
	c.mu.RUnlock()

	resp := make(chan *NewMessage, 1)
	done := make(chan struct{})

	waitFunc := func(m *NewMessage) error {
		select {
		case resp <- m:
		case <-done:
		}
		if c.stopPropagation {
			return ErrEndGroup
		}
		return nil
	}

	filters := c.buildFilters()
	h := c.Client.On(OnEdit, waitFunc, filters)
	h.SetGroup(ConversationGroup)

	timeout := time.Duration(c.timeout) * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-c.ctx.Done():
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationClosed
	case <-timer.C:
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationTimeout
	case m := <-resp:
		close(done)
		c.Client.RemoveHandle(h)
		c.mu.Lock()
		c.lastMsg = m
		c.mu.Unlock()
		return m, nil
	}
}

func (c *Conversation) GetReply() (*NewMessage, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrConversationClosed
	}
	c.mu.RUnlock()

	resp := make(chan *NewMessage, 1)
	done := make(chan struct{})

	waitFunc := func(m *NewMessage) error {
		select {
		case resp <- m:
		case <-done:
		}
		if c.stopPropagation {
			return ErrEndGroup
		}
		return nil
	}

	filters := c.buildFilters()
	filters = append(filters, FilterReply)
	h := c.Client.On(OnMessage, waitFunc, filters)
	h.SetGroup(ConversationGroup)

	timeout := time.Duration(c.timeout) * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-c.ctx.Done():
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationClosed
	case <-timer.C:
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationTimeout
	case m := <-resp:
		close(done)
		c.Client.RemoveHandle(h)
		c.mu.Lock()
		c.lastMsg = m
		c.mu.Unlock()
		return m, nil
	}
}

func (c *Conversation) MarkRead() (*MessagesAffectedMessages, error) {
	c.mu.RLock()
	lastMsg := c.lastMsg
	c.mu.RUnlock()
	if lastMsg != nil {
		return c.Client.SendReadAck(c.Peer, lastMsg.ID)
	}
	return c.Client.SendReadAck(c.Peer)
}

func (c *Conversation) WaitClick(match ...string) (*CallbackQuery, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrConversationClosed
	}
	c.mu.RUnlock()

	resp := make(chan *CallbackQuery, 1)
	done := make(chan struct{})

	waitFunc := func(b *CallbackQuery) error {
		select {
		case resp <- b:
		case <-done:
		}
		if c.stopPropagation {
			return ErrEndGroup
		}
		return nil
	}

	h := c.Client.On(OnCallbackQuery, waitFunc, CustomCallback(func(b *CallbackQuery) bool {
		if !c.Client.PeerEquals(b.Peer, c.Peer) {
			return false
		}

		if len(match) > 0 {
			data := b.DataString()
			return slices.Contains(match, data)
		}

		return true
	}))
	h.SetGroup(ConversationGroup)

	timeout := time.Duration(c.timeout) * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-c.ctx.Done():
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationClosed
	case <-timer.C:
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationTimeout
	case b := <-resp:
		close(done)
		c.Client.RemoveHandle(h)
		return b, nil
	}
}

func (c *Conversation) WaitEvent(ev Update) (Update, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrConversationClosed
	}
	c.mu.RUnlock()

	resp := make(chan Update, 1)
	done := make(chan struct{})

	waitFunc := func(u Update, _ *Client) error {
		select {
		case resp <- u:
		case <-done:
		}
		return nil
	}

	h := c.Client.On(ev, waitFunc)
	h.SetGroup(ConversationGroup)

	timeout := time.Duration(c.timeout) * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-c.ctx.Done():
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationClosed
	case <-timer.C:
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationTimeout
	case u := <-resp:
		close(done)
		c.Client.RemoveHandle(h)
		return u, nil
	}
}

func (c *Conversation) WaitRead() (*UpdateReadChannelInbox, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrConversationClosed
	}
	c.mu.RUnlock()

	resp := make(chan *UpdateReadChannelInbox, 1)
	done := make(chan struct{})

	waitFunc := func(u Update) error {
		switch v := u.(type) {
		case *UpdateReadChannelInbox:
			select {
			case resp <- v:
			case <-done:
			}
		}
		return nil
	}

	h := c.Client.On(&UpdateReadChannelInbox{}, waitFunc)
	h.SetGroup(ConversationGroup)

	timeout := time.Duration(c.timeout) * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-c.ctx.Done():
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationClosed
	case <-timer.C:
		close(done)
		c.Client.RemoveHandle(h)
		return nil, ErrConversationTimeout
	case u := <-resp:
		close(done)
		c.Client.RemoveHandle(h)
		return u, nil
	}
}

// Close closes the conversation and cancels any pending operations
func (c *Conversation) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
}

func (c *Conversation) Ask(text any, opts ...*SendOptions) (*NewMessage, error) {
	if _, err := c.Respond(text, opts...); err != nil {
		return nil, fmt.Errorf("sending message: %w", err)
	}
	msg, err := c.GetResponse()
	if err != nil {
		return nil, err
	}
	if c.checkAbort(msg) {
		return nil, ErrConversationAborted
	}
	return msg, nil
}

func (c *Conversation) AskMedia(text any, opts ...*SendOptions) (*NewMessage, error) {
	if _, err := c.Respond(text, opts...); err != nil {
		return nil, fmt.Errorf("sending message: %w", err)
	}
	return c.WaitForMedia()
}

func (c *Conversation) AskPhoto(text any, opts ...*SendOptions) (*NewMessage, error) {
	if _, err := c.Respond(text, opts...); err != nil {
		return nil, fmt.Errorf("sending message: %w", err)
	}
	return c.WaitForPhoto()
}

func (c *Conversation) AskDocument(text any, opts ...*SendOptions) (*NewMessage, error) {
	if _, err := c.Respond(text, opts...); err != nil {
		return nil, fmt.Errorf("sending message: %w", err)
	}
	return c.WaitForDocument()
}

func (c *Conversation) AskVideo(text any, opts ...*SendOptions) (*NewMessage, error) {
	if _, err := c.Respond(text, opts...); err != nil {
		return nil, fmt.Errorf("sending message: %w", err)
	}
	return c.WaitForVideo()
}

func (c *Conversation) AskVoice(text any, opts ...*SendOptions) (*NewMessage, error) {
	if _, err := c.Respond(text, opts...); err != nil {
		return nil, fmt.Errorf("sending message: %w", err)
	}
	return c.WaitForVoice()
}

func (c *Conversation) GetResponseMatching(pattern *regexp.Regexp) (*NewMessage, error) {
	return c.waitForMessage(func(m *NewMessage) bool {
		return pattern.MatchString(m.Text())
	})
}

func (c *Conversation) GetResponseContaining(words ...string) (*NewMessage, error) {
	return c.waitForMessage(func(m *NewMessage) bool {
		text := strings.ToLower(m.Text())
		for _, word := range words {
			if strings.Contains(text, strings.ToLower(word)) {
				return true
			}
		}
		return false
	})
}

// GetResponseExact waits for a message with exact text match (case-insensitive)
func (c *Conversation) GetResponseExact(options ...string) (*NewMessage, error) {
	return c.waitForMessage(func(m *NewMessage) bool {
		text := strings.ToLower(strings.TrimSpace(m.Text()))
		for _, opt := range options {
			if text == strings.ToLower(opt) {
				return true
			}
		}
		return false
	})
}

func (c *Conversation) WaitForPhoto() (*NewMessage, error) {
	return c.waitForMessage(func(m *NewMessage) bool {
		return m.Photo() != nil
	})
}

func (c *Conversation) WaitForDocument() (*NewMessage, error) {
	return c.waitForMessage(func(m *NewMessage) bool {
		return m.Document() != nil
	})
}

func (c *Conversation) WaitForVoice() (*NewMessage, error) {
	return c.waitForMessage(func(m *NewMessage) bool {
		if doc := m.Document(); doc != nil {
			for _, attr := range doc.Attributes {
				if _, ok := attr.(*DocumentAttributeAudio); ok {
					return true
				}
			}
		}
		return false
	})
}

func (c *Conversation) WaitForVideo() (*NewMessage, error) {
	return c.waitForMessage(func(m *NewMessage) bool {
		return m.Video() != nil
	})
}

func (c *Conversation) WaitForSticker() (*NewMessage, error) {
	return c.waitForMessage(func(m *NewMessage) bool {
		return m.Sticker() != nil
	})
}

func (c *Conversation) WaitForMedia() (*NewMessage, error) {
	return c.waitForMessage(func(m *NewMessage) bool {
		return m.Media() != nil
	})
}

// Choice sends a message with inline buttons and waits for a button click.
// eg. choices := []string{"Option 1", "Option 2", "Option 3"}
func (c *Conversation) Choice(text string, choices []string) (*CallbackQuery, error) {
	kb := NewKeyboard()
	var buttons []KeyboardButton
	for _, choice := range choices {
		buttons = append(buttons, Button.Data(choice, choice))
	}
	kb.AddRow(buttons...)

	_, err := c.Respond(text, &SendOptions{
		ReplyMarkup: kb.Build(),
	})
	if err != nil {
		return nil, fmt.Errorf("sending choice message: %w", err)
	}

	return c.WaitClick()
}

func (c *Conversation) ChoiceRow(text string, rows ...[]string) (*CallbackQuery, error) {
	kb := NewKeyboard()
	for _, row := range rows {
		var buttons []KeyboardButton
		for _, choice := range row {
			buttons = append(buttons, Button.Data(choice, choice))
		}
		kb.AddRow(buttons...)
	}

	_, err := c.Respond(text, &SendOptions{
		ReplyMarkup: kb.Build(),
	})
	if err != nil {
		return nil, fmt.Errorf("sending choice message: %w", err)
	}

	return c.WaitClick()
}

// AskUntil keeps asking until the validator returns true or maxRetries is reached.
// On each failed validation, it sends the retryMessage if provided.
func (c *Conversation) AskUntil(question string, validator func(*NewMessage) bool, maxRetries int, retryMessage ...string) (*NewMessage, error) {
	retry := "Invalid response. Please try again."
	if len(retryMessage) > 0 {
		retry = retryMessage[0]
	}

	for attempt := range maxRetries {
		msg, err := c.Ask(question)
		if err != nil {
			return nil, err
		}

		if validator(msg) {
			return msg, nil
		}

		if attempt < maxRetries-1 {
			question = retry
		}
	}

	return nil, fmt.Errorf("%w: after %d attempts", ErrValidationFailed, maxRetries)
}

// AskNumber asks for a numeric response
func (c *Conversation) AskNumber(question string, maxRetries ...int) (int64, error) {
	retries := getVariadic(maxRetries, 3)
	msg, err := c.AskUntil(question, func(m *NewMessage) bool {
		_, err := parseInt64(m.Text())
		return err == nil
	}, retries, "Please enter a valid number.")
	if err != nil {
		return 0, err
	}
	num, _ := parseInt64(msg.Text())
	return num, nil
}

// AskYesNo asks a yes/no question and returns the boolean response
func (c *Conversation) AskYesNo(question string) (bool, error) {
	msg, err := c.AskUntil(question, func(m *NewMessage) bool {
		text := strings.ToLower(strings.TrimSpace(m.Text()))
		return text == "yes" || text == "no" || text == "y" || text == "n"
	}, 3, "Please answer with 'yes' or 'no'.")
	if err != nil {
		return false, err
	}
	text := strings.ToLower(strings.TrimSpace(msg.Text()))
	return text == "yes" || text == "y", nil
}

// ConversationStep represents a single step in a multi-step conversation
type ConversationStep struct {
	Name       string
	Question   string
	Validator  func(*NewMessage) bool
	RetryMsg   string
	MaxRetries int
	MediaType  string                                                            // "photo", "video", "document", "voice", "sticker", "media"
	AskFunc    func(*Conversation, string, ...*SendOptions) (*NewMessage, error) // Custom ask function
	Skippable  bool                                                              // Allow skipping this step
	SkipWords  []string                                                          // Words that skip this step (e.g., "skip", "pass")
	Condition  func(map[string]*NewMessage) bool                                 // Only ask if condition is true
	Transform  func(*NewMessage) any                                             // Transform the answer before storing
}

// ConversationWizard manages multi-step conversations
type ConversationWizard struct {
	conv           *Conversation
	steps          []ConversationStep
	answers        map[string]*NewMessage
	transformed    map[string]any    // Transformed answers
	progressMsg    string            // Progress message format (e.g., "Step %d/%d")
	onStepComplete func(string, int) // Callback after each step (stepName, stepNumber)
	allowBack      bool              // Allow going back to previous steps
	skipConfirmMsg string            // Message to confirm skip
}

func (c *Conversation) Wizard() *ConversationWizard {
	return &ConversationWizard{
		conv:        c,
		answers:     make(map[string]*NewMessage),
		transformed: make(map[string]any),
	}
}

// WithProgress sets a progress message format
func (w *ConversationWizard) WithProgress(format string) *ConversationWizard {
	w.progressMsg = format
	return w
}

// OnStepComplete sets a callback for step completion
func (w *ConversationWizard) OnStepComplete(fn func(string, int)) *ConversationWizard {
	w.onStepComplete = fn
	return w
}

// AllowBack enables going back to previous steps
func (w *ConversationWizard) AllowBack() *ConversationWizard {
	w.allowBack = true
	return w
}

func (w *ConversationWizard) Step(name, question string, opts ...func(*ConversationStep)) *ConversationWizard {
	step := ConversationStep{
		Name:       name,
		Question:   question,
		MaxRetries: 3,
	}
	for _, opt := range opts {
		opt(&step)
	}
	w.steps = append(w.steps, step)
	return w
}

func WithValidator(fn func(*NewMessage) bool) func(*ConversationStep) {
	return func(s *ConversationStep) {
		s.Validator = fn
	}
}

func WithRetryMessage(msg string) func(*ConversationStep) {
	return func(s *ConversationStep) {
		s.RetryMsg = msg
	}
}

func WithMaxRetries(n int) func(*ConversationStep) {
	return func(s *ConversationStep) {
		s.MaxRetries = n
	}
}

func WithMediaType(mediaType string) func(*ConversationStep) {
	return func(s *ConversationStep) {
		s.MediaType = mediaType
	}
}

func WithAskFunc(fn func(*Conversation, string, ...*SendOptions) (*NewMessage, error)) func(*ConversationStep) {
	return func(s *ConversationStep) {
		s.AskFunc = fn
	}
}

func ExpectPhoto() func(*ConversationStep) {
	return WithMediaType("photo")
}

func ExpectVideo() func(*ConversationStep) {
	return WithMediaType("video")
}

func ExpectDocument() func(*ConversationStep) {
	return WithMediaType("document")
}

func ExpectVoice() func(*ConversationStep) {
	return WithMediaType("voice")
}

func ExpectSticker() func(*ConversationStep) {
	return WithMediaType("sticker")
}

func ExpectMedia() func(*ConversationStep) {
	return WithMediaType("media")
}

// WithSkip makes a step skippable
func WithSkip(skipWords ...string) func(*ConversationStep) {
	if len(skipWords) == 0 {
		skipWords = []string{"skip", "pass"}
	}
	return func(s *ConversationStep) {
		s.Skippable = true
		s.SkipWords = skipWords
	}
}

// WithCondition adds a condition to determine if step should be asked
func WithCondition(fn func(map[string]*NewMessage) bool) func(*ConversationStep) {
	return func(s *ConversationStep) {
		s.Condition = fn
	}
}

// WithTransform adds a transformation function for the answer
func WithTransform(fn func(*NewMessage) any) func(*ConversationStep) {
	return func(s *ConversationStep) {
		s.Transform = fn
	}
}

func (w *ConversationWizard) Run() (map[string]*NewMessage, error) {
	for i, step := range w.steps {
		if step.Condition != nil && !step.Condition(w.answers) {
			continue
		}

		if w.progressMsg != "" {
			w.conv.Respond(fmt.Sprintf(w.progressMsg, i+1, len(w.steps)))
		}

		question := step.Question
		if step.Skippable {
			if w.skipConfirmMsg != "" {
				question += "\n" + w.skipConfirmMsg
			} else {
				question += fmt.Sprintf("\n(Type '%s' to skip)", step.SkipWords[0])
			}
		}

		var msg *NewMessage
		var err error

		if step.AskFunc != nil {
			msg, err = step.AskFunc(w.conv, question)
		} else if step.MediaType != "" {
			switch step.MediaType {
			case "photo":
				msg, err = w.conv.AskPhoto(question)
			case "video":
				msg, err = w.conv.AskVideo(question)
			case "document":
				msg, err = w.conv.AskDocument(question)
			case "voice":
				msg, err = w.conv.AskVoice(question)
			case "sticker":
				if _, err = w.conv.Respond(question); err != nil {
					return w.answers, fmt.Errorf("step %q: sending message: %w", step.Name, err)
				}
				msg, err = w.conv.WaitForSticker()
			case "media":
				msg, err = w.conv.AskMedia(question)
			default:
				return w.answers, fmt.Errorf("step %q: unknown media type: %s", step.Name, step.MediaType)
			}
		} else if step.Validator != nil {
			retryMsg := step.RetryMsg
			if retryMsg == "" {
				retryMsg = "Invalid input. Please try again."
			}
			msg, err = w.conv.AskUntil(question, step.Validator, step.MaxRetries, retryMsg)
		} else {
			msg, err = w.conv.Ask(question)
		}

		if err != nil {
			return w.answers, fmt.Errorf("step %q failed: %w", step.Name, err)
		}

		if step.Skippable && msg != nil {
			text := strings.ToLower(strings.TrimSpace(msg.Text()))
			skipped := false
			for _, skipWord := range step.SkipWords {
				if text == strings.ToLower(skipWord) {
					skipped = true
					break
				}
			}
			if skipped {
				continue
			}
		}

		w.answers[step.Name] = msg

		if step.Transform != nil {
			w.transformed[step.Name] = step.Transform(msg)
		}

		if w.onStepComplete != nil {
			w.onStepComplete(step.Name, i+1)
		}
	}

	return w.answers, nil
}

func (w *ConversationWizard) GetAnswer(name string) *NewMessage {
	return w.answers[name]
}

func (w *ConversationWizard) GetAnswerText(name string) string {
	if msg := w.answers[name]; msg != nil {
		return msg.Text()
	}
	return ""
}

// GetTransformed returns the transformed answer for a step
func (w *ConversationWizard) GetTransformed(name string) any {
	return w.transformed[name]
}

// GetAllTransformed returns all transformed answers
func (w *ConversationWizard) GetAllTransformed() map[string]any {
	return w.transformed
}

// HasAnswer checks if a step has been answered
func (w *ConversationWizard) HasAnswer(name string) bool {
	_, exists := w.answers[name]
	return exists
}

func (c *Conversation) buildFilters() []Filter {
	var filters []Filter
	switch c.Peer.(type) {
	case *InputPeerChannel, *InputPeerChat:
		filters = append(filters, InChat(c.Client.GetPeerID(c.Peer)))
		if c.fromUser != 0 {
			filters = append(filters, FromUser(c.fromUser))
		}
	case *InputPeerUser, *InputPeerSelf:
		if c.fromUser != 0 {
			filters = append(filters, FromUser(c.fromUser))
		} else {
			filters = append(filters, FromUser(c.Client.GetPeerID(c.Peer)))
		}
	}
	if c.isPrivate {
		filters = append(filters, FilterPrivate)
		filters = append(filters, Not(FilterGroup), Not(FilterChannel))
	}
	return filters
}
