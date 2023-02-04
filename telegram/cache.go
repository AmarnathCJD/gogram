package telegram

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"

	aes "github.com/amarnathcjd/gogram/internal/aes_ige"
	"github.com/amarnathcjd/gogram/internal/utils"
)

const (
	// CacheUpdateInterval is the interval in seconds at which the cache is updated
	CacheUpdateInterval = 60
	// AesKey is the key used to encrypt the cache file
	AesKey = "12345678901234567890123456789012"
)

type CACHE struct {
	chats      map[int64]*ChatObj
	users      map[int64]*UserObj
	channels   map[int64]*Channel
	InputPeers *InputPeerCache `json:"input_peers,omitempty"`
	logger     *utils.Logger
}

type InputPeerCache struct {
	InputChannels map[int64]*InputPeerChannel `json:"channels,omitempty"`
	InputUsers    map[int64]*InputPeerUser    `json:"users,omitempty"`
	InputChats    map[int64]*InputPeerChat    `json:"chats,omitempty"`
}

func (c *CACHE) periodicallyFlushToFile() {
	for {
		time.Sleep(time.Duration(CacheUpdateInterval) * time.Second)
		c.flushToFile()
	}
}

func (c *CACHE) flushToFile() {
	f, err := os.OpenFile("cache.journal", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Println(err)
	}
	// encode to aes encrypted json file
	var b []byte
	b, err = json.Marshal(c)
	if err != nil {
		c.logger.Error("Error while marshalling cache.journal: %v", err)
	}
	b, err = aes.EncryptAES(b, AesKey)
	if err != nil {
		c.logger.Error("Error while encrypting cache.journal: %v", err)
	}
	_, err = f.Write(b)
	if err != nil {
		c.logger.Error("Error while writing to cache.journal: %v", err)
	}
}

func (c *CACHE) loadFromFile() {
	var b []byte
	b, err := os.ReadFile("cache.journal")
	if err != nil {
		return
	}
	b, err = aes.DecryptAES(b, AesKey)
	if err != nil {
		c.logger.Error("Error while decrypting cache.journal: %v", err)
	}
	err = json.Unmarshal(b, c)
	if err != nil {
		c.logger.Error("Error while unmarshalling cache.journal: %v", err)
	}
}

var (
	cache = NewCache()
)

func NewCache() *CACHE {
	c := &CACHE{
		chats:    make(map[int64]*ChatObj),
		users:    make(map[int64]*UserObj),
		channels: make(map[int64]*Channel),
		InputPeers: &InputPeerCache{
			InputChannels: make(map[int64]*InputPeerChannel),
			InputUsers:    make(map[int64]*InputPeerUser),
			InputChats:    make(map[int64]*InputPeerChat),
		},
		logger: utils.NewLogger("Cache").SetLevel(LIB_LOG_LEVEL),
	}
	c.loadFromFile()
	go c.periodicallyFlushToFile()
	return c
}

func (c *CACHE) getUserPeer(userID int64) (InputUser, error) {
	for _, user := range c.InputPeers.InputUsers {
		if user.UserID == userID {
			return &InputUserObj{UserID: user.UserID, AccessHash: user.AccessHash}, nil
		}
	}
	return nil, fmt.Errorf("no user with id %d or missing from cache", userID)
}

func (c *CACHE) getChannelPeer(channelID int64) (InputChannel, error) {
	for _, channel := range c.InputPeers.InputChannels {
		if channel.ChannelID == channelID {
			return &InputChannelObj{ChannelID: channel.ChannelID, AccessHash: channel.AccessHash}, nil
		}
	}
	return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
}

func (c *CACHE) GetInputPeer(peerID int64) (InputPeer, error) {

	if strings.HasPrefix(strconv.Itoa(int(peerID)), "-100") {
		peerID = peerID - 1000000000000
	}
	for _, user := range c.InputPeers.InputUsers {
		if user.UserID == peerID {
			return &InputPeerUser{UserID: user.UserID, AccessHash: user.AccessHash}, nil
		}
	}
	for _, chat := range c.InputPeers.InputChats {
		if chat.ChatID == peerID {
			return &InputPeerChat{ChatID: chat.ChatID}, nil
		}
	}
	for _, channel := range c.InputPeers.InputChannels {
		if channel.ChannelID == peerID {
			return &InputPeerChannel{ChannelID: channel.ChannelID, AccessHash: channel.AccessHash}, nil
		}
	}
	return nil, fmt.Errorf("no peer with id %d", peerID)
}

// ------------------ Get Chat/Channel/User From Cache/Telgram ------------------

