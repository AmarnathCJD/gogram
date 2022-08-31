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
		Caption       interface{}         `json:"caption,omitempty"`
		ParseMode     string              `json:"parse_mode,omitempty"`
		Silent        bool                `json:"silent,omitempty"`
		LinkPreview   bool                `json:"link_preview,omitempty"`
		ReplyMarkup   ReplyMarkup         `json:"reply_markup,omitempty"`
		ClearDraft    bool                `json:"clear_draft,omitempty"`
		NoForwards    bool                `json:"no_forwards,omitempty"`
		Thumb         InputFile           `json:"thumb,omitempty"`
		NoSoundVideo  bool                `json:"no_sound_video,omitempty"`
		ForceDocument bool                `json:"force_document,omitempty"`
		ReplyID       int32               `json:"reply_id,omitempty"`
		FileName      string              `json:"file_name,omitempty"`
		TTL           int32               `json:"ttl,omitempty"`
		Attributes    []DocumentAttribute `json:"attributes,omitempty"`
	}

	CustomAttrs struct {
		FileName      string              `json:"file_name,omitempty"`
		Thumb         InputFile           `json:"thumb,omitempty"`
		Attributes    []DocumentAttribute `json:"attributes,omitempty"`
		ForceDocument bool                `json:"force_document,omitempty"`
		TTL           int32               `json:"ttl,omitempty"`
	}

	ForwardOptions struct {
		HideCaption bool `json:"hide_caption,omitempty"`
		HideAuthor  bool `json:"hide_author,omitempty"`
		Silent      bool `json:"silent,omitempty"`
		Protected   bool `json:"protected,omitempty"`
	}

	Participant struct {
		User        *UserObj           `json:"user,omitempty"`
		Admin       bool               `json:"admin,omitempty"`
		Banned      bool               `json:"banned,omitempty"`
		Creator     bool               `json:"creator,omitempty"`
		Left        bool               `json:"left,omitempty"`
		Participant ChannelParticipant `json:"participant,omitempty"`
		Rights      *ChatAdminRights   `json:"rights,omitempty"`
	}

	ParticipantOptions struct {
		Query  string                    `json:"query,omitempty"`
		Filter ChannelParticipantsFilter `json:"filter,omitempty"`
		Offset int32                     `json:"offset,omitempty"`
		Limit  int32                     `json:"limit,omitempty"`
	}

	AdminOptions struct {
		IsAdmin bool             `json:"is_admin,omitempty"`
		Rights  *ChatAdminRights `json:"rights,omitempty"`
		Rank    string           `json:"rank,omitempty"`
	}

	ActionResult struct {
		Peer   InputPeer `json:"peer,omitempty"`
		Client *Client   `json:"client,omitempty"`
	}

	InlineSendOptions struct {
		Gallery      bool   `json:"gallery,omitempty"`
		NextOffset   string `json:"next_offset,omitempty"`
		CacheTime    int32  `json:"cache_time,omitempty"`
		Private      bool   `json:"private,omitempty"`
		SwitchPm     string `json:"switch_pm,omitempty"`
		SwitchPmText string `json:"switch_pm_text,omitempty"`
	}

	ArticleOptions struct {
		Thumb       InputWebDocument `json:"thumb,omitempty"`
		Content     InputWebDocument `json:"content,omitempty"`
		LinkPreview bool             `json:"link_preview,omitempty"`
		ReplyMarkup ReplyMarkup      `json:"reply_markup,omitempty"`
		Entities    []MessageEntity  `json:"entities,omitempty"`
		ParseMode   string           `json:"parse_mode,omitempty"`
		Caption     string           `json:"caption,omitempty"`
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
