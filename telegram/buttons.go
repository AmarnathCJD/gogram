// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"bytes"
	"strings"

	"github.com/pkg/errors"
)

// ButtonBuilder provides methods to create various types of keyboard buttons
type ButtonBuilder struct{}

// Button is a singleton instance of ButtonBuilder
var Button = ButtonBuilder{}

// KeyboardBuilder builds keyboard layouts
type KeyboardBuilder struct {
	rows []*KeyboardButtonRow
}

// NewKeyboard initializes a new keyboard builder.
func NewKeyboard() *KeyboardBuilder {
	return &KeyboardBuilder{
		rows: make([]*KeyboardButtonRow, 0),
	}
}

// AddRow adds a new row of buttons to the keyboard.
func (kb *KeyboardBuilder) AddRow(buttons ...KeyboardButton) *KeyboardBuilder {
	if len(buttons) > 0 {
		kb.rows = append(kb.rows, &KeyboardButtonRow{Buttons: buttons})
	}
	return kb
}

// NewGrid arranges buttons into a grid based on specified rows (x) and columns (y).
// If there are fewer buttons than x*y, the last row may contain fewer buttons.
func (kb *KeyboardBuilder) NewGrid(rows, cols int, buttons ...KeyboardButton) *KeyboardBuilder {
	totalButtons := len(buttons)
	if totalButtons == 0 {
		return kb
	}

	for i := range rows {
		start := i * cols
		if start >= totalButtons {
			break
		}

		end := min(start+cols, totalButtons)
		kb.AddRow(buttons[start:end]...)
	}

	return kb
}

// NewColumn arranges buttons into a grid based on specified number of buttons (x) per column.
func (kb *KeyboardBuilder) NewColumn(buttonsPerCol int, buttons ...KeyboardButton) *KeyboardBuilder {
	// i.e x buttons per column
	totalButtons := len(buttons)
	if totalButtons == 0 {
		return kb
	}

	for i := 0; i < totalButtons; i += buttonsPerCol {
		end := min(i+buttonsPerCol, totalButtons)
		kb.AddRow(buttons[i:end]...)
	}

	return kb
}

// NewRow arranges buttons into a grid based on specified number of buttons (y) per row.
func (kb *KeyboardBuilder) NewRow(buttonsPerRow int, buttons ...KeyboardButton) *KeyboardBuilder {
	// i.e y buttons per row
	totalButtons := len(buttons)
	if totalButtons == 0 {
		return kb
	}

	for i := range buttonsPerRow {
		var rowButtons []KeyboardButton
		for j := i; j < totalButtons; j += buttonsPerRow {
			rowButtons = append(rowButtons, buttons[j])
		}
		if len(rowButtons) > 0 {
			kb.AddRow(rowButtons...)
		}
	}

	return kb
}

// Build finalizes the keyboard and returns the inline markup.
func (kb *KeyboardBuilder) Build() *ReplyInlineMarkup {
	return &ReplyInlineMarkup{Rows: kb.rows}
}

// BuildReplyOptions contains options for reply keyboard
type BuildReplyOptions struct {
	ResizeKeyboard bool
	OneTime        bool
	Selective      bool
	Persistent     bool
	Placeholder    string
}

// BuildReply finalizes the keyboard and returns the reply keyboard markup.
func (kb *KeyboardBuilder) BuildReply(opts ...BuildReplyOptions) *ReplyKeyboardMarkup {
	opt := getVariadic(opts, BuildReplyOptions{})
	return &ReplyKeyboardMarkup{
		Resize:      opt.ResizeKeyboard,
		SingleUse:   opt.OneTime,
		Selective:   opt.Selective,
		Placeholder: opt.Placeholder,
		Rows:        kb.rows,
		Persistent:  opt.Persistent,
	}
}

// Text creates a simple text button for the keyboard.
// Returns a KeyboardButtonObj with the specified text.
func (ButtonBuilder) Text(text string) *KeyboardButtonObj {
	return &KeyboardButtonObj{Text: text}
}

// Force creates a force reply button with a placeholder.
// Returns a ReplyKeyboardForceReply with the specified placeholder text.
func (ButtonBuilder) Force(placeHolder string) *ReplyKeyboardForceReply {
	return &ReplyKeyboardForceReply{Placeholder: placeHolder}
}

// Auth creates a URL authentication button with optional write access.
// Returns an InputKeyboardButtonURLAuth with text, URL, forward text, bot, and write access flag.
func (ButtonBuilder) Auth(text, url, forwardText string, bot InputUser, requestWriteAccess ...bool) *InputKeyboardButtonURLAuth {
	return &InputKeyboardButtonURLAuth{
		Text:               text,
		URL:                url,
		FwdText:            forwardText,
		Bot:                bot,
		RequestWriteAccess: getVariadic(requestWriteAccess, false),
	}
}

