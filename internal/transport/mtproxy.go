// Copyright (c) 2025 RoseLoverX

package transport

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"strings"
	"time"

	"errors"

	"github.com/amarnathcjd/gogram/internal/utils"
)

const (
	_MAX_GREASE        = 8
	_MAX_TLS_MSG       = 16 * 1024 // 16 KB
	fakeTLSHandshakeID = 0xee
)

// TLS Hello operation types
type tlsOp struct {
	opType string
	value  any
}

var _TLS_HELLO_OPS = []tlsOp{
	{"string", []byte("\x16\x03\x01\x02\x00\x01\x00\x01\xfc\x03\x03")},
	{"zero", 32},
	{"string", []byte("\x20")},
	{"id", nil},
	{"string", []byte("\x00\x20")},
	{"grease", 0},
	{"string", []byte("\x13\x01\x13\x02\x13\x03\xc0\x2b\xc0\x2f\xc0\x2c\xc0\x30\xcc\xa9\xcc\xa8\xc0\x13\xc0\x14\x00\x9c\x00\x9d\x00\x2f\x00\x35\x01\x00\x01\x93")},
	{"grease", 2},
	{"string", []byte("\x00\x00")},
	{"permutation", [][]tlsOp{
		{
			{"string", []byte("\x00\x00")},
			{"begin_scope", nil},
			{"begin_scope", nil},
			{"string", []byte("\x00")},
			{"begin_scope", nil},
			{"domain", nil},
			{"end_scope", nil},
			{"end_scope", nil},
			{"end_scope", nil},
		},
		{
			{"string", []byte("\x00\x05\x00\x05\x01\x00\x00\x00\x00")},
		},
		{
			{"string", []byte("\x00\x0a\x00\x0a\x00\x08")},
			{"grease", 4},
			{"string", []byte("\x00\x1d\x00\x17\x00\x18")},
		},
		{
			{"string", []byte("\x00\x0b\x00\x02\x01\x00")},
		},
		{
			{"string", []byte("\x00\x0d\x00\x12\x00\x10\x04\x03\x08\x04\x04\x01\x05\x03\x08\x05\x05\x01\x08\x06\x06\x01")},
		},
		{
			{"string", []byte("\x00\x10\x00\x0e\x00\x0c\x02\x68\x32\x08\x68\x74\x74\x70\x2f\x31\x2e\x31")},
		},
		{
			{"string", []byte("\x00\x12\x00\x00")},
		},
		{
			{"string", []byte("\x00\x17\x00\x00")},
		},
		{
			{"string", []byte("\x00\x1b\x00\x03\x02\x00\x02")},
		},
		{
			{"string", []byte("\x00\x23\x00\x00")},
		},
		{
			{"string", []byte("\x00\x2b\x00\x07\x06")},
			{"grease", 6},
			{"string", []byte("\x03\x04\x03\x03")},
		},
		{
			{"string", []byte("\x00\x2d\x00\x02\x01\x01")},
		},
		{
			{"string", []byte("\x00\x33\x00\x2b\x00\x29")},
			{"grease", 4},
			{"string", []byte("\x00\x01\x00\x00\x1d\x00\x20")},
			{"k", nil},
		},
		{
			{"string", []byte("\x44\x69\x00\x05\x00\x03\x02\x68\x32")},
		},
		{
			{"string", []byte("\xff\x01\x00\x01\x00")},
		},
	}},
	{"grease", 3},
	{"string", []byte("\x00\x01\x00\x00\x15")},
}

type MTProxyConfig struct {
	ProxyURL    *url.URL
	TargetAddr  string
	DCID        int16
	ModeVariant uint8
	LocalAddr   string
}

type fakeTLSConn struct {
	conn        net.Conn
	reader      io.Reader
	buffer      []byte
	firstPacket bool
}

var _ net.Conn = (*fakeTLSConn)(nil)

