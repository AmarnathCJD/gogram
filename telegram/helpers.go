package telegram

import (
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

var (
	DataCenters = map[int]string{
		1: "149.154.175.58:443",
		2: "149.154.167.50:443",
		3: "149.154.175.100:443",
		4: "149.154.167.91:443",
		5: "91.108.56.151:443",
	}
)

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func GetHostIp(dcID int) string {
	if ip, ok := DataCenters[dcID]; ok {
		return ip
	}
	panic("Invalid Data Center ID")
}

func getStr(a string, b string) string {
	if a == "" {
		return b
	}
	return a
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

func (c *Client) getMultiMedia(m interface{}, attrs *CustomAttrs) ([]*InputSingleMedia, error) {
	var media []*InputSingleMedia
	var inputMedia []InputMedia
	switch m := m.(type) {
	case *InputSingleMedia:
		media = append(media, m)
	case []*InputSingleMedia:
		media = m
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
		switch m := m.(type) {
		case *InputMediaUploadedPhoto, *InputMediaUploadedDocument, *InputMediaPhotoExternal, *InputMediaDocumentExternal:
			uploadedMedia, err := c.MessagesUploadMedia(&InputPeerSelf{}, m) // Have to Upload Media only if not Cached
			if err != nil {
				return nil, err
			}
			inputUploadedMedia, err := c.getSendableMedia(uploadedMedia, attrs)
			if err != nil {
				return nil, err
			}
			media = append(media, &InputSingleMedia{
				Media:    inputUploadedMedia,
				RandomID: GenRandInt(),
			})
		default:
			media = append(media, &InputSingleMedia{
				Media:    m,
				RandomID: GenRandInt(),
			})
		}
	}
	return media, nil
}

func processUpdates(updates Updates) []*MessageObj {
	var messages []*MessageObj
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
			}
		}
	}
	return messages
}

func processUpdate(upd Updates) *MessageObj {
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
			Out:       update.Out,
			Media:     update.Media,
			Entities:  update.Entities,
			TtlPeriod: update.TtlPeriod,
		}
	case *UpdatesObj:
		upd := update.Updates[0]
		if len(update.Updates) == 2 {
			upd = update.Updates[1]
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
		case *UpdateMessageID:
			return &MessageObj{
				ID: upd.ID,
			}
		default:
			fmt.Println("unknown update type", reflect.TypeOf(upd).String())
		}
	case *UpdateShortMessage:
		return &MessageObj{Out: update.Out, ID: update.ID, PeerID: &PeerUser{}, Date: update.Date, Entities: update.Entities, TtlPeriod: update.TtlPeriod}
	case *UpdateShortChatMessage:
		return &MessageObj{Out: update.Out, ID: update.ID, PeerID: &PeerChat{}, Date: update.Date, Entities: update.Entities, TtlPeriod: update.TtlPeriod}
	case *UpdateShort:
		upd = &UpdatesObj{Updates: []Update{update.Update}}
		goto updateTypeSwitch
	default:
		fmt.Println("unknown update type", reflect.TypeOf(update).String())
	}
	return nil
}

