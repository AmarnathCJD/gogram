// Copyright (c) 2024 RoseLoverX

package telegram

import "errors"

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

func (pu *ParticipantUpdate) ChannelID() int64 {
	if pu.Channel != nil {
		return pu.Channel.ID
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

	if pu.Old != nil && pu.New != nil {
		if _, ok := pu.Old.(*ChannelParticipantBanned); ok {
			if _, ok := pu.New.(*ChannelParticipantObj); ok {
				return true
			}
		}
		if _, ok := pu.Old.(*ChannelParticipantLeft); ok {
			if _, ok := pu.New.(*ChannelParticipantObj); ok {
				return true
			}
		}
	} else if pu.Old == nil && pu.New != nil {
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

	if pu.Old != nil && pu.New != nil {
		if _, ok := pu.Old.(*ChannelParticipantLeft); ok {
			if _, ok := pu.New.(*ChannelParticipantObj); ok {
				return true
			}
		}
		if _, ok := pu.Old.(*ChannelParticipantBanned); ok {
			if _, ok := pu.New.(*ChannelParticipantObj); ok {
				return true
			}
		}
	} else if pu.Old == nil && pu.New != nil {
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
	if pu.Old != nil && pu.New != nil {
		if _, ok := pu.Old.(*ChannelParticipantObj); ok {
			if _, ok := pu.New.(*ChannelParticipantAdmin); ok {
				return true
			}
		}
		if _, ok := pu.Old.(*ChannelParticipantBanned); ok {
			if _, ok := pu.New.(*ChannelParticipantAdmin); ok {
				return true
			}
		}
	}
	return false
}

func (pu *ParticipantUpdate) IsDemoted() bool {
	if pu.Old != nil && pu.New != nil {
		if _, ok := pu.Old.(*ChannelParticipantAdmin); ok {
			if _, ok := pu.New.(*ChannelParticipantObj); ok {
				return true
			}
			if _, ok := pu.New.(*ChannelParticipantBanned); ok {
				return true
			}
		}
	}
	return false
}

func (pu *ParticipantUpdate) Marshal(nointent ...bool) string {
	return pu.Client.JSON(pu.OriginalUpdate, nointent)
}

func (pu *ParticipantUpdate) Ban() (bool, error) {
	if pu.User == nil {
		return false, errors.New("ParticipantUpdate.Ban: User is nil")
	}

	if pu.Channel == nil {
		return false, errors.New("ParticipantUpdate.Ban: Channel is nil")
	}

	_, err := pu.Client.EditBanned(pu.Channel, pu.User, &BannedOptions{
		Ban: true,
	})

	return err == nil, err
}

func (pu *ParticipantUpdate) Unban() (bool, error) {
	if pu.User == nil {
		return false, errors.New("ParticipantUpdate.Unban: User is nil")
	}

	if pu.Channel == nil {
		return false, errors.New("ParticipantUpdate.Unban: Channel is nil")
	}

	_, err := pu.Client.EditBanned(pu.Channel, pu.User, &BannedOptions{
		Unban: true,
	})

	return err == nil, err
}

func (pu *ParticipantUpdate) Kick() (bool, error) {
	if pu.User == nil {
		return false, errors.New("ParticipantUpdate.Kick: User is nil")
	}

	if pu.Channel == nil {
		return false, errors.New("ParticipantUpdate.Kick: Channel is nil")
	}

	_, err := pu.Client.KickParticipant(pu.Channel, pu.User)
	return err == nil, err
}

func (pu *ParticipantUpdate) Promote() (bool, error) {
	if pu.User == nil {
		return false, errors.New("ParticipantUpdate.Promote: User is nil")
	}

	if pu.Channel == nil {
		return false, errors.New("ParticipantUpdate.Promote: Channel is nil")
	}

	_, err := pu.Client.EditAdmin(pu.Channel, pu.User, &AdminOptions{
		IsAdmin: true,
	})

	return err == nil, err
}

func (pu *ParticipantUpdate) Demote() (bool, error) {
	if pu.User == nil {
		return false, errors.New("ParticipantUpdate.Demote: User is nil")
	}

	if pu.Channel == nil {
		return false, errors.New("ParticipantUpdate.Demote: Channel is nil")
	}

	_, err := pu.Client.EditAdmin(pu.Channel, pu.User, &AdminOptions{
		IsAdmin: false,
	})

	return err == nil, err
}

type JoinRequestUpdate struct {
	Client         *Client
	OriginalUpdate *UpdatePendingJoinRequests
	Channel        *Channel
	Users          []*UserObj
	PendingCount   int32
}
