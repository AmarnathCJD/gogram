// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"fmt"
	"math"
	"reflect"
	"time"

	"errors"
)

// GetChatPhotos returns the profile photos of a chat
//
//	Params:
//	 - chatID: The ID of the chat
//	 - limit: The maximum number of photos to be returned
func (c *Client) GetChatPhotos(chatID any, limit ...int32) ([]Photo, error) {
	l := getVariadic(limit, 1)
	messages, err := c.GetMessages(chatID, &SearchOption{
		Limit:  l,
		Filter: &InputMessagesFilterChatPhotos{},
	})
	if err != nil {
		return nil, err
	}
	var photos []Photo
	for _, message := range messages {
		if message.Action != nil {
			switch action := message.Action.(type) {
			case *MessageActionChatEditPhoto:
				photos = append(photos, action.Photo)
			case *MessageActionChatDeletePhoto:
			}
		}
	}
	return photos, nil
}

// GetChatPhoto returns the current chat photo
//
//	Params:
//	 - chatID: chat id
func (c *Client) GetChatPhoto(chatID any) (Photo, error) {
	photos, err := c.GetChatPhotos(chatID)
	if err != nil {
		return &PhotoObj{}, err
	}
	if len(photos) > 0 {
		return photos[0], nil
	}
	return &PhotoObj{}, nil // GetFullChannel TODO
}

// JoinChannel joins a channel or chat by its username or id
//
//	Params:
//	- Channel: the username or id of the channel or chat
func (c *Client) JoinChannel(channel any) (*Channel, error) {
	switch p := channel.(type) {
	case string:
		if TgJoinRe.MatchString(p) {
			match := TgJoinRe.FindStringSubmatch(p)[1]
			result, err := c.MessagesImportChatInvite(match)
			if err != nil {
				if !MatchError(err, "USER_ALREADY_PARTICIPANT") {
					return nil, err
				}
				resp, errJoin := c.MessagesCheckChatInvite(match)
				if errJoin != nil {
					return nil, errJoin
				}
				if already, ok := resp.(*ChatInviteAlready); ok {
					if chat, ok := already.Chat.(*Channel); ok {
						return chat, nil
					}
				}
				return nil, err
			}

			if updates, ok := result.(*UpdatesObj); ok {
				c.Cache.UpdatePeersToCache(updates.Users, updates.Chats)
				for _, chat := range updates.Chats {
					if ch, ok := chat.(*Channel); ok {
						return ch, nil
					}
				}
			}
			return nil, nil
		}
		if UsernameRe.MatchString(p) {
			return c.joinChannelByPeer(UsernameRe.FindStringSubmatch(p)[1])
		}
		return nil, errors.New("invalid channel or chat")
	case *InputPeerChannel, *InputPeerChat, int, int32, int64:
		return c.joinChannelByPeer(p)
	case *ChatInviteExported:
		_, err := c.MessagesImportChatInvite(p.Link)
		return nil, err
	default:
		return c.joinChannelByPeer(channel)
	}
}

func (c *Client) joinChannelByPeer(channel any) (*Channel, error) {
	channel, err := c.ResolvePeer(channel)
	if err != nil {
		return nil, err
	}
	if chat, ok := channel.(*InputPeerChannel); ok {
		_, err = c.ChannelsJoinChannel(&InputChannelObj{ChannelID: chat.ChannelID, AccessHash: chat.AccessHash})
		if err != nil {
			return nil, err
		}
		return c.GetChannel(chat.ChannelID)
	} else if chat, ok := channel.(*InputPeerChat); ok {
		_, err = c.MessagesAddChatUser(chat.ChatID, &InputUserEmpty{}, 0)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("peer is not a channel or chat, but %T", channel)
	}
	return nil, nil
}

