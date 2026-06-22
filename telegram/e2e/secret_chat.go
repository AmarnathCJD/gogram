// Copyright (c) 2025 @AmarnathCJD

package e2e

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	aes "github.com/amarnathcjd/gogram/internal/aes_ige"
	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/utils"
)

const (
	CurrentLayer = 73
	MinLayer     = 46
)

type SecretChat struct {
	ID              int32
	AccessHash      int64
	UserID          int64
	AdminID         int64
	SharedKey       []byte
	KeyFingerprint  int64
	Layer           int32
	InSeqNoCounter  int32
	OutSeqNoCounter int32
	TTL             int32
	IsOriginator    bool
	State           string
	DH              *DH
	CreatedAt       time.Time
	LastMessageTime time.Time

	MessageCount int32
	KeyCreatedAt time.Time
	PendingRekey bool

	mu sync.RWMutex
}

type SecretChatManager struct {
	chats map[int32]*SecretChat
	mu    sync.RWMutex
}

func NewSecretChatManager() *SecretChatManager {
	return &SecretChatManager{
		chats: make(map[int32]*SecretChat),
	}
}

func (m *SecretChatManager) CreateSecretChat(chatID int32, userID int64, prime []byte, g int32, extraEntropy []byte) (*SecretChat, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	dh, err := NewDH(prime, g)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create DH: %w", err)
	}

	if err := dh.GenerateKey(extraEntropy); err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}

	chat := &SecretChat{
		ID:           chatID,
		UserID:       userID,
		Layer:        MinLayer,
		TTL:          0,
		IsOriginator: true,
		State:        "requested",
		DH:           dh,
		CreatedAt:    time.Now(),
		KeyCreatedAt: time.Now(),
	}

	m.chats[chatID] = chat

	return chat, dh.GA.Bytes(), nil
}

func (m *SecretChatManager) AcceptSecretChat(chatID int32, accessHash int64, userID int64, adminID int64, ourSelfID int64, gA []byte, prime []byte, g int32, extraEntropy []byte) (*SecretChat, []byte, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	dh, err := NewDH(prime, g)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to create DH: %w", err)
	}

	if err := dh.GenerateKey(extraEntropy); err != nil {
		return nil, nil, 0, fmt.Errorf("failed to generate key: %w", err)
	}

	if err := dh.ComputeSharedKey(gA); err != nil {
		return nil, nil, 0, fmt.Errorf("failed to compute shared key: %w", err)
	}

	fingerprint := ComputeKeyFingerprint(dh.SharedKey)

	chat := &SecretChat{
		ID:              chatID,
		AccessHash:      accessHash,
		UserID:          userID,
		AdminID:         adminID,
		SharedKey:       dh.SharedKey,
		KeyFingerprint:  fingerprint,
		Layer:           MinLayer,
		TTL:             0,
		IsOriginator:    false,
		State:           "ready",
		DH:              dh,
		CreatedAt:       time.Now(),
		KeyCreatedAt:    time.Now(),
		LastMessageTime: time.Now(),
	}

	m.chats[chatID] = chat

	return chat, dh.GB.Bytes(), fingerprint, nil
}

func (m *SecretChatManager) CompleteKeyExchange(chatID int32, gB []byte, fingerprint int64, ourSelfID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	chat, exists := m.chats[chatID]
	if !exists {
		return errors.New("secret chat not found")
	}

	chat.mu.Lock()
	defer chat.mu.Unlock()

	if err := chat.DH.ComputeSharedKey(gB); err != nil {
		return fmt.Errorf("failed to compute shared key: %w", err)
	}

	expectedFingerprint := ComputeKeyFingerprint(chat.DH.SharedKey)
	if expectedFingerprint != fingerprint {
		return fmt.Errorf("key fingerprint mismatch: expected %d, got %d", expectedFingerprint, fingerprint)
	}

	chat.SharedKey = chat.DH.SharedKey
	chat.KeyFingerprint = fingerprint
	chat.AdminID = ourSelfID
	chat.State = "ready"
	chat.LastMessageTime = time.Now()

	return nil
}

