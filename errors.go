// Copyright (c) 2024 RoseLoverX

package gogram

import (
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/amarnathcjd/gogram/internal/mtproto/objects"
)

type ErrResponseCode struct {
	Code           int64
	Message        string
	Description    string
	AdditionalInfo any // some errors has additional data like timeout seconds, dc id etc.
}

func RpcErrorToNative(r *objects.RpcError, method ...string) error {
	nativeErrorName, additionalData := TryExpandError(r.ErrorMessage)

	desc, ok := errorMessages[nativeErrorName]
	if !ok {
		desc = nativeErrorName
	}

	if additionalData != nil {
		desc = fmt.Sprintf(desc, additionalData)
	}

	if len(method) > 0 {
		desc = fmt.Sprintf("%s (method: %s)", desc, strings.Join(method, ", "))
	}

	return &ErrResponseCode{
		Code:           int64(r.ErrorCode),
		Message:        nativeErrorName,
		Description:    desc,
		AdditionalInfo: additionalData,
	}
}

type prefixSuffix struct {
	prefix string
	suffix string
	kind   reflect.Kind // int string bool etc.
}

var specificErrors = []prefixSuffix{
	{"EMAIL_UNCONFIRMED_", "", reflect.Int},
	{"FILE_MIGRATE_", "", reflect.Int},
	{"FILE_PART_", "_MISSING", reflect.Int},
	{"FLOOD_TEST_PHONE_WAIT_", "", reflect.Int},
	{"FLOOD_WAIT_", "", reflect.Int},
	{"FLOOD_PREMIUM_WAIT_", "", reflect.Int},
	{"INPUT_FETCH_ERROR_", "", reflect.Int},
	{"INTERDC_", "_CALL_ERROR", reflect.Int},
	{"INTERDC_", "_CALL_RICH_ERROR", reflect.Int},
	{"NETWORK_MIGRATE_", "", reflect.Int},
	{"PASSWORD_TOO_FRESH_", "", reflect.Int},
	{"PHONE_MIGRATE_", "", reflect.Int},
	{"SESSION_TOO_FRESH_", "", reflect.Int},
	{"SLOWMODE_WAIT_", "", reflect.Int},
	{"STATS_MIGRATE_", "", reflect.Int},
	{"TAKEOUT_INIT_DELAY_", "", reflect.Int},
	{"USER_MIGRATE_", "", reflect.Int},
	{"PREVIOUS_CHAT_IMPORT_ACTIVE_WAIT_", "MIN", reflect.Int},
	{"PREMIUM_SUB_ACTIVE_UNTIL_X", "", reflect.Int},
	{"STORY_SEND_FLOOD_MONTHLY_X", "", reflect.Int},
	{"STORY_SEND_FLOOD_WEEKLY_X", "", reflect.Int},
}

func TryExpandError(errStr string) (nativeErrorName string, additionalData any) {
	var chosenPrefixSuffix *prefixSuffix

	for _, errCase := range specificErrors {
		if strings.HasPrefix(errStr, errCase.prefix) && strings.HasSuffix(errStr, errCase.suffix) {
			errCase := errCase
			chosenPrefixSuffix = &errCase
			break
		}
	}

	if chosenPrefixSuffix == nil {
		return errStr, nil // common error, returning
	}

	nativeErrorName = chosenPrefixSuffix.prefix + "X" + chosenPrefixSuffix.suffix
	trimmedData := strings.TrimSuffix(strings.TrimPrefix(errStr, chosenPrefixSuffix.prefix), chosenPrefixSuffix.suffix)

	switch v := chosenPrefixSuffix.kind; v {
	case reflect.Int:
		var err error
		additionalData, err = strconv.Atoi(trimmedData)
		if err != nil {
			log.Printf("failed to parse %s as int: %s", trimmedData, err.Error())
		}

	case reflect.String:
		additionalData = trimmedData

	default:
		panic("couldn't parse this type: " + v.String())
	}

	return nativeErrorName, additionalData
}

func (e *ErrResponseCode) Error() string {
	return fmt.Sprintf("[%s] %s (code %d)", e.Message, e.Description, e.Code)
}

