package telegram

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	ige "github.com/amarnathcjd/gogram/internal/aes_ige"
	"github.com/amarnathcjd/gogram/internal/utils"
	"github.com/pkg/errors"
)

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ResolveDataCenterIP(dc int, test, ipv6 bool) string {
	return utils.GetDefaultHostIp(dc, test, ipv6)
}

func joinAbsWorkingDir(filename string) string {
	if filename == "" {
		filename = "session.dat" // default filename for session file
	}

	if !filepath.IsAbs(filename) || !strings.Contains(filename, string(filepath.Separator)) {
		workDir, err := os.Getwd()
		if err != nil {
			panic(err)
		}

		return filepath.Join(workDir, filename)
	}

	return filename
}

func PathIsWritable(path string) bool {
	file, err := os.OpenFile(path, os.O_WRONLY, 0666)
	if err != nil {
		return false
	}
	defer file.Close()
	return true
}

func GenRandInt() int64 {
	return int64(rand.Int31())
}

func (c *Client) getMultiMedia(m any, attrs *MediaMetadata) ([]*InputSingleMedia, error) {
	var signatures = make([]string, 0)

	var media []*InputSingleMedia
	if attrs == nil {
		attrs = &MediaMetadata{}
	}

	attrs.Inline = true // force to call media.uploadToSelf
	var inputMedia []InputMedia
	switch m := m.(type) {
	case *InputSingleMedia:
		media = append(media, m)
	case []*InputSingleMedia:
		media = m
	case []NewMessage:
		for _, msg := range m {
			if md := msg.Media(); md != nil {
				mediaObj, err := c.getSendableMedia(md, attrs)
				if err != nil {
					return nil, err
				}
				inputMedia = append(inputMedia, mediaObj)
			}
		}
	case []*NewMessage:
		for _, msg := range m {
			if md := msg.Media(); md != nil {
				mediaObj, err := c.getSendableMedia(md, attrs)
				if err != nil {
					return nil, err
				}
				inputMedia = append(inputMedia, mediaObj)
			}
		}
	case []InputMedia:
		for _, m := range m {
			mediaObj, err := c.getSendableMedia(m, attrs)
			if err != nil {
				return nil, err
			}
			inputMedia = append(inputMedia, mediaObj)
		}
	case []InputFile:
		for _, m := range m {
			mediaObj, err := c.getSendableMedia(m, attrs)
			if err != nil {
				return nil, err
			}
			inputMedia = append(inputMedia, mediaObj)
		}
	case []MessageMedia:
		for _, m := range m {
			mediaObj, err := c.getSendableMedia(m, attrs)
			if err != nil {
				return nil, err
			}
			inputMedia = append(inputMedia, mediaObj)
		}
	case []string:
		for _, m := range m {
			if !IsURL(m) && !attrs.SkipHash {
				signature, err := calculateSha256Hash(m)
				if err != nil {
					return nil, err
				}

				var found bool
				for i, sig := range signatures {
					if sig == signature {
						inputMedia = append(inputMedia, inputMedia[i])
						found = true
						break
					}
				}

				signatures = append(signatures, signature)
				if found {
					continue
				}
			}
			mediaObj, err := c.getSendableMedia(m, attrs)
			if err != nil {
				return nil, err
			}
			inputMedia = append(inputMedia, mediaObj)
		}
	case [][]byte:
		for _, m := range m {
			mediaObj, err := c.getSendableMedia(m, attrs)
			if err != nil {
				return nil, err
			}
			inputMedia = append(inputMedia, mediaObj)
		}
	case string, InputFile, InputMedia, MessageMedia, []byte:
		mediaObj, err := c.getSendableMedia(m, attrs)
		if err != nil {
			return nil, err
		}
		inputMedia = append(inputMedia, mediaObj)
	case nil:
		inputMedia = append(inputMedia, &InputMediaEmpty{})
	}
	for _, m := range inputMedia {
		media = append(media, &InputSingleMedia{
			Media:    m,
			RandomID: GenRandInt(),
		})
	}
	return media, nil
}

