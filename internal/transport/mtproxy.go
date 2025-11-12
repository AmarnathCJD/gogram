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
	"log"
	"math/big"
	"net"
	"net/url"
	"time"

	"github.com/pkg/errors"
)

const (
	_MOD               = 0x7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFED
	_POW               = 0x3FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF6
	_MAX_GREASE        = 8
	_MAX_TLS_MSG       = 16 * 1024 // 16 KB
	defaultSNI         = "media.steampered.com"
	fakeTLSHandshakeID = 0xee
)

// TLS Hello operation types
type tlsOp struct {
	opType string
	value  interface{}
}

var _TLS_HELLO_OPS = []tlsOp{
	{"string", []byte("\x16\x03\x01\x02\x00\x01\x00\x01\xfc\x03\x03")},
	{"zero", 32},
	{"string", []byte("\x20")},
	{"id", nil},
	{"string", []byte("\x00\x20")},
	{"grease", 0},
	{
		"string",
		[]byte("\x13\x01\x13\x02\x13\x03\xc0\x2b\xc0\x2f\xc0" +
			"\x2c\xc0\x30\xcc\xa9\xcc\xa8\xc0\x13\xc0\x14" +
			"\x00\x9c\x00\x9d\x00\x2f\x00\x35\x01\x00\x01\x93"),
	},
	{"grease", 2},
	{"string", []byte("\x00\x00")},
	{
		"permutation",
		[][]tlsOp{
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
			{{"string", []byte("\x00\x05\x00\x05\x01\x00\x00\x00\x00")}},
			{
				{"string", []byte("\x00\x0a\x00\x0a\x00\x08")},
				{"grease", 4},
				{"string", []byte("\x00\x1d\x00\x17\x00\x18")},
			},
			{{"string", []byte("\x00\x0b\x00\x02\x01\x00")}},
			{
				{
					"string",
					[]byte("\x00\x0d\x00\x12\x00\x10\x04\x03\x08\x04\x04" +
						"\x01\x05\x03\x08\x05\x05\x01\x08\x06\x06\x01"),
				},
			},
			{
				{
					"string",
					[]byte("\x00\x10\x00\x0e\x00\x0c\x02\x68\x32\x08\x68" +
						"\x74\x74\x70\x2f\x31\x2e\x31"),
				},
			},
			{{"string", []byte("\x00\x12\x00\x00")}},
			{{"string", []byte("\x00\x17\x00\x00")}},
			{{"string", []byte("\x00\x1b\x00\x03\x02\x00\x02")}},
			{{"string", []byte("\x00\x23\x00\x00")}},
			{
				{"string", []byte("\x00\x2b\x00\x07\x06")},
				{"grease", 6},
				{"string", []byte("\x03\x04\x03\x03")},
			},
			{{"string", []byte("\x00\x2d\x00\x02\x01\x01")}},
			{
				{"string", []byte("\x00\x33\x00\x2b\x00\x29")},
				{"grease", 4},
				{"string", []byte("\x00\x01\x00\x00\x1d\x00\x20")},
				{"k", nil},
			},
			{{"string", []byte("\x44\x69\x00\x05\x00\x03\x02\x68\x32")}},
			{{"string", []byte("\xff\x01\x00\x01\x00")}},
		},
	},
	{"grease", 3},
	{"string", []byte("\x00\x01\x00\x00\x15")},
}

// MTProxyConfig holds the configuration for MTProxy connection
type MTProxyConfig struct {
	ProxyURL    *url.URL
	TargetAddr  string
	DCID        int16
	ModeVariant uint8
	LocalAddr   string
}

// fakeTLSConn wraps a connection with Fake TLS handshake support
type fakeTLSConn struct {
	conn   net.Conn
	reader io.Reader
	buffer []byte
}

// Ensure fakeTLSConn implements net.Conn interface
var _ net.Conn = (*fakeTLSConn)(nil)