func (c *Client) getUserFromCache(userID int64) (*UserObj, error) {

	for _, user := range c.Cache.users {
		if user.ID == userID {
			return user, nil
		}
	}
	userPeer, err := c.Cache.getUserPeer(userID)
	if err != nil {
		return nil, err
	}
	users, err := c.UsersGetUsers([]InputUser{userPeer})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("no user with id %d", userID)
	}
	user, ok := users[0].(*UserObj)
	if !ok {
		return nil, fmt.Errorf("no user with id %d", userID)
	}
	return user, nil
}

func (c *Client) getChannelFromCache(channelID int64) (*Channel, error) {

	for _, channel := range c.Cache.channels {
		if channel.ID == channelID {
			return channel, nil
		}
	}
	channelPeer, err := c.Cache.getChannelPeer(channelID)
	if err != nil {
		return nil, err
	}
	channels, err := c.ChannelsGetChannels([]InputChannel{channelPeer})
	if err != nil {
		return nil, err
	}
	channelsObj, ok := channels.(*MessagesChatsObj)
	if !ok {
		return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
	}
	if len(channelsObj.Chats) == 0 {
		return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
	}
	channel, ok := channelsObj.Chats[0].(*Channel)
	if !ok {
		return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
	}
	return channel, nil
}

func (c *Client) getChatFromCache(chatID int64) (*ChatObj, error) {
	for _, chat := range c.Cache.chats {
		if chat.ID == chatID {
			return chat, nil
		}
	}
	chat, err := c.MessagesGetChats([]int64{chatID})
	if err != nil {
		return nil, err
	}
	chatsObj, ok := chat.(*MessagesChatsObj)
	if !ok {
		return nil, fmt.Errorf("no chat with id %d or missing from cache", chatID)
	}
	if len(chatsObj.Chats) == 0 {
		return nil, fmt.Errorf("no chat with id %d or missing from cache", chatID)
	}
	chatObj, ok := chatsObj.Chats[0].(*ChatObj)
	if !ok {
		return nil, fmt.Errorf("no chat with id %d or missing from cache", chatID)
	}
	return chatObj, nil
}

// ----------------- Get User/Channel/Chat from cache -----------------

func (c *Client) GetUser(userID int64) (*UserObj, error) {
	user, err := c.getUserFromCache(userID)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (c *Client) GetChannel(channelID int64) (*Channel, error) {
	channel, err := c.getChannelFromCache(channelID)
	if err != nil {
		return nil, err
	}
	return channel, nil
}

func (c *Client) GetChat(chatID int64) (*ChatObj, error) {
	chat, err := c.getChatFromCache(chatID)
	if err != nil {
		return nil, err
	}
	return chat, nil
}

// ----------------- Update User/Channel/Chat in cache -----------------

func (c *CACHE) UpdateUser(user *UserObj) {

	c.users[user.ID] = user
	peerUser := &InputPeerUser{UserID: user.ID, AccessHash: user.AccessHash}
	c.InputPeers.InputUsers[user.ID] = peerUser
}

func (c *CACHE) UpdateChannel(channel *Channel) {

	c.channels[channel.ID] = channel
	peerChannel := &InputPeerChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash}
	c.InputPeers.InputChannels[channel.ID] = peerChannel
}

func (c *CACHE) UpdateChat(chat *ChatObj) {

	c.chats[chat.ID] = chat
	peerChat := &InputPeerChat{ChatID: chat.ID}
	c.InputPeers.InputChats[chat.ID] = peerChat
}

func (cache *CACHE) UpdatePeersToCache(u []User, c []Chat) {
	for _, user := range u {
		us, ok := user.(*UserObj)
		if ok {
			cache.UpdateUser(us)
		}
	}
	for _, chat := range c {
		ch, ok := chat.(*ChatObj)
		if ok {
			cache.UpdateChat(ch)
		} else {
			channel, ok := chat.(*Channel)
			if ok {
				cache.UpdateChannel(channel)
			}
		}
	}
	go cache.flushToFile()
}

// ----------------- Cache Misc Functions -----------------

func (c *CACHE) GetSize() uintptr {

	return unsafe.Sizeof(c.users) + unsafe.Sizeof(c.chats) + unsafe.Sizeof(c.channels)
}

func (c *CACHE) Purge() {

	c.users = make(map[int64]*UserObj)
	c.chats = make(map[int64]*ChatObj)
	c.channels = make(map[int64]*Channel)
}

// ----------------- Custom Peer Types -----------------

func (c *Client) GetPeerUser(userID int64) (*InputPeerUser, error) {

	if peer, ok := c.Cache.InputPeers.InputUsers[userID]; ok {
		return peer, nil
	}
	return nil, fmt.Errorf("no user with id %d or missing from cache", userID)
}

func (c *Client) GetPeerChannel(channelID int64) (*InputPeerChannel, error) {

	if peer, ok := c.Cache.InputPeers.InputChannels[channelID]; ok {
		return peer, nil
	}
	return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
}

// ----------------- AES Encryption -----------------