func calculateSha256Hash(localFile string) (string, error) {
	file, err := os.Open(localFile)
	if err != nil {
		return "", err
	}

	defer file.Close()
	// calculate sha256 hash
	// if file is too large take a hash of the first 10MB

	if stat, err := file.Stat(); err == nil && stat.Size() > 50*1024*200 {
		hash := sha256.New()
		if _, err := io.CopyN(hash, file, 50*1024*200); err != nil {
			return "", err
		}

		return fmt.Sprintf("%x", hash.Sum(nil)), nil
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func processUpdates(updates Updates) []Message {
	var messages []Message
	switch updates := updates.(type) {
	case *UpdatesObj:
		for _, update := range updates.Updates {
			switch update := update.(type) {
			case *UpdateNewMessage:
				messages = append(messages, update.Message.(*MessageObj))
			case *UpdateNewChannelMessage:
				messages = append(messages, update.Message.(*MessageObj))
			case *UpdateEditMessage:
				messages = append(messages, update.Message.(*MessageObj))
			case *UpdateEditChannelMessage:
				messages = append(messages, update.Message.(*MessageObj))
			case *UpdateBotEditBusinessMessage:
				messages = append(messages, update.Message.(*MessageObj))
			}
		}
	}
	return messages
}

func (c *Client) processUpdate(upd Updates) *MessageObj {
	if upd == nil {
		return nil
	}
updateTypeSwitch:
	switch update := upd.(type) {
	case *UpdateShortSentMessage:
		return &MessageObj{
			ID:        update.ID,
			PeerID:    &PeerUser{},
			Date:      update.Date,
			FromID:    &PeerUser{UserID: c.GetPeerID(c.Me())},
			Out:       update.Out,
			Media:     update.Media,
			Entities:  update.Entities,
			TtlPeriod: update.TtlPeriod,
		}
	case *UpdatesObj:
		upd := update.Updates[0]
		for _, u := range update.Updates {
			switch u.(type) {
			case *UpdateNewMessage, *UpdateNewChannelMessage, *UpdateEditMessage, *UpdateEditChannelMessage:
				upd = u
			}
		}
		switch upd := upd.(type) {
		case *UpdateNewMessage:
			return upd.Message.(*MessageObj)
		case *UpdateNewChannelMessage:
			return upd.Message.(*MessageObj)
		case *UpdateEditMessage:
			return upd.Message.(*MessageObj)
		case *UpdateEditChannelMessage:
			return upd.Message.(*MessageObj)
		case *UpdateBotEditBusinessMessage:
			return upd.Message.(*MessageObj)
		case *UpdateMessageID:
			return &MessageObj{
				ID: upd.ID,
			}
		default:
			c.Log.Debug("unknown update type", reflect.TypeOf(upd).String())
		}
	case *UpdateShortMessage:
		return &MessageObj{Out: update.Out, ID: update.ID, PeerID: &PeerUser{}, Date: update.Date, Entities: update.Entities, TtlPeriod: update.TtlPeriod, ReplyTo: update.ReplyTo, FromID: &PeerUser{UserID: update.UserID}, ViaBotID: update.ViaBotID, Message: update.Message, MediaUnread: update.MediaUnread, Silent: update.Silent, FwdFrom: update.FwdFrom}
	case *UpdateShortChatMessage:
		chat, err := c.GetPeer(update.ChatID)
		if err != nil {
			chat = &PeerChat{}
		}
		return &MessageObj{Out: update.Out, ID: update.ID, PeerID: chat.(*PeerChat), Date: update.Date, Entities: update.Entities, TtlPeriod: update.TtlPeriod}
	case *UpdateShort:
		upd = &UpdatesObj{Updates: []Update{update.Update}}
		goto updateTypeSwitch
	default:
		c.Log.Debug("unknown update type", reflect.TypeOf(upd).String())
	}
	return nil
}

func (c *Client) GetSendablePeer(PeerID any) (InputPeer, error) {
PeerSwitch:
	switch Peer := PeerID.(type) {
	case nil:
		return nil, errors.New("peer is nil, cannot send nil peer")
	case *PeerUser:
		peerEntity, err := c.GetPeerUser(Peer.UserID)
		if err != nil {
			return nil, err
		}
		return &InputPeerUser{UserID: peerEntity.UserID, AccessHash: peerEntity.AccessHash}, nil
	case *PeerChat:
		return &InputPeerChat{ChatID: Peer.ChatID}, nil
	case *PeerChannel:
		peerEntity, err := c.GetPeerChannel(Peer.ChannelID)
		if err != nil {
			return nil, err
		}
		return &InputPeerChannel{ChannelID: peerEntity.ChannelID, AccessHash: peerEntity.AccessHash}, nil
	case *InputPeerChat:
		return Peer, nil
	case *InputPeerChannel:
		return Peer, nil
	case *InputPeerUser:
		return Peer, nil
	case *InputPeer:
		return *Peer, nil
	case *InputUserSelf:
		return &InputPeerSelf{}, nil
	case *InputPeerSelf:
		return Peer, nil
	case *InputPeerEmpty:
		return Peer, nil
	case *InputPeerUserFromMessage:
		return Peer, nil
	case *InputPeerChannelFromMessage:
		return Peer, nil
	case *InputUserObj:
		return &InputPeerUser{UserID: Peer.UserID, AccessHash: Peer.AccessHash}, nil
	case *InputChannelObj:
		return &InputPeerChannel{ChannelID: Peer.ChannelID, AccessHash: Peer.AccessHash}, nil
	case *ChatObj:
		return &InputPeerChat{ChatID: Peer.ID}, nil
	case *Channel:
		return &InputPeerChannel{ChannelID: Peer.ID, AccessHash: Peer.AccessHash}, nil
	case *UserObj:
		return &InputPeerUser{UserID: Peer.ID, AccessHash: Peer.AccessHash}, nil
	case int64, int32, int:
		peerEntity, err := c.GetInputPeer(getAnyInt(PeerID))
		if peerEntity == nil || err != nil {
			return nil, err
		}
		return peerEntity, nil
	case string:
		if i, err := strconv.ParseInt(Peer, 10, 64); err == nil {
			PeerID = i
			goto PeerSwitch
		}
		if Peer == "me" || Peer == "self" {
			return &InputPeerSelf{}, nil
		}

		if peerMap, ok := c.Cache.usernameMap[strings.TrimPrefix(Peer, "@")]; ok {
			if peerHash, ok := c.Cache.InputPeers.InputChannels[peerMap]; ok {
				return &InputPeerChannel{ChannelID: peerMap, AccessHash: peerHash}, nil
			}
			if peerHash, ok := c.Cache.InputPeers.InputUsers[peerMap]; ok {
				return &InputPeerUser{UserID: peerMap, AccessHash: peerHash}, nil
			}
		}

		peerEntity, err := c.ResolveUsername(Peer)
		if err != nil {
			return nil, err
		}
		switch peerEntity := peerEntity.(type) {
		case *ChatObj:
			return &InputPeerChat{ChatID: peerEntity.ID}, nil
		case *Channel:
			return &InputPeerChannel{ChannelID: peerEntity.ID, AccessHash: peerEntity.AccessHash}, nil
		case *UserObj:
			return &InputPeerUser{UserID: peerEntity.ID, AccessHash: peerEntity.AccessHash}, nil
		case *ChannelForbidden:
			return &InputPeerChannel{ChannelID: peerEntity.ID, AccessHash: peerEntity.AccessHash}, nil
		case *ChatForbidden:
			return &InputPeerChat{ChatID: peerEntity.ID}, nil
		default:
			return nil, errors.New(fmt.Sprintf("unknown peer type %s", reflect.TypeOf(peerEntity).String()))
		}
	case *ChannelForbidden:
		return &InputPeerChannel{ChannelID: Peer.ID, AccessHash: Peer.AccessHash}, nil
	case *ChatForbidden:
		return &InputPeerChat{ChatID: Peer.ID}, nil
	case *InputUserFromMessage:
		return &InputPeerUserFromMessage{Peer: Peer.Peer, MsgID: Peer.MsgID, UserID: Peer.UserID}, nil
	case *InputChannelFromMessage:
		return &InputPeerChannelFromMessage{Peer: Peer.Peer, MsgID: Peer.MsgID, ChannelID: Peer.ChannelID}, nil
	default:
		return nil, errors.New("Failed to get sendable peer, unknown type " + reflect.TypeOf(PeerID).String())
	}
}

// ResolvePeer resolves a peer to a sendable peer, searches the cache if the peer is already resolved
func (c *Client) ResolvePeer(peerToResolve any) (InputPeer, error) {
	return c.GetSendablePeer(peerToResolve)
}

func (c *Client) GetSendableChannel(PeerID any) (InputChannel, error) {
	rawPeer, err := c.ResolvePeer(PeerID)
	if err != nil {
		return nil, err
	}

	switch rawPeer := rawPeer.(type) {
	case *InputPeerChannel:
		return &InputChannelObj{ChannelID: rawPeer.ChannelID, AccessHash: rawPeer.AccessHash}, nil
	case *InputPeerChannelFromMessage:
		return &InputChannelFromMessage{Peer: rawPeer.Peer, MsgID: rawPeer.MsgID, ChannelID: rawPeer.ChannelID}, nil
	case *InputPeerChat, *InputPeerUser:
		return nil, errors.New("given peer is not a channel")
	default:
		return nil, errors.New("failed to get sendable channel, unknown type " + reflect.TypeOf(rawPeer).String())
	}
}

func (c *Client) GetSendableUser(PeerID any) (InputUser, error) {
	rawPeer, err := c.ResolvePeer(PeerID)
	if err != nil {
		return nil, err
	}

	switch rawPeer := rawPeer.(type) {
	case *InputPeerUser:
		return &InputUserObj{UserID: rawPeer.UserID, AccessHash: rawPeer.AccessHash}, nil
	case *InputPeerUserFromMessage:
		return &InputUserFromMessage{Peer: rawPeer.Peer, MsgID: rawPeer.MsgID, UserID: rawPeer.UserID}, nil
	case *InputPeerChat, *InputPeerChannel:
		return nil, errors.New("given peer is not a user")
	default:
		return nil, errors.New("failed to get sendable user, unknown type " + reflect.TypeOf(rawPeer).String())
	}
}

func (c *Client) GetPeerID(Peer any) int64 {
	if Peer == nil {
		return 0
	}
	switch Peer := Peer.(type) {
	case *PeerChat:
		return Peer.ChatID
	case *PeerChannel:
		return Peer.ChannelID
	case *PeerUser:
		return Peer.UserID
	case *InputPeerChat:
		return Peer.ChatID
	case *InputPeerChannel:
		return Peer.ChannelID
	case *InputPeerUser:
		return Peer.UserID
	case *UserObj:
		return Peer.ID
	case *ChatObj:
		return Peer.ID
	case *Channel:
		return Peer.ID
	case *InputUserObj:
		return Peer.UserID
	case *InputChannelObj:
		return Peer.ChannelID
	default:
		return 0
	}
}

func getAnyInt(v any) int64 {
	switch v := v.(type) {
	case int64:
		return v
	case int32:
		return int64(v)
	case int:
		return int64(v)
	default:
		return 0
	}
}

func (c *Client) GetSendableMedia(mediaFile any, attr *MediaMetadata) (InputMedia, error) {
	return c.getSendableMedia(mediaFile, attr)
}

func (c *Client) getSendableMedia(mediaFile any, attributes *MediaMetadata) (InputMedia, error) {
	attr := getValue(attributes, &MediaMetadata{})
	var thumbnail InputFile

	switch thumb := attr.Thumb.(type) {
	case InputFile, *InputFile:
		thumbnail = getValueAny(thumb, &InputFileObj{}).(InputFile)
	case nil:

	default:
		fi, err := c.UploadFile(thumb)
		if err != nil {
			return nil, err
		}
		thumbnail = fi
	}

mediaTypeSwitch:
	switch media := mediaFile.(type) {
	case string:
		if IsURL(media) {
			if _, isImage := MimeTypes.MIME(media); isImage && !attr.ForceDocument {
				photoExt := &InputMediaPhotoExternal{URL: media, TtlSeconds: getValue(attr.TTL, 0), Spoiler: getValue(attr.Spoiler, false)}
				if attr.Inline {
					return c.uploadToSelf(photoExt)
				}

				return photoExt, nil
			}

			if attr == nil {
				return nil, errors.New("attributes cannot be nil")
			}
			documentExt := &InputMediaDocumentExternal{URL: media, TtlSeconds: getValue(attr.TTL, 0), Spoiler: getValue(attr.Spoiler, false)}
			if attr.Inline {
				return c.uploadToSelf(documentExt)
			}

			return documentExt, nil
		} else {
			if _, err := os.Stat(media); err == nil {
				uploadOpts := &UploadOptions{}
				if attr.ProgressManager != nil {
					uploadOpts.ProgressManager = attr.ProgressManager
				}
				uploadOpts.Threads = attr.UploadThreads

				mediaFile, err = c.UploadFile(media, uploadOpts)
				if err != nil {
					return nil, err
				}
				attr.FileAbsPath = media

				goto mediaTypeSwitch
			} else {
				return nil, err
			}
		}
	case InputMedia:
		return media, nil
	case *InputMedia:
		return *media, nil
	case Document:
		switch media := media.(type) {
		case *DocumentObj:
			return &InputMediaDocument{ID: &InputDocumentObj{ID: media.ID, AccessHash: media.AccessHash, FileReference: media.FileReference}, TtlSeconds: getValue(attr.TTL, 0), Spoiler: getValue(attr.Spoiler, false)}, nil
		case *DocumentEmpty:
			return &InputMediaDocument{ID: &InputDocumentEmpty{}, TtlSeconds: getValue(attr.TTL, 0), Spoiler: getValue(attr.Spoiler, false)}, nil
		}
	case Photo:
		switch media := media.(type) {
		case *PhotoObj:
			return &InputMediaPhoto{ID: &InputPhotoObj{ID: media.ID, AccessHash: media.AccessHash, FileReference: media.FileReference}, TtlSeconds: getValue(attr.TTL, 0), Spoiler: getValue(attr.Spoiler, false)}, nil
		case *PhotoEmpty:
			return &InputMediaPhoto{ID: &InputPhotoEmpty{}, TtlSeconds: getValue(attr.TTL, 0), Spoiler: getValue(attr.Spoiler, false)}, nil
		}
	case MessageMedia:
		switch media := media.(type) {
		case *MessageMediaPhoto:
			Photo := media.Photo.(*PhotoObj)
			return &InputMediaPhoto{ID: &InputPhotoObj{ID: Photo.ID, AccessHash: Photo.AccessHash, FileReference: Photo.FileReference}, TtlSeconds: getValue(attr.TTL, 0), Spoiler: getValue(attr.Spoiler, false)}, nil
		case *MessageMediaDocument:
			return &InputMediaDocument{ID: &InputDocumentObj{ID: media.Document.(*DocumentObj).ID, AccessHash: media.Document.(*DocumentObj).AccessHash, FileReference: media.Document.(*DocumentObj).FileReference}, TtlSeconds: getValue(attr.TTL, 0), Spoiler: getValue(attr.Spoiler, false)}, nil
		case *MessageMediaGeo:
			return &InputMediaGeoPoint{GeoPoint: &InputGeoPointObj{Lat: media.Geo.(*GeoPointObj).Lat, Long: media.Geo.(*GeoPointObj).Long}}, nil
		case *MessageMediaGame:
			return &InputMediaGame{ID: &InputGameID{ID: media.Game.ID, AccessHash: media.Game.AccessHash}}, nil
		case *MessageMediaContact:
			return &InputMediaContact{FirstName: media.FirstName, LastName: media.LastName, PhoneNumber: media.PhoneNumber, Vcard: media.Vcard}, nil
		case *MessageMediaDice:
			return &InputMediaDice{Emoticon: media.Emoticon}, nil
		case *MessageMediaPoll:
			return convertPoll(media), nil
		case *MessageMediaUnsupported:
			return nil, errors.New("unsupported media type: MessageMediaUnsupported (Maybe you need to update the library)")
		default:
			return nil, errors.New(fmt.Sprintf("unknown media type: %s", reflect.TypeOf(media).String()))
		}
	case InputFile, *InputFile:
		var (
			IsPhoto   bool
			mimeType  string
			fileName  string
			mediaFile InputFile
		)
		switch media := media.(type) {
		case *InputFileObj:
			mimeType, IsPhoto = MimeTypes.MIME(getValue(attr.FileName, media.Name))
			fileName = getValue(attr.FileName, media.Name)
			mediaFile = media
		case *InputFileBig:
			mimeType, IsPhoto = MimeTypes.MIME(getValue(attr.FileName, media.Name))
			fileName = getValue(attr.FileName, media.Name)
			mediaFile = media
		}

		if attr.MimeType != "" {
			mimeType = attr.MimeType
		}

		uploadedPhoto := &InputMediaUploadedPhoto{File: mediaFile, TtlSeconds: getValue(attr.TTL, 0), Spoiler: getValue(attr.Spoiler, false)}
		if IsPhoto && !attr.ForceDocument {
			if attr.Inline {
				return c.uploadToSelf(uploadedPhoto)
			}

			return uploadedPhoto, nil
		} else {
			mediaAttributes := getValueSlice(attr.Attributes, []DocumentAttribute{&DocumentAttributeFilename{FileName: fileName}})
			hasFileName := false
			mediaAttributes, dur, err := GatherVideoMetadata(getValue(attr.FileAbsPath, fileName), mediaAttributes)
			if err != nil {
				c.Log.Debug(errors.Wrap(err, "gathering video metadata"))
			}

			for _, at := range mediaAttributes {
				if _, ok := at.(*DocumentAttributeFilename); ok {
					hasFileName = true
				}
			}

			if thumbnail == nil && !attr.DisableThumb {
				thumbFile, err := c.gatherVideoThumb(getValue(attr.FileAbsPath, fileName), dur)
				if err != nil {
					c.Log.Debug(errors.Wrap(err, "gathering video thumb"))
				} else {
					thumbnail = thumbFile
				}
			}

			if !hasFileName {
				mediaAttributes = append(mediaAttributes, &DocumentAttributeFilename{FileName: fileName})
			}

			uploadedDoc := &InputMediaUploadedDocument{File: mediaFile, MimeType: mimeType, Attributes: mediaAttributes, Thumb: getValueAny(thumbnail, &InputFileObj{}).(InputFile), TtlSeconds: getValue(attr.TTL, 0), Spoiler: getValue(attr.Spoiler, false), ForceFile: getValue(attr.ForceDocument, false)}
			if attr.Inline {
				return c.uploadToSelf(uploadedDoc)
			}

			return uploadedDoc, nil
		}
	case []byte, *io.Reader, *bytes.Buffer, *os.File:
		var uopts *UploadOptions = &UploadOptions{}
		if attr != nil {
			uopts.ProgressManager = attr.ProgressManager
			if attr.FileName != "" {
				uopts.FileName = attr.FileName
			}
		}
		var err error
		mediaFile, err = c.UploadFile(media, uopts)
		if err != nil {
			return nil, err
		}
		goto mediaTypeSwitch
	case nil:
		return nil, errors.New("media given is nil, cannot send nil media")
	}
	return nil, errors.New(fmt.Sprintf("unknown media type: %s", reflect.TypeOf(mediaFile).String()))
}

func (c *Client) uploadToSelf(mediaFile InputMedia) (InputMedia, error) {
	upl, err := c.MessagesUploadMedia("", &InputPeerSelf{}, mediaFile)
	if err != nil {
		return nil, err
	}

	switch upl := upl.(type) {
	case *MessageMediaPhoto:
		switch photo := upl.Photo.(type) {
		case *PhotoObj:
			return &InputMediaPhoto{ID: &InputPhotoObj{ID: photo.ID, AccessHash: photo.AccessHash, FileReference: photo.FileReference}}, nil
		case *PhotoEmpty:
			return &InputMediaPhoto{ID: &InputPhotoEmpty{}}, nil
		}
	case *MessageMediaDocument:
		switch doc := upl.Document.(type) {
		case *DocumentObj:
			return &InputMediaDocument{ID: &InputDocumentObj{ID: doc.ID, AccessHash: doc.AccessHash, FileReference: doc.FileReference}}, nil
		case *DocumentEmpty:
			return &InputMediaDocument{ID: &InputDocumentEmpty{}}, nil
		}
	}

	return nil, errors.New("failed to upload media")
}

func convertPoll(poll *MessageMediaPoll) *InputMediaPoll {
	newPoll := &InputMediaPoll{
		Poll:             poll.Poll,
		Solution:         poll.Results.Solution,
		SolutionEntities: poll.Results.SolutionEntities,
	}

	var correctAnswers [][]byte
	for _, answer := range poll.Results.Results {
		if answer.Correct {
			correctAnswers = append(correctAnswers, answer.Option)
		}
	}

	newPoll.CorrectAnswers = correctAnswers
	return newPoll
}

func GatherVideoMetadata(path string, attrs []DocumentAttribute) ([]DocumentAttribute, int64, error) {
	var dur float64

	if !IsFfmpegInstalled() {
		if strings.HasSuffix(path, "mp4") {
			if r, err := utils.ParseDuration(path); err == nil {
				if IsStreamableFile(path) {

					for _, attr := range attrs {
						if att, ok := attr.(*DocumentAttributeVideo); ok {
							att.Duration = getValue(att.Duration, float64(r/1000))
							return attrs, int64(r / 1000), nil
						}
					}

					attrs = append(attrs, &DocumentAttributeVideo{
						RoundMessage:      false,
						SupportsStreaming: true,
						W:                 512,
						H:                 512,
						Duration:          float64(r / 1000),
					})
				}

				return attrs, int64(r / 1000), nil
			}
		}
	}

	if IsStreamableFile(path) {
		var (
			width  int64
			height int64
		)

		cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration:stream=width:stream=height", "-of", "default=noprint_wrappers=1:nokey=1", path)
		out, err := cmd.Output()

		if err != nil {
			return attrs, 0, errors.Wrap(err, "gathering video metadata")
		}

		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		dur, _ = strconv.ParseFloat(strings.TrimSpace(lines[len(lines)-1]), 64)

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if w, err := strconv.ParseInt(line, 10, 32); err == nil {
				if width == 0 {
					width = w
				} else if height == 0 {
					height = w
					break
				}
			}
		}

		for _, attr := range attrs {
			if att, ok := attr.(*DocumentAttributeVideo); ok {
				att.W = getValue(att.W, int32(width))
				att.H = getValue(att.H, int32(height))
				att.Duration = getValue(att.Duration, dur)
				return attrs, int64(getValue(att.Duration, float64(dur))), nil
			}
		}

		attrs = append(attrs, &DocumentAttributeVideo{
			RoundMessage:      false,
			SupportsStreaming: true,
			W:                 int32(width),
			H:                 int32(height),
			Duration:          dur,
		})
	}

	if filepath.Ext(path) == ".gif" {
		attrs = append(attrs, &DocumentAttributeAnimated{})
	}

	if IsAudioFile(path) {
		var (
			performer string
			title     string
			//	waveform  []byte
		)

		cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format_tags=artist,title", "-of", "json", path)
		out, err := cmd.Output()

		cmd_duration := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
		out_duration, err_duration := cmd_duration.Output()

		if err == nil {
			type ProbeMeta struct {
				Format struct {
					Tags struct {
						Title  string `json:"title"`
						Artist string `json:"artist"`
					} `json:"tags"`
				} `json:"format"`
			}

			var meta ProbeMeta
			if err := json.Unmarshal(out, &meta); err == nil {
				performer = meta.Format.Tags.Artist
				title = meta.Format.Tags.Title
			}

			if performer == "" {
				performer = "Unknown"
			}

			if title == "" {
				title = strings.Replace(filepath.Base(path), filepath.Ext(path), "", 1)
			}
		}

		if err_duration == nil {
			dur, _ = strconv.ParseFloat(strings.TrimSpace(string(out_duration)), 64)
		}

		for _, attr := range attrs {
			if att, ok := attr.(*DocumentAttributeAudio); ok {
				att.Performer = getValue(att.Performer, performer)
				att.Title = getValue(att.Title, title)
				att.Duration = getValue(att.Duration, int32(dur))
				return attrs, int64(getValue(att.Duration, int32(dur))), nil
			}
		}

		attrs = append(attrs, &DocumentAttributeAudio{
			Voice:     false,
			Performer: performer,
			Title:     title,
			Duration:  int32(dur),
		})
	}

	return attrs, int64(dur), nil
}

func IsStreamable(mimeType string) bool {
	switch mimeType {
	case "video/mp4", "video/webm", "video/mpeg", "video/matroska", "video/3gpp", "video/3gpp2", "video/x-matroska", "video/quicktime", "video/x-msvideo", "video/x-ms-wmv", "video/x-m4v", "video/x-flv":
		return true
	default:
		return false
	}
}

func IsStreamableFile(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".mp4", ".webm", ".mpeg", ".mkv", ".3gpp", ".3gpp2", ".x-matroska", ".quicktime", ".x-msvideo", ".x-ms-wmv", ".x-m4v", ".x-flv":
		return true
	default:
		return false
	}
}

