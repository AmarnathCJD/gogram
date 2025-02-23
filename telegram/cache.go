// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"encoding/gob"
	"encoding/json"

	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amarnathcjd/gogram/internal/utils"
)

type CACHE struct {
	*sync.RWMutex
	fileName    string
	chats       map[int64]*ChatObj
	users       map[int64]*UserObj
	channels    map[int64]*Channel
	usernameMap map[string]int64
	memory      bool
	disabled    bool
	InputPeers  *InputPeerCache `json:"input_peers,omitempty"`
	logger      *utils.Logger

	wipeScheduled atomic.Bool
}

type InputPeerCache struct {
	InputChannels map[int64]int64 `json:"channels,omitempty"`
	InputUsers    map[int64]int64 `json:"users,omitempty"`
}

func (c *CACHE) SetWriteFile(write bool) *CACHE {
	c.memory = !write
	return c
}

func (c *CACHE) Clear() {
	c.Lock()
	defer c.Unlock()

	c.chats = make(map[int64]*ChatObj)
	c.users = make(map[int64]*UserObj)
	c.channels = make(map[int64]*Channel)
	c.usernameMap = make(map[string]int64)
	c.InputPeers = &InputPeerCache{
		InputChannels: make(map[int64]int64),
		InputUsers:    make(map[int64]int64),
	}
}

func (c *CACHE) ExportJSON() ([]byte, error) {
	c.RLock()
	defer c.RUnlock()

	return json.Marshal(c.InputPeers)
}

func (c *CACHE) ImportJSON(data []byte) error {
	c.Lock()
	defer c.Unlock()

	return json.Unmarshal(data, c.InputPeers)
}

type CacheConfig struct {
	MaxSize    int // Max size of cache: TODO
	LogLevel   utils.LogLevel
	LogNoColor bool
	LogName    string
	Memory     bool
	Disabled   bool
}

func NewCache(fileName string, opts ...*CacheConfig) *CACHE {
	opt := getVariadic(opts, &CacheConfig{
		LogLevel: utils.InfoLevel,
	})

	c := &CACHE{
		RWMutex:     &sync.RWMutex{},
		fileName:    fileName,
		chats:       make(map[int64]*ChatObj),
		users:       make(map[int64]*UserObj),
		channels:    make(map[int64]*Channel),
		usernameMap: make(map[string]int64),
		InputPeers: &InputPeerCache{
			InputChannels: make(map[int64]int64),
			InputUsers:    make(map[int64]int64),
		},
		memory:   opt.Memory,
		disabled: opt.Disabled,
		logger: utils.NewLogger("gogram " + getLogPrefix("cache", opt.LogName)).
			SetLevel(opt.LogLevel).
			NoColor(opt.LogNoColor),
	}

	if !opt.Memory && !opt.Disabled {
		c.logger.Debug("initialized cache (" + c.fileName + ") successfully")
	}

	if !opt.Disabled {
		if _, err := os.Stat(c.fileName); err == nil && !c.memory {
			c.ReadFile()
		}
	}

	return c
}

func (c *CACHE) Disable() *CACHE {
	c.disabled = true
	return c
}

// --------- Cache file Functions ---------
func (c *CACHE) WriteFile() {
	if c.disabled || c.memory {
		return
	}
	file, err := os.OpenFile(c.fileName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		c.logger.Error("error opening cache file: ", err)
		return
	}
	defer file.Close()
	enc := gob.NewEncoder(file)
	c.Lock()
	defer c.Unlock()
	if err := enc.Encode(c.InputPeers); err != nil {
		c.logger.Error("error encoding cache file: ", err)
	}
}

func (c *CACHE) ReadFile() {
	file, err := os.Open(c.fileName)
	if err != nil && !os.IsNotExist(err) {
		c.logger.Error("error opening cache file: ", err)
		return
	}

	if os.IsNotExist(err) {
		return
	}

	defer file.Close()
	dec := gob.NewDecoder(file)
	c.Lock()
	dec.Decode(&c.InputPeers)
	c.Unlock()

	if !c.memory {
		c.logger.Debug(fmt.Sprintf("loaded %d users, %d channels from cache", len(c.InputPeers.InputUsers), len(c.InputPeers.InputChannels)))
	}
}

