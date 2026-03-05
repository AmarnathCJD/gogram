// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"bytes"
	"strings"

	"errors"
)

type ButtonBuilder struct{}

var Button = ButtonBuilder{}

type KeyboardBuilder struct {
	rows []*KeyboardButtonRow
}

// NewKeyboard initializes a new keyboard builder.
func NewKeyboard() *KeyboardBuilder {
	return &KeyboardBuilder{}
}

// AddRow adds a new row of buttons to the keyboard.
func (kb *KeyboardBuilder) AddRow(buttons ...KeyboardButton) *KeyboardBuilder {
	kb.rows = append(kb.rows, &KeyboardButtonRow{Buttons: buttons})
	return kb
}

// Add adds a single button to the last present row.
// If no rows exist, a new row is created.
func (kb *KeyboardBuilder) Add(button KeyboardButton) *KeyboardBuilder {
	if len(kb.rows) == 0 {
		kb.rows = append(kb.rows, &KeyboardButtonRow{Buttons: []KeyboardButton{button}})
	} else {
		kb.rows[len(kb.rows)-1].Buttons = append(kb.rows[len(kb.rows)-1].Buttons, button)
	}
	return kb
}

// NewGrid arranges buttons into a grid based on specified rows (x) and columns (y).
// If there are fewer buttons than x*y, the last row may contain fewer buttons.
func (kb *KeyboardBuilder) NewGrid(x, y int, buttons ...KeyboardButton) *KeyboardBuilder {
	totalButtons := len(buttons)
	for i := 0; i < x && i*y < totalButtons; i++ {
		endIndex := min((i+1)*y, totalButtons)
		rowButtons := buttons[i*y : endIndex]
		kb.AddRow(rowButtons...)
	}

	if totalButtons > x*y {
		kb.AddRow(buttons[x*y:]...)
	}

	return kb
}

// NewColumn arranges buttons into a grid based on specified number of buttons (x) per column.
func (kb *KeyboardBuilder) NewColumn(x int, buttons ...KeyboardButton) *KeyboardBuilder {
	// i.e x buttons per column
	for i := 0; i < len(buttons); i += x {
		endIndex := min(i+x, len(buttons))
		kb.AddRow(buttons[i:endIndex]...)
	}
	return kb
}

// NewRow arranges buttons into a grid based on specified number of buttons (y) per row.
func (kb *KeyboardBuilder) NewRow(y int, buttons ...KeyboardButton) *KeyboardBuilder {
	// i.e y buttons per row
	for i := range y {
		var rowButtons []KeyboardButton
		for j := i; j < len(buttons); j += y {
			rowButtons = append(rowButtons, buttons[j])
		}
		kb.AddRow(rowButtons...)
	}
	return kb
}

// Build finalizes the keyboard and returns the inline markup.
func (kb *KeyboardBuilder) Build() *ReplyInlineMarkup {
	return &ReplyInlineMarkup{Rows: kb.rows}
}

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

func (ButtonBuilder) Text(text string) *KeyboardButtonObj {
	return &KeyboardButtonObj{Text: text}
}

func (kb *KeyboardButtonObj) Primary() *KeyboardButtonObj {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonObj) Danger() *KeyboardButtonObj {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonObj) Success() *KeyboardButtonObj {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonObj) Icon(emojiID int64) *KeyboardButtonObj {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) Force(placeHolder string) *ReplyKeyboardForceReply {
	return &ReplyKeyboardForceReply{Placeholder: placeHolder}
}

func (ButtonBuilder) Auth(text, url, forwardText string, bot InputUser, requestWriteAccess ...bool) *InputKeyboardButtonURLAuth {
	return &InputKeyboardButtonURLAuth{Text: text, URL: url, FwdText: forwardText, Bot: bot, RequestWriteAccess: getVariadic(requestWriteAccess, false)}
}

