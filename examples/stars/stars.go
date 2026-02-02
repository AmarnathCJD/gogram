package examples

import (
	"fmt"

	"github.com/amarnathcjd/gogram/telegram"
)

var paidUsers = make(map[int64]int32)

func main() {
	bot, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:   123456,
		AppHash: "YOUR_APP_HASH",
	})

	bot.Conn()
	bot.LoginBot("YOUR_BOT_TOKEN")

	bot.AddRawHandler(&telegram.UpdateBotPrecheckoutQuery{}, preCheckoutQueryUpd)
	bot.On("edit", func(m *telegram.NewMessage) error {
		paidUsers[m.SenderID()] = m.Invoice().ReceiptMsgID
		return bot.E(m.Respond("Payment Successful!"))
	}, telegram.FilterFunc(func(upd *telegram.NewMessage) bool {
		return upd.Invoice() != nil && upd.Invoice().ReceiptMsgID != 0
	}))

	bot.On("message:pay", payCmd)
	bot.On("message:status", statusCmd)
	bot.On("message:refund", refundCmd) // new type of message handlers, eg<>
}

func payCmd(m *telegram.NewMessage) error {
	invoice := telegram.InputMediaInvoice{
		Title:       "Test Product",
		Description: "Test Description",
		Payload:     []byte(""),
		Invoice: &telegram.Invoice{
			Test:     true,
			Currency: "USD",
			Prices: []*telegram.LabeledPrice{
				{
					Amount: 1,
					Label:  "1 USD",
				},
			},
		},
		ProviderData: &telegram.DataJson{},
	}

	m.ReplyMedia(&invoice)
	return nil
}

func preCheckoutQueryUpd(upd telegram.Update, c *telegram.Client) error {
	_upd := upd.(*telegram.UpdateBotPrecheckoutQuery)
	c.MessagesSetBotPrecheckoutResults(true, _upd.QueryID, "Success")
	return nil
}

func statusCmd(m *telegram.NewMessage) error {
	if _, ok := paidUsers[m.SenderID()]; ok {
		return m.E(m.Respond("You have paid!"))
	}
	return m.E(m.Respond("You have not paid!"))
}

func refundCmd(m *telegram.NewMessage) error {
	if recpt_id, ok := paidUsers[m.SenderID()]; ok {
		delete(paidUsers, m.SenderID())
		u, _ := m.Client.GetSendableUser(m.SenderID())
		m.Client.PaymentsRefundStarsCharge(u, fmt.Sprintf("%d", recpt_id))
		return m.E(m.Respond("Refund Successful!"))
	}
	return m.E(m.Respond("You have not paid!"))
}
