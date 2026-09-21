// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"container/list"
	"encoding/gob"
	"encoding/json"
	"errors"
	"io"
	"maps"

	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amarnathcjd/gogram/internal/utils"

	"slices"
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
	mu   sync.Mutex
	path string
}

func NewFileCacheStorage(path string) *FileCacheStorage {
	return &FileCacheStorage{path: path}
}

// SetPath updates the storage path (useful for user-specific cache files)
func (f *FileCacheStorage) SetPath(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.path = path
}

func (f *FileCacheStorage) Read() (*InputPeerCache, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	f.mu.Lock()
	defer f.mu.Unlock()
	return utils.AtomicWriteFile(f.path, 0600, func(w io.Writer) error { return gob.NewEncoder(w).Encode(peers) })
}

func (f *FileCacheStorage) Close() error {
	return nil
}

// MemoryCacheStorage implements CacheStorage for in-memory only (no persistence)
type MemoryCacheStorage struct {
	mu   sync.RWMutex
	data *InputPeerCache
}

func NewMemoryCacheStorage() *MemoryCacheStorage {
	return &MemoryCacheStorage{}
}

func (m *MemoryCacheStorage) Read() (*InputPeerCache, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.data == nil {
		return nil, os.ErrNotExist
	}
	return cloneInputPeerCache(m.data), nil
}

func (m *MemoryCacheStorage) Write(peers *InputPeerCache) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = cloneInputPeerCache(peers)
	return nil
}

func (m *MemoryCacheStorage) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
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

	minChannels map[int64]int64
	minUsers    map[int64]int64

	mediaCache   map[string]*CachedMedia
	mediaCacheMu sync.RWMutex
	mediaOrder   *list.List
	mediaIndex   map[string]*list.Element

	wipeScheduled atomic.Bool
	writePending  atomic.Bool
	lastWrite     time.Time
	writeMu       sync.Mutex
	writeTimer    *time.Timer
	wipeTimer     *time.Timer
	closed        bool

	lru           *list.List
	lruIndex      map[cachePeerKey]*list.Element
	peerUsernames map[cachePeerKey][]string
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

