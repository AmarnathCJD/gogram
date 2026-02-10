// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"fmt"
)

type ParticipantUpdate struct {
	Client         *Client
	OriginalUpdate *UpdateChannelParticipant
	Channel        *Channel
	User           *UserObj
	Actor          *UserObj
	Old            ChannelParticipant
	New            ChannelParticipant
	Invite         ExportedChatInvite
	Date           int32
}

func (pu *ParticipantUpdate) ChatID() int64 {
	if pu.Channel != nil {
		return pu.Channel.ID
	}
	return 0
}

func (pu *ParticipantUpdate) ChannelID() int64 {
	if pu.Channel != nil {
		return -100_000_000_0000 - pu.Channel.ID
	}
	return 0
}

func (pu *ParticipantUpdate) UserID() int64 {
	if pu.User != nil {
		return pu.User.ID
	}
	return 0
}

func (pu *ParticipantUpdate) ActorID() int64 {
	if pu.Actor != nil {
		return pu.Actor.ID
	}
	return 0
}

func (pu *ParticipantUpdate) IsAdded() bool {
	if pu.ActorID() != 0 && pu.UserID() != 0 {
		if pu.ActorID() == pu.UserID() {
			return false
		}
	}

	if pu.Old == nil && pu.New != nil {
		if _, ok := pu.New.(*ChannelParticipantObj); ok {
			return true
		} else if _, ok := pu.New.(*ChannelParticipantAdmin); ok {
			return true
		}
	}
	return false
}

func (pu *ParticipantUpdate) IsLeft() bool {
	return pu.New == nil
}

func (pu *ParticipantUpdate) IsJoined() bool {
	if pu.ActorID() != 0 && pu.UserID() != 0 {
		if pu.ActorID() != pu.UserID() {
			return false
		}
	}

	if pu.Old == nil && pu.New != nil {
		if _, ok := pu.New.(*ChannelParticipantObj); ok {
			return true
		}
	}
	return false
}

func (pu *ParticipantUpdate) IsBanned() bool {
	if pu.Old != nil && pu.New != nil {
		if _, ok := pu.Old.(*ChannelParticipantObj); ok {
			if _, ok := pu.New.(*ChannelParticipantBanned); ok {
				return true
			}
		}
	}
	return false
}

func (pu *ParticipantUpdate) IsKicked() bool {
	if pu.Old != nil && pu.New != nil {
		if _, ok := pu.Old.(*ChannelParticipantObj); ok {
			if _, ok := pu.New.(*ChannelParticipantLeft); ok {
				return true
			}
		}
	}
	return false
}

func (pu *ParticipantUpdate) IsPromoted() bool {
	if pu.New == nil {
		return false
	}

	switch pu.New.(type) {
	case *ChannelParticipantAdmin, *ChannelParticipantCreator:
		if pu.Old == nil {
			return true
		}

		switch pu.Old.(type) {
		case *ChannelParticipantAdmin, *ChannelParticipantCreator:
			return false
		default:
			return true
		}
	}

	return false
}

func (pu *ParticipantUpdate) IsDemoted() bool {
	if pu.Old == nil {
		return false
	}

	if _, ok := pu.Old.(*ChannelParticipantAdmin); ok {
		if pu.New == nil {
			return true
		}

		switch pu.New.(type) {
		case *ChannelParticipantAdmin, *ChannelParticipantCreator:
			return false
		default:
			return true
		}
	}
	return false
}

func (pu *ParticipantUpdate) Marshal(noindent ...bool) string {
	return MarshalWithTypeName(pu.OriginalUpdate, noindent...)
}

func (pu *ParticipantUpdate) Ban() (bool, error) {
	if pu.User == nil || pu.Channel == nil {
		return false, fmt.Errorf("user or channel is nil, cannot ban")
	}

	_, err := pu.Client.EditBanned(pu.Channel, pu.User, &BannedOptions{
		Ban: true,
	})

	return err == nil, err
}

func (pu *ParticipantUpdate) Unban() (bool, error) {
	if pu.User == nil || pu.Channel == nil {
		return false, fmt.Errorf("user or channel is nil, cannot unban")
	}

	_, err := pu.Client.EditBanned(pu.Channel, pu.User, &BannedOptions{
		Unban: true,
	})

	return err == nil, err
}

func (pu *ParticipantUpdate) Kick() (bool, error) {
	if pu.User == nil || pu.Channel == nil {
		return false, fmt.Errorf("user or channel is nil, cannot kick")
	}

	_, err := pu.Client.KickParticipant(pu.Channel, pu.User)
	return err == nil, err
}

func (pu *ParticipantUpdate) Promote() (bool, error) {
	if pu.User == nil || pu.Channel == nil {
		return false, fmt.Errorf("user or channel is nil, cannot promote")
	}

	_, err := pu.Client.EditAdmin(pu.Channel, pu.User, &AdminOptions{
		IsAdmin: true,
	})

	return err == nil, err
}

