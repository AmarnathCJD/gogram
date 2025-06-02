// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"math"
	"reflect"
	"time"

	"github.com/pkg/errors"
)

// GetChatPhotos returns the profile photos of a chat
//
//	Params:
//	 - chatID: The ID of the chat
//	 - limit: The maximum number of photos to be returned
func (c *Client) GetChatPhotos(chatID any, limit ...int32) ([]Photo, error) {
	limitVal := int32(1)
	if len(limit) > 0 && limit[0] > 0 {
		limitVal = limit[0]
	}

	messages, err := c.GetMessages(chatID, &SearchOption{
		Limit:  limitVal,
		Filter: &InputMessagesFilterChatPhotos{},
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch chat photos")
	}

	photos := make([]Photo, 0, len(messages))
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
	photos, err := c.GetChatPhotos(chatID, 1)
	if err != nil {
		return &PhotoObj{}, errors.Wrap(err, "failed to get chat photo")
	} else if len(photos) > 0 {
		return photos[0], nil
	}

	return &PhotoObj{}, nil // GetFullChannel TODO
}

// JoinChannel joins a channel or chat by its username or id
//
//	Params:
//	- Channel: the username or id of the channel or chat
func (c *Client) JoinChannel(Channel any) (bool, error) {
	switch p := Channel.(type) {
	case string:
		if TG_JOIN_RE.MatchString(p) {
			result, err := c.MessagesImportChatInvite(TG_JOIN_RE.FindStringSubmatch(p)[1])
			if err != nil {
				return false, errors.Wrap(err, "failed to import chat invite")
			}

			switch result := result.(type) {
			case *UpdatesObj:
				c.Cache.UpdatePeersToCache(result.Users, result.Chats)
			}

			return true, nil
		} else if USERNAME_RE.MatchString(p) {
			return c.joinChannelByPeer(USERNAME_RE.FindStringSubmatch(p)[1])
		}

		return false, errors.New("invalid channel or chat")
	case *InputPeerChannel, *InputPeerChat, int, int32, int64:
		return c.joinChannelByPeer(p)
	case *ChatInviteExported:
		if _, err := c.MessagesImportChatInvite(p.Link); err != nil {
			return false, errors.Wrap(err, "failed to join via invite link")
		}

		return true, nil
	default:
		return c.joinChannelByPeer(Channel)
	}
}

func (c *Client) joinChannelByPeer(Channel any) (bool, error) {
	channel, err := c.ResolvePeer(Channel)
	if err != nil {
		return false, errors.Wrap(err, "failed to resolve peer")
	}

	if chat, ok := channel.(*InputPeerChannel); ok {
		if _, err = c.ChannelsJoinChannel(
			&InputChannelObj{ChannelID: chat.ChannelID, AccessHash: chat.AccessHash},
		); err != nil {
			return false, errors.Wrap(err, "failed to join channel")
		}
	} else if chat, ok := channel.(*InputPeerChat); ok {
		if _, err = c.MessagesAddChatUser(chat.ChatID, &InputUserEmpty{}, 0); err != nil {
			return false, errors.Wrap(err, "failed to join chat")
		}
	} else {
		return false, errors.New("peer is not a channel or chat")
	}

	return true, nil
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
		return errors.Wrap(err, "failed to resolve peer")
	}

	switch p := channel.(type) {
	case *InputPeerChannel:
		_, err = c.ChannelsLeaveChannel(&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash})
		if err != nil {
			return errors.Wrap(err, "failed to leave channel")
		}
	case *InputPeerChat:
		_, err = c.MessagesDeleteChatUser(revokeChat, p.ChatID, &InputUserEmpty{})
		if err != nil {
			return errors.Wrap(err, "failed to leave chat")
		}
	default:
		return errors.New("peer is not a channel or chat")
	}

	return nil
}

// Constants for participant statuses
const (
	Admin      = "admin"
	Creator    = "creator"
	Member     = "member"
	Restricted = "restricted"
	Left       = "left"
	Kicked     = "kicked"
)

// Participant represents a chat member with status and rights.
type Participant struct {
	User        *UserObj           `json:"user,omitempty"`
	Participant ChannelParticipant `json:"participant,omitempty"`
	Status      string             `json:"status,omitempty"`
	Rights      *ChatAdminRights   `json:"rights,omitempty"`
	Rank        string             `json:"rank,omitempty"`
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
		return nil, errors.New("peer is not a channel")
	}

	participant, err := c.ChannelsGetParticipant(
		&InputChannelObj{ChannelID: chat.ChannelID, AccessHash: chat.AccessHash}, user,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get participant")
	}
	c.Cache.UpdatePeersToCache(participant.Users, participant.Chats)

	status, rights, rank, userID := parseParticipant(participant.Participant, c)
	partUser, err := c.GetUser(userID.(int64))
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user")
	}

	return &Participant{
		User:        partUser,
		Participant: participant.Participant,
		Status:      status,
		Rights:      rights,
		Rank:        rank,
	}, nil
}