func (c *Client) GetSendablePeer(PeerID interface{}) (InputPeer, error) {
PeerSwitch:
	switch Peer := PeerID.(type) {
	case *PeerUser:
		PeerEntity, err := c.GetPeerUser(Peer.UserID)
		if err != nil {
			return nil, err
		}
		return &InputPeerUser{UserID: Peer.UserID, AccessHash: PeerEntity.AccessHash}, nil
	case *PeerChat:
		return &InputPeerChat{ChatID: Peer.ChatID}, nil
	case *PeerChannel:
		PeerEntity, err := c.GetPeerChannel(Peer.ChannelID)
		if err != nil {
			return nil, err
		}
		return &InputPeerChannel{ChannelID: Peer.ChannelID, AccessHash: PeerEntity.AccessHash}, nil
	case *InputPeerChat:
		return Peer, nil
	case *InputPeerChannel:
		PeerEntity, err := c.GetPeerChannel(Peer.ChannelID)
		if err != nil {
			return nil, err
		}
		return &InputPeerChannel{ChannelID: PeerEntity.ID, AccessHash: PeerEntity.AccessHash}, nil
	case *InputPeerUser:
		PeerEntity, err := c.GetPeerUser(Peer.UserID)
		if err != nil {
			return nil, err
		}
		return &InputPeerUser{UserID: PeerEntity.ID, AccessHash: PeerEntity.AccessHash}, nil
	case *InputPeer:
		return *Peer, nil
		// TODO: Add more types
	case *InputUserObj:
		return &InputPeerUser{UserID: Peer.UserID, AccessHash: Peer.AccessHash}, nil
	case *ChatObj:
		return &InputPeerChat{ChatID: Peer.ID}, nil
	case *Channel:
		return &InputPeerChannel{ChannelID: Peer.ID, AccessHash: Peer.AccessHash}, nil
	case *UserObj:
		return &InputPeerUser{UserID: Peer.ID, AccessHash: Peer.AccessHash}, nil
	case int64, int32, int:
		PeerEntity, err := c.GetInputPeer(Peer.(int64))
		if PeerEntity == nil {
			return nil, err
		}
		return PeerEntity, nil
	case string:
		if i, err := strconv.ParseInt(Peer, 10, 64); err == nil {
			PeerID = i
			goto PeerSwitch
		}
		if Peer == "me" {
			return &InputPeerSelf{}, nil
		}
		PeerEntity, err := c.ResolveUsername(Peer)
		if err != nil {
			return nil, err
		}
		switch PeerEntity := PeerEntity.(type) {
		case *ChatObj:
			return &InputPeerChat{ChatID: PeerEntity.ID}, nil
		case *Channel:
			return &InputPeerChannel{ChannelID: PeerEntity.ID, AccessHash: PeerEntity.AccessHash}, nil
		case *UserObj:
			return &InputPeerUser{UserID: PeerEntity.ID, AccessHash: PeerEntity.AccessHash}, nil
		default:
			return nil, errors.New(fmt.Sprintf("unknown peer type %s", reflect.TypeOf(PeerEntity).String()))
		}
	default:
		return nil, errors.New("failed to get sendable peer")
	}
}

func (c *Client) GetPeerID(Peer Peer) int64 {
	switch Peer := Peer.(type) {
	case *PeerChat:
		return Peer.ChatID
	case *PeerChannel:
		return Peer.ChannelID
	case *PeerUser:
		return Peer.UserID
	default:
		return 0
	}
}