// URL creates a button that links to a URL.
// Returns a KeyboardButtonURL with the specified text and URL.
func (ButtonBuilder) URL(text, url string) *KeyboardButtonURL {
	return &KeyboardButtonURL{Text: text, URL: url}
}

// Data creates a callback button with associated data.
// Returns a KeyboardButtonCallback with text and data converted to bytes.
func (ButtonBuilder) Data(text, data string) *KeyboardButtonCallback {
	return &KeyboardButtonCallback{Text: text, Data: []byte(data)}
}

// RequestLocation creates a button to request the user's location.
// Returns a KeyboardButtonRequestGeoLocation with the specified text.
func (ButtonBuilder) RequestLocation(text string) *KeyboardButtonRequestGeoLocation {
	return &KeyboardButtonRequestGeoLocation{Text: text}
}

// Buy creates a button for initiating a purchase.
// Returns a KeyboardButtonBuy with the specified text.
func (ButtonBuilder) Buy(text string) *KeyboardButtonBuy {
	return &KeyboardButtonBuy{Text: text}
}

// Game creates a button for launching a game.
// Returns a KeyboardButtonGame with the specified text.
func (ButtonBuilder) Game(text string) *KeyboardButtonGame {
	return &KeyboardButtonGame{Text: text}
}

// RequestPhone creates a button to request the user's phone number.
// Returns a KeyboardButtonRequestPhone with the specified text.
func (ButtonBuilder) RequestPhone(text string) *KeyboardButtonRequestPhone {
	return &KeyboardButtonRequestPhone{Text: text}
}

// RequestPeerOptions holds configuration options for peer request buttons.
type RequestPeerOptions struct {
	NameRquested      bool
	UsernameRequested bool
	PhotoRequested    bool
	MaxQuantity       int32
}

// RequestPeer creates a button to request peer information with custom options.
// Returns an InputKeyboardButtonRequestPeer with text, button ID, peer type, and options.
func (ButtonBuilder) RequestPeer(text string, buttonID int32, peerType RequestPeerType, options ...RequestPeerOptions) *InputKeyboardButtonRequestPeer {
	opt := getVariadic(options, RequestPeerOptions{})
	return &InputKeyboardButtonRequestPeer{Text: text, ButtonID: buttonID, PeerType: peerType, NameRequested: opt.NameRquested, UsernameRequested: opt.UsernameRequested, PhotoRequested: opt.PhotoRequested, MaxQuantity: opt.MaxQuantity}
}

// RequestPoll creates a button to request a poll or quiz.
// Returns a KeyboardButtonRequestPoll with text and quiz flag.
func (ButtonBuilder) RequestPoll(text string, quiz bool) *KeyboardButtonRequestPoll {
	return &KeyboardButtonRequestPoll{Text: text, Quiz: quiz}
}

// SwitchInline creates a button to switch to inline mode.
// Returns a KeyboardButtonSwitchInline with text, same peer flag, and query.
func (ButtonBuilder) SwitchInline(text string, samePeer bool, query string) *KeyboardButtonSwitchInline {
	return &KeyboardButtonSwitchInline{Text: text, SamePeer: samePeer, Query: query}
}

// WebView creates a button to open a web view.
// Returns a KeyboardButtonSimpleWebView with text and URL.
func (ButtonBuilder) WebView(text, url string) *KeyboardButtonSimpleWebView {
	return &KeyboardButtonSimpleWebView{Text: text, URL: url}
}

// Mention creates a button to mention a user profile.
// Returns an InputKeyboardButtonUserProfile with text and user ID.
func (ButtonBuilder) Mention(text string, user InputUser) *InputKeyboardButtonUserProfile {
	return &InputKeyboardButtonUserProfile{Text: text, UserID: user}
}

// Copy creates a button that copies text to the clipboard.
// Returns a KeyboardButtonCopy with display text and text to copy.
func (ButtonBuilder) Copy(text string, copyText string) *KeyboardButtonCopy {
	return &KeyboardButtonCopy{Text: text, CopyText: copyText}
}

// Row creates a row of buttons for the keyboard.
// Returns a KeyboardButtonRow containing the specified buttons.
func (ButtonBuilder) Row(Buttons ...KeyboardButton) *KeyboardButtonRow {
	return &KeyboardButtonRow{Buttons: Buttons}
}

