// Code generated from E2E TL Schema; DO NOT EDIT. (c) @amarnathcjd

package e2e

import tl "github.com/amarnathcjd/gogram/internal/encoding/tl"

// ===== DecryptedMessage (Latest: Layer 73) =====
type DecryptedMessage interface {
	tl.Object
	ImplementsDecryptedMessage()
}

// decryptedMessage#91cc4674 flags:# no_webpage:flags.1?true silent:flags.5?true random_id:long ttl:int message:string media:flags.9?DecryptedMessageMedia entities:flags.7?Vector<MessageEntity> via_bot_name:flags.11?string reply_to_random_id:flags.3?long grouped_id:flags.17?long = DecryptedMessage;
type DecryptedMessageObj struct {
	NoWebpage       bool                  `tl:"flag:1,encoded_in_bitflags"`
	Silent          bool                  `tl:"flag:5,encoded_in_bitflags"`
	RandomID        int64                 // random_id:long
	TTL             int32                 // ttl:int
	Message         string                // message:string
	Media           DecryptedMessageMedia `tl:"flag:9"`  // media:flags.9?DecryptedMessageMedia
	Entities        []MessageEntity       `tl:"flag:7"`  // entities:flags.7?Vector<MessageEntity>
	ViaBotName      string                `tl:"flag:11"` // via_bot_name:flags.11?string
	ReplyToRandomID int64                 `tl:"flag:3"`  // reply_to_random_id:flags.3?long
	GroupedID       int64                 `tl:"flag:17"` // grouped_id:flags.17?long
}

func (*DecryptedMessageObj) CRC() uint32 {
	return 0x91cc4674
}

func (*DecryptedMessageObj) FlagIndex() int {
	return 0
}

func (*DecryptedMessageObj) ImplementsDecryptedMessage() {}

// decryptedMessageService#73164160 random_id:long action:DecryptedMessageAction = DecryptedMessage;
type DecryptedMessageService struct {
	RandomID int64                  // random_id:long
	Action   DecryptedMessageAction // action:DecryptedMessageAction
}

func (*DecryptedMessageService) CRC() uint32 {
	return 0x73164160
}

func (*DecryptedMessageService) ImplementsDecryptedMessage() {}

// ===== DecryptedMessageLayer =====

// decryptedMessageLayer#1be31789 random_bytes:bytes layer:int in_seq_no:int out_seq_no:int message:DecryptedMessage = DecryptedMessageLayer;
type DecryptedMessageLayer struct {
	RandomBytes []byte           // random_bytes:bytes
	Layer       int32            // layer:int
	InSeqNo     int32            // in_seq_no:int
	OutSeqNo    int32            // out_seq_no:int
	Message     DecryptedMessage // message:DecryptedMessage
}

func (*DecryptedMessageLayer) CRC() uint32 {
	return 0x1be31789
}

// ===== DecryptedMessageMedia (Latest: Layers 45/143) =====

type DecryptedMessageMedia interface {
	tl.Object
	ImplementsDecryptedMessageMedia()
}

// decryptedMessageMediaEmpty#89f5c4a = DecryptedMessageMedia;
type DecryptedMessageMediaEmpty struct{}

func (*DecryptedMessageMediaEmpty) CRC() uint32 {
	return 0x089f5c4a
}

func (*DecryptedMessageMediaEmpty) ImplementsDecryptedMessageMedia() {}

// decryptedMessageMediaPhoto#f1fa8d78 thumb:bytes thumb_w:int thumb_h:int w:int h:int size:int key:bytes iv:bytes caption:string = DecryptedMessageMedia;
type DecryptedMessageMediaPhoto struct {
	Thumb   []byte // thumb:bytes
	ThumbW  int32  // thumb_w:int
	ThumbH  int32  // thumb_h:int
	W       int32  // w:int
	H       int32  // h:int
	Size    int32  // size:int
	Key     []byte // key:bytes
	Iv      []byte // iv:bytes
	Caption string // caption:string
}

