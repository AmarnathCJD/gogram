// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"github.com/pkg/errors"
)

// GetChatPhotos returns the profile photos of a chat
//
//	Params:
//	 - chatID: The ID of the chat
//	 - limit: The maximum number of photos to be returned
func (c *Client) GetChatPhotos(chatID interface{}, limit ...int32) ([]Photo, error) {
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
func (c *Client) GetChatPhoto(chatID interface{}) (Photo, error) {
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
func (c *Client) JoinChannel(Channel interface{}) error {
	switch p := Channel.(type) {
	case string:
		if TG_JOIN_RE.MatchString(p) {
			_, err := c.MessagesImportChatInvite(TG_JOIN_RE.FindStringSubmatch(p)[2])
			if err != nil {
				return err
			}
		}
	default:
		channel, err := c.GetSendablePeer(Channel)
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
func (c *Client) LeaveChannel(Channel interface{}, Revoke ...bool) error {
	revokeChat := getVariadic(Revoke, false).(bool)
	channel, err := c.GetSendablePeer(Channel)
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
func (c *Client) GetChatMember(chatID, userID interface{}) (*Participant, error) {
	channel, err := c.GetSendablePeer(chatID)
	if err != nil {
		return nil, err
	}
	user, err := c.GetSendablePeer(userID)
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
func (c *Client) GetChatMembers(chatID interface{}, Opts ...*ParticipantOptions) ([]*Participant, int32, error) {
	channel, err := c.GetSendablePeer(chatID)
	if err != nil {
		return nil, 0, err
	}
	chat, ok := channel.(*InputPeerChannel)
	if !ok {
		return nil, 0, errors.New("peer is not a channel")
	}
	opts := getVariadic(Opts, &ParticipantOptions{Filter: &ChannelParticipantsSearch{}, Limit: 1}).(*ParticipantOptions)
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

// Edit Admin rights of a user in a chat,
// returns true if successful
func (c *Client) EditAdmin(PeerID, UserID interface{}, Opts ...*AdminOptions) (bool, error) {
	opts := getVariadic(Opts, &AdminOptions{IsAdmin: true, Rights: &ChatAdminRights{}, Rank: "Admin"}).(*AdminOptions)
	peer, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return false, err
	}
	u, err := c.GetSendablePeer(UserID)
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

// Edit Restricted rights of a user in a chat,
// returns true if successful
func (c *Client) EditBanned(PeerID, UserID interface{}, opts ...*BannedOptions) (bool, error) {
	o := getVariadic(opts, &BannedOptions{Ban: true, Rights: &ChatBannedRights{}}).(*BannedOptions)
	if o.Rights == nil {
		o.Rights = &ChatBannedRights{}
	}
	peer, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return false, err
	}
	u, err := c.GetSendablePeer(UserID)
	if err != nil {
		return false, err
	}
	switch p := peer.(type) {
	case *InputPeerChannel:
		if o.Ban {
			o.Rights.ViewMessages = true
		}
		if o.Unban {
			o.Rights.ViewMessages = false
		}
		if o.Mute {
			o.Rights.SendMessages = true
		}
		if o.Unmute {
			o.Rights.SendMessages = false
		}
		_, err := c.ChannelsEditBanned(&InputChannelObj{ChannelID: p.ChannelID, AccessHash: p.AccessHash}, u, o.Rights)
		if err != nil {
			return false, err
		}
	case *InputPeerChat:
		if o.Ban || o.Unban || o.Mute || o.Unmute {
			if u, ok := u.(*InputPeerUser); ok {
				_, err := c.MessagesDeleteChatUser(o.Revoke, p.ChatID, &InputUserObj{UserID: u.UserID, AccessHash: u.AccessHash})
				return err == nil, err
			}
		} else {
			if u, ok := u.(*InputPeerUser); ok {
				_, err := c.MessagesAddChatUser(p.ChatID, &InputUserObj{UserID: u.UserID, AccessHash: u.AccessHash}, 0)
				return err == nil, err
			}
		}
	default:
		return false, errors.New("peer is not a chat or channel")
	}
	return true, nil
}

func (c *Client) KickParticipant(PeerID, UserID interface{}) (bool, error) {
	peer, err := c.GetSendablePeer(PeerID)
	if err != nil {
		return false, err
	}
	u, err := c.GetSendablePeer(UserID)
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
func (c *Client) EditTitle(PeerID interface{}, Title string, Opts ...*TitleOptions) (bool, error) {
	opts := getVariadic(Opts, &TitleOptions{}).(*TitleOptions)
	peer, err := c.GetSendablePeer(PeerID)
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
func (c *Client) GetStats(channelID interface{}, messageID ...interface{}) (*StatsBroadcastStats, *StatsMessageStats, error) {
	peerID, err := c.GetSendablePeer(channelID)
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
func (c *Client) GetChatInviteLink(peerID interface{}, LinkOpts ...*InviteLinkOptions) (ExportedChatInvite, error) {
	LinkOptions := getVariadic(LinkOpts, &InviteLinkOptions{}).(*InviteLinkOptions)
	peer, err := c.GetSendablePeer(peerID)
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

func (c *Client) CreateChannel(title string, opts ...ChannelOptions) (*Channel, error) {
	opt := getVariadic(opts, &ChannelOptions{}).(*ChannelOptions)
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
	switch u := u.(type) {
	case *UpdatesObj:
		chat := u.Chats[0]
		if ch, ok := chat.(*Channel); ok {
			return ch, nil
		}
	}
	return nil, errors.New("empty reply from server")
}

func (c *Client) DeleteChannel(channelID interface{}) (*Updates, error) {
	peer, err := c.GetSendablePeer(channelID)
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

func (c *Client) GetChatJoinRequests(channelID interface{}, lim int) ([]*UserObj, error) {
	perLimit := 100
	var currentOffsetUser InputUser = &InputUserEmpty{}
	currentOffsetDate := 0

	// Set active limit to lim if it's less than perLimit, else set it to perLimit
	activeLimit := perLimit
	if lim < perLimit {
		activeLimit = lim
	}

	// Get sendable peer
	peer, err := c.GetSendablePeer(channelID)
	if err != nil {
		return nil, err
	}

	// Initialize empty slice to store all users
	var allUsers []*UserObj

	// Loop until lim is reached
	for {
		// Get chat invite importers
		chatInviteImporters, err := c.MessagesGetChatInviteImporters(&MessagesGetChatInviteImportersParams{
			Requested:  true,
			Peer:       peer,
			Q:          "",
			OffsetDate: int32(currentOffsetDate),
			OffsetUser: currentOffsetUser,
			Limit:      int32(activeLimit),
		})
		if err != nil {
			return nil, err
		}

		// Add all UserObj objects to allUsers slice
		for _, user := range chatInviteImporters.Users {
			if u, ok := user.(*UserObj); ok {
				allUsers = append(allUsers, u)
			}
		}

		// Decrement lim by activeLimit
		lim -= activeLimit

		// Break out of loop if lim is reached
		if lim <= 0 {
			break
		}

		// Set current offset user and date for next iteration
		if len(chatInviteImporters.Users) > 0 {
			if u, ok := chatInviteImporters.Users[len(chatInviteImporters.Users)-1].(*UserObj); ok {
				currentOffsetUser = &InputUserObj{UserID: u.ID, AccessHash: u.AccessHash}
			}
		}
		if len(chatInviteImporters.Importers) > 0 {
			currentOffsetDate = int(chatInviteImporters.Importers[len(chatInviteImporters.Importers)-1].Date)
		}
	}

	return allUsers, nil
}