func (c *Client) getSendableMedia(mediaFile interface{}, attr *CustomAttrs) (InputMedia, error) {
mediaTypeSwitch:
	switch media := mediaFile.(type) {
	case string:
		if IsUrl(media) {
			if _, isImage := resolveMimeType(media); isImage {
				return &InputMediaPhotoExternal{URL: media}, nil
			}
			return &InputMediaDocumentExternal{URL: media, TtlSeconds: getValue(attr.TTL, 0).(int32)}, nil
		} else {
			if _, err := os.Stat(media); err == nil {
				mediaFile, err = c.UploadFile(media)
				if err != nil {
					return nil, err
				}
				goto mediaTypeSwitch
			} else if err != nil {
				return nil, err
			}
		}
	case InputMedia:
		return media, nil
	case MessageMedia:
		switch media := media.(type) {
		case *MessageMediaPhoto:
			Photo := media.Photo.(*PhotoObj)
			return &InputMediaPhoto{ID: &InputPhotoObj{ID: Photo.ID, AccessHash: Photo.AccessHash, FileReference: Photo.FileReference}, TtlSeconds: getValue(attr.TTL, 0).(int32)}, nil
		case *MessageMediaDocument:
			return &InputMediaDocument{ID: &InputDocumentObj{ID: media.Document.(*DocumentObj).ID, AccessHash: media.Document.(*DocumentObj).AccessHash, FileReference: media.Document.(*DocumentObj).FileReference}}, nil
		case *MessageMediaGeo:
			return &InputMediaGeoPoint{GeoPoint: &InputGeoPointObj{Lat: media.Geo.(*GeoPointObj).Lat, Long: media.Geo.(*GeoPointObj).Long}}, nil
		case *MessageMediaGame:
			return &InputMediaGame{ID: &InputGameID{ID: media.Game.ID, AccessHash: media.Game.AccessHash}}, nil
		case *MessageMediaContact:
			return &InputMediaContact{FirstName: media.FirstName, LastName: media.LastName, PhoneNumber: media.PhoneNumber, Vcard: media.Vcard}, nil
		case *MessageMediaDice:
			return &InputMediaDice{Emoticon: media.Emoticon}, nil
		default:
			return nil, errors.New(fmt.Sprintf("unknown media type: %s", reflect.TypeOf(media).String()))
		}
	case InputFile:
		var (
			IsPhoto  bool
			mimeType string
			fileName string
		)
		switch media := media.(type) {
		case *InputFileObj:
			mimeType, IsPhoto = resolveMimeType(getValue(attr.FileName, media.Name).(string))
			fileName = getValue(attr.FileName, media.Name).(string)
		case *InputFileBig:
			mimeType, IsPhoto = resolveMimeType(getValue(attr.FileName, media.Name).(string))
			fileName = getValue(attr.FileName, media.Name).(string)
		}
		if IsPhoto {
			return &InputMediaUploadedPhoto{File: media}, nil
		} else {
			var Attributes = getValue(attr.Attributes, []DocumentAttribute{&DocumentAttributeFilename{FileName: fileName}}).([]DocumentAttribute)
			if len(Attributes) == 0 {
				Attributes = []DocumentAttribute{&DocumentAttributeFilename{FileName: fileName}}
			}
			return &InputMediaUploadedDocument{File: media, MimeType: mimeType, Attributes: Attributes, Thumb: getValue(attr.Thumb, &InputFileObj{}).(InputFile), TtlSeconds: getValue(attr.TTL, 0).(int32)}, nil
		}
	case []byte:
		var err error
		mediaFile, err = c.UploadFile(media)
		if err != nil {
			return nil, err
		}
		goto mediaTypeSwitch
	}
	return nil, errors.New(fmt.Sprintf("unknown media type: %s", reflect.TypeOf(mediaFile).String()))
}

func (c *Client) ResolveUsername(username string) (interface{}, error) {
	resp, err := c.ContactsResolveUsername(strings.TrimPrefix(username, "@"))
	if err != nil {
		return nil, errors.Wrap(err, "resolving username")
	}
	c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
	if len(resp.Users) != 0 {
		return resp.Users[0].(*UserObj), nil
	} else if len(resp.Chats) != 0 {
		switch Peer := resp.Chats[0].(type) {
		case *ChatObj:
			return Peer, nil
		case *Channel:
			return Peer, nil
		default:
			return nil, fmt.Errorf("got wrong response: %s", reflect.TypeOf(resp).String())
		}
	} else {
		return nil, fmt.Errorf("no user or chat has username %s", username)
	}
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
		m.Message = &MessageObj{ID: message.ID, PeerID: message.PeerID}
		m.Action = &MessageActionEmpty{}
	}
	if m.Message.FromID != nil {
		m.Sender = c.getSender(m.Message.FromID)
	} else {
		m.Sender = c.getSender(m.Message.PeerID)
	}
	m.Chat = c.getChat(m.Message.PeerID)
	m.Channel = c.getChannel(m.Message.PeerID)
	if m.Channel != nil && (m.Sender.ID == m.Channel.ID) {
		m.SenderChat = c.getChannel(m.Message.FromID)
	} else {
		m.SenderChat = &Channel{}
	}
	m.Peer = c.getPeer(m.Message.PeerID)
	return m
}