func (*DecryptedMessageMediaPhoto) CRC() uint32 {
	return 0xf1fa8d78
}

func (*DecryptedMessageMediaPhoto) ImplementsDecryptedMessageMedia() {}

// decryptedMessageMediaVideo#970c8c0e thumb:bytes thumb_w:int thumb_h:int duration:int mime_type:string w:int h:int size:int key:bytes iv:bytes caption:string = DecryptedMessageMedia;
type DecryptedMessageMediaVideo struct {
	Thumb    []byte // thumb:bytes
	ThumbW   int32  // thumb_w:int
	ThumbH   int32  // thumb_h:int
	Duration int32  // duration:int
	MimeType string // mime_type:string
	W        int32  // w:int
	H        int32  // h:int
	Size     int32  // size:int
	Key      []byte // key:bytes
	Iv       []byte // iv:bytes
	Caption  string // caption:string
}

func (*DecryptedMessageMediaVideo) CRC() uint32 {
	return 0x970c8c0e
}

func (*DecryptedMessageMediaVideo) ImplementsDecryptedMessageMedia() {}

// decryptedMessageMediaGeoPoint#35480a59 lat:double long:double = DecryptedMessageMedia;
type DecryptedMessageMediaGeoPoint struct {
	Lat  float64 // lat:double
	Long float64 // long:double
}

func (*DecryptedMessageMediaGeoPoint) CRC() uint32 {
	return 0x35480a59
}

func (*DecryptedMessageMediaGeoPoint) ImplementsDecryptedMessageMedia() {}

// decryptedMessageMediaContact#588a0a97 phone_number:string first_name:string last_name:string user_id:int = DecryptedMessageMedia;
type DecryptedMessageMediaContact struct {
	PhoneNumber string // phone_number:string
	FirstName   string // first_name:string
	LastName    string // last_name:string
	UserID      int32  // user_id:int
}

func (*DecryptedMessageMediaContact) CRC() uint32 {
	return 0x588a0a97
}

func (*DecryptedMessageMediaContact) ImplementsDecryptedMessageMedia() {}

// decryptedMessageMediaDocument#6abd9782 thumb:bytes thumb_w:int thumb_h:int mime_type:string size:long key:bytes iv:bytes attributes:Vector<DocumentAttribute> caption:string = DecryptedMessageMedia; (Layer 143)
type DecryptedMessageMediaDocument struct {
	Thumb      []byte              // thumb:bytes
	ThumbW     int32               // thumb_w:int
	ThumbH     int32               // thumb_h:int
	MimeType   string              // mime_type:string
	Size       int64               // size:long
	Key        []byte              // key:bytes
	Iv         []byte              // iv:bytes
	Attributes []DocumentAttribute // attributes:Vector<DocumentAttribute>
	Caption    string              // caption:string
}

func (*DecryptedMessageMediaDocument) CRC() uint32 {
	return 0x6abd9782
}

func (*DecryptedMessageMediaDocument) ImplementsDecryptedMessageMedia() {}

// decryptedMessageMediaVenue#8a0df56f lat:double long:double title:string address:string provider:string venue_id:string = DecryptedMessageMedia;
type DecryptedMessageMediaVenue struct {
	Lat      float64 // lat:double
	Long     float64 // long:double
	Title    string  // title:string
	Address  string  // address:string
	Provider string  // provider:string
	VenueID  string  // venue_id:string
}

func (*DecryptedMessageMediaVenue) CRC() uint32 {
	return 0x8a0df56f
}

func (*DecryptedMessageMediaVenue) ImplementsDecryptedMessageMedia() {}

// decryptedMessageMediaWebPage#e50511d8 url:string = DecryptedMessageMedia;
type DecryptedMessageMediaWebPage struct {
	URL string // url:string
}

func (*DecryptedMessageMediaWebPage) CRC() uint32 {
	return 0xe50511d8
}

func (*DecryptedMessageMediaWebPage) ImplementsDecryptedMessageMedia() {}

