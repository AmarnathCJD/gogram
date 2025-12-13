// Copyright (c) 2025 @AmarnathCJD

package transport

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"slices"
	"time"

	"errors"
)

// obfuscatedConn wraps a connection with AES-256-CTR obfuscation
// needed for websocket connections and MTProxy support
type obfuscatedConn struct {
	conn      io.ReadWriteCloser
	encryptor cipher.Stream
	decryptor cipher.Stream
}

func NewObfuscatedConn(conn io.ReadWriteCloser, protocolID []byte) (*obfuscatedConn, error) {
	if len(protocolID) > 4 {
		return nil, errors.New("protocol ID must be 4 bytes or less")
	}

	paddedProtocol := make([]byte, 4)
	for i := range 4 {
		paddedProtocol[i] = protocolID[i%len(protocolID)]
	}

	init, err := generateInitPayload(paddedProtocol)
	if err != nil {
		return nil, fmt.Errorf("generating init payload: %w", err)
	}

	initRev := make([]byte, 64)
	for i := range 64 {
		initRev[i] = init[63-i]
	}

	encryptKey := init[8:40]
	encryptIV := init[40:56]
	decryptKey := initRev[8:40]
	decryptIV := initRev[40:56]

	encryptBlock, err := aes.NewCipher(encryptKey)
	if err != nil {
		return nil, fmt.Errorf("creating encrypt cipher: %w", err)
	}
	encryptor := cipher.NewCTR(encryptBlock, encryptIV)

	decryptBlock, err := aes.NewCipher(decryptKey)
	if err != nil {
		return nil, fmt.Errorf("creating decrypt cipher: %w", err)
	}
	decryptor := cipher.NewCTR(decryptBlock, decryptIV)

	encryptedInit := make([]byte, 64)
	copy(encryptedInit, init)
	encryptor.XORKeyStream(encryptedInit, encryptedInit)

	finalInit := make([]byte, 64)
	copy(finalInit, init[:56])
	copy(finalInit[56:], encryptedInit[56:])

	_, err = conn.Write(finalInit)
	if err != nil {
		return nil, fmt.Errorf("sending init payload: %w", err)
	}

	return &obfuscatedConn{
		conn:      conn,
		encryptor: encryptor,
		decryptor: decryptor,
	}, nil
}

func generateInitPayload(protocolID []byte) ([]byte, error) {

	forbiddenFirst := []uint32{
		0x44414548,
		0x54534f50,
		0x20544547,
		0x4954504f,
		0x02010316,
		0xdddddddd, // padded intermediate
		0xeeeeeeee, // intermediate
	}

	for {
		init := make([]byte, 64)

		if _, err := rand.Read(init); err != nil {
			return nil, fmt.Errorf("reading random bytes: %w", err)
		}

		if init[0] == 0xef {
			continue
		}

		firstInt := binary.LittleEndian.Uint32(init[0:4])
		forbidden := slices.Contains(forbiddenFirst, firstInt)
		if forbidden {
			continue
		}

		secondInt := binary.LittleEndian.Uint32(init[4:8])
		if secondInt == 0x00000000 {
			continue
		}

		copy(init[56:60], protocolID)

		return init, nil
	}
}

func (o *obfuscatedConn) Read(b []byte) (int, error) {
	n, err := o.conn.Read(b)
	if err != nil {
		return n, err
	}

	o.decryptor.XORKeyStream(b[:n], b[:n])

	return n, nil
}

func (o *obfuscatedConn) Write(b []byte) (int, error) {
	encrypted := make([]byte, len(b))
	o.encryptor.XORKeyStream(encrypted, b)

	return o.conn.Write(encrypted)
}

func (o *obfuscatedConn) Close() error {
	return o.conn.Close()
}

func (o *obfuscatedConn) LocalAddr() net.Addr {
	if c, ok := o.conn.(net.Conn); ok {
		return c.LocalAddr()
	}
	return nil
}

func (o *obfuscatedConn) RemoteAddr() net.Addr {
	if c, ok := o.conn.(net.Conn); ok {
		return c.RemoteAddr()
	}
	return nil
}

func (o *obfuscatedConn) SetDeadline(t time.Time) error {
	if c, ok := o.conn.(net.Conn); ok {
		return c.SetDeadline(t)
	}
	return nil
}

func (o *obfuscatedConn) SetReadDeadline(t time.Time) error {
	if c, ok := o.conn.(net.Conn); ok {
		return c.SetReadDeadline(t)
	}
	return nil
}

func (o *obfuscatedConn) SetWriteDeadline(t time.Time) error {
	if c, ok := o.conn.(net.Conn); ok {
		return c.SetWriteDeadline(t)
	}
	return nil
}
