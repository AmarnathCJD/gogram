// Copyright (c) 2025 RoseLoverX

package transport

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"io"

	"github.com/pkg/errors"
)

// obfuscatedConn wraps a connection with AES-256-CTR obfuscation
// needed for websocket connections and MTProxy support
type obfuscatedConn struct {
	conn      io.ReadWriteCloser
	encryptor cipher.Stream
	decryptor cipher.Stream
}

func newObfuscatedConn(conn io.ReadWriteCloser, protocolID []byte) (*obfuscatedConn, error) {
	if len(protocolID) > 4 {
		return nil, errors.New("protocol ID must be 4 bytes or less")
	}

	paddedProtocol := make([]byte, 4)
	for i := 0; i < 4; i++ {
		paddedProtocol[i] = protocolID[i%len(protocolID)]
	}

	init, err := generateInitPayload(paddedProtocol)
	if err != nil {
		return nil, errors.Wrap(err, "generating init payload")
	}

	initRev := make([]byte, 64)
	for i := 0; i < 64; i++ {
		initRev[i] = init[63-i]
	}

	encryptKey := init[8:40]
	encryptIV := init[40:56]
	decryptKey := initRev[8:40]
	decryptIV := initRev[40:56]

	encryptBlock, err := aes.NewCipher(encryptKey)
	if err != nil {
		return nil, errors.Wrap(err, "creating encrypt cipher")
	}
	encryptor := cipher.NewCTR(encryptBlock, encryptIV)

	decryptBlock, err := aes.NewCipher(decryptKey)
	if err != nil {
		return nil, errors.Wrap(err, "creating decrypt cipher")
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
		return nil, errors.Wrap(err, "sending init payload")
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
			return nil, errors.Wrap(err, "reading random bytes")
		}

		if init[0] == 0xef {
			continue
		}

		firstInt := binary.LittleEndian.Uint32(init[0:4])
		forbidden := false
		for _, f := range forbiddenFirst {
			if firstInt == f {
				forbidden = true
				break
			}
		}
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
