package telegram

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/pkg/errors"
)

func (c *Client) GetMe() (*UserObj, error) {
	resp, err := c.UsersGetFullUser(&InputUserSelf{})
	if err != nil {
		return nil, errors.Wrap(err, "getting user")
	}
	user, ok := resp.Users[0].(*UserObj)
	if !ok {
		return nil, errors.New("got wrong response: " + reflect.TypeOf(resp).String())
	}
	return user, nil
}

func (c *Client) ResolveUsername(username string) (interface{}, error) {
	resp, err := c.ContactsResolveUsername(username)
	if err != nil {
		return nil, errors.Wrap(err, "resolving username")
	}
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

func (c *Client) GetPeerID(peer interface{}) (int64, error) {
	switch Peer := peer.(type) {
	case *InputPeerChat:
		return Peer.ChatID, nil
	case *InputPeerChannel:
		return Peer.ChannelID, nil
	case *InputPeerUser:
		return Peer.UserID, nil
	case *ChatObj:
		return Peer.ID, nil
	case *Channel:
		return Peer.ID, nil
	case *UserObj:
		return Peer.ID, nil
	case int64:
		return Peer, nil
	default:
		return 0, errors.New("unknown peer type")
	}
}

type SendOptions struct {
	ReplyID     int32
	Caption     interface{}
	ParseMode   string
	Silent      bool
	LinkPreview bool
	ReplyMarkup ReplyMarkup
	ClearDraft  bool
	NoForwards  bool
}

func (c *Client) GetSendablePeer(Peer interface{}) (InputPeer, error) {
	switch Peer := Peer.(type) {
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
	case *ChatObj:
		return &InputPeerChat{ChatID: Peer.ID}, nil
	case *Channel:
		return &InputPeerChannel{ChannelID: Peer.ID, AccessHash: Peer.AccessHash}, nil
	case *UserObj:
		return &InputPeerUser{UserID: Peer.ID, AccessHash: Peer.AccessHash}, nil
	case int64:
		PeerEntity := c.GetInputPeer(Peer)
		if PeerEntity == nil {
			return nil, errors.New("unknown peer")
		}
		return PeerEntity, nil
	case int32:
		PeerEntity := c.GetInputPeer(int64(Peer))
		if PeerEntity == nil {
			return nil, errors.New("unknown peer")
		}
		return PeerEntity, nil
	case int:
		PeerEntity := c.GetInputPeer(int64(Peer))
		if PeerEntity == nil {
			return nil, errors.New("unknown peer")
		}
		return PeerEntity, nil
	case string:
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
			return nil, errors.New("unknown peer type")
		}
	default:
		return nil, errors.New("failed to get sendable peer")
	}
}

func (c *Client) GetSendableMedia(media interface{}) (InputMedia, error) {
	switch media := media.(type) {
	case string:
		if _, err := url.ParseRequestURI(media); err != nil {
			for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
				if strings.HasSuffix(media, ext) {
					return &InputMediaPhotoExternal{URL: media}, nil
				}
			}
			return &InputMediaDocumentExternal{URL: media}, nil
		} else {
			// local file, soon
			return nil, err
		}
	case InputMedia:
		return media, nil
	case MessageMedia:
		switch media := media.(type) {
		case *MessageMediaPhoto:
			Photo := media.Photo.(*PhotoObj)
			return &InputMediaPhoto{ID: &InputPhotoObj{ID: Photo.ID, AccessHash: Photo.AccessHash, FileReference: Photo.FileReference}}, nil
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
	default:
		return nil, errors.New(fmt.Sprintf("unknown media type: %s", reflect.TypeOf(media).String()))
	}
}