func DecodeMTProtoProxySecret(value string) (proto byte, secret []byte, serverHostname []byte, err error) {
	var data []byte

	if len(value)%2 == 0 {
		data, err = hexDecode(value)
		if err != nil {
			data = nil
		}
	}

	if data == nil {
		padding := len(value) % 4
		if padding > 0 {
			value += strings.Repeat("=", 4-padding)
		}

		data, err = base64.URLEncoding.DecodeString(value)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("invalid mtproxy secret: %w", err)
		}
	}

	proto = 0
	secret = data
	serverHostname = nil

	if len(data) >= 17 {
		proto = data[0]
		secret = data[1:17]
		if proto == fakeTLSHandshakeID {
			hostnameBytes := trimNullBytes(data[17:])
			if len(hostnameBytes) > 0 {
				serverHostname = make([]byte, len(hostnameBytes))
				copy(serverHostname, hostnameBytes)
			}
		}
	}
	return proto, secret, serverHostname, nil
}

func hexDecode(s string) ([]byte, error) {
	result := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		var b byte
		_, err := fmt.Sscanf(s[i:i+2], "%02x", &b)
		if err != nil {
			return nil, err
		}
		result[i/2] = b
	}
	return result, nil
}

func trimNullBytes(b []byte) []byte {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] != 0 {
			return b[:i+1]
		}
	}
	return b[:0]
}

func getY2(x *big.Int) *big.Int {
	mod := new(big.Int)
	mod.SetString("7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFED", 16)

	// y^2 = x^3 + 486662*x^2 + x (mod p)
	x2 := new(big.Int).Mul(x, x)
	x2.Mod(x2, mod)

	x3 := new(big.Int).Mul(x2, x)
	x3.Mod(x3, mod)

	coeff := big.NewInt(486662)
	term := new(big.Int).Mul(coeff, x2)
	term.Mod(term, mod)

	result := new(big.Int).Add(x3, term)
	result.Add(result, x)
	result.Mod(result, mod)

	return result
}

func getDoubleX(x *big.Int) *big.Int {
	mod := new(big.Int)
	mod.SetString("7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFED", 16)

	y2 := getY2(x)

	// numer = ((x*x - 1) % mod)^2 % mod
	x2 := new(big.Int).Mul(x, x)
	x2.Mod(x2, mod)

	numer := new(big.Int).Sub(x2, big.NewInt(1))
	numer.Mod(numer, mod)
	numer.Mul(numer, numer)
	numer.Mod(numer, mod)

	// denom = (4 * y2) % mod
	denom := new(big.Int).Mul(big.NewInt(4), y2)
	denom.Mod(denom, mod)

	// denom^-1
	denomInv := new(big.Int).ModInverse(denom, mod)

	// result = (numer * denom^-1) % mod
	result := new(big.Int).Mul(numer, denomInv)
	result.Mod(result, mod)

	return result
}

func generatePublicKey() ([]byte, error) {
	mod := new(big.Int)
	mod.SetString("7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFED", 16)

	pow := new(big.Int)
	pow.SetString("3FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF6", 16)

	var x *big.Int
	for {
		keyBytes := make([]byte, 32)
		if _, err := rand.Read(keyBytes); err != nil {
			return nil, fmt.Errorf("generating random bytes: %w", err)
		}

		keyBytes[31] &= 0x7F
		x = new(big.Int).SetBytes(keyBytes)

		// x = (x * x) % mod
		x.Mul(x, x)
		x.Mod(x, mod)

		y2 := getY2(x)

		// Check if y2^pow % mod == 1
		check := new(big.Int).Exp(y2, pow, mod)
		if check.Cmp(big.NewInt(1)) == 0 {
			break
		}
	}

	for i := 0; i < 3; i++ {
		x = getDoubleX(x)
	}

	keyBytes := x.Bytes()

	if len(keyBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(keyBytes):], keyBytes)
		keyBytes = padded
	}

	result := make([]byte, 32)
	for i := 0; i < 32; i++ {
		result[i] = keyBytes[31-i]
	}

	return result, nil
}