// parseParticipant extracts status, rights, rank, and user ID from a participant.]
func parseParticipant(p ChannelParticipant, c *Client) (status string, rights *ChatAdminRights, rank string, userID int64) {
	rights = &ChatAdminRights{}
	status = Member

	switch part := p.(type) {
	case *ChannelParticipantCreator:
		status = Creator
		rights = part.AdminRights
		rank = part.Rank
		userID = part.UserID
	case *ChannelParticipantAdmin:
		status = Admin
		rights = part.AdminRights
		rank = part.Rank
		userID = part.UserID
	case *ChannelParticipantObj, *ChannelParticipantSelf:
		userID = part.(*ChannelParticipantObj).UserID
	case *ChannelParticipantBanned, *ChannelParticipantLeft:
		status = Restricted
		if banned, ok := part.(*ChannelParticipantBanned); ok {
			userID = c.GetPeerID(banned.Peer)
		} else {
			userID = c.GetPeerID(part.(*ChannelParticipantLeft).Peer)
		}
	}
	return
}

// ParticipantOptions configures chat member retrieval.
type ParticipantOptions struct {
	Query            string                    `json:"query,omitempty"`
	Filter           ChannelParticipantsFilter `json:"filter,omitempty"`
	Offset           int32                     `json:"offset,omitempty"`
	Limit            int32                     `json:"limit,omitempty"`
	SleepThresholdMs int32                     `json:"sleep_threshold_ms,omitempty"`
}

// GetChatMembers returns the members of a chat
//
//	Params:
//	 - chatID: The ID of the chat
//	 - filter: The filter to use
//	 - offset: The offset to use
//	 - limit: The limit to use
func (c *Client) GetChatMembers(chatID any, opts ...*ParticipantOptions) ([]*Participant, int32, error) {
	channel, err := c.ResolvePeer(chatID)
	if err != nil {
		return nil, 0, err
	}

	chat, ok := channel.(*InputPeerChannel)
	if !ok {
		return nil, 0, errors.New("peer is not a channel")
	}
	opt := getVariadic(opts, &ParticipantOptions{Filter: &ChannelParticipantsSearch{}, Limit: 1})
	if opt.Query != "" {
		opt.Filter = &ChannelParticipantsSearch{Q: opt.Query}
	}
	if opt.Filter == nil {
		opt.Filter = &ChannelParticipantsSearch{}
	}

	participantsList := make([]*Participant, 0, opt.Limit)
	var fetched, totalCount int32
	const batchSize int32 = 200

	// var fetched int32 = 0
	// var participantsList []*Participant
	// var reqLimit, reqOffset int32 = 200, opts.Offset
	// var totalCount int32

	for {
		remaining := opt.Limit - fetched
		if remaining <= 0 && opt.Limit > 0 {
			break
		}

		reqLimit := min(remaining, batchSize)
		participants, err := c.ChannelsGetParticipants(
			&InputChannelObj{ChannelID: chat.ChannelID, AccessHash: chat.AccessHash},
			opt.Filter, opt.Offset+fetched, reqLimit, 0,
		)
		if err != nil {
			return nil, 0, errors.Wrap(err, "failed to get participants")
		}

		cParts, ok := participants.(*ChannelsChannelParticipantsObj)
		if !ok {
			return nil, 0, errors.New("unexpected response type for participants")
		}
		if opt.Limit == -1 {
			opt.Limit = cParts.Count
			continue
		}

		c.Cache.UpdatePeersToCache(cParts.Users, cParts.Chats)
		for _, p := range cParts.Participants {
			status, rights, rank, userID := parseParticipant(p, c)
			partUser, err := c.GetUser(userID)
			if err != nil {
				return nil, 0, errors.Wrap(err, "failed to get user")
			}

			participantsList = append(participantsList, &Participant{
				User:        partUser,
				Participant: p,
				Status:      status,
				Rights:      rights,
				Rank:        rank,
			})
			fetched++
		}

		totalCount = cParts.Count
		if int32(len(cParts.Participants)) < reqLimit || (opt.Limit > 0 && fetched >= opt.Limit) {
			break
		}
		if opt.SleepThresholdMs > 0 {
			time.Sleep(time.Duration(opt.SleepThresholdMs) * time.Millisecond)
		}
	}
	return participantsList, totalCount, nil
}

