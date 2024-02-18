package telegram

import "regexp"

const (
	ApiVersion = 174
	Version    = "v2.3.7"

	LogDebug   = "debug"
	LogInfo    = "info"
	LogWarn    = "warn"
	LogError   = "error"
	LogDisable = "disabled"

	MarkDown   string = "Markdown"
	HTML       string = "HTML"
	MarkDownV2 string = "MarkdownV2"

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
	oneTimeMedia  = 2147483647
)

var (
	USERNAME_RE = regexp.MustCompile(`(?i)@|(?:https?://)?(?:www\.)?(?:telegram\.(?:me|dog)|t\.me)/(@|\+|joinchat/)?`)
	TG_JOIN_RE  = regexp.MustCompile(`(?i)tg://join\?invite=([a-z0-9_\-]{22})`)
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
