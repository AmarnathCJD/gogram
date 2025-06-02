// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
)

type InlineSendOptions struct {
	Gallery      bool   `json:"gallery,omitempty"`
	NextOffset   string `json:"next_offset,omitempty"`
	CacheTime    int32  `json:"cache_time,omitempty"`
	Private      bool   `json:"private,omitempty"`
	SwitchPm     string `json:"switch_pm,omitempty"`
	SwitchPmText string `json:"switch_pm_text,omitempty"`
}

const (
	defaultCacheTime  = 60
	defaultStartParam = "start"
)

// AnswerInlineQuery responds to an inline query with results
func (c *Client) AnswerInlineQuery(QueryID int64, Results []InputBotInlineResult, Options ...*InlineSendOptions) (bool, error) {
	options := getVariadic(Options, &InlineSendOptions{})
	options.CacheTime = getValue(options.CacheTime, defaultCacheTime)

	request := &MessagesSetInlineBotResultsParams{
		Gallery:    options.Gallery,
		Private:    options.Private,
		QueryID:    QueryID,
		Results:    Results,
		CacheTime:  options.CacheTime,
		NextOffset: options.NextOffset,
	}
	if options.SwitchPm != "" {
		request.SwitchPm = &InlineBotSwitchPm{
			Text:       options.SwitchPm,
			StartParam: getValue(options.SwitchPmText, defaultStartParam),
		}
	}

	resp, err := c.MessagesSetInlineBotResults(request)
	if err != nil {
		return false, err
	}
	return resp, nil
}

type CallbackOptions struct {
	Alert     bool   `json:"alert,omitempty"`
	CacheTime int32  `json:"cache_time,omitempty"`
	URL       string `json:"url,omitempty"`
}

// AnswerCallbackQuery responds to a callback query
func (c *Client) AnswerCallbackQuery(QueryID int64, Text string, Opts ...*CallbackOptions) (bool, error) {
	options := getVariadic(Opts, &CallbackOptions{})
	request := &MessagesSetBotCallbackAnswerParams{
		QueryID: QueryID,
		Message: Text,
		Alert:   options.Alert,
	}
	if options.URL != "" {
		request.URL = options.URL
	}
	if options.CacheTime != 0 {
		request.CacheTime = options.CacheTime
	}
	resp, err := c.MessagesSetBotCallbackAnswer(request)
	if err != nil {
		return false, err
	}
	return resp, nil
}

// SetBotCommands configures bot commands
func (c *Client) SetBotCommands(commands []*BotCommand, scope *BotCommandScope, languageCode ...string) (bool, error) {
	language := getVariadic(languageCode, "en")
	return c.BotsSetBotCommands(*scope, language, commands)
}

// SetBotDefaultPrivileges sets default admin rights for the bot
func (c *Client) SetBotDefaultPrivileges(privileges *ChatAdminRights, forChannels ...bool) (resp bool, err error) {
	if getVariadic(forChannels, true) {
		return c.BotsSetBotBroadcastDefaultAdminRights(privileges)
	}
	return c.BotsSetBotGroupDefaultAdminRights(privileges)
}

// SetChatMenuButton sets the bot's menu button for a specific user
func (c *Client) SetChatMenuButton(userID int64, button *BotMenuButton) (bool, error) {
	peer, err := c.ResolvePeer(userID)
	if err != nil {
		return false, err
	}

	peerUser, ok := peer.(*InputPeerUser)
	if !ok {
		return false, errors.New("invalid user peer")
	}

	inputUser := &InputUserObj{
		AccessHash: peerUser.AccessHash,
		UserID:     peerUser.UserID,
	}

	return c.BotsSetBotMenuButton(inputUser, *button)
}

// In testing stage, TODO
// returns list of users and chats in chans
func (c *Client) Broadcast(ctx ...context.Context) (chan User, chan Chat, error) {
	var ctxC context.Context
	if len(ctx) > 0 {
		ctxC = ctx[0]
	} else {
		ctxC = context.Background()
	}

	state, err := c.UpdatesGetState()
	if err != nil {
		return nil, nil, err
	}

	userChan := make(chan User, 100)
	chatChan := make(chan Chat, 100)

	go c.broadcastWorker(ctxC, state.Pts, userChan, chatChan)

	return userChan, chatChan, nil
}

func (c *Client) broadcastWorker(ctx context.Context, endPts int32, userChan chan<- User, chatChan chan<- Chat) {
	defer close(userChan)
	defer close(chatChan)

	req := &UpdatesGetDifferenceParams{
		Pts:           1,
		Date:          1,
		PtsLimit:      5000,
		PtsTotalLimit: 2147483647, // max int32
		Qts:           1,
	}

	users := make(map[int64]struct{})
	chats := make(map[int64]struct{})

	for req.Pts < endPts {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, err := c.MakeRequestCtx(ctx, req)
		if err != nil {
			if handleIfFlood(err, c) {
				continue
			}
			c.Logger.Error(err)
			return
		}

		switch u := updates.(type) {
		case *UpdatesDifferenceObj:
			c.processDifference(u, users, chats, userChan, chatChan)
			req.Pts = u.State.Pts
			req.Qts = u.State.Qts
			req.Date = u.State.Date

		case *UpdatesDifferenceSlice:
			c.processDifferenceSlice(u, users, chats, userChan, chatChan)
			req.Pts = u.IntermediateState.Pts
			req.Qts = u.IntermediateState.Qts
			req.Date = u.IntermediateState.Date

		case *UpdatesDifferenceEmpty:
			break

		case *UpdatesDifferenceTooLong:
			endPts = u.Pts
		}

		time.Sleep(150 * time.Millisecond)
	}
}