func cloneInputPeerCache(peers *InputPeerCache) *InputPeerCache {
	if peers == nil {
		return nil
	}
	return &InputPeerCache{OwnerID: peers.OwnerID, InputUsers: maps.Clone(peers.InputUsers), InputChannels: maps.Clone(peers.InputChannels), UsernameMap: maps.Clone(peers.UsernameMap)}
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
	c.minChannels = make(map[int64]int64)
	c.minUsers = make(map[int64]int64)
	c.lru = list.New()
	c.lruIndex = make(map[cachePeerKey]*list.Element)
	c.peerUsernames = make(map[cachePeerKey][]string)
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

	if peers == nil {
		return errors.New("cache storage returned no peer data")
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
	c.rebuildLRULocked()

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
	if c == nil || userID == 0 {
		return nil
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.Lock()
	defer c.Unlock()
	if c.disabled || c.memory {
		return nil
	}

	if c.binded {
		return nil
	}
	c.binded = true

	target := c.fileNameForUser(userID)
	if target == "" {
		target = c.fileName
	}

	if target != "" && c.fileName != target {
		if fStorage, ok := c.storage.(*FileCacheStorage); ok {
			fStorage.SetPath(target)
		}

		if err := c.loadFileIntoLocked(target, userID); err != nil {
			if os.IsNotExist(err) {
				c.logger.Debug("cache missing (user %d), starting fresh", userID)
				c.resetLocked()
			} else if strings.Contains(err.Error(), "owner mismatch") {
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
			return nil
		}
	}

	c.ensureInputPeersLocked()
	c.InputPeers.OwnerID = userID
	return nil
}

func (c *CACHE) SetWriteFile(write bool) *CACHE {
	c.Lock()
	defer c.Unlock()
	c.memory = !write
	return c
}

func (c *CACHE) Clear() {
	c.Lock()
	defer c.Unlock()

	ownerID := c.InputPeers.OwnerID
	c.resetLocked()
	c.InputPeers.OwnerID = ownerID
}

func (c *CACHE) ExportJSON() ([]byte, error) {
	return json.Marshal(c.snapshotInputPeers())
}

func (c *CACHE) ImportJSON(data []byte) error {
	var peers *InputPeerCache
	if err := json.Unmarshal(data, &peers); err != nil {
		return err
	}
	if peers == nil {
		return errors.New("cache import must contain an object")
	}
	c.Lock()
	defer c.Unlock()
	c.resetLocked()
	c.InputPeers = peers
	c.ensureInputPeersLocked()
	maps.Copy(c.usernameMap, peers.UsernameMap)
	c.rebuildLRULocked()
	return nil
}

type CacheConfig struct {
	MaxSize  int          // Maximum cached peers (0 = 10000, negative = unlimited)
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
		minChannels: make(map[int64]int64),
		minUsers:    make(map[int64]int64),
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
	if c.maxSize == 0 {
		c.maxSize = 10000
	}
	c.lru = list.New()
	c.lruIndex = make(map[cachePeerKey]*list.Element)
	c.peerUsernames = make(map[cachePeerKey][]string)

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
	c.Lock()
	defer c.Unlock()
	c.disabled = true
	return c
}

// --------- Cache file Functions ---------
func (c *CACHE) WriteFile() {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return
	}
	c.RLock()
	disabled := c.disabled || c.memory
	c.RUnlock()
	if disabled {
		c.writePending.Store(false)
		return
	}

	if wait := 2*time.Second - time.Since(c.lastWrite); wait > 0 {
		c.scheduleWriteLocked(wait)
		return
	}
	if c.writeTimer != nil {
		c.writeTimer.Stop()
		c.writeTimer = nil
	}
	c.writeFileLocked()
}

// scheduleWriteLocked coalesces all writes into one timer, including explicit
// WriteFile calls. A burst must never spawn a sleeping goroutine per call.
func (c *CACHE) scheduleWriteLocked(delay time.Duration) {
	if c.closed || c.writeTimer != nil {
		return
	}
	c.writePending.Store(true)
	c.writeTimer = time.AfterFunc(delay, func() {
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		c.writeTimer = nil
		if !c.closed {
			c.writeFileLocked()
		}
	})
}

func (c *CACHE) writeFileLocked() error {
	c.RLock()
	disabled := c.disabled || c.memory
	c.RUnlock()
	if disabled {
		c.writePending.Store(false)
		return nil
	}
	peers := c.snapshotInputPeers()

	var err error
	if c.storage != nil {
		err = c.storage.Write(peers)
	} else if c.fileName != "" {
		err = NewFileCacheStorage(c.fileName).Write(peers)
	} else {
		c.writePending.Store(false)
		return nil
	}

	if err != nil {
		c.logger.Error("failed to write cache: %v", err)
		c.writePending.Store(true)
		c.scheduleWriteLocked(2 * time.Second)
		return err
	} else {
		c.lastWrite = time.Now()
	}
	c.writePending.Store(false)
	return nil
}

func (c *CACHE) ReadFile() {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
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
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
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
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return nil
	}
	if c.writeTimer != nil {
		c.writeTimer.Stop()
		c.writeTimer = nil
	}
	if c.wipeTimer != nil {
		c.wipeTimer.Stop()
		c.wipeTimer = nil
	}
	c.closed = true
	var err error
	if c.writePending.Load() {
		err = c.writeFileLocked()
	}
	if c.storage != nil {
		err = errors.Join(err, c.storage.Close())
	}
	return err
}

func (c *CACHE) getUserPeer(userID int64) (InputUser, error) {
	c.Lock()
	defer c.Unlock()

	if userHash, ok := c.InputPeers.InputUsers[userID]; ok {
		c.touchUserLRU(userID)
		return &InputUserObj{UserID: userID, AccessHash: userHash}, nil
	}

	return nil, fmt.Errorf("no user with id '%d' or missing from cache", userID)
}

func (c *CACHE) getChannelPeer(channelID int64) (InputChannel, error) {
	c.Lock()
	defer c.Unlock()

	if channelHash, ok := c.InputPeers.InputChannels[channelID]; ok {
		c.touchChannelLRU(channelID)
		return &InputChannelObj{ChannelID: channelID, AccessHash: channelHash}, nil
	}

	return nil, fmt.Errorf("no channel with id '%d' or missing from cache", channelID)
}

func (c *CACHE) LookupUsername(username string) (peerID int64, accessHash int64, isChannel bool, found bool) {
	c.RLock()
	defer c.RUnlock()

	username = normalizeUsername(username)
	peerID, ok := c.usernameMap[username]
	if !ok {
		return 0, 0, false, false
	}

	if peerID < 0 {
		hash, ok := c.InputPeers.InputChannels[-peerID]
		return -peerID, hash, true, ok
	}
	// Check users
	if hash, ok := c.InputPeers.InputUsers[peerID]; ok {
		return peerID, hash, false, true
	}
	if hash, ok := c.InputPeers.InputChannels[peerID]; ok {
		return peerID, hash, true, true
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

	// check if it's a channel without -100 prefix before hitting the network
	c.Cache.RLock()
	channelHash, channelExists := c.Cache.InputPeers.InputChannels[peerID]
	c.Cache.RUnlock()

	if channelExists {
		return &InputPeerChannel{peerID, channelHash}, nil
	}

	user, err := c.getUserFromCache(peerID)
	if err == nil {
		return &InputPeerUser{peerID, user.AccessHash}, nil
	}

	return nil, fmt.Errorf("resolving peer '%d': %w", peerID, err)
}

// ------------------ Get Chat/Channel/User From Cache/Telegram ------------------

func (c *Client) getUserFromCache(userID int64) (*UserObj, error) {
	value, err := c.fetchPeerOnce(cachePeerKey{'u', userID}, func() (any, error) { return c.fetchUser(userID) })
	if err != nil {
		return nil, err
	}
	return value.(*UserObj), nil
}

func (c *Client) fetchUser(userID int64) (*UserObj, error) {
	c.Cache.Lock()
	if user, found := c.Cache.users[userID]; found {
		c.Cache.touchUserLRU(userID)
		c.Cache.Unlock()
		return user, nil
	}
	c.Cache.Unlock()

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

	switch u := users[0].(type) {
	case *UserObj:
		c.Cache.UpdateUser(u)
		return u, nil
	case *UserEmpty:
		return nil, fmt.Errorf("user '%d' is unknown to this account; a valid access_hash is required to resolve them", userID)
	default:
		return nil, fmt.Errorf("expected UserObj for id '%d', but got %T", userID, users[0])
	}
}

func (c *Client) getChannelFromCache(channelID int64) (*Channel, error) {
	value, err := c.fetchPeerOnce(cachePeerKey{'c', channelID}, func() (any, error) { return c.fetchChannel(channelID) })
	if err != nil {
		return nil, err
	}
	return value.(*Channel), nil
}

func (c *Client) fetchChannel(channelID int64) (*Channel, error) {
	c.Cache.Lock()
	if channel, found := c.Cache.channels[channelID]; found {
		c.Cache.touchChannelLRU(channelID)
		c.Cache.Unlock()
		return channel, nil
	}
	c.Cache.Unlock()

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

	switch ch := channelsObj.Chats[0].(type) {
	case *Channel:
		c.Cache.UpdateChannel(ch)
		return ch, nil
	case *ChannelForbidden:
		return nil, fmt.Errorf("channel '%d' is forbidden (access denied)", channelID)
	case *ChatEmpty:
		return nil, fmt.Errorf("channel '%d' is unknown to this account; a valid access_hash is required", channelID)
	default:
		return nil, fmt.Errorf("expected Channel for id '%d', but got %T", channelID, channelsObj.Chats[0])
	}
}

func (c *Client) getChatFromCache(chatID int64) (*ChatObj, error) {
	value, err := c.fetchPeerOnce(cachePeerKey{'g', chatID}, func() (any, error) { return c.fetchChat(chatID) })
	if err != nil {
		return nil, err
	}
	return value.(*ChatObj), nil
}

func (c *Client) fetchChat(chatID int64) (*ChatObj, error) {
	c.Cache.Lock()
	if chat, found := c.Cache.chats[chatID]; found {
		c.Cache.touchLRU(cachePeerKey{'g', chatID})
		c.Cache.Unlock()
		return chat, nil
	}
	c.Cache.Unlock()

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
	if user == nil {
		return false
	}
	c.Lock()
	defer c.Unlock()
	defer c.enforceSizeLimit()
	c.touchUserLRU(user.ID)

	if user.Min {
		if existingUser, ok := c.users[user.ID]; ok && !existingUser.Min {
			return false
		}
		c.users[user.ID] = user
		if user.AccessHash != 0 {
			if _, hasReal := c.InputPeers.InputUsers[user.ID]; !hasReal {
				c.minUsers[user.ID] = user.AccessHash
			}
		}
		return false
	}

	c.users[user.ID] = user
	delete(c.minUsers, user.ID)

	usernamesChanged := c.updateUsernames(cachePeerKey{kind: 'u', id: user.ID}, user.Username, user.Usernames)

	if currAccessHash, ok := c.InputPeers.InputUsers[user.ID]; ok {
		c.touchUserLRU(user.ID)
		if currAccessHash != user.AccessHash {
			c.InputPeers.InputUsers[user.ID] = user.AccessHash
			return true
		}
		return usernamesChanged
	}

	c.InputPeers.InputUsers[user.ID] = user.AccessHash
	c.touchUserLRU(user.ID)
	c.enforceSizeLimit()
	return true
}

func (c *CACHE) UpdateChannel(channel *Channel) bool {
	if channel == nil {
		return false
	}
	c.Lock()
	defer c.Unlock()
	defer c.enforceSizeLimit()
	c.touchChannelLRU(channel.ID)

	if channel.Min {
		if existingCh, ok := c.channels[channel.ID]; ok && !existingCh.Min {
			return false
		}
		c.channels[channel.ID] = channel
		if channel.AccessHash != 0 {
			if _, hasReal := c.InputPeers.InputChannels[channel.ID]; !hasReal {
				c.minChannels[channel.ID] = channel.AccessHash
			}
		}
		return false
	}

	c.channels[channel.ID] = channel
	delete(c.minChannels, channel.ID)

	usernamesChanged := c.updateUsernames(cachePeerKey{kind: 'c', id: channel.ID}, channel.Username, channel.Usernames)

	if currAccessHash, ok := c.InputPeers.InputChannels[channel.ID]; ok {
		c.touchChannelLRU(channel.ID)
		if currAccessHash != channel.AccessHash {
			c.InputPeers.InputChannels[channel.ID] = channel.AccessHash
			return true
		}
		return usernamesChanged
	}

	c.InputPeers.InputChannels[channel.ID] = channel.AccessHash
	c.touchChannelLRU(channel.ID)
	c.enforceSizeLimit()
	return true
}

func (c *CACHE) UpdateChat(chat *ChatObj) bool {
	if chat == nil {
		return false
	}
	c.Lock()
	defer c.Unlock()
	c.chats[chat.ID] = chat
	c.touchLRU(cachePeerKey{kind: 'g', id: chat.ID})
	c.enforceSizeLimit()

	return true
}

func (cache *CACHE) UpdatePeersToCache(users []User, chats []Chat) {
	cache.RLock()
	disabled, memory := cache.disabled, cache.memory
	cache.RUnlock()
	if disabled && cache.wipeScheduled.CompareAndSwap(false, true) {
		// schedule a wipe of the cache after 20 seconds
		cache.writeMu.Lock()
		if !cache.closed {
			cache.wipeTimer = time.AfterFunc(20*time.Second, func() {
				cache.writeMu.Lock()
				defer cache.writeMu.Unlock()
				if !cache.closed {
					cache.Clear()
				}
				cache.wipeTimer = nil
				cache.wipeScheduled.Store(false)
			})
		}
		cache.writeMu.Unlock()
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
			cache.touchLRU(cachePeerKey{kind: 'g', id: ch.ID})
			cache.enforceSizeLimit()
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
				cache.touchChannelLRU(ch.ID)
			}
			cache.touchChannelLRU(ch.ID)
			cache.enforceSizeLimit()
			cache.Unlock()
		case *Community:
			cache.Lock()
			if ch.Min {
				if existing, ok := cache.channels[ch.ID]; !ok || existing.Min {
					cache.channels[ch.ID] = &Channel{ID: ch.ID, AccessHash: ch.AccessHash, Title: ch.Title, Min: true}
					if ch.AccessHash != 0 {
						if _, hasReal := cache.InputPeers.InputChannels[ch.ID]; !hasReal {
							cache.minChannels[ch.ID] = ch.AccessHash
						}
					}
				}
			} else {
				cache.channels[ch.ID] = &Channel{ID: ch.ID, AccessHash: ch.AccessHash, Title: ch.Title}
				delete(cache.minChannels, ch.ID)
				if curr, ok := cache.InputPeers.InputChannels[ch.ID]; !ok || curr != ch.AccessHash {
					cache.InputPeers.InputChannels[ch.ID] = ch.AccessHash
					cache.touchChannelLRU(ch.ID)
					cache.enforceSizeLimit()
					totalUpdates[1]++
				} else {
					cache.touchChannelLRU(ch.ID)
				}
			}
			cache.touchChannelLRU(ch.ID)
			cache.enforceSizeLimit()
			cache.Unlock()
		case *CommunityForbidden:
			cache.Lock()
			if _, ok := cache.InputPeers.InputChannels[ch.ID]; !ok {
				cache.channels[ch.ID] = &Channel{ID: ch.ID, AccessHash: ch.AccessHash, Title: ch.Title}
				cache.InputPeers.InputChannels[ch.ID] = ch.AccessHash
				cache.touchChannelLRU(ch.ID)
			}
			cache.touchChannelLRU(ch.ID)
			cache.enforceSizeLimit()
			cache.Unlock()
		case *ChatEmpty:
		}
	}

	if totalUpdates[0] > 0 || totalUpdates[1] > 0 {
		if !memory && !disabled {
			cache.writeMu.Lock()
			cache.scheduleWriteLocked(max(time.Second, 2*time.Second-time.Since(cache.lastWrite)))
			cache.writeMu.Unlock()
		}
		if cache.logger.Lev() <= DebugLevel {
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
	const channelOffset int64 = -1000000000000
	if id < channelOffset {
		return channelOffset - id
	}
	return -id
}

// GetCachedMedia retrieves a cached media by its key (URL or file hash)
func (c *CACHE) GetCachedMedia(key string) (*CachedMedia, bool) {
	c.RLock()
	disabled := c.disabled
	c.RUnlock()
	if disabled {
		return nil, false
	}
	c.mediaCacheMu.Lock()
	defer c.mediaCacheMu.Unlock()
	media := c.mediaCache[key]
	if media == nil {
		return nil, false
	}
	if media.ExpiresAt > 0 && time.Now().Unix() >= media.ExpiresAt {
		c.deleteCachedMediaLocked(key)
		return nil, false
	}
	if e := c.mediaIndex[key]; e != nil {
		c.mediaOrder.MoveToBack(e)
	}
	copy := *media
	return &copy, true
}

func (c *CACHE) SetCachedMedia(key string, media *CachedMedia, ttlSeconds ...int64) {
	if media == nil {
		return
	}
	c.RLock()
	disabled := c.disabled
	c.RUnlock()
	if disabled {
		return
	}
	copy := *media
	copy.CachedAt = time.Now().Unix()
	ttl := int64(24 * 60 * 60)
	if len(ttlSeconds) > 0 {
		if ttlSeconds[0] == -1 {
			ttl = 0
		} else if ttlSeconds[0] > 0 {
			ttl = ttlSeconds[0]
		}
	}
	copy.ExpiresAt = 0
	if ttl > 0 {
		copy.ExpiresAt = copy.CachedAt + ttl
	}
	c.mediaCacheMu.Lock()
	defer c.mediaCacheMu.Unlock()
	if c.mediaCache == nil {
		c.mediaCache = make(map[string]*CachedMedia)
	}
	if c.mediaOrder == nil {
		c.mediaOrder = list.New()
		c.mediaIndex = make(map[string]*list.Element)
	}
	c.mediaCache[key] = &copy
	if e := c.mediaIndex[key]; e != nil {
		c.mediaOrder.MoveToBack(e)
	} else {
		c.mediaIndex[key] = c.mediaOrder.PushBack(key)
	}
	for len(c.mediaCache) > mediaCacheHardCap {
		c.deleteCachedMediaLocked(c.mediaOrder.Front().Value.(string))
	}
}

func (c *CACHE) deleteCachedMediaLocked(key string) {
	delete(c.mediaCache, key)
	if e := c.mediaIndex[key]; e != nil {
		c.mediaOrder.Remove(e)
		delete(c.mediaIndex, key)
	}
}
func (c *CACHE) DeleteCachedMedia(key string) {
	c.mediaCacheMu.Lock()
	defer c.mediaCacheMu.Unlock()
	c.deleteCachedMediaLocked(key)
}

const mediaCacheHardCap = 2000

func (c *CACHE) ClearMediaCache() {
	c.mediaCacheMu.Lock()
	defer c.mediaCacheMu.Unlock()
	c.mediaCache = make(map[string]*CachedMedia)
	c.mediaOrder = list.New()
	c.mediaIndex = make(map[string]*list.Element)
}

type cachePeerKey struct {
	kind byte // user, channel, or basic group; their numeric IDs can overlap
	id   int64
}

func (c *CACHE) touchUserLRU(id int64)    { c.touchLRU(cachePeerKey{'u', id}) }
func (c *CACHE) touchChannelLRU(id int64) { c.touchLRU(cachePeerKey{'c', id}) }

func (c *CACHE) touchLRU(key cachePeerKey) {
	if c.maxSize < 0 {
		return
	}
	if e := c.lruIndex[key]; e != nil {
		c.lru.MoveToBack(e)
		return
	}
	c.lruIndex[key] = c.lru.PushBack(key)
}

func normalizeUsername(name string) string { return strings.ToLower(strings.TrimPrefix(name, "@")) }

func (c *CACHE) updateUsernames(key cachePeerKey, primary string, aliases []*Username) bool {
	var names []string
	if primary != "" {
		names = append(names, normalizeUsername(primary))
	}
	for _, alias := range aliases {
		if alias != nil && alias.Active && alias.Username != "" {
			name := normalizeUsername(alias.Username)
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	if slices.Equal(c.peerUsernames[key], names) {
		return false
	}
	c.removeUsernames(key)
	value := key.id
	if key.kind == 'c' {
		value = -value
	}
	for _, name := range names {
		c.usernameMap[name] = value
	}
	if len(names) > 0 {
		c.peerUsernames[key] = names
	}
	return true
}

func (c *CACHE) removeUsernames(key cachePeerKey) {
	value := key.id
	if key.kind == 'c' {
		value = -value
	}
	for _, name := range c.peerUsernames[key] {
		if c.usernameMap[name] == value {
			delete(c.usernameMap, name)
		}
	}
	delete(c.peerUsernames, key)
}

func (c *CACHE) enforceSizeLimit() {
	if c.maxSize <= 0 {
		return
	}
	for c.lru.Len() > c.maxSize {
		e := c.lru.Front()
		key := e.Value.(cachePeerKey)
		c.lru.Remove(e)
		delete(c.lruIndex, key)
		c.removeUsernames(key)
		switch key.kind {
		case 'u':
			delete(c.users, key.id)
			delete(c.minUsers, key.id)
			delete(c.InputPeers.InputUsers, key.id)
		case 'c':
			delete(c.channels, key.id)
			delete(c.minChannels, key.id)
			delete(c.InputPeers.InputChannels, key.id)
		case 'g':
			delete(c.chats, key.id)
		}
	}
}

func (c *CACHE) rebuildLRULocked() {
	c.lru = list.New()
	c.lruIndex = make(map[cachePeerKey]*list.Element)
	c.peerUsernames = make(map[cachePeerKey][]string)
	for id := range c.InputPeers.InputUsers {
		c.touchUserLRU(id)
	}
	for id := range c.InputPeers.InputChannels {
		c.touchChannelLRU(id)
	}
	for id := range c.users {
		c.touchUserLRU(id)
	}
	for id := range c.channels {
		c.touchChannelLRU(id)
	}
	for id := range c.chats {
		c.touchLRU(cachePeerKey{'g', id})
	}
	for name, id := range c.usernameMap {
		key := cachePeerKey{'u', id}
		if id < 0 {
			key = cachePeerKey{'c', -id}
		} else if _, ok := c.InputPeers.InputUsers[id]; !ok {
			if _, ok := c.InputPeers.InputChannels[id]; ok {
				key.kind = 'c'
				c.usernameMap[name] = -id
			} else {
				delete(c.usernameMap, name)
				continue
			}
		}
		normalized := normalizeUsername(name)
		if normalized != name {
			delete(c.usernameMap, name)
			if key.kind == 'c' {
				c.usernameMap[normalized] = -key.id
			} else {
				c.usernameMap[normalized] = key.id
			}
		}
		c.peerUsernames[key] = append(c.peerUsernames[key], normalized)
	}
	c.enforceSizeLimit()
}

type peerLookup struct {
	done  chan struct{}
	value any
	err   error
}

func (c *Client) fetchPeerOnce(key cachePeerKey, fetch func() (any, error)) (any, error) {
	c.Cache.Lock()
	var cached any
	switch key.kind {
	case 'u':
		if value := c.Cache.users[key.id]; value != nil {
			cached = value
		}
	case 'c':
		if value := c.Cache.channels[key.id]; value != nil {
			cached = value
		}
	case 'g':
		if value := c.Cache.chats[key.id]; value != nil {
			cached = value
		}
	}
	if cached != nil {
		c.Cache.touchLRU(key)
	}
	c.Cache.Unlock()
	if cached != nil {
		return cached, nil
	}

	c.peerFetchMu.Lock()
	if call := c.peerFetches[key]; call != nil {
		c.peerFetchMu.Unlock()
		<-call.done
		return call.value, call.err
	}
	if c.peerFetches == nil {
		c.peerFetches = make(map[cachePeerKey]*peerLookup)
	}
	call := &peerLookup{done: make(chan struct{}), err: errors.New("peer lookup interrupted")}
	c.peerFetches[key] = call
	c.peerFetchMu.Unlock()
	defer func() {
		c.peerFetchMu.Lock()
		delete(c.peerFetches, key)
		close(call.done)
		c.peerFetchMu.Unlock()
	}()
	call.value, call.err = fetch()
	return call.value, call.err
}