// decryptedMessageMediaExternalDocument#fa95b0dd id:long access_hash:long date:int mime_type:string size:int thumb:PhotoSize dc_id:int attributes:Vector<DocumentAttribute> = DecryptedMessageMedia;
type DecryptedMessageMediaExternalDocument struct {
	ID         int64               // id:long
	AccessHash int64               // access_hash:long
	Date       int32               // date:int
	MimeType   string              // mime_type:string
	Size       int32               // size:int
	Thumb      PhotoSize           // thumb:PhotoSize
	DcID       int32               // dc_id:int
	Attributes []DocumentAttribute // attributes:Vector<DocumentAttribute>
}

func (*DecryptedMessageMediaExternalDocument) CRC() uint32 {
	return 0xfa95b0dd
}

func (*DecryptedMessageMediaExternalDocument) ImplementsDecryptedMessageMedia() {}

// ===== DecryptedMessageAction =====

type DecryptedMessageAction interface {
	tl.Object
	ImplementsDecryptedMessageAction()
}

// decryptedMessageActionSetMessageTTL#a1733aec ttl_seconds:int = DecryptedMessageAction;
type DecryptedMessageActionSetMessageTTL struct {
	TTLSeconds int32 // ttl_seconds:int
}

func (*DecryptedMessageActionSetMessageTTL) CRC() uint32 {
	return 0xa1733aec
}

func (*DecryptedMessageActionSetMessageTTL) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionReadMessages#c4f40be random_ids:Vector<long> = DecryptedMessageAction;
type DecryptedMessageActionReadMessages struct {
	RandomIDs []int64 // random_ids:Vector<long>
}

func (*DecryptedMessageActionReadMessages) CRC() uint32 {
	return 0x0c4f40be
}

func (*DecryptedMessageActionReadMessages) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionDeleteMessages#65614304 random_ids:Vector<long> = DecryptedMessageAction;
type DecryptedMessageActionDeleteMessages struct {
	RandomIDs []int64 // random_ids:Vector<long>
}

func (*DecryptedMessageActionDeleteMessages) CRC() uint32 {
	return 0x65614304
}

func (*DecryptedMessageActionDeleteMessages) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionScreenshotMessages#8ac1f475 random_ids:Vector<long> = DecryptedMessageAction;
type DecryptedMessageActionScreenshotMessages struct {
	RandomIDs []int64 // random_ids:Vector<long>
}

func (*DecryptedMessageActionScreenshotMessages) CRC() uint32 {
	return 0x8ac1f475
}

func (*DecryptedMessageActionScreenshotMessages) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionFlushHistory#6719e45c = DecryptedMessageAction;
type DecryptedMessageActionFlushHistory struct{}

func (*DecryptedMessageActionFlushHistory) CRC() uint32 {
	return 0x6719e45c
}

func (*DecryptedMessageActionFlushHistory) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionResend#511110b0 start_seq_no:int end_seq_no:int = DecryptedMessageAction;
type DecryptedMessageActionResend struct {
	StartSeqNo int32 // start_seq_no:int
	EndSeqNo   int32 // end_seq_no:int
}

func (*DecryptedMessageActionResend) CRC() uint32 {
	return 0x511110b0
}

func (*DecryptedMessageActionResend) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionNotifyLayer#f3048883 layer:int = DecryptedMessageAction;
type DecryptedMessageActionNotifyLayer struct {
	Layer int32 // layer:int
}

func (*DecryptedMessageActionNotifyLayer) CRC() uint32 {
	return 0xf3048883
}

func (*DecryptedMessageActionNotifyLayer) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionTyping#ccb27641 action:SendMessageAction = DecryptedMessageAction;
type DecryptedMessageActionTyping struct {
	Action SendMessageAction // action:SendMessageAction
}

func (*DecryptedMessageActionTyping) CRC() uint32 {
	return 0xccb27641
}

func (*DecryptedMessageActionTyping) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionRequestKey#f3c9611b exchange_id:long g_a:bytes = DecryptedMessageAction;
type DecryptedMessageActionRequestKey struct {
	ExchangeID int64  // exchange_id:long
	GA         []byte // g_a:bytes
}

