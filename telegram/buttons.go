// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"bytes"

	"github.com/pkg/errors"
)

type Button struct{}

func (Button) Force(placeHolder string) *ReplyKeyboardForceReply {
	return &ReplyKeyboardForceReply{Placeholder: placeHolder}
}

func (Button) Auth(Text string, URL string, ForwardText string, ButtonID int32) *KeyboardButtonURLAuth {
	return &KeyboardButtonURLAuth{Text: Text, URL: URL, FwdText: ForwardText, ButtonID: ButtonID}
}

func (Button) URL(Text string, URL string) *KeyboardButtonURL {
	return &KeyboardButtonURL{Text: Text, URL: URL}
}

func (Button) Data(Text string, Data string) *KeyboardButtonCallback {
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
	return &KeyboardButtonRequestPeer{Text: Text, ButtonID: ButtonID, PeerType: PeerType, MaxQuantity: getVariadic(Max, int32(0)).(int32)}
}

func (Button) RequestPoll(Text string, Quiz bool) *KeyboardButtonRequestPoll {
	return &KeyboardButtonRequestPoll{Text: Text, Quiz: Quiz}
}

func (Button) SwitchInline(Text string, SamePeer bool, Query string) *KeyboardButtonSwitchInline {
	return &KeyboardButtonSwitchInline{Text: Text, SamePeer: SamePeer, Query: Query}
}

func (Button) WebView(Text string, URL string) *KeyboardButtonSimpleWebView {
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

// message.Click() is a function that clicks a button in a message.
//
// It takes one optional argument, which can be either:
//   - the text of the button to click
//   - the data of the button to click
//   - the coordinates of the button to click ([x, y])
//
// If no argument is given, the first button will be clicked.
func (m *NewMessage) Click(options ...any) (*MessagesBotCallbackAnswer, error) {
	requestParams := &MessagesGetBotCallbackAnswerParams{
		Peer:  m.Peer,
		MsgID: m.ID,
		Game:  false,
	}

	if m.ReplyMarkup() == nil {
		return nil, errors.New("message has no buttons")
	}

	switch messageButtons := (*m.ReplyMarkup()).(type) {
	case *ReplyInlineMarkup:
		if len(messageButtons.Rows) == 0 {
			return nil, errors.New("message has no buttons")
		}

		switch len(options) {
		case 0:
			if len(messageButtons.Rows[0].Buttons) == 0 {
				return nil, errors.New("message has no buttons")
			}

			switch button := messageButtons.Rows[0].Buttons[0].(type) {
			case *KeyboardButtonCallback:
				requestParams.Data = button.Data
				//case *KeyboardButtonSimpleWebView:
			}

		case 1:
			currentX := 0
			currentY := 0
			for _, row := range messageButtons.Rows {
				for _, button := range row.Buttons {
					switch opt := options[0].(type) {
					case string:
						switch button := button.(type) {
						case *KeyboardButtonCallback:
							if button.Text == opt {
								requestParams.Data = button.Data
							}
						}
					case []byte:
						switch button := button.(type) {
						case *KeyboardButtonCallback:
							if bytes.Equal(button.Data, opt) {
								requestParams.Data = button.Data
							}
						}
					case int, int32, int64:
						if optInt, ok := opt.(int); ok && optInt == currentX {
							switch button := button.(type) {
							case *KeyboardButtonCallback:
								requestParams.Data = button.Data
							}
						}

					case []int, []int32, []int64:
						if optInts, ok := opt.([]int); ok && len(optInts) == 2 {
							if optInts[0] == currentX && optInts[1] == currentY {
								switch button := button.(type) {
								case *KeyboardButtonCallback:
									requestParams.Data = button.Data
								}
							}
						}
					}
					currentY++
				}
				currentX++
				currentY = 0
			}
		}
	}

	if requestParams.Data == nil {
		return nil, errors.New("button with given text/data/(x,y) not found")
	}

	return m.Client.MessagesGetBotCallbackAnswer(requestParams)
}