// DecodeMTProtoProxySecret decodes the MTProto proxy secret from hex or base64
func DecodeMTProtoProxySecret(value string) (proto byte, secret []byte, serverHostname []byte, err error) {
	var data []byte

	// Try hex decode first
	if len(value)%2 == 0 {
		data, err = hexDecode(value)
		if err != nil {
			data = nil
		}
	}

	// If hex failed, try base64
	if data == nil {
		padding := len(value) % 4
		if padding > 0 {
			value += string(make([]byte, 4-padding))
			for i := len(value) - (4 - padding); i < len(value); i++ {
				value = value[:i] + "=" + value[i:]
			}
		}

		data, err = base64.URLEncoding.DecodeString(value)
		if err != nil {
			return 0, nil, nil, errors.Wrap(err, "invalid MTProto proxy secret")
		}
	}

	// Parse the secret
	proto = 0
	secret = data
	serverHostname = nil

	if len(data) >= 17 {
		proto = data[0]
		secret = data[1:17]
		if proto == fakeTLSHandshakeID {
			serverHostname = trimNullBytes(data[17:])
		}
	}
	// print secret value
	log.Printf("[MTProxy] Secret: %x", secret)
	return proto, secret, serverHostname, nil
}

// hexDecode decodes a hex string
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

// trimNullBytes removes trailing null bytes
func trimNullBytes(b []byte) []byte {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] != 0 {
			return b[:i+1]
		}
	}
	return b[:0]
}

// getY2 calculates y^2 for the elliptic curve
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

// getDoubleX calculates the x-coordinate doubling
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

// generatePublicKey generates a public key for TLS handshake
func generatePublicKey() ([]byte, error) {
	mod := new(big.Int)
	mod.SetString("7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFED", 16)

	pow := new(big.Int)
	pow.SetString("3FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF6", 16)

	var x *big.Int
	for {
		keyBytes := make([]byte, 32)
		if _, err := rand.Read(keyBytes); err != nil {
			return nil, errors.Wrap(err, "generating random bytes")
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

	// Double x three times
	for i := 0; i < 3; i++ {
		x = getDoubleX(x)
	}

	// Convert to bytes (big-endian)
	keyBytes := x.Bytes()

	// Pad to 32 bytes if needed
	if len(keyBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(keyBytes):], keyBytes)
		keyBytes = padded
	}

	// Reverse the bytes
	result := make([]byte, 32)
	for i := 0; i < 16; i++ {
		result[i], result[31-i] = keyBytes[31-i], keyBytes[i]
	}

	return result, nil
}

