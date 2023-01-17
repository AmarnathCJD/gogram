package session

import (
	"encoding/base64"
	"strings"
)

const (
	Prefix = "1B"
)

type (
	StringSession struct {
		AuthKey     []byte
		AuthKeyHash []byte
		DCID        int
		IpAddr      string
		Encoded     string
	}
)

func (s StringSession) Encode() []byte {
	return []byte(Prefix + base64.RawURLEncoding.EncodeToString(s.AuthKey))
}

func (s StringSession) AppendAllValues() string {
	return string(s.GetAuthKey()) + "::" + string(s.GetAuthKeyHash()) + "::" + string(s.IpAddr) + "::" + string(rune(s.DCID))
}

func (s StringSession) GetAuthKey() string {
	return string(s.AuthKey)
}

func (s StringSession) GetAuthKeyHash() string {
	return string(s.AuthKeyHash)
}

func (s StringSession) EncodeToString() string {
	return Prefix + base64.RawURLEncoding.EncodeToString([]byte(s.AppendAllValues()))
}

func (s StringSession) GetDCID() int {
	return s.DCID
}

func (s StringSession) Decode() (AuthKey, AuthKeyHash []byte, DCID int, IpAddr string, err error) {
	Decoded, err := base64.RawURLEncoding.DecodeString(s.Encoded[len(Prefix):])
	if err != nil {
		return nil, nil, 0, "", err
	}
	DecodedString := string(Decoded)
	AuthKey, AuthKeyHash, IpAddr, DCID = s.SplitValues(DecodedString)
	return AuthKey, AuthKeyHash, DCID, IpAddr, nil
}

func (s StringSession) SplitValues(DecodedString string) (AuthKey, AuthKeyHash []byte, IpAddr string, DCID int) {
	Sep := strings.Split(DecodedString, "::")
	AuthKey = []byte(Sep[0])
	AuthKeyHash = []byte(Sep[1])
	IpAddr = Sep[2]
	DCID = int(Sep[3][0])
	return AuthKey, AuthKeyHash, IpAddr, DCID
}