func IsAudioFile(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".mp3", ".ogg", ".wav", ".flac", ".m4a", ".alac", ".vorbis", ".opus":
		return true
	default:
		return false
	}
}

func (c *Client) gatherVideoThumb(path string, duration int64) (InputFile, error) {
	if duration == 0 {
		duration = 2
	}

	if IsAudioFile(path) {
		// get embedded album art
		cmd := exec.Command("ffmpeg", "-i", path, "-vf", "scale=200:100:force_original_aspect_ratio=increase,thumbnail", "-frames:v", "1", path+".png")
		_, err := cmd.CombinedOutput()

		if err != nil {
			return nil, errors.Wrap(err, "gathering audio thumb")
		}

		defer os.Remove(path + ".png")
		fi, err := c.UploadFile(path + ".png")

		return fi, err
	}

	// ffmpeg -i input.mp4 -ss 00:00:01.000 -vframes 1 output.png
	getPosition := func(duration int64) int64 {
		if duration <= 10 {
			return (duration / 2) + 1
		} else {
			return int64(rand.Int31n(int32(duration)/2) + 1)
		}
	}

	cmd := exec.Command("ffmpeg", "-ss", strconv.FormatInt(getPosition(duration), 10), "-i", path, "-vframes", "1", path+".png")
	_, err := cmd.Output()

	if err != nil {
		return nil, errors.Wrap(err, "gathering video thumb")
	}

	defer os.Remove(path + ".png")
	fi, err := c.UploadFile(path + ".png")

	return fi, err
}