// LeaveChannel leaves a channel or chat
//
//	Params:
//	 - Channel: Channel or chat to leave
//	 - Revoke: If true, the channel will be deleted
func (c *Client) LeaveChannel(Channel any, Revoke ...bool) error {
	revokeChat := getVariadic(Revoke, false)
	channel, err := c.ResolvePeer(Channel)
	if err != nil {
		return err
	}
	if chat, ok := channel.(*InputPeerChannel); ok {
		_, err = c.ChannelsLeaveChannel(&InputChannelObj{ChannelID: chat.ChannelID, AccessHash: chat.AccessHash})
		if err != nil {
			return err
		}
	} else if chat, ok := channel.(*InputPeerChat); ok {
		_, err = c.MessagesDeleteChatUser(revokeChat, chat.ChatID, &InputUserSelf{})
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("peer is not a channel or chat, but %T", channel)
	}
	return nil
}

const (
	Admin      = "admin"
	Creator    = "creator"
	Member     = "member"
	Restricted = "restricted"
	Left       = "left"
	Kicked     = "kicked"
)

type Participant struct {
	User        *UserObj           `json:"user,omitempty"`
	Participant ChannelParticipant `json:"participant,omitempty"`
	Status      string             `json:"status,omitempty"`
	Rights      *ChatAdminRights   `json:"rights,omitempty"`
	Rank        string             `json:"rank,omitempty"`
}

func (c *Client) processParticipant(p ChannelParticipant) (*Participant, error) {
	var (
		status string           = Member
		rights *ChatAdminRights = &ChatAdminRights{}
		rank   string           = ""
		userID int64            = 0
	)
	switch pt := p.(type) {
	case *ChannelParticipantCreator:
		status, rights, rank, userID = Creator, pt.AdminRights, pt.Rank, pt.UserID
	case *ChannelParticipantAdmin:
		status, rights, rank, userID = Admin, pt.AdminRights, pt.Rank, pt.UserID
	case *ChannelParticipantObj:
		userID = pt.UserID
	case *ChannelParticipantSelf:
		userID = pt.UserID
	case *ChannelParticipantBanned:
		status = Restricted
		userID = c.GetPeerID(pt.Peer)
	case *ChannelParticipantLeft:
		status = Left
		userID = c.GetPeerID(pt.Peer)
	}
	user, err := c.GetUser(userID)
	if err != nil {
		return nil, err
	}
	return &Participant{
		User:        user,
		Participant: p,
		Status:      status,
		Rights:      rights,
		Rank:        rank,
	}, nil
}

// GetChatMember returns the members of a chat
//
//	Params:
//	 - chatID: The ID of the chat
//	 - userID: The ID of the user
func (c *Client) GetChatMember(chatID, userID any) (*Participant, error) {
	channel, err := c.ResolvePeer(chatID)
	if err != nil {
		return nil, err
	}
	user, err := c.ResolvePeer(userID)
	if err != nil {
		return nil, err
	}
	chat, ok := channel.(*InputPeerChannel)
	if !ok {
		return nil, fmt.Errorf("peer is not a channel, but %T", channel)
	}
	participant, err := c.ChannelsGetParticipant(&InputChannelObj{ChannelID: chat.ChannelID, AccessHash: chat.AccessHash}, user)
	if err != nil {
		return nil, err
	}
	c.Cache.UpdatePeersToCache(participant.Users, participant.Chats)
	return c.processParticipant(participant.Participant)
}

type ParticipantOptions struct {
	Query            string                    `json:"query,omitempty"`
	Filter           ChannelParticipantsFilter `json:"filter,omitempty"`
	Offset           int32                     `json:"offset,omitempty"`
	Limit            int32                     `json:"limit,omitempty"`
	SleepThresholdMs int32                     `json:"sleep_threshold_ms,omitempty"`
	ErrorCallback    IterErrorCallback         `json:"-"` // callback for handling errors with progress info
}