func (c *Client) getSender(FromID Peer) *UserObj {
	if FromID == nil {
		return &UserObj{}
	}
	switch FromID := FromID.(type) {
	case *PeerUser:
		u, err := c.Cache.GetUser(FromID.UserID)
		if err == nil {
			return u
		}
	case *PeerChat:
		u, err := c.Cache.GetChat(FromID.ChatID)
		if err == nil {
			return &UserObj{ID: FromID.ChatID, FirstName: u.Title, LastName: "", Username: "", Phone: "", AccessHash: 0, Photo: nil, Status: nil, Bot: false, Verified: false, Restricted: false}
		}
	case *PeerChannel:
		u, err := c.Cache.GetChannel(FromID.ChannelID)
		if err == nil {
			return &UserObj{ID: FromID.ChannelID, AccessHash: u.AccessHash, Username: u.Username, FirstName: u.Title, LastName: "", Phone: "", Bot: false, Verified: false, LangCode: ""}
		}
	}
	return &UserObj{}
}

func (c *Client) getChat(PeerID Peer) *ChatObj {
	switch PeerID := PeerID.(type) {
	case *PeerChat:
		chat, err := c.Cache.GetChat(PeerID.ChatID)
		if err == nil {
			return chat
		}
	}
	return nil
}

func (c *Client) getChannel(PeerID Peer) *Channel {
	switch PeerID := PeerID.(type) {
	case *PeerChannel:
		channel, err := c.Cache.GetChannel(PeerID.ChannelID)
		if err == nil {
			return channel
		}
	}
	return nil
}

func (c *Client) getPeer(PeerID Peer) InputPeer {
	if PeerID == nil {
		return nil
	}
	switch PeerID := PeerID.(type) {
	case *PeerUser:
		u, err := c.Cache.GetUser(PeerID.UserID)
		if err == nil {
			return &InputPeerUser{UserID: PeerID.UserID, AccessHash: u.AccessHash}
		}
	case *PeerChat:
		return &InputPeerChat{ChatID: PeerID.ChatID}
	case *PeerChannel:
		u, err := c.Cache.GetChannel(PeerID.ChannelID)
		if err == nil {
			return &InputPeerChannel{ChannelID: PeerID.ChannelID, AccessHash: u.AccessHash}
		}
	}
	return nil
}

func packInlineQuery(c *Client, query *UpdateBotInlineQuery) *InlineQuery {
	var (
		iq = &InlineQuery{}
	)
	iq.QueryID = query.QueryID
	iq.Query = query.Query
	iq.Offset = query.Offset
	iq.Client = c
	iq.Sender, _ = c.Cache.GetUser(query.UserID)
	iq.SenderID = query.UserID
	iq.OriginalUpdate = query
	return iq
}

func packCallbackQuery(c *Client, query *UpdateBotCallbackQuery) *CallbackQuery {
	var (
		cq = &CallbackQuery{}
	)
	cq.QueryID = query.QueryID
	cq.Data = query.Data
	cq.Client = c
	cq.Sender, _ = c.Cache.GetUser(query.UserID)
	cq.Chat = c.getChat(query.Peer)
	cq.Channel = c.getChannel(query.Peer)
	cq.OriginalUpdate = query
	cq.Peer = query.Peer
	cq.MessageID = query.MsgID
	cq.SenderID = query.UserID
	if cq.Channel != nil {
		cq.ChatID = cq.Channel.ID
	} else {
		cq.ChatID = cq.Chat.ID
	}
	return cq
}

func GetInputCheckPassword(password string, accountPassword *AccountPassword) (InputCheckPasswordSRP, error) {
	alg := accountPassword.CurrentAlgo
	current, ok := alg.(*PasswordKdfAlgoSHA256SHA256Pbkdf2Hmacsha512Iter100000SHA256ModPow)

	if !ok {
		return nil, errors.New("invalid CurrentAlgo type")
	}

	mp := &ModPow{
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