// ResolveUsername resolves a username to a user or chat Peer
func (c *Client) ResolveUsername(username string, ref ...string) (any, error) {
	var referer = getVariadic(ref, "")

	resp, err := c.ContactsResolveUsername(strings.TrimPrefix(username, "@"), referer)
	if err != nil {
		return nil, errors.Wrap(err, "resolving username")
	}
	c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
	if len(resp.Users) != 0 {
		switch User := resp.Users[0].(type) {
		case *UserObj:
			return User, nil
		default:
			return nil, fmt.Errorf("got wrong response: %s", reflect.TypeOf(resp).String())
		}
	} else if len(resp.Chats) != 0 {
		switch Peer := resp.Chats[0].(type) {
		case *ChatObj:
			return Peer, nil
		case *Channel:
			return Peer, nil
		case *ChannelForbidden:
			return Peer, nil
		case *ChatForbidden:
			return Peer, nil
		default:
			return nil, fmt.Errorf("got wrong response: %s", reflect.TypeOf(resp).String())
		}
	} else {
		return nil, fmt.Errorf("no user or chat has username %s", username)
	}
}

func PackMessages(c *Client, messages []Message) []*NewMessage {
	var (
		packedMessages []*NewMessage
	)

	for _, message := range messages {
		packedMessages = append(packedMessages, packMessage(c, message))
	}

	return packedMessages
}

