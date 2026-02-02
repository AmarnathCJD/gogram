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
	resp, err := c.UsersGetFullUser(&InputUserSelf{})
	if err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}
	user, ok := resp.Users[0].(*UserObj)
	if !ok {
		return nil, errors.New("got wrong response: " + reflect.TypeOf(resp).String())
	}
	c.clientData.me = user
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
	if p, ok := p.Photo.(*PhotoObj); ok {
		if p.VideoSizes != nil {
			return int64(p.VideoSizes[len(p.VideoSizes)-1].(*VideoSizeObj).Size)
		}
		size, _ := getPhotoSize(p.Sizes[len(p.Sizes)-1])
		return size
	}
	return 0
}

func (p *UserPhoto) DcID() int32 {
	if p, ok := p.Photo.(*PhotoObj); ok {
		return p.DcID
	}
	return 4
}

func (p *UserPhoto) InputLocation() (*InputPhotoFileLocation, error) {
	if photo, ok := p.Photo.(*PhotoObj); ok {
		if photo.VideoSizes != nil {
			return &InputPhotoFileLocation{
				ID:            photo.ID,
				AccessHash:    photo.AccessHash,
				FileReference: photo.FileReference,
				ThumbSize:     photo.VideoSizes[0].(*VideoSizeObj).Type,
			}, nil
		}
		_, thumbSize := getPhotoSize(photo.Sizes[len(photo.Sizes)-1])
		return &InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     thumbSize,
		}, nil
	}
	return nil, errors.New("could not convert photo: " + reflect.TypeOf(p.Photo).String())
}