func (kb *InputKeyboardButtonURLAuth) Primary() *InputKeyboardButtonURLAuth {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *InputKeyboardButtonURLAuth) Danger() *InputKeyboardButtonURLAuth {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *InputKeyboardButtonURLAuth) Success() *InputKeyboardButtonURLAuth {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *InputKeyboardButtonURLAuth) Icon(emojiID int64) *InputKeyboardButtonURLAuth {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) URL(text, url string) *KeyboardButtonURL {
	return &KeyboardButtonURL{Text: text, URL: url}
}

func (kb *KeyboardButtonURL) Primary() *KeyboardButtonURL {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonURL) Danger() *KeyboardButtonURL {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonURL) Success() *KeyboardButtonURL {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonURL) Icon(emojiID int64) *KeyboardButtonURL {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) Data(text, data string) *KeyboardButtonCallback {
	return &KeyboardButtonCallback{Text: text, Data: []byte(data)}
}

func (kb *KeyboardButtonCallback) Primary() *KeyboardButtonCallback {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonCallback) Danger() *KeyboardButtonCallback {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonCallback) Success() *KeyboardButtonCallback {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonCallback) Icon(emojiID int64) *KeyboardButtonCallback {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) RequestLocation(text string) *KeyboardButtonRequestGeoLocation {
	return &KeyboardButtonRequestGeoLocation{Text: text}
}

func (kb *KeyboardButtonRequestGeoLocation) Primary() *KeyboardButtonRequestGeoLocation {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonRequestGeoLocation) Danger() *KeyboardButtonRequestGeoLocation {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonRequestGeoLocation) Success() *KeyboardButtonRequestGeoLocation {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonRequestGeoLocation) Icon(emojiID int64) *KeyboardButtonRequestGeoLocation {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) Buy(text string) *KeyboardButtonBuy {
	return &KeyboardButtonBuy{Text: text}
}

func (kb *KeyboardButtonBuy) Primary() *KeyboardButtonBuy {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonBuy) Danger() *KeyboardButtonBuy {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonBuy) Success() *KeyboardButtonBuy {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonBuy) Icon(emojiID int64) *KeyboardButtonBuy {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) Game(text string) *KeyboardButtonGame {
	return &KeyboardButtonGame{Text: text}
}

func (kb *KeyboardButtonGame) Primary() *KeyboardButtonGame {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonGame) Danger() *KeyboardButtonGame {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonGame) Success() *KeyboardButtonGame {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonGame) Icon(emojiID int64) *KeyboardButtonGame {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) RequestPhone(text string) *KeyboardButtonRequestPhone {
	return &KeyboardButtonRequestPhone{Text: text}
}