// gathered all errors from all methods. don't have reference in docs at all
var errorMessages = map[string]string{
	"ABOUT_TOO_LONG":                      "About string too long.",
	"ACCESS_TOKEN_EXPIRED":                "Access token expired.",
	"ACCESS_TOKEN_INVALID":                "Access token invalid.",
	"ACTIVE_USER_REQUIRED":                "The method is only available to already activated users.",
	"ADDRESS_INVALID":                     "The specified geopoint address is invalid.",
	"ADMINS_TOO_MUCH":                     "There are too many admins.",
	"ADMIN_ID_INVALID":                    "The specified admin ID is invalid.",
	"ADMIN_RANK_EMOJI_NOT_ALLOWED":        "An admin rank cannot contain emojis.",
	"ADMIN_RANK_INVALID":                  "The specified admin rank is invalid.",
	"ADMIN_RIGHTS_EMPTY":                  "The admin rights configuration has no rights set.",
	"ALBUM_PHOTOS_TOO_MANY":               "You have uploaded too many profile photos, delete some before retrying.",
	"ANONYMOUS_REACTIONS_DISABLED":        "Sorry, anonymous administrators cannot leave reactions or participate in polls.",
	"API_ID_INVALID":                      "API ID invalid.",
	"API_ID_PUBLISHED_FLOOD":              "This API ID was published somewhere, you can't use it now.",
	"ARTICLE_TITLE_EMPTY":                 "The title of the article is empty.",
	"AUDIO_CONTENT_URL_EMPTY":             "The remote URL specified in the content field is empty.",
	"AUDIO_TITLE_EMPTY":                   "An empty audio title was provided.",
	"AUTH_BYTES_INVALID":                  "The provided authorization is invalid.",
	"AUTH_KEY_DUPLICATED":                 "The authorization key was used under two different IP addresses simultaneously and is now invalid.",
	"AUTH_KEY_INVALID":                    "The Authorization Key is invalid.",
	"AUTH_KEY_PERM_EMPTY":                 "The method is unavailable for temporary authorization keys, not bound to permanent.",
	"AUTH_KEY_UNREGISTERED":               "The key is not registered in the system.",
	"AUTH_RESTART":                        "Restart the authorization process.",
	"AUTH_TOKEN_ALREADY_ACCEPTED":         "The specified auth token was already accepted.",
	"AUTH_TOKEN_EXCEPTION":                "An error occurred while importing the auth token.",
	"AUTH_TOKEN_EXPIRED":                  "The authorization token has expired.",
	"AUTH_TOKEN_INVALID":                  "The specified auth token is invalid.",
	"AUTH_TOKEN_INVALID2":                 "An invalid authorization token was provided.",
	"AUTH_TOKEN_INVALIDX":                 "The specified auth token is invalid.",
	"AUTOARCHIVE_NOT_AVAILABLE":           "The autoarchive setting is not available at this time; please check the client configuration.",
	"BANK_CARD_NUMBER_INVALID":            "The specified card number is invalid.",
	"BANNED_RIGHTS_INVALID":               "You provided some invalid flags in the banned rights.",
	"BASE_PORT_LOC_INVALID":               "Base port location invalid.",
	"BOOST_NOT_MODIFIED":                  "You are already boosting the specified channel.",
	"BOOST_PEER_INVALID":                  "The specified boost_peer is invalid.",
	"BOOSTS_EMPTY":                        "No boost slots were specified.",
	"BOOSTS_REQUIRED":                     "The specified channel must first be boosted by its users in order to perform this action.",
	"BOT_APP_INVALID":                     "The specified bot app is invalid.",
	"BOT_APP_SHORTNAME_INVALID":           "The specified bot app short name is invalid.",
	"BOT_CHANNELS_NA":                     "Bots can't edit admin privileges.",
	"BOT_COMMANDS_TOO_MUCH":               "The provided commands are too many.",
	"BOT_COMMAND_DESCRIPTION_INVALID":     "The specified command description is invalid.",
	"BOT_COMMAND_INVALID":                 "The specified command is invalid.",
	"BOT_DOMAIN_INVALID":                  "Bot domain invalid.",
	"BOT_GAMES_DISABLED":                  "Bot games cannot be used in this type of chat.",
	"BOT_GROUPS_BLOCKED":                  "This bot can't be added to groups.",
	"BOT_INLINE_DISABLED":                 "This bot can't be used in inline mode.",
	"BOT_INVALID":                         "This is not a valid bot.",
	"BOT_METHOD_INVALID":                  "The API access for bot users is restricted. This method cannot be executed as a bot.",
	"BOT_MISSING":                         "Only bots can call this method.",
	"BOT_ONESIDE_NOT_AVAIL":               "Bots can't pin messages in PM just for themselves.",
	"BOT_PAYMENTS_DISABLED":               "Please enable bot payments in BotFather before calling this method.",
	"BOT_POLLS_DISABLED":                  "You cannot create polls under a bot account.",
	"BOT_RESPONSE_TIMEOUT":                "A timeout occurred while fetching data from the bot.",
	"BOT_SCORE_NOT_MODIFIED":              "The score wasn't modified.",
	"BOT_WEBVIEW_DISABLED":                "A webview cannot be opened in the specified conditions.",
	"BOTS_TOO_MUCH":                       "There are too many bots in this chat/channel.",
	"BROADCAST_CALLS_DISABLED":            "Broadcast calls are disabled for this chat/channel.",
	"BROADCAST_FORBIDDEN":                 "Channel poll voters and reactions cannot be fetched to prevent deanonymization.",
	"BROADCAST_ID_INVALID":                "Broadcast ID invalid.",
	"BROADCAST_PUBLIC_VOTERS_FORBIDDEN":   "You can't forward polls with public voters.",
	"BROADCAST_REQUIRED":                  "This method can only be called on a channel.",
	"BUTTON_DATA_INVALID":                 "The data of one or more of the buttons you provided is invalid.",
	"BUTTON_TEXT_INVALID":                 "The specified button text is invalid.",
	"BUTTON_TYPE_INVALID":                 "The type of one or more of the buttons you provided is invalid.",
	"BUTTON_URL_INVALID":                  "Button URL invalid.",
	"BUTTON_USER_INVALID":                 "The user_id passed to the button is invalid.",
	"BUTTON_USER_PRIVACY_RESTRICTED":      "The privacy setting of the user specified in the button do not allow creating such a button.",
	"CALL_ALREADY_ACCEPTED":               "The call was already accepted.",
	"CALL_ALREADY_DECLINED":               "The call was already declined.",
	"CALL_OCCUPY_FAILED":                  "The call failed because the user is already making another call.",
	"CALL_PEER_INVALID":                   "The provided call peer object is invalid.",
	"CALL_PROTOCOL_COMPAT_LAYER_INVALID":  "The other side of the call does not support any of the VoIP protocols supported by the local client.",
	"CALL_PROTOCOL_FLAGS_INVALID":         "Call protocol flags invalid.",
	"CDN_METHOD_INVALID":                  "You can't call this method in a CDN DC.",
	"CDN_UPLOAD_TIMEOUT":                  "A server-side timeout occurred while reuploading the file to the CDN DC.",
	"CHANNELS_ADMIN_LOCATED_TOO_MUCH":     "The user has reached the limit of public geogroups.",
	"CHANNELS_ADMIN_PUBLIC_TOO_MUCH":      "You're admin of too many public channels, make some channels private to change the username of this channel.",
	"CHANNELS_TOO_MUCH":                   "You have joined too many channels/supergroups.",
	"CHANNEL_ADD_INVALID":                 "The specified channel is invalid.",
	"CHANNEL_BANNED":                      "The channel is banned.",
	"CHANNEL_FORUM_MISSING":               "This supergroup is not a forum.",
	"CHANNEL_ID_INVALID":                  "The specified supergroup ID is invalid.",
	"CHANNEL_INVALID":                     "The provided channel is invalid.",
	"CHANNEL_PARTICIPANT_MISSING":         "The current user is not in the channel.",
	"CHANNEL_PRIVATE":                     "You haven't joined this channel/supergroup.",
	"CHANNEL_PUBLIC_GROUP_NA":             "Channel/supergroup not available.",
	"CHANNEL_TOO_BIG":                     "This channel has too many participants (>1000) to be deleted.",
	"CHANNEL_TOO_LARGE":                   "Channel is too large to be deleted.",
	"CHATLIST_EXCLUDE_INVALID":            "The specified exclude_peers are invalid.",
	"CHAT_ABOUT_NOT_MODIFIED":             "About text has not changed.",
	"CHAT_ABOUT_TOO_LONG":                 "Chat about too long.",
	"CHAT_ADMIN_INVITE_REQUIRED":          "You do not have the rights to do this.",
	"CHAT_ADMIN_REQUIRED":                 "You must be an admin in this chat to do this.",
	"CHAT_DISCUSSION_UNALLOWED":           "You can't enable forum topics in a discussion group linked to a channel.",
	"CHAT_FORBIDDEN":                      "You cannot write in this chat.",
	"CHAT_FORWARDS_RESTRICTED":            "You can't forward messages from a protected chat.",
	"CHAT_GET_FAILED":                     "Chat retrieval failed.",
	"CHAT_GUEST_SEND_FORBIDDEN":           "You must join the discussion group before commenting.",
	"CHAT_ID_EMPTY":                       "The provided chat ID is empty.",
	"CHAT_ID_GENERATE_FAILED":             "Failure while generating the chat ID.",
	"CHAT_ID_INVALID":                     "The provided chat id is invalid.",
	"CHAT_INVALID":                        "Invalid chat.",
	"CHAT_INVITE_PERMANENT":               "You can't set an expiration date on permanent invite links.",
	"CHAT_LINK_EXISTS":                    "The chat is public, you can't hide the history to new users.",
	"CHAT_NOT_MODIFIED":                   "No changes were made to chat information because the new information is identical to the current information.",
	"CHAT_PUBLIC_REQUIRED":                "You can only enable join requests in public groups.",
	"CHAT_RESTRICTED":                     "You can't send messages in this chat, you were restricted.",
	"CHAT_REVOKE_DATE_UNSUPPORTED":        "Date restrictions are not available for using with non-user peers.",
	"CHAT_SEND_AUDIOS_FORBIDDEN":          "You can't send audio messages in this chat.",
	"CHAT_SEND_DOCS_FORBIDDEN":            "You can't send documents in this chat.",
	"CHAT_SEND_GAME_FORBIDDEN":            "You can't send a game to this chat.",
	"CHAT_SEND_GIFS_FORBIDDEN":            "You can't send gifs in this chat.",
	"CHAT_SEND_INLINE_FORBIDDEN":          "You can't send inline messages in this group.",
	"CHAT_SEND_MEDIA_FORBIDDEN":           "You can't send media in this chat.",
	"CHAT_SEND_PHOTOS_FORBIDDEN":          "You can't send photos in this chat.",
	"CHAT_SEND_PLAIN_FORBIDDEN":           "You can't send non-media (text) messages in this chat.",
	"CHAT_SEND_POLL_FORBIDDEN":            "You can't send polls in this chat.",
	"CHAT_SEND_STICKERS_FORBIDDEN":        "You can't send stickers in this chat.",
	"CHAT_SEND_VIDEOS_FORBIDDEN":          "You can't send videos in this chat.",
	"CHAT_SEND_VOICES_FORBIDDEN":          "You can't send voice recordings in this chat.",
	"CHAT_TITLE_EMPTY":                    "No chat title provided.",
	"CHAT_TOO_BIG":                        "This method is not available for groups that are too big.",
	"CHAT_WRITE_FORBIDDEN":                "You can't write in this chat.",
	"CHP_CALL_FAIL":                       "The statistics cannot be retrieved at this time.",
	"CODE_EMPTY":                          "The provided code is empty.",
	"CODE_HASH_INVALID":                   "Code hash invalid.",
	"CODE_INVALID":                        "Code invalid.",
	"COLOR_INVALID":                       "The specified color palette ID was invalid.",
	"CONNECTION_API_ID_INVALID":           "The provided API id is invalid.",
	"CONNECTION_APP_VERSION_EMPTY":        "App version is empty.",
	"CONNECTION_DEVICE_MODEL_EMPTY":       "Device model empty.",
	"CONNECTION_LANG_PACK_INVALID":        "The specified language pack is not valid.",
	"CONNECTION_LAYER_INVALID":            "Layer invalid.",
	"CONNECTION_NOT_INITED":               "Connection not initialized.",
	"CONNECTION_SYSTEM_EMPTY":             "Connection system empty.",
	"CONNECTION_SYSTEM_LANG_CODE_EMPTY":   "The system language string was empty during connection.",
	"CONTACT_ADD_MISSING":                 "Contact to add is missing.",
	"CONTACT_ID_INVALID":                  "The provided contact ID is invalid.",
	"CONTACT_MISSING":                     "The specified user is not a contact.",
	"CONTACT_NAME_EMPTY":                  "Contact name empty.",
	"CONTACT_REQ_MISSING":                 "Missing contact request.",
	"CREATE_CALL_FAILED":                  "An error occurred while creating the call.",
	"CURRENCY_TOTAL_AMOUNT_INVALID":       "The total amount of all prices is invalid.",
	"CUSTOM_REACTIONS_TOO_MANY":           "Too many custom reactions were specified.",
	"DATA_INVALID":                        "Encrypted data invalid.",
	"DATA_JSON_INVALID":                   "The provided JSON data is invalid.",
	"DATA_TOO_LONG":                       "Data too long.",
	"DATE_EMPTY":                          "Date empty.",
	"DC_ID_INVALID":                       "The provided DC ID is invalid.",
	"DH_G_A_INVALID":                      "g_a invalid.",
	"DOCUMENT_INVALID":                    "The specified document is invalid.",
	"EDIT_BOT_INVITE_FORBIDDEN":           "Normal users can't edit invites that were created by bots.",
	"EMAIL_HASH_EXPIRED":                  "Email hash expired.",
	"EMAIL_INVALID":                       "The specified email is invalid.",
	"EMAIL_NOT_SETUP":                     "Login email not set up.",
	"EMAIL_UNCONFIRMED":                   "Email unconfirmed.",
	"EMAIL_VERIFY_EXPIRED":                "The verification email has expired.",
	"EMOJI_INVALID":                       "The specified theme emoji is invalid.",
	"EMOJI_MARKUP_INVALID":                "The specified video_emoji_markup was invalid.",
	"EMOJI_NOT_MODIFIED":                  "The theme wasn't changed.",
	"EMOTICON_EMPTY":                      "The emoji is empty.",
	"EMOTICON_INVALID":                    "The specified emoji is invalid.",
	"EMOTICON_STICKERPACK_MISSING":        "The emoji cannot be empty.",
	"ENCRYPTED_MESSAGE_INVALID":           "Encrypted message invalid.",
	"ENCRYPTION_ALREADY_ACCEPTED":         "Secret chat already accepted.",
	"ENCRYPTION_ALREADY_DECLINED":         "The secret chat was already declined.",
	"ENCRYPTION_DECLINED":                 "The secret chat was declined.",
	"ENCRYPTION_ID_INVALID":               "The provided secret chat ID is invalid.",
	"ENCRYPTION_OCCUPY_FAILED":            "Internal server error while accepting secret chat.",
	"ENTITIES_TOO_LONG":                   "You provided too many styled message entities.",
	"ENTITY_BOUNDS_INVALID":               "A specified entity offset or length is invalid.",
	"ENTITY_MENTION_USER_INVALID":         "You mentioned an invalid user.",
	"ERROR_TEXT_EMPTY":                    "The provided error message is empty.",
	"EXPIRE_DATE_INVALID":                 "The specified expiration date is invalid.",
	"EXPIRE_FORBIDDEN":                    "The provided expire date is forbidden.",
	"EXPORT_CARD_INVALID":                 "Provided card is invalid.",
	"EXTERNAL_URL_INVALID":                "External URL invalid.",
	"FIELD_NAME_EMPTY":                    "A required field is missing.",
	"FIELD_NAME_INVALID":                  "A provided field is invalid.",
	"FILEREF_UPGRADE_NEEDED":              "The client has to be updated in order to support file references.",
	"FILE_CONTENT_TYPE_INVALID":           "File content-type is invalid.",
	"FILE_EMPTY":                          "An empty file was provided.",
	"FILE_EMTPY":                          "An empty file was provided.",
	"FILE_ID_INVALID":                     "The provided file id is invalid.",
	"FILE_PARTS_INVALID":                  "The number of file parts is invalid.",
	"FILE_PART_EMPTY":                     "The provided file part is empty.",
	"FILE_PART_INVALID":                   "The file part number is invalid.",
	"FILE_PART_LENGTH_INVALID":            "The length of a file part is invalid.",
	"FILE_PART_SIZE_CHANGED":              "Provided file part size has changed.",
	"FILE_PART_SIZE_INVALID":              "The provided file part size is invalid.",
	"FILE_PART_TOO_BIG":                   "The uploaded file part is too big.",
	"FILE_REFERENCE_EMPTY":                "An empty file reference was specified.",
	"FILE_REFERENCE_EXPIRED":              "File reference expired, it must be refetched.",
	"FILE_REFERENCE_INVALID":              "The specified file reference is invalid.",
	"FILE_TITLE_EMPTY":                    "An empty file title was specified.",
	"FILE_TOKEN_INVALID":                  "The specified file token is invalid.",
	"FILTER_ID_INVALID":                   "The specified filter ID is invalid.",
	"FILTER_INCLUDE_EMPTY":                "The include_peers vector of the filter is empty.",
	"FILTER_NOT_SUPPORTED":                "The specified filter cannot be used in this context.",
	"FILTER_TITLE_EMPTY":                  "The title field of the filter is empty.",
	"FIRSTNAME_INVALID":                   "The first name is invalid.",
	"FOLDER_ID_EMPTY":                     "An empty folder ID was specified.",
	"FOLDER_ID_INVALID":                   "Invalid folder ID.",
	"FORUM_ENABLED":                       "You can't execute the specified action because the group is a forum; disable forum functionality to continue.",
	"FRESH_CHANGE_ADMINS_FORBIDDEN":       "You were just elected admin, you can't add or modify other admins yet.",
	"FRESH_CHANGE_PHONE_FORBIDDEN":        "You can't change phone number right after logging in, please wait at least 24 hours.",
	"FRESH_RESET_AUTHORISATION_FORBIDDEN": "You can't logout other sessions if less than 24 hours have passed since you logged on the current session.",
	"FROM_MESSAGE_BOT_DISABLED":           "Bots can't use fromMessage min constructors.",
	"FROM_PEER_INVALID":                   "The specified from_id is invalid.",
	"FROZEN_METHOD_INVALID":               "You tried to use a method that is not available for frozen accounts.",
	"FROZEN_PARTICIPANT_MISSING":          "Your account is frozen and can't access the chat.",
	"GAME_BOT_INVALID":                    "Bots can't send another bot's game.",
	"GENERAL_MODIFY_ICON_FORBIDDEN":       "You can't modify the icon of the General topic.",
	"GEO_POINT_INVALID":                   "Invalid geoposition provided.",
	"GIFT_SLUG_EXPIRED":                   "The specified gift slug has expired.",
	"GIFT_SLUG_INVALID":                   "The specified slug is invalid.",
	"GIF_CONTENT_TYPE_INVALID":            "GIF content-type invalid.",
	"GIF_ID_INVALID":                      "The provided GIF ID is invalid.",
	"GRAPH_EXPIRED_RELOAD":                "This graph has expired, please obtain a new graph token.",
	"GRAPH_INVALID_RELOAD":                "Invalid graph token provided, please reload the stats and provide the updated token.",
	"GRAPH_OUTDATED_RELOAD":               "The graph is outdated, please get a new async token.",
	"GROUPCALL_ADD_PARTICIPANTS_FAILED":   "Failed to add participants to the group call.",
	"GROUPCALL_ALREADY_DISCARDED":         "The group call was already discarded.",
	"GROUPCALL_ALREADY_STARTED":           "The groupcall has already started, you can join directly.",
	"GROUPCALL_FORBIDDEN":                 "The group call has already ended.",
	"GROUPCALL_INVALID":                   "The specified group call is invalid.",
	"GROUPCALL_JOIN_MISSING":              "You haven't joined this group call.",
	"GROUPCALL_NOT_MODIFIED":              "Group call settings weren't modified.",
	"GROUPCALL_SSRC_DUPLICATE_MUCH":       "The app needs to retry joining the group call with a new SSRC value.",
	"GROUPED_MEDIA_INVALID":               "Invalid grouped media.",
	"GROUP_CALL_INVALID":                  "Group call invalid.",
	"HASH_INVALID":                        "The provided hash is invalid.",
	"HIDE_REQUESTER_MISSING":              "The join request was missing or was already handled.",
	"HISTORY_GET_FAILED":                  "Fetching of history failed.",
	"IMAGE_PROCESS_FAILED":                "Failure while processing image.",
	"IMPORT_FILE_INVALID":                 "The specified chat export file is invalid.",
	"IMPORT_FORMAT_UNRECOGNIZED":          "The specified chat export file was exported from an unsupported chat app.",
	"IMPORT_ID_INVALID":                   "The specified import ID is invalid.",
	"IMPORT_TOKEN_INVALID":                "The specified token is invalid.",
	"INLINE_BOT_REQUIRED":                 "Only the inline bot can edit message.",
	"INLINE_RESULT_EXPIRED":               "The inline query expired.",
	"INPUT_CHATLIST_INVALID":              "The specified folder is invalid.",
	"INPUT_CONSTRUCTOR_INVALID":           "The provided constructor is invalid.",
	"INPUT_FETCH_ERROR":                   "An error occurred while deserializing TL parameters.",
	"INPUT_FETCH_FAIL":                    "Failed deserializing TL payload.",
	"INPUT_FILTER_INVALID":                "The specified filter is invalid.",
	"INPUT_LAYER_INVALID":                 "The provided layer is invalid.",
	"INPUT_METHOD_INVALID":                "The specified method is invalid.",
	"INPUT_REQUEST_TOO_LONG":              "The input request was too long.",
	"INPUT_TEXT_EMPTY":                    "The specified text is empty.",
	"INPUT_TEXT_TOO_LONG":                 "The specified text is too long.",
	"INPUT_USER_DEACTIVATED":              "The specified user was deleted.",
	"INVITES_TOO_MUCH":                    "The maximum number of per-folder invites was reached.",
	"INVITE_FORBIDDEN_WITH_JOINAS":        "You cannot invite users while anonymously joined as a channel.",
	"INVITE_HASH_EMPTY":                   "The invite hash is empty.",
	"INVITE_HASH_EXPIRED":                 "The invite link has expired.",
	"INVITE_HASH_INVALID":                 "The invite hash is invalid.",
	"INVITE_REQUEST_SENT":                 "You have successfully requested to join this chat or channel.",
	"INVITE_REVOKED_MISSING":              "The specified invite link was already revoked or is invalid.",
	"INVITE_SLUG_EMPTY":                   "The specified invite slug is empty.",
	"INVITE_SLUG_EXPIRED":                 "The specified chat folder link has expired.",
	"INVOICE_PAYLOAD_INVALID":             "The specified invoice payload is invalid.",
	"JOIN_AS_PEER_INVALID":                "The specified peer cannot be used to join a group call.",
	"LANG_CODE_INVALID":                   "The specified language code is invalid.",
	"LANG_CODE_NOT_SUPPORTED":             "The specified language code is not supported.",
	"LANG_PACK_INVALID":                   "The provided language pack is invalid.",
	"LASTNAME_INVALID":                    "The last name is invalid.",
	"LIMIT_INVALID":                       "The provided limit is invalid.",
	"LINK_NOT_MODIFIED":                   "Discussion link not modified.",
	"LOCATION_INVALID":                    "The provided location is invalid.",
	"MAX_DATE_INVALID":                    "The specified maximum date is invalid.",
	"MAX_ID_INVALID":                      "The provided max ID is invalid.",
	"MAX_QTS_INVALID":                     "The specified max_qts is invalid.",
	"MD5_CHECKSUM_INVALID":                "The MD5 checksums do not match.",
	"MEDIA_CAPTION_TOO_LONG":              "The caption is too long.",
	"MEDIA_EMPTY":                         "The provided media object is invalid.",
	"MEDIA_FILE_INVALID":                  "The specified media file is invalid.",
	"MEDIA_GROUPED_INVALID":               "You tried to send media of different types in an album.",
	"MEDIA_INVALID":                       "Media invalid.",
	"MEDIA_NEW_INVALID":                   "The new media is invalid.",
	"MEDIA_PREV_INVALID":                  "Previous media invalid.",
	"MEDIA_TTL_INVALID":                   "The specified media TTL is invalid.",
	"MEDIA_TYPE_INVALID":                  "The specified media type cannot be used in stories.",
	"MEDIA_VIDEO_STORY_MISSING":           "A non-story video cannot be republished as a story.",
	"MEGAGROUP_GEO_REQUIRED":              "This method can only be invoked on a geogroup.",
	"MEGAGROUP_ID_INVALID":                "Invalid supergroup ID.",
	"MEGAGROUP_PREHISTORY_HIDDEN":         "Group with hidden history for new members can't be set as discussion groups.",
	"MEGAGROUP_REQUIRED":                  "You can only use this method on a supergroup.",
	"MEMBER_NO_LOCATION":                  "An internal failure occurred while fetching user info (couldn't find location).",
	"MEMBER_OCCUPY_PRIMARY_LOC_FAILED":    "Occupation of primary member location failed.",
	"MESSAGE_AUTHOR_REQUIRED":             "Message author required.",
	"MESSAGE_DELETE_FORBIDDEN":            "You can't delete one of the messages you tried to delete, most likely because it is a service message.",
	"MESSAGE_EDIT_TIME_EXPIRED":           "You can't edit this message anymore, too much time has passed since its creation.",
	"MESSAGE_EMPTY":                       "The provided message is empty.",
	"MESSAGE_IDS_EMPTY":                   "No message ids were provided.",
	"MESSAGE_ID_INVALID":                  "The provided message id is invalid.",
	"MESSAGE_NOT_MODIFIED":                "The provided message data is identical to the previous message data, the message wasn't modified.",
	"MESSAGE_POLL_CLOSED":                 "Poll closed.",
	"MESSAGE_TOO_LONG":                    "The provided message is too long.",
	"METHOD_INVALID":                      "The specified method is invalid.",
	"MIN_DATE_INVALID":                    "The specified minimum date is invalid.",
	"MSGID_DECREASE_RETRY":                "The request should be retried with a lower message ID.",
	"MSG_ID_INVALID":                      "Invalid message ID provided.",
	"MSG_TOO_OLD":                         "Time has passed since the message was sent, read receipts were deleted.",
	"MSG_WAIT_FAILED":                     "A waiting call returned an error.",
	"MT_SEND_QUEUE_TOO_LONG":              "The message was not sent because the send queue is too long.",
	"MULTI_MEDIA_TOO_LONG":                "Too many media files for album.",
	"NEED_CHAT_INVALID":                   "The provided chat is invalid.",
	"NEED_MEMBER_INVALID":                 "The provided member is invalid or does not exist.",
	"NEW_SALT_INVALID":                    "The new salt is invalid.",
	"NEW_SETTINGS_EMPTY":                  "No password is set on the current account, and no new password was specified in new_settings.",
	"NEW_SETTINGS_INVALID":                "The new password settings are invalid.",
	"NEXT_OFFSET_INVALID":                 "The specified offset is longer than 64 bytes.",
	"NOT_ALLOWED":                         "Action not allowed.",
	"OFFSET_INVALID":                      "The provided offset is invalid.",
	"OFFSET_PEER_ID_INVALID":              "The provided offset peer is invalid.",
	"OPTIONS_TOO_MUCH":                    "Too many options provided.",
	"OPTION_INVALID":                      "Invalid option selected.",
	"ORDER_INVALID":                       "The specified username order is invalid.",
	"PACK_SHORT_NAME_INVALID":             "Short pack name invalid.",
	"PACK_SHORT_NAME_OCCUPIED":            "A stickerpack with this name already exists.",
	"PACK_TITLE_INVALID":                  "The stickerpack title is invalid.",
	"PARTICIPANTS_TOO_FEW":                "Not enough participants.",
	"PARTICIPANT_CALL_FAILED":             "Failure while making call.",
	"PARTICIPANT_ID_INVALID":              "The specified participant ID is invalid.",
	"PARTICIPANT_JOIN_MISSING":            "User must join the Video Chat before enabling presentation.",
	"PARTICIPANT_VERSION_OUTDATED":        "The other participant does not use an up to date telegram client with support for calls.",
	"PASSWORD_EMPTY":                      "The provided password is empty.",
	"PASSWORD_HASH_INVALID":               "The provided password hash is invalid.",
	"PASSWORD_MISSING":                    "You must enable 2FA in order to transfer ownership of a channel.",
	"PASSWORD_RECOVERY_EXPIRED":           "The recovery code has expired.",
	"PASSWORD_RECOVERY_NA":                "No email was set, can't recover password via email.",
	"PASSWORD_REQUIRED":                   "A 2FA password must be configured to use Telegram Passport.",
	"PAYMENT_PROVIDER_INVALID":            "The specified payment provider is invalid.",
	"PAYMENT_UNSUPPORTED":                 "This payment method is not acceptable.",
	"PEERS_LIST_EMPTY":                    "The specified list of peers is empty.",
	"PEER_FLOOD":                          "Too many requests.",
	"PEER_HISTORY_EMPTY":                  "You can't pin an empty chat with a user.",
	"PEER_ID_INVALID":                     "The provided peer id is invalid.",
	"PEER_ID_NOT_SUPPORTED":               "The provided peer ID is not supported.",
	"PERSISTENT_TIMESTAMP_EMPTY":          "Persistent timestamp empty.",
	"PERSISTENT_TIMESTAMP_INVALID":        "Persistent timestamp invalid.",
	"PERSISTENT_TIMESTAMP_OUTDATED":       "Channel internal replication issues, try again later.",
	"PHONE_CODE_EMPTY":                    "phone_code is missing.",
	"PHONE_CODE_EXPIRED":                  "The phone code you provided has expired.",
	"PHONE_CODE_HASH_EMPTY":               "phone_code_hash is missing.",
	"PHONE_CODE_INVALID":                  "The provided phone code is invalid.",
	"PHONE_HASH_EXPIRED":                  "An invalid or expired phone_code_hash was provided.",
	"PHONE_NOT_OCCUPIED":                  "No user is associated to the specified phone number.",
	"PHONE_NUMBER_APP_SIGNUP_FORBIDDEN":   "You can't sign up using this app.",
	"PHONE_NUMBER_BANNED":                 "The provided phone number is banned from telegram.",
	"PHONE_NUMBER_FLOOD":                  "You asked for the code too many times.",
	"PHONE_NUMBER_INVALID":                "The phone number is invalid.",
	"PHONE_NUMBER_OCCUPIED":               "The phone number is already in use.",
	"PHONE_NUMBER_UNOCCUPIED":             "The phone number is not yet being used.",
	"PHONE_PASSWORD_FLOOD":                "You have tried logging in too many times.",
	"PHONE_PASSWORD_PROTECTED":            "This phone is password protected.",
	"PHOTO_CONTENT_TYPE_INVALID":          "Photo mime-type invalid.",
	"PHOTO_CONTENT_URL_EMPTY":             "Photo URL invalid.",
	"PHOTO_CROP_FILE_MISSING":             "Photo crop file missing.",
	"PHOTO_CROP_SIZE_SMALL":               "Photo is too small.",
	"PHOTO_EXT_INVALID":                   "The extension of the photo is invalid.",
	"PHOTO_FILE_MISSING":                  "Profile photo file missing.",
	"PHOTO_ID_INVALID":                    "Photo ID invalid.",
	"PHOTO_INVALID":                       "Photo invalid.",
	"PHOTO_INVALID_DIMENSIONS":            "The photo dimensions are invalid.",
	"PHOTO_SAVE_FILE_INVALID":             "Internal issues, try again later.",
	"PHOTO_THUMB_URL_EMPTY":               "Photo thumbnail URL is empty.",
	"PINNED_DIALOGS_TOO_MUCH":             "Too many pinned dialogs.",
	"PIN_RESTRICTED":                      "You can't pin messages.",
	"POLL_ANSWERS_INVALID":                "Invalid poll answers were provided.",
	"POLL_ANSWER_INVALID":                 "One of the poll answers is not acceptable.",
	"POLL_OPTION_DUPLICATE":               "Duplicate poll options provided.",
	"POLL_OPTION_INVALID":                 "Invalid poll option provided.",
	"POLL_QUESTION_INVALID":               "One of the poll questions is not acceptable.",
	"POLL_UNSUPPORTED":                    "This layer does not support polls in the issued method.",
	"POLL_VOTE_REQUIRED":                  "Cast a vote in the poll before calling this method.",
	"POSTPONED_TIMEOUT":                   "An internal timeout occurred with the Telegram server, try again later.",
	"PREMIUM_ACCOUNT_REQUIRED":            "A premium account is required to execute this action.",
	"PREMIUM_CURRENTLY_UNAVAILABLE":       "Premium is currently unavailable.",
	"PRIVACY_KEY_INVALID":                 "The privacy key is invalid.",
	"PRIVACY_PREMIUM_REQUIRED":            "You need a Telegram Premium subscription to send a message to this user.",
	"PRIVACY_TOO_LONG":                    "Too many privacy rules were specified, the current limit is 1000.",
	"PRIVACY_VALUE_INVALID":               "The specified privacy rule combination is invalid.",
	"PTS_CHANGE_EMPTY":                    "No PTS change.",
	"PUBLIC_CHANNEL_MISSING":              "You can only export group call invite links for public chats or channels.",
	"PUBLIC_KEY_REQUIRED":                 "A public key is required.",
	"QUERY_ID_EMPTY":                      "The query ID is empty.",
	"QUERY_ID_INVALID":                    "The query ID is invalid.",
	"QUERY_TOO_SHORT":                     "The query string is too short.",
	"QUIZ_ANSWER_MISSING":                 "You can forward a quiz while hiding the original author only after choosing an option in the quiz.",
	"QUIZ_CORRECT_ANSWERS_EMPTY":          "No correct quiz answer was specified.",
	"QUIZ_CORRECT_ANSWERS_TOO_MUCH":       "You specified too many correct answers in a quiz, quizzes can only have one right answer!",
	"QUIZ_CORRECT_ANSWER_INVALID":         "An invalid value was provided to the correct_answers field.",
	"QUIZ_MULTIPLE_INVALID":               "Quizzes can't have the multiple_choice flag set!",
	"RANDOM_ID_DUPLICATE":                 "You provided a random ID that was already used.",
	"RANDOM_ID_EMPTY":                     "Random ID empty.",
	"RANDOM_ID_INVALID":                   "A provided random ID is invalid.",
	"RANDOM_LENGTH_INVALID":               "Random length invalid.",
	"RANGES_INVALID":                      "Invalid range provided.",
	"REACTIONS_TOO_MANY":                  "The message already has too many reaction emojis, you can't react with a new emoji.",
	"REACTION_EMPTY":                      "Empty reaction provided.",
	"REACTION_INVALID":                    "The specified reaction is invalid.",
	"REFLECTOR_NOT_AVAILABLE":             "Invalid call reflector server.",
	"REG_ID_GENERATE_FAILED":              "Failure while generating registration ID.",
	"REPLY_MARKUP_BUY_EMPTY":              "Reply markup for buy button empty.",
	"REPLY_MARKUP_GAME_EMPTY":             "The provided reply markup for the game is empty.",
	"REPLY_MARKUP_INVALID":                "The provided reply markup is invalid.",
	"REPLY_MARKUP_TOO_LONG":               "The specified reply_markup is too long.",
	"REPLY_MESSAGE_ID_INVALID":            "The specified reply-to message ID is invalid.",
	"REPLY_TO_INVALID":                    "The specified reply_to field is invalid.",
	"REPLY_TO_USER_INVALID":               "The replied-to user is invalid.",
	"RESET_REQUEST_MISSING":               "No password reset is in progress.",
	"RESULTS_TOO_MUCH":                    "Too many results were provided.",
	"RESULT_ID_DUPLICATE":                 "You provided a duplicate result ID.",
	"RESULT_ID_EMPTY":                     "Result ID empty.",
	"RESULT_ID_INVALID":                   "One of the specified result IDs is invalid.",
	"RESULT_TYPE_INVALID":                 "Result type invalid.",
	"REVOTE_NOT_ALLOWED":                  "You cannot change your vote.",
	"RIGHTS_NOT_MODIFIED":                 "The new admin rights are equal to the old rights, no change was made.",
	"RIGHT_FORBIDDEN":                     "Your admin rights do not allow you to do this.",
	"RPC_CALL_FAIL":                       "Telegram is having internal issues, please try again later.",
	"RPC_MCGET_FAIL":                      "Telegram is having internal issues, please try again later.",
	"RSA_DECRYPT_FAILED":                  "Internal RSA decryption failed.",
	"SCHEDULE_BOT_NOT_ALLOWED":            "Bots cannot schedule messages.",
	"SCHEDULE_DATE_INVALID":               "Invalid schedule date provided.",
	"SCHEDULE_DATE_TOO_LATE":              "You can't schedule a message this far in the future.",
	"SCHEDULE_STATUS_PRIVATE":             "Can't schedule until user is online, if the user's last seen timestamp is hidden by their privacy settings.",
	"SCHEDULE_TOO_MUCH":                   "There are too many scheduled messages.",
	"SCORE_INVALID":                       "The specified game score is invalid.",
	"SEARCH_QUERY_EMPTY":                  "The search query is empty.",
	"SEARCH_WITH_LINK_NOT_SUPPORTED":      "You cannot provide a search query and an invite link at the same time.",
	"SECONDS_INVALID":                     "Invalid duration provided.",
	"SEND_AS_PEER_INVALID":                "You can't send messages as the specified peer.",
	"SEND_CODE_UNAVAILABLE":               "Returned when all available options for this type of number were already used.",
	"SEND_MEDIA_INVALID":                  "The specified media is invalid.",
	"SEND_MESSAGE_MEDIA_INVALID":          "Invalid media provided.",
	"SEND_MESSAGE_TYPE_INVALID":           "The message type is invalid.",
	"SENSITIVE_CHANGE_FORBIDDEN":          "You can't change your sensitive content settings.",
	"SESSION_EXPIRED":                     "The authorization has expired.",
	"SESSION_PASSWORD_NEEDED":             "2FA is enabled, use a password to login.",
	"SESSION_REVOKED":                     "The authorization has been invalidated because the user terminated all sessions.",
	"SETTINGS_INVALID":                    "Invalid settings were provided.",
	"SHA256_HASH_INVALID":                 "The provided SHA256 hash is invalid.",
	"SHORTNAME_OCCUPY_FAILED":             "An error occurred when trying to register the short-name used for the sticker pack. Try a different name.",
	"SHORT_NAME_INVALID":                  "The specified short name is invalid.",
	"SHORT_NAME_OCCUPIED":                 "The specified short name is already in use.",
	"SIGN_IN_FAILED":                      "Failure while signing in.",
	"SLOTS_EMPTY":                         "The specified slot list is empty.",
	"SLOWMODE_MULTI_MSGS_DISABLED":        "Slowmode is enabled, you cannot forward multiple messages to this group.",
	"SLUG_INVALID":                        "The specified invoice slug is invalid.",
	"SMS_CODE_CREATE_FAILED":              "An error occurred while creating the SMS code.",
	"SRP_ID_INVALID":                      "Invalid SRP ID provided.",
	"SRP_PASSWORD_CHANGED":                "Password has changed.",
	"START_PARAM_EMPTY":                   "The start parameter is empty.",
	"START_PARAM_INVALID":                 "Start parameter invalid.",
	"START_PARAM_TOO_LONG":                "Start parameter is too long.",
	"STICKERPACK_STICKERS_TOO_MUCH":       "There are too many stickers in this stickerpack, you can't add any more.",
	"STICKERSET_INVALID":                  "The provided sticker set is invalid.",
	"STICKERSET_OWNER_ANONYMOUS":          "Provided stickerset can't be installed as group stickerset to prevent admin deanonymization.",
	"STICKERS_EMPTY":                      "No sticker provided.",
	"STICKERS_TOO_MUCH":                   "There are too many stickers in this stickerpack, you can't add any more.",
	"STICKER_DOCUMENT_INVALID":            "The specified sticker document is invalid.",
	"STICKER_EMOJI_INVALID":               "Sticker emoji invalid.",
	"STICKER_FILE_INVALID":                "Sticker file invalid.",
	"STICKER_GIF_DIMENSIONS":              "The specified video sticker has invalid dimensions.",
	"STICKER_ID_INVALID":                  "The provided sticker ID is invalid.",
	"STICKER_INVALID":                     "The provided sticker is invalid.",
	"STICKER_MIME_INVALID":                "The specified sticker MIME type is invalid.",
	"STICKER_PNG_DIMENSIONS":              "Sticker png dimensions invalid.",
	"STICKER_PNG_NOPNG":                   "One of the specified stickers is not a valid PNG file.",
	"STICKER_TGS_NODOC":                   "You must send the animated sticker as a document.",
	"STICKER_TGS_NOTGS":                   "Invalid TGS sticker provided.",
	"STICKER_THUMB_PNG_NOPNG":             "Incorrect stickerset thumb file provided, PNG / WEBP expected.",
	"STICKER_THUMB_TGS_NOTGS":             "Incorrect stickerset TGS thumb file provided.",
	"STICKER_VIDEO_BIG":                   "The specified video sticker is too big.",
	"STICKER_VIDEO_NODOC":                 "You must send the video sticker as a document.",
	"STICKER_VIDEO_NOWEBM":                "The specified video sticker is not in webm format.",
	"STORAGE_CHECK_FAILED":                "Server storage check failed.",
	"STORE_INVALID_SCALAR_TYPE":           "Invalid scalar type.",
	"STORIES_NEVER_CREATED":               "This peer hasn't ever posted any stories.",
	"STORIES_TOO_MUCH":                    "You have hit the maximum active stories limit; you should buy a Premium subscription or wait for the oldest story to expire.",
	"STORY_ID_EMPTY":                      "You specified no story IDs.",
	"STORY_ID_INVALID":                    "The specified story ID is invalid.",
	"STORY_NOT_MODIFIED":                  "The new story information you passed is equal to the previous story information, thus it wasn't modified.",
	"STORY_PERIOD_INVALID":                "The specified story period is invalid for this account.",
	"SWITCH_PM_TEXT_EMPTY":                "The switch_pm.text field was empty.",
	"TAKEOUT_INVALID":                     "The specified takeout ID is invalid.",
	"TAKEOUT_REQUIRED":                    "A takeout session needs to be initialized first.",
	"TASK_ALREADY_EXISTS":                 "An email reset was already requested.",
	"TEMP_AUTH_KEY_ALREADY_BOUND":         "The passed temporary key is already bound to another perm_auth_key_id.",
	"TEMP_AUTH_KEY_EMPTY":                 "No temporary auth key provided.",
	"THEME_FILE_INVALID":                  "Invalid theme file provided.",
	"THEME_FORMAT_INVALID":                "Invalid theme format provided.",
	"THEME_INVALID":                       "Invalid theme provided.",
	"THEME_MIME_INVALID":                  "The theme's MIME type is invalid.",
	"THEME_TITLE_INVALID":                 "The specified theme title is invalid.",
	"TIMEOUT":                             "A timeout occurred while fetching data from the worker.",
	"Timedout":                            "Timeout while fetching data.",
	"Timeout":                             "Timeout while fetching data.",
	"TITLE_INVALID":                       "The specified stickerpack title is invalid.",
	"TMP_PASSWORD_DISABLED":               "The temporary password is disabled.",
	"TMP_PASSWORD_INVALID":                "Password auth needs to be regenerated.",
	"TOKEN_EMPTY":                         "The specified token is empty.",
	"TOKEN_INVALID":                       "The provided token is invalid.",
	"TOKEN_TYPE_INVALID":                  "The specified token type is invalid.",
	"TOPICS_EMPTY":                        "You specified no topic IDs.",
	"TOPIC_CLOSED":                        "This topic was closed, you can't send messages to it anymore.",
	"TOPIC_CLOSE_SEPARATELY":              "The close flag cannot be provided together with any of the other flags.",
	"TOPIC_DELETED":                       "The specified topic was deleted.",
	"TOPIC_HIDE_SEPARATELY":               "The hide flag cannot be provided together with any of the other flags.",
	"TOPIC_ID_INVALID":                    "The specified topic ID is invalid.",
	"TOPIC_NOT_MODIFIED":                  "The updated topic info is equal to the current topic info, nothing was changed.",
	"TOPIC_TITLE_EMPTY":                   "The specified topic title is empty.",
	"TO_LANG_INVALID":                     "The specified destination language is invalid.",
	"TRANSCRIPTION_FAILED":                "Audio transcription failed.",
	"TTL_DAYS_INVALID":                    "The provided TTL is invalid.",
	"TTL_MEDIA_INVALID":                   "Invalid media Time To Live was provided.",
	"TTL_PERIOD_INVALID":                  "The specified TTL period is invalid.",
	"TYPES_EMPTY":                         "No top peer type was provided.",
	"TYPE_CONSTRUCTOR_INVALID":            "The type constructor is invalid.",
	"UNKNOWN_ERROR":                       "The server has returned an unknown error.",
	"UNKNOWN_METHOD":                      "The method you tried to call cannot be called on non-CDN DCs.",
	"UNTIL_DATE_INVALID":                  "Invalid until date provided.",
	"UPDATE_APP_TO_LOGIN":                 "This layer no longer supports logging in, please update your app.",
	"URL_INVALID":                         "Invalid URL provided.",
	"USAGE_LIMIT_INVALID":                 "The specified usage limit is invalid.",
	"USERNAMES_ACTIVE_TOO_MUCH":           "The maximum number of active usernames was reached.",
	"USERNAME_INVALID":                    "The provided username is not valid.",
	"USERNAME_NOT_MODIFIED":               "The username was not modified.",
	"USERNAME_NOT_OCCUPIED":               "The provided username is not occupied.",
	"USERNAME_OCCUPIED":                   "The provided username is already occupied.",
	"USERNAME_PURCHASE_AVAILABLE":         "The specified username can be purchased.",
	"USERPIC_PRIVACY_REQUIRED":            "You need to disable privacy settings for your profile picture in order to make your geolocation public.",
	"USERPIC_UPLOAD_REQUIRED":             "You must have a profile picture to publish your geolocation.",
	"USERS_TOO_FEW":                       "Not enough users.",
	"USERS_TOO_MUCH":                      "The maximum number of users has been exceeded.",
	"USER_ADMIN_INVALID":                  "You're not an admin.",
	"USER_ALREADY_INVITED":                "You have already invited this user.",
	"USER_ALREADY_PARTICIPANT":            "The user is already in the group.",
	"USER_BANNED_IN_CHANNEL":              "You're banned from sending messages in supergroups/channels.",
	"USER_BLOCKED":                        "User blocked.",
	"USER_BOT":                            "Bots can only be admins in channels.",
	"USER_BOT_INVALID":                    "This method can only be invoked by bot accounts.",
	"USER_BOT_REQUIRED":                   "This method can only be called by a bot.",
	"USER_CHANNELS_TOO_MUCH":              "One of the users you tried to add is already in too many channels/supergroups.",
	"USER_CREATOR":                        "You can't leave this channel, because you're its creator.",
	"USER_DEACTIVATED":                    "The user has been deleted/deactivated.",
	"USER_DEACTIVATED_BAN":                "The user has been deleted/deactivated.",
	"USER_DELETED":                        "You can't send this secret message because the other participant deleted their account.",
	"USER_ID_INVALID":                     "The provided user ID is invalid.",
	"USER_INVALID":                        "Invalid user provided.",
	"USER_IS_BLOCKED":                     "You were blocked by this user.",
	"USER_IS_BOT":                         "Bots can't send messages to other bots.",
	"USER_KICKED":                         "This user was kicked from this supergroup/channel.",
	"USER_NOT_MUTUAL_CONTACT":             "The provided user is not a mutual contact.",
	"USER_NOT_PARTICIPANT":                "You're not a member of this supergroup/channel.",
	"USER_PRIVACY_RESTRICTED":             "The user's privacy settings do not allow you to do this.",
	"USER_PUBLIC_MISSING":                 "Cannot generate a link to stories posted by a peer without a username.",
	"USER_RESTRICTED":                     "You're spamreported, you can't create channels or chats.",
	"USER_VOLUME_INVALID":                 "The specified user volume is invalid.",
	"VENUE_ID_INVALID":                    "The specified venue ID is invalid.",
	"VIDEO_CONTENT_TYPE_INVALID":          "The video's content type is invalid.",
	"VIDEO_FILE_INVALID":                  "The specified video file is invalid.",
	"VIDEO_TITLE_EMPTY":                   "The specified video title is empty.",
	"VOICE_MESSAGES_FORBIDDEN":            "This user's privacy settings forbid you from sending voice messages.",
	"WALLPAPER_FILE_INVALID":              "The specified wallpaper file is invalid.",
	"WALLPAPER_INVALID":                   "The specified wallpaper is invalid.",
	"WALLPAPER_MIME_INVALID":              "The specified wallpaper MIME type is invalid.",
	"WALLPAPER_NOT_FOUND":                 "The specified wallpaper could not be found.",
	"WC_CONVERT_URL_INVALID":              "WC convert URL invalid.",
	"WEBDOCUMENT_INVALID":                 "Invalid webdocument URL provided.",
	"WEBDOCUMENT_MIME_INVALID":            "Invalid webdocument mime type provided.",
	"WEBDOCUMENT_SIZE_TOO_BIG":            "Webdocument is too big!",
	"WEBDOCUMENT_URL_INVALID":             "The specified webdocument URL is invalid.",
	"WEBPAGE_CURL_FAILED":                 "Failure while fetching the webpage with cURL.",
	"WEBPAGE_MEDIA_EMPTY":                 "Webpage media empty.",
	"WEBPAGE_NOT_FOUND":                   "A preview for the specified webpage url could not be generated.",
	"WEBPAGE_URL_INVALID":                 "The specified webpage url is invalid.",
	"WEBPUSH_AUTH_INVALID":                "The specified web push authentication secret is invalid.",
	"WEBPUSH_KEY_INVALID":                 "The specified web push elliptic curve Diffie-Hellman public key is invalid.",
	"WEBPUSH_TOKEN_INVALID":               "The specified web push token is invalid.",
	"WORKER_BUSY_TOO_LONG_RETRY":          "Telegram workers are too busy to respond immediately.",
	"YOU_BLOCKED_USER":                    "You blocked this user.",

	// Errors with additional data
	"2FA_CONFIRM_WAIT_X":                    "You'll be able to reset your account in %v seconds. If not, account will be deleted in 1 week for security reasons.",
	"EMAIL_UNCONFIRMED_X":                   "Email unconfirmed, the length of the code must be %v.",
	"FILE_MIGRATE_X":                        "The file to be accessed is currently stored in DC %v.",
	"FILE_PART_X_MISSING":                   "Part %v of the file is missing from storage.",
	"FLOOD_PREMIUM_WAIT_X":                  "A wait of %v seconds is required before calling the method.",
	"FLOOD_TEST_PHONE_WAIT_X":               "A wait of %v seconds is required in the test servers.",
	"FLOOD_WAIT_X":                          "Please wait %v seconds before repeating the action.",
	"INPUT_FETCH_ERROR_X":                   "An error occurred while deserializing TL parameters: %v.",
	"INTERDC_X_CALL_ERROR":                  "An error occurred while communicating with DC %v.",
	"INTERDC_X_CALL_RICH_ERROR":             "A rich error occurred while communicating with DC %v.",
	"NETWORK_MIGRATE_X":                     "The source IP address is associated with DC %v.",
	"PASSWORD_TOO_FRESH_X":                  "The password was modified less than 24 hours ago, try again in %v seconds.",
	"PHONE_MIGRATE_X":                       "The phone number a user is trying to use for authorization is associated with DC %v.",
	"PREMIUM_SUB_ACTIVE_UNTIL_X":            "You already have a premium subscription active until unixtime %v.",
	"PREVIOUS_CHAT_IMPORT_ACTIVE_WAIT_XMIN": "Import for this chat is already in progress, wait %v minutes before starting a new one.",
	"SESSION_TOO_FRESH_X":                   "This session was created less than 24 hours ago, try again in %v seconds.",
	"SLOWMODE_WAIT_X":                       "Slowmode is enabled in this chat: wait %v seconds before sending another message to this chat.",
	"STATS_MIGRATE_X":                       "The channel statistics must be fetched from DC %v.",
	"STORY_SEND_FLOOD_MONTHLY_X":            "You've hit the monthly story limit; wait for the specified number of seconds before posting a new story.",
	"STORY_SEND_FLOOD_WEEKLY_X":             "You've hit the weekly story limit; wait for the specified number of seconds before posting a new story.",
	"TAKEOUT_INIT_DELAY_X":                  "Sorry, for security reasons, you will be able to begin downloading your data in %v seconds.",
	"USER_MIGRATE_X":                        "The user whose identity is being used to execute queries is associated with DC %v.",
}