func (m *SecretChatManager) GetSecretChat(chatID int32) (*SecretChat, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	chat, exists := m.chats[chatID]
	if !exists {
		return nil, errors.New("secret chat not found")
	}

	return chat, nil
}

func (chat *SecretChat) EncryptMessage(plaintext []byte) (msgKey []byte, encrypted []byte, err error) {
	chat.mu.Lock()
	defer chat.mu.Unlock()

	if chat.State != "ready" {
		return nil, nil, errors.New("secret chat not ready")
	}

	if chat.ShouldRekey() {
		chat.PendingRekey = true
	}

	msgKey, encrypted, err = aes.EncryptMessageMTProto(chat.SharedKey, plaintext, chat.IsOriginator)
	if err != nil {
		return nil, nil, err
	}

	chat.MessageCount++
	chat.LastMessageTime = time.Now()

	return msgKey, encrypted, nil
}

func (chat *SecretChat) DecryptMessage(msgKey []byte, encrypted []byte) (plaintext []byte, err error) {
	chat.mu.Lock()
	defer chat.mu.Unlock()

	if chat.State != "ready" {
		return nil, errors.New("secret chat not ready")
	}

	plaintext, err = aes.DecryptMessageMTProto(chat.SharedKey, msgKey, encrypted, !chat.IsOriginator)
	if err != nil {
		return nil, err
	}

	chat.MessageCount++
	chat.LastMessageTime = time.Now()

	return plaintext, nil
}

func (chat *SecretChat) ShouldRekey() bool {
	if chat.PendingRekey {
		return false
	}
	if chat.MessageCount >= 100 {
		return true
	}
	if time.Since(chat.KeyCreatedAt) > 7*24*time.Hour && chat.MessageCount > 0 {
		return true
	}
	return false
}

// NextOutSeqNo returns the next wire-format outgoing seq_no.
//
// The core.telegram.org/api/end-to-end/seq_no page documents the *opposite*
// parity convention from what every actual client (tdlib, tdesktop,
// MadelineProto) uses. Going by tdlib's SecretChatActor.cpp the wire formula
// is `(counter+1)*2 - 1 - x` where x=0 for the creator, x=1 for the
// responder, i.e.:
//
//	creator   out_seq_no = 1, 3, 5, ...   (ODD)
//	responder out_seq_no = 0, 2, 4, ...   (EVEN)
//
// The peer's validator rejects mismatched parity silently, which manifested
// as Telegram-Android dropping our messages and cancelling the chat.
func (chat *SecretChat) NextOutSeqNo() int32 {
	chat.mu.Lock()
	defer chat.mu.Unlock()

	chat.OutSeqNoCounter++
	x := int32(0)
	if !isCreator(chat) {
		x = 1
	}
	return chat.OutSeqNoCounter*2 - 1 - x
}

// CurrentInSeqNo returns the wire-format InSeqNo to send with a message.
//
//	creator   in_seq_no = 2*received + 0   (EVEN)
//	responder in_seq_no = 2*received + 1   (ODD)
//
// (Mirror of NextOutSeqNo's parity, matches tdlib's check
// `in_seq_no % 2 == auth_state_.x`.)
func (chat *SecretChat) CurrentInSeqNo() int32 {
	chat.mu.RLock()
	defer chat.mu.RUnlock()

	x := int32(0)
	if !isCreator(chat) {
		x = 1
	}
	return 2*chat.InSeqNoCounter + x
}

// RecordIncomingSeqNo increments the receive counter after a successful decrypt.
func (chat *SecretChat) RecordIncomingSeqNo() {
	chat.mu.Lock()
	defer chat.mu.Unlock()
	chat.InSeqNoCounter++
}

func isCreator(chat *SecretChat) bool {
	return chat.IsOriginator
}

func (chat *SecretChat) UpdateLayer(layer int32) {
	chat.mu.Lock()
	defer chat.mu.Unlock()

	if layer > chat.Layer {
		chat.Layer = layer
	}
}

