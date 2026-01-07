// Copyright (c) 2025 @AmarnathCJD

package transport

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/amarnathcjd/gogram/internal/utils"
)

const maxTLSPacketLength = 2878

type MTProxyConfig struct {
	Host, Port    string
	Secret        []byte
	FakeTlsDomain []byte
}

type mtproxyConn struct {
	conn                 net.Conn
	encryptor, decryptor cipher.Stream
	config               *MTProxyConfig
	useFakeTls           bool
	isFirstWrite         bool
	obfTag               []byte
	readBuffer, tlsBuf   bytes.Buffer
}

func ParseMTProxySecret(secret string) (*MTProxyConfig, error) {
	var secretBytes []byte
	var err error

	if isHex(secret) {
		secretBytes, err = hex.DecodeString(secret)
	} else {
		secretBytes, err = base64.RawURLEncoding.DecodeString(secret)
		if err != nil {
			secretBytes, err = base64.StdEncoding.DecodeString(secret)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to decode secret: %w", err)
	}

	if len(secretBytes) < 16 || len(secretBytes) > 199 {
		return nil, errors.New("invalid secret length")
	}

	cfg := &MTProxyConfig{}
	switch {
	case len(secretBytes) == 16:
		cfg.Secret = secretBytes
	case secretBytes[0] == 0xdd:
		cfg.Secret = secretBytes[1:]
	case secretBytes[0] == 0xee && len(secretBytes) >= 18:
		cfg.Secret = secretBytes[1:17]
		cfg.FakeTlsDomain = secretBytes[17:]
	default:
		return nil, errors.New("unsupported secret format")
	}
	return cfg, nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s)%2 == 0 && len(s) > 0
}

func DialMTProxy(ctx context.Context, proxy *utils.Proxy, targetHost string, dcID int16, modeVariant uint8, localAddr string, logger *utils.Logger) (Conn, error) {
	if proxy == nil || proxy.Secret == "" {
		return nil, errors.New("mtproxy secret is required")
	}

	config, err := ParseMTProxySecret(proxy.Secret)
	if err != nil {
		return nil, err
	}
	config.Host = proxy.Host
	config.Port = fmt.Sprintf("%d", proxy.Port)

	dialer := &net.Dialer{Timeout: DefaultTimeout}
	if localAddr != "" {
		if laddr, err := net.ResolveTCPAddr("tcp", localAddr); err == nil {
			dialer.LocalAddr = laddr
		}
	}

	conn, err := dialer.DialContext(ctx, "tcp", config.Host+":"+config.Port)
	if err != nil {
		return nil, fmt.Errorf("connecting to mtproxy: %w", err)
	}

	m := &mtproxyConn{conn: conn, config: config, useFakeTls: config.FakeTlsDomain != nil, isFirstWrite: true}

	if config.FakeTlsDomain != nil {
		conn.SetDeadline(time.Now().Add(15 * time.Second))
		if err := m.fakeTlsHandshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("TLS handshake failed: %w", err)
		}
	}

	// Padded Intermediate for fake TLS, Intermediate otherwise
	protocolTag := []byte{0xee, 0xee, 0xee, 0xee}
	if config.FakeTlsDomain != nil {
		protocolTag = []byte{0xdd, 0xdd, 0xdd, 0xdd}
	}

	obfTag, err := m.initObfuscation(dcID, protocolTag)
	if err != nil {
		conn.Close()
		return nil, err
	}

	if config.FakeTlsDomain != nil {
		m.obfTag = obfTag
	} else {
		conn.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err = conn.Write(obfTag); err != nil {
			conn.Close()
			return nil, fmt.Errorf("writing obfuscation tag: %w", err)
		}
	}
	conn.SetDeadline(time.Time{})
	return m, nil
}

func (m *mtproxyConn) initObfuscation(dcID int16, protocolTag []byte) ([]byte, error) {
	forbidden := []uint32{0x44414548, 0x54534f50, 0x20544547, 0x4954504f, 0x02010316, 0xdddddddd, 0xeeeeeeee, 0xefefefef}

	var random []byte
	for {
		random = make([]byte, 64)
		rand.Read(random)
		if random[0] == 0xef || slices.Contains(forbidden, binary.LittleEndian.Uint32(random[:4])) || binary.LittleEndian.Uint32(random[4:8]) == 0 {
			continue
		}
		break
	}

	copy(random[56:60], protocolTag)
	binary.LittleEndian.PutUint16(random[60:62], uint16(dcID))

	randomRev := make([]byte, 48)
	for i := range 48 {
		randomRev[i] = random[55-i]
	}

	encKey, decKey := sha256Sum(random[8:40], m.config.Secret), sha256Sum(randomRev[:32], m.config.Secret)
	encIV, decIV := random[40:56], randomRev[32:48]

	encBlock, _ := aes.NewCipher(encKey)
	decBlock, _ := aes.NewCipher(decKey)
	m.encryptor = cipher.NewCTR(encBlock, encIV)
	m.decryptor = cipher.NewCTR(decBlock, decIV)

	encrypted := make([]byte, 64)
	m.encryptor.XORKeyStream(encrypted, random)
	copy(random[56:64], encrypted[56:64])

	return random, nil
}