func packMessage(c *Client, message Message) *NewMessage {
	var (
		m = &NewMessage{}
	)
	switch message := message.(type) {
	case *MessageObj:
		m.ID = message.ID
		m.OriginalUpdate = message
		m.Message = message
		m.Client = c
	case *MessageService:
		m.ID = message.ID
		m.OriginalUpdate = message
		m.Client = c
		m.Message = &MessageObj{
			Out: message.Out, Mentioned: message.Mentioned, MediaUnread: message.MediaUnread, Silent: message.Silent, ID: message.ID, FromID: message.FromID, PeerID: message.PeerID, Date: message.Date, Message: "", Post: message.Post, FromScheduled: false, ReplyTo: message.ReplyTo, FwdFrom: nil, ViaBotID: 0, Legacy: false, EditHide: false, GroupedID: 0, ReplyMarkup: nil,
		}
		m.Action = message.Action
	case *MessageEmpty:
		m.ID = message.ID
		m.OriginalUpdate = message
		m.Client = c
		m.Message = &MessageObj{ID: message.ID, PeerID: message.PeerID, FromID: &PeerUser{}}
		m.Action = &MessageActionEmpty{}
	default:
		return nil
	}

	// Set the chat and channel information
	m.Chat = c.getChat(m.Message.PeerID)
	m.Channel = c.getChannel(m.Message.PeerID)

	if m.Channel != nil && m.Channel.Min {
		if actualChannel, err := c.ChannelsGetChannels([]InputChannel{&InputChannelObj{ChannelID: m.Channel.ID, AccessHash: m.Channel.AccessHash}}); err == nil {
			switch actualChannels := actualChannel.(type) {
			case *MessagesChatsObj:
				if len(actualChannels.Chats) > 0 {
					if channelActual, ok := actualChannels.Chats[0].(*Channel); ok {
						c.Cache.UpdateChannel(channelActual)
						m.Channel = channelActual
					}
				}
			case *MessagesChatsSlice:
				if len(actualChannels.Chats) > 0 {
					if channelActual, ok := actualChannels.Chats[0].(*Channel); ok {
						c.Cache.UpdateChannel(channelActual)
						m.Channel = channelActual
					}
				}
			}
		}
	} else if m.Channel != nil && !m.Channel.Min {
		c.Cache.UpdateChannel(m.Channel)
	}

	if m.Message.Out {
		m.Sender = c.Me()
	} else {
		if m.Message.FromID != nil {
			m.Sender = c.getSender(m.Message.FromID)
			if m.Sender != nil && m.Sender.Min {
				if actualUser, err := c.UsersGetUsers([]InputUser{&InputUserFromMessage{
					Peer:   c.getInputPeer(m.Message.PeerID),
					MsgID:  m.ID,
					UserID: m.Sender.ID,
				}}); err == nil && len(actualUser) > 0 {
					if userActual, ok := actualUser[0].(*UserObj); ok {
						c.Cache.UpdateUser(userActual)
						m.Sender = userActual
					}
				}
			} else if m.Sender != nil && !m.Sender.Min {
				c.Cache.UpdateUser(m.Sender)
			}
		} else {
			m.Sender = c.getSender(m.Message.PeerID)
		}
	}

	if (m.Channel != nil && m.Sender != nil) && (m.Sender.ID == m.Channel.ID) {
		m.SenderChat = c.getChannel(m.Message.FromID)
	} else {
		m.SenderChat = &Channel{}
	}

	if m.Chat == nil && m.Sender != nil {
		m.Chat = &ChatObj{ID: m.Sender.ID, Title: m.Sender.FirstName} // private user/bot chat
	}

	m.Peer = c.getInputPeer(m.Message.PeerID)
	if m.IsMedia() {
		FileID := PackBotFileID(m.Media())
		m.File = &CustomFile{
			FileID: FileID,
			Name:   GetFileName(m.Media()),
			Size:   getFileSize(m.Media()),
			Ext:    getFileExt(m.Media()),
		}
	}
	return m
}