func (c *Client) processDifference(diff *UpdatesDifferenceObj, users map[int64]struct{}, chats map[int64]struct{}, userChan chan<- User, chatChan chan<- Chat) {
	for _, user := range diff.Users {
		if uz, ok := user.(*UserObj); ok {
			if _, exists := users[uz.ID]; !exists {
				userChan <- uz
				users[uz.ID] = struct{}{}
			}
		}
	}

	for _, chat := range diff.Chats {
		switch cz := chat.(type) {
		case *ChatObj, *Channel:
			id := getChatID(cz)
			if _, exists := chats[id]; !exists {
				chatChan <- cz
				chats[id] = struct{}{}
			}
		}
	}
}

func (c *Client) processDifferenceSlice(diff *UpdatesDifferenceSlice, users map[int64]struct{}, chats map[int64]struct{}, userChan chan<- User, chatChan chan<- Chat) {
	for _, user := range diff.Users {
		if uz, ok := user.(*UserObj); ok {
			if _, exists := users[uz.ID]; !exists {
				userChan <- uz
				users[uz.ID] = struct{}{}
			}
		}
	}

	for _, chat := range diff.Chats {
		switch cz := chat.(type) {
		case *ChatObj, *Channel:
			id := getChatID(cz)
			if _, exists := chats[id]; !exists {
				chatChan <- cz
				chats[id] = struct{}{}
			}
		}
	}
}

func getChatID(chat interface{}) int64 {
	switch c := chat.(type) {
	case *ChatObj:
		return c.ID
	case *Channel:
		return c.ID
	default:
		return 0
	}
}

// Buy a gift for a user
func (c *Client) SendNewGift(toPeer any, giftId int64, message ...string) (PaymentsPaymentResult, error) {
	userPeer, err := c.ResolvePeer(toPeer)
	if err != nil {
		return nil, err
	}

	inv := &InputInvoiceStarGift{
		Peer:           userPeer,
		GiftID:         giftId,
		IncludeUpgrade: false,
		HideName:       false,
	}

	if len(message) > 0 {
		entites, textPart := c.FormatMessage(message[0], c.ParseMode())
		inv.Message = &TextWithEntities{
			Text:     textPart,
			Entities: entites,
		}
	}

	form, err := c.PaymentsGetPaymentForm(inv, &DataJson{})
	if err != nil {
		return nil, err
	}

	return c.PaymentsSendStarsForm(form.(*PaymentsPaymentFormObj).FormID, inv)
}

// Transfer a saved gift to another user (must be a unique gift)
func (c *Client) SendGift(toPeer any, giftID int64, message ...string) (PaymentsPaymentResult, error) {
	mygifts, err := c.PaymentsGetSavedStarGifts(&PaymentsGetSavedStarGiftsParams{
		Peer: &InputPeerSelf{},
	})
	if err != nil {
		return nil, err
	}

	userPeer, err := c.ResolvePeer(toPeer)
	if err != nil {
		return nil, err
	}

	var msgID int32
	for _, vx := range mygifts.Gifts {
		switch v := vx.Gift.(type) {
		case *StarGiftObj:
			if v.ID == giftID {
				msgID = vx.MsgID
				break
			}
		case *StarGiftUnique:
			if v.ID == giftID {
				msgID = vx.MsgID
				break
			}
		}
	}

	if msgID == 0 {
		return nil, fmt.Errorf("specified unique gift not found")
	}

	inv := &InputInvoiceStarGiftTransfer{
		ToID: userPeer,
		Stargift: &InputSavedStarGiftUser{
			MsgID: msgID,
		},
	}

	form, err := c.PaymentsGetPaymentForm(inv, &DataJson{})
	if err != nil {
		return nil, err
	}

	return c.PaymentsSendStarsForm(form.(*PaymentsPaymentFormStarGift).FormID, inv)
}

func (c *Client) GetMyGifts(unique ...bool) ([]*StarGift, error) {
	gifts, err := c.PaymentsGetSavedStarGifts(&PaymentsGetSavedStarGiftsParams{
		Peer: &InputPeerSelf{},
	})
	if err != nil {
		return nil, err
	}

	if !getVariadic(unique, false) {
		result := make([]*StarGift, len(gifts.Gifts))
		for i, v := range gifts.Gifts {
			result[i] = &v.Gift
		}
		return result, nil
	}

	var uniqueGifts []*StarGift
	for _, v := range gifts.Gifts {
		if _, ok := v.Gift.(*StarGiftUnique); ok {
			uniqueGifts = append(uniqueGifts, &v.Gift)
		}
	}

	return uniqueGifts, nil
}
