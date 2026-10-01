package telegram

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"errors"
)

// GetMe returns the current user
func (c *Client) GetMe() (*UserObj, error) {
	resp, err := c.UsersGetUsers([]InputUser{&InputUserSelf{}})
	if err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}
	if len(resp) != 1 {
		return nil, fmt.Errorf("getting user: expected one self user, got %d", len(resp))
	}
	user, ok := resp[0].(*UserObj)
	if !ok || user == nil {
		return nil, errors.New("got wrong response: " + reflect.TypeOf(resp).String())
	}
	c.setMe(user)
	if c.Cache != nil {
		if err := c.Cache.BindToUser(user.ID); err != nil {
			c.Log.WithError(err).Warn("failed to bind cache to user")
		}
	}

	return user, nil
}

type PhotosOptions struct {
	MaxID  int64 // Maximum photo ID to return (for pagination)
	Offset int32 // Number of photos to skip
	Limit  int32 // Maximum number of photos to return (max: 80)
}

type UserPhoto struct {
	Photo Photo
}

func (p *UserPhoto) FileID() string {
	return PackBotFileID(p.Photo)
}

func (p *UserPhoto) FileSize() int64 {
	if p == nil {
		return 0
	}
	_, _, size, _, err := GetFileLocation(p.Photo, FileLocationOptions{Video: true})
	if err != nil {
		return 0
	}
	return size
}

func (p *UserPhoto) DcID() int32 {
	if p != nil {
		if photo, ok := p.Photo.(*PhotoObj); ok && photo != nil {
			return photo.DcID
		}
	}
	return 4
}

func (p *UserPhoto) InputLocation() (*InputPhotoFileLocation, error) {
	if p == nil {
		return nil, errors.New("photo is nil")
	}
	location, _, _, _, err := GetFileLocation(p.Photo, FileLocationOptions{Video: true})
	if err != nil {
		return nil, err
	}
	photo, ok := location.(*InputPhotoFileLocation)
	if !ok {
		return nil, fmt.Errorf("could not convert photo: %T", p.Photo)
	}
	return photo, nil
}

// GetProfilePhotos returns the profile photos of a user
//
//	Params:
//	 - userID: The user ID
//	 - Offset: The offset to start from
//	 - Limit: The number of photos to return
//	 - MaxID: The maximum ID of the photo to return
func (c *Client) GetProfilePhotos(userID any, Opts ...*PhotosOptions) ([]UserPhoto, error) {
	Options := *getVariadic(Opts, &PhotosOptions{})
	if Options.Limit > 80 {
		Options.Limit = 80
	} else if Options.Limit < 1 {
		Options.Limit = 1
	}
	peer, err := c.GetSendableUser(userID)
	if err != nil {
		return nil, err
	}
	resp, err := c.PhotosGetUserPhotos(
		peer,
		Options.Offset,
		Options.MaxID,
		Options.Limit,
	)
	if err != nil {
		return nil, err
	}
	switch p := resp.(type) {
	case *PhotosPhotosObj:
		c.Cache.UpdatePeersToCache(p.Users, []Chat{})
		photos := make([]UserPhoto, len(p.Photos))
		for i, photo := range p.Photos {
			photos[i] = UserPhoto{Photo: photo}
		}
		return photos, nil
	case *PhotosPhotosSlice:
		c.Cache.UpdatePeersToCache(p.Users, []Chat{})
		photos := make([]UserPhoto, len(p.Photos))
		for i, photo := range p.Photos {
			photos[i] = UserPhoto{Photo: photo}
		}
		return photos, nil
	default:
		return nil, errors.New("could not convert photos: " + reflect.TypeOf(resp).String())
	}
}

type DialogOptions struct {
	OffsetID         int32             // Message ID to start from
	OffsetDate       int32             // Unix timestamp to start from
	OffsetPeer       InputPeer         // Peer to start from
	Limit            int32             // Maximum number of dialogs to return
	ExcludePinned    bool              // Exclude pinned dialogs from results
	FolderID         int32             // Folder ID to get dialogs from (0 for main list)
	Hash             int64             // Hash for caching
	SleepThresholdMs int32             // Delay between requests in milliseconds
	Context          context.Context   // Context for cancellation
	ErrorCallback    IterErrorCallback // callback for handling errors with progress info
}