// Keyboard constructs a keyboard from rows of buttons.
// Returns a ReplyInlineMarkup with the specified rows.
func (ButtonBuilder) Keyboard(Rows ...*KeyboardButtonRow) *ReplyInlineMarkup {
	return &ReplyInlineMarkup{Rows: Rows}
}

// Clear creates a button to hide the keyboard.
// Returns a ReplyKeyboardHide to remove the keyboard.
func (ButtonBuilder) Clear() *ReplyKeyboardHide {
	return &ReplyKeyboardHide{}
}

// ClickOptions holds options for clicking a button, such as game mode or password.
type ClickOptions struct {
	Game     bool
	Password string
}

// Click clicks a button in a message.
//
// If no argument is given, the first button will be clicked.
//
// If an argument is provided, it can be one of the following:
//   - The text of the button to click.
//   - The data of the button to click.
//   - The coordinates of the button to click as a slice of integers [x, y].
func (m *NewMessage) Click(options ...any) (*MessagesBotCallbackAnswer, error) {
	requestParams := &MessagesGetBotCallbackAnswerParams{
		Peer:  m.Peer,
		MsgID: m.ID,
		Game:  false,
	}

	if len(options) > 0 {
		if opt, ok := options[0].(*ClickOptions); ok {
			requestParams.Game = opt.Game
			if opt.Password != "" {
				accountPasswordSrp, err := m.Client.AccountGetPassword()
				if err != nil {
					return nil, err
				}

				password, err := GetInputCheckPassword(opt.Password, accountPasswordSrp)
				if err != nil {
					return nil, err
				}

				requestParams.Password = password
			}
		}
	}

	if m.ReplyMarkup() == nil {
		return nil, errors.New("replyMarkup: message has no buttons")
	}

	if messageButtons, ok := (*m.ReplyMarkup()).(*ReplyInlineMarkup); ok {
		if len(messageButtons.Rows) == 0 {
			return nil, errors.New("replyMarkup: rows are empty")
		}

		switch len(options) {
		case 0:
			// Default: click the first button in the first row
			if len(messageButtons.Rows[0].Buttons) == 0 {
				return nil, errors.New("replyMarkup: row(0) has no buttons")
			}

			if button, ok := messageButtons.Rows[0].Buttons[0].(*KeyboardButtonCallback); ok {
				requestParams.Data = button.Data
			} else {
				return nil, errors.New("replyMarkup: first button is not a callback button")
			}

		case 1:
			// Search for button by text, data, or coordinates
			currentX, currentY := 0, 0
			found := false
			for _, row := range messageButtons.Rows {
				for _, button := range row.Buttons {
					switch opt := options[0].(type) {
					case string:
						if btn, ok := button.(*KeyboardButtonCallback); ok && strings.EqualFold(btn.Text, opt) {
							requestParams.Data = btn.Data
							found = true
						}
					case []byte:
						if btn, ok := button.(*KeyboardButtonCallback); ok && bytes.Equal(btn.Data, opt) {
							requestParams.Data = btn.Data
							found = true
						}
					case int, int32, int64:
						if optInt, ok := toInt(opt); ok && optInt == currentX {
							if btn, ok := button.(*KeyboardButtonCallback); ok {
								requestParams.Data = btn.Data
								found = true
							}
						}
					case []int, []int32, []int64:
						if optInts, ok := toIntSlice(opt); ok && len(optInts) == 2 && optInts[0] == currentX && optInts[1] == currentY {
							if btn, ok := button.(*KeyboardButtonCallback); ok {
								requestParams.Data = btn.Data
								found = true
							}
						}
					default:
						return nil, errors.New("replyMarkup: invalid argument type (expected string, []byte, int, or []int)")
					}
					if found {
						break
					}
					currentY++
				}
				if found {
					break
				}
				currentX++
				currentY = 0
			}
			if !found {
				return nil, errors.New("replyMarkup: button not found for given input")
			}
		}
	}

	if requestParams.Data == nil {
		return nil, errors.New("replyMarkup: button with given (text, data, or coordinates) not found")
	}

	return m.Client.MessagesGetBotCallbackAnswer(requestParams)
}

// toInt converts various integer types to int for comparison.
func toInt(val any) (int, bool) {
	switch v := val.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	default:
		return 0, false
	}
}

// toIntSlice converts various integer slice types to []int for comparison.
func toIntSlice(val any) ([]int, bool) {
	switch v := val.(type) {
	case []int:
		return v, true
	case []int32:
		result := make([]int, len(v))
		for i, x := range v {
			result[i] = int(x)
		}
		return result, true
	case []int64:
		result := make([]int, len(v))
		for i, x := range v {
			result[i] = int(x)
		}
		return result, true
	default:
		return nil, false
	}
}