// "FILE_REFERENCE_*":                  "The file reference expired, it must be refreshed",
// "INPUT_METHOD_INVALID_1192227_X":    "Invalid method",
// "INPUT_METHOD_INVALID_1400137063_X": "Invalid method",
// "INPUT_METHOD_INVALID_1604042050_X": "Invalid method",

type BadMsgError struct {
	*objects.BadMsgNotification
	Description string
}

func BadMsgErrorFromNative(in *objects.BadMsgNotification) *BadMsgError {
	return &BadMsgError{
		BadMsgNotification: in,
		Description:        badMsgErrorCodes[uint8(in.Code)],
	}
}

func (e *BadMsgError) Error() string {
	return fmt.Sprintf("%v (code %v)", e.Description, e.Code)
}

// https://core.telegram.org/mtproto/service_messages_about_messages#notice-of-ignored-error-message
var badMsgErrorCodes = map[uint8]string{
	16: "msg_id too low (most likely, client time is wrong; it would be worthwhile to synchronize it using msg_id notifications and re-send the original message with the “correct” msg_id or wrap it in a container with a new msg_id if the original message had waited too long on the client to be transmitted)",
	17: "msg_id too high (similar to the previous case, the client time has to be synchronized, and the message re-sent with the correct msg_id",
	18: "incorrect two lower order msg_id bits (the server expects client message msg_id to be divisible by 4)",
	19: "container msg_id is the same as msg_id of a previously received message (this must never happen)",
	20: "message too old, and it cannot be verified whether the server has received a message with this msg_id or not",
	32: "msg_seqno too low (the server has already received a message with a lower msg_id but with either a higher or an equal and odd seqno)",
	33: "msg_seqno too high (similarly, there is a message with a higher msg_id but with either a lower or an equal and odd seqno)",
	34: "an even msg_seqno expected (irrelevant message), but odd received",
	35: "odd msg_seqno expected (relevant message), but even received",
	48: "incorrect server salt (in this case, the bad_server_salt response is received with the correct salt, and the message is to be re-sent with it)",
	64: "invalid container",
}

