package main

import (
	"encoding/base64"
	"fmt"

	"github.com/amarnathcjd/gogram/telegram"
)

// This function decodes the Pyrogram Session String to gogram's Session Format
func decodePyrogramSessionString(encodedString string) (*telegram.Session, error) {
	// SESSION_STRING_FORMAT: Big-endian, uint8, uint32, bool, 256-byte array, uint64, bool
	const (
		dcIDSize     = 1 // uint8
		apiIDSize    = 4 // uint32
		testModeSize = 1 // bool (uint8)
		authKeySize  = 256
		userIDSize   = 8 // uint64
		isBotSize    = 1 // bool (uint8)
	)

	// Add padding to the base64 string if necessary
	for len(encodedString)%4 != 0 {
		encodedString += "="
	}

	packedData, err := base64.URLEncoding.DecodeString(encodedString)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 string: %w", err)
	}

	expectedSize := dcIDSize + apiIDSize + testModeSize + authKeySize + userIDSize + isBotSize
	if len(packedData) != expectedSize {
		return nil, fmt.Errorf("unexpected data length: got %d, want %d", len(packedData), expectedSize)
	}

	return &telegram.Session{
		Hostname: telegram.ResolveDC(int(uint8(packedData[0])), packedData[5] != 0, false),
		AppID:    int32(uint32(packedData[1])<<24 | uint32(packedData[2])<<16 | uint32(packedData[3])<<8 | uint32(packedData[4])),
		Key:      packedData[6 : 6+authKeySize],
	}, nil
}

func main() {
	sessionString := "<PYROGRAM_STRING_SESSION>"

	sess, err := decodePyrogramSessionString(sessionString)
	if err != nil {
		panic(err)
	}

	client, err := telegram.NewClient(telegram.ClientConfig{
		//AppID:         6, // <App_ID>
		StringSession: sess.Encode(),
		MemorySession: true,
	})

	if err != nil {
		panic(err)
	}

	me, err := client.GetMe()
	if err != nil {
		panic(err)
	}

	fmt.Println(client.JSON(me, true))
}
