package telegram

import (
	"fmt"
	"sync"
)

type CACHE struct {
	sync.Mutex
	chats    map[int32]*ChatObj
	users    map[int32]*UserObj
	channels map[int32]*Channel
}

var (
	cache = NewCache()
)

func NewCache() *CACHE {
	return &CACHE{
		chats:    make(map[int32]*ChatObj),
		users:    make(map[int32]*UserObj),
		channels: make(map[int32]*Channel),
	}
}

func (c *CACHE) GetChat(chat_id int32) (*ChatObj, error) {
	c.Lock()
	defer c.Unlock()
	if chat, ok := c.chats[chat_id]; ok {
		return chat, nil
	}
	return nil, fmt.Errorf("no chat with id %d", chat_id)
}

func (c *CACHE) GetUser(user_id int32) (*UserObj, error) {
	c.Lock()
	defer c.Unlock()
	if user, ok := c.users[user_id]; ok {
		return user, nil
	}
	return nil, fmt.Errorf("no user with id %d", user_id)
}

func (c *CACHE) GetChannel(channel_id int32) (*Channel, error) {
	c.Lock()
	defer c.Unlock()
	if channel, ok := c.channels[channel_id]; ok {
		return channel, nil
	}
	return nil, fmt.Errorf("no channel with id %d", channel_id)
}

func (c *CACHE) UpdateUser(user *UserObj) {
	c.Lock()
	defer c.Unlock()
	c.users[user.ID] = user
}

func (c *CACHE) UpdateChat(chat *ChatObj) {
	c.Lock()
	defer c.Unlock()
	c.chats[chat.ID] = chat
}

func (c *CACHE) GetSize() int {
	c.Lock()
	defer c.Unlock()
	return len(c.chats) + len(c.users)
}

func (cache *CACHE) UpdatePeersToCache(u []User, c []Chat) {
	cache.Lock()
	defer cache.Unlock()
	for _, user := range u {
		us, ok := user.(*UserObj)
		if ok {
			cache.users[us.ID] = us
		}
	}
	for _, chat := range c {
		ch, ok := chat.(*ChatObj)
		if ok {
			cache.chats[ch.ID] = ch
		} else {
			channel, ok := chat.(*Channel)
			if ok {
				cache.channels[channel.ID] = channel
			}
		}
	}
}

func (cache *CACHE) GetPeersFromCache(u []int32, c []int32) ([]User, []Chat) {
	cache.Lock()
	defer cache.Unlock()
	var users []User
	var chats []Chat
	for _, user := range u {
		if user, ok := cache.users[user]; ok {
			users = append(users, user)
		}
	}
	for _, chat := range c {
		if chat, ok := cache.chats[chat]; ok {
			chats = append(chats, chat)
		}
	}
	return users, chats
}

func (client *Client) SaveToCache(u []User, c []Chat) {
	client.Cache.UpdatePeersToCache(u, c)
}

func (client *Client) GetPeerChat(chat_id int32) (*ChatObj, error) {
	return client.Cache.GetChat(chat_id)
}

func (client *Client) GetPeerUser(user_id int32) (*UserObj, error) {
	return client.Cache.GetUser(user_id)
}

func (client *Client) GetPeerChannel(channel_id int32) (*Channel, error) {
	return client.Cache.GetChannel(channel_id)
}

func (client *Client) GetAllPeers() (int, int) {
	return len(client.Cache.users), len(client.Cache.chats)
}