func sha256Sum(data, secret []byte) []byte {
	h := sha256.New()
	h.Write(data)
	h.Write(secret)
	return h.Sum(nil)
}

func (m *mtproxyConn) fakeTlsHandshake() error {
	hello := generateFakeTlsHello(m.config.FakeTlsDomain, m.config.Secret)
	helloRand := make([]byte, 32)
	copy(helloRand, hello[11:43])

	if _, err := m.conn.Write(hello); err != nil {
		return err
	}

	tlsServerPrefixes := [][]byte{{0x16, 0x03, 0x03}, {0x14, 0x03, 0x03, 0x00, 0x01, 0x01, 0x17, 0x03, 0x03}}
	var respBuf bytes.Buffer

	for _, prefix := range tlsServerPrefixes {
		buf := make([]byte, len(prefix)+2)
		if _, err := io.ReadFull(m.conn, buf); err != nil {
			return err
		}
		respBuf.Write(buf)
		if !bytes.Equal(buf[:len(prefix)], prefix) {
			return fmt.Errorf("invalid server hello prefix")
		}
		skip := make([]byte, binary.BigEndian.Uint16(buf[len(prefix):]))
		if _, err := io.ReadFull(m.conn, skip); err != nil {
			return err
		}
		respBuf.Write(skip)
	}

	resp := respBuf.Bytes()
	var hashIn bytes.Buffer
	hashIn.Write(helloRand)
	hashIn.Write(resp[:11])
	hashIn.Write(make([]byte, 32))
	hashIn.Write(resp[43:])

	mac := hmac.New(sha256.New, m.config.Secret)
	mac.Write(hashIn.Bytes())
	if !bytes.Equal(mac.Sum(nil), resp[11:43]) {
		return errors.New("server response hash mismatch")
	}

	_, err := m.conn.Write([]byte{0x14, 0x03, 0x03, 0x00, 0x01, 0x01})
	return err
}

func (m *mtproxyConn) Write(b []byte) (int, error) {
	encrypted := make([]byte, len(b))
	m.encryptor.XORKeyStream(encrypted, b)

	if !m.useFakeTls {
		_, err := m.conn.Write(encrypted)
		return len(b), err
	}

	var result bytes.Buffer
	for offset := 0; offset < len(encrypted); {
		header := []byte{0x17, 0x03, 0x03, 0x00, 0x00}
		var payload []byte

		if m.isFirstWrite && len(m.obfTag) > 0 {
			m.isFirstWrite = false
			end := min(offset+maxTLSPacketLength-len(m.obfTag), len(encrypted))
			payload = append(m.obfTag, encrypted[offset:end]...)
			offset = end
		} else {
			end := min(offset+maxTLSPacketLength, len(encrypted))
			payload = encrypted[offset:end]
			offset = end
		}

		binary.BigEndian.PutUint16(header[3:], uint16(len(payload)))
		result.Write(header)
		result.Write(payload)
	}

	_, err := m.conn.Write(result.Bytes())
	return len(b), err
}

func (m *mtproxyConn) Read(b []byte) (int, error) {
	if m.readBuffer.Len() > 0 {
		return m.readBuffer.Read(b)
	}

	if !m.useFakeTls {
		n, err := m.conn.Read(b)
		if err != nil {
			return 0, err
		}
		m.decryptor.XORKeyStream(b[:n], b[:n])
		return n, nil
	}

	for {
		if m.tlsBuf.Len() >= 5 {
			header := m.tlsBuf.Bytes()[:5]
			if header[0] != 0x17 || header[1] != 0x03 || header[2] != 0x03 {
				return 0, fmt.Errorf("invalid TLS header: %x", header[:3])
			}
			length := int(binary.BigEndian.Uint16(header[3:5]))
			if m.tlsBuf.Len() >= 5+length {
				m.tlsBuf.Next(5)
				payload := make([]byte, length)
				m.tlsBuf.Read(payload)
				m.decryptor.XORKeyStream(payload, payload)
				m.readBuffer.Write(payload)
				if m.readBuffer.Len() > 0 {
					return m.readBuffer.Read(b)
				}
				continue
			}
		}
		tmp := make([]byte, 16384)
		n, err := m.conn.Read(tmp)
		if err != nil {
			return 0, err
		}
		m.tlsBuf.Write(tmp[:n])
	}
}

