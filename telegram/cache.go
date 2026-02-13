// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"encoding/gob"
	"encoding/json"
	"maps"

	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CacheStorage defines the interface for cache persistence backends
type CacheStorage interface {
	// Read reads cache data from storage
	Read() (*InputPeerCache, error)

	// Write writes cache data to storage
	Write(*InputPeerCache) error

	// Close closes the storage backend
	Close() error
}

// FileCacheStorage implements CacheStorage for file-based persistence
type FileCacheStorage struct {
	path string
}

func NewFileCacheStorage(path string) *FileCacheStorage {
	return &FileCacheStorage{path: path}
}

// SetPath updates the storage path (useful for user-specific cache files)
func (f *FileCacheStorage) SetPath(path string) {
	f.path = path
}

func (f *FileCacheStorage) Read() (*InputPeerCache, error) {
	file, err := os.Open(f.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	dec := gob.NewDecoder(file)
	var peers InputPeerCache
	if err := dec.Decode(&peers); err != nil {
		return nil, err
	}
	return &peers, nil
}

func (f *FileCacheStorage) Write(peers *InputPeerCache) error {
	file, err := os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := gob.NewEncoder(file)
	return enc.Encode(peers)
}

func (f *FileCacheStorage) Close() error {
	return nil
}

// MemoryCacheStorage implements CacheStorage for in-memory only (no persistence)
type MemoryCacheStorage struct {
	data *InputPeerCache
}

func NewMemoryCacheStorage() *MemoryCacheStorage {
	return &MemoryCacheStorage{}
}

func (m *MemoryCacheStorage) Read() (*InputPeerCache, error) {
	if m.data == nil {
		return nil, os.ErrNotExist
	}
	return m.data, nil
}

func (m *MemoryCacheStorage) Write(peers *InputPeerCache) error {
	m.data = peers
	return nil
}

func (m *MemoryCacheStorage) Close() error {
	m.data = nil
	return nil
}

type CACHE struct {
	*sync.RWMutex
	fileName    string
	baseName    string
	chats       map[int64]*ChatObj
	users       map[int64]*UserObj
	channels    map[int64]*Channel
	usernameMap map[string]int64
	memory      bool
	disabled    bool
	maxSize     int
	InputPeers  *InputPeerCache `json:"input_peers,omitempty"`
	logger      Logger
	binded      bool
	storage     CacheStorage

	mediaCache   map[string]*CachedMedia
	mediaCacheMu sync.RWMutex

	wipeScheduled atomic.Bool
	writePending  atomic.Bool
	lastWrite     time.Time
	writeMu       sync.Mutex
}

type CachedMedia struct {
	FileID    string `json:"file_id"`
	CachedAt  int64  `json:"cached_at"`
	ExpiresAt int64  `json:"expires_at"`
}

type InputPeerCache struct {
	InputChannels map[int64]int64  `json:"channels,omitempty"`
	InputUsers    map[int64]int64  `json:"users,omitempty"`
	UsernameMap   map[string]int64 `json:"username_map,omitempty"`
	OwnerID       int64            `json:"owner_id,omitempty"`
}

func newInputPeerCache() *InputPeerCache {
	return &InputPeerCache{
		InputChannels: make(map[int64]int64),
		InputUsers:    make(map[int64]int64),
		UsernameMap:   make(map[string]int64),
	}
}

func (c *CACHE) ensureInputPeersLocked() {
	if c.InputPeers == nil {
		c.InputPeers = newInputPeerCache()
		return
	}
	if c.InputPeers.InputChannels == nil {
		c.InputPeers.InputChannels = make(map[int64]int64)
	}
	if c.InputPeers.InputUsers == nil {
		c.InputPeers.InputUsers = make(map[int64]int64)
	}
	if c.InputPeers.UsernameMap == nil {
		c.InputPeers.UsernameMap = make(map[string]int64)
	}
}

func (c *CACHE) resetLocked() {
	c.chats = make(map[int64]*ChatObj)
	c.users = make(map[int64]*UserObj)
	c.channels = make(map[int64]*Channel)
	c.usernameMap = make(map[string]int64)
	c.InputPeers = newInputPeerCache()
}

func (c *CACHE) fileNameForUser(userID int64) string {
	if userID == 0 || c.baseName == "" {
		return c.baseName
	}
	base := c.baseName
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = ".db"
	}
	return fmt.Sprintf("%s_%d%s", name, userID, ext)
}