// GetChatMembers returns the members of a chat
//
//	Params:
//	 - chatID: The ID of the chat
//	 - filter: The filter to use
//	 - offset: The offset to use
//	 - limit: The limit to use
func (c *Client) GetChatMembers(chatID any, Opts ...*ParticipantOptions) ([]*Participant, int32, error) {
	channel, err := c.ResolvePeer(chatID)
	if err != nil {
		return nil, 0, err
	}
	chat, ok := channel.(*InputPeerChannel)
	if !ok {
		return nil, 0, fmt.Errorf("peer is not a channel, but %T", channel)
	}
	opts := getVariadic(Opts, &ParticipantOptions{Filter: &ChannelParticipantsSearch{}, Limit: 1})
	if opts.Query != "" {
		opts.Filter = &ChannelParticipantsSearch{Q: opts.Query}
	} else if opts.Filter == nil {
		opts.Filter = &ChannelParticipantsSearch{}
	}

	var fetched int32 = 0
	var participantsList []*Participant
	var reqLimit, reqOffset int32 = 200, opts.Offset
	var totalCount int32

	for {
		remaining := opts.Limit - int32(fetched)
		reqLimit = min(remaining, 200)

		participants, err := c.ChannelsGetParticipants(&InputChannelObj{ChannelID: chat.ChannelID, AccessHash: chat.AccessHash}, opts.Filter, reqOffset, reqLimit, 0)
		if err != nil {
			return nil, 0, err
		}
		cParts, ok := participants.(*ChannelsChannelParticipantsObj)
		if opts.Limit == -1 {
			opts.Limit = cParts.Count
			continue
		}

		if !ok {
			return nil, 0, errors.New("could not get participants")
		}
		c.Cache.UpdatePeersToCache(cParts.Users, cParts.Chats)
		for _, p := range cParts.Participants {
			part, err := c.processParticipant(p)
			if err != nil {
				return nil, 0, err
			}
			participantsList = append(participantsList, part)
			fetched++
		}

		if fetched >= opts.Limit || len(cParts.Participants) == 0 {
			break
		}

		reqOffset = fetched
		totalCount = cParts.Count

		time.Sleep(time.Duration(opts.SleepThresholdMs) * time.Millisecond)
	}
	return participantsList, totalCount, nil
}

func (c *Client) IterChatMembers(chatID any, Opts ...*ParticipantOptions) (<-chan *Participant, <-chan error) {
	ch := make(chan *Participant)
	errCh := make(chan error)

	var peerToAct, err = c.ResolvePeer(chatID)
	if err != nil {
		errCh <- err
		close(ch)
		return ch, errCh
	}

	var chat, ok = peerToAct.(*InputPeerChannel)
	if !ok {
		errCh <- fmt.Errorf("peer is not a channel, but %T", peerToAct)
		close(ch)
		return ch, errCh
	}

	go func() {
		defer close(ch)
		defer close(errCh)

		var opts = getVariadic(Opts, &ParticipantOptions{
			Limit:            1,
			SleepThresholdMs: 20,
			Filter:           &ChannelParticipantsSearch{},
		})

		if opts.Query != "" {
			opts.Filter = &ChannelParticipantsSearch{Q: opts.Query}
		} else if opts.Filter == nil {
			opts.Filter = &ChannelParticipantsSearch{}
		}

		var fetched int32 = 0
		req := &ChannelsGetParticipantsParams{
			Channel: &InputChannelObj{ChannelID: chat.ChannelID, AccessHash: chat.AccessHash},
			Filter:  opts.Filter,
			Offset:  opts.Offset,
			Limit:   200,
			Hash:    0,
		}

		for {
			if opts.Limit == -1 {
				req.Limit = 0
				resp, err := c.MakeRequest(req)
				if err != nil {
					errCh <- err
					return
				}

				switch resp := resp.(type) {
				case *ChannelsChannelParticipantsObj:
					if resp.Count == 0 {
						return
					}
					opts.Limit = resp.Count
				case *ChannelsChannelParticipantsNotModified:
				default:
				}

				continue
			}

			remaining := opts.Limit - int32(fetched)
			perReqLimit := int32(200)
			if remaining < perReqLimit {
				perReqLimit = remaining
			}
			req.Limit = perReqLimit

			resp, err := c.MakeRequest(req)
			if err != nil {
				if handleIfFlood(err, c) {
					continue
				}
				if opts.ErrorCallback != nil {
					if opts.ErrorCallback(err, &IterProgressInfo{
						Fetched:      fetched,
						CurrentBatch: 0,
						Limit:        opts.Limit,
						Offset:       req.Offset,
					}) {
						continue
					}
				}
				errCh <- err
				return
			}

			switch resp := resp.(type) {
			case *ChannelsChannelParticipantsObj:
				c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
				for _, p := range resp.Participants {
					part, err := c.processParticipant(p)
					if err != nil {
						errCh <- err
						return
					}
					ch <- part
					fetched++
				}
				if len(resp.Participants) < int(perReqLimit) || fetched >= opts.Limit && opts.Limit > 0 {
					return
				}

				req.Offset = fetched
			default:
				errCh <- errors.New("unexpected response: " + reflect.TypeOf(resp).String())
				return
			}

			time.Sleep(time.Duration(opts.SleepThresholdMs) * time.Millisecond)
		}
	}()

	return ch, errCh
}

