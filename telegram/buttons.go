// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"bytes"
	"strings"

	"github.com/pkg/errors"
)

// Button is a helper struct for creating buttons for messages.
type Button struct{}

func (Button) Force(placeHolder string) *ReplyKeyboardForceReply {
	return &ReplyKeyboardForceReply{Placeholder: placeHolder}
}

func (Button) Auth(Text, URL, ForwardText string, ButtonID int32) *KeyboardButtonURLAuth {
	return &KeyboardButtonURLAuth{Text: Text, URL: URL, FwdText: ForwardText, ButtonID: ButtonID}
}

func (Button) URL(Text, URL string) *KeyboardButtonURL {
	return &KeyboardButtonURL{Text: Text, URL: URL}
}

func (Button) Data(Text, Data string) *KeyboardButtonCallback {
	return &KeyboardButtonCallback{Text: Text, Data: []byte(Data)}
}

func (Button) RequestLocation(Text string) *KeyboardButtonRequestGeoLocation {
	return &KeyboardButtonRequestGeoLocation{Text: Text}
}

func (Button) Buy(Text string) *KeyboardButtonBuy {
	return &KeyboardButtonBuy{Text: Text}
}

func (Button) Game(Text string) *KeyboardButtonGame {
	return &KeyboardButtonGame{Text: Text}
}

func (Button) RequestPhone(Text string) *KeyboardButtonRequestPhone {
	return &KeyboardButtonRequestPhone{Text: Text}
}

func (Button) RequestPeer(Text string, ButtonID int32, PeerType RequestPeerType, Max ...int32) *KeyboardButtonRequestPeer {
	return &KeyboardButtonRequestPeer{Text: Text, ButtonID: ButtonID, PeerType: PeerType, MaxQuantity: getVariadic(Max, int32(0))}
}

func (Button) RequestPoll(Text string, Quiz bool) *KeyboardButtonRequestPoll {
	return &KeyboardButtonRequestPoll{Text: Text, Quiz: Quiz}
}

func (Button) SwitchInline(Text string, SamePeer bool, Query string) *KeyboardButtonSwitchInline {
	return &KeyboardButtonSwitchInline{Text: Text, SamePeer: SamePeer, Query: Query}
}

func (Button) WebView(Text, URL string) *KeyboardButtonSimpleWebView {
	return &KeyboardButtonSimpleWebView{Text: Text, URL: URL}
}

func (Button) Mention(Text string, UserID int64) *KeyboardButtonUserProfile {
	return &KeyboardButtonUserProfile{Text: Text, UserID: UserID}
}

func (Button) Row(Buttons ...KeyboardButton) *KeyboardButtonRow {
	return &KeyboardButtonRow{Buttons: Buttons}
}

func (Button) Keyboard(Rows ...*KeyboardButtonRow) *ReplyInlineMarkup {
	return &ReplyInlineMarkup{Rows: Rows}
}

func (Button) Clear() *ReplyKeyboardHide {
	return &ReplyKeyboardHide{}
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