type BadSystemMessageCode int32

const (
	ErrBadMsgUnknown             BadSystemMessageCode = 0
	ErrBadMsgIdTooLow            BadSystemMessageCode = 16
	ErrBadMsgIdTooHigh           BadSystemMessageCode = 17
	ErrBadMsgIncorrectMsgIdBits  BadSystemMessageCode = 18
	ErrBadMsgWrongContainerMsgId BadSystemMessageCode = 19 // this must never happen
	ErrBadMsgMessageTooOld       BadSystemMessageCode = 20
	ErrBadMsgSeqNoTooLow         BadSystemMessageCode = 32
	ErrBadMsgSeqNoTooHigh        BadSystemMessageCode = 33
	ErrBadMsgSeqNoExpectedEven   BadSystemMessageCode = 34
	ErrBadMsgSeqNoExpectedOdd    BadSystemMessageCode = 35
	ErrBadMsgServerSaltIncorrect BadSystemMessageCode = 48
	ErrBadMsgInvalidContainer    BadSystemMessageCode = 64
)

func AnyError(err error, errs ...string) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	for _, e := range errs {
		if strings.Contains(errStr, e) {
			return true
		}
	}
	return false
}

type errorSessionConfigsChanged struct{}

func (*errorSessionConfigsChanged) Error() string {
	return "session configuration was changed, need to repeat request"
}