type AdminOptions struct {
	IsAdmin bool             `json:"is_admin,omitempty"`
	Rights  *ChatAdminRights `json:"rights,omitempty"`
	Rank    string           `json:"rank,omitempty"`
}

// EditAdmin edits the admin rights of a user in a chat.
//
// This function modifies the admin permissions of a user within a chat.
//
// Args:
//   - PeerID: The ID of the chat.
//   - UserID: The ID of the user whose admin rights are to be modified.
//   - Opts: Optional arguments for setting the user's admin rights.
//
// Returns:
//   - bool: True if the operation was successful, False otherwise.
//   - error: An error if the operation fails.
func (c *Client) EditAdmin(PeerID, UserID any, Opts ...*AdminOptions) (bool, error) {
	opts := getVariadic(Opts, &AdminOptions{IsAdmin: true, Rights: &ChatAdminRights{}, Rank: "Admin"})
	peer, err := c.ResolvePeer(PeerID)
	if err != nil {
		return false, err
	}
	u, err := c.ResolvePeer(UserID)
	if err != nil {
		return false, err
	}
	user, ok := u.(*InputPeerUser)
	if !ok {
		return false, fmt.Errorf("peer is not a user, but %T", u)
	}
	switch p := peer.(type) {
	case *InputPeerChannel:
		if opts.IsAdmin {
			_, err := c.ChannelsEditAdmin(&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash}, &InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash}, opts.Rights, opts.Rank)
			if err != nil {
				return false, err
			}
		} else {
			_, err := c.ChannelsEditAdmin(&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash}, &InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash}, &ChatAdminRights{}, "")
			if err != nil {
				return false, err
			}
		}
	case *InputPeerChat:
		_, err := c.MessagesEditChatAdmin(p.ChatID, &InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash}, opts.IsAdmin)
		if err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("peer is not a chat or channel, but %T", peer)
	}
	return true, nil
}

type AdminBuilder struct {
	client         *Client
	chatID, userID any
	isAdmin        bool
	rights         *ChatAdminRights
	rank           string
}

func (c *Client) EditAdminBuilder(chatID, userID any) *AdminBuilder {
	return &AdminBuilder{client: c, chatID: chatID, userID: userID}
}

func (b *AdminBuilder) WithRights(rights *ChatAdminRights) *AdminBuilder {
	b.rights = rights
	return b
}

func (b *AdminBuilder) WithRank(rank string) *AdminBuilder {
	b.rank = rank
	return b
}

func (b *AdminBuilder) Promote() (bool, error) {
	b.isAdmin = true
	return b.client.EditAdmin(b.chatID, b.userID, &AdminOptions{IsAdmin: true, Rights: b.rights, Rank: b.rank})
}

