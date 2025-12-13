// Copyright (c) 2025 @AmarnathCJD

package utils

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"maps"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

var (
	RegexpDCMigrate = regexp.MustCompile(`DC (\d+)`)
)

// ------------------ Telegram Data Center Configs ------------------

var DCList = DCOptions{
	DCs: map[int][]DC{
		1: {{Addr: "149.154.175.58:443", IPv6: false}},
		2: {{Addr: "149.154.167.50:443", IPv6: false}},
		3: {{Addr: "149.154.175.100:443", IPv6: false}},
		4: {
			{Addr: "149.154.167.91:443", IPv6: false},
			{Addr: "[2001:067c:04e8:f002::a]:443", IPv6: true}, // dc2 ipv6, since ipv6 of dc4 is unreachable
		},
		5: {{Addr: "91.108.56.151:443", IPv6: false}},
	},
	TestDCs: map[int]string{
		1: "149.154.175.10:443",
		2: "149.154.167.40:443",
		3: "149.154.175.117:443",
	},
	CDNDCs: map[int][]DC{
		201: {{Addr: "91.108.23.100:443", IPv6: false}},
		203: {
			{Addr: "91.105.192.100:443", IPv6: false},
			{Addr: "[2a0a:f280:0203:000a:5000:0000:0000:0100]:443", IPv6: true},
		},
	},
}

type DC struct {
	Addr string
	IPv6 bool
}

type DCOptions struct {
	DCs     map[int][]DC
	TestDCs map[int]string
	CDNDCs  map[int][]DC
}

func NewDCOptions() *DCOptions {
	return &DCOptions{
		DCs:     maps.Clone(DCList.DCs),
		TestDCs: maps.Clone(DCList.TestDCs),
		CDNDCs:  maps.Clone(DCList.CDNDCs),
	}
}

func (opt *DCOptions) SetDCs(dcs map[int][]DC, cdnDCs map[int][]DC) {
	for id, newDCs := range dcs {
		opt.DCs[id] = mergeUnique(opt.DCs[id], newDCs)
	}

	for id, newCDNs := range cdnDCs {
		opt.CDNDCs[id] = mergeUnique(opt.CDNDCs[id], newCDNs)
	}
}

func mergeUnique(existing, new []DC) []DC {
	seen := make(map[DC]struct{}, len(existing)+len(new))
	result := make([]DC, 0, len(existing)+len(new))

	for _, dc := range existing {
		if _, exists := seen[dc]; !exists {
			seen[dc] = struct{}{}
			result = append(result, dc)
		}
	}

	for _, dc := range new {
		if _, exists := seen[dc]; !exists {
			seen[dc] = struct{}{}
			result = append(result, dc)
		}
	}

	return result
}

func (opt *DCOptions) GetCDNAddr(dc int) (string, bool) {
	addrs, ok := opt.CDNDCs[dc]
	if !ok || len(addrs) == 0 {
		return "", false
	}
	return addrs[0].Addr, addrs[0].IPv6
}

func (opt *DCOptions) GetHostIP(dc int, test, ipv6 bool) string {
	if test {
		if addr, ok := opt.TestDCs[dc]; ok {
			return addr
		}
	}

	dcAddrs, ok := opt.DCs[dc]
	if !ok {
		return ""
	}

	return selectDCAddr(dcAddrs, ipv6)
}

func GetDefaultHostIP(dc int, test, ipv6 bool) string {
	if test {
		if addr, ok := DCList.TestDCs[dc]; ok {
			return addr
		}
	}

	dcAddrs, ok := DCList.DCs[dc]
	if !ok {
		return ""
	}

	return selectDCAddr(dcAddrs, ipv6)
}

func selectDCAddr(dcs []DC, preferIPv6 bool) string {
	if len(dcs) == 0 {
		return ""
	}

	if preferIPv6 {
		for _, dc := range dcs {
			if dc.IPv6 {
				return dc.Addr
			}
		}
	}

	for _, dc := range dcs {
		if !dc.IPv6 {
			return FmtIP(dc.Addr)
		}
	}

	return dcs[0].Addr
}

