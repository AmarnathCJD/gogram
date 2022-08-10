package telegram

type (
	SendOptions struct {
		ReplyID     int32
		Caption     interface{}
		ParseMode   string
		Silent      bool
		LinkPreview bool
		ReplyMarkup ReplyMarkup
		ClearDraft  bool
		NoForwards  bool
	}
	Participant struct {
		User        *UserObj
		Admin       bool
		Banned      bool
		Creator     bool
		Left        bool
		Participant ChannelParticipant
	}
)

func (p Participant) IsCreator() bool {
	return p.Creator
}

func (p Participant) IsAdmin() bool {
	return p.Admin
}

func (p Participant) IsBanned() bool {
	return p.Banned
}

func (p Participant) IsLeft() bool {
	return p.Left
}

func (p Participant) GetUser() *UserObj {
	return p.User
}

func (p Participant) GetParticipant() ChannelParticipant {
	return p.Participant
}

func (r ChatAdminRights) CanChangeInfo() bool {
	return r.ChangeInfo
}

func (r ChatAdminRights) CanPostMessages() bool {
	return r.PostMessages
}

func (r ChatAdminRights) CanEditMessages() bool {
	return r.EditMessages
}

func (r ChatAdminRights) CanDeleteMessages() bool {
	return r.DeleteMessages
}

func (r ChatAdminRights) CanBanUsers() bool {
	return r.BanUsers
}

func (r ChatAdminRights) CanInviteUsers() bool {
	return r.InviteUsers
}

func (r ChatAdminRights) CanPinMessages() bool {
	return r.PinMessages
}

func (r ChatAdminRights) CanPromoteMembers() bool {
	return r.AddAdmins
}

func (r ChatAdminRights) IsAnonymous() bool {
	return r.Anonymous
}

func (r ChatAdminRights) CanManageCall() bool {
	return r.ManageCall
}

func (p Participant) GetRank() string {
	return "soon to be implemented"
}