func (c *CACHE) getUserPeer(userID int64) (InputUser, error) {
	c.RLock()
	defer c.RUnlock()

	if userHash, ok := c.InputPeers.InputUsers[userID]; ok {
		return &InputUserObj{UserID: userID, AccessHash: userHash}, nil
	}

	return nil, fmt.Errorf("no user with id '%d' or missing from cache", userID)
}

func (c *CACHE) getChannelPeer(channelID int64) (InputChannel, error) {
	c.RLock()
	defer c.RUnlock()

	if channelHash, ok := c.InputPeers.InputChannels[channelID]; ok {
		return &InputChannelObj{ChannelID: channelID, AccessHash: channelHash}, nil
	}

	return nil, fmt.Errorf("no channel with id '%d' or missing from cache", channelID)
}

func (c *Client) GetInputPeer(peerID int64) (InputPeer, error) {
	// if peerID is negative, it is a channel or a chat (botAPILike)
	if peerID < 0 {
		peerIdStr := strconv.Itoa(int(peerID))
		if strings.HasPrefix(peerIdStr, "-100") {
			if channel, err := c.getChannelFromCache(peerID); err != nil {
				return nil, err
			} else {
				return &InputPeerChannel{peerID, channel.AccessHash}, nil
			}
		} else {
			if _, err := c.getChatFromCache(peerID); err != nil {
				return nil, err
			}
			return &InputPeerChat{peerID}, nil
		}
	} else {
		if user, err := c.getUserFromCache(peerID); err != nil {
			return nil, err
		} else {
			return &InputPeerUser{peerID, user.AccessHash}, nil
		}
	}
}

// ------------------ Get Chat/Channel/User From Cache/Telgram ------------------

func (c *Client) getUserFromCache(userID int64) (*UserObj, error) {
	c.Cache.RLock()
	if user, found := c.Cache.users[userID]; found {
		c.Cache.RUnlock()
		return user, nil
	}
	c.Cache.RUnlock()

	userPeer, err := c.Cache.getUserPeer(userID)

	// if user is not in cache and if the bot is participant in the user, try with access hash = 0
	var inputPeerUser InputUser = &InputUserObj{UserID: userID, AccessHash: 0}
	if err == nil {
		inputPeerUser = userPeer
	}

	users, err := c.UsersGetUsers([]InputUser{inputPeerUser})
	if err != nil {
		return nil, err
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("no user with id '%d'", userID)
	}

	user, ok := users[0].(*UserObj)
	if !ok {
		return nil, fmt.Errorf("expected UserObj for id '%d', but got different type", userID)
	}
	c.Cache.UpdateUser(user)

	return user, nil
}

func (c *Client) getChannelFromCache(channelID int64) (*Channel, error) {
	c.Cache.RLock()
	if channel, found := c.Cache.channels[channelID]; found {
		c.Cache.RUnlock()
		return channel, nil
	}
	c.Cache.RUnlock()

	channelPeer, err := c.Cache.getChannelPeer(channelID)

	// convert botApiLikeChannelID to normal
	peerID := channelID
	peerIdStr := strconv.Itoa(int(channelID))
	if strings.HasPrefix(peerIdStr, "-100") {
		peerIdStr = strings.TrimPrefix(peerIdStr, "-100")
		if peerIdInt, err := strconv.Atoi(peerIdStr); err == nil {
			peerID = int64(peerIdInt)
		}
	}
		
	// if channel is not in cache and if the bot is participant in the channel, try with access hash = 0
	var inputChannel InputChannel = &InputChannelObj{ChannelID: peerID, AccessHash: 0}
	if err == nil {
		inputChannel = channelPeer
	}

	channels, err := c.ChannelsGetChannels([]InputChannel{inputChannel})
	if err != nil {
		return nil, err
	}

	channelsObj, ok := channels.(*MessagesChatsObj)
	if !ok {
		return nil, fmt.Errorf("expected MessagesChatsObj for channel id '%d', but got different type", channelID)
	}

	if len(channelsObj.Chats) == 0 {
		return nil, fmt.Errorf("no channel with id '%d'", channelID)
	}

	channel, ok := channelsObj.Chats[0].(*Channel)
	if !ok {
		return nil, fmt.Errorf("expected Channel for id '%d', but got different type", channelID)
	}
	c.Cache.UpdateChannel(channel)
	channel.ID = channelID // return botApiLikeChannelID

	return channel, nil
}