func startFakeTLS(conn net.Conn, key []byte, serverHostname []byte) (*fakeTLSConn, error) {
	sessionID := make([]byte, 32)
	if _, err := rand.Read(sessionID); err != nil {
		return nil, fmt.Errorf("generating session ID: %w", err)
	}

	grease := make([]byte, _MAX_GREASE)
	if _, err := rand.Read(grease); err != nil {
		return nil, fmt.Errorf("generating grease for faketls: %w", err)
	}

	for i := range grease {
		g := (grease[i] & 0xF0) + 0x0A
		if i%2 == 1 && g == grease[i-1] {
			g ^= 0x10
		}
		grease[i] = g
	}

	publicKey, err := generatePublicKey()
	if err != nil {
		return nil, fmt.Errorf("generating public key: %w", err)
	}

	buffer := make([]byte, 0, 1024)
	stack := make([]int, 0)

	var writeOps func(ops []tlsOp)
	writeOps = func(ops []tlsOp) {
		for _, op := range ops {
			switch op.opType {
			case "k":
				buffer = append(buffer, publicKey...)

			case "id":
				buffer = append(buffer, sessionID...)

			case "zero":
				count := op.value.(int)
				buffer = append(buffer, make([]byte, count)...)

			case "string":
				buffer = append(buffer, op.value.([]byte)...)

			case "grease":
				idx := op.value.(int)
				g := grease[idx]
				buffer = append(buffer, g, g)

			case "domain":
				sni := serverHostname
				if len(sni) > 253 {
					sni = sni[:253]
				}
				buffer = append(buffer, sni...)

			case "begin_scope":
				stack = append(stack, len(buffer))
				buffer = append(buffer, 0x00, 0x00) // placeholder

			case "end_scope":
				if len(stack) == 0 {
					continue
				}
				start := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				length := len(buffer) - start - 2
				buffer[start] = byte((length >> 8) & 0xFF)
				buffer[start+1] = byte(length & 0xFF)

			case "permutation":
				parts := make([][]tlsOp, len(op.value.([][]tlsOp)))
				for i, v := range op.value.([][]tlsOp) {
					parts[i] = make([]tlsOp, len(v))
					copy(parts[i], v)
				}
				for i := len(parts) - 1; i > 0; i-- {
					jBig, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
					if err != nil {
						break
					}
					j := int(jBig.Int64())
					parts[i], parts[j] = parts[j], parts[i]
				}
				for _, part := range parts {
					writeOps(part)
				}
			}
		}
	}

	writeOps(_TLS_HELLO_OPS)

	if len(buffer) > 515 {
		return nil, errors.New("handshake buffer too large for padding")
	}

	padSize := 515 - len(buffer)
	buffer = append(buffer, byte(padSize>>8), byte(padSize&0xFF))
	buffer = append(buffer, make([]byte, padSize)...)

	mac := hmac.New(sha256.New, key)
	mac.Write(buffer)
	digest := mac.Sum(nil)

	oldValue := binary.LittleEndian.Uint32(digest[28:32])
	newValue := oldValue ^ uint32(time.Now().Unix())
	binary.LittleEndian.PutUint32(digest[28:32], newValue)

	clientRandom := digest[:32]
	copy(buffer[11:11+32], clientRandom)

	_, err = conn.Write(buffer)
	if err != nil {
		return nil, fmt.Errorf("sending client hello: %w", err)
	}

	header, payload, err := readTLSRecord(conn)
	if err != nil {
		return nil, fmt.Errorf("reading server hello: %w", err)
	}

	if len(payload) < 38 {
		return nil, errors.New("server hello too short")
	}

	if payload[0] != 0x02 {
		return nil, fmt.Errorf("unexpected handshake type: %#x", payload[0])
	}

	length := int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
	if length+4 != len(payload) {
		return nil, errors.New("handshake length mismatch")
	}

	version := uint16(payload[4])<<8 | uint16(payload[5])
	if version != 0x0303 {
		return nil, fmt.Errorf("unexpected TLS version: %#x", version)
	}

	serverDigest := make([]byte, 32)
	copy(serverDigest, payload[6:38])

	payloadZeroed := make([]byte, len(payload))
	copy(payloadZeroed, payload)
	for i := 6; i < 38; i++ {
		payloadZeroed[i] = 0
	}

	nextHeader, nextPayload, err := readTLSRecord(conn)
	if err != nil {
		return nil, fmt.Errorf("reading change cipher spec: %w", err)
	}

	if nextHeader[0] != 0x14 {
		return nil, fmt.Errorf("unexpected TLS record: %#x", nextHeader[0])
	}
	payloadZeroed = append(payloadZeroed, nextHeader...)
	payloadZeroed = append(payloadZeroed, nextPayload...)

	nextHeader, nextPayload, err = readTLSRecord(conn)
	if err != nil {
		return nil, fmt.Errorf("reading random data: %w", err)
	}
	if nextHeader[0] != 0x17 {
		return nil, fmt.Errorf("unexpected TLS record: %#x", nextHeader[0])
	}

	payloadZeroed = append(payloadZeroed, nextHeader...)
	payloadZeroed = append(payloadZeroed, nextPayload...)

	mac = hmac.New(sha256.New, key)
	mac.Write(clientRandom)
	mac.Write(header)
	mac.Write(payloadZeroed)
	computedDigest := mac.Sum(nil)

	if !hmac.Equal(serverDigest, computedDigest) {
		return nil, errors.New("TLS handshake verification failed: HMAC mismatch")
	}

	return &fakeTLSConn{
		conn:   conn,
		reader: conn,
		buffer: make([]byte, 0),
	}, nil
}

