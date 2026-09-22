// Copyright (c) 2025 @AmarnathCJD

package session

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
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
	if decoded.DcID < 0 || decoded.DcID > math.MaxInt32 || credentials.Validate() != nil {
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

	if after, ok := strings.CutPrefix(encoded, sessionPrefixLegacy); ok {
		decoded, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil {
			return err
		}
		for _, separator := range []string{sessionSeparator, legacySeparator} {
			const keySize, hashSize = 256, 8
			hashStart := keySize + len(separator)
			hashEnd := hashStart + hashSize
			if len(decoded) < hashEnd+len(separator) ||
				string(decoded[keySize:hashStart]) != separator ||
				string(decoded[hashEnd:hashEnd+len(separator)]) != separator {
				continue
			}
			suffix := string(decoded[hashEnd+len(separator):])
			appStart := strings.LastIndex(suffix, separator)
			if appStart < 0 {
				return ErrInvalidSession
			}
			dcStart := strings.LastIndex(suffix[:appStart], separator)
			if dcStart < 0 {
				return ErrInvalidSession
			}
			dcID, err := strconv.Atoi(suffix[dcStart+len(separator) : appStart])
			if err != nil {
				return err
			}
			appID, err := strconv.ParseInt(suffix[appStart+len(separator):], 10, 32)
			if err != nil {
				return err
			}
			s.AuthKey = bytes.Clone(decoded[:keySize])
			s.AuthKeyHash = bytes.Clone(decoded[hashStart:hashEnd])
			s.IpAddr = suffix[:dcStart]
			s.DcID = dcID
			s.AppID = int32(appID)
			return nil
		}
		return ErrInvalidSession
	}

	return ErrInvalidSession
}
