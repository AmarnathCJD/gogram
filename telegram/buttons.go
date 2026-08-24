package telegram

import (
	"bytes"
	"errors"
	"strings"
)

type ButtonBuilder struct{}

var Button = ButtonBuilder{}

type KeyboardBuilder struct{ rows []*KeyboardInlineButtonRow }
type ReplyKeyboardBuilder struct{ rows []*KeyboardButtonRow }
type BuildReplyOptions struct {
	ResizeKeyboard, OneTime, Selective, Persistent bool
	Placeholder                                    string
}

func NewKeyboard() *KeyboardBuilder           { return &KeyboardBuilder{} }
func NewReplyKeyboard() *ReplyKeyboardBuilder { return &ReplyKeyboardBuilder{} }
func (k *KeyboardBuilder) AddRow(b ...KeyboardInlineButton) *KeyboardBuilder {
	p := make([]*KeyboardInlineButton, len(b))
	for i := range b {
		p[i] = &b[i]
	}
	k.rows = append(k.rows, &KeyboardInlineButtonRow{Buttons: p})
	return k
}
func (k *KeyboardBuilder) Add(b KeyboardInlineButton) *KeyboardBuilder {
	if len(k.rows) == 0 {
		return k.AddRow(b)
	}
	k.rows[len(k.rows)-1].Buttons = append(k.rows[len(k.rows)-1].Buttons, &b)
	return k
}
func (k *KeyboardBuilder) NewGrid(_, n int, b ...KeyboardInlineButton) *KeyboardBuilder {
	return k.grid(n, b...)
}
func (k *KeyboardBuilder) NewColumn(n int, b ...KeyboardInlineButton) *KeyboardBuilder {
	return k.grid(n, b...)
}
func (k *KeyboardBuilder) NewRow(n int, b ...KeyboardInlineButton) *KeyboardBuilder {
	return k.grid(n, b...)
}
func (k *KeyboardBuilder) grid(n int, b ...KeyboardInlineButton) *KeyboardBuilder {
	if n <= 0 {
		return k
	}
	for i := 0; i < len(b); i += n {
		e := i + n
		if e > len(b) {
			e = len(b)
		}
		k.AddRow(b[i:e]...)
	}
	return k
}
func (k *KeyboardBuilder) Build() *ReplyInlineMarkup { return &ReplyInlineMarkup{Rows: k.rows} }
func (k *ReplyKeyboardBuilder) AddRow(b ...KeyboardButton) *ReplyKeyboardBuilder {
	p := make([]*KeyboardButton, len(b))
	for i := range b {
		p[i] = &b[i]
	}
	k.rows = append(k.rows, &KeyboardButtonRow{Buttons: p})
	return k
}
func (k *ReplyKeyboardBuilder) Build(o ...BuildReplyOptions) *ReplyKeyboardMarkup {
	v := getVariadic(o, BuildReplyOptions{})
	return &ReplyKeyboardMarkup{Resize: v.ResizeKeyboard, SingleUse: v.OneTime, Selective: v.Selective, Persistent: v.Persistent, Placeholder: v.Placeholder, Rows: k.rows}
}

func (ButtonBuilder) Data(t, d string) KeyboardInlineButton {
	return KeyboardInlineButton{Text: t, Type: &InlineButtonTypeCallback{Data: []byte(d)}}
}
func (ButtonBuilder) URL(t, u string) KeyboardInlineButton {
	return KeyboardInlineButton{Text: t, Type: &InlineButtonTypeURL{URL: u}}
}
func (ButtonBuilder) SwitchInline(t string, s bool, q string) KeyboardInlineButton {
	return KeyboardInlineButton{Text: t, Type: &InlineButtonTypeSwitchInline{SamePeer: s, Query: q}}
}
func (ButtonBuilder) WebView(t, u string) KeyboardInlineButton {
	return KeyboardInlineButton{Text: t, Type: &InlineButtonTypeWebView{URL: u}}
}
func (ButtonBuilder) Game(t string) KeyboardInlineButton {
	return KeyboardInlineButton{Text: t, Type: &InlineButtonTypeGame{}}
}
func (ButtonBuilder) Buy(t string) KeyboardInlineButton {
	return KeyboardInlineButton{Text: t, Type: &InlineButtonTypeBuy{}}
}
func (ButtonBuilder) Copy(t, c string) KeyboardInlineButton {
	return KeyboardInlineButton{Text: t, Type: &InlineButtonTypeCopy{CopyText: c}}
}
func (ButtonBuilder) Text(t string) KeyboardButton {
	return KeyboardButton{Text: t, Type: &ButtonTypeDefault{}}
}
func (ButtonBuilder) RequestLocation(t string) KeyboardButton {
	return KeyboardButton{Text: t, Type: &ButtonTypeRequestGeoLocation{}}
}
func (ButtonBuilder) RequestPhone(t string) KeyboardButton {
	return KeyboardButton{Text: t, Type: &ButtonTypeRequestPhone{}}
}
func (ButtonBuilder) RequestPoll(t string, q bool) KeyboardButton {
	return KeyboardButton{Text: t, Type: &ButtonTypeRequestPoll{Quiz: q}}
}
func (ButtonBuilder) Clear() *ReplyKeyboardHide { return &ReplyKeyboardHide{} }

func (m *NewMessage) Click(o ...any) (*MessagesBotCallbackAnswer, error) {
	if m.ReplyMarkup() == nil {
		return nil, errors.New("replyMarkup: message has no buttons")
	}
	mk, ok := (*m.ReplyMarkup()).(*ReplyInlineMarkup)
	if !ok {
		return nil, errors.New("replyMarkup: not inline markup")
	}
	for x, r := range mk.Rows {
		for y, b := range r.Buttons {
			match := len(o) == 0 && x == 0 && y == 0
			if len(o) > 0 {
				switch v := o[0].(type) {
				case string:
					match = strings.EqualFold(b.Text, v)
				case []byte:
					t, z := b.Type.(*InlineButtonTypeCallback)
					match = z && bytes.Equal(t.Data, v)
				case []int:
					match = len(v) == 2 && v[0] == x && v[1] == y
				}
			}
			if match {
				if t, z := b.Type.(*InlineButtonTypeCallback); z {
					return m.Client.MessagesGetBotCallbackAnswer(&MessagesGetBotCallbackAnswerParams{Peer: m.Peer, MsgID: m.ID, Data: t.Data})
				}
			}
		}
	}
	return nil, errors.New("replyMarkup: callback button not found")
}
