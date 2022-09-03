// Copyright (c) 2022 RoseLoverX

package session

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/pkg/errors"
)

type genericFileSessionLoader struct {
	path       string
	lastEdited time.Time
	cached     *Session
}

var _ SessionLoader = (*genericFileSessionLoader)(nil)

func NewFromFile(path string) SessionLoader {
	return &genericFileSessionLoader{path: path}
}

func (l *genericFileSessionLoader) Load() (*Session, error) {
	info, err := os.Stat(l.path)
	switch {
	case err == nil:
	case errors.Is(err, syscall.ENOENT):
		return nil, errors.Wrap(err, "file not found")
	default:
		return nil, err
	}

	if info.ModTime().Equal(l.lastEdited) && l.cached != nil {
		return l.cached, nil
	}

	data, err := ioutil.ReadFile(l.path)
	data = decodeBytes(data)
	if err != nil {
		return nil, errors.Wrap(err, "reading file")
	}

	file := new(tokenStorageFormat)
	err = json.Unmarshal(data, file)
	if err != nil {
		return nil, errors.Wrap(err, "parsing file")
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

	return ioutil.WriteFile(l.path, encodeBytes(data), 0600)
}

type tokenStorageFormat struct {
	Key      string `json:"key"`
	Hash     string `json:"hash"`
	Salt     string `json:"salt"`
	Hostname string `json:"hostname"`
}

func (t *tokenStorageFormat) writeSession(s *Session) {
	t.Key = base64.StdEncoding.EncodeToString(s.Key)
	t.Hash = base64.StdEncoding.EncodeToString(s.Hash)
	t.Salt = encodeInt64ToBase64(s.Salt)
	t.Hostname = s.Hostname
}

func (t *tokenStorageFormat) readSession() (*Session, error) {
	s := new(Session)
	var err error

	s.Key, err = base64.StdEncoding.DecodeString(t.Key)
	if err != nil {
		return nil, errors.Wrap(err, "invalid binary data of 'key'")
	}
	s.Hash, err = base64.StdEncoding.DecodeString(t.Hash)
	if err != nil {
		return nil, errors.Wrap(err, "invalid binary data of 'hash'")
	}
	s.Salt, err = decodeInt64ToBase64(t.Salt)
	if err != nil {
		return nil, errors.Wrap(err, "invalid binary data of 'salt'")
	}
	s.Hostname = t.Hostname
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

func encodeBytes(b []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(b))
}

func decodeBytes(b []byte) []byte {
	bs := string(b)
	bd, _ := base64.StdEncoding.DecodeString(bs)
	return []byte(bd)
}