func (*DecryptedMessageActionRequestKey) CRC() uint32 {
	return 0xf3c9611b
}

func (*DecryptedMessageActionRequestKey) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionAcceptKey#6fe1735b exchange_id:long g_b:bytes key_fingerprint:long = DecryptedMessageAction;
type DecryptedMessageActionAcceptKey struct {
	ExchangeID     int64  // exchange_id:long
	GB             []byte // g_b:bytes
	KeyFingerprint int64  // key_fingerprint:long
}

func (*DecryptedMessageActionAcceptKey) CRC() uint32 {
	return 0x6fe1735b
}

func (*DecryptedMessageActionAcceptKey) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionCommitKey#ec2e0b9b exchange_id:long key_fingerprint:long = DecryptedMessageAction;
type DecryptedMessageActionCommitKey struct {
	ExchangeID     int64 // exchange_id:long
	KeyFingerprint int64 // key_fingerprint:long
}

func (*DecryptedMessageActionCommitKey) CRC() uint32 {
	return 0xec2e0b9b
}

func (*DecryptedMessageActionCommitKey) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionAbortKey#dd05ec6b exchange_id:long = DecryptedMessageAction;
type DecryptedMessageActionAbortKey struct {
	ExchangeID int64 // exchange_id:long
}

func (*DecryptedMessageActionAbortKey) CRC() uint32 {
	return 0xdd05ec6b
}

func (*DecryptedMessageActionAbortKey) ImplementsDecryptedMessageAction() {}

// decryptedMessageActionNoop#a82fdd63 = DecryptedMessageAction;
type DecryptedMessageActionNoop struct{}

func (*DecryptedMessageActionNoop) CRC() uint32 {
	return 0xa82fdd63
}

func (*DecryptedMessageActionNoop) ImplementsDecryptedMessageAction() {}

// ===== MessageEntity (Layer 101) =====

type MessageEntity interface {
	tl.Object
	ImplementsMessageEntity()
}

// messageEntityUnknown#bb92ba95 offset:int length:int = MessageEntity;
type MessageEntityUnknown struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityUnknown) CRC() uint32 {
	return 0xbb92ba95
}

func (*MessageEntityUnknown) ImplementsMessageEntity() {}

// messageEntityMention#fa04579d offset:int length:int = MessageEntity;
type MessageEntityMention struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityMention) CRC() uint32 {
	return 0xfa04579d
}

func (*MessageEntityMention) ImplementsMessageEntity() {}

// messageEntityHashtag#6f635b0d offset:int length:int = MessageEntity;
type MessageEntityHashtag struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityHashtag) CRC() uint32 {
	return 0x6f635b0d
}

func (*MessageEntityHashtag) ImplementsMessageEntity() {}

// messageEntityBotCommand#6cef8ac7 offset:int length:int = MessageEntity;
type MessageEntityBotCommand struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityBotCommand) CRC() uint32 {
	return 0x6cef8ac7
}

func (*MessageEntityBotCommand) ImplementsMessageEntity() {}

// messageEntityUrl#6ed02538 offset:int length:int = MessageEntity;
type MessageEntityURL struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityURL) CRC() uint32 {
	return 0x6ed02538
}

func (*MessageEntityURL) ImplementsMessageEntity() {}

// messageEntityEmail#64e475c2 offset:int length:int = MessageEntity;
type MessageEntityEmail struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityEmail) CRC() uint32 {
	return 0x64e475c2
}

func (*MessageEntityEmail) ImplementsMessageEntity() {}

// messageEntityBold#bd610bc9 offset:int length:int = MessageEntity;
type MessageEntityBold struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityBold) CRC() uint32 {
	return 0xbd610bc9
}

func (*MessageEntityBold) ImplementsMessageEntity() {}

// messageEntityItalic#826f8b60 offset:int length:int = MessageEntity;
type MessageEntityItalic struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityItalic) CRC() uint32 {
	return 0x826f8b60
}