// startFakeTLS performs the Fake TLS handshake
func startFakeTLS(conn net.Conn, key []byte, serverHostname []byte) (*fakeTLSConn, error) {
	log.Printf("[MTProxy] Starting Fake TLS handshake with SNI: %s", string(serverHostname))

	if len(serverHostname) == 0 {
		serverHostname = []byte(defaultSNI)
	}

	// Generate session ID
	sessionID := make([]byte, 32)
	if _, err := rand.Read(sessionID); err != nil {
		return nil, errors.Wrap(err, "generating session ID")
	}

	// print sessionid, key and serverhostname
	log.Printf("[MTProxy] Session ID: %x", sessionID)
	log.Printf("[MTProxy] Key: %x", key)
	log.Printf("[MTProxy] Server Hostname: %s", string(serverHostname))

	// Generate grease values
	grease := make([]byte, _MAX_GREASE)
	if _, err := rand.Read(grease); err != nil {
		return nil, errors.Wrap(err, "generating grease")
	}

	for i := range _MAX_GREASE {
		g := (grease[i] & 0xF0) + 0x0A
		if i%2 == 1 && g == grease[i-1] {
			g ^= 0x10
		}
		grease[i] = g
	}

	// Generate public key
	publicKey, err := generatePublicKey()
	if err != nil {
		return nil, errors.Wrap(err, "generating public key")
	}

	// Build the TLS Client Hello
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
				parts := op.value.([][]tlsOp)
				// Shuffle parts (simplified - just use as-is for now)
				for _, part := range parts {
					writeOps(part)
				}
			}
		}
	}

	writeOps(_TLS_HELLO_OPS)

	// Add padding
	if len(buffer) > 515 {
		return nil, errors.New("handshake buffer too large for padding")
	}

	padSize := 515 - len(buffer)
	buffer = append(buffer, byte(padSize>>8), byte(padSize&0xFF))
	buffer = append(buffer, make([]byte, padSize)...)

	log.Printf("[MTProxy] Client Hello buffer size: %d bytes (with %d bytes padding)", len(buffer), padSize)

	// Compute client random using HMAC
	mac := hmac.New(sha256.New, key)
	mac.Write(buffer)
	digest := mac.Sum(nil)

	// print buffer
	//log.Printf("[MTProxy] Client Hello buffer before timestamp XOR: %x", buffer)
	// print digest
	//log.Printf("[MTProxy] HMAC Digest before timestamp XOR: %x", digest)
	// XOR the timestamp
	oldValue := binary.LittleEndian.Uint32(digest[28:32])
	newValue := oldValue ^ uint32(time.Now().Unix())
	binary.LittleEndian.PutUint32(digest[28:32], newValue)

	clientRandom := digest[:32]
	copy(buffer[11:11+32], clientRandom)

	log.Printf("[MTProxy] Client random: %x", clientRandom[:8])
	// print buffer to be sent
	//log.Printf("[MTProxy] Client Hello buffer: %x", buffer)

	_, err = conn.Write(buffer)
	if err != nil {
		return nil, errors.Wrap(err, "sending client hello")
	}

	log.Printf("[MTProxy] Client Hello sent successfully")

	header, payload, err := readTLSRecord(conn)
	if err != nil {
		return nil, errors.Wrap(err, "reading server hello")
	} else {
		log.Printf("[MTProxy] Server Hello received: %d bytes", len(payload))
	}

	if len(payload) < 38 {
		return nil, errors.New("server hello too short")
	}

	if payload[0] != 0x02 {
		return nil, fmt.Errorf("unexpected handshake type: %#x", payload[0])
	}

	// length = int.from_bytes(payload[1:4])
	length := int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
	if length+4 != len(payload) {
		return nil, errors.New("handshake length mismatch")
	}

	version := uint16(payload[4])<<8 | uint16(payload[5])
	if version != 0x0303 {
		return nil, fmt.Errorf("unexpected TLS version: %#x", version)
	}

	// Extract server digest from server hello
	serverDigest := make([]byte, 32)
	copy(serverDigest, payload[6:38])

	var payloadZeroed = make([]byte, len(payload))
	// payload_zeroed[6:38] = b'\x00' * 32
	copy(payloadZeroed, payload)
	for i := 6; i < 38; i++ {
		payloadZeroed[i] = 0
	}

	nextHeader, nextPayload, err := readTLSRecord(conn)
	if err != nil {
		return nil, errors.Wrap(err, "reading change cipher spec")
	}

	if nextHeader[0] != 0x14 {
		return nil, fmt.Errorf("unexpected TLS record: %#x", nextHeader[0])
	}
	log.Printf("[MTProxy] Change Cipher Spec received")
	// payload_zeroed.extend(next_header+next_payload)
	payloadZeroed = append(payloadZeroed, nextHeader...)
	payloadZeroed = append(payloadZeroed, nextPayload...)

	nextHeader, nextPayload, err = readTLSRecord(conn)
	if err != nil {
		return nil, errors.Wrap(err, "reading random data")
	}
	if nextHeader[0] != 0x17 {
		return nil, fmt.Errorf("unexpected TLS record: %#x", nextHeader[0])
	}

	// payload_zeroed.extend(next_header+next_payload)
	payloadZeroed = append(payloadZeroed, nextHeader...)
	payloadZeroed = append(payloadZeroed, nextPayload...)

	/*
		computed_digest = (
			hmac.new(
				key,
				client_random + header + payload_zeroed,
				digestmod=hashlib.sha256
			).digest()
		)

	*/

	var computedDigest []byte
	mac = hmac.New(sha256.New, key)
	mac.Write(clientRandom)
	mac.Write(header)
	mac.Write(payloadZeroed)
	computedDigest = mac.Sum(nil)

	originalDigest := serverDigest
	fmt.Printf("[MTProxy] Server Digest: %x", originalDigest)
	fmt.Printf("[MTProxy] Computed Digest: %x", computedDigest)
	if !hmac.Equal(originalDigest[:], computedDigest) {
		return nil, errors.New("TLS handshake verification failed: HMAC mismatch")
	}

	log.Printf("[MTProxy] Fake TLS handshake completed successfully")

	return &fakeTLSConn{
		conn:   conn,
		reader: conn,
		buffer: make([]byte, 0),
	}, nil
}