func packDeleteMessage(c *Client, delete Update) *DeleteMessage {
	var deleteMessage *DeleteMessage = &DeleteMessage{}
	switch d := delete.(type) {
	case *UpdateDeleteMessages:
		deleteMessage.Messages = d.Messages
		deleteMessage.ChannelID = 0
	case *UpdateDeleteChannelMessages:
		deleteMessage.Messages = d.Messages
		deleteMessage.ChannelID = d.ChannelID
	}

	deleteMessage.Client = c
	return deleteMessage
}

func packJoinRequest(c *Client, update *UpdatePendingJoinRequests) *JoinRequestUpdate {
	var (
		jr = &JoinRequestUpdate{}
	)
	jr.Client = c
	jr.OriginalUpdate = update
	jr.Channel, _ = c.GetChannel(c.GetPeerID(update.Peer))

	for _, userID := range update.RecentRequesters {
		if user, err := c.GetUser(userID); err == nil {
			jr.Users = append(jr.Users, user)
		} else {
			c.Log.Debug(errors.Wrapf(err, "getting user %d for join request", userID))
		}
	}

	jr.PendingCount = update.RequestsPending
	return jr
}

func packInlineQuery(c *Client, query *UpdateBotInlineQuery) *InlineQuery {
	var (
		iq = &InlineQuery{}
	)
	iq.QueryID = query.QueryID
	iq.Query = query.Query
	iq.Offset = query.Offset
	iq.Client = c
	iq.Sender, _ = c.GetUser(query.UserID)
	iq.SenderID = query.UserID
	iq.OriginalUpdate = query
	return iq
}

