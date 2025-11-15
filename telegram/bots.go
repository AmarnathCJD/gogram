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

// In testing stage, TODO
// returns list of users and chats in chans
func (c *Client) Broadcast(ctx ...context.Context) (chan User, chan Chat, error) {
	var ctxC context.Context
	if len(ctx) > 0 {
		ctxC = ctx[0]
	} else {
		ctxC = context.Background()
	}

	s, err := c.UpdatesGetState()
	if err != nil {
		return nil, nil, err
	}

	endPts := s.Pts

	var users = make(map[int64]User)
	var chats = make(map[int64]Chat)

	var userChan = make(chan User, 100)
	var chatChan = make(chan Chat, 100)

	req := &UpdatesGetDifferenceParams{
		Pts:           1,
		Date:          1,
		PtsLimit:      5000,
		PtsTotalLimit: 2147483647, // max int32
		Qts:           1,
	}

	go func() {
		defer close(userChan)
		defer close(chatChan)

		for req.Pts < endPts {
			select {
			case <-ctxC.Done():
				return
			default:
			}

			updates, err := c.MakeRequestCtx(ctxC, req)
			if err != nil {
				if handleIfFlood(err, c) {
					continue
				}
				c.Logger.WithError(err).Error("")
				return
			}

			switch u := updates.(type) {
			case *UpdatesDifferenceObj:
				for _, user := range u.Users {
					switch uz := user.(type) {
					case *UserObj:
						if _, ok := users[uz.ID]; !ok {
							userChan <- uz
						}
						users[uz.ID] = uz
					}
				}
				for _, chat := range u.Chats {
					switch cz := chat.(type) {
					case *ChatObj:
						if _, ok := chats[cz.ID]; !ok {
							chatChan <- cz
						}
						chats[cz.ID] = cz
					case *Channel:
						if _, ok := chats[cz.ID]; !ok {
							chatChan <- cz
						}
						chats[cz.ID] = cz
					}
				}

				req.Pts = u.State.Pts
				req.Qts = u.State.Qts
				req.Date = u.State.Date
			case *UpdatesDifferenceSlice:
				for _, user := range u.Users {
					switch uz := user.(type) {
					case *UserObj:
						if _, ok := users[uz.ID]; !ok {
							userChan <- uz
						}
						users[uz.ID] = uz
					}
				}
				for _, chat := range u.Chats {
					switch cz := chat.(type) {
					case *ChatObj:
						if _, ok := chats[cz.ID]; !ok {
							chatChan <- cz
						}
						chats[cz.ID] = cz
					case *Channel:
						if _, ok := chats[cz.ID]; !ok {
							chatChan <- cz
						}
						chats[cz.ID] = cz
					}
				}

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
	}()

	return userChan, chatChan, nil
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