func (b *AdminBuilder) Demote() (bool, error) {
	b.isAdmin = false
	return b.client.EditAdmin(b.chatID, b.userID, &AdminOptions{IsAdmin: false, Rights: &ChatAdminRights{}, Rank: ""})
}

func (b *AdminBuilder) Invoke() (bool, error) {
	return b.client.EditAdmin(b.chatID, b.userID, &AdminOptions{Rights: b.rights, Rank: b.rank})
}

type BannedOptions struct {
	Ban      bool              `json:"ban,omitempty"`
	Unban    bool              `json:"unban,omitempty"`
	Mute     bool              `json:"mute,omitempty"`
	Unmute   bool              `json:"unmute,omitempty"`
	Rights   *ChatBannedRights `json:"rights,omitempty"`
	Revoke   bool              `json:"revoke,omitempty"`
	TillDate int32             `json:"till_date,omitempty"`
}

// EditBanned modifies the ban status of a user in a chat or channel.
//
// This function can ban, unban, mute, or unmute a user in a chat or channel.
//
// Args:
//   - PeerID: The ID of the chat or channel.
//   - UserID: The ID of the user to be banned.
//   - opts: Optional arguments for banning the user.
//
// Returns:
//   - bool: True if the operation was successful, False otherwise.
//   - error: An error if the operation fails.
func (c *Client) EditBanned(PeerID, UserID any, opts ...*BannedOptions) (bool, error) {
	o := getVariadic(opts, &BannedOptions{Ban: true, Rights: &ChatBannedRights{}})
	if o.Rights == nil {
		o.Rights = &ChatBannedRights{}
	}

	peer, err := c.ResolvePeer(PeerID)
	if err != nil {
		return false, err
	}

	u, err := c.ResolvePeer(UserID)
	if err != nil {
		return false, err
	}

	switch p := peer.(type) {
	case *InputPeerChannel:
		return handleChannelBan(c, p, u, o)
	case *InputPeerChat:
		return handleChatBan(c, p, u, o)
	default:
		return false, fmt.Errorf("peer is not a chat or channel, but %T", peer)
	}
}

func handleChannelBan(c *Client, p *InputPeerChannel, u InputPeer, o *BannedOptions) (bool, error) {
	o.Rights.ViewMessages = o.Ban
	o.Rights.SendMessages = o.Mute
	if o.TillDate != 0 {
		o.Rights.UntilDate = o.TillDate
	}

	_, err := c.ChannelsEditBanned(&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash}, u, o.Rights)
	return err == nil, err
}

func handleChatBan(c *Client, p *InputPeerChat, u InputPeer, o *BannedOptions) (bool, error) {
	if u, ok := u.(*InputPeerUser); ok {
		if o.Ban || o.Unban || o.Mute || o.Unmute {
			_, err := c.MessagesDeleteChatUser(o.Revoke, p.ChatID, &InputUserObj{UserID: u.UserID, AccessHash: u.AccessHash})
			return err == nil, err
		} else {
			_, err := c.MessagesAddChatUser(p.ChatID, &InputUserObj{UserID: u.UserID, AccessHash: u.AccessHash}, 0)
			return err == nil, err
		}
	}

	return false, errors.New("user is not a valid InputPeerUser")
}

func (c *Client) KickParticipant(PeerID, UserID any) (bool, error) {
	peer, err := c.ResolvePeer(PeerID)
	if err != nil {
		return false, err
	}
	u, err := c.ResolvePeer(UserID)
	if err != nil {
		return false, err
	}
	switch p := peer.(type) {
	case *InputPeerChannel:
		_, err := c.EditBanned(p, u, &BannedOptions{Ban: true})
		if err != nil {
			return false, err
		}
		_, err = c.EditBanned(p, u, &BannedOptions{Unban: true})
		if err != nil {
			return false, err
		}
	case *InputPeerChat:
		user, ok := u.(*InputPeerUser)
		if !ok {
			return false, fmt.Errorf("peer is not a user, but %T", u)
		}
		_, err := c.MessagesDeleteChatUser(false, c.GetPeerID(p), &InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash})
		if err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("peer is not a chat or channel, but %T", peer)
	}
	return true, nil
}