func (*errorSessionConfigsChanged) CRC() uint32 {
	return 0x00000000
}

type errorDCMigrated struct {
	dc int32
}

func (e *errorDCMigrated) Error() string {
	return fmt.Sprintf("[DC_MIGRATE] The DC was migrated to %d, need to repeat request", e.dc)
}

func (*errorDCMigrated) CRC() uint32 {
	return 0x00000000
}

var ErrAuthKeyInvalid = fmt.Errorf("auth key invalid (code -404) - too many failures")

func FormatDecodeError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if !strings.Contains(s, "object with provided crc not registered") {
		return s
	}
	var crc, field, root string
	if m := regexp.MustCompile(`crc not registered: (0x[0-9a-fA-F]+)`).FindStringSubmatch(s); len(m) > 1 {
		crc = m[1]
	}
	if m := regexp.MustCompile(`decode object: (\w+\.\w+):`).FindAllStringSubmatch(s, -1); len(m) > 0 {
		field = m[len(m)-1][1]
	}
	if m := regexp.MustCompile(`\*([a-z]+\.\w+):`).FindStringSubmatch(s); len(m) > 1 {
		root = m[1]
	}
	return fmt.Sprintf("decode error: unknown crc %s at %s (in %s) - report to github.com/amarnathcjd/gogram", crc, field, root)
}