func (kb *KeyboardButtonRequestPhone) Primary() *KeyboardButtonRequestPhone {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonRequestPhone) Danger() *KeyboardButtonRequestPhone {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonRequestPhone) Success() *KeyboardButtonRequestPhone {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonRequestPhone) Icon(emojiID int64) *KeyboardButtonRequestPhone {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

type RequestPeerOptions struct {
	NameRequested     bool
	UsernameRequested bool
	PhotoRequested    bool
	MaxQuantity       int32
}

func (ButtonBuilder) RequestPeer(text string, buttonID int32, peerType RequestPeerType, options ...RequestPeerOptions) *InputKeyboardButtonRequestPeer {
	opt := getVariadic(options, RequestPeerOptions{})
	return &InputKeyboardButtonRequestPeer{Text: text, ButtonID: buttonID, PeerType: peerType, NameRequested: opt.NameRequested, UsernameRequested: opt.UsernameRequested, PhotoRequested: opt.PhotoRequested, MaxQuantity: opt.MaxQuantity}
}

func (ButtonBuilder) RequestPoll(text string, quiz bool) *KeyboardButtonRequestPoll {
	return &KeyboardButtonRequestPoll{Text: text, Quiz: quiz}
}

func (kb *KeyboardButtonRequestPoll) Primary() *KeyboardButtonRequestPoll {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonRequestPoll) Danger() *KeyboardButtonRequestPoll {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonRequestPoll) Success() *KeyboardButtonRequestPoll {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonRequestPoll) Icon(emojiID int64) *KeyboardButtonRequestPoll {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) SwitchInline(text string, samePeer bool, query string) *KeyboardButtonSwitchInline {
	return &KeyboardButtonSwitchInline{Text: text, SamePeer: samePeer, Query: query}
}

func (kb *KeyboardButtonSwitchInline) Primary() *KeyboardButtonSwitchInline {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonSwitchInline) Danger() *KeyboardButtonSwitchInline {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonSwitchInline) Success() *KeyboardButtonSwitchInline {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonSwitchInline) Icon(emojiID int64) *KeyboardButtonSwitchInline {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) WebView(text, url string) *KeyboardButtonWebView {
	return &KeyboardButtonWebView{Text: text, URL: url}
}

func (kb *KeyboardButtonWebView) Primary() *KeyboardButtonWebView {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonWebView) Danger() *KeyboardButtonWebView {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonWebView) Success() *KeyboardButtonWebView {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonWebView) Icon(emojiID int64) *KeyboardButtonWebView {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) SimpleWebView(text, url string) *KeyboardButtonSimpleWebView {
	return &KeyboardButtonSimpleWebView{Text: text, URL: url}
}

func (kb *KeyboardButtonSimpleWebView) Primary() *KeyboardButtonSimpleWebView {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonSimpleWebView) Danger() *KeyboardButtonSimpleWebView {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonSimpleWebView) Success() *KeyboardButtonSimpleWebView {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonSimpleWebView) Icon(emojiID int64) *KeyboardButtonSimpleWebView {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) Mention(text string, user InputUser) *InputKeyboardButtonUserProfile {
	return &InputKeyboardButtonUserProfile{Text: text, UserID: user}
}

func (kb *InputKeyboardButtonUserProfile) Primary() *InputKeyboardButtonUserProfile {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *InputKeyboardButtonUserProfile) Danger() *InputKeyboardButtonUserProfile {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *InputKeyboardButtonUserProfile) Success() *InputKeyboardButtonUserProfile {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *InputKeyboardButtonUserProfile) Icon(emojiID int64) *InputKeyboardButtonUserProfile {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) Copy(text string, copyText string) *KeyboardButtonCopy {
	return &KeyboardButtonCopy{Text: text, CopyText: copyText}
}

func (kb *KeyboardButtonCopy) Primary() *KeyboardButtonCopy {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgPrimary = true
	return kb
}

func (kb *KeyboardButtonCopy) Danger() *KeyboardButtonCopy {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgDanger = true
	return kb
}

func (kb *KeyboardButtonCopy) Success() *KeyboardButtonCopy {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.BgSuccess = true
	return kb
}

func (kb *KeyboardButtonCopy) Icon(emojiID int64) *KeyboardButtonCopy {
	if kb.Style == nil {
		kb.Style = &KeyboardButtonStyle{}
	}
	kb.Style.Icon = emojiID
	return kb
}

func (ButtonBuilder) Row(Buttons ...KeyboardButton) *KeyboardButtonRow {
	return &KeyboardButtonRow{Buttons: Buttons}
}

func (ButtonBuilder) Keyboard(Rows ...*KeyboardButtonRow) *ReplyInlineMarkup {
	return &ReplyInlineMarkup{Rows: Rows}
}

func (ButtonBuilder) Clear() *ReplyKeyboardHide {
	return &ReplyKeyboardHide{}
}

func InlineData(pairs ...string) *ReplyInlineMarkup {
	if len(pairs)%2 != 0 {
		pairs = append(pairs, pairs[len(pairs)-1])
	}
	var buttons []KeyboardButton
	for i := 0; i < len(pairs); i += 2 {
		buttons = append(buttons, &KeyboardButtonCallback{Text: pairs[i], Data: []byte(pairs[i+1])})
	}
	return &ReplyInlineMarkup{Rows: []*KeyboardButtonRow{{Buttons: buttons}}}
}

func InlineDataGrid(perRow int, pairs ...string) *ReplyInlineMarkup {
	if len(pairs)%2 != 0 {
		pairs = append(pairs, pairs[len(pairs)-1])
	}
	var rows []*KeyboardButtonRow
	var buttons []KeyboardButton
	for i := 0; i < len(pairs); i += 2 {
		buttons = append(buttons, &KeyboardButtonCallback{Text: pairs[i], Data: []byte(pairs[i+1])})
		if len(buttons) == perRow {
			rows = append(rows, &KeyboardButtonRow{Buttons: buttons})
			buttons = nil
		}
	}
	if len(buttons) > 0 {
		rows = append(rows, &KeyboardButtonRow{Buttons: buttons})
	}
	return &ReplyInlineMarkup{Rows: rows}
}

func InlineURL(pairs ...string) *ReplyInlineMarkup {
	if len(pairs)%2 != 0 {
		pairs = append(pairs, pairs[len(pairs)-1])
	}
	var buttons []KeyboardButton
	for i := 0; i < len(pairs); i += 2 {
		buttons = append(buttons, &KeyboardButtonURL{Text: pairs[i], URL: pairs[i+1]})
	}
	return &ReplyInlineMarkup{Rows: []*KeyboardButtonRow{{Buttons: buttons}}}
}

func InlineURLGrid(perRow int, pairs ...string) *ReplyInlineMarkup {
	if len(pairs)%2 != 0 {
		pairs = append(pairs, pairs[len(pairs)-1])
	}
	var rows []*KeyboardButtonRow
	var buttons []KeyboardButton
	for i := 0; i < len(pairs); i += 2 {
		buttons = append(buttons, &KeyboardButtonURL{Text: pairs[i], URL: pairs[i+1]})
		if len(buttons) == perRow {
			rows = append(rows, &KeyboardButtonRow{Buttons: buttons})
			buttons = nil
		}
	}
	if len(buttons) > 0 {
		rows = append(rows, &KeyboardButtonRow{Buttons: buttons})
	}
	return &ReplyInlineMarkup{Rows: rows}
}

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
			if len(messageButtons.Rows[0].Buttons) == 0 {
				return nil, errors.New("replyMarkup: row(0) has no buttons")
			}

			if button, ok := messageButtons.Rows[0].Buttons[0].(*KeyboardButtonCallback); ok {
				requestParams.Data = button.Data
			}

		case 1:
			currentX := 0
			currentY := 0
			for _, row := range messageButtons.Rows {
				for _, button := range row.Buttons {
					switch opt := options[0].(type) {
					case string:
						if button, ok := button.(*KeyboardButtonCallback); ok && strings.EqualFold(button.Text, opt) {
							requestParams.Data = button.Data
						}
					case []byte:
						if button, ok := button.(*KeyboardButtonCallback); ok && bytes.Equal(button.Data, opt) {
							requestParams.Data = button.Data
						}
					case int, int32, int64:
						if optInt, ok := opt.(int); ok && optInt == currentX {
							if button, ok := button.(*KeyboardButtonCallback); ok {
								requestParams.Data = button.Data
							}
						}

					case []int, []int32, []int64:
						if optInts, ok := opt.([]int); ok && len(optInts) == 2 {
							if optInts[0] == currentX && optInts[1] == currentY {
								if button, ok := button.(*KeyboardButtonCallback); ok {
									requestParams.Data = button.Data
								}
							}
						}

					default:
						return nil, errors.New("replyMarkup: invalid argument type (expected string, []byte, int, or []int)")
					}
					currentY++
				}
				currentX++
				currentY = 0
			}
		}
	}

	if requestParams.Data == nil {
		return nil, errors.New("replyMarkup: button with given (text, data, or coordinates) not found")
	}

	return m.Client.MessagesGetBotCallbackAnswer(requestParams)
}