func (c *Client) IterChatMembers(chatID any, opts ...*ParticipantOptions) (<-chan *Participant, <-chan error) {
	ch := make(chan *Participant, 100) // Buffered channel for better throughput
	errCh := make(chan error, 1)       // Buffered error channel

	chat, err := c.ResolvePeer(chatID)
	if err != nil {
		errCh <- errors.Wrap(err, "failed to resolve chat peer")
		close(ch)
		close(errCh)
		return ch, errCh
	}

	chatPeer, ok := chat.(*InputPeerChannel)
	if !ok {
		errCh <- errors.New("peer is not a channel")
		close(ch)
		close(errCh)
		return ch, errCh
	}

	go func() {
		defer close(ch)
		defer close(errCh)

		opt := getVariadic(opts, &ParticipantOptions{
			Limit:            1,
			SleepThresholdMs: 20,
			Filter:           &ChannelParticipantsSearch{},
		})
		if opt.Query != "" {
			opt.Filter = &ChannelParticipantsSearch{Q: opt.Query}
		}
		if opt.Filter == nil {
			opt.Filter = &ChannelParticipantsSearch{}
		}

		req := &ChannelsGetParticipantsParams{
			Channel: &InputChannelObj{ChannelID: chatPeer.ChannelID, AccessHash: chatPeer.AccessHash},
			Filter:  opt.Filter,
			Offset:  opt.Offset,
			Limit:   200,
			Hash:    0,
		}
		var fetched int32
		const batchSize int32 = 200

		for {
			if opt.Limit > 0 && fetched >= opt.Limit {
				return
			}
			req.Limit = min(opt.Limit-fetched, batchSize)
			if opt.Limit == -1 {
				req.Limit = 0
			}

			resp, err := c.MakeRequest(req)
			if err != nil {
				if handleIfFlood(err, c) {
					continue
				}
				errCh <- errors.Wrap(err, "failed to fetch participants")
				return
			}

			switch r := resp.(type) {
			case *ChannelsChannelParticipantsObj:
				if opt.Limit == -1 {
					opt.Limit = r.Count
					continue
				}
				c.Cache.UpdatePeersToCache(r.Users, r.Chats)
				for _, p := range r.Participants {
					status, rights, rank, userID := parseParticipant(p, c)
					partUser, err := c.GetUser(userID)
					if err != nil {
						errCh <- errors.Wrap(err, "failed to get user")
						return
					}
					ch <- &Participant{
						User:        partUser,
						Participant: p,
						Status:      status,
						Rights:      rights,
						Rank:        rank,
					}
					fetched++
				}
				if len(r.Participants) < int(req.Limit) || (opt.Limit > 0 && fetched >= opt.Limit) {
					return
				}

				req.Offset = fetched
				if opt.SleepThresholdMs > 0 {
					time.Sleep(time.Duration(opt.SleepThresholdMs) * time.Millisecond)
				}
			default:
				errCh <- errors.New("unexpected response: " + reflect.TypeOf(resp).String())
				return
			}
		}
	}()

	return ch, errCh
}

// AdminOptions configures admin rights for a user.
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
		return false, errors.New("peer is not a user")
	}

	switch p := peer.(type) {
	case *InputPeerChannel:
		if opts.IsAdmin {
			if _, err = c.ChannelsEditAdmin(
				&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
				&InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash}, opts.Rights, opts.Rank,
			); err != nil {
				return false, err
			}
		} else {
			if _, err = c.ChannelsEditAdmin(
				&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
				&InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash}, &ChatAdminRights{}, "",
			); err != nil {
				return false, err
			}
		}
	case *InputPeerChat:
		if _, err = c.MessagesEditChatAdmin(
			p.ChatID, &InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash}, opts.IsAdmin,
		); err != nil {
			return false, err
		}
	default:
		return false, errors.New("peer is not a chat or channel")
	}

	return true, nil
}

// BannedOptions configures ban/mute settings for a user.
type BannedOptions struct {
	Ban    bool              `json:"ban,omitempty"`
	Unban  bool              `json:"unban,omitempty"`
	Mute   bool              `json:"mute,omitempty"`
	Unmute bool              `json:"unmute,omitempty"`
	Rights *ChatBannedRights `json:"rights,omitempty"`
	Revoke bool              `json:"revoke,omitempty"`
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
		return false, errors.New("peer is not a chat or channel")
	}
}

