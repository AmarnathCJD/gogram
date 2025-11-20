// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"context"
	"fmt"
	"time"

	"errors"
)

type InlineSendOptions struct {
	Gallery      bool   `json:"gallery,omitempty"`
	NextOffset   string `json:"next_offset,omitempty"`
	CacheTime    int32  `json:"cache_time,omitempty"`
	Private      bool   `json:"private,omitempty"`
	SwitchPm     string `json:"switch_pm,omitempty"`
	SwitchPmText string `json:"switch_pm_text,omitempty"`
}

func (c *Client) AnswerInlineQuery(QueryID int64, Results []InputBotInlineResult, Options ...*InlineSendOptions) (bool, error) {
	options := getVariadic(Options, &InlineSendOptions{})
	options.CacheTime = getValue(options.CacheTime, 60)
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
			StartParam: getValue(options.SwitchPmText, "start"),
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

// BOT COMMANDS

func (c *Client) SetBotCommands(commands []*BotCommand, scope *BotCommandScope, languageCode ...string) (bool, error) {
	resp, err := c.BotsSetBotCommands(*scope, getVariadic(languageCode, "en"), commands)
	if err != nil {
		return false, err
	}
	return resp, nil
}

func (c *Client) SetBotDefaultPrivileges(privileges *ChatAdminRights, ForChannels ...bool) (resp bool, err error) {
	forCh := getVariadic(ForChannels, true)
	if forCh {
		resp, err = c.BotsSetBotBroadcastDefaultAdminRights(privileges)
		return
	}
	resp, err = c.BotsSetBotGroupDefaultAdminRights(privileges)
	return
}

func (c *Client) SetChatMenuButton(userID int64, button *BotMenuButton) (bool, error) {
	peer, err := c.ResolvePeer(userID)
	if err != nil {
		return false, err
	}
	peerUser, ok := peer.(*InputPeerUser)
	if !ok {
		return false, errors.New("invalid user")
	}
	resp, err := c.BotsSetBotMenuButton(&InputUserObj{AccessHash: peerUser.AccessHash, UserID: peerUser.UserID}, *button)
	if err != nil {
		return false, err
	}
	return resp, nil
}

// Broadcast walks through the update history and invokes callbacks for each unique user/chat.
// It is intended for bots that need a lightweight way to collect peers for messaging.
// An optional sleep threshold may be provided to slow down polling between difference requests.
func (c *Client) Broadcast(ctx context.Context, userCallback func(User) error, chatCallback func(Chat) error, sleepThreshold ...time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if userCallback == nil && chatCallback == nil {
		return errors.New("broadcast: at least one callback must be provided")
	}

	delay := 150 * time.Millisecond
	if len(sleepThreshold) > 0 && sleepThreshold[0] > 0 {
		delay = sleepThreshold[0]
	} else if c.clientData.sleepThresholdMs > 0 {
		delay = time.Duration(c.clientData.sleepThresholdMs) * time.Millisecond
	}

	state, err := c.UpdatesGetState()
	if err != nil {
		return fmt.Errorf("broadcast: updates.getState: %w", err)
	}

	req := &UpdatesGetDifferenceParams{
		Pts:           1,
		Date:          1,
		PtsLimit:      5000,
		PtsTotalLimit: 2147483647, // max int32
		Qts:           1,
	}
	endPts := state.Pts
	usersSeen := make(map[int64]struct{})
	chatsSeen := make(map[int64]struct{})

	for req.Pts < endPts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		updates, err := c.MakeRequestCtx(ctx, req)
		if err != nil {
			if handleIfFlood(err, c) {
				continue
			}
			return fmt.Errorf("broadcast: updates.getDifference: %w", err)
		}

		var (
			nextPts  int32
			nextQts  int32
			nextDate int32
			advance  bool
		)

		switch u := updates.(type) {
		case *UpdatesDifferenceObj:
			if err := deliverBroadcastUsers(u.Users, usersSeen, userCallback); err != nil {
				return err
			}
			if err := deliverBroadcastChats(u.Chats, chatsSeen, chatCallback); err != nil {
				return err
			}
			nextPts = u.State.Pts
			nextQts = u.State.Qts
			nextDate = u.State.Date
			advance = true
		case *UpdatesDifferenceSlice:
			if err := deliverBroadcastUsers(u.Users, usersSeen, userCallback); err != nil {
				return err
			}
			if err := deliverBroadcastChats(u.Chats, chatsSeen, chatCallback); err != nil {
				return err
			}
			nextPts = u.IntermediateState.Pts
			nextQts = u.IntermediateState.Qts
			nextDate = u.IntermediateState.Date
			advance = true
		case *UpdatesDifferenceEmpty:
			return nil
		case *UpdatesDifferenceTooLong:
			endPts = u.Pts
			continue
		default:
			return fmt.Errorf("broadcast: unexpected updates type %T", updates)
		}

		if advance {
			req.Pts = nextPts
			req.Qts = nextQts
			req.Date = nextDate
		}

	}

	return nil
}