func (c *Client) getChatFromCache(chatID int64) (*ChatObj, error) {
	c.Cache.RLock()
	if chat, found := c.Cache.chats[chatID]; found {
		c.Cache.RUnlock()
		return chat, nil
	}
	c.Cache.RUnlock()

	chat, err := c.MessagesGetChats([]int64{chatID})
	if err != nil {
		return nil, err
	}

	chatsObj, ok := chat.(*MessagesChatsObj)
	if !ok {
		return nil, fmt.Errorf("expected MessagesChatsObj for chat id %d, but got different type", chatID)
	}

	if len(chatsObj.Chats) == 0 {
		return nil, fmt.Errorf("no chat with id '%d'", chatID)
	}

	chatObj, ok := chatsObj.Chats[0].(*ChatObj)
	if !ok {
		return nil, fmt.Errorf("expected ChatObj for id '%d', but got different type", chatID)
	}
	c.Cache.UpdateChat(chatObj)

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

// mux function to getChat/getChannel/getUser
func (c *Client) GetPeer(peerID int64) (any, error) {
	if chat, err := c.GetChat(peerID); err == nil {
		return chat, nil
	} else if channel, err := c.GetChannel(peerID); err == nil {
		return channel, nil
	} else if user, err := c.GetUser(peerID); err == nil {
		return user, nil
	} else {
		return nil, err
	}
}

// ----------------- Update User/Channel/Chat in cache -----------------

func (c *CACHE) UpdateUser(user *UserObj) bool {
	c.Lock()
	defer c.Unlock()

	if user.Username != "" {
		c.usernameMap[user.Username] = user.ID
	}
	if _, ok := c.users[user.ID]; !ok {
		c.users[user.ID] = user
	}

	if user.Min {
		if userFromCache, ok := c.users[user.ID]; ok {
			if userFromCache.Min {
				c.users[user.ID] = user
			}
			return false
		}
		c.users[user.ID] = user
		return false
	}

	if currAccessHash, ok := c.InputPeers.InputUsers[user.ID]; ok {
		if currAccessHash != user.AccessHash {
			c.InputPeers.InputUsers[user.ID] = user.AccessHash
			c.users[user.ID] = user
			return true
		}
		return false
	}

	c.InputPeers.InputUsers[user.ID] = user.AccessHash
	return true
}

func (c *CACHE) UpdateChannel(channel *Channel) bool {
	c.Lock()
	defer c.Unlock()

	if channel.Username != "" {
		c.usernameMap[channel.Username] = channel.ID
	}

    // normal -> botApiLike channel id conversion :')
    peerIdStr := strconv.Itoa(int(channel.ID))
    if !strings.HasPrefix(peerIdStr, "-100") {
        channel.ID, _ = strconv.ParseInt("-100" + peerIdStr, 10, 64) // channel.ID was already an int64, so ignored err
    }

	if _, ok := c.channels[channel.ID]; !ok {
		c.channels[channel.ID] = channel
	}
	if currAccessHash, ok := c.InputPeers.InputChannels[channel.ID]; ok {
		if activeCh, ok := c.channels[channel.ID]; ok {
			if activeCh.Min {
				c.InputPeers.InputChannels[channel.ID] = channel.AccessHash
				c.channels[channel.ID] = channel
				return true
			}

			if !activeCh.Min && channel.Min {
				return false
			}
		}

		if currAccessHash != channel.AccessHash && !channel.Min {
			c.InputPeers.InputChannels[channel.ID] = channel.AccessHash
			c.channels[channel.ID] = channel
			return true
		}

		return false
	}

	c.InputPeers.InputChannels[channel.ID] = channel.AccessHash
	return true
}

func (c *CACHE) UpdateChat(chat *ChatObj) bool {
	c.Lock()
	defer c.Unlock()
	c.chats[chat.ID] = chat

	return true
}

func (cache *CACHE) UpdatePeersToCache(users []User, chats []Chat) {
	if cache.disabled && !cache.wipeScheduled.Load() {
		// schedule a wipe of the cache after 20 seconds
		cache.wipeScheduled.Store(true)
		go func() {
			<-time.After(20 * time.Second)
			cache.Clear()
			cache.wipeScheduled.Store(false)
		}()
	}

	totalUpdates := [2]int{0, 0}

	for _, user := range users {
		switch us := user.(type) {
		case *UserObj:
			if updated := cache.UpdateUser(us); updated {
				totalUpdates[0]++
			}
		case *UserEmpty:
		}
	}

	for _, chat := range chats {
		switch ch := chat.(type) {
		case *ChatObj:
			if updated := cache.UpdateChat(ch); updated {
				totalUpdates[1]++
			}
		case *Channel:
			if updated := cache.UpdateChannel(ch); updated {
				totalUpdates[1]++
			}
		case *ChatForbidden:
			cache.Lock()
			if _, ok := cache.chats[ch.ID]; !ok {
				cache.chats[ch.ID] = &ChatObj{
					ID: ch.ID,
				}
			}
			cache.Unlock()
		case *ChannelForbidden:
			cache.Lock()
			if _, ok := cache.InputPeers.InputChannels[ch.ID]; !ok {
				cache.channels[ch.ID] = &Channel{
					ID:         ch.ID,
					Broadcast:  ch.Broadcast,
					Megagroup:  ch.Megagroup,
					AccessHash: ch.AccessHash,
					Title:      ch.Title,
				}
				cache.InputPeers.InputChannels[ch.ID] = ch.AccessHash
			}
			cache.Unlock()
		case *ChatEmpty:
		}
	}

	if totalUpdates[0] > 0 || totalUpdates[1] > 0 {
		if !cache.memory && !cache.disabled {
			go cache.WriteFile() // write to file asynchronously
		}
		cache.logger.Debug(
			fmt.Sprintf("updated %d users %d chats and %d channels in cache (u: %d, c: %d ut: %d, ct: %d, cc: %d)",
				totalUpdates[0], totalUpdates[1], totalUpdates[1], len(users), len(chats),
				len(cache.InputPeers.InputUsers), len(cache.chats), len(cache.InputPeers.InputChannels),
			),
		)
	}
}

func (c *Client) GetPeerUser(userID int64) (*InputPeerUser, error) {
	c.Cache.RLock()
	defer c.Cache.RUnlock()

	if peer, ok := c.Cache.InputPeers.InputUsers[userID]; ok {
		return &InputPeerUser{UserID: userID, AccessHash: peer}, nil
	}
	return nil, fmt.Errorf("no user with id '%d' or missing from cache", userID)
}

func (c *Client) GetPeerChannel(channelID int64) (*InputPeerChannel, error) {
	c.Cache.RLock()
	defer c.Cache.RUnlock()

	if peer, ok := c.Cache.InputPeers.InputChannels[channelID]; ok {
		return &InputPeerChannel{ChannelID: channelID, AccessHash: peer}, nil
	}
	return nil, fmt.Errorf("no channel with id '%d' or missing from cache", channelID)
}

func (c *Client) IdInCache(id int64) bool {
	c.Cache.RLock()
	defer c.Cache.RUnlock()

	if _, ok := c.Cache.InputPeers.InputUsers[id]; ok {
		return true
	}
	if _, ok := c.Cache.InputPeers.InputChannels[id]; ok {
		return true
	}

	return false
}
