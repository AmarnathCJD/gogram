// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"errors"
)

type InlineSendOptions struct {
	Gallery          bool   // Display results as a gallery grid
	NextOffset       string // Offset for pagination in inline results
	CacheTime        int32  // Cache duration in seconds (default: 60)
	Private          bool   // Results visible only to the requesting user
	SwitchPm         string // Text for the "Switch to PM" above inline results
	SwitchPmText     string // Deep link parameter for start= in Switch to PM
	SwitchWebView    string // Text for "Switch to WebView" button (for game bots)
	SwitchWebViewURL string // URL for "Switch to WebView" button (for game bots)
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
	if options.SwitchWebView != "" && options.SwitchWebViewURL != "" {
		request.SwitchWebview = &InlineBotWebView{
			Text: options.SwitchWebView,
			URL:  options.SwitchWebViewURL,
		}
	}
	resp, err := c.MessagesSetInlineBotResults(request)
	if err != nil {
		return false, err
	}
	return resp, nil
}

type CallbackOptions struct {
	Alert     bool   // Show as alert popup instead of toast notification
	CacheTime int32  // Cache duration for the answer in seconds
	URL       string // URL to open (for game bots)
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

type StickerKind int

const (
	StickerRegular StickerKind = iota
	StickerMask
	StickerEmoji
)

type StickerInput struct {
	Document InputDocument
	Emoji    string
	Keywords string
	Mask     *MaskCoords
}

type CreateStickerSetOptions struct {
	Kind      StickerKind
	TextColor bool
	Software  string
	Thumb     InputDocument
}

func (c *Client) nzStickerShortName(shortName string) (string, error) {
	if !c.clientData.botAcc {
		return shortName, nil
	}
	me := c.Me()
	if me == nil || me.Username == "" {
		return "", errors.New("bot has no username; cannot derive sticker pack suffix")
	}
	suffix := "_by_" + me.Username
	if strings.HasSuffix(strings.ToLower(shortName), strings.ToLower(suffix)) {
		return shortName, nil
	}
	return shortName + suffix, nil
}

func (c *Client) CreateStickerSet(owner any, title, shortName string, stickers []StickerInput, opts ...*CreateStickerSetOptions) (MessagesStickerSet, error) {
	if title == "" || shortName == "" {
		return nil, errors.New("title and shortName are required")
	}
	if len(stickers) == 0 {
		return nil, errors.New("at least one sticker required")
	}
	finalShortName, err := c.nzStickerShortName(shortName)
	if err != nil {
		return nil, err
	}
	opt := getVariadic(opts, &CreateStickerSetOptions{})
	peer, err := c.ResolvePeer(owner)
	if err != nil {
		return nil, fmt.Errorf("resolve owner: %w", err)
	}
	user := toInputUser(peer)

	items := make([]*InputStickerSetItem, len(stickers))
	for i, s := range stickers {
		if s.Document == nil {
			return nil, fmt.Errorf("sticker %d missing Document", i)
		}
		if s.Emoji == "" {
			return nil, fmt.Errorf("sticker %d missing Emoji", i)
		}
		items[i] = &InputStickerSetItem{
			Document:   s.Document,
			Emoji:      s.Emoji,
			Keywords:   s.Keywords,
			MaskCoords: s.Mask,
		}
	}

	params := &StickersCreateStickerSetParams{
		UserID:    user,
		Title:     title,
		ShortName: finalShortName,
		Stickers:  items,
		Software:  opt.Software,
		Thumb:     opt.Thumb,
		TextColor: opt.TextColor,
	}
	switch opt.Kind {
	case StickerMask:
		params.Masks = true
	case StickerEmoji:
		params.Emojis = true
	}
	return c.StickersCreateStickerSet(params)
}

func (c *Client) AddSticker(set InputStickerSet, sticker StickerInput) (MessagesStickerSet, error) {
	if set == nil {
		return nil, errors.New("set is nil")
	}
	if sticker.Document == nil || sticker.Emoji == "" {
		return nil, errors.New("Document and Emoji required")
	}
	return c.StickersAddStickerToSet(set, &InputStickerSetItem{
		Document:   sticker.Document,
		Emoji:      sticker.Emoji,
		Keywords:   sticker.Keywords,
		MaskCoords: sticker.Mask,
	})
}

func (c *Client) RemoveSticker(sticker InputDocument) (MessagesStickerSet, error) {
	if sticker == nil {
		return nil, errors.New("sticker is nil")
	}
	return c.StickersRemoveStickerFromSet(sticker)
}

func (c *Client) MoveSticker(sticker InputDocument, position int32) (MessagesStickerSet, error) {
	if sticker == nil {
		return nil, errors.New("sticker is nil")
	}
	return c.StickersChangeStickerPosition(sticker, position)
}

func (c *Client) ReplaceSticker(sticker InputDocument, newSticker StickerInput) (MessagesStickerSet, error) {
	if sticker == nil || newSticker.Document == nil {
		return nil, errors.New("both stickers required")
	}
	return c.StickersReplaceSticker(sticker, &InputStickerSetItem{
		Document:   newSticker.Document,
		Emoji:      newSticker.Emoji,
		Keywords:   newSticker.Keywords,
		MaskCoords: newSticker.Mask,
	})
}

func (c *Client) EditSticker(sticker InputDocument, emoji string, keywords string, mask *MaskCoords) (MessagesStickerSet, error) {
	if sticker == nil {
		return nil, errors.New("sticker is nil")
	}
	return c.StickersChangeSticker(sticker, emoji, mask, keywords)
}

func (c *Client) RenameStickerSet(set InputStickerSet, title string) (MessagesStickerSet, error) {
	if set == nil || title == "" {
		return nil, errors.New("set and title required")
	}
	return c.StickersRenameStickerSet(set, title)
}

func (c *Client) DeleteStickerSet(set InputStickerSet) error {
	if set == nil {
		return errors.New("set is nil")
	}
	_, err := c.StickersDeleteStickerSet(set)
	return err
}

func (c *Client) SetStickerSetThumb(set InputStickerSet, thumb InputDocument) (MessagesStickerSet, error) {
	if set == nil {
		return nil, errors.New("set is nil")
	}
	return c.StickersSetStickerSetThumb(set, thumb, 0)
}

func (c *Client) CheckStickerShortName(shortName string) (bool, error) {
	if shortName == "" {
		return false, errors.New("shortName is required")
	}
	return c.StickersCheckShortName(shortName)
}

func (c *Client) SuggestStickerShortName(title string) (string, error) {
	if title == "" {
		return "", errors.New("title is required")
	}
	resp, err := c.StickersSuggestShortName(title)
	if err != nil {
		return "", err
	}
	return resp.ShortName, nil
}
