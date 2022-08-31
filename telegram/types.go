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

	MediaOptions struct {
		Caption       interface{}
		ParseMode     string
		Silent        bool
		LinkPreview   bool
		ReplyMarkup   ReplyMarkup
		ClearDraft    bool
		NoForwards    bool
		Thumb         InputFile
		NoSoundVideo  bool
		ForceDocument bool
		ReplyID       int32
		FileName      string
		TTL           int32
		Attributes    []DocumentAttribute
	}

	CustomAttrs struct {
		FileName      string
		Thumb         InputFile
		Attributes    []DocumentAttribute
		ForceDocument bool
		TTL           int32
	}

	ForwardOptions struct {
		HideCaption bool
		HideAuthor  bool
		Silent      bool
		Protected   bool
	}

	Participant struct {
		User        *UserObj
		Admin       bool
		Banned      bool
		Creator     bool
		Left        bool
		Participant ChannelParticipant
		Rights      *ChatAdminRights
	}

	ParticipantOptions struct {
		Query  string
		Filter ChannelParticipantsFilter
		Offset int32
		Limit  int32
	}

	ActionResult struct {
		Peer   InputPeer
		Client *Client
	}

	InlineSendOptions struct {
		Gallery      bool
		NextOffset   string
		CacheTime    int32
		Private      bool
		SwitchPm     string
		SwitchPmText string
	}

	ArticleOptions struct {
		Thumb       InputWebDocument
		Content     InputWebDocument
		LinkPreview bool
		ReplyMarkup ReplyMarkup
		Entities    []MessageEntity
		ParseMode   string
		Caption     string
	}
)

var (
	ParticipantsAdmins = &ParticipantOptions{
		Filter: &ChannelParticipantsAdmins{},
		Query:  "",
		Offset: 0,
		Limit:  50,
	}
)

// Cancel the pointed Action,
// Returns true if the action was cancelled
func (a *ActionResult) Cancel() bool {
	b, err := a.Client.MessagesSetTyping(a.Peer, 0, &SendMessageCancelAction{})
	if err != nil {
		return false
	}
	return b
}

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
	if pp, ok := p.Participant.(*ChannelParticipantCreator); ok {
		return pp.Rank
	} else if pp, ok := p.Participant.(*ChannelParticipantAdmin); ok {
		return pp.Rank
	}
	return ""
}
