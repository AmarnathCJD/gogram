// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"math"

	"github.com/pkg/errors"
)

// GetChatPhotos returns the profile photos of a chat
//
//	Params:
//	 - chatID: The ID of the chat
//	 - limit: The maximum number of photos to be returned
func (c *Client) GetChatPhotos(chatID any, limit ...int32) ([]Photo, error) {
	if limit == nil {
		limit = []int32{1}
	}
	messages, err := c.GetMessages(chatID, &SearchOption{Limit: limit[0],
		Filter: &InputMessagesFilterChatPhotos{}})
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
func (c *Client) JoinChannel(Channel any) error {
	switch p := Channel.(type) {
	case string:
		if TG_JOIN_RE.MatchString(p) {
			_, err := c.MessagesImportChatInvite(TG_JOIN_RE.FindStringSubmatch(p)[1])
			if err != nil {
				return err
			}
		}
	default:
		channel, err := c.ResolvePeer(Channel)
		if err != nil {
			return err
		}
		if chat, ok := channel.(*InputPeerChannel); ok {
			_, err = c.ChannelsJoinChannel(&InputChannelObj{ChannelID: chat.ChannelID, AccessHash: chat.AccessHash})
			if err != nil {
				return err
			}
		} else if chat, ok := channel.(*InputPeerChat); ok {
			_, err = c.MessagesAddChatUser(chat.ChatID, &InputUserEmpty{}, 0)
			if err != nil {
				return err
			}
		} else {
			return errors.New("peer is not a channel or chat")
		}
	}
	return nil
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
		_, err = c.MessagesDeleteChatUser(revokeChat, chat.ChatID, &InputUserEmpty{})
		if err != nil {
			return err
		}
	} else {
		return errors.New("peer is not a channel or chat")
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
	participant, err := c.ChannelsGetParticipant(&InputChannelObj{ChannelID: chat.ChannelID, AccessHash: chat.AccessHash}, user)
	if err != nil {
		return nil, err
	}
	c.Cache.UpdatePeersToCache(participant.Users, participant.Chats)
	var (
		status string           = Member
		rights *ChatAdminRights = &ChatAdminRights{}
		rank   string           = ""
		UserID int64            = 0
	)
	switch p := participant.Participant.(type) {
	case *ChannelParticipantCreator:
		status = Creator
		rights = p.AdminRights
		rank = p.Rank
		UserID = p.UserID
	case *ChannelParticipantAdmin:
		status = Admin
		rights = p.AdminRights
		rank = p.Rank
		UserID = p.UserID
	case *ChannelParticipantObj:
		status = Member
		UserID = p.UserID
	case *ChannelParticipantSelf:
		status = Member
		UserID = p.UserID
	case *ChannelParticipantBanned:
		status = Restricted
		UserID = c.GetPeerID(p.Peer)
	case *ChannelParticipantLeft:
		status = Left
		UserID = c.GetPeerID(p.Peer)
	}
	partUser, err := c.GetUser(UserID)
	if err != nil {
		return nil, err
	}
	return &Participant{
		User:        partUser,
		Participant: participant.Participant,
		Status:      status,
		Rights:      rights,
		Rank:        rank,
	}, nil
}

type ParticipantOptions struct {
	Query  string                    `json:"query,omitempty"`
	Filter ChannelParticipantsFilter `json:"filter,omitempty"`
	Offset int32                     `json:"offset,omitempty"`
	Limit  int32                     `json:"limit,omitempty"`
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
		return nil, 0, errors.New("peer is not a channel")
	}
	opts := getVariadic(Opts, &ParticipantOptions{Filter: &ChannelParticipantsSearch{}, Limit: 1})
	if opts.Query != "" {
		opts.Filter = &ChannelParticipantsSearch{Q: opts.Query}
	}
	participants, err := c.ChannelsGetParticipants(&InputChannelObj{ChannelID: chat.ChannelID, AccessHash: chat.AccessHash}, opts.Filter, opts.Offset, opts.Limit, 0)
	if err != nil {
		return nil, 0, err
	}
	cParts, ok := participants.(*ChannelsChannelParticipantsObj)
	if !ok {
		return nil, 0, errors.New("could not get participants")
	}
	c.Cache.UpdatePeersToCache(cParts.Users, cParts.Chats)
	var (
		status string           = Member
		rights *ChatAdminRights = &ChatAdminRights{}
		rank   string           = ""
		UserID int64            = 0
	)
	participantsList := make([]*Participant, 0)
	for _, p := range cParts.Participants {
		switch p := p.(type) {
		case *ChannelParticipantCreator:
			status = Creator
			rights = p.AdminRights
			rank = p.Rank
			UserID = p.UserID
		case *ChannelParticipantAdmin:
			status = Admin
			rights = p.AdminRights
			rank = p.Rank
			UserID = p.UserID
		case *ChannelParticipantObj:
			status = Member
		case *ChannelParticipantSelf:
			status = Member
			UserID = p.UserID
		case *ChannelParticipantBanned:
			status = Restricted
			UserID = c.GetPeerID(p.Peer)
		case *ChannelParticipantLeft:
			status = Left
			UserID = c.GetPeerID(p.Peer)
		}
		partUser, err := c.GetUser(UserID)
		if err != nil {
			return nil, 0, err
		}
		participantsList = append(participantsList, &Participant{
			User:        partUser,
			Participant: p,
			Status:      status,
			Rights:      rights,
			Rank:        rank,
		})
	}
	return participantsList, cParts.Count, nil
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
		return false, errors.New("peer is not a user")
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
		return false, errors.New("peer is not a chat or channel")
	}
	return true, nil
}

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
			return false, errors.New("peer is not a user")
		}
		_, err := c.MessagesDeleteChatUser(false, c.GetPeerID(p), &InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash})
		if err != nil {
			return false, err
		}
	default:
		return false, errors.New("peer is not a chat or channel")
	}
	return true, nil
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
		resp, err := c.StatsGetMessageStats(true, &InputChannelObj{
			ChannelID:  channelPeer.ChannelID,
			AccessHash: channelPeer.AccessHash,
		}, MessageID)
		if err != nil {
			return nil, nil, err
		}
		return nil, resp, nil
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
	if u, ok := u.(*UpdatesObj); ok {
		chat := u.Chats[0]
		if ch, ok := chat.(*Channel); ok {
			return ch, nil
		}
	}
	return nil, errors.New("empty reply from server")
}