func (c *CACHE) loadFileIntoLocked(path string, expectedOwnerID int64) error {
	var peers *InputPeerCache
	var err error

	if c.storage != nil {
		peers, err = c.storage.Read()
		if err != nil {
			return err
		}
	} else {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		dec := gob.NewDecoder(file)
		var p InputPeerCache
		if err := dec.Decode(&p); err != nil {
			return err
		}
		peers = &p
	}

	if expectedOwnerID != 0 && peers.OwnerID != 0 {
		if peers.OwnerID != expectedOwnerID {
			return fmt.Errorf("cache owner mismatch: expected %d, got %d", expectedOwnerID, peers.OwnerID)
		}
	}

	c.InputPeers = peers
	c.ensureInputPeersLocked()
	c.usernameMap = make(map[string]int64, len(c.InputPeers.UsernameMap))
	maps.Copy(c.usernameMap, c.InputPeers.UsernameMap)

	c.logger.WithFields(map[string]any{
		"users":     len(c.InputPeers.InputUsers),
		"channels":  len(c.InputPeers.InputChannels),
		"usernames": len(c.usernameMap),
		"owner_id":  c.InputPeers.OwnerID,
	}).Debug("cache loaded")

	return nil
}

func (c *CACHE) snapshotInputPeers() *InputPeerCache {
	c.RLock()
	defer c.RUnlock()

	var peers *InputPeerCache
	if c.InputPeers == nil {
		peers = newInputPeerCache()
	} else {
		peers = &InputPeerCache{
			InputChannels: make(map[int64]int64, len(c.InputPeers.InputChannels)),
			InputUsers:    make(map[int64]int64, len(c.InputPeers.InputUsers)),
			UsernameMap:   make(map[string]int64, len(c.usernameMap)),
			OwnerID:       c.InputPeers.OwnerID,
		}
		maps.Copy(peers.InputChannels, c.InputPeers.InputChannels)
		maps.Copy(peers.InputUsers, c.InputPeers.InputUsers)
	}
	maps.Copy(peers.UsernameMap, c.usernameMap)
	return peers
}

func (c *CACHE) BindToUser(userID int64) error {
	if c == nil || c.disabled || c.memory {
		return nil
	}

	// Allow binding with 0 initially (loads base cache file)
	// Later RebindToUser will be called with actual user ID
	if userID == 0 {
		if c.binded {
			return nil // Already bound to something
		}
		c.binded = true
		c.Lock()
		defer c.Unlock()
		// Try to load base cache file if it exists
		if c.fileName != "" {
			if err := c.loadFileIntoLocked(c.fileName, 0); err != nil {
				if !os.IsNotExist(err) {
					c.logger.WithError(err).Debug("base cache load failed")
				} else {
					c.logger.Debug("base cache missing, starting empty")
				}
			}
		}
		return nil
	}

	if c.binded {
		return nil // Already bound
	}

	c.binded = true

	target := c.fileNameForUser(userID)
	if target == "" {
		target = c.fileName
	}

	c.Lock()
	defer c.Unlock()

	if target != "" && c.fileName != target {
		// Update storage path for file-based storage
		if fStorage, ok := c.storage.(*FileCacheStorage); ok {
			fStorage.SetPath(target)
		}

		if err := c.loadFileIntoLocked(target, userID); err != nil {
			if os.IsNotExist(err) {
				// No cache file for this user yet, start fresh
				c.logger.Debug("cache missing (user %d), starting fresh", userID)
				c.resetLocked()
			} else if strings.Contains(err.Error(), "owner mismatch") {
				// Owner mismatch detected during load - don't use this cache
				c.logger.WithError(err).Warn("cache owner mismatch detected, starting fresh cache")
				c.resetLocked()
			} else {
				c.logger.WithError(err).Warn("failed to load user-specific cache, starting empty cache")
				c.resetLocked()
			}
		}
		c.fileName = target
		c.logger.Debug("cache bound (user %d): %s", userID, target)
	}

	if c.InputPeers.OwnerID != 0 {
		if c.InputPeers.OwnerID != userID {
			c.logger.WithFields(map[string]any{
				"cache_owner":  c.InputPeers.OwnerID,
				"current_user": userID,
				"cache_file":   c.fileName,
			}).Warn("cache owner mismatch after load, clearing cache")
			c.resetLocked()
		} else {
			// Valid cache for this user
			return nil
		}
	}

	c.ensureInputPeersLocked()
	c.InputPeers.OwnerID = userID
	return nil
}

