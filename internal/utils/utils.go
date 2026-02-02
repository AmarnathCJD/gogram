// Copyright (c) 2024 RoseLoverX

package utils

import (
	cr "crypto/rand"
	"crypto/sha1"
	"fmt"
	"maps"
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
		4: {{"149.154.167.91:443", false},
			{"[2001:067c:04e8:f002::a]:443", true}}, // THIS IS DC2 IPv6
		5: {{"91.108.56.151:443", false}},
	},
	TestDCs: map[int]string{
		1: "149.154.175.10:443",
		2: "149.154.167.40:443",
		3: "149.154.175.117:443",
	},
	CdnDCs: map[int][]DC{
		201: {{"91.108.23.100:443", false}},
		203: {{"91.105.192.100:443", false},
			{"[2a0a:f280:0203:000a:5000:0000:0000:0100]:443", true}},
	},
}

// TODO: Fix DC4 Ipv6 is Unreachable

type DC struct {
	Addr string
	V    bool
}

type DCOptions struct {
	DCS     map[int][]DC
	TestDCs map[int]string
	CdnDCs  map[int][]DC
}

func NewDCOptions() *DCOptions {
	// make a copy of the default DCOptions
	opt := &DCOptions{
		DCS:     make(map[int][]DC),
		TestDCs: make(map[int]string),
		CdnDCs:  make(map[int][]DC),
	}
	maps.Copy(opt.DCS, DcList.DCS)
	maps.Copy(opt.TestDCs, DcList.TestDCs)
	maps.Copy(opt.CdnDCs, DcList.CdnDCs)
	return opt
}

func (opt *DCOptions) SetDCs(dcs map[int][]DC, cdnDcs map[int][]DC) {
	for key, newDCs := range dcs {
		opt.DCS[key] = mergeUnique(opt.DCS[key], newDCs)
	}

	for key, newCDNs := range cdnDcs {
		opt.CdnDCs[key] = mergeUnique(opt.CdnDCs[key], newCDNs)
	}
}

func mergeUnique(existing, new []DC) []DC {
	uniqueMap := make(map[DC]struct{})

	for _, dc := range existing {
		uniqueMap[dc] = struct{}{}
	}

	for _, dc := range new {
		uniqueMap[dc] = struct{}{}
	}

	result := make([]DC, 0, len(uniqueMap))
	for dc := range uniqueMap {
		result = append(result, dc)
	}

	return result
}

func (opt *DCOptions) GetCdnAddr(dc int) (string, bool) {
	if addrs, ok := opt.CdnDCs[dc]; ok {
		return addrs[0].Addr, addrs[0].V
	}
	return "", false
}

func (opt *DCOptions) GetHostIp(dc int, test bool, ipv6 bool) string {
	dcMap, ok := opt.DCS[dc]
	if !ok {
		return ""
	}

	if test {
		if addr, ok := opt.TestDCs[dc]; ok {
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
			return FmtIp(dc.Addr)
		}
	}

	return dcMap[0].Addr
}

func GetDefaultHostIp(dc int, test bool, ipv6 bool) string {
	dcMap, ok := DcList.DCS[dc]
	if !ok {
		return ""
	}

	if test {
		if addr, ok := DcList.TestDCs[dc]; ok {
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
			return FmtIp(dc.Addr)
		}
	}

	return dcMap[0].Addr
}

func (opt *DCOptions) SearchAddr(addr string) int {
	for dc, addrs := range opt.DCS {
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

func FmtIp(ipv6WithPort string) string {
	if strings.HasPrefix(ipv6WithPort, "[") || strings.Contains(ipv6WithPort, ".") {
		return ipv6WithPort
	}

	lastColon := strings.LastIndex(ipv6WithPort, ":")
	if lastColon == -1 {
		return ipv6WithPort
	}

	address := ipv6WithPort[:lastColon]
	port := ipv6WithPort[lastColon+1:]

	// 2001:0b28:f23f:f005:0000:0000:0000:000a -> [2001:0b28:f23f:f005::a]
	// convert 0000:0000:0000:0000:0000:0000:0000:000a -> ::a
	address = strings.Replace(address, "0000:0000:0000:000", ":", 1)
	// remove preceding zeros
	address = strings.Replace(address, ":0", ":", -1)

	return fmt.Sprintf("[%s]:%s", address, port)
}

func Vtcp(isV6 bool) string {
	if isV6 {
		return "Tcp6"
	}
	return "Tcp"
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
