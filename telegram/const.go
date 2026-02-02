package telegram

import (
	"regexp"
)

const (
	ApiVersion = 223
	Version    = "v1.7.2"

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

	OneTimeMediaTTL = 2147483647
)

var (
	UsernameRe = regexp.MustCompile(`(?i)(?:@|(?:https?:\/\/)?(?:www\.)?(?:telegram\.(?:me|dog)|t\.me)\/)([\w\d_]+)`)
	TgJoinRe   = regexp.MustCompile(`^(?:https?://)?(?:www\.)?t(?:elegram)?\.(?:org|me|dog)/(?:joinchat/|\+)([\w-]+)$`)

	regexFloodWait        = regexp.MustCompile(`Please wait (\d+) seconds before repeating the action`)
	regexFloodWaitBasic   = regexp.MustCompile(`FLOOD_WAIT_(\d+)`)
	regexFloodWaitPremium = regexp.MustCompile(`FLOOD_PREMIUM_WAIT_(\d+)`)
	regexPhone            = regexp.MustCompile(`^\+?[0-9]{10,13}$`)
	proxyURLRegex         = regexp.MustCompile(`^([a-fA-F0-9]+)@([a-zA-Z0-9\.\-]+):(\d+)$`)

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