// GetProfilePhotos returns the profile photos of a user
//
//	Params:
//	 - userID: The user ID
//	 - Offset: The offset to start from
//	 - Limit: The number of photos to return
//	 - MaxID: The maximum ID of the photo to return
func (c *Client) GetProfilePhotos(userID any, Opts ...*PhotosOptions) ([]UserPhoto, error) {
	Options := getVariadic(Opts, &PhotosOptions{})
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

func (c *Client) GetDialogs(Opts ...*DialogOptions) ([]TLDialog, error) {
	options := getVariadic(Opts, &DialogOptions{
		Limit:            1,
		OffsetPeer:       &InputPeerEmpty{},
		SleepThresholdMs: 20,
	})
	if options.OffsetPeer == nil {
		options.OffsetPeer = &InputPeerEmpty{}
	}

	if options.SleepThresholdMs == 0 {
		options.SleepThresholdMs = 20
	}

	var req = &MessagesGetDialogsParams{
		OffsetDate:    options.OffsetDate,
		OffsetID:      options.OffsetID,
		OffsetPeer:    options.OffsetPeer,
		ExcludePinned: options.ExcludePinned,
		FolderID:      options.FolderID,
		Hash:          options.Hash,
	}

	var dialogs []TLDialog
	var fetched int

	for {
		remaining := int32(100)
		if options.Limit > 0 {
			remaining = options.Limit - int32(fetched)
			if remaining <= 0 {
				break
			}
		}

		req.Limit = min(remaining, 100)

		resp, err := c.MessagesGetDialogs(req)
		if handleIfFlood(err, c) {
			continue
		} else if err != nil {
			return nil, err
		}

		switch p := resp.(type) {
		case *MessagesDialogsObj:
			if len(p.Dialogs) == 0 {
				return dialogs, nil
			}

			c.Cache.UpdatePeersToCache(p.Users, p.Chats)
			var newDialogs []TLDialog
			for _, dialog := range p.Dialogs {
				newDialogs = append(newDialogs, packDialog(dialog))
			}

			dialogs = append(dialogs, newDialogs...)
			fetched += len(p.Dialogs)

			if len(p.Messages) > 0 {
				if m, ok := p.Messages[len(p.Messages)-1].(*MessageObj); ok {
					req.OffsetID = m.ID
					req.OffsetDate = m.Date
				}
			}
			if lastPeer, err := c.GetSendablePeer(p.Dialogs[len(p.Dialogs)-1].(*DialogObj).Peer); err == nil {
				req.OffsetPeer = lastPeer
			}

			if len(p.Dialogs) < int(req.Limit) {
				return dialogs, nil
			}

		case *MessagesDialogsSlice:
			if len(p.Dialogs) == 0 {
				return dialogs, nil
			}

			c.Cache.UpdatePeersToCache(p.Users, p.Chats)
			var newDialogs []TLDialog
			for _, dialog := range p.Dialogs {
				newDialogs = append(newDialogs, packDialog(dialog))
			}

			dialogs = append(dialogs, newDialogs...)
			fetched += len(newDialogs)

			if len(p.Messages) > 0 {
				if m, ok := p.Messages[len(p.Messages)-1].(*MessageObj); ok {
					req.OffsetID = m.ID
					req.OffsetDate = m.Date
				}
			}
			if lastPeer, err := c.GetSendablePeer(p.Dialogs[len(p.Dialogs)-1].(*DialogObj).Peer); err == nil {
				req.OffsetPeer = lastPeer
			}

			if len(p.Dialogs) < int(req.Limit) {
				return dialogs, nil
			}

		case *MessagesDialogsNotModified:
			return dialogs, nil

		default:
			return nil, errors.New("could not convert dialogs: " + reflect.TypeOf(resp).String())
		}

		if options.Limit > 0 && fetched >= int(options.Limit) {
			break
		}

		time.Sleep(time.Duration(options.SleepThresholdMs) * time.Millisecond)
	}

	return dialogs, nil
}

func (c *Client) IterDialogs(callback func(*TLDialog) error, Opts ...*DialogOptions) error {
	options := getVariadic(Opts, &DialogOptions{
		Limit:            1,
		OffsetPeer:       &InputPeerEmpty{},
		SleepThresholdMs: 20,
	})

	if options.OffsetPeer == nil {
		options.OffsetPeer = &InputPeerEmpty{}
	}
	if options.SleepThresholdMs == 0 {
		options.SleepThresholdMs = 20
	}

	var ctx context.Context
	if options.Context != nil {
		ctx = options.Context
	} else {
		ctx = context.Background()
	}

	var req = &MessagesGetDialogsParams{
		OffsetDate:    options.OffsetDate,
		OffsetID:      options.OffsetID,
		OffsetPeer:    options.OffsetPeer,
		ExcludePinned: options.ExcludePinned,
		FolderID:      options.FolderID,
		Hash:          options.Hash,
	}

	var fetched int

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		remaining := int32(100)
		if options.Limit > 0 {
			remaining = options.Limit - int32(fetched)
			if remaining <= 0 {
				return nil
			}
		}

		req.Limit = min(remaining, 100)

		resp, err := c.MakeRequestCtx(ctx, req)
		if handleIfFlood(err, c) {
			continue
		} else if err != nil {
			if options.ErrorCallback != nil {
				if options.ErrorCallback(err, &IterProgressInfo{
					Fetched:      int32(fetched),
					CurrentBatch: 0,
					Limit:        options.Limit,
					Offset:       req.OffsetID,
				}) {
					continue
				}
			}
			return err
		}

		switch p := resp.(type) {
		case *MessagesDialogsObj:
			if len(p.Dialogs) == 0 {
				return nil
			}

			c.Cache.UpdatePeersToCache(p.Users, p.Chats)
			for _, dialog := range p.Dialogs {
				d := packDialog(dialog)
				if err := callback(&d); err != nil {
					if err == ErrStopIteration {
						return nil
					}
					return err
				}
			}

			if len(p.Messages) > 0 {
				if m, ok := p.Messages[len(p.Messages)-1].(*MessageObj); ok {
					req.OffsetID = m.ID
					req.OffsetDate = m.Date
				}
			}
			if len(p.Dialogs) > 0 {
				if lastPeer, err := c.GetSendablePeer(p.Dialogs[len(p.Dialogs)-1].(*DialogObj).Peer); err == nil {
					req.OffsetPeer = lastPeer
				}
			}

			fetched += len(p.Dialogs)
			if len(p.Dialogs) < int(req.Limit) {
				return nil
			}

		case *MessagesDialogsSlice:
			if len(p.Dialogs) == 0 {
				return nil
			}

			c.Cache.UpdatePeersToCache(p.Users, p.Chats)
			for _, dialog := range p.Dialogs {
				d := packDialog(dialog)
				if err := callback(&d); err != nil {
					if err == ErrStopIteration {
						return nil
					}
					return err
				}
			}

			if len(p.Messages) > 0 {
				if m, ok := p.Messages[len(p.Messages)-1].(*MessageObj); ok {
					req.OffsetID = m.ID
					req.OffsetDate = m.Date
				}
			}
			if len(p.Dialogs) > 0 {
				if lastPeer, err := c.GetSendablePeer(p.Dialogs[len(p.Dialogs)-1].(*DialogObj).Peer); err == nil {
					req.OffsetPeer = lastPeer
				}
			}

			fetched += len(p.Dialogs)
			if len(p.Dialogs) < int(req.Limit) {
				return nil
			}

		case *MessagesDialogsNotModified:
			return nil

		default:
			return errors.New("could not convert dialogs: " + reflect.TypeOf(resp).String())
		}

		if options.Limit > 0 && fetched >= int(options.Limit) {
			return nil
		}

		time.Sleep(time.Duration(options.SleepThresholdMs) * time.Millisecond)
	}
}

func packDialog(dialog Dialog) TLDialog {
	switch d := dialog.(type) {
	case *DialogObj:
		var dl = TLDialog{
			Dialog:     d,
			Peer:       d.Peer,
			TopMessage: d.TopMessage,
		}
		switch d.Peer.(type) {
		case *PeerUser:
			dl.PeerType = 1
		case *PeerChat:
			dl.PeerType = 2
		case *PeerChannel:
			dl.PeerType = 3
		}
		return dl
	}
	return TLDialog{}
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