type BannedBuilder struct {
	client            *Client
	chatID, userID    any
	ban, mute, revoke bool
	rights            *ChatBannedRights
	tillDate          int32
}

func (c *Client) EditBannedBuilder(chatID, userID any) *BannedBuilder {
	return &BannedBuilder{client: c, chatID: chatID, userID: userID}
}

func (b *BannedBuilder) WithRights(rights *ChatBannedRights) *BannedBuilder {
	b.rights = rights
	return b
}

func (b *BannedBuilder) Ban(until ...int) (bool, error) {
	b.ban = true
	return b.client.EditBanned(b.chatID, b.userID, &BannedOptions{
		Ban:    true,
		Rights: b.rights,
		Revoke: b.revoke,
		TillDate: func() int32 {
			if len(until) > 0 {
				return int32(until[0])
			}
			return b.tillDate
		}(),
	})
}

func (b *BannedBuilder) Unban() (bool, error) {
	b.ban = false
	return b.client.EditBanned(b.chatID, b.userID, &BannedOptions{
		Unban:  true,
		Rights: b.rights,
		Revoke: b.revoke,
	})
}
func (b *BannedBuilder) Mute() (bool, error) {
	b.mute = true
	return b.client.EditBanned(b.chatID, b.userID, &BannedOptions{
		Mute:     true,
		Rights:   b.rights,
		Revoke:   b.revoke,
		TillDate: b.tillDate,
	})
}

func (b *BannedBuilder) Unmute() (bool, error) {
	b.mute = false
	return b.client.EditBanned(b.chatID, b.userID, &BannedOptions{
		Ban:    b.ban,
		Mute:   b.mute,
		Revoke: b.revoke,
	})
}

func (b *BannedBuilder) Revoke() *BannedBuilder {
	b.revoke = true
	return b
}

func (b *BannedBuilder) TillDate(tillDate int32) *BannedBuilder {
	b.tillDate = tillDate
	return b
}

func (b *BannedBuilder) Invoke() (bool, error) {
	return b.client.EditBanned(b.chatID, b.userID, &BannedOptions{
		Revoke:   b.revoke,
		Rights:   b.rights,
		TillDate: b.tillDate,
	})
}

type TitleOptions struct {
	LastName string `json:"last_name,omitempty"`
	About    string `json:"about,omitempty"`
}

// Edit the title of a chat, channel or self,
// returns true if successful
func (c *Client) EditTitle(PeerID any, Title string, Opts ...*TitleOptions) (bool, error) {
	opts := getVariadic(Opts, &TitleOptions{})
	peer, err := c.ResolvePeer(PeerID)
	if err != nil {
		return false, err
	}
	switch p := peer.(type) {
	case *InputPeerChannel:
		_, err := c.ChannelsEditTitle(&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash}, Title)
		if err != nil {
			return false, err
		}
	case *InputPeerChat:
		_, err := c.MessagesEditChatTitle(p.ChatID, Title)
		if err != nil {
			return false, err
		}
	case *InputPeerSelf:
		_, err := c.AccountUpdateProfile(opts.LastName, Title, opts.About)
		if err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("peer is not a chat or channel or self, but %T", peer)
	}
	return true, nil
}

// GetStats returns the stats of the channel or message
//
//	Params:
//	 - channelID: the channel ID
//	 - messageID: the message ID
func (c *Client) GetStats(channel any, messageID ...any) (*StatsBroadcastStats, *StatsMessageStats, error) {
	peerID, err := c.GetSendableChannel(channel)
	if err != nil {
		return nil, nil, err
	}
	msgID := getVariadic[any](messageID, int32(0)).(int32)
	if msgID > 0 {
		resp, err := c.StatsGetMessageStats(true, peerID, msgID)
		if err != nil {
			return nil, nil, err
		}
		return nil, resp, nil
	}
	resp, err := c.StatsGetBroadcastStats(true, peerID)
	if err != nil {
		return nil, nil, err
	}
	return resp, nil, nil
}

