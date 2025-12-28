package telegram

import (
	"regexp"
)

const (
	ApiVersion = 220
	Version    = "v1.7.0"

	ModeAbridged           = "modeAbridged"
	ModeFull               = "modeFull"
	ModeIntermediate       = "modeIntermediate"
	ModePaddedIntermediate = "modePaddedIntermediate"

	MarkDown string = "Markdown"
	HTML     string = "HTML"

	EntityUser    string = "user"
	EntityChat    string = "chat"
	EntityChannel string = "channel"
	EntityUnknown string = "unknown"

	OnNewMessage          = "OnNewMessage"
	OnEditMessage         = "OnEditMessage"
	OnChatAction          = "OnChatAction"
	OnInlineQuery         = "OnInlineQuery"
	OnCallbackQuery       = "OnCallbackQuery"
	OnInlineCallbackQuery = "OnInlineCallbackQuery"
	OnChosenInlineResult  = "OnChosenInlineResult"
	OnDeleteMessage       = "OnDeleteMessage"

	OneTimeMediaTTL = 2147483647
)

var (
	UsernameRe = regexp.MustCompile(`(?i)(?:@|(?:https?:\/\/)?(?:www\.)?(?:telegram\.(?:me|dog)|t\.me)\/)([\w\d_]+)`)
	TgJoinRe   = regexp.MustCompile(`^(?:https?://)?(?:www\.)?t(?:elegram)?\.(?:org|me|dog)/(?:joinchat/|\+)([\w-]+)$`)

	Actions = map[string]SendMessageAction{
		"typing":          &SendMessageTypingAction{},
		"upload_photo":    &SendMessageUploadPhotoAction{},
		"record_video":    &SendMessageRecordVideoAction{},
		"upload_video":    &SendMessageUploadVideoAction{},
		"record_audio":    &SendMessageRecordAudioAction{},
		"upload_audio":    &SendMessageUploadAudioAction{},
		"upload_document": &SendMessageUploadDocumentAction{},
		"game":            &SendMessageGamePlayAction{},
		"cancel":          &SendMessageCancelAction{},
		"round_video":     &SendMessageUploadRoundAction{},
		"call":            &SpeakingInGroupCallAction{},
		"record_round":    &SendMessageRecordRoundAction{},
		"history_import":  &SendMessageHistoryImportAction{},
		"geo":             &SendMessageGeoLocationAction{},
		"choose_contact":  &SendMessageChooseContactAction{},
		"choose_sticker":  &SendMessageChooseStickerAction{},
		"emoji":           &SendMessageEmojiInteraction{},
		"emoji_seen":      &SendMessageEmojiInteractionSeen{},
	}
)