func GenerateRandomID() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b[:])), nil
}

func (m *SecretChatManager) RemoveSecretChat(chatID int32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if chat, ok := m.chats[chatID]; ok {
		zeroize(chat)
		delete(m.chats, chatID)
	}
}

func (m *SecretChatManager) UpdateChatID(oldID, newID int32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if chat, exists := m.chats[oldID]; exists {
		delete(m.chats, oldID)
		chat.ID = newID
		m.chats[newID] = chat
	}
}

func (m *SecretChatManager) Close(chatID int32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if chat, ok := m.chats[chatID]; ok {
		zeroize(chat)
		delete(m.chats, chatID)
	}
}

func zeroize(chat *SecretChat) {
	chat.mu.Lock()
	defer chat.mu.Unlock()
	for i := range chat.SharedKey {
		chat.SharedKey[i] = 0
	}
	chat.SharedKey = nil
	if chat.DH != nil {
		for i := range chat.DH.SharedKey {
			chat.DH.SharedKey[i] = 0
		}
		chat.DH.SharedKey = nil
		chat.DH.A = nil
	}
}

func (m *SecretChatManager) GetAllChats() []*SecretChat {
	m.mu.RLock()
	defer m.mu.RUnlock()

	chats := make([]*SecretChat, 0, len(m.chats))
	for _, chat := range m.chats {
		chats = append(chats, chat)
	}

	return chats
}

func SerializeDecryptedMessage(msg DecryptedMessage, layer int32, inSeqNo, outSeqNo int32) ([]byte, error) {
	layeredMsg := &DecryptedMessageLayer{
		RandomBytes: utils.RandomBytes(15),
		Layer:       layer,
		InSeqNo:     inSeqNo,
		OutSeqNo:    outSeqNo,
		Message:     msg,
	}
	return tl.Marshal(layeredMsg)
}

func DeserializeDecryptedMessage(data []byte) (*DecryptedMessageLayer, error) {
	obj, err := tl.DecodeUnknownObject(data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decode decrypted message layer: %w", err)
	}
	layeredMsg, ok := obj.(*DecryptedMessageLayer)
	if !ok {
		return nil, fmt.Errorf("decoded object is not DecryptedMessageLayer")
	}
	return layeredMsg, nil
}

func SerializeDecryptedMessageService(msg *DecryptedMessageService, layer int32, inSeqNo, outSeqNo int32) ([]byte, error) {
	layeredMsg := &DecryptedMessageLayer{
		RandomBytes: utils.RandomBytes(15),
		Layer:       layer,
		InSeqNo:     inSeqNo,
		OutSeqNo:    outSeqNo,
		Message:     msg,
	}
	return tl.Marshal(layeredMsg)
}

type EncryptedFileKey struct {
	Key         []byte
	IV          []byte
	Fingerprint int32
}

func GenerateFileEncryptionKey() (*EncryptedFileKey, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	iv := make([]byte, 32)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	h := md5.New()
	h.Write(key)
	h.Write(iv)
	digest := h.Sum(nil)

	fingerprint := binary.LittleEndian.Uint32(digest[0:4]) ^ binary.LittleEndian.Uint32(digest[4:8])

	return &EncryptedFileKey{
		Key:         key,
		IV:          iv,
		Fingerprint: int32(fingerprint),
	}, nil
}

func EncryptFile(data []byte, key, iv []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	if len(iv) != 32 {
		return nil, fmt.Errorf("IV must be 32 bytes, got %d", len(iv))
	}

	paddingLen := 0
	if len(data)%16 != 0 {
		paddingLen = 16 - (len(data) % 16)
	}

	paddedData := make([]byte, len(data)+paddingLen)
	copy(paddedData, data)
	if paddingLen > 0 {
		if _, err := rand.Read(paddedData[len(data):]); err != nil {
			return nil, fmt.Errorf("failed to generate padding: %w", err)
		}
	}

	cipher, err := aes.NewCipher(key, iv)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	encrypted := make([]byte, len(paddedData))
	if err := cipher.DoAES256IGEencrypt(paddedData, encrypted); err != nil {
		return nil, fmt.Errorf("failed to encrypt: %w", err)
	}

	return encrypted, nil
}

