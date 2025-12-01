// Copyright (c) 2025 @AmarnathCJD

package session

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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

func (l *genericFileSessionLoader) Load() (*Session, error) {
	info, err := os.Stat(l.path)
	switch {
	case err == nil:
	case errors.Is(err, syscall.ENOENT):
		return nil, fmt.Errorf("file not found: %w", err)
	default:
		return nil, err
	}

	if info.ModTime().Equal(l.lastEdited) && l.cached != nil {
		return l.cached, nil
	}

	data, err := os.ReadFile(l.path)
	data = decodeBytes(data, l.key)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
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
	if stat, err := os.Stat(dir); err != nil {
		return fmt.Errorf("%v: directory not found", dir)
	} else if !stat.IsDir() {
		return fmt.Errorf("%v: not a directory", dir)
	}
	file := new(tokenStorageFormat)
	file.writeSession(s)
	data, _ := json.Marshal(file)

	return os.WriteFile(l.path, encodeBytes(data, l.key), 0600)
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

func encodeBytes(b []byte, key string) []byte {
	aesByte, _ := aes.EncryptAES(b, key)
	return aesByte
}

func decodeBytes(b []byte, key string) []byte {
	aesByte, _ := aes.DecryptAES(b, key)
	return aesByte
}

func NewInMemory() SessionLoader {
	return &inMemorySessionLoader{}
}

type inMemorySessionLoader struct {
	s *Session
}

var _ SessionLoader = (*inMemorySessionLoader)(nil)

func (l *inMemorySessionLoader) Path() string {
	return ":memory:"
}

func (l *inMemorySessionLoader) Key() string {
	return "in-memory"
}

func (l *inMemorySessionLoader) Load() (*Session, error) {
	return l.s, nil
}

func (l *inMemorySessionLoader) Store(s *Session) error {
	l.s = s
	return nil
}

func (l *inMemorySessionLoader) Delete() error {
	l.s = nil
	return nil
}