type InviteLinkOptions struct {
	LegacyRevokePermanent bool   `json:"legacy_revoke_permanent,omitempty"`
	Expire                int32  `json:"expire,omitempty"`
	Limit                 int32  `json:"limit,omitempty"`
	Title                 string `json:"title,omitempty"`
	RequestNeeded         bool   `json:"request_needed,omitempty"`
}

// GetChatInviteLink returns the invite link of a chat
//
//	Params:
//	 - peerID : The ID of the chat
//	 - LegacyRevoke : If true, the link will be revoked
//	 - Expire: The time in seconds after which the link will expire
//	 - Limit: The maximum number of users that can join the chat using the link
//	 - Title: The title of the link
//	 - RequestNeeded: If true, join requests will be needed to join the chat
func (c *Client) GetChatInviteLink(peerID any, LinkOpts ...*InviteLinkOptions) (ExportedChatInvite, error) {
	LinkOptions := getVariadic(LinkOpts, &InviteLinkOptions{})
	peer, err := c.ResolvePeer(peerID)
	if err != nil {
		return nil, err
	}
	link, err := c.MessagesExportChatInvite(&MessagesExportChatInviteParams{
		Peer:                  peer,
		LegacyRevokePermanent: LinkOptions.LegacyRevokePermanent,
		RequestNeeded:         LinkOptions.RequestNeeded,
		UsageLimit:            LinkOptions.Limit,
		Title:                 LinkOptions.Title,
		ExpireDate:            LinkOptions.Expire,
	})
	return link, err
}

type ChannelOptions struct {
	About        string        `json:"about,omitempty"`
	NotBroadcast bool          `json:"broadcast,omitempty"`
	Megagroup    bool          `json:"megagroup,omitempty"`
	Address      string        `json:"address,omitempty"`
	ForImport    bool          `json:"for_import,omitempty"`
	GeoPoint     InputGeoPoint `json:"geo_point,omitempty"`
}

func (c *Client) CreateChannel(title string, opts ...*ChannelOptions) (*Channel, error) {
	opt := getVariadic(opts, &ChannelOptions{})
	resp, err := c.ChannelsCreateChannel(&ChannelsCreateChannelParams{
		Broadcast: !opt.NotBroadcast,
		GeoPoint:  opt.GeoPoint,
		About:     opt.About,
		ForImport: opt.ForImport,
		Address:   opt.Address,
		Megagroup: opt.Megagroup,
		Title:     title,
	})
	if err != nil {
		return nil, err
	}
	if updates, ok := resp.(*UpdatesObj); ok {
		c.Cache.UpdatePeersToCache(updates.Users, updates.Chats)
		for _, chat := range updates.Chats {
			if ch, ok := chat.(*Channel); ok {
				return ch, nil
			}
		}
	}
	return nil, errors.New("empty reply from server")
}

func (c *Client) DeleteChannel(channel any) (*Updates, error) {
	peer, err := c.GetSendableChannel(channel)
	if err != nil {
		return nil, err
	}

	u, err := c.ChannelsDeleteChannel(peer)

	if err != nil {
		return nil, err
	}

	return &u, nil
}

type JoinRequest struct {
	User        *UserObj
	Date        int32
	ApprovedBy  int64
	Requested   bool
	About       string
	ViaChatlist bool
}