func (m *mtproxyConn) Close() error                       { return m.conn.Close() }
func (m *mtproxyConn) LocalAddr() net.Addr                { return m.conn.LocalAddr() }
func (m *mtproxyConn) RemoteAddr() net.Addr               { return m.conn.RemoteAddr() }
func (m *mtproxyConn) SetDeadline(t time.Time) error      { return m.conn.SetDeadline(t) }
func (m *mtproxyConn) SetReadDeadline(t time.Time) error  { return m.conn.SetReadDeadline(t) }
func (m *mtproxyConn) SetWriteDeadline(t time.Time) error { return m.conn.SetWriteDeadline(t) }

// Fake TLS ClientHello generation
var (
	keyMod     = new(big.Int).Sub(new(big.Int).Exp(big.NewInt(2), big.NewInt(255), nil), big.NewInt(19))
	quadResPow = new(big.Int).Div(new(big.Int).Sub(keyMod, big.NewInt(1)), big.NewInt(4))
)

type tlsHelloWriter struct {
	buf    []byte
	pos    int
	domain []byte
	grease []byte
	scopes []int
}

func generateFakeTlsHello(domain, secret []byte) []byte {
	w := &tlsHelloWriter{buf: make([]byte, 517), domain: domain, grease: initGrease(7)}

	w.writeBytes([]byte{0x16, 0x03, 0x01, 0x02, 0x00, 0x01, 0x00, 0x01, 0xFC, 0x03, 0x03})
	w.writeZero(32)
	w.writeBytes([]byte{0x20})
	w.writeRandom(32)

	w.writeBytes([]byte{0x00, 0x20})
	w.writeGrease(0)
	w.writeBytes([]byte{0x13, 0x01, 0x13, 0x02, 0x13, 0x03, 0xC0, 0x2B, 0xC0, 0x2F, 0xC0, 0x2C, 0xC0, 0x30, 0xCC, 0xA9, 0xCC, 0xA8, 0xC0, 0x13, 0xC0, 0x14, 0x00, 0x9C, 0x00, 0x9D, 0x00, 0x2F, 0x00, 0x35, 0x01, 0x00, 0x01, 0x93})
	w.writeGrease(2)
	w.writeBytes([]byte{0x00, 0x00, 0x00, 0x00})

	w.beginScope()
	w.beginScope()
	w.writeBytes([]byte{0x00})
	w.beginScope()
	w.writeBytes(domain)
	w.endScope()
	w.endScope()
	w.endScope()

	w.writeBytes([]byte{0x00, 0x17, 0x00, 0x00, 0xFF, 0x01, 0x00, 0x01, 0x00, 0x00, 0x0A, 0x00, 0x0A, 0x00, 0x08})
	w.writeGrease(4)
	w.writeBytes([]byte{0x00, 0x1D, 0x00, 0x17, 0x00, 0x18, 0x00, 0x0B, 0x00, 0x02, 0x01, 0x00, 0x00, 0x23, 0x00, 0x00, 0x00, 0x10, 0x00, 0x0E, 0x00, 0x0C, 0x02, 0x68, 0x32, 0x08, 0x68, 0x74, 0x74, 0x70, 0x2F, 0x31, 0x2E, 0x31, 0x00, 0x05, 0x00, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0x00, 0x12, 0x00, 0x10, 0x04, 0x03, 0x08, 0x04, 0x04, 0x01, 0x05, 0x03, 0x08, 0x05, 0x05, 0x01, 0x08, 0x06, 0x06, 0x01, 0x00, 0x12, 0x00, 0x00, 0x00, 0x33, 0x00, 0x2B, 0x00, 0x29})
	w.writeGrease(4)
	w.writeBytes([]byte{0x00, 0x01, 0x00, 0x00, 0x1D, 0x00, 0x20})
	w.writeKey()
	w.writeBytes([]byte{0x00, 0x2D, 0x00, 0x02, 0x01, 0x01, 0x00, 0x2B, 0x00, 0x0B, 0x0A})
	w.writeGrease(6)
	w.writeBytes([]byte{0x03, 0x04, 0x03, 0x03, 0x03, 0x02, 0x03, 0x01, 0x00, 0x1B, 0x00, 0x03, 0x02, 0x00, 0x02})
	w.writeGrease(3)
	w.writeBytes([]byte{0x00, 0x01, 0x00, 0x00, 0x15})

	padSize := 515 - w.pos
	w.beginScope()
	w.writeZero(padSize)
	w.endScope()

	mac := hmac.New(sha256.New, secret)
	mac.Write(w.buf)
	hash := mac.Sum(nil)
	ts := uint32(time.Now().Unix())
	binary.LittleEndian.PutUint32(hash[28:], binary.LittleEndian.Uint32(hash[28:])^ts)
	copy(w.buf[11:], hash)

	return w.buf
}