func packInlineSend(c *Client, query *UpdateBotInlineSend) *InlineSend {
	var (
		is = &InlineSend{}
	)

	is.ID = query.ID
	is.MsgID = query.MsgID
	is.Client = c
	is.Sender, _ = c.GetUser(query.UserID)
	is.SenderID = query.UserID
	is.OriginalUpdate = query

	return is
}

func packCallbackQuery(c *Client, query *UpdateBotCallbackQuery) *CallbackQuery {
	var (
		cq = &CallbackQuery{}
	)
	cq.QueryID = query.QueryID
	cq.Data = query.Data
	cq.Client = c
	cq.Sender, _ = c.GetUser(query.UserID)
	cq.Chat = c.getChat(query.Peer)
	cq.Channel = c.getChannel(query.Peer)
	cq.OriginalUpdate = query
	cq.Peer = query.Peer
	cq.MessageID = query.MsgID
	cq.SenderID = query.UserID
	if cq.Channel != nil {
		cq.ChatID = cq.Channel.ID
	} else if cq.Chat != nil {
		cq.ChatID = cq.Chat.ID
	} else {
		cq.ChatID = c.GetPeerID(query.Peer)
	}
	return cq
}

func packInlineCallbackQuery(c *Client, query *UpdateInlineBotCallbackQuery) *InlineCallbackQuery {
	var (
		cq = &InlineCallbackQuery{}
	)
	cq.QueryID = query.QueryID
	cq.Data = query.Data
	cq.Client = c
	cq.Sender, _ = c.GetUser(query.UserID)
	cq.OriginalUpdate = query
	cq.Data = query.Data
	cq.GameShortName = query.GameShortName
	cq.MsgID = query.MsgID
	cq.SenderID = query.UserID
	return cq
}

