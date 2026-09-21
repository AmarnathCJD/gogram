// Copyright (c) 2025 @AmarnathCJD

package session

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

const (
	sessionPrefix       = "1BvE"
	sessionPrefixLegacy = "1BvX"
	sessionSeparator    = ":_:"
	legacySeparator     = "::"
)

var (
	ErrInvalidSession = errors.New("the session string is invalid/has been tampered with")
)

type StringSession struct {
	AuthKey     []byte `json:"key,omitempty"`     // AUTH_KEY
	AuthKeyHash []byte `json:"hash,omitempty"`    // AUTH_KEY_HASH
	DcID        int    `json:"dc_id,omitempty"`   // DC ID
	IpAddr      string `json:"ip_addr,omitempty"` // IP address of DC
	AppID       int32  `json:"app_id,omitempty"`  // APP_ID
}

func NewStringSession(authKey, authKeyHash []byte, dcID int, ipAddr string, appID int32) *StringSession {
	return &StringSession{
		AuthKey:     bytes.Clone(authKey),
		AuthKeyHash: bytes.Clone(authKeyHash),
		DcID:        dcID,
		IpAddr:      ipAddr,
		AppID:       appID,
	}
}

func NewEmptyStringSession() *StringSession {
	return &StringSession{}
}

func (s *StringSession) Encode() string {
	jsonSession, err := json.Marshal(s)
	if err != nil {
		return ""
	}

	return sessionPrefix + base64.RawURLEncoding.EncodeToString(jsonSession)
}

func (s *StringSession) Decode(encoded string) error {
	if s == nil || len(encoded) > 8192 {
		return ErrInvalidSession
	}
	var decoded StringSession
	if err := decoded.decode(encoded); err != nil {
		return err
	}
	credentials := &Session{Key: decoded.AuthKey, Hash: decoded.AuthKeyHash, Hostname: decoded.IpAddr, AppID: decoded.AppID}
	if decoded.DcID <= 0 || credentials.Validate() != nil {
		return ErrInvalidSession
	}
	*s = decoded
	return nil
}

func (s *StringSession) decode(encoded string) error {
	if after, ok := strings.CutPrefix(encoded, sessionPrefix); ok {
		// Decode modern json session
		decoded, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil {
			return err
		}

		err = json.Unmarshal(decoded, s)
		if err != nil {
			return err
		}

		return nil
	}

	if strings.HasPrefix(encoded, sessionPrefixLegacy) {
		// Decode legacy session with separators
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, sessionPrefixLegacy))
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
				s.AuthKey = []byte(v)
			case 1:
				s.AuthKeyHash = []byte(v)
			case 2:
				s.IpAddr = v
			case 3:
				dcId, err := strconv.Atoi(v)
				if err != nil {
					return err
				}

				s.DcID = dcId
			case 4:
				appId, err := strconv.ParseInt(v, 10, 32)
				if err != nil {
					return err
				}

				s.AppID = int32(appId)
			}
		}
		return nil
	}

	return ErrInvalidSession
}