func (*MessageEntityItalic) ImplementsMessageEntity() {}

// messageEntityCode#28a20571 offset:int length:int = MessageEntity;
type MessageEntityCode struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityCode) CRC() uint32 {
	return 0x28a20571
}

func (*MessageEntityCode) ImplementsMessageEntity() {}

// messageEntityPre#73924be0 offset:int length:int language:string = MessageEntity;
type MessageEntityPre struct {
	Offset   int32  // offset:int
	Length   int32  // length:int
	Language string // language:string
}

func (*MessageEntityPre) CRC() uint32 {
	return 0x73924be0
}

func (*MessageEntityPre) ImplementsMessageEntity() {}

// messageEntityTextUrl#76a6d327 offset:int length:int url:string = MessageEntity;
type MessageEntityTextURL struct {
	Offset int32  // offset:int
	Length int32  // length:int
	URL    string // url:string
}

func (*MessageEntityTextURL) CRC() uint32 {
	return 0x76a6d327
}

func (*MessageEntityTextURL) ImplementsMessageEntity() {}

// messageEntityUnderline#9c4e7e8b offset:int length:int = MessageEntity;
type MessageEntityUnderline struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityUnderline) CRC() uint32 {
	return 0x9c4e7e8b
}

func (*MessageEntityUnderline) ImplementsMessageEntity() {}

// messageEntityStrike#bf0693d4 offset:int length:int = MessageEntity;
type MessageEntityStrike struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityStrike) CRC() uint32 {
	return 0xbf0693d4
}

func (*MessageEntityStrike) ImplementsMessageEntity() {}

// messageEntityBlockquote#20df5d0 offset:int length:int = MessageEntity;
type MessageEntityBlockquote struct {
	Offset int32 // offset:int
	Length int32 // length:int
}

func (*MessageEntityBlockquote) CRC() uint32 {
	return 0x020df5d0
}

func (*MessageEntityBlockquote) ImplementsMessageEntity() {}

// ===== DocumentAttribute (Layer 66) =====

type DocumentAttribute interface {
	tl.Object
	ImplementsDocumentAttribute()
}

// documentAttributeImageSize#6c37c15c w:int h:int = DocumentAttribute;
type DocumentAttributeImageSize struct {
	W int32 // w:int
	H int32 // h:int
}

func (*DocumentAttributeImageSize) CRC() uint32 {
	return 0x6c37c15c
}

func (*DocumentAttributeImageSize) ImplementsDocumentAttribute() {}

// documentAttributeAnimated#11b58939 = DocumentAttribute;
type DocumentAttributeAnimated struct{}

func (*DocumentAttributeAnimated) CRC() uint32 {
	return 0x11b58939
}

func (*DocumentAttributeAnimated) ImplementsDocumentAttribute() {}

// documentAttributeSticker#3a556302 alt:string stickerset:InputStickerSet = DocumentAttribute;
type DocumentAttributeSticker struct {
	Alt        string          // alt:string
	Stickerset InputStickerSet // stickerset:InputStickerSet
}

func (*DocumentAttributeSticker) CRC() uint32 {
	return 0x3a556302
}

func (*DocumentAttributeSticker) ImplementsDocumentAttribute() {}

// documentAttributeVideo#ef02ce6 flags:# round_message:flags.0?true duration:int w:int h:int = DocumentAttribute; (Layer 66)
type DocumentAttributeVideo struct {
	RoundMessage bool  `tl:"flag:0,encoded_in_bitflags"` // round_message:flags.0?true
	Duration     int32 // duration:int
	W            int32 // w:int
	H            int32 // h:int
}

func (*DocumentAttributeVideo) CRC() uint32 {
	return 0x0ef02ce6
}

func (*DocumentAttributeVideo) FlagIndex() int {
	return 0
}

func (*DocumentAttributeVideo) ImplementsDocumentAttribute() {}

