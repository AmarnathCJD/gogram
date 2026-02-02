package telegram

import (
	"reflect"
	"time"

	"github.com/pkg/errors"
)

// GetMe returns the current user
func (c *Client) GetMe() (*UserObj, error) {
	resp, err := c.UsersGetFullUser(&InputUserSelf{})
	if err != nil {
		return nil, errors.Wrap(err, "getting user")
	}
	user, ok := resp.Users[0].(*UserObj)
	if !ok {
		return nil, errors.New("got wrong response: " + reflect.TypeOf(resp).String())
	}
	c.clientData.me = user

	return user, nil
}

type PhotosOptions struct {
	MaxID  int64 `json:"max_id,omitempty"`
	Offset int32 `json:"offset,omitempty"`
	Limit  int32 `json:"limit,omitempty"`
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
	peer, err := c.ResolvePeer(userID)
	if err != nil {
		return nil, err
	}
	User, ok := peer.(*InputPeerUser)
	if !ok {
		return nil, errors.New("given peer is not a user")
	}
	resp, err := c.PhotosGetUserPhotos(
		&InputUserObj{UserID: User.UserID, AccessHash: User.AccessHash},
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
	OffsetID      int32     `json:"offset_id,omitempty"`
	OffsetDate    int32     `json:"offset_date,omitempty"`
	OffsetPeer    InputPeer `json:"offset_peer,omitempty"`
	Limit         int32     `json:"limit,omitempty"`
	ExcludePinned bool      `json:"exclude_pinned,omitempty"`
	FolderID      int32     `json:"folder_id,omitempty"`
	Hash          int64     `json:"hash,omitempty"`
}

type CustomDialog struct {
	Dialog   Dialog
	Peer     Peer
	PeerType string
} // TODO

// GetDialogs returns the dialogs of the user
//
//	Params:
//	 - OffsetID: The offset ID of the dialog
//	 - OffsetDate: The offset date of the dialog
//	 - OffsetPeer: The offset peer of the dialog
//	 - Limit: The number of dialogs to return
//	 - ExcludePinned: Whether to exclude pinned dialogs
//	 - FolderID: The folder ID to get dialogs from
func (c *Client) GetDialogs(Opts ...*DialogOptions) ([]Dialog, error) {
	Options := getVariadic(Opts, &DialogOptions{})
	if Options.Limit < 0 {
		Options.Limit = 1
	}
	if Options.OffsetPeer == nil {
		Options.OffsetPeer = &InputPeerEmpty{}
	}

	var reqLimit int32 = 100
	if Options.Limit > 0 && Options.Limit < 100 {
		reqLimit = Options.Limit
	}

	var req = &MessagesGetDialogsParams{
		OffsetDate:    Options.OffsetDate,
		OffsetID:      Options.OffsetID,
		OffsetPeer:    Options.OffsetPeer,
		Limit:         reqLimit,
		ExcludePinned: Options.ExcludePinned,
		FolderID:      Options.FolderID,
		Hash:          Options.Hash,
	}

	var dialogs []Dialog

	for {
		if len(dialogs) >= int(Options.Limit) && Options.Limit > 0 {
			return dialogs, nil
		}

		resp, err := c.MessagesGetDialogs(req)
		if handleIfFlood(err, c) {
			continue
		} else if err != nil {
			return nil, err
		}

		switch p := resp.(type) {
		case *MessagesDialogsObj:
			c.Cache.UpdatePeersToCache(p.Users, p.Chats)
			dialogs = append(dialogs, p.Dialogs...)
			if len(p.Dialogs) < 100 {
				return dialogs, nil
			}
			if len(p.Messages) > 0 {
				if m, ok := p.Messages[len(p.Messages)-1].(*MessageObj); ok {
					req.OffsetID = m.ID
					req.OffsetDate = m.Date
				}
			}

			if lastPeer, err := c.GetSendablePeer(p.Dialogs[len(p.Dialogs)-1].(*DialogObj).Peer); err == nil {
				req.OffsetPeer = lastPeer
			}

		case *MessagesDialogsSlice:
			c.Cache.UpdatePeersToCache(p.Users, p.Chats)
			dialogs = append(dialogs, p.Dialogs...)
			if len(p.Dialogs) < 100 {
				return dialogs, nil
			}

			if len(p.Messages) > 0 {
				if m, ok := p.Messages[len(p.Messages)-1].(*MessageObj); ok {
					req.OffsetID = m.ID
					req.OffsetDate = m.Date
				}
			}

			if lastPeer, err := c.GetSendablePeer(p.Dialogs[len(p.Dialogs)-1].(*DialogObj).Peer); err == nil {
				req.OffsetPeer = lastPeer
			}
		case *MessagesDialogsNotModified:
			return dialogs, nil

		default:
			return nil, errors.New("could not convert dialogs: " + reflect.TypeOf(resp).String())
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// GetCommonChats returns the common chats of a user
//
//	Params:
//	 - userID: The user Identifier
func (c *Client) GetCommonChats(userID any) ([]Chat, error) {
	peer, err := c.ResolvePeer(userID)
	if err != nil {
		return nil, err
	}
	user, ok := peer.(*InputPeerUser)
	if !ok {
		return nil, errors.New("peer is not a user")
	}
	resp, err := c.MessagesGetCommonChats(&InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash}, 0, 100)
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
//
//	Params:
//	 - emoji: The emoji status to set
func (c *Client) SetEmojiStatus(emoji ...any) (bool, error) {
	var status EmojiStatus
	if len(emoji) == 0 {
		status = &EmojiStatusEmpty{}
	} else {
		em := emoji[0]
		_, ok := em.(EmojiStatus)
		if !ok {
			return false, errors.New("emoji is not an EmojiStatus")
		}
		status = em.(EmojiStatus)
	}
	_, err := c.AccountUpdateEmojiStatus(status)
	return err == nil, err
}