func (pu *ParticipantUpdate) Demote() (bool, error) {
	if pu.User == nil || pu.Channel == nil {
		return false, fmt.Errorf("user or channel is nil, cannot demote")
	}

	_, err := pu.Client.EditAdmin(pu.Channel, pu.User, &AdminOptions{
		IsAdmin: false,
	})

	return err == nil, err
}

type JoinRequestUpdate struct {
	Client            *Client
	OriginalUpdate    *UpdatePendingJoinRequests
	BotOriginalUpdate *UpdateBotChatInviteRequester
	Channel           *Channel
	Chat              *ChatObj
	Users             []*UserObj
	PendingCount      int32
}

func (jru *JoinRequestUpdate) ChatID() int64 {
	if jru.Channel != nil {
		return jru.Channel.ID
	} else if jru.Chat != nil {
		return jru.Chat.ID
	}
	return 0
}

func (jru *JoinRequestUpdate) ChannelID() int64 {
	if jru.Channel != nil {
		return -100_000_000_0000 - jru.Channel.ID
	} else if jru.Chat != nil {
		return -jru.Chat.ID
	}
	return 0
}

func (jru *JoinRequestUpdate) UserIDs() []int64 {
	ids := make([]int64, 0, len(jru.Users))
	for _, u := range jru.Users {
		ids = append(ids, u.ID)
	}
	return ids
}

func (jru *JoinRequestUpdate) IsEmpty() bool {
	return len(jru.Users) == 0
}

func (jru *JoinRequestUpdate) HasUser(userID int64) bool {
	for _, u := range jru.Users {
		if u.ID == userID {
			return true
		}
	}
	return false
}

func (jru *JoinRequestUpdate) GetPeer() (Peer, error) {
	if jru.OriginalUpdate != nil {
		return jru.OriginalUpdate.Peer, nil
	} else if jru.BotOriginalUpdate != nil {
		return jru.BotOriginalUpdate.Peer, nil
	}
	return nil, fmt.Errorf("no original update found")
}

func (jru *JoinRequestUpdate) GetInputPeer() (InputPeer, error) {
	peer, err := jru.GetPeer()
	if err != nil {
		return nil, err
	}
	return jru.Client.GetSendablePeer(peer)
}

func (jru *JoinRequestUpdate) Approve(userID int64) (bool, error) {
	if jru.Channel == nil && jru.Chat == nil {
		return false, fmt.Errorf("channel/chat is nil")
	}
	peer, err := jru.GetInputPeer()
	if err != nil {
		return false, err
	}

	user, err := jru.Client.GetSendableUser(userID)
	if err != nil {
		return false, err
	}
	_, err = jru.Client.MessagesHideChatJoinRequest(true, peer, user)
	return err == nil, err
}

func (jru *JoinRequestUpdate) ApproveAll() (bool, error) {
	if jru.Channel == nil && jru.Chat == nil {
		return false, fmt.Errorf("channel/chat is nil")
	}
	peer, err := jru.GetInputPeer()
	if err != nil {
		return false, err
	}
	link := ""

	if jru.BotOriginalUpdate != nil {
		switch jru.BotOriginalUpdate.Invite.(type) {
		case *ChatInviteExported:
			link = jru.BotOriginalUpdate.Invite.(*ChatInviteExported).Link
		}
	}
	_, err = jru.Client.MessagesHideAllChatJoinRequests(true, peer, link)
	return err == nil, err
}

func (jru *JoinRequestUpdate) Decline(userID int64) (bool, error) {
	if jru.Channel == nil && jru.Chat == nil {
		return false, fmt.Errorf("channel/chat is nil")
	}
	peer, err := jru.GetInputPeer()
	if err != nil {
		return false, err
	}
	user, err := jru.Client.GetSendableUser(userID)
	if err != nil {
		return false, err
	}
	_, err = jru.Client.MessagesHideChatJoinRequest(false, peer, user)
	return err == nil, err
}

func (jru *JoinRequestUpdate) DeclineAll() (bool, error) {
	if jru.Channel == nil && jru.Chat == nil {
		return false, fmt.Errorf("channel/chat is nil")
	}
	peer, err := jru.GetInputPeer()
	if err != nil {
		return false, err
	}
	link := ""
	if jru.BotOriginalUpdate != nil {
		switch jru.BotOriginalUpdate.Invite.(type) {
		case *ChatInviteExported:
			link = jru.BotOriginalUpdate.Invite.(*ChatInviteExported).Link
		}
	}
	_, err = jru.Client.MessagesHideAllChatJoinRequests(false, peer, link)
	return err == nil, err
}

func (jru *JoinRequestUpdate) Marshal(noindent ...bool) string {
	if jru.OriginalUpdate != nil {
		return MarshalWithTypeName(jru.OriginalUpdate, noindent...)
	} else if jru.BotOriginalUpdate != nil {
		return MarshalWithTypeName(jru.BotOriginalUpdate, noindent...)
	}
	return "{}"
}