// documentAttributeAudio#9852f9c6 flags:# voice:flags.10?true duration:int title:flags.0?string performer:flags.1?string waveform:flags.2?bytes = DocumentAttribute; (Layer 46)
type DocumentAttributeAudio struct {
	Voice     bool   `tl:"flag:10,encoded_in_bitflags"` // voice:flags.10?true
	Duration  int32  // duration:int
	Title     string `tl:"flag:0"` // title:flags.0?string
	Performer string `tl:"flag:1"` // performer:flags.1?string
	Waveform  []byte `tl:"flag:2"` // waveform:flags.2?bytes
}

func (*DocumentAttributeAudio) CRC() uint32 {
	return 0x9852f9c6
}

func (*DocumentAttributeAudio) FlagIndex() int {
	return 0
}

func (*DocumentAttributeAudio) ImplementsDocumentAttribute() {}

// documentAttributeFilename#15590068 file_name:string = DocumentAttribute;
type DocumentAttributeFilename struct {
	FileName string // file_name:string
}

func (*DocumentAttributeFilename) CRC() uint32 {
	return 0x15590068
}

func (*DocumentAttributeFilename) ImplementsDocumentAttribute() {}

// ===== PhotoSize =====

type PhotoSize interface {
	tl.Object
	ImplementsPhotoSize()
}

// photoSizeEmpty#e17e23c type:string = PhotoSize;
type PhotoSizeEmpty struct {
	Type string // type:string
}

func (*PhotoSizeEmpty) CRC() uint32 {
	return 0x0e17e23c
}

func (*PhotoSizeEmpty) ImplementsPhotoSize() {}

// photoSize#77bfb61b type:string location:FileLocation w:int h:int size:int = PhotoSize;
type PhotoSizeObj struct {
	Type     string       // type:string
	Location FileLocation // location:FileLocation
	W        int32        // w:int
	H        int32        // h:int
	Size     int32        // size:int
}

func (*PhotoSizeObj) CRC() uint32 {
	return 0x77bfb61b
}

func (*PhotoSizeObj) ImplementsPhotoSize() {}

// photoCachedSize#e9a734fa type:string location:FileLocation w:int h:int bytes:bytes = PhotoSize;
type PhotoCachedSize struct {
	Type     string       // type:string
	Location FileLocation // location:FileLocation
	W        int32        // w:int
	H        int32        // h:int
	Bytes    []byte       // bytes:bytes
}

func (*PhotoCachedSize) CRC() uint32 {
	return 0xe9a734fa
}

func (*PhotoCachedSize) ImplementsPhotoSize() {}

// ===== FileLocation =====

type FileLocation interface {
	tl.Object
	ImplementsFileLocation()
}

// fileLocationUnavailable#7c596b46 volume_id:long local_id:int secret:long = FileLocation;
type FileLocationUnavailable struct {
	VolumeID int64 // volume_id:long
	LocalID  int32 // local_id:int
	Secret   int64 // secret:long
}

func (*FileLocationUnavailable) CRC() uint32 {
	return 0x7c596b46
}

func (*FileLocationUnavailable) ImplementsFileLocation() {}

// fileLocation#53d69076 dc_id:int volume_id:long local_id:int secret:long = FileLocation;
type FileLocationObj struct {
	DcID     int32 // dc_id:int
	VolumeID int64 // volume_id:long
	LocalID  int32 // local_id:int
	Secret   int64 // secret:long
}

func (*FileLocationObj) CRC() uint32 {
	return 0x53d69076
}

func (*FileLocationObj) ImplementsFileLocation() {}

// ===== InputStickerSet =====

type InputStickerSet interface {
	tl.Object
	ImplementsInputStickerSet()
}

// inputStickerSetEmpty#ffb62b95 = InputStickerSet;
type InputStickerSetEmpty struct{}

func (*InputStickerSetEmpty) CRC() uint32 {
	return 0xffb62b95
}

func (*InputStickerSetEmpty) ImplementsInputStickerSet() {}

// inputStickerSetShortName#861cc8a0 short_name:string = InputStickerSet;
type InputStickerSetShortName struct {
	ShortName string // short_name:string
}