func initGrease(size int) []byte {
	buf := make([]byte, size)
	rand.Read(buf)
	for i := range buf {
		buf[i] = (buf[i] & 0xF0) + 0x0A
	}
	for i := 1; i < size; i += 2 {
		if buf[i] == buf[i-1] {
			buf[i] ^= 0x10
		}
	}
	return buf
}

func (w *tlsHelloWriter) writeBytes(data []byte) { copy(w.buf[w.pos:], data); w.pos += len(data) }
func (w *tlsHelloWriter) writeRandom(n int)      { rand.Read(w.buf[w.pos : w.pos+n]); w.pos += n }
func (w *tlsHelloWriter) writeZero(n int)        { w.pos += n }
func (w *tlsHelloWriter) writeGrease(seed int) {
	w.buf[w.pos], w.buf[w.pos+1] = w.grease[seed], w.grease[seed]
	w.pos += 2
}
func (w *tlsHelloWriter) beginScope() { w.scopes = append(w.scopes, w.pos); w.pos += 2 }
func (w *tlsHelloWriter) endScope() {
	begin := w.scopes[len(w.scopes)-1]
	w.scopes = w.scopes[:len(w.scopes)-1]
	binary.BigEndian.PutUint16(w.buf[begin:], uint16(w.pos-begin-2))
}

func (w *tlsHelloWriter) writeKey() {
	for {
		key := make([]byte, 32)
		rand.Read(key)
		key[31] &= 0x7F
		x := bytesToBigInt(key)
		y := getY2(x)
		if isQuadraticResidue(y) {
			for range 3 {
				x = getDoubleX(x)
			}
			w.writeBytes(bigIntToBytes(x, 32))
			return
		}
	}
}

func getY2(x *big.Int) *big.Int {
	y := new(big.Int).Set(x)
	y.Add(y, big.NewInt(486662)).Mod(y, keyMod)
	y.Mul(y, x).Mod(y, keyMod)
	y.Add(y, big.NewInt(1)).Mod(y, keyMod)
	y.Mul(y, x).Mod(y, keyMod)
	return y
}

func getDoubleX(x *big.Int) *big.Int {
	denom := new(big.Int).Mul(getY2(x), big.NewInt(4))
	denom.Mod(denom, keyMod)
	numer := new(big.Int).Mul(x, x)
	numer.Sub(numer, big.NewInt(1)).Mod(numer, keyMod)
	numer.Mul(numer, numer).Mod(numer, keyMod)
	denom.ModInverse(denom, keyMod)
	numer.Mul(numer, denom).Mod(numer, keyMod)
	return numer
}

func isQuadraticResidue(a *big.Int) bool {
	return new(big.Int).Exp(a, quadResPow, keyMod).Cmp(big.NewInt(1)) == 0
}

func bigIntToBytes(n *big.Int, length int) []byte {
	b := n.Bytes()
	result := make([]byte, length)
	for i := 0; i < len(b) && i < length; i++ {
		result[i] = b[len(b)-1-i]
	}
	return result
}

func bytesToBigInt(b []byte) *big.Int {
	rev := make([]byte, len(b))
	for i := range b {
		rev[len(b)-1-i] = b[i]
	}
	return new(big.Int).SetBytes(rev)
}

func ParseMTProxyURL(urlStr string) (*utils.Proxy, error) {
	urlStr = strings.TrimPrefix(strings.TrimPrefix(urlStr, "tg://proxy?"), "https://t.me/proxy?")
	params := make(map[string]string)
	for part := range strings.SplitSeq(urlStr, "&") {
		if kv := strings.SplitN(part, "=", 2); len(kv) == 2 {
			params[kv[0]] = kv[1]
		}
	}
	if params["server"] == "" || params["port"] == "" || params["secret"] == "" {
		return nil, errors.New("invalid mtproxy URL")
	}
	var port int
	fmt.Sscanf(params["port"], "%d", &port)
	return &utils.Proxy{Type: "mtproxy", Host: params["server"], Port: port, Secret: params["secret"]}, nil
}
