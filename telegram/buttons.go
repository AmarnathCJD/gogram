package telegram

import "github.com/pkg/errors"

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
//  - the text of the button to click
//  - the data of the button to click
//  - the coordinates of the button to click
//
// If no argument is given, the first button will be clicked.
func (m *NewMessage) Click(o ...any) (*MessagesBotCallbackAnswer, error) {
	toClickText := ""
	toClickXY := []int32{}
	toClickData := []byte{}

	if len(o) == 0 {
		toClickXY = []int32{0, 0}
	} else if len(o) == 1 {
		switch v := o[0].(type) {
		case string:
			toClickText = v
		case []int32:
			toClickXY = v
		case []byte:
			toClickData = v
		}
	}

	requestParams := &MessagesGetBotCallbackAnswerParams{
		Peer:  m.Peer,
		MsgID: m.ID,
		Game:  false,
	}

	if m.ReplyMarkup() != nil {
		if toClickData != nil {
			requestParams.Data = toClickData
			return m.Client.MessagesGetBotCallbackAnswer(requestParams)
		}
	}

	if m.ReplyMarkup() != nil {
		switch mark := (*m.ReplyMarkup()).(type) {
		case *ReplyInlineMarkup:
			for ix, row := range mark.Rows {
				for iy, button := range row.Buttons {
					switch button := button.(type) {
					case *KeyboardButtonCallback:
						if button.Text == toClickText || (len(toClickXY) == 2 && ix == int(toClickXY[0]) && iy == int(toClickXY[1])) {
							requestParams.Data = button.Data
						}
					}
				}
			}
		}
	}

	if requestParams.Data == nil {
		return nil, errors.New("no button found")
	}

	return m.Client.MessagesGetBotCallbackAnswer(requestParams)
}