func (*InputStickerSetShortName) CRC() uint32 {
	return 0x861cc8a0
}

func (*InputStickerSetShortName) ImplementsInputStickerSet() {}

// ===== SendMessageAction (Layer 66) =====

type SendMessageAction interface {
	tl.Object
	ImplementsSendMessageAction()
}

// sendMessageTypingAction#16bf744e = SendMessageAction;
type SendMessageTypingAction struct{}

func (*SendMessageTypingAction) CRC() uint32 {
	return 0x16bf744e
}

func (*SendMessageTypingAction) ImplementsSendMessageAction() {}

// sendMessageCancelAction#fd5ec8f5 = SendMessageAction;
type SendMessageCancelAction struct{}

func (*SendMessageCancelAction) CRC() uint32 {
	return 0xfd5ec8f5
}

func (*SendMessageCancelAction) ImplementsSendMessageAction() {}

// sendMessageRecordVideoAction#a187d66f = SendMessageAction;
type SendMessageRecordVideoAction struct{}

func (*SendMessageRecordVideoAction) CRC() uint32 {
	return 0xa187d66f
}

func (*SendMessageRecordVideoAction) ImplementsSendMessageAction() {}

// sendMessageUploadVideoAction#92042ff7 = SendMessageAction;
type SendMessageUploadVideoAction struct{}

func (*SendMessageUploadVideoAction) CRC() uint32 {
	return 0x92042ff7
}

func (*SendMessageUploadVideoAction) ImplementsSendMessageAction() {}

// sendMessageRecordAudioAction#d52f73f7 = SendMessageAction;
type SendMessageRecordAudioAction struct{}

func (*SendMessageRecordAudioAction) CRC() uint32 {
	return 0xd52f73f7
}

func (*SendMessageRecordAudioAction) ImplementsSendMessageAction() {}

// sendMessageUploadAudioAction#e6ac8a6f = SendMessageAction;
type SendMessageUploadAudioAction struct{}

func (*SendMessageUploadAudioAction) CRC() uint32 {
	return 0xe6ac8a6f
}

func (*SendMessageUploadAudioAction) ImplementsSendMessageAction() {}

// sendMessageUploadPhotoAction#990a3c1a = SendMessageAction;
type SendMessageUploadPhotoAction struct{}

func (*SendMessageUploadPhotoAction) CRC() uint32 {
	return 0x990a3c1a
}

func (*SendMessageUploadPhotoAction) ImplementsSendMessageAction() {}

// sendMessageUploadDocumentAction#8faee98e = SendMessageAction;
type SendMessageUploadDocumentAction struct{}

func (*SendMessageUploadDocumentAction) CRC() uint32 {
	return 0x8faee98e
}

func (*SendMessageUploadDocumentAction) ImplementsSendMessageAction() {}

// sendMessageGeoLocationAction#176f8ba1 = SendMessageAction;
type SendMessageGeoLocationAction struct{}

func (*SendMessageGeoLocationAction) CRC() uint32 {
	return 0x176f8ba1
}

func (*SendMessageGeoLocationAction) ImplementsSendMessageAction() {}

// sendMessageChooseContactAction#628cbc6f = SendMessageAction;
type SendMessageChooseContactAction struct{}

func (*SendMessageChooseContactAction) CRC() uint32 {
	return 0x628cbc6f
}

func (*SendMessageChooseContactAction) ImplementsSendMessageAction() {}

// sendMessageRecordRoundAction#88f27fbc = SendMessageAction; (Layer 66)
type SendMessageRecordRoundAction struct{}

func (*SendMessageRecordRoundAction) CRC() uint32 {
	return 0x88f27fbc
}

func (*SendMessageRecordRoundAction) ImplementsSendMessageAction() {}

// sendMessageUploadRoundAction#bb718624 = SendMessageAction; (Layer 66)
type SendMessageUploadRoundAction struct{}

func (*SendMessageUploadRoundAction) CRC() uint32 {
	return 0xbb718624
}

func (*SendMessageUploadRoundAction) ImplementsSendMessageAction() {}