func handleChannelBan(c *Client, p *InputPeerChannel, u InputPeer, o *BannedOptions) (bool, error) {
	o.Rights.ViewMessages = o.Ban
	o.Rights.SendMessages = o.Mute

	_, err := c.ChannelsEditBanned(&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash}, u, o.Rights)
	return err == nil, err
}

// handleChatBan applies ban/mute settings for a chat user.
func handleChatBan(c *Client, p *InputPeerChat, u InputPeer, o *BannedOptions) (bool, error) {
	user, ok := u.(*InputPeerUser)
	if !ok {
		return false, errors.New("user is not a valid InputPeerUser")
	}

	if o.Ban || o.Unban || o.Mute || o.Unmute {
		_, err := c.MessagesDeleteChatUser(o.Revoke, p.ChatID, &InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash})
		if err != nil {
			return false, errors.Wrap(err, "failed to delete chat user")
		}
		return true, nil
	}

	_, err := c.MessagesAddChatUser(p.ChatID, &InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash}, 0)
	if err != nil {
		return false, errors.Wrap(err, "failed to add chat user")
	}
	return true, nil
}

// KickParticipant removes a user from a chat or channel efficiently.
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
		if _, err := c.EditBanned(p, u, &BannedOptions{Ban: true}); err != nil {
			return false, err
		}

		if _, err = c.EditBanned(p, u, &BannedOptions{Unban: true}); err != nil {
			return false, err
		}
	case *InputPeerChat:
		user, ok := u.(*InputPeerUser)
		if !ok {
			return false, errors.New("peer is not a user")
		}

		if _, err := c.MessagesDeleteChatUser(
			false, c.GetPeerID(p), &InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash},
		); err != nil {
			return false, err
		}
	default:
		return false, errors.New("peer is not a chat or channel")
	}
	return true, nil
}

// TitleOptions configures title and profile settings.
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
		if _, err := c.ChannelsEditTitle(
			&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash}, Title,
		); err != nil {
			return false, err
		}
	case *InputPeerChat:
		if _, err = c.MessagesEditChatTitle(p.ChatID, Title); err != nil {
			return false, err
		}
	case *InputPeerSelf:
		if _, err := c.AccountUpdateProfile(opts.LastName, Title, opts.About); err != nil {
			return false, err
		}
	default:
		return false, errors.New("peer is not a chat or channel or self")
	}
	return true, nil
}

// GetStats returns the stats of the channel or message
//
//	Params:
//	 - channelID: the channel ID
//	 - messageID: the message ID
func (c *Client) GetStats(channelID any, messageID ...any) (*StatsBroadcastStats, *StatsMessageStats, error) {
	peerID, err := c.ResolvePeer(channelID)
	if err != nil {
		return nil, nil, err
	}

	channelPeer, ok := peerID.(*InputPeerChannel)
	if !ok {
		return nil, nil, errors.New("could not convert peer to channel")
	}

	var MessageID int32 = getVariadic(messageID, 0).(int32)
	if MessageID > 0 {
		stats, err := c.StatsGetMessageStats(true, &InputChannelObj{
			ChannelID:  channelPeer.ChannelID,
			AccessHash: channelPeer.AccessHash,
		}, MessageID)
		if err != nil {
			return nil, nil, err
		}
		return nil, stats, nil
	}

	resp, err := c.StatsGetBroadcastStats(true, &InputChannelObj{
		ChannelID:  channelPeer.ChannelID,
		AccessHash: channelPeer.AccessHash,
	})
	if err != nil {
		return nil, nil, err
	}
	return resp, nil, nil
}

// InviteLinkOptions configures chat invite link settings.
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

// ChannelOptions configures settings for creating a channel.
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
	u, err := c.ChannelsCreateChannel(&ChannelsCreateChannelParams{
		Broadcast: !opt.NotBroadcast,
		GeoPoint:  opt.GeoPoint,
		About:     opt.About, ForImport: opt.ForImport,
		Address:   opt.Address,
		Megagroup: opt.Megagroup,
		Title:     title,
	})
	if err != nil {
		return nil, err
	}

	if updates, ok := u.(*UpdatesObj); ok && len(updates.Chats) > 0 {
		if ch, ok := updates.Chats[0].(*Channel); ok {
			return ch, nil
		}
	}
	return nil, errors.New("empty or invalid response from server")
}