func readTLSRecord(conn net.Conn) (header []byte, payload []byte, err error) {
	header = make([]byte, 5)
	_, err = io.ReadFull(conn, header)
	if err != nil {
		return nil, nil, err
	}

	version := uint16(header[1])<<8 | uint16(header[2])
	if version != 0x0303 {
		return nil, nil, fmt.Errorf("unexpected TLS version: %#x", version)
	}

	length := int(header[3])<<8 | int(header[4])
	if length > 64*1024-5 {
		return nil, nil, fmt.Errorf("TLS record too large: %d", length)
	}

	payload = make([]byte, length)
	_, err = io.ReadFull(conn, payload)
	if err != nil {
		return nil, nil, err
	}

	return header, payload, nil
}

func (f *fakeTLSConn) Read(b []byte) (int, error) {
	if len(f.buffer) > 0 {
		n := copy(b, f.buffer)
		f.buffer = f.buffer[n:]
		return n, nil
	}

	_, payload, err := readTLSRecord(f.conn)
	if err != nil {
		return 0, err
	}

	n := copy(b, payload)
	if n < len(payload) {
		f.buffer = append(f.buffer, payload[n:]...)
	}

	return n, nil
}

func (f *fakeTLSConn) Write(b []byte) (int, error) {
	totalWritten := 0

	// Some MTProxy implementations expect a ChangeCipherSpec record before
	if !f.firstPacket {
		ccs := make([]byte, 5+1)
		ccs[0] = 0x14 // ChangeCipherSpec
		ccs[1] = 0x03 // TLS 1.2
		ccs[2] = 0x03
		binary.BigEndian.PutUint16(ccs[3:5], 1)
		ccs[5] = 0x01

		if _, err := f.conn.Write(ccs); err != nil {
			return 0, err
		}
		f.firstPacket = true
	}

	for offset := 0; offset < len(b); offset += _MAX_TLS_MSG {
		end := min(offset+_MAX_TLS_MSG, len(b))
		chunk := b[offset:end]

		record := make([]byte, 5+len(chunk))
		record[0] = 0x17 // application data
		record[1] = 0x03 // TLS 1.2
		record[2] = 0x03
		binary.BigEndian.PutUint16(record[3:5], uint16(len(chunk)))
		copy(record[5:], chunk)

		_, err := f.conn.Write(record)
		if err != nil {
			return totalWritten, err
		}

		totalWritten += len(chunk)
	}

	return totalWritten, nil
}

func (f *fakeTLSConn) Close() error {
	return f.conn.Close()
}

func (f *fakeTLSConn) LocalAddr() net.Addr {
	return f.conn.LocalAddr()
}

func (f *fakeTLSConn) RemoteAddr() net.Addr {
	return f.conn.RemoteAddr()
}

func (f *fakeTLSConn) SetDeadline(t time.Time) error {
	return f.conn.SetDeadline(t)
}

func (f *fakeTLSConn) SetReadDeadline(t time.Time) error {
	return f.conn.SetReadDeadline(t)
}

func (f *fakeTLSConn) SetWriteDeadline(t time.Time) error {
	return f.conn.SetWriteDeadline(t)
}

// mtproxyConn wraps an obfuscated connection for MTProxy
type mtproxyConn struct {
	reader  *Reader
	conn    io.ReadWriteCloser
	timeout time.Duration
}

func (m *mtproxyConn) Close() error {
	if m.reader != nil {
		m.reader.Close()
	}
	return m.conn.Close()
}