func packChannelParticipant(c *Client, update *UpdateChannelParticipant) *ParticipantUpdate {
	var (
		pu = &ParticipantUpdate{}
	)
	pu.Client = c
	pu.OriginalUpdate = update
	pu.Channel = c.getChannel(&PeerChannel{ChannelID: update.ChannelID})
	pu.User, _ = c.GetUser(update.UserID)
	pu.Actor, _ = c.GetUser(update.ActorID)
	pu.Old = update.PrevParticipant
	pu.New = update.NewParticipant
	pu.Date = update.Date
	pu.Invite = update.Invite
	return pu
}

func (c *Client) getSender(FromID Peer) *UserObj {
	if FromID == nil {
		return &UserObj{}
	}
	switch FromID := FromID.(type) {
	case *PeerUser:
		u, err := c.GetUser(FromID.UserID)
		if err == nil {
			return u
		}
	case *PeerChat:
		u, err := c.GetChat(FromID.ChatID)
		if err == nil {
			return &UserObj{ID: FromID.ChatID, FirstName: u.Title, LastName: "", Username: "", Phone: "", AccessHash: 0, Photo: nil, Status: nil, Bot: false, Verified: false, Restricted: false}
		}
	case *PeerChannel:
		u, err := c.GetChannel(FromID.ChannelID)
		if err == nil {
			return &UserObj{ID: FromID.ChannelID, AccessHash: u.AccessHash, Username: u.Username, FirstName: u.Title, LastName: "", Phone: "", Bot: false, Verified: false, LangCode: ""}
		}
	}
	return &UserObj{}
}

func (c *Client) getChat(PeerID Peer) *ChatObj {
	switch PeerID := PeerID.(type) {
	case *PeerChat:
		chat, err := c.GetChat(PeerID.ChatID)
		if err == nil {
			return chat
		}
	}
	return nil
}

func (c *Client) getChannel(PeerID Peer) *Channel {
	switch PeerID := PeerID.(type) {
	case *PeerChannel:
		channel, err := c.GetChannel(PeerID.ChannelID)
		if err == nil {
			return channel
		}
	}
	return nil
}

func (c *Client) getInputPeer(PeerID Peer) InputPeer {
	if PeerID == nil {
		return nil
	}
	switch PeerID := PeerID.(type) {
	case *PeerUser:
		u, err := c.GetUser(PeerID.UserID)
		if err == nil {
			return &InputPeerUser{UserID: PeerID.UserID, AccessHash: u.AccessHash}
		}
	case *PeerChat:
		return &InputPeerChat{ChatID: PeerID.ChatID}
	case *PeerChannel:
		u, err := c.GetChannel(PeerID.ChannelID)
		if err == nil {
			return &InputPeerChannel{ChannelID: PeerID.ChannelID, AccessHash: u.AccessHash}
		}
	}
	return nil
}

func (c *Client) getPeer(PeerID InputPeer) Peer {
	if PeerID == nil {
		return nil
	}
	switch PeerID := PeerID.(type) {
	case *InputPeerUser:
		return &PeerUser{UserID: PeerID.UserID}
	case *InputPeerChat:
		return &PeerChat{ChatID: PeerID.ChatID}
	case *InputPeerChannel:
		return &PeerChannel{ChannelID: PeerID.ChannelID}
	}
	return nil
}

func GetInputCheckPassword(password string, accountPassword *AccountPassword) (InputCheckPasswordSRP, error) {
	alg := accountPassword.CurrentAlgo
	current, ok := alg.(*PasswordKdfAlgoSHA256SHA256Pbkdf2Hmacsha512Iter100000SHA256ModPow)

	if !ok {
		return nil, errors.New("invalid CurrentAlgo type")
	}

	mp := &ige.ModPow{
		Salt1: current.Salt1,
		Salt2: current.Salt2,
		G:     current.G,
		P:     current.P,
	}

	res, err := GetInputCheckPasswordAlgo(password, accountPassword.SRPB, mp)
	if err != nil {
		return nil, errors.Wrap(err, "processing password")
	}

	if res == nil {
		return &InputCheckPasswordEmpty{}, nil
	}

	return &InputCheckPasswordSRPObj{
		SRPID: accountPassword.SRPID,
		A:     res.GA,
		M1:    res.M1,
	}, nil
}

// GetInputCheckPassword returns the input check password for the given password and salt.
// all the internal functions are in internal/ige, send pr if you want to use them directly
// https://core.telegram.org/api/srp#checking-the-password-with-srp
func GetInputCheckPasswordAlgo(password string, srpB []byte, mp *ige.ModPow) (*ige.SrpAnswer, error) {
	return ige.GetInputCheckPassword(password, srpB, mp, ige.RandomBytes(randombyteLen))
}

func ComputeDigest(algo *PasswordKdfAlgoSHA256SHA256Pbkdf2Hmacsha512Iter100000SHA256ModPow, password string) []byte {
	hash := ige.PasswordHash2([]byte(password), algo.Salt1, algo.Salt2)
	value := ige.BigExp(big.NewInt(int64(algo.G)), ige.BytesToBig(hash), ige.BytesToBig(algo.P))
	return ige.Pad256(value.Bytes())
}

func (c *Client) Stringify(object any) string {
	return c.JSON(object)
}

// easy wrapper for json.MarshalIndent, returns string
func (c *Client) JSON(object any, nointent ...any) string {
	if len(nointent) > 0 {
		switch _noi := nointent[0].(type) {
		case bool:
			if _noi {
				data, err := json.Marshal(object)
				if err != nil {
					return fmt.Sprintf("marshal: %s", err)
				}
				return string(data)
			}
		}
	}

	data, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return fmt.Sprintf("marshal: %s", err)
	}
	return string(data)
}