func (c *Client) DeleteChannel(channelID any) (*Updates, error) {
	peer, err := c.ResolvePeer(channelID)
	if err != nil {
		return nil, err
	}

	channelPeer, ok := peer.(*InputPeerChannel)
	if !ok {
		return nil, errors.New("could not convert peer to channel")
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

func (c *Client) GetChatJoinRequests(channelID any, lim int, query ...string) ([]*UserObj, error) {
	var currentOffsetUser InputUser = &InputUserEmpty{}
	currentOffsetDate := 0

	current := 0
	if lim <= 0 {
		lim = math.MaxInt32
	}

	limit := min(lim, 100)

	peer, err := c.ResolvePeer(channelID)
	if err != nil {
		return nil, err
	}

	var allUsers []*UserObj

	// Loop until lim is reached
	for {
		// Get chat invite importers
		chatInviteImporters, err := c.MessagesGetChatInviteImporters(&MessagesGetChatInviteImportersParams{
			Requested:  true,
			Peer:       peer,
			Q:          getVariadic(query, ""),
			OffsetDate: int32(currentOffsetDate),
			OffsetUser: currentOffsetUser,
			Limit:      int32(limit),
		})
		if err != nil {
			return nil, err
		}

		if len(chatInviteImporters.Importers) == 0 {
			break
		}

		// Add all UserObj objects to allUsers slice
		for _, user := range chatInviteImporters.Users {
			u, ok := user.(*UserObj)
			if !ok {
				c.Logger.Debug("user is not a UserObj")
				continue
			}
			allUsers = append(allUsers, u)
			current++
		}

		// Break if limit is reached
		if current >= lim {
			break
		}

		// Set current offset user and date for next iteration
		if len(chatInviteImporters.Users) > 0 {
			lastUser := chatInviteImporters.Users[len(chatInviteImporters.Users)-1]
			if u, ok := lastUser.(*UserObj); ok {
				currentOffsetUser = &InputUserObj{UserID: u.ID, AccessHash: u.AccessHash}
			}
		}
		if len(chatInviteImporters.Importers) > 0 {
			currentOffsetDate = int(chatInviteImporters.Importers[len(chatInviteImporters.Importers)-1].Date)
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