func (m *mtproxyConn) Write(b []byte) (int, error) {
	return m.conn.Write(b)
}

func (m *mtproxyConn) Read(b []byte) (int, error) {
	if m.timeout > 0 {
		if setter, ok := m.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
			err := setter.SetReadDeadline(time.Now().Add(m.timeout))
			if err != nil {
				return 0, fmt.Errorf("setting read deadline: %w", err)
			}
		}
	}

	n, err := m.reader.Read(b)

	if err != nil {
		if e, ok := err.(*net.OpError); ok || err == io.ErrClosedPipe {
			if e != nil && e.Err.Error() == "i/o timeout" || err == io.ErrClosedPipe {
				return 0, fmt.Errorf("required to reconnect: %w", err)
			}
		}
		switch err {
		case io.EOF, context.Canceled:
			return 0, err
		default:
			return 0, fmt.Errorf("unexpected error: %w", err)
		}
	}
	return n, nil
}

func DialMTProxy(proxy *utils.Proxy, targetAddr string, dcID int16, modeVariant uint8, localAddr string, logger *utils.Logger) (Conn, error) {
	secret := proxy.Secret
	if secret == "" {
		return nil, errors.New("mtproxy secret is required")
	}

	proto, secretBytes, serverHostname, err := DecodeMTProtoProxySecret(secret)
	if err != nil {
		return nil, fmt.Errorf("decoding MTProto proxy secret: %w", err)
	}

	if logger != nil {
		logger.WithFields(map[string]any{
			"proxy": proxy.GetHost(),
			"dc":    dcID,
		}).Debug("[mtproxy] connecting to proxy")
	}

	var dialer net.Dialer
	if localAddr != "" {
		addr, err := net.ResolveTCPAddr("tcp", localAddr)
		if err != nil {
			return nil, fmt.Errorf("resolving local address: %w", err)
		}
		dialer.LocalAddr = addr
	}

	proxyAddr := net.JoinHostPort(proxy.GetHost(), fmt.Sprintf("%d", proxy.GetPort()))
	rawConn, err := dialer.Dial("tcp", proxyAddr)
	if err != nil {
		if logger != nil {
			logger.WithError(err).WithField("proxy", proxyAddr).Error("[mtproxy] TCP connection failed")
		}
		return nil, fmt.Errorf("connecting to MTProxy: %w", err)
	}

	tcpConnection, ok := rawConn.(*net.TCPConn)
	if !ok {
		rawConn.Close()
		return nil, errors.New("expected TCP connection")
	}

	if logger != nil {
		logger.Debug("[mtproxy] TCP connection established")
	}

	var conn net.Conn = tcpConnection

	if proto == fakeTLSHandshakeID {
		if logger != nil {
			logger.WithField("sni", string(serverHostname)).Debug("[mtproxy] starting Fake TLS handshake")
		}
		ftlsConn, err := startFakeTLS(tcpConnection, secretBytes, serverHostname)
		if err != nil {
			tcpConnection.Close()
			if logger != nil {
				logger.WithError(err).Error("[mtproxy] fake TLS handshake failed")
			}
			return nil, fmt.Errorf("fake TLS handshake failed: %w", err)
		}
		conn = ftlsConn
		if logger != nil {
			logger.Debug("[mtproxy] fake TLS handshake completed")
		}
	}

	protocolID := ProtocolID(modeVariant)
	obfConn, err := NewObfuscatedConnWithSecret(conn, protocolID, secretBytes, dcID)
	if err != nil {
		conn.Close()
		if logger != nil {
			logger.WithError(err).Error("[mtproxy] obfuscation failed")
		}
		return nil, fmt.Errorf("creating obfuscated connection: %w", err)
	}

	if logger != nil {
		logger.WithFields(map[string]any{
			"proxy": proxy.GetHost(),
			"dc":    dcID,
		}).Info("[mtproxy] connection established")
	}

	// The obfuscated connection handles:
	// - TLS framing (if using fake TLS)
	// - Obfuscation encryption/decryption
	// - Protocol tag (embedded in handshake)

	return &mtproxyConn{
		reader: NewReader(context.TODO(), obfConn),
		conn:   obfConn,
	}, nil
}