// RebindToUser rebinds the cache to a different user ID
// Used when actual user ID is discovered after initial binding with 0
func (c *CACHE) RebindToUser(userID int64) error {
	if c == nil || c.disabled || c.memory || userID == 0 {
		return nil
	}

	c.Lock()
	defer c.Unlock()

	currentOwner := c.InputPeers.OwnerID
	if currentOwner == userID {
		return nil
	}

	if currentOwner == 0 {
		c.logger.WithFields(map[string]any{
			"users":      len(c.InputPeers.InputUsers),
			"channels":   len(c.InputPeers.InputChannels),
			"usernames":  len(c.usernameMap),
			"cache_file": c.fileName,
		}).Debug("adopting unbound cache (user %d)", userID)
		c.ensureInputPeersLocked()
		c.InputPeers.OwnerID = userID
		return nil
	}

	if currentOwner != userID {
		c.logger.WithFields(map[string]any{
			"old_owner": currentOwner,
			"new_owner": userID,
		}).Info("cache owner changed, rebinding...")
		c.resetLocked()
	}

	// Load user-specific cache file
	target := c.fileNameForUser(userID)
	if target == "" {
		target = c.fileName
	}

	if target != c.fileName {
		// Update storage path for file-based storage
		if fStorage, ok := c.storage.(*FileCacheStorage); ok {
			fStorage.SetPath(target)
		}

		if err := c.loadFileIntoLocked(target, userID); err != nil {
			if os.IsNotExist(err) {
				c.logger.Debug("no existing cache for user %d, starting fresh", userID)
			} else if strings.Contains(err.Error(), "owner mismatch") {
				c.logger.WithError(err).Warn("cache owner mismatch during rebind, starting fresh")
				c.resetLocked()
			} else {
				c.logger.WithError(err).Warn("failed to load cache during rebind, starting fresh")
				c.resetLocked()
			}
		} else {
			c.logger.Debug("user cache loaded: %s", target)
		}
		c.fileName = target
		c.logger.Debug("cache rebound (user %d): %s", userID, target)
	}

	// Set owner ID
	c.ensureInputPeersLocked()
	c.InputPeers.OwnerID = userID
	return nil
}

func (c *CACHE) SetWriteFile(write bool) *CACHE {
	c.memory = !write
	return c
}

func (c *CACHE) Clear() {
	c.Lock()
	defer c.Unlock()

	ownerID := c.InputPeers.OwnerID
	c.chats = make(map[int64]*ChatObj)
	c.users = make(map[int64]*UserObj)
	c.channels = make(map[int64]*Channel)
	c.usernameMap = make(map[string]int64)
	c.InputPeers = newInputPeerCache()
	c.InputPeers.OwnerID = ownerID
}

func (c *CACHE) ExportJSON() ([]byte, error) {
	c.RLock()
	defer c.RUnlock()

	return json.Marshal(c.InputPeers)
}

