// Copyright (c) 2025 @AmarnathCJD

package session

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

const (
	sessionPrefix    = "1BvX"
	sessionSeparator = ":_:"
	legacySeparator  = "::"
)

var (
	ErrInvalidSession = errors.New("the session string is invalid/has been tampered with")
)

type StringSession struct {
	authKey     []byte
	authKeyHash []byte
	dcID        int
	ipAddr      string
	appID       int32
}

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
	sessionContents := []string{
		string(s.authKey),
		string(s.authKeyHash),
		s.ipAddr,
		strconv.Itoa(s.dcID),
		strconv.FormatInt(int64(s.appID), 10),
	}
	return sessionPrefix + base64.RawURLEncoding.EncodeToString([]byte(strings.Join(sessionContents, sessionSeparator)))
}

func (s *StringSession) Decode(encoded string) error {
	if len(encoded) < len(sessionPrefix) || encoded[:len(sessionPrefix)] != sessionPrefix {
		return ErrInvalidSession
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded[len(sessionPrefix):])
	if err != nil {
		return err
	}
	decodedString := string(decoded)
	split := strings.Split(decodedString, sessionSeparator)
	if len(split) != 5 {
		// try again with "::" as a separator (backward compatibility)
		split = strings.Split(decodedString, legacySeparator)
	}
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
			dcId, err := strconv.Atoi(v)
			if err != nil {
				return err
			}

			s.dcID = dcId
		case 4:
			appId, err := strconv.ParseInt(v, 10, 32)
			if err != nil {
				return err
			}

			s.appID = int32(appId)
		}
	}
	return nil
}
