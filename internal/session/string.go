// Copyright (c) 2024 RoseLoverX

package session

import (
	"encoding/base64"
	"errors"
	"strings"
)

var (
	ErrInvalidSession = errors.New("the session string is invalid/has been tampered with")
)

type (
	StringSession struct {
		authKey     []byte
		authKeyHash []byte
		dcID        int
		ipAddr      string
		appID       int32
	}
)

func NewStringSession(authKey, authKeyHash []byte, dcID int, ipAddr string, appID int32) *StringSession {
	return &StringSession{
		authKey:     authKey,
		authKeyHash: authKeyHash,
		dcID:        dcID,
		ipAddr:      ipAddr,
		appID:       appID,
	}
}

func NewEmptyStringSession() *StringSession {
	return &StringSession{}
}

func (s StringSession) AuthKey() []byte {
	return s.authKey
}

func (s StringSession) AuthKeyHash() []byte {
	return s.authKeyHash
}

func (s StringSession) DcID() int {
	return s.dcID
}

func (s StringSession) IpAddr() string {
	return s.ipAddr
}

func (s StringSession) AppID() int32 {
	return s.appID
}

func (s *StringSession) Encode() string {
	stringPrefix := "1BvX"
	sessionContents := []string{
		string(s.authKey),
		string(s.authKeyHash),
		s.ipAddr,
		string(rune(s.dcID)),
		string(rune(s.appID)),
	}
	return stringPrefix + base64.RawURLEncoding.EncodeToString([]byte(strings.Join(sessionContents, "::")))
}

func (s *StringSession) Decode(encoded string) error {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded[len("1BvX"):])
	if err != nil {
		return err
	}
	decodedString := string(decoded)
	split := strings.Split(decodedString, "::")
	if len(split) != 5 {
		return ErrInvalidSession
	}
	for i, v := range split {
		switch i {
		case 0:
			s.authKey = []byte(v)
		case 1:
			s.authKeyHash = []byte(v)
		case 2:
			s.ipAddr = v
		case 3:
			s.dcID = int(v[0])
		case 4:
			s.appID = int32(v[0])
		}
	}
	return nil
}
