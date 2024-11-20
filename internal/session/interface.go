// Copyright (c) 2024 RoseLoverX

package session

import "fmt"

// SessionLoader is the interface which allows you to access sessions from different storages (like
// filesystem, database, s3 storage, etc.)
type SessionLoader interface {
	Load() (*Session, error)
	Store(*Session) error
	Path() string
	Delete() error
}

// Session is a basic data of specific session. Typically, session stores default hostname of mtproto server
// (cause all accounts ties to specific server after sign in), session key, server hash and salt.
type Session struct {
	Key      []byte
	Hash     []byte
	Salt     int64
	Hostname string
	AppID    int32
}

var (
	ErrSessionNotFound = fmt.Errorf("session not found")
	ErrPathNotFound    = "file not found"
	ErrFileNotExists   = "no such file or directory"
)
