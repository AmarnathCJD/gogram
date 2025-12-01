// Copyright (c) 2025 @AmarnathCJD

package session

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"errors"

	aes "github.com/amarnathcjd/gogram/internal/aes_ige"
	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

const defaultAESKey = "1234567890123456"

type genericFileSessionLoader struct {
	path       string
	lastEdited time.Time
	cached     *Session
	key        string
}

var _ SessionLoader = (*genericFileSessionLoader)(nil)

func NewFromFile(path string, authAesKey string) SessionLoader {
	if authAesKey == "" {
		authAesKey = defaultAESKey
	}

	return &genericFileSessionLoader{path: path, key: authAesKey}
}

func (l *genericFileSessionLoader) Path() string {
	return l.path
}

func (l *genericFileSessionLoader) Key() string {
	return l.key
}

func (l *genericFileSessionLoader) Exists() bool {
	_, err := os.Stat(l.path)
	return err == nil
}

func (l *genericFileSessionLoader) Load() (*Session, error) {
	info, err := os.Stat(l.path)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("file not found: %w", err)
	default:
		return nil, err
	}

	if info.ModTime().Equal(l.lastEdited) && l.cached != nil {
		return l.cached, nil
	}

	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	data, err = decodeBytes(data, l.key)
	if err != nil {
		return nil, fmt.Errorf("decrypting session: %w", err)
	}

	file := new(tokenStorageFormat)
	err = json.Unmarshal(data, file)
	if err != nil {
		return nil, fmt.Errorf("parsing file: %w", err)
	}

	s, err := file.readSession()
	if err != nil {
		return nil, err
	}

	l.cached = s
	l.lastEdited = info.ModTime()

	return s, nil
}

func (l *genericFileSessionLoader) Store(s *Session) error {
	dir, _ := filepath.Split(l.path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("creating session directory: %w", err)
		}
	}
	file := new(tokenStorageFormat)
	file.writeSession(s)
	data, _ := json.Marshal(file)

	encrypted, err := encodeBytes(data, l.key)
	if err != nil {
		return fmt.Errorf("encrypting session: %w", err)
	}
	return os.WriteFile(l.path, encrypted, 0600)
}

func (l *genericFileSessionLoader) Delete() error {
	return os.Remove(l.path)
}

type tokenStorageFormat struct {
	Key      string `json:"key"`
	Hash     string `json:"hash"`
	Salt     string `json:"salt"`
	Hostname string `json:"hostname"`
	AppID    int32  `json:"app_id"`
}

func (t *tokenStorageFormat) writeSession(s *Session) {
	t.Key = base64.StdEncoding.EncodeToString(s.Key)
	t.Hash = base64.StdEncoding.EncodeToString(s.Hash)
	t.Salt = encodeInt64ToBase64(s.Salt)
	t.Hostname = s.Hostname
	t.AppID = s.AppID
}

func (t *tokenStorageFormat) readSession() (*Session, error) {
	s := new(Session)
	var err error

	s.Key, err = base64.StdEncoding.DecodeString(t.Key)
	if err != nil {
		return nil, fmt.Errorf("invalid binary data of 'key': %w", err)
	}
	s.Hash, err = base64.StdEncoding.DecodeString(t.Hash)
	if err != nil {
		return nil, fmt.Errorf("invalid binary data of 'hash': %w", err)
	}
	s.Salt, err = decodeInt64ToBase64(t.Salt)
	if err != nil {
		return nil, fmt.Errorf("invalid binary data of 'salt': %w", err)
	}
	s.Hostname = t.Hostname
	s.AppID = t.AppID
	return s, nil
}

func encodeInt64ToBase64(i int64) string {
	buf := make([]byte, tl.LongLen)
	binary.LittleEndian.PutUint64(buf, uint64(i))
	return base64.StdEncoding.EncodeToString(buf)
}

func decodeInt64ToBase64(i string) (int64, error) {
	buf, err := base64.StdEncoding.DecodeString(i)
	if err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(buf)), nil
}

func encodeBytes(b []byte, key string) ([]byte, error) {
	return aes.EncryptAES(b, key)
}

func decodeBytes(b []byte, key string) ([]byte, error) {
	return aes.DecryptAES(b, key)
}

func NewInMemory() SessionLoader {
	return &inMemorySessionLoader{}
}

type inMemorySessionLoader struct {
	mu sync.RWMutex
	s  *Session
}

var _ SessionLoader = (*inMemorySessionLoader)(nil)

func (l *inMemorySessionLoader) Path() string {
	return ":memory:"
}

func (l *inMemorySessionLoader) Key() string {
	return "in-memory"
}

func (l *inMemorySessionLoader) Exists() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.s != nil
}

func (l *inMemorySessionLoader) Load() (*Session, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.s, nil
}

func (l *inMemorySessionLoader) Store(s *Session) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.s = s
	return nil
}

func (l *inMemorySessionLoader) Delete() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.s = nil
	return nil
}