// EncryptFileStream encrypts src into dst using AES-256-IGE with the given
// key/IV, padding the trailing partial block with random bytes so that the
// total output length is a multiple of 16. It buffers at most ~64 KiB at a
// time and is safe for arbitrarily large inputs.
//
// totalSize must be the exact number of plaintext bytes available on src; it
// is used to compute the final padding length. Pass -1 if unknown, and the
// stream will be drained first (less memory-efficient).
func EncryptFileStream(dst io.Writer, src io.Reader, key, iv []byte, totalSize int64) (int64, error) {
	if len(key) != 32 {
		return 0, fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	if len(iv) != 32 {
		return 0, fmt.Errorf("IV must be 32 bytes, got %d", len(iv))
	}

	cipher, err := aes.NewCipher(key, iv)
	if err != nil {
		return 0, fmt.Errorf("failed to create cipher: %w", err)
	}

	const chunkSize = 64 * 1024 // multiple of 16

	var written, readTotal int64
	tail := make([]byte, 0, 16)

	for {
		inBuf := make([]byte, chunkSize)
		n, err := io.ReadFull(src, inBuf)
		if n > 0 {
			readTotal += int64(n)
		}
		aligned := n
		atEOF := err == io.ErrUnexpectedEOF || err == io.EOF
		if atEOF {
			aligned = (n / 16) * 16
		} else if err != nil {
			return written, fmt.Errorf("read source: %w", err)
		}

		if aligned > 0 {
			outBuf := make([]byte, aligned)
			if err := cipher.DoAES256IGEencrypt(inBuf[:aligned], outBuf); err != nil {
				return written, fmt.Errorf("encrypt block: %w", err)
			}
			if _, err := dst.Write(outBuf); err != nil {
				return written, fmt.Errorf("write encrypted: %w", err)
			}
			written += int64(aligned)
		}

		if atEOF {
			tail = append(tail, inBuf[aligned:n]...)
			break
		}
	}

	if totalSize >= 0 && readTotal != totalSize {
		return written, fmt.Errorf("source size mismatch: declared %d, read %d", totalSize, readTotal)
	}

	pad := 0
	if len(tail)%16 != 0 {
		pad = 16 - (len(tail) % 16)
	}
	if pad > 0 || len(tail) > 0 {
		final := make([]byte, len(tail)+pad)
		copy(final, tail)
		if pad > 0 {
			if _, err := rand.Read(final[len(tail):]); err != nil {
				return written, fmt.Errorf("generate padding: %w", err)
			}
		}
		out := make([]byte, len(final))
		if err := cipher.DoAES256IGEencrypt(final, out); err != nil {
			return written, fmt.Errorf("encrypt final block: %w", err)
		}
		if _, err := dst.Write(out); err != nil {
			return written, fmt.Errorf("write final: %w", err)
		}
		written += int64(len(out))
	}

	return written, nil
}

func DecryptFile(encryptedData []byte, key, iv []byte, originalSize int) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	if len(iv) != 32 {
		return nil, fmt.Errorf("IV must be 32 bytes, got %d", len(iv))
	}

	cipher, err := aes.NewCipher(key, iv)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	decrypted := make([]byte, len(encryptedData))
	if err := cipher.DoAES256IGEdecrypt(encryptedData, decrypted); err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	if originalSize > 0 && originalSize <= len(decrypted) {
		return decrypted[:originalSize], nil
	}

	return decrypted, nil
}

func VerifyFileFingerprint(key, iv []byte, fingerprint int32) bool {
	h := md5.New()
	h.Write(key)
	h.Write(iv)
	digest := h.Sum(nil)

	expectedFingerprint := binary.LittleEndian.Uint32(digest[0:4]) ^ binary.LittleEndian.Uint32(digest[4:8])
	return int32(expectedFingerprint) == fingerprint
}
