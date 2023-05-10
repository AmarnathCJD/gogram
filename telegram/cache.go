package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/amarnathcjd/gogram/internal/utils"
)

const (
	// CacheUpdateInterval is the interval in seconds at which the cache is updated
	CacheUpdateInterval = 60
)

type CACHE struct {
	*sync.RWMutex
	chats      map[int64]*ChatObj
	users      map[int64]*UserObj
	channels   map[int64]*Channel
	InputPeers *InputPeerCache `json:"input_peers,omitempty"`
	logger     *utils.Logger
}

type InputPeerCache struct {
	InputChannels map[int64]int64 `json:"channels,omitempty"`
	InputUsers    map[int64]int64 `json:"users,omitempty"`
	InputChats    map[int64]int64 `json:"chats,omitempty"`
}

func (c *CACHE) flushToFile() {
	tmpfile, err := os.CreateTemp("", "cache-*.tmp")
	if err != nil {
		log.Println(err)
		return
	}
	defer os.Remove(tmpfile.Name())

	var b []byte
	b, err = json.Marshal(c)
	if err != nil {
		c.logger.Error("Error while marshalling cache.journal: ", err)
		return
	}

	if _, err := tmpfile.Write(b); err != nil {
		c.logger.Error("Error while writing cache to temporary file: ", err)
		return
	}

	if err := tmpfile.Close(); err != nil {
		c.logger.Error("Error while closing temporary file: ", err)
		return
	}

	if err := os.Rename(tmpfile.Name(), "cache.journal"); err != nil {
		c.logger.Error("Error while moving temporary file to cache file: ", err)
	}

	c.logger.Debug("Cache flushed to file successfully")
	//  schedule next flush in 80 seconds
	go time.AfterFunc(80*time.Second, c.flushToFile)
}

func (c *CACHE) loadFromFile() {
	file, err := os.Open("cache.journal")
	if err != nil {
		if os.IsNotExist(err) {
			// cache file doesn't exist, this is not an error
			return
		}
		c.logger.Error("Error while opening cache.journal: %v", err)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		c.logger.Error("Error while getting cache.journal file info: %v", err)
		return
	}
	if stat.Size() == 0 {
		// empty cache file, nothing to load
		return
	}

	data := make([]byte, stat.Size())
	if _, err := io.ReadFull(file, data); err != nil {
		c.logger.Error("Error while reading cache.journal: %v", err)
		return
	}

	if err := json.Unmarshal(data, c); err != nil {
		c.logger.Error("Error while unmarshalling cache.journal: %v", err)
		return
	}
}

var cache = NewCache()

func NewCache() *CACHE {
	c := &CACHE{
		RWMutex:  &sync.RWMutex{},
		chats:    make(map[int64]*ChatObj),
		users:    make(map[int64]*UserObj),
		channels: make(map[int64]*Channel),
		InputPeers: &InputPeerCache{
			InputChannels: make(map[int64]int64),
			InputUsers:    make(map[int64]int64),
			InputChats:    make(map[int64]int64),
		},
		logger: utils.NewLogger("cache").SetLevel(LIB_LOG_LEVEL),
	}
	c.logger.Debug("Cache initialized successfully")
	
	return c
}

func (c *CACHE) startCacheFileUpdater() {
	c.loadFromFile()
	go c.writeOnKill()
	go time.AfterFunc(80*time.Second, c.flushToFile)
}

func (c *CACHE) writeOnKill() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	sig := <-signals
	fmt.Printf("\nReceived signal: %v, flushing cache to file and exiting...\n", sig)
	c.flushToFile()
}

func (c *CACHE) getUserPeer(userID int64) (InputUser, error) {
	for userId, accessHash := range c.InputPeers.InputUsers {
		if userId == userID {
			return &InputUserObj{UserID: userId, AccessHash: accessHash}, nil
		}
	}
	return nil, fmt.Errorf("no user with id %d or missing from cache", userID)
}

func (c *CACHE) getChannelPeer(channelID int64) (InputChannel, error) {
	for channelId, channelHash := range c.InputPeers.InputChannels {
		if channelId == channelID {
			return &InputChannelObj{ChannelID: channelId, AccessHash: channelHash}, nil
		}
	}
	return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
}

func (c *CACHE) GetInputPeer(peerID int64) (InputPeer, error) {
	// if peerID is negative, it is a channel or a chat
	if strings.HasPrefix(strconv.Itoa(int(peerID)), "-100") {
		// remove -100 from peerID
		peerIdStr := strconv.Itoa(int(peerID))
		peerIdStr = strings.TrimPrefix(peerIdStr, "-100")
		peerIdInt, err := strconv.Atoi(peerIdStr)
		if err != nil {
			return nil, err
		}
		peerID = int64(peerIdInt)
	}
	c.RLock()
	defer c.RUnlock()
	for userId, userHash := range c.InputPeers.InputUsers {
		if userId == peerID {
			return &InputPeerUser{userId, userHash}, nil
		}
	}
	for chatId := range c.InputPeers.InputChats {
		if chatId == peerID {
			return &InputPeerChat{ChatID: chatId}, nil
		}
	}
	for channelId, channelHash := range c.InputPeers.InputChannels {
		if channelId == peerID {
			return &InputPeerChannel{channelId, channelHash}, nil
		}
	}
	return nil, fmt.Errorf("there is no peer with id %d or missing from cache", peerID)
}

// ------------------ Get Chat/Channel/User From Cache/Telgram ------------------

func (c *Client) getUserFromCache(userID int64) (*UserObj, error) {
	c.Cache.RLock()
	defer c.Cache.RUnlock()
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
	c.Cache.RLock()
	defer c.Cache.RUnlock()

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
	c.Cache.RLock()
	defer c.Cache.RUnlock()
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
	c.Lock()
	defer c.Unlock()

	c.users[user.ID] = user
	c.InputPeers.InputUsers[user.ID] = user.AccessHash
}

func (c *CACHE) UpdateChannel(channel *Channel) {
	c.Lock()
	defer c.Unlock()

	c.channels[channel.ID] = channel
	c.InputPeers.InputChannels[channel.ID] = channel.AccessHash
}

func (c *CACHE) UpdateChat(chat *ChatObj) {
	c.Lock()
	defer c.Unlock()

	c.chats[chat.ID] = chat
	c.InputPeers.InputChats[chat.ID] = chat.ID
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
}

func (c *Client) GetPeerUser(userID int64) (*InputPeerUser, error) {
	if peer, ok := c.Cache.InputPeers.InputUsers[userID]; ok {
		return &InputPeerUser{UserID: userID, AccessHash: peer}, nil
	}
	return nil, fmt.Errorf("no user with id %d or missing from cache", userID)
}

func (c *Client) GetPeerChannel(channelID int64) (*InputPeerChannel, error) {

	if peer, ok := c.Cache.InputPeers.InputChannels[channelID]; ok {
		return &InputPeerChannel{ChannelID: channelID, AccessHash: peer}, nil
	}
	return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
}