// readTLSRecord reads a single TLS record
func readTLSRecord(conn net.Conn) (header []byte, payload []byte, err error) {
	header = make([]byte, 5)
	_, err = io.ReadFull(conn, header)
	if err != nil {
		return nil, nil, err
	}

	if header[0] == 0x16 {
		log.Printf("[MTProxy] TLS Handshake record received")
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

// Read reads data from the Fake TLS connection, handling TLS records
func (f *fakeTLSConn) Read(b []byte) (int, error) {
	// If we have buffered data, return it
	if len(f.buffer) > 0 {
		n := copy(b, f.buffer)
		f.buffer = f.buffer[n:]
		return n, nil
	}

	// Read the next TLS record
	_, payload, err := readTLSRecord(f.conn)
	if err != nil {
		return 0, err
	}

	// Copy to output buffer
	n := copy(b, payload)
	if n < len(payload) {
		// Store remaining data
		f.buffer = append(f.buffer, payload[n:]...)
	}

	return n, nil
}

// Write writes data to the Fake TLS connection, wrapping in TLS records
func (f *fakeTLSConn) Write(b []byte) (int, error) {
	totalWritten := 0

	for offset := 0; offset < len(b); offset += _MAX_TLS_MSG {
		end := offset + _MAX_TLS_MSG
		if end > len(b) {
			end = len(b)
		}
		chunk := b[offset:end]

		// Build TLS application data record
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

// Close closes the underlying connection
func (f *fakeTLSConn) Close() error {
	return f.conn.Close()
}

// LocalAddr returns the local network address
func (f *fakeTLSConn) LocalAddr() net.Addr {
	return f.conn.LocalAddr()
}

// RemoteAddr returns the remote network address
func (f *fakeTLSConn) RemoteAddr() net.Addr {
	return f.conn.RemoteAddr()
}

// SetDeadline sets the read and write deadlines
func (f *fakeTLSConn) SetDeadline(t time.Time) error {
	return f.conn.SetDeadline(t)
}

// SetReadDeadline sets the read deadline
func (f *fakeTLSConn) SetReadDeadline(t time.Time) error {
	return f.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline
func (f *fakeTLSConn) SetWriteDeadline(t time.Time) error {
	return f.conn.SetWriteDeadline(t)
}

// DialMTProxy establishes a connection through an MTProxy
func DialMTProxy(proxyURL *url.URL, targetAddr string, dcID int16, modeVariant uint8, localAddr string) (Conn, error) {
	log.Printf("[MTProxy] Connecting to MTProxy: %s", proxyURL.Host)

	// Parse the secret from the URL
	secret := proxyURL.User.Username()
	if secret == "" {
		return nil, errors.New("MTProxy secret is required")
	}

	proto, secretBytes, serverHostname, err := DecodeMTProtoProxySecret(secret)
	if err != nil {
		return nil, errors.Wrap(err, "decoding MTProxy secret")
	}

	log.Printf("[MTProxy] Secret decoded: proto=%#x, secret_len=%d, sni=%s", proto, len(secretBytes), string(serverHostname))

	// Connect to the proxy
	var dialer net.Dialer
	if localAddr != "" {
		addr, err := net.ResolveTCPAddr("tcp", localAddr)
		if err != nil {
			return nil, errors.Wrap(err, "resolving local address")
		}
		dialer.LocalAddr = addr
	}

	rawConn, err := dialer.Dial("tcp", proxyURL.Host)
	if err != nil {
		return nil, errors.Wrap(err, "connecting to MTProxy")
	}

	tcpConnection, ok := rawConn.(*net.TCPConn)
	if !ok {
		rawConn.Close()
		return nil, errors.New("expected TCP connection")
	}

	log.Printf("[MTProxy] Connected to proxy server")

	// Wrap with appropriate connection type
	var conn net.Conn = tcpConnection

	// If Fake TLS is required, perform handshake
	if proto == fakeTLSHandshakeID {
		ftlsConn, err := startFakeTLS(tcpConnection, secretBytes, serverHostname)
		if err != nil {
			tcpConnection.Close()
			return nil, errors.Wrap(err, "Fake TLS handshake failed")
		}
		conn = ftlsConn
	}

	// Create obfuscated connection
	protocolID := ProtocolID(modeVariant)
	obfConn, err := NewObfuscatedConnWithSecret(conn, protocolID, secretBytes, dcID)
	if err != nil {
		conn.Close()
		return nil, errors.Wrap(err, "creating obfuscated connection")
	}

	log.Printf("[MTProxy] Obfuscated connection established")

	return &tcpConn{
		reader: NewReader(context.TODO(), obfConn),
		conn:   tcpConnection,
	}, nil
}
