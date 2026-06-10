// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"errors"
	"fmt"
	"time"
)

type StoryPrivacy int

const (
	StoryPublic StoryPrivacy = iota
	StoryContacts
	StoryCloseFriends
	StorySelectedContacts
	StoryCustom
)

func (p StoryPrivacy) toRules(allowedUsers []InputUser) []InputPrivacyRule {
	switch p {
	case StoryPublic:
		return []InputPrivacyRule{&InputPrivacyValueAllowAll{}}
	case StoryContacts:
		return []InputPrivacyRule{&InputPrivacyValueAllowContacts{}}
	case StoryCloseFriends:
		return []InputPrivacyRule{&InputPrivacyValueAllowCloseFriends{}}
	case StorySelectedContacts:
		if len(allowedUsers) == 0 {
			return []InputPrivacyRule{&InputPrivacyValueAllowContacts{}}
		}
		return []InputPrivacyRule{&InputPrivacyValueAllowUsers{Users: allowedUsers}}
	}
	return nil
}

type StoryOptions struct {
	Caption       string
	ParseMode     string
	Period        time.Duration
	Privacy       StoryPrivacy
	AllowedUsers  []InputUser
	PrivacyRules  []InputPrivacyRule
	Pinned        bool
	Noforwards    bool
	MediaAreas    []MediaArea
	Music         InputDocument
	Albums        []int32
	FwdFromPeer   any
	FwdFromStory  int32
	Upload        *UploadOptions
}

func (c *Client) SendStory(peerID, media any, opts ...*StoryOptions) (int32, error) {
	opt := getVariadic(opts, &StoryOptions{})

	peer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return 0, fmt.Errorf("resolve peer: %w", err)
	}

	storyMedia, err := c.getSendableMedia(media, &MediaMetadata{Upload: opt.Upload, Inline: true})
	if err != nil {
		return 0, fmt.Errorf("prepare media: %w", err)
	}

	caption, entities := opt.Caption, []MessageEntity(nil)
	if opt.Caption != "" {
		entities, caption = c.FormatMessage(opt.Caption, getValue(opt.ParseMode, c.ParseMode()))
	}

	rules := opt.PrivacyRules
	if rules == nil {
		rules = opt.Privacy.toRules(opt.AllowedUsers)
	}
	if rules == nil {
		rules = []InputPrivacyRule{&InputPrivacyValueAllowAll{}}
	}

	params := &StoriesSendStoryParams{
		Pinned:       opt.Pinned,
		Noforwards:   opt.Noforwards,
		Peer:         peer,
		Media:        storyMedia,
		MediaAreas:   opt.MediaAreas,
		Caption:      caption,
		Entities:     entities,
		PrivacyRules: rules,
		RandomID:     GenerateRandomLong(),
		Albums:       opt.Albums,
		Music:        opt.Music,
	}
	if opt.Period > 0 {
		params.Period = int32(opt.Period / time.Second)
	}
	if opt.FwdFromPeer != nil {
		fwd, err := c.GetSendablePeer(opt.FwdFromPeer)
		if err != nil {
			return 0, fmt.Errorf("resolve fwd peer: %w", err)
		}
		params.FwdFromID = fwd
		params.FwdFromStory = opt.FwdFromStory
	}

	upd, err := c.StoriesSendStory(params)
	if err != nil {
		return 0, err
	}
	return extractStoryID(upd, params.RandomID), nil
}

func extractStoryID(upd Updates, randomID int64) int32 {
	scan := func(updates []Update) int32 {
		for _, u := range updates {
			if uid, ok := u.(*UpdateStoryID); ok && uid.RandomID == randomID {
				return uid.ID
			}
		}
		for _, u := range updates {
			if us, ok := u.(*UpdateStory); ok {
				if obj, ok := us.Story.(*StoryItemObj); ok {
					return obj.ID
				}
			}
		}
		return 0
	}
	switch v := upd.(type) {
	case *UpdatesObj:
		return scan(v.Updates)
	case *UpdateShort:
		return scan([]Update{v.Update})
	}
	return 0
}

