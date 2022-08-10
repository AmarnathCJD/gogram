package telegram

import (
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"strings"

	"github.com/pkg/errors"
)

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

func (c *Client) GetSendableMedia(media interface{}) (InputMedia, error) {
	switch media := media.(type) {
	case string:
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
			if strings.HasSuffix(media, ext) {
				return &InputMediaPhotoExternal{URL: media}, nil
			}
		}
		return &InputMediaDocumentExternal{URL: media}, nil

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

func (c *Client) ResolveUsername(username string) (interface{}, error) {
	resp, err := c.ContactsResolveUsername(username)
	if err != nil {
		return nil, errors.Wrap(err, "resolving username")
	}
	go func() { c.Cache.UpdatePeersToCache(resp.Users, resp.Chats) }()
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

func PackMessage(client *Client, message *MessageObj) *NewMessage {
	var Chat *ChatObj
	var Sender *UserObj
	var SenderChat *ChatObj
	var Channel *Channel
	if message.PeerID != nil {
		switch Peer := message.PeerID.(type) {
		case *PeerUser:
			Chat = &ChatObj{
				ID: Peer.UserID,
			}
		case *PeerChat:
			Chat, _ = client.GetPeerChat(Peer.ChatID)
		case *PeerChannel:
			Channel, _ = client.GetPeerChannel(Peer.ChannelID)
			Chat = &ChatObj{
				ID:    Channel.ID,
				Title: Channel.Title,
			}
		}
	}
	if message.FromID != nil {
		switch From := message.FromID.(type) {
		case *PeerUser:
			Sender, _ = client.GetPeerUser(From.UserID)
		case *PeerChat:
			SenderChat, _ = client.GetPeerChat(From.ChatID)
			Sender = &UserObj{
				ID: SenderChat.ID,
			}
		case *PeerChannel:
			Channel, _ = client.GetPeerChannel(From.ChannelID)
			Sender = &UserObj{
				ID: Channel.ID,
			}
		}
	}
	return &NewMessage{
		Client:         client,
		OriginalUpdate: message,
		Chat:           Chat,
		Sender:         Sender,
		SenderChat:     SenderChat,
		Channel:        Channel,
		ID:             message.ID,
	}
}