type TLDialog struct {
	Dialog     Dialog
	Peer       Peer
	TopMessage int32
	PeerType   int
}

func (d *TLDialog) IsUser() bool {
	return d.PeerType == 1
}

func (d *TLDialog) IsChat() bool {
	return d.PeerType == 2
}

func (d *TLDialog) IsChannel() bool {
	return d.PeerType == 3
}

func (d *TLDialog) GetID() int64 {
	switch p := d.Peer.(type) {
	case *PeerUser:
		return p.UserID
	case *PeerChat:
		return p.ChatID
	case *PeerChannel:
		return p.ChannelID
	}
	return 0
}

func (d *TLDialog) GetChannelID() int64 {
	if d.Peer != nil {
		switch peer := d.Peer.(type) {
		case *PeerChannel:
			if peer != nil {
				return -100_000_000_0000 - peer.ChannelID
			}
		case *PeerChat:
			if peer != nil {
				return -peer.ChatID
			}
		case *PeerUser:
			if peer != nil {
				return peer.UserID
			}
		}
	}
	return 0
}

func (d *TLDialog) GetInputPeer(c *Client) (InputPeer, error) {
	return c.GetSendablePeer(d.Peer)
}

func (d *TLDialog) GetUser(c *Client) (*UserObj, error) {
	return c.GetUser(d.GetID())
}

func (d *TLDialog) GetChat(c *Client) (*ChatObj, error) {
	return c.GetChat(d.GetID())
}

func (d *TLDialog) GetChannel(c *Client) (*Channel, error) {
	return c.GetChannel(d.GetID())
}

func (c *Client) GetDialogs(opts ...*DialogOptions) ([]TLDialog, error) {
	var dialogs []TLDialog
	err := c.IterDialogs(func(d *TLDialog) error {
		dialogs = append(dialogs, *d)
		return nil
	}, opts...)
	return dialogs, err
}

func (c *Client) IterDialogs(callback func(*TLDialog) error, opts ...*DialogOptions) error {
	return c.iterDialogs(callback, func(ctx context.Context, req *MessagesGetDialogsParams) (any, error) {
		return c.MakeRequest(ctx, req)
	}, opts...)
}