func (c *CACHE) ImportJSON(data []byte) error {
	c.Lock()
	defer c.Unlock()

	c.ensureInputPeersLocked()
	return json.Unmarshal(data, c.InputPeers)
}

type CacheConfig struct {
	MaxSize  int          // Maximum entries to cache (0 = unlimited)
	LogLevel LogLevel     // Log verbosity for cache operations
	LogColor bool         // Enable colored log output
	Logger   Logger       // Custom logger instance
	LogName  string       // Logger name prefix
	Memory   bool         // Keep cache in memory only (no persistence)
	Disabled bool         // Disable caching entirely
	Storage  CacheStorage // Custom storage backend (overrides file-based storage)
}

func NewCache(fileName string, opts ...*CacheConfig) *CACHE {
	opt := getVariadic(opts, &CacheConfig{
		LogLevel: InfoLevel,
	})

	c := &CACHE{
		RWMutex:     &sync.RWMutex{},
		fileName:    fileName,
		baseName:    fileName,
		chats:       make(map[int64]*ChatObj),
		users:       make(map[int64]*UserObj),
		channels:    make(map[int64]*Channel),
		usernameMap: make(map[string]int64),
		InputPeers:  newInputPeerCache(),
		mediaCache:  make(map[string]*CachedMedia),
		memory:      opt.Memory,
		disabled:    opt.Disabled,
		maxSize:     opt.MaxSize,
		logger: getValue(opt.Logger,
			NewDefaultLogger("gogram "+
				lp("cache", opt.LogName)).
				SetColor(opt.LogColor).
				SetLevel(opt.LogLevel)),
	}

	if opt.Storage != nil {
		c.storage = opt.Storage
		c.memory = false
	} else if opt.Memory {
		c.storage = NewMemoryCacheStorage()
	} else if !opt.Disabled && fileName != "" {
		c.storage = NewFileCacheStorage(fileName)
	}

	if !opt.Memory && !opt.Disabled {
		if opt.Storage != nil {
			c.logger.Debug("using custom storage backend")
		} else {
			c.logger.Debug("cache base file: %s", c.fileName)
		}
	}

	return c
}

func (c *CACHE) Disable() *CACHE {
	c.disabled = true
	return c
}

// enforceSizeLimit removes oldest entries if cache exceeds maxSize
// Must be called while holding write lock
func (c *CACHE) enforceSizeLimit() {
	if c.maxSize <= 0 {
		return // unlimited
	}

	totalSize := len(c.InputPeers.InputUsers) + len(c.InputPeers.InputChannels)
	if totalSize <= c.maxSize {
		return
	}

	excess := totalSize - c.maxSize
	removed := 0

	// Remove oldest users first (simple strategy: remove arbitrary entries)
	for userID := range c.InputPeers.InputUsers {
		if removed >= excess {
			break
		}
		delete(c.InputPeers.InputUsers, userID)
		if user, ok := c.users[userID]; ok {
			// Clean up username mapping
			if user.Username != "" {
				delete(c.usernameMap, user.Username)
			}
			delete(c.users, userID)
		}
		removed++
	}

	// If still need to remove more, remove channels
	if removed < excess {
		for channelID := range c.InputPeers.InputChannels {
			if removed >= excess {
				break
			}
			delete(c.InputPeers.InputChannels, channelID)
			if channel, ok := c.channels[channelID]; ok {
				// Clean up username mapping
				if channel.Username != "" {
					delete(c.usernameMap, channel.Username)
				}
				delete(c.channels, channelID)
			}
			removed++
		}
	}

	if removed > 0 {
		c.logger.Debug("cache limit: evicted %d entries (max=%d)", removed, c.maxSize)
	}
}

