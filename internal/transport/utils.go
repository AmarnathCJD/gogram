// Copyright (c) 2025 @AmarnathCJD

package transport

import (
	"fmt"
	"net"
	"strings"
)

type ErrNotMultiple struct {
	Len int
}

func (e *ErrNotMultiple) Error() string {
	msg := "size of message not multiple of 4"
	if e.Len != 0 {
		return fmt.Sprintf(msg+" (got %v)", e.Len)
	}
	return msg
}

func FormatWebSocketURI(host string, tls bool, dc int, testMode bool) string {
	isIP := net.ParseIP(strings.Split(host, ":")[0]) != nil

	if tls {
		if strings.Contains(host, "web.telegram.org") {
			return fmt.Sprintf("wss://%s/apiws", host)
		}

		dcName := [...]string{"pluto", "venus", "aurora", "vesta", "flora"}
		name := "vesta"
		if dc >= 1 && dc <= 5 {
			name = dcName[dc-1]
		}
		testSuffix := ""
		if testMode {
			testSuffix = "_test"
		}
		return fmt.Sprintf("wss://%s.web.telegram.org:443/apiws%s", name, testSuffix)
	}

	if isIP {
		hostOnly := strings.Split(host, ":")[0]
		return fmt.Sprintf("ws://%s:80/apiws", hostOnly)
	}

	if strings.Contains(host, "web.telegram.org") {
		return fmt.Sprintf("ws://%s/apiws", host)
	}

	dcName := [...]string{"pluto", "venus", "aurora", "vesta", "flora"}
	name := "vesta"
	if dc >= 1 && dc <= 5 {
		name = dcName[dc-1]
	}
	testSuffix := ""
	if testMode {
		testSuffix = "_test"
	}

	return fmt.Sprintf("ws://%s.web.telegram.org:80/apiws%s", name, testSuffix)
}

func ProtocolID(variant uint8) []byte {
	switch variant {
	case 1:
		return []byte{0xee, 0xee, 0xee, 0xee}
	case 2:
		return []byte{0xdd, 0xdd, 0xdd, 0xdd}
	default:
		return []byte{0xef}
	}
}
