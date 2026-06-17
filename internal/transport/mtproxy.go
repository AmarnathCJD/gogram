// Copyright (c) 2025 @AmarnathCJD

package transport

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/amarnathcjd/gogram/internal/utils"
)

const (
	maxTLSPacketLength = 2878
	maxFakeTlsRecord   = 32 * 1024
	mtproxyReadBufSize = 16 * 1024
)

var obfuscationForbiddenWords = []uint32{
	0x44414548, 0x54534f50, 0x20544547, 0x4954504f,
	0x02010316, 0xdddddddd, 0xeeeeeeee, 0xefefefef,
}

var mtproxyReadPool = sync.Pool{
	New: func() any {
		b := make([]byte, mtproxyReadBufSize)
		return &b
	},
}

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
			if logger != nil {
				logger.Debug("[mtproxy] fakeTLS handshake failed: %v", err)
			}
			return nil, fmt.Errorf("TLS handshake failed: %w", err)
		}
		if logger != nil {
			logger.Debug("[mtproxy] fakeTLS handshake OK")
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
	var random []byte
	for {
		random = make([]byte, 64)
		if _, err := rand.Read(random); err != nil {
			return nil, fmt.Errorf("rand: %w", err)
		}
		if random[0] == 0xef || slices.Contains(obfuscationForbiddenWords, binary.LittleEndian.Uint32(random[:4])) || binary.LittleEndian.Uint32(random[4:8]) == 0 {
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
	clientRandom := make([]byte, 32)
	copy(clientRandom, hello[11:43])

	if _, err := m.conn.Write(hello); err != nil {
		return err
	}

	part1Prefix := []byte{0x16, 0x03, 0x03}
	part3Prefix := []byte{0x14, 0x03, 0x03, 0x00, 0x01, 0x01, 0x17, 0x03, 0x03}

	parts12 := make([]byte, len(part1Prefix)+2)
	if _, err := io.ReadFull(m.conn, parts12); err != nil {
		return fmt.Errorf("read server hello part1: %w", err)
	}
	if !bytes.Equal(parts12[:len(part1Prefix)], part1Prefix) {
		return fmt.Errorf("invalid server hello part1: %x", parts12[:len(part1Prefix)])
	}
	part2Size := int(binary.BigEndian.Uint16(parts12[len(part1Prefix):]))
	part2 := make([]byte, part2Size)
	if _, err := io.ReadFull(m.conn, part2); err != nil {
		return fmt.Errorf("read server hello part2: %w", err)
	}

	parts34 := make([]byte, len(part3Prefix)+2)
	if _, err := io.ReadFull(m.conn, parts34); err != nil {
		return fmt.Errorf("read server hello part3: %w", err)
	}
	if !bytes.Equal(parts34[:len(part3Prefix)], part3Prefix) {
		return fmt.Errorf("invalid server hello part3: %x", parts34[:len(part3Prefix)])
	}
	part4Size := int(binary.BigEndian.Uint16(parts34[len(part3Prefix):]))
	part4 := make([]byte, part4Size)
	if _, err := io.ReadFull(m.conn, part4); err != nil {
		return fmt.Errorf("read server hello part4: %w", err)
	}

	serverHello := make([]byte, 0, len(parts12)+len(part2)+len(parts34)+len(part4))
	serverHello = append(serverHello, parts12...)
	serverHello = append(serverHello, part2...)
	serverHello = append(serverHello, parts34...)
	serverHello = append(serverHello, part4...)

	const serverDigestOffset = 11
	if len(serverHello) < serverDigestOffset+helloDigestLen {
		return fmt.Errorf("server hello too short: %d", len(serverHello))
	}
	serverDigest := make([]byte, helloDigestLen)
	copy(serverDigest, serverHello[serverDigestOffset:serverDigestOffset+helloDigestLen])
	for i := range helloDigestLen {
		serverHello[serverDigestOffset+i] = 0
	}

	fullData := make([]byte, 0, helloDigestLen+len(serverHello))
	fullData = append(fullData, clientRandom...)
	fullData = append(fullData, serverHello...)

	mac := hmac.New(sha256.New, m.config.Secret)
	mac.Write(fullData)
	if !hmac.Equal(mac.Sum(nil), serverDigest) {
		return errors.New("server response hash mismatch")
	}

	_, err := m.conn.Write([]byte{0x14, 0x03, 0x03, 0x00, 0x01, 0x01})
	return err
}

func (m *mtproxyConn) Write(b []byte) (int, error) {
	encrypted := make([]byte, len(b))
	m.encryptor.XORKeyStream(encrypted, b)

	if !m.useFakeTls {
		n, err := m.conn.Write(encrypted)
		if err != nil {
			return n, err
		}
		return len(b), nil
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

	if _, err := m.conn.Write(result.Bytes()); err != nil {
		return 0, err
	}
	return len(b), nil
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
			if length > maxFakeTlsRecord {
				return 0, fmt.Errorf("oversized TLS record: %d", length)
			}
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
		tmpPtr := mtproxyReadPool.Get().(*[]byte)
		tmp := *tmpPtr
		n, err := m.conn.Read(tmp)
		if n > 0 {
			m.tlsBuf.Write(tmp[:n])
		}
		mtproxyReadPool.Put(tmpPtr)
		if err != nil {
			return 0, err
		}
	}
}

func (m *mtproxyConn) IsFakeTLS() bool { return m.useFakeTls }

func (m *mtproxyConn) Close() error                       { return m.conn.Close() }
func (m *mtproxyConn) LocalAddr() net.Addr                { return m.conn.LocalAddr() }
func (m *mtproxyConn) RemoteAddr() net.Addr               { return m.conn.RemoteAddr() }
func (m *mtproxyConn) SetDeadline(t time.Time) error      { return m.conn.SetDeadline(t) }
func (m *mtproxyConn) SetReadDeadline(t time.Time) error  { return m.conn.SetReadDeadline(t) }
func (m *mtproxyConn) SetWriteDeadline(t time.Time) error { return m.conn.SetWriteDeadline(t) }

const (
	helloDigestLen = 32
	kMaxGrease     = 8
	helloMinSize   = 513
)

type helloWriter struct {
	buf      []byte
	domain   []byte
	grease   []byte
	scopes   []int
	digestAt int
}

func newHelloWriter(domain []byte) *helloWriter {
	return &helloWriter{
		buf:      make([]byte, 0, 2048),
		domain:   domain,
		grease:   prepareGreases(),
		digestAt: -1,
	}
}

func prepareGreases() []byte {
	buf := make([]byte, kMaxGrease)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	for i := range buf {
		buf[i] = (buf[i] & 0xF0) | 0x0A
	}
	for i := 0; i+1 < kMaxGrease; i += 2 {
		if buf[i] == buf[i+1] {
			buf[i+1] ^= 0x10
		}
	}
	return buf
}

func (w *helloWriter) bytes(data []byte) { w.buf = append(w.buf, data...) }
func (w *helloWriter) byte1(b byte)      { w.buf = append(w.buf, b) }
func (w *helloWriter) random(n int) {
	off := len(w.buf)
	w.buf = append(w.buf, make([]byte, n)...)
	if _, err := rand.Read(w.buf[off:]); err != nil {
		panic(err)
	}
}
func (w *helloWriter) zero(n int) {
	if n == helloDigestLen && w.digestAt < 0 {
		w.digestAt = len(w.buf)
	}
	w.buf = append(w.buf, make([]byte, n)...)
}
func (w *helloWriter) grease1(seed int) {
	w.buf = append(w.buf, w.grease[seed], w.grease[seed])
}
func (w *helloWriter) openScope() {
	w.scopes = append(w.scopes, len(w.buf))
	w.buf = append(w.buf, 0, 0)
}
func (w *helloWriter) closeScope() {
	at := w.scopes[len(w.scopes)-1]
	w.scopes = w.scopes[:len(w.scopes)-1]
	binary.BigEndian.PutUint16(w.buf[at:], uint16(len(w.buf)-at-2))
}
func (w *helloWriter) domainSNI() { w.buf = append(w.buf, w.domain...) }

func (w *helloWriter) pubKey() {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	w.buf = append(w.buf, pub...)
}

func (w *helloWriter) mlkemHybrid() {
	const elements = 384
	const tail = 32
	totalOut := elements*3 + tail
	off := len(w.buf)
	w.buf = append(w.buf, make([]byte, totalOut)...)
	source := make([]byte, elements*8+tail)
	if _, err := rand.Read(source); err != nil {
		panic(err)
	}
	out := w.buf[off : off+totalOut]
	for i := 0; i < elements; i++ {
		a := int(binary.LittleEndian.Uint32(source[i*8:])) & 0x7fffffff % 3329
		b := int(binary.LittleEndian.Uint32(source[i*8+4:])) & 0x7fffffff % 3329
		out[i*3] = byte(a)
		out[i*3+1] = byte((a >> 8) | ((b & 0xf) << 4))
		out[i*3+2] = byte(b >> 4)
	}
	copy(out[elements*3:], source[elements*8:])
}

func (w *helloWriter) echRandomLen() {
	choices := [4]int{144, 176, 208, 240}
	var b [1]byte
	rand.Read(b[:])
	w.random(choices[int(b[0])%4])
}

func (w *helloWriter) padExt() {
	cur := len(w.buf)
	if cur >= helloMinSize {
		return
	}
	w.bytes([]byte{0x00, 0x15})
	w.openScope()
	w.zero(helloMinSize - cur)
	w.closeScope()
}

func (w *helloWriter) shufflePermutation(parts [][]byte) {
	for i := len(parts) - 1; i > 0; i-- {
		var b [4]byte
		rand.Read(b[:])
		j := int(binary.LittleEndian.Uint32(b[:])) & 0x7fffffff % (i + 1)
		parts[i], parts[j] = parts[j], parts[i]
	}
	for _, p := range parts {
		w.bytes(p)
	}
}

func generateFakeTlsHello(domain, secret []byte) []byte {
	w := newHelloWriter(domain)

	w.bytes([]byte{0x16, 0x03, 0x01})
	w.openScope()
	w.bytes([]byte{0x01, 0x00})
	w.openScope()
	w.bytes([]byte{0x03, 0x03})
	w.zero(helloDigestLen)
	w.byte1(0x20)
	w.random(32)
	w.bytes([]byte{0x00, 0x20})
	w.grease1(0)
	w.bytes([]byte{
		0x13, 0x01, 0x13, 0x02, 0x13, 0x03, 0xc0, 0x2b,
		0xc0, 0x2f, 0xc0, 0x2c, 0xc0, 0x30, 0xcc, 0xa9,
		0xcc, 0xa8, 0xc0, 0x13, 0xc0, 0x14, 0x00, 0x9c,
		0x00, 0x9d, 0x00, 0x2f, 0x00, 0x35, 0x01, 0x00,
	})
	w.openScope()
	w.grease1(2)
	w.bytes([]byte{0x00, 0x00})

	parts := w.buildPermutation()
	w.shufflePermutation(parts)

	w.grease1(3)
	w.bytes([]byte{0x00, 0x01, 0x00})
	w.padExt()
	w.closeScope()
	w.closeScope()
	w.closeScope()

	if w.digestAt < 0 {
		return w.buf
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(w.buf)
	digest := mac.Sum(nil)
	ts := uint32(time.Now().Unix())
	binary.LittleEndian.PutUint32(digest[28:], binary.LittleEndian.Uint32(digest[28:])^ts)
	copy(w.buf[w.digestAt:], digest)

	return w.buf
}

func (w *helloWriter) buildPermutation() [][]byte {
	builders := []func(*helloWriter){
		// SNI server_name
		func(p *helloWriter) {
			p.bytes([]byte{0x00, 0x00})
			p.openScope()
			p.openScope()
			p.byte1(0x00)
			p.openScope()
			p.domainSNI()
			p.closeScope()
			p.closeScope()
			p.closeScope()
		},
		// status_request
		func(p *helloWriter) {
			p.bytes([]byte{0x00, 0x05, 0x00, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00})
		},
		// supported_groups
		func(p *helloWriter) {
			p.bytes([]byte{0x00, 0x0a, 0x00, 0x0c, 0x00, 0x0a})
			p.grease1(4)
			p.bytes([]byte{0x11, 0xec, 0x00, 0x1d, 0x00, 0x17, 0x00, 0x18})
		},
		// ec_point_formats
		func(p *helloWriter) {
			p.bytes([]byte{0x00, 0x0b, 0x00, 0x02, 0x01, 0x00})
		},
		// signature_algorithms
		func(p *helloWriter) {
			p.bytes([]byte{
				0x00, 0x0d, 0x00, 0x12, 0x00, 0x10, 0x04, 0x03,
				0x08, 0x04, 0x04, 0x01, 0x05, 0x03, 0x08, 0x05,
				0x05, 0x01, 0x08, 0x06, 0x06, 0x01,
			})
		},
		// ALPN h2 / http1.1
		func(p *helloWriter) {
			p.bytes([]byte{
				0x00, 0x10, 0x00, 0x0e, 0x00, 0x0c, 0x02, 0x68,
				0x32, 0x08, 0x68, 0x74, 0x74, 0x70, 0x2f, 0x31,
				0x2e, 0x31,
			})
		},
		// signed_certificate_timestamp
		func(p *helloWriter) {
			p.bytes([]byte{0x00, 0x12, 0x00, 0x00})
		},
		// extended_master_secret
		func(p *helloWriter) {
			p.bytes([]byte{0x00, 0x17, 0x00, 0x00})
		},
		// compress_certificate (brotli)
		func(p *helloWriter) {
			p.bytes([]byte{0x00, 0x1b, 0x00, 0x03, 0x02, 0x00, 0x02})
		},
		// session_ticket
		func(p *helloWriter) {
			p.bytes([]byte{0x00, 0x23, 0x00, 0x00})
		},
		// supported_versions TLS1.3 + TLS1.2
		func(p *helloWriter) {
			p.bytes([]byte{0x00, 0x2b, 0x00, 0x07, 0x06})
			p.grease1(6)
			p.bytes([]byte{0x03, 0x04, 0x03, 0x03})
		},
		// psk_key_exchange_modes
		func(p *helloWriter) {
			p.bytes([]byte{0x00, 0x2d, 0x00, 0x02, 0x01, 0x01})
		},
		// key_share (mlkem + x25519)
		func(p *helloWriter) {
			p.bytes([]byte{0x00, 0x33, 0x04, 0xef, 0x04, 0xed})
			p.grease1(4)
			p.bytes([]byte{0x00, 0x01, 0x00, 0x11, 0xec, 0x04, 0xc0})
			p.mlkemHybrid()
			p.pubKey()
			p.bytes([]byte{0x00, 0x1d, 0x00, 0x20})
			p.pubKey()
		},
		// application_settings
		func(p *helloWriter) {
			p.bytes([]byte{0x44, 0xcd, 0x00, 0x05, 0x00, 0x03, 0x02, 0x68, 0x32})
		},
		// ECH GREASE
		func(p *helloWriter) {
			p.bytes([]byte{0xfe, 0x0d})
			p.openScope()
			p.bytes([]byte{0x00, 0x00, 0x01, 0x00, 0x01})
			p.random(1)
			p.bytes([]byte{0x00, 0x20})
			p.random(32)
			p.openScope()
			p.echRandomLen()
			p.closeScope()
			p.closeScope()
		},
		// renegotiation_info
		func(p *helloWriter) {
			p.bytes([]byte{0xff, 0x01, 0x00, 0x01, 0x00})
		},
	}

	out := make([][]byte, len(builders))
	for i, fn := range builders {
		sub := newHelloWriter(w.domain)
		sub.grease = w.grease
		fn(sub)
		out[i] = sub.buf
	}
	return out
}

func ParseMTProxyURL(urlStr string) (*utils.Proxy, error) {
	urlStr = strings.TrimPrefix(strings.TrimPrefix(urlStr, "tg://proxy?"), "https://t.me/proxy?")
	params, err := url.ParseQuery(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid mtproxy URL: %w", err)
	}
	server := params.Get("server")
	portStr := params.Get("port")
	secret := params.Get("secret")
	if server == "" || portStr == "" || secret == "" {
		return nil, errors.New("invalid mtproxy URL: missing server/port/secret")
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid mtproxy URL: bad port %q", portStr)
	}
	return &utils.Proxy{Type: "mtproxy", Host: server, Port: port, Secret: secret}, nil
}