func (c *Client) DeleteChannel(channelID any) (*Updates, error) {
	peer, err := c.ResolvePeer(channelID)
	if err != nil {
		return nil, err
	}

	channelPeer, ok := peer.(*InputPeerChannel)
	if !ok {
		return nil, errors.New("peer is not a channel")
	}

	u, err := c.ChannelsDeleteChannel(&InputChannelObj{
		ChannelID:  channelPeer.ChannelID,
		AccessHash: channelPeer.AccessHash,
	})
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// JoinRequest represents a chat join request.
type JoinRequest struct {
	User        *UserObj
	Date        int32
	ApprovedBy  int64
	Requested   bool
	About       string
	ViaChatlist bool
}

// GetChatJoinRequests fetches chat join requests with optimized batching.
// Parameters:
//   - channelID: The ID of the channel (any type, resolved to peer)
//   - lim: Maximum number of requests to fetch
//   - query: Optional search query for filtering requests
func (c *Client) GetChatJoinRequests(channelID any, lim int, query ...string) ([]*JoinRequest, error) {
	peer, err := c.ResolvePeer(channelID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve channel peer")
	}
	if lim <= 0 {
		lim = math.MaxInt32
	}

	const batchSize = 100
	limit := min(lim, batchSize)
	allUsers := make([]*JoinRequest, 0, limit)
	currentOffsetUser := InputUser(&InputUserEmpty{})
	currentOffsetDate := int32(0)
	current := 0

	for {
		importers, err := c.MessagesGetChatInviteImporters(&MessagesGetChatInviteImportersParams{
			Requested:  true,
			Peer:       peer,
			Q:          getVariadic(query, ""),
			OffsetDate: currentOffsetDate,
			OffsetUser: currentOffsetUser,
			Limit:      int32(limit),
		})
		if err != nil {
			return nil, errors.Wrap(err, "failed to get chat invite importers")
		}
		if len(importers.Importers) == 0 {
			break
		}

		for _, user := range importers.Importers {
			var userObj *UserObj
			for _, u := range importers.Users {
				if uObj, ok := u.(*UserObj); ok && uObj.ID == user.UserID {
					userObj = uObj
					break
				}
			}
			if userObj == nil {
				userObj = &UserObj{ID: user.UserID}
			}

			allUsers = append(allUsers, &JoinRequest{
				User:        userObj,
				Date:        user.Date,
				ApprovedBy:  user.ApprovedBy,
				Requested:   user.Requested,
				About:       user.About,
				ViaChatlist: user.ViaChatlist,
			})
			current++
			if current >= lim {
				break
			}
		}
		if current >= lim {
			break
		}
		if len(importers.Users) > 0 {
			if u, ok := importers.Users[len(importers.Users)-1].(*UserObj); ok {
				currentOffsetUser = &InputUserObj{UserID: u.ID, AccessHash: u.AccessHash}
			}
		}
		if len(importers.Importers) > 0 {
			currentOffsetDate = importers.Importers[len(importers.Importers)-1].Date
		}
	}
	return allUsers, nil
}

// ApproveJoinRequest approves all pending join requests in a chat
func (c *Client) ApproveAllJoinRequests(channelID any, invite ...string) error {
	peer, err := c.ResolvePeer(channelID)
	if err != nil {
		return err
	}

	_, err = c.MessagesHideAllChatJoinRequests(true, peer, getVariadic(invite, ""))
	return err
}

// TransferChatOwnership transfers the ownership of a chat to another user
func (c *Client) TransferChatOwnership(chatID any, userID any, password string) error {
	var inputChannel *InputChannelObj
	var inputUser *InputUserObj

	peer, err := c.ResolvePeer(chatID)
	if err != nil {
		return err
	}

	if channel, ok := peer.(*InputPeerChannel); !ok {
		return errors.New("chat peer is not a channel")
	} else {
		inputChannel = &InputChannelObj{
			ChannelID:  channel.ChannelID,
			AccessHash: channel.AccessHash,
		}
	}

	user, err := c.ResolvePeer(userID)
	if err != nil {
		return err
	}

	if inpUser, ok := user.(*InputPeerUser); !ok {
		return errors.New("user peer is not a user")
	} else {
		inputUser = &InputUserObj{
			UserID:     inpUser.UserID,
			AccessHash: inpUser.AccessHash,
		}
	}

	accountPassword, err := c.AccountGetPassword()
	if err != nil {
		return err
	}

	passwordSrp, err := GetInputCheckPassword(password, accountPassword)
	if err != nil {
		return err
	}

	_, err = c.ChannelsEditCreator(inputChannel, inputUser, passwordSrp)
	return err
}