type EditStoryOptions struct {
	Caption      string
	ParseMode    string
	Media        any
	MediaAreas   []MediaArea
	PrivacyRules []InputPrivacyRule
	Privacy      StoryPrivacy
	AllowedUsers []InputUser
	Music        InputDocument
	Upload       *UploadOptions
}

func (c *Client) EditStory(peerID any, storyID int32, opts ...*EditStoryOptions) error {
	opt := getVariadic(opts, &EditStoryOptions{})
	peer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return fmt.Errorf("resolve peer: %w", err)
	}

	params := &StoriesEditStoryParams{Peer: peer, ID: storyID}

	if opt.Caption != "" {
		entities, caption := c.FormatMessage(opt.Caption, getValue(opt.ParseMode, c.ParseMode()))
		params.Caption = caption
		params.Entities = entities
	}
	if opt.Media != nil {
		m, err := c.getSendableMedia(opt.Media, &MediaMetadata{Upload: opt.Upload, Inline: true})
		if err != nil {
			return fmt.Errorf("prepare media: %w", err)
		}
		params.Media = m
	}
	if len(opt.MediaAreas) > 0 {
		params.MediaAreas = opt.MediaAreas
	}
	if opt.Music != nil {
		params.Music = opt.Music
	}
	if opt.PrivacyRules != nil {
		params.PrivacyRules = opt.PrivacyRules
	} else if rules := opt.Privacy.toRules(opt.AllowedUsers); rules != nil {
		params.PrivacyRules = rules
	}

	_, err = c.StoriesEditStory(params)
	return err
}

func (c *Client) DeleteStory(peerID any, ids ...int32) ([]int32, error) {
	if len(ids) == 0 {
		return nil, errors.New("DeleteStory: at least one story id required")
	}
	peer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	return c.StoriesDeleteStories(peer, ids)
}

func (c *Client) GetStories(peerID any, ids ...int32) ([]StoryItem, error) {
	if len(ids) == 0 {
		return nil, errors.New("GetStories: at least one story id required")
	}
	peer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	resp, err := c.StoriesGetStoriesByID(peer, ids)
	if err != nil {
		return nil, err
	}
	return resp.Stories, nil
}

func (c *Client) GetPeerStories(peerID any) ([]StoryItem, error) {
	peer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	resp, err := c.StoriesGetPeerStories(peer)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Stories == nil {
		return nil, nil
	}
	return resp.Stories.Stories, nil
}

func (c *Client) GetPinnedStories(peerID any, offsetID, limit int32) ([]StoryItem, error) {
	peer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	resp, err := c.StoriesGetPinnedStories(peer, offsetID, limit)
	if err != nil {
		return nil, err
	}
	return resp.Stories, nil
}

func (c *Client) GetStoriesArchive(peerID any, offsetID, limit int32) ([]StoryItem, error) {
	peer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	resp, err := c.StoriesGetStoriesArchive(peer, offsetID, limit)
	if err != nil {
		return nil, err
	}
	return resp.Stories, nil
}

func (c *Client) MarkStoriesRead(peerID any, maxID int32) ([]int32, error) {
	peer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	return c.StoriesReadStories(peer, maxID)
}

func (c *Client) ReactToStory(peerID any, storyID int32, reaction any) error {
	peer, err := c.GetSendablePeer(peerID)
	if err != nil {
		return err
	}
	r, err := toReaction(reaction)
	if err != nil {
		return err
	}
	_, err = c.StoriesSendReaction(true, peer, storyID, r)
	return err
}

func toReaction(r any) (Reaction, error) {
	switch v := r.(type) {
	case Reaction:
		return v, nil
	case string:
		if v == "" {
			return &ReactionEmpty{}, nil
		}
		return &ReactionEmoji{Emoticon: v}, nil
	case int64:
		return &ReactionCustomEmoji{DocumentID: v}, nil
	case nil:
		return &ReactionEmpty{}, nil
	}
	return nil, fmt.Errorf("unsupported type %T", r)
}
