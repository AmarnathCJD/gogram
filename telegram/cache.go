// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/amarnathcjd/gogram/internal/utils"
)

type CACHE struct {
	*sync.RWMutex
	fileN      string
	chats      map[int64]*ChatObj
	users      map[int64]*UserObj
	channels   map[int64]*Channel
	writeFile  bool
	file       *os.File
	InputPeers *InputPeerCache `json:"input_peers,omitempty"`
	logger     *utils.Logger
}

type InputPeerCache struct {
	InputChannels map[int64]int64 `json:"channels,omitempty"`
	InputUsers    map[int64]int64 `json:"users,omitempty"`
	InputChats    map[int64]int64 `json:"chats,omitempty"`
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

func NewCache(logLevel string, fileN string) *CACHE {
	c := &CACHE{
		RWMutex:  &sync.RWMutex{},
		fileN:    fileN + ".db",
		chats:    make(map[int64]*ChatObj),
		users:    make(map[int64]*UserObj),
		channels: make(map[int64]*Channel),
		InputPeers: &InputPeerCache{
			InputChannels: make(map[int64]int64),
			InputUsers:    make(map[int64]int64),
			InputChats:    make(map[int64]int64),
		},
		logger: utils.NewLogger("gogram - cache").SetLevel(logLevel),
	}

	c.logger.Debug("initialized cache (" + c.fileN + ") successfully")

	if _, err := os.Stat(c.fileN); err == nil && c.writeFile {
		c.ReadFile()
	}

	return c
}

// --------- Cache file Functions ---------
func (c *CACHE) WriteFile() {
	//c.Lock()
	//defer c.Unlock() // necessary?

	if c.file == nil {
		var err error
		c.file, err = os.OpenFile(c.fileN, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			c.logger.Error("error opening cache file: ", err)
			return
		}

		defer c.file.Close()
	}

	// write format: 'type:id:access_hash,...'
	// type: 1 for user, 2 for chat, 3 for channel

	for id, accessHash := range c.InputPeers.InputUsers {
		_, _ = c.file.WriteString(fmt.Sprintf("1:%d:%d,", id, accessHash))
	}
	for id, accessHash := range c.InputPeers.InputChats {
		_, _ = c.file.WriteString(fmt.Sprintf("2:%d:%d,", id, accessHash))
	}
	for id, accessHash := range c.InputPeers.InputChannels {
		_, _ = c.file.WriteString(fmt.Sprintf("3:%d:%d,", id, accessHash))
	}

	c.file = nil
}

func (c *CACHE) ReadFile() {
	c.Lock()
	defer c.Unlock()

	if c.file == nil {
		var err error
		c.file, err = os.Open(c.fileN)
		if err != nil && !os.IsNotExist(err) {
			c.logger.Error("error opening cache file: ", err)
			return
		}

		defer c.file.Close()
	}

	// read till each , using buffer
	// format: 'type:id:access_hash,...'

	_totalLoaded := 0

	buffer := make([]byte, 1)
	var data []byte
	for {
		_, err := c.file.Read(buffer)
		if err != nil {
			break
		}
		if buffer[0] == ',' {
			// process data
			// data format: 'type:id:access_hash'
			data = append(data, buffer[0])
			// process data
			if processed := c.processData(data); processed {
				_totalLoaded++
			}
			// reset data
			data = nil
		} else {
			data = append(data, buffer[0])
		}
	}

	if _totalLoaded != 0 {
		c.logger.Debug("loaded ", _totalLoaded, " peers from cacheFile")
	}

	c.file = nil
}

func (c *CACHE) processData(data []byte) bool {
	// data format: 'type:id:access_hash'
	// type: 1 for user, 2 for chat, 3 for channel
	// split data
	splitData := strings.Split(string(data), ":")
	if len(splitData) != 3 {
		return false
	}
	// convert to int
	id, err := strconv.Atoi(splitData[1])
	if err != nil {
		return false
	}
	accessHash, err := strconv.Atoi(strings.TrimSuffix(splitData[2], ","))
	if err != nil {
		return false
	}

	// process data
	switch splitData[0] {
	case "1":
		c.InputPeers.InputUsers[int64(id)] = int64(accessHash)
	case "2":
		c.InputPeers.InputChats[int64(id)] = int64(accessHash)
	case "3":
		c.InputPeers.InputChannels[int64(id)] = int64(accessHash)
	default:
		return false
	}

	return true
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
	peerIdStr := strconv.Itoa(int(peerID))
	if strings.HasPrefix(peerIdStr, "-100") {
		peerIdStr = strings.TrimPrefix(peerIdStr, "-100")

		if peerIdInt, err := strconv.Atoi(peerIdStr); err == nil {
			peerID = int64(peerIdInt)
		} else {
			return nil, err
		}
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

func (c *CACHE) UpdateUser(user *UserObj) bool {
	c.Lock()
	defer c.Unlock()

	if currAccessHash, ok := c.InputPeers.InputUsers[user.ID]; ok {
		if currAccessHash != user.AccessHash {
			c.InputPeers.InputUsers[user.ID] = user.AccessHash
			c.users[user.ID] = user
			return true
		}
		return false
	}

	c.users[user.ID] = user
	c.InputPeers.InputUsers[user.ID] = user.AccessHash

	return true
}

func (c *CACHE) UpdateChannel(channel *Channel) bool {
	c.Lock()
	defer c.Unlock()

	if currAccessHash, ok := c.InputPeers.InputChannels[channel.ID]; ok {
		if currAccessHash != channel.AccessHash {
			c.InputPeers.InputChannels[channel.ID] = channel.AccessHash
			c.channels[channel.ID] = channel
			return true
		}
		return false
	}
	c.channels[channel.ID] = channel
	c.InputPeers.InputChannels[channel.ID] = channel.AccessHash

	return true
}

func (c *CACHE) UpdateChat(chat *ChatObj) bool {
	c.Lock()
	defer c.Unlock()

	if _, ok := c.InputPeers.InputChats[chat.ID]; ok {
		return false
	}

	c.chats[chat.ID] = chat
	c.InputPeers.InputChats[chat.ID] = chat.ID

	return true
}

func (cache *CACHE) UpdatePeersToCache(u []User, c []Chat) {
	_totalUpdates := [2]int{0, 0}
	for _, user := range u {
		us, ok := user.(*UserObj)
		if ok {
			if upd := cache.UpdateUser(us); upd {
				_totalUpdates[0]++
			}
		}
	}
	for _, chat := range c {
		ch, ok := chat.(*ChatObj)
		if ok {
			if upd := cache.UpdateChat(ch); upd {
				_totalUpdates[1]++
			}
		} else {
			channel, ok := chat.(*Channel)
			if ok {
				if upd := cache.UpdateChannel(channel); upd {
					_totalUpdates[1]++
				}
			}
		}
	}

	if _totalUpdates[0] != 0 || _totalUpdates[1] != 0 {
		if cache.writeFile {
			go cache.WriteFile() // write to file
		}
		if _totalUpdates[0] != 0 && _totalUpdates[1] != 0 {
			cache.logger.Debug("updated ", _totalUpdates[0], "(u) and ", _totalUpdates[1], "(c) to ", cache.fileN, " (u:", len(cache.InputPeers.InputUsers), ", c:", len(cache.InputPeers.InputChats), ")")
		} else if _totalUpdates[0] != 0 {
			cache.logger.Debug("updated ", _totalUpdates[0], "(u) to ", cache.fileN, " (u:", len(cache.InputPeers.InputUsers), ", c:", len(cache.InputPeers.InputChats), ")")
		} else {
			cache.logger.Debug("updated ", _totalUpdates[1], "(c) to ", cache.fileN, " (u:", len(cache.InputPeers.InputUsers), ", c:", len(cache.InputPeers.InputChats), ")")
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