func (c *Client) iterDialogs(callback func(*TLDialog) error, fetch func(context.Context, *MessagesGetDialogsParams) (any, error), opts ...*DialogOptions) error {
	if callback == nil {
		return errors.New("dialog callback is nil")
	}
	options := *getVariadic(opts, &DialogOptions{Limit: 1, SleepThresholdMs: 20})
	if options.OffsetPeer == nil {
		options.OffsetPeer = &InputPeerEmpty{}
	}
	if options.SleepThresholdMs == 0 {
		options.SleepThresholdMs = 20
	}
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	req := &MessagesGetDialogsParams{
		OffsetDate: options.OffsetDate, OffsetID: options.OffsetID,
		OffsetPeer: options.OffsetPeer, ExcludePinned: options.ExcludePinned,
		FolderID: options.FolderID, Hash: options.Hash,
	}
	seen := make(map[[2]int64]struct{})
	var fetched int32
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		req.Limit = 100
		if options.Limit > 0 {
			req.Limit = min(req.Limit, options.Limit-fetched)
			if req.Limit <= 0 {
				return nil
			}
		}
		resp, err := fetch(ctx, req)
		if err != nil {
			if handleIfFlood(err, c, ctx) {
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if options.ErrorCallback != nil && options.ErrorCallback(err, &IterProgressInfo{
				Fetched: fetched, Limit: options.Limit, Offset: req.OffsetID,
			}) {
				continue
			}
			return err
		}
		var dialogs []Dialog
		var messages []Message
		complete := false
		if isNilSource(resp) {
			return fmt.Errorf("unexpected dialogs response: %T", resp)
		}
		switch p := resp.(type) {
		case *MessagesDialogsObj:
			dialogs, messages, complete = p.Dialogs, p.Messages, true
			c.Cache.UpdatePeersToCache(p.Users, p.Chats)
		case *MessagesDialogsSlice:
			dialogs, messages = p.Dialogs, p.Messages
			c.Cache.UpdatePeersToCache(p.Users, p.Chats)
		case *MessagesDialogsNotModified:
			return nil
		default:
			return fmt.Errorf("unexpected dialogs response: %T", resp)
		}

		dates := make(map[[3]int64]int32, len(messages))
		for _, raw := range messages {
			var peer Peer
			var id, date int32
			switch m := raw.(type) {
			case *MessageObj:
				if m != nil {
					peer, id, date = m.PeerID, m.ID, m.Date
				}
			case *MessageService:
				if m != nil {
					peer, id, date = m.PeerID, m.ID, m.Date
				}
			}
			if !isNilSource(peer) {
				dates[[3]int64{int64(peer.CRC()), c.GetPeerID(peer), int64(id)}] = date
			}
		}
		var nextPeer Peer
		var nextID, nextDate int32
		for i := len(dialogs) - 1; i >= 0; i-- {
			d := packDialog(dialogs[i])
			if isNilSource(d.Peer) {
				continue
			}
			if date, ok := dates[[3]int64{int64(d.Peer.CRC()), d.GetID(), int64(d.TopMessage)}]; ok {
				nextPeer, nextID, nextDate = d.Peer, d.TopMessage, date
				break
			}
		}
		before := fetched
		for _, raw := range dialogs {
			d := packDialog(raw)
			if isNilSource(d.Peer) {
				continue
			}
			key := [2]int64{int64(d.Peer.CRC()), d.GetID()}
			if folder, ok := raw.(*DialogFolder); ok && folder.Folder != nil {
				key = [2]int64{int64(folder.CRC()), int64(folder.Folder.ID)}
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := callback(&d); err != nil {
				if errors.Is(err, ErrStopIteration) {
					return nil
				}
				return err
			}
			fetched++
			if options.Limit > 0 && fetched >= options.Limit {
				return nil
			}
		}
		if complete || len(dialogs) < int(req.Limit) || fetched == before || nextPeer == nil {
			return nil
		}
		if req.ExcludePinned && req.OffsetID == nextID && req.OffsetDate == nextDate &&
			c.GetPeerType(req.OffsetPeer) == c.GetPeerType(nextPeer) && c.GetPeerID(req.OffsetPeer) == c.GetPeerID(nextPeer) {
			return nil
		}
		peer, err := c.GetSendablePeer(nextPeer)
		if err != nil {
			return err
		}
		req.OffsetPeer, req.OffsetID, req.OffsetDate = peer, nextID, nextDate
		req.ExcludePinned = true
		req.Hash = 0
		if err := sleepContext(ctx, time.Duration(options.SleepThresholdMs)*time.Millisecond); err != nil {
			return err
		}
	}
}

func packDialog(dialog Dialog) TLDialog {
	dl := TLDialog{Dialog: dialog}
	switch d := dialog.(type) {
	case *DialogObj:
		if d != nil {
			dl.Peer, dl.TopMessage = d.Peer, d.TopMessage
		}
	case *DialogFolder:
		if d != nil {
			dl.Peer, dl.TopMessage = d.Peer, d.TopMessage
		}
	}
	switch dl.Peer.(type) {
	case *PeerUser:
		dl.PeerType = 1
	case *PeerChat:
		dl.PeerType = 2
	case *PeerChannel:
		dl.PeerType = 3
	}
	return dl
}

// GetCommonChats returns the common chats of a user
func (c *Client) GetCommonChats(userId any) ([]Chat, error) {
	peer, err := c.GetSendableUser(userId)
	if err != nil {
		return nil, err
	}
	resp, err := c.MessagesGetCommonChats(peer, 0, 100)
	if err != nil {
		return nil, err
	}
	switch p := resp.(type) {
	case *MessagesChatsObj:
		c.Cache.UpdatePeersToCache([]User{}, p.Chats)
		return p.Chats, nil
	case *MessagesChatsSlice:
		c.Cache.UpdatePeersToCache([]User{}, p.Chats)
		return p.Chats, nil
	default:
		return nil, errors.New("could not convert chats: " + reflect.TypeOf(resp).String())
	}
}

// SetEmojiStatus sets the emoji status of the user
func (c *Client) SetEmojiStatus(emoji ...EmojiStatus) (bool, error) {
	var status EmojiStatus
	if len(emoji) == 0 {
		status = &EmojiStatusEmpty{}
	} else {
		status = emoji[0]
	}
	_, err := c.AccountUpdateEmojiStatus(status)
	return err == nil, err
}

// SetProfileAudio Adds or removes music from profile of the user
func (c *Client) SetProfileAudio(audio any, unset ...bool) (bool, error) {
	fi, err := c.ResolveMedia(audio, &MediaMetadata{Inline: true})
	if err != nil {
		return false, err
	}

	switch fi := fi.(type) {
	case *InputMediaDocument:
		_, err = c.AccountSaveMusic(len(unset) > 0 && unset[0], fi.ID, nil)
		return err == nil, err
	default:
		return false, errors.New("could not convert audio: " + reflect.TypeOf(fi).String())
	}
}

type SetAwayMessageOptions struct {
	ShortcutID  int32
	OfflineOnly bool
	Schedule    BusinessAwayMessageSchedule
	Recipients  *InputBusinessRecipients
}

func (c *Client) SetAwayMessage(opts SetAwayMessageOptions) error {
	if opts.ShortcutID == 0 {
		return errors.New("ShortcutID is required")
	}
	schedule := opts.Schedule
	if schedule == nil {
		schedule = &BusinessAwayMessageScheduleAlways{}
	}
	_, err := c.AccountUpdateBusinessAwayMessage(&InputBusinessAwayMessage{
		OfflineOnly: opts.OfflineOnly,
		ShortcutID:  opts.ShortcutID,
		Schedule:    schedule,
		Recipients:  opts.Recipients,
	})
	return err
}

func (c *Client) ClearAwayMessage() error {
	_, err := c.AccountUpdateBusinessAwayMessage(nil)
	return err
}

type SetGreetingOptions struct {
	ShortcutID     int32
	NoActivityDays int32
	Recipients     *InputBusinessRecipients
}

func (c *Client) SetGreetingMessage(opts SetGreetingOptions) error {
	if opts.ShortcutID == 0 {
		return errors.New("ShortcutID is required")
	}
	if opts.NoActivityDays <= 0 {
		opts.NoActivityDays = 7
	}
	_, err := c.AccountUpdateBusinessGreetingMessage(&InputBusinessGreetingMessage{
		ShortcutID:     opts.ShortcutID,
		NoActivityDays: opts.NoActivityDays,
		Recipients:     opts.Recipients,
	})
	return err
}

func (c *Client) ClearGreetingMessage() error {
	_, err := c.AccountUpdateBusinessGreetingMessage(nil)
	return err
}

type BusinessIntroSpec struct {
	Title       string
	Description string
	Sticker     InputDocument
}

func (c *Client) SetBusinessIntro(intro *BusinessIntroSpec) error {
	if intro == nil {
		_, err := c.AccountUpdateBusinessIntro(nil)
		return err
	}
	_, err := c.AccountUpdateBusinessIntro(&InputBusinessIntro{
		Title:       intro.Title,
		Description: intro.Description,
		Sticker:     intro.Sticker,
	})
	return err
}

func (c *Client) SetBusinessLocation(address string, geo InputGeoPoint) error {
	if address == "" {
		return errors.New("address is required")
	}
	_, err := c.AccountUpdateBusinessLocation(geo, address)
	return err
}

func (c *Client) ClearBusinessLocation() error {
	_, err := c.AccountUpdateBusinessLocation(nil, "")
	return err
}

type BusinessHourWindow struct {
	Day        time.Weekday
	OpenHour   int
	OpenMinute int
	CloseHour  int
	CloseMin   int
}

func (c *Client) SetBusinessHours(timezone string, windows []BusinessHourWindow) error {
	if timezone == "" {
		return errors.New("timezone is required")
	}
	hours := &BusinessWorkHours{
		TimezoneID: timezone,
	}
	for _, w := range windows {
		startMinute := (int32(w.Day) * 24 * 60) + int32(w.OpenHour*60+w.OpenMinute)
		endMinute := (int32(w.Day) * 24 * 60) + int32(w.CloseHour*60+w.CloseMin)
		if endMinute <= startMinute {
			return fmt.Errorf("window for %s has end <= start", w.Day)
		}
		hours.WeeklyOpen = append(hours.WeeklyOpen, &BusinessWeeklyOpen{
			StartMinute: startMinute,
			EndMinute:   endMinute,
		})
	}
	_, err := c.AccountUpdateBusinessWorkHours(hours)
	return err
}

func (c *Client) ClearBusinessHours() error {
	_, err := c.AccountUpdateBusinessWorkHours(nil)
	return err
}

type BusinessChatLinkSpec struct {
	Message  string
	Title    string
	Entities []MessageEntity
}

func (c *Client) CreateBusinessChatLink(spec BusinessChatLinkSpec) (*BusinessChatLink, error) {
	if spec.Message == "" {
		return nil, errors.New("message is required")
	}
	return c.AccountCreateBusinessChatLink(&InputBusinessChatLink{
		Message:  spec.Message,
		Title:    spec.Title,
		Entities: spec.Entities,
	})
}

func (c *Client) EditBusinessChatLink(slug string, spec BusinessChatLinkSpec) (*BusinessChatLink, error) {
	if slug == "" {
		return nil, errors.New("slug is required")
	}
	return c.AccountEditBusinessChatLink(slug, &InputBusinessChatLink{
		Message:  spec.Message,
		Title:    spec.Title,
		Entities: spec.Entities,
	})
}

func (c *Client) DeleteBusinessChatLink(slug string) error {
	if slug == "" {
		return errors.New("slug is required")
	}
	_, err := c.AccountDeleteBusinessChatLink(slug)
	return err
}

func (c *Client) ListBusinessChatLinks() ([]*BusinessChatLink, error) {
	resp, err := c.AccountGetBusinessChatLinks()
	if err != nil {
		return nil, err
	}
	return resp.Links, nil
}

type ConnectedBotRights struct {
	ReplyToMessages         bool
	DeleteSentMessages      bool
	DeleteReceivedMessages  bool
	EditName                bool
	EditBio                 bool
	EditProfilePhoto        bool
	EditUsername            bool
	ViewGifts               bool
	SellGifts               bool
	ChangeGiftSettings      bool
	TransferAndUpgradeGifts bool
	TransferStars           bool
	ManageStories           bool
}

type ConnectBotOptions struct {
	Bot        any
	Rights     ConnectedBotRights
	Recipients *InputBusinessRecipients
}

func (c *Client) ConnectBusinessBot(opts ConnectBotOptions) error {
	if opts.Bot == nil {
		return errors.New("bot is required")
	}
	botPeer, err := c.ResolvePeer(opts.Bot)
	if err != nil {
		return fmt.Errorf("resolve bot: %w", err)
	}
	if opts.Recipients == nil {
		return errors.New("recipients is required")
	}

	botRec := &InputBusinessBotRecipients{
		ExistingChats:   opts.Recipients.ExistingChats,
		NewChats:        opts.Recipients.NewChats,
		Contacts:        opts.Recipients.Contacts,
		NonContacts:     opts.Recipients.NonContacts,
		ExcludeSelected: opts.Recipients.ExcludeSelected,
		Users:           opts.Recipients.Users,
	}
	r := opts.Rights
	rights := &BusinessBotRights{
		Reply:                   r.ReplyToMessages,
		DeleteSentMessages:      r.DeleteSentMessages,
		DeleteReceivedMessages:  r.DeleteReceivedMessages,
		EditName:                r.EditName,
		EditBio:                 r.EditBio,
		EditProfilePhoto:        r.EditProfilePhoto,
		EditUsername:            r.EditUsername,
		ViewGifts:               r.ViewGifts,
		SellGifts:               r.SellGifts,
		ChangeGiftSettings:      r.ChangeGiftSettings,
		TransferAndUpgradeGifts: r.TransferAndUpgradeGifts,
		TransferStars:           r.TransferStars,
		ManageStories:           r.ManageStories,
	}
	_, err = c.AccountUpdateConnectedBot(false, rights, toInputUser(botPeer), botRec)
	return err
}

func (c *Client) DisconnectBusinessBot(bot any) error {
	botPeer, err := c.ResolvePeer(bot)
	if err != nil {
		return err
	}
	_, err = c.AccountUpdateConnectedBot(true, &BusinessBotRights{}, toInputUser(botPeer), &InputBusinessBotRecipients{})
	return err
}