// --------- Cache file Functions ---------
func (c *CACHE) WriteFile() {
	if c.disabled || c.memory {
		return
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// debounce: don't write if we wrote recently
	if time.Since(c.lastWrite) < 2*time.Second {
		return
	}

	peers := c.snapshotInputPeers()

	var err error
	if c.storage != nil {
		err = c.storage.Write(peers)
	} else if c.fileName != "" {
		file, fileErr := os.OpenFile(c.fileName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if fileErr != nil {
			c.logger.Error("failed to open cache file: %v", fileErr)
			return
		}
		defer file.Close()

		enc := gob.NewEncoder(file)
		err = enc.Encode(peers)
	} else {
		return
	}

	if err != nil {
		c.logger.Error("failed to write cache: %v", err)
	} else {
		c.lastWrite = time.Now()
		c.writePending.Store(false)
	}
}

func (c *CACHE) ReadFile() {
	c.Lock()
	defer c.Unlock()

	var err error
	// Pass 0 as expectedOwnerID - validation will happen in BindToUser
	if c.storage != nil {
		err = c.loadFileIntoLocked("", 0)
	} else if c.fileName != "" {
		err = c.loadFileIntoLocked(c.fileName, 0)
	} else {
		return
	}

	if err != nil {
		if !os.IsNotExist(err) {
			c.logger.Error("failed to read cache: %v", err)
		}
		return
	}

	if !c.memory {
		c.logger.WithFields(map[string]any{
			"users":     len(c.InputPeers.InputUsers),
			"channels":  len(c.InputPeers.InputChannels),
			"usernames": len(c.usernameMap),
		}).Debug("loaded cache from disk")
	}
}

// SetStorage sets a custom storage backend for the cache
func (c *CACHE) SetStorage(storage CacheStorage) *CACHE {
	c.Lock()
	defer c.Unlock()

	if c.storage != nil {
		c.storage.Close()
	}

	c.storage = storage
	c.memory = false

	return c
}

// Close closes the cache and underlying storage
func (c *CACHE) Close() error {
	if c.storage != nil {
		return c.storage.Close()
	}
	return nil
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

func (c *CACHE) LookupUsername(username string) (peerID int64, accessHash int64, isChannel bool, found bool) {
	c.RLock()
	defer c.RUnlock()

	username = strings.TrimPrefix(username, "@")
	peerID, ok := c.usernameMap[username]
	if !ok {
		return 0, 0, false, false
	}

	// Check channels first
	if hash, ok := c.InputPeers.InputChannels[peerID]; ok {
		return peerID, hash, true, true
	}
	// Check users
	if hash, ok := c.InputPeers.InputUsers[peerID]; ok {
		return peerID, hash, false, true
	}

	// Username exists but access hash is missing - return found=true with 0 hash
	// Caller should handle by fetching from API
	c.logger.WithFields(map[string]any{
		"username": username,
		"peer_id":  peerID,
	}).Debug("username in cache but access hash missing, needs refresh")
	return peerID, 0, false, true
}

func (c *Client) GetInputPeer(peerID int64) (InputPeer, error) {
	// channel id (negative with -100 prefix)
	if strings.HasPrefix(strconv.FormatInt(peerID, 10), "-100") {
		channelID := trimSuffixHundred(peerID)
		c.Cache.RLock()
		if channelHash, ok := c.Cache.InputPeers.InputChannels[channelID]; ok {
			c.Cache.RUnlock()
			return &InputPeerChannel{channelID, channelHash}, nil
		}
		c.Cache.RUnlock()

		// try to fetch from Telegram
		if channel, err := c.getChannelFromCache(channelID); err == nil {
			return &InputPeerChannel{channelID, channel.AccessHash}, nil
		}

		return nil, fmt.Errorf("there is no channel with id '%d' or missing from cache", peerID)
	}

	// chat id (negative)
	if peerID < 0 {
		chatID := peerID * -1
		c.Cache.RLock()
		_, chatExists := c.Cache.chats[chatID]
		c.Cache.RUnlock()

		if chatExists {
			return &InputPeerChat{chatID}, nil
		}

		// try to fetch from Telegram
		if _, err := c.getChatFromCache(chatID); err == nil {
			return &InputPeerChat{chatID}, nil
		}

		return nil, fmt.Errorf("there is no chat with id '%d' or missing from cache", peerID)
	}

	// user id (positive)
	c.Cache.RLock()
	userHash, userExists := c.Cache.InputPeers.InputUsers[peerID]
	c.Cache.RUnlock()

	if userExists {
		return &InputPeerUser{peerID, userHash}, nil
	}

	// try to fetch from Telegram
	if user, err := c.getUserFromCache(peerID); err == nil {
		return &InputPeerUser{peerID, user.AccessHash}, nil
	}

	// check if it's a channel without -100 prefix
	c.Cache.RLock()
	channelHash, channelExists := c.Cache.InputPeers.InputChannels[peerID]
	c.Cache.RUnlock()

	if channelExists {
		return &InputPeerChannel{peerID, channelHash}, nil
	}

	return nil, fmt.Errorf("there is no peer with id '%d' or missing from cache", peerID)
}

// ------------------ Get Chat/Channel/User From Cache/Telegram ------------------

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
		// If fetch with cached access hash failed, retry with access hash = 0
		if inputPeerUser.(*InputUserObj).AccessHash != 0 {
			c.Cache.logger.WithFields(map[string]any{
				"user_id": userID,
				"error":   err.Error(),
			}).Debug("retrying user fetch with access_hash=0")
			users, err = c.UsersGetUsers([]InputUser{&InputUserObj{UserID: userID, AccessHash: 0}})
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
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

	// if channel is not in cache and if the bot is participant in the channel, try with access hash = 0
	var inputChannel InputChannel = &InputChannelObj{ChannelID: channelID, AccessHash: 0}
	if err == nil {
		inputChannel = channelPeer
	}

	channels, err := c.ChannelsGetChannels([]InputChannel{inputChannel})
	if err != nil {
		if inputChannel.(*InputChannelObj).AccessHash != 0 {
			c.Cache.logger.WithFields(map[string]any{
				"channel_id": channelID,
				"error":      err.Error(),
			}).Debug("retrying channel fetch with access_hash=0")
			channels, err = c.ChannelsGetChannels([]InputChannel{&InputChannelObj{ChannelID: channelID, AccessHash: 0}})
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
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

	// Skip min users if we already have full user data
	if user.Min {
		if existingUser, ok := c.users[user.ID]; ok && !existingUser.Min {
			// Keep the full user data, don't overwrite with min
			return false
		}
		// Update even if it's min (for first time or min->min)
		c.users[user.ID] = user
		// Don't update username map for min users to avoid inconsistency
		return false // Don't trigger file write for min users
	}

	// Full user data - always update
	c.users[user.ID] = user

	// Update username mapping only for non-min users with access hash
	if user.Username != "" {
		c.usernameMap[user.Username] = user.ID
		// Ensure InputPeers has the mapping too
		if _, ok := c.InputPeers.InputUsers[user.ID]; !ok {
			c.InputPeers.InputUsers[user.ID] = user.AccessHash
		}
	}

	// Check if access hash changed
	if currAccessHash, ok := c.InputPeers.InputUsers[user.ID]; ok {
		if currAccessHash != user.AccessHash {
			c.InputPeers.InputUsers[user.ID] = user.AccessHash
			return true
		}
		return false
	}

	// New user
	c.InputPeers.InputUsers[user.ID] = user.AccessHash

	// Enforce size limit after adding new entry
	c.enforceSizeLimit()

	return true
}

func (c *CACHE) UpdateChannel(channel *Channel) bool {
	c.Lock()
	defer c.Unlock()

	// Skip min channels if we already have full channel data
	if channel.Min {
		if existingCh, ok := c.channels[channel.ID]; ok && !existingCh.Min {
			// Keep the full channel data, don't overwrite with min
			return false
		}
		// Update even if it's min (for first time or min->min)
		c.channels[channel.ID] = channel
		// Don't update username map for min channels to avoid inconsistency
		return false // Don't trigger file write for min channels
	}

	// Full channel data - always update
	c.channels[channel.ID] = channel

	// Update username mapping only for non-min channels with access hash
	if channel.Username != "" {
		c.usernameMap[channel.Username] = channel.ID
		// Ensure InputPeers has the mapping too
		if _, ok := c.InputPeers.InputChannels[channel.ID]; !ok {
			c.InputPeers.InputChannels[channel.ID] = channel.AccessHash
		}
	}

	// Check if access hash changed
	if currAccessHash, ok := c.InputPeers.InputChannels[channel.ID]; ok {
		if currAccessHash != channel.AccessHash {
			c.InputPeers.InputChannels[channel.ID] = channel.AccessHash
			return true
		}
		return false
	}

	// New channel
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
			// Debounced async write
			if !cache.writePending.Load() {
				cache.writePending.Store(true)
				go func() {
					time.Sleep(1 * time.Second)
					cache.WriteFile()
				}()
			}
		}
		cache.RLock()
		cache.logger.WithFields(map[string]any{
			"new_users": totalUpdates[0],
			"new_chats": totalUpdates[1],
			"users":     len(cache.InputPeers.InputUsers),
			"channels":  len(cache.InputPeers.InputChannels),
			"usernames": len(cache.usernameMap),
		}).Debug("cache updated")
		cache.RUnlock()
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

	channelID = trimSuffixHundred(channelID)

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

func trimSuffixHundred(id int64) int64 {
	if id >= 0 {
		return id
	}

	s := strconv.FormatInt(id, 10)
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "100")

	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	if id < 0 {
		return -id
	}
	return id
}

// GetCachedMedia retrieves a cached media by its key (URL or file hash)
func (c *CACHE) GetCachedMedia(key string) (*CachedMedia, bool) {
	if c.disabled {
		return nil, false
	}
	c.mediaCacheMu.RLock()
	defer c.mediaCacheMu.RUnlock()

	if c.mediaCache == nil {
		return nil, false
	}

	media, ok := c.mediaCache[key]
	if !ok {
		return nil, false
	}

	if media.ExpiresAt > 0 && time.Now().Unix() > media.ExpiresAt {
		return nil, false
	}

	return media, true
}

func (c *CACHE) SetCachedMedia(key string, media *CachedMedia, ttlSeconds ...int64) {
	if c.disabled {
		return
	}
	c.mediaCacheMu.Lock()
	defer c.mediaCacheMu.Unlock()

	if c.mediaCache == nil {
		c.mediaCache = make(map[string]*CachedMedia)
	}

	media.CachedAt = time.Now().Unix()

	ttl := int64(24 * 60 * 60) // default 24 hours
	if len(ttlSeconds) > 0 {
		if ttlSeconds[0] == -1 {
			ttl = 0 // no expiry
		} else if ttlSeconds[0] > 0 {
			ttl = ttlSeconds[0]
		}
	}

	if ttl > 0 {
		media.ExpiresAt = media.CachedAt + ttl
	}

	c.mediaCache[key] = media

	if len(c.mediaCache) > 2000 {
		c.cleanupMediaCache()
	}
}

func (c *CACHE) DeleteCachedMedia(key string) {
	c.mediaCacheMu.Lock()
	defer c.mediaCacheMu.Unlock()

	delete(c.mediaCache, key)
}

func (c *CACHE) cleanupMediaCache() {
	now := time.Now().Unix()
	for key, media := range c.mediaCache {
		if media.ExpiresAt > 0 && now > media.ExpiresAt {
			delete(c.mediaCache, key)
		}
	}
}

func (c *CACHE) ClearMediaCache() {
	c.mediaCacheMu.Lock()
	defer c.mediaCacheMu.Unlock()
	c.mediaCache = make(map[string]*CachedMedia)
}