func (c *Client) SendMessage(peerID interface{}, TextObj interface{}, Opts ...*SendOptions) (*MessageObj, error) {
	var options SendOptions
	if len(Opts) > 0 {
		options = *Opts[0]
	}
	if options.ParseMode == "" {
		options.ParseMode = c.ParseMode
	}
	var e []MessageEntity
	var Text string
	switch TextObj := TextObj.(type) {
	case string:
		Text, e = c.ParseEntity(TextObj, options.ParseMode)
	case *Entity:
		Text = TextObj.GetText()
		e = TextObj.Entities()
	case MessageMedia:
		return c.SendMedia(peerID, TextObj, Opts[0])
	case InputMedia:
		return c.SendMedia(peerID, TextObj, Opts[0])
	}
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	Update, err := c.MessagesSendMessage(&MessagesSendMessageParams{
		Peer:         PeerToSend,
		Message:      Text,
		RandomID:     GenRandInt(),
		ReplyToMsgID: options.ReplyID,
		Entities:     e,
		ReplyMarkup:  options.ReplyMarkup,
		NoWebpage:    options.LinkPreview,
		Silent:       options.Silent,
		ClearDraft:   options.ClearDraft,
	})
	if err != nil {
		return nil, err
	}
	return ProcessMessageUpdate(Update), err
}

func (c *Client) EditMessage(peerID interface{}, MsgID int32, TextObj interface{}, Opts ...*SendOptions) (*MessageObj, error) {
	var options SendOptions
	if len(Opts) > 0 {
		options = *Opts[0]
	}
	if options.ParseMode == "" {
		options.ParseMode = c.ParseMode
	}
	var err error
	var e []MessageEntity
	var Text string
	switch TextObj := TextObj.(type) {
	case string:
		Text, e = c.ParseEntity(TextObj, options.ParseMode)
	case *Entity:
		Text = TextObj.GetText()
		e = TextObj.Entities()
	}
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	Update, err := c.MessagesEditMessage(&MessagesEditMessageParams{
		Peer:        PeerToSend,
		Message:     Text,
		ID:          MsgID,
		Entities:    e,
		ReplyMarkup: nil,
		NoWebpage:   options.LinkPreview,
	})
	if err != nil {
		return nil, err
	}
	return ProcessMessageUpdate(Update), err
}

func (c *Client) DeleteMessage(peerID interface{}, MsgIDs ...int32) error {
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return err
	}
	PeerChannel, ok := PeerToSend.(*InputPeerChannel)
	if !ok {
		return errors.New("peer is not a channel")
	}
	_, err = c.ChannelsDeleteMessages(&InputChannelObj{ChannelID: PeerChannel.ChannelID, AccessHash: PeerChannel.AccessHash}, MsgIDs)
	return err
}

func (c *Client) ForwardMessage(peerID interface{}, MsgID int32) {}

func (c *Client) SendMedia(peerID interface{}, Media interface{}, Opts ...*SendOptions) (*MessageObj, error) {
	var options SendOptions
	if len(Opts) > 0 {
		options = *Opts[0]
	}
	if options.ParseMode == "" {
		options.ParseMode = c.ParseMode
	}
	var Caption string
	var e []MessageEntity
	switch Capt := options.Caption.(type) {
	case string:
		Caption, e = c.ParseEntity(Capt, options.ParseMode)
	case *Entity:
		Caption = Capt.GetText()
		e = Capt.Entities()
	}
	PeerToSend, err := c.GetSendablePeer(peerID)
	if err != nil {
		return nil, err
	}
	MediaFile, err := c.GetSendableMedia(Media)
	if err != nil {
		return nil, err
	}
	Update, err := c.MessagesSendMedia(&MessagesSendMediaParams{
		Peer:         PeerToSend,
		Media:        MediaFile,
		Message:      Caption,
		RandomID:     GenRandInt(),
		ReplyToMsgID: options.ReplyID,
		Entities:     e,
		ReplyMarkup:  options.ReplyMarkup,
		Silent:       options.Silent,
		ClearDraft:   options.ClearDraft,
		Noforwards:   options.NoForwards,
	})
	fmt.Println(Update)
	return nil, err
}

func ProcessMessageUpdate(update Updates) *MessageObj {
	if update == nil {
		return nil
	}
	switch update := update.(type) {
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
	}
	return nil
}
