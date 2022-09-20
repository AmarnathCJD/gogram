package telegram

import (
	"fmt"
	"sync"
)

type CACHE struct {
	sync.RWMutex
	chats    map[int64]*ChatObj
	users    map[int64]*UserObj
	channels map[int64]*Channel
}

var (
	cache = NewCache()
)

func NewCache() *CACHE {
	return &CACHE{
		chats:    make(map[int64]*ChatObj),
		users:    make(map[int64]*UserObj),
		channels: make(map[int64]*Channel),
	}
}

func (c *CACHE) GetChat(chat_id int64) (*ChatObj, error) {
	c.RLock()
	defer c.RUnlock()
	if chat, ok := c.chats[chat_id]; ok {
		return chat, nil
	}
	return nil, fmt.Errorf("no chat with id %d", chat_id)
}

func (c *CACHE) GetUser(user_id int64) (*UserObj, error) {
	c.RLock()
	defer c.RUnlock()
	if user, ok := c.users[user_id]; ok {
		return user, nil
	}
	return nil, fmt.Errorf("no user with id %d", user_id)
}

func (c *CACHE) GetChannel(channel_id int64) (*Channel, error) {
	c.RLock()
	defer c.RUnlock()
	if channel, ok := c.channels[channel_id]; ok {
		return channel, nil
	}
	return nil, fmt.Errorf("no channel with id %d", channel_id)
}

func (c *CACHE) UpdateUser(user *UserObj) {
	c.RLock()
	defer c.RUnlock()
	c.users[user.ID] = user
}

func (c *CACHE) UpdateChat(chat *ChatObj) {
	c.RLock()
	defer c.RUnlock()
	c.chats[chat.ID] = chat
}

func (c *CACHE) GetSize() int {
	c.RLock()
	defer c.RUnlock()
	return len(c.chats) + len(c.users)
}

func (cache *CACHE) UpdatePeersToCache(u []User, c []Chat) {
	cache.RLock()
	defer cache.RUnlock()
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

func (cache *CACHE) GetPeersFromCache(u []int64, c []int64) ([]User, []Chat) {
	cache.RLock()
	defer cache.RUnlock()
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

func (client *Client) GetPeerChat(chat_id int64) (*ChatObj, error) {
	return client.Cache.GetChat(chat_id)
}

func (client *Client) GetPeerUser(user_id int64) (*UserObj, error) {
	return client.Cache.GetUser(user_id)
}

func (client *Client) GetPeerChannel(channel_id int64) (*Channel, error) {
	return client.Cache.GetChannel(channel_id)
}

func (client *Client) GetCacheCount() int {
	return client.Cache.GetSize()
}

func (client *Client) GetInputPeer(peer_id int64) (InputPeer, error) {
	if peer, err := client.GetPeerUser(peer_id); err == nil {
		return &InputPeerUser{
			UserID:     peer_id,
			AccessHash: peer.AccessHash,
		}, nil
	} else if _, err := client.GetPeerChat(peer_id); err == nil {
		return &InputPeerChat{
			ChatID: peer_id,
		}, nil
	} else if peer, err := client.GetPeerChannel(peer_id); err == nil {
		return &InputPeerChannel{
			ChannelID:  peer_id,
			AccessHash: peer.AccessHash,
		}, nil
	}
	return nil, fmt.Errorf("cannot cast %v to any kind of inputpeer", peer_id)
}
