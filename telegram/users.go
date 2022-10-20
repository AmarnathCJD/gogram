package telegram

import (
	"reflect"

	"github.com/pkg/errors"
)

func (c *Client) GetMe() (*UserObj, error) {
	resp, err := c.UsersGetFullUser(&InputUserSelf{})
	if err != nil {
		return nil, errors.Wrap(err, "getting user")
	}
	user, ok := resp.Users[0].(*UserObj)
	if !ok {
		return nil, errors.New("got wrong response: " + reflect.TypeOf(resp).String())
	}
	return user, nil
}

type PhotosOptions struct {
	MaxID  int64 `json:"max_id,omitempty"`
	Offset int32 `json:"offset,omitempty"`
	Limit  int32 `json:"limit,omitempty"`
}

// GetProfilePhotos returns the profile photos of a user
//  Params:
//   - userID: The user ID
//   - Offset: The offset to start from
//   - Limit: The number of photos to return
//   - MaxID: The maximum ID of the photo to return
func (c *Client) GetProfilePhotos(userID interface{}, Opts ...*PhotosOptions) ([]Photo, error) {
	Options := getVariadic(Opts, &PhotosOptions{}).(*PhotosOptions)
	if Options.Limit > 80 {
		Options.Limit = 80
	} else if Options.Limit < 1 {
		Options.Limit = 1
	}
	peer, err := c.GetSendablePeer(userID)
	if err != nil {
		return nil, err
	}
	User, ok := peer.(*InputPeerUser)
	if !ok {
		return nil, errors.New("peer is not a user")
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
		return p.Photos, nil
	case *PhotosPhotosSlice:
		c.Cache.UpdatePeersToCache(p.Users, []Chat{})
		return p.Photos, nil
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
}

type CustomDialog struct{} // TODO

func (c *Client) GetDialogs(Opts ...*DialogOptions) ([]Dialog, error) {
	Options := getVariadic(Opts, &DialogOptions{}).(*DialogOptions)
	if Options.Limit > 1000 {
		Options.Limit = 1000
	} else if Options.Limit < 1 {
		Options.Limit = 1
	}
	resp, err := c.MessagesGetDialogs(&MessagesGetDialogsParams{
		OffsetDate:    Options.OffsetDate,
		OffsetID:      Options.OffsetID,
		OffsetPeer:    Options.OffsetPeer,
		Limit:         Options.Limit,
		FolderID:      Options.FolderID,
		ExcludePinned: Options.ExcludePinned,
	})
	if err != nil {
		return nil, err
	}
	switch p := resp.(type) {
	case *MessagesDialogsObj:
		go func() { c.Cache.UpdatePeersToCache(p.Users, p.Chats) }()
		return p.Dialogs, nil
	case *MessagesDialogsSlice:
		go func() { c.Cache.UpdatePeersToCache(p.Users, p.Chats) }()
		return p.Dialogs, nil
	default:
		return nil, errors.New("could not convert dialogs: " + reflect.TypeOf(resp).String())
	}
}

func (c *Client) GetCommonChats(userID interface{}) ([]Chat, error) {
	peer, err := c.GetSendablePeer(userID)
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
		go func() { c.Cache.UpdatePeersToCache([]User{}, p.Chats) }()
		return p.Chats, nil
	case *MessagesChatsSlice:
		go func() { c.Cache.UpdatePeersToCache([]User{}, p.Chats) }()
		return p.Chats, nil
	default:
		return nil, errors.New("could not convert chats: " + reflect.TypeOf(resp).String())
	}
}

func (c *Client) SetEmojiStatus(emoji ...interface{}) (bool, error) {
	var status EmojiStatus
	if len(emoji) > 1 {
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
