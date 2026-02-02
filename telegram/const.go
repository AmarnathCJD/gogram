package telegram

import (
	"regexp"

	"github.com/amarnathcjd/gogram/internal/utils"
)

const (
	ApiVersion = 210
	Version    = "v1.6.0"

	LogDebug   = utils.DebugLevel
	LogInfo    = utils.InfoLevel
	LogWarn    = utils.WarnLevel
	LogError   = utils.ErrorLevel
	LogTrace   = utils.TraceLevel
	LogDisable = utils.NoLevel

	ModeAbridged     = "modeAbridged"
	ModeFull         = "modeFull"
	ModeIntermediate = "modeIntermediate"

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
)

const (
	randombyteLen = 256
	OneTimeMedia  = 2147483647
)

var (
	USERNAME_RE = regexp.MustCompile(`(?i)(?:@|(?:https?:\/\/)?(?:www\.)?(?:telegram\.(?:me|dog)|t\.me)\/)([\w\d_]+)`)
	TG_JOIN_RE  = regexp.MustCompile(`^(?:https?://)?(?:www\.)?t(?:elegram)?\.(?:org|me|dog)/(?:joinchat/|\+)([\w-]+)$`)
)

var (
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