func (c *Client) GetChatJoinRequests(channelID any, limit int, query ...string) ([]*JoinRequest, error) {
	var currentOffsetUser InputUser = &InputUserEmpty{}
	var currentOffsetDate int32 = 0
	var fetched int = 0

	if limit <= 0 {
		limit = math.MaxInt32
	}

	peer, err := c.ResolvePeer(channelID)
	if err != nil {
		return nil, err
	}

	var results []*JoinRequest
	for fetched < limit {
		batchLimit := min(limit-fetched, 100)
		resp, err := c.MessagesGetChatInviteImporters(&MessagesGetChatInviteImportersParams{
			Requested:  true,
			Peer:       peer,
			Q:          getVariadic(query, ""),
			OffsetDate: currentOffsetDate,
			OffsetUser: currentOffsetUser,
			Limit:      int32(batchLimit),
		})
		if err != nil {
			return nil, err
		}

		if len(resp.Importers) == 0 {
			break
		}

		userMap := make(map[int64]*UserObj)
		for _, u := range resp.Users {
			if user, ok := u.(*UserObj); ok {
				userMap[user.ID] = user
			}
		}

		for _, importer := range resp.Importers {
			user := userMap[importer.UserID]
			if user == nil {
				user = &UserObj{ID: importer.UserID}
			}
			results = append(results, &JoinRequest{
				User:        user,
				Date:        importer.Date,
				ApprovedBy:  importer.ApprovedBy,
				Requested:   importer.Requested,
				About:       importer.About,
				ViaChatlist: importer.ViaChatlist,
			})
			fetched++
		}

		if len(resp.Users) > 0 {
			if u, ok := resp.Users[len(resp.Users)-1].(*UserObj); ok {
				currentOffsetUser = &InputUserObj{UserID: u.ID, AccessHash: u.AccessHash}
			}
		}
		currentOffsetDate = resp.Importers[len(resp.Importers)-1].Date
	}
	return results, nil
}

// ApproveJoinRequest approves all pending join requests in a chat
func (c *Client) ApproveAllJoinRequests(channel any, invite ...string) error {
	peer, err := c.ResolvePeer(channel)
	if err != nil {
		return err
	}

	_, err = c.MessagesHideAllChatJoinRequests(true, peer, getVariadic(invite, ""))
	return err
}

// TransferChatOwnership transfers the ownership of a chat to another user
func (c *Client) TransferChatOwnership(chatID any, userID any, password string) error {
	peer, err := c.GetSendablePeer(chatID)
	if err != nil {
		return err
	}

	user, err := c.GetSendableUser(userID)
	if err != nil {
		return err
	}

	accountPassword, err := c.AccountGetPassword()
	if err != nil {
		return err
	}

	passwordSrp, err := GetInputCheckPassword(password, accountPassword)
	if err != nil {
		return err
	}

	_, err = c.MessagesEditChatCreator(peer, user, passwordSrp)
	return err
}

func (c *Client) GetLinkedChannel(channel any) (*Channel, error) {
	peer, err := c.GetSendableChannel(channel)
	if err != nil {
		return nil, err
	}
	resp, err := c.ChannelsGetFullChannel(peer)
	if err != nil {
		return nil, err
	}
	c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
	if fullChat, ok := resp.FullChat.(*ChannelFull); ok {
		if fullChat.LinkedChatID != 0 {
			return c.GetChannel(fullChat.LinkedChatID)
		}
		return nil, errors.New("channel is not linked")
	}
	return nil, errors.New("could not get full channel info")
}

func (c *Client) ExportInvite(channel any) (ExportedChatInvite, error) {
	peer, err := c.ResolvePeer(channel)
	if err != nil {
		return nil, err
	}

	return c.MessagesExportChatInvite(&MessagesExportChatInviteParams{
		Peer: peer,
	})
}

func (c *Client) RevokeInvite(channel any, invite string) error {
	peer, err := c.ResolvePeer(channel)
	if err != nil {
		return err
	}

	_, err = c.MessagesEditExportedChatInvite(&MessagesEditExportedChatInviteParams{
		Peer:    peer,
		Link:    invite,
		Revoked: true,
	})
	return err
}
