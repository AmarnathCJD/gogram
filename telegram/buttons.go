package telegram

type Button struct{}

func (b Button) Force(placeHolder string) *ReplyKeyboardForceReply {
	return &ReplyKeyboardForceReply{Placeholder: placeHolder}
}

func (b Button) Auth(Text string, URL string, ForwardText string, ButtonID int32) *KeyboardButtonURLAuth {
	return &KeyboardButtonURLAuth{Text: Text, URL: URL, FwdText: ForwardText, ButtonID: ButtonID}
}

func (b Button) URL(Text string, URL string) *KeyboardButtonURL {
	return &KeyboardButtonURL{Text: Text, URL: URL}
}

func (b Button) Data(Text string, Data string) *KeyboardButtonCallback {
	return &KeyboardButtonCallback{Text: Text, Data: []byte(Data)}
}

func (b Button) RequestLocation(Text string) *KeyboardButtonRequestGeoLocation {
	return &KeyboardButtonRequestGeoLocation{Text: Text}
}

func (b Button) Buy(Text string) *KeyboardButtonBuy {
	return &KeyboardButtonBuy{Text: Text}
}

func (b Button) Game(Text string) *KeyboardButtonGame {
	return &KeyboardButtonGame{Text: Text}
}

func (b Button) RequestPhone(Text string) *KeyboardButtonRequestPhone {
	return &KeyboardButtonRequestPhone{Text: Text}
}

func (b Button) RequestPoll(Text string, Quiz bool) *KeyboardButtonRequestPoll {
	return &KeyboardButtonRequestPoll{Text: Text, Quiz: Quiz}
}

func (b Button) SwitchInline(Text string, SamePeer bool, Query string) *KeyboardButtonSwitchInline {
	return &KeyboardButtonSwitchInline{Text: Text, SamePeer: SamePeer, Query: Query}
}

func (b Button) WebView(Text string, URL string) *KeyboardButtonSimpleWebView {
	return &KeyboardButtonSimpleWebView{Text: Text, URL: URL}
}

func (b Button) Mention(Text string, UserID int64) *KeyboardButtonUserProfile {
	return &KeyboardButtonUserProfile{Text: Text, UserID: UserID}
}

func (b Button) Row(Buttons ...KeyboardButton) *KeyboardButtonRow {
	return &KeyboardButtonRow{Buttons: Buttons}
}

func (b Button) Keyboard(Rows ...*KeyboardButtonRow) *ReplyInlineMarkup {
	return &ReplyInlineMarkup{Rows: Rows}
}

func (b Button) Clear() *ReplyKeyboardHide {
	return &ReplyKeyboardHide{}
}

type ButtonID struct {
	Data []byte
	Text string
}

// In Beta
func (m *NewMessage) Click(o ...*ButtonID) (*MessagesBotCallbackAnswer, error) {
	if m.ReplyMarkup() != nil {
		switch mark := (*m.ReplyMarkup()).(type) {
		case *ReplyInlineMarkup:
			for _, row := range mark.Rows {
				for _, button := range row.Buttons {
					switch b := button.(type) {
					case *KeyboardButtonCallback:
						for _, id := range o {
							if string(id.Data) == string(b.Data) || id.Text == b.Text {
								return m.Client.MessagesGetBotCallbackAnswer(&MessagesGetBotCallbackAnswerParams{
									Peer:  m.Peer,
									MsgID: m.ID,
									Data:  b.Data,
									Game:  false,
								})
							}
						}
					}
				}
			}
		}
	}
	return nil, nil
}
