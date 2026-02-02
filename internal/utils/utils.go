// Copyright (c) 2024 RoseLoverX

package utils

import (
	cr "crypto/rand"
	"crypto/sha1"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ------------------ Telegram Data Center Configs ------------------

var DcList = DCOptions{
	DCS: map[int][]DC{
		1: {{"149.154.175.58:443", false}},
		2: {{"149.154.167.50:443", false}},
		3: {{"149.154.175.100:443", false}},
		4: {{"149.154.167.91:443", false}},
		5: {{"91.108.56.151:443", false}},
	},
}

var TestDataCenters = map[int]string{
	1: "149.154.175.10:443",
	2: "149.154.167.40:443",
	3: "149.154.175.117:443",
}

type DC struct {
	Addr string
	V    bool
}

type DCOptions struct {
	DCS map[int][]DC
}

func SetDCs(dcs map[int][]DC) {
	DcList.DCS = dcs
}

func GetAddr(dc int) (string, bool) {
	if addrs, ok := DcList.DCS[dc]; ok {
		return addrs[0].Addr, addrs[0].V
	}
	return "", false
}

func GetHostIp(dc int, test bool, ipv6 bool) string {
	dcMap, ok := DcList.DCS[dc]
	if !ok {
		return ""
	}

	if test {
		if addr, ok := TestDataCenters[dc]; ok {
			return addr
		}
	}

	if ipv6 {
		for _, dc := range dcMap {
			if dc.V {
				return dc.Addr
			}
		}
	}

	for _, dc := range dcMap {
		if !dc.V {
			return dc.Addr
		}
	}

	return dcMap[0].Addr
}

func SearchAddr(addr string) int {
	for dc, addrs := range DcList.DCS {
		for _, a := range addrs {
			if a.Addr == addr {
				return dc
			}
		}
	}

	if strings.Contains(addr, "91.108.56") {
		return 5
	} else if strings.Contains(addr, "149.154.175") {
		return 1
	} else if strings.Contains(addr, "149.154.167") {
		return 2
	}

	return 4
}

type PingParams struct {
	PingID int64
}

func (*PingParams) CRC() uint32 {
	return 0x7abe77ec
}

type UpdatesGetStateParams struct{}

func NewMsgIDGenerator() func(timeOffset int64) int64 {
	var (
		mu        sync.Mutex
		lastMsgID int64
	)
	return func(timeOffset int64) int64 {
		mu.Lock()
		defer mu.Unlock()

		now_time := time.Now().Add(time.Duration(timeOffset) * time.Second)
		now := now_time.UnixNano()

		nowSec := now / int64(time.Second)
		nowNano := (now % int64(time.Second)) & -4 // mod 4

		msgID := (nowSec << 32) | nowNano
		if msgID <= lastMsgID {
			msgID = lastMsgID + 4
		}

		lastMsgID = msgID
		return msgID
	}
}

func AuthKeyHash(key []byte) []byte {
	return Sha1Byte(key)[12:20]
}

func GenerateSessionID() int64 {
	source := rand.NewSource(time.Now().UnixNano())
	return rand.New(source).Int63()
}

func FullStack() {
	buf := make([]byte, 1024)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			fmt.Fprintln(os.Stderr, string(buf[:n]))
		}
		buf = make([]byte, 2*len(buf))
	}
}

func Sha1Byte(input []byte) []byte {
	r := sha1.Sum(input)
	return r[:]
}

func Sha1(input string) []byte {
	r := sha1.Sum([]byte(input))
	return r[:]
}

func RandomBytes(size int) []byte {
	b := make([]byte, size)
	_, _ = cr.Read(b)
	return b
}

func Xor(dst, src []byte) {
	for i := range dst {
		dst[i] ^= src[i]
	}
}

func AskForConfirmation() bool {
	var response string
	_, _ = fmt.Scanln(&response)
	return response == "y" || response == "Y"
}
