package examples

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"

	"github.com/bs9/spread_service_gogram/telegram"
)

// This function decodes the Telethon Session String to gogram's Session Format
// Make sure the Schema Layer of Telethon Session matches with telegram.ApiVersion value
func decodeTelethonSessionString(sessionString string) (*telegram.Session, error) {
	data, err := base64.URLEncoding.DecodeString(sessionString[1:])
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %v", err)
	}

	ipLen := 4
	if len(data) == 352 {
		ipLen = 16
	}

	expectedLen := 1 + ipLen + 2 + 256
	if len(data) != expectedLen {
		return nil, fmt.Errorf("invalid session string length")
	}

	// ">B{}sH256s"
	offset := 1

	// IP Address (4 or 16 bytes based on IPv4 or IPv6)
	ipData := data[offset : offset+ipLen]
	ip := net.IP(ipData)
	ipAddress := ip.String()
	offset += ipLen

	// Port (2 bytes, Big Endian)
	port := binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2

	// Auth Key (256 bytes)
	var authKey [256]byte
	copy(authKey[:], data[offset:offset+256])

	return &telegram.Session{
		Hostname: ipAddress + ":" + fmt.Sprint(port),
		Key:      authKey[:],
	}, nil
}

func main() {
	sessionString := "<TELETHON_STRING_SESSION>"

	sess, err := decodeTelethonSessionString(sessionString)
	if err != nil {
		panic(err)
	}

	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:         6, // <App_ID>
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