func deliverBroadcastUsers(users []User, seen map[int64]struct{}, callback func(User) error) error {
	if callback == nil {
		return nil
	}
	for _, user := range users {
		usr, ok := user.(*UserObj)
		if !ok {
			continue
		}
		if _, exists := seen[usr.ID]; exists {
			continue
		}
		seen[usr.ID] = struct{}{}
		if err := callback(usr); err != nil {
			return fmt.Errorf("broadcast user callback: %w", err)
		}
	}
	return nil
}

func deliverBroadcastChats(chats []Chat, seen map[int64]struct{}, callback func(Chat) error) error {
	if callback == nil {
		return nil
	}
	for _, chat := range chats {
		var chatID int64
		switch ch := chat.(type) {
		case *ChatObj:
			chatID = ch.ID
		case *Channel:
			chatID = ch.ID
		default:
			continue
		}

		if _, exists := seen[chatID]; exists {
			continue
		}
		seen[chatID] = struct{}{}
		if err := callback(chat); err != nil {
			return fmt.Errorf("broadcast chat callback: %w", err)
		}
	}
	return nil
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
		entities, textPart := c.FormatMessage(message[0], c.ParseMode())
		inv.Message = &TextWithEntities{
			Text:     textPart,
			Entities: entities,
		}
	}

	form, err := c.PaymentsGetPaymentForm(inv, &DataJson{})
	if err != nil {
		return nil, err
	}

	return c.PaymentsSendStarsForm(form.(*PaymentsPaymentFormObj).FormID, inv)
}

// Transfer a saved gift to another user (must be a unique gift)
func (c *Client) SendGift(toPeer any, giftId int64, message ...string) (PaymentsPaymentResult, error) {
	mygifts, _ := c.PaymentsGetSavedStarGifts(&PaymentsGetSavedStarGiftsParams{
		Peer: &InputPeerSelf{},
	})

	userPeer, err := c.ResolvePeer(toPeer)
	if err != nil {
		return nil, err
	}

	var toSend int32

	for _, vx := range mygifts.Gifts {
		switch v := vx.Gift.(type) {
		case *StarGiftObj:
			if v.ID == giftId {
				toSend = vx.MsgID
				break
			}
		case *StarGiftUnique:
			if v.ID == giftId {
				toSend = vx.MsgID
				break
			}
		}
	}

	if toSend == 0 {
		return nil, fmt.Errorf("specified unique gift not found")
	}

	inv := &InputInvoiceStarGiftTransfer{
		ToID: userPeer,
		Stargift: &InputSavedStarGiftUser{
			MsgID: toSend,
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

	var uq = getVariadic(unique, false)
	if !uq {
		var giftsArray []*StarGift
		for _, v := range gifts.Gifts {
			giftsArray = append(giftsArray, &v.Gift)
		}
		return giftsArray, nil
	}

	var uniqueGifts []*StarGift
	for _, v := range gifts.Gifts {
		if _, ok := v.Gift.(*StarGiftUnique); ok {
			uniqueGifts = append(uniqueGifts, &v.Gift)
		}
	}

	return uniqueGifts, nil
}