func (opt *DCOptions) SearchAddr(addr string) int {
	for dcID, addrs := range opt.DCs {
		for _, dc := range addrs {
			if dc.Addr == addr {
				return dcID
			}
		}
	}

	switch {
	case strings.Contains(addr, "91.108.56"):
		return 5
	case strings.Contains(addr, "149.154.175"):
		return 1
	case strings.Contains(addr, "149.154.167"):
		return 2
	default:
		return 4
	}
}

func FmtIP(ipv6WithPort string) string {
	if strings.Contains(ipv6WithPort, ".") || strings.HasPrefix(ipv6WithPort, "[") {
		return ipv6WithPort
	}

	lastColon := strings.LastIndex(ipv6WithPort, ":")
	if lastColon == -1 {
		return ipv6WithPort
	}

	host := strings.Trim(ipv6WithPort[:lastColon], "[]")
	port := ipv6WithPort[lastColon+1:]

	ip := net.ParseIP(host)
	if ip == nil {
		return ipv6WithPort
	}

	return net.JoinHostPort(ip.String(), port)
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
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return int64(binary.BigEndian.Uint64(b))
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
	_, _ = rand.Read(b)
	return b
}

func Xor(dst, src []byte) {
	for i := range dst {
		dst[i] ^= src[i]
	}
}

func RandomSenderID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 2)
	for i := range b {
		buf := make([]byte, 1)
		_, err := rand.Read(buf)
		if err != nil {
			return ""
		}
		b[i] = chars[int(buf[0])%len(chars)]
	}
	return string(b)
}

func OrDefault[T comparable](val, def T) T {
	var zero T
	if val == zero {
		return def
	}
	return val
}

func AskForConfirmation() bool {
	var response string
	_, _ = fmt.Scanln(&response)
	return response == "y" || response == "Y"
}

func FmtMethod(data tl.Object) string {
	if data == nil {
		return "nil"
	}
	return strings.TrimSuffix(strings.TrimPrefix(fmt.Sprintf("%T", data), "*telegram."), "Params")
}

func MinSafeDuration(d int) time.Duration {
	if d == 0 {
		return 60 * time.Second
	}
	return time.Duration(d) * time.Second
}

func IsTransportError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "transport is closed") ||
		strings.Contains(errStr, "connection was forcibly closed") ||
		strings.Contains(errStr, "connection reset by peer") ||
		strings.Contains(errStr, "broken pipe")
}

// ------------------ Proxy Configuration ------------------

type Proxy struct {
	Type     string // socks4, socks5, http, https, mtproxy
	Host     string
	Port     int
	Username string
	Password string
	Secret   string // for MTProxy
}

func (p *Proxy) GetHost() string     { return p.Host }
func (p *Proxy) GetPort() int        { return p.Port }
func (p *Proxy) GetType() string     { return p.Type }
func (p *Proxy) GetUsername() string { return p.Username }
func (p *Proxy) GetPassword() string { return p.Password }
func (p *Proxy) GetSecret() string   { return p.Secret }

func (p *Proxy) IsEmpty() bool {
	return p == nil || p.Host == ""
}

func (p *Proxy) String() string {
	if p.IsEmpty() {
		return ""
	}
	if p.Type == "mtproxy" {
		return fmt.Sprintf("mtproxy://%s@%s:%d", p.Secret, p.Host, p.Port)
	}
	if p.Username != "" {
		if p.Password != "" {
			return fmt.Sprintf("%s://%s:%s@%s:%d", p.Type, p.Username, p.Password, p.Host, p.Port)
		}
		return fmt.Sprintf("%s://%s@%s:%d", p.Type, p.Username, p.Host, p.Port)
	}
	return fmt.Sprintf("%s://%s:%d", p.Type, p.Host, p.Port)
}

func (p *Proxy) ToURL() *url.URL {
	if p.IsEmpty() {
		return nil
	}

	u := &url.URL{
		Scheme: p.Type,
		Host:   fmt.Sprintf("%s:%d", p.Host, p.Port),
	}

	if p.Username != "" {
		if p.Password != "" {
			u.User = url.UserPassword(p.Username, p.Password)
		} else {
			u.User = url.User(p.Username)
		}
	}

	return u
}
