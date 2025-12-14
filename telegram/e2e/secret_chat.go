// Copyright (c) 2025 @AmarnathCJD
// Secret Chat manager for E2E encrypted chats

package e2e

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	aes "github.com/amarnathcjd/gogram/internal/aes_ige"
	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/utils"
)

const (
	CurrentLayer = 73 // MTProto Secret Chat Layer 73
	MinLayer     = 46 // Minimum supported layer
)

// SecretChat represents an active secret chat session
type SecretChat struct {
	ID              int32
	AccessHash      int64
	UserID          int64
	AdminID         int64
	SharedKey       []byte
	KeyFingerprint  int64
	Layer           int32  // Remote party's layer
	InSeqNo         int32  // Incoming sequence number
	OutSeqNo        int32  // Outgoing sequence number
	TTL             int32  // Default TTL for messages
	IsOriginator    bool   // Whether we initiated the chat
	State           string // "requested", "accepted", "ready"
	DH              *DH    // Diffie-Hellman instance
	CreatedAt       time.Time
	LastMessageTime time.Time

	// Perfect Forward Secrecy
	MessageCount int32     // Messages encrypted with current key
	KeyCreatedAt time.Time // When current key was created
	PendingRekey bool      // Whether rekey is in progress

	mu sync.RWMutex
}

// SecretChatManager manages all secret chats
type SecretChatManager struct {
	chats map[int32]*SecretChat
	mu    sync.RWMutex
}

// NewSecretChatManager creates a new secret chat manager
func NewSecretChatManager() *SecretChatManager {
	return &SecretChatManager{
		chats: make(map[int32]*SecretChat),
	}
}

// CreateSecretChat creates a new secret chat (as originator)
func (m *SecretChatManager) CreateSecretChat(chatID int32, userID int64, prime []byte, g int32) (*SecretChat, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create DH instance
	dh, err := NewDH(prime, g)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create DH: %w", err)
	}

	// Generate our key
	if err := dh.GenerateKey(); err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}

	chat := &SecretChat{
		ID:           chatID,
		UserID:       userID,
		Layer:        MinLayer, // Start with minimum layer
		InSeqNo:      0,
		OutSeqNo:     0,
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

// AcceptSecretChat accepts a secret chat request (as responder)
func (m *SecretChatManager) AcceptSecretChat(chatID int32, accessHash int64, userID int64, adminID int64, gA []byte, prime []byte, g int32) (*SecretChat, []byte, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create DH instance
	dh, err := NewDH(prime, g)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to create DH: %w", err)
	}

	// Generate our key
	if err := dh.GenerateKey(); err != nil {
		return nil, nil, 0, fmt.Errorf("failed to generate key: %w", err)
	}

	// Compute shared key
	if err := dh.ComputeSharedKey(gA); err != nil {
		return nil, nil, 0, fmt.Errorf("failed to compute shared key: %w", err)
	}

	// Compute key fingerprint
	fingerprint := ComputeKeyFingerprint(dh.SharedKey)

	// Debug logging
	fmt.Printf("RESPONDER: Computed shared key (first 32 bytes): %x\n", dh.SharedKey[:32])
	fmt.Printf("RESPONDER: Key fingerprint: %d\n", fingerprint)

	chat := &SecretChat{
		ID:              chatID,
		AccessHash:      accessHash,
		UserID:          userID,
		AdminID:         adminID,
		SharedKey:       dh.SharedKey,
		KeyFingerprint:  fingerprint,
		Layer:           MinLayer,
		InSeqNo:         0,
		OutSeqNo:        0,
		TTL:             0,
		IsOriginator:    false,
		State:           "ready", // Responder has shared key immediately, so state is ready
		DH:              dh,
		CreatedAt:       time.Now(),
		KeyCreatedAt:    time.Now(),
		LastMessageTime: time.Now(),
	}

	m.chats[chatID] = chat

	return chat, dh.GB.Bytes(), fingerprint, nil
}

// CompleteKeyExchange completes the key exchange (for originator after receiving g_b)
func (m *SecretChatManager) CompleteKeyExchange(chatID int32, gB []byte, fingerprint int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	chat, exists := m.chats[chatID]
	if !exists {
		return errors.New("secret chat not found")
	}

	chat.mu.Lock()
	defer chat.mu.Unlock()

	// Compute shared key using our private 'a' and received 'g_b'
	// key = g_b^a mod p
	if err := chat.DH.ComputeSharedKey(gB); err != nil {
		return fmt.Errorf("failed to compute shared key: %w", err)
	}

	// Verify fingerprint matches what responder computed
	// Responder computed: key = g_a^b mod p
	// We computed: key = g_b^a mod p
	// These should be equal: g^(ab) mod p
	expectedFingerprint := ComputeKeyFingerprint(chat.DH.SharedKey)

	// Debug logging
	fmt.Printf("ORIGINATOR: Computed shared key (first 32 bytes): %x\n", chat.DH.SharedKey[:32])
	fmt.Printf("ORIGINATOR: Our fingerprint: %d\n", expectedFingerprint)
	fmt.Printf("ORIGINATOR: Received fingerprint: %d\n", fingerprint)

	if expectedFingerprint != fingerprint {
		return fmt.Errorf("key fingerprint mismatch: expected %d, got %d", expectedFingerprint, fingerprint)
	}

	chat.SharedKey = chat.DH.SharedKey
	chat.KeyFingerprint = fingerprint
	chat.State = "ready"
	chat.LastMessageTime = time.Now()

	return nil
}

// GetSecretChat retrieves a secret chat by ID
func (m *SecretChatManager) GetSecretChat(chatID int32) (*SecretChat, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	chat, exists := m.chats[chatID]
	if !exists {
		return nil, errors.New("secret chat not found")
	}

	return chat, nil
}

// EncryptMessage encrypts a message for a secret chat
func (chat *SecretChat) EncryptMessage(plaintext []byte) (msgKey []byte, encrypted []byte, err error) {
	chat.mu.Lock()
	defer chat.mu.Unlock()

	if chat.State != "ready" {
		return nil, nil, errors.New("secret chat not ready")
	}

	// Check if we need to rekey
	if chat.ShouldRekey() {
		chat.PendingRekey = true
		// Note: Actual rekeying should be triggered by the application
	}

	// Encrypt using MTProto 2.0
	msgKey, encrypted, err = aes.EncryptMessageMTProto(chat.SharedKey, plaintext, chat.IsOriginator)
	if err != nil {
		return nil, nil, err
	}

	// Increment message counter
	chat.MessageCount++
	chat.LastMessageTime = time.Now()

	return msgKey, encrypted, nil
}

// DecryptMessage decrypts a message from a secret chat
func (chat *SecretChat) DecryptMessage(msgKey []byte, encrypted []byte) (plaintext []byte, err error) {
	chat.mu.Lock()
	defer chat.mu.Unlock()

	if chat.State != "ready" {
		return nil, errors.New("secret chat not ready")
	}

	// Decrypt using MTProto 2.0
	plaintext, err = aes.DecryptMessageMTProto(chat.SharedKey, msgKey, encrypted, chat.IsOriginator)
	if err != nil {
		return nil, err
	}

	// Increment message counter
	chat.MessageCount++
	chat.LastMessageTime = time.Now()

	return plaintext, nil
}

// ShouldRekey determines if we should initiate rekeying
// Rekey if: more than 100 messages OR key older than 1 week (and at least 1 message sent)
func (chat *SecretChat) ShouldRekey() bool {
	if chat.PendingRekey {
		return false
	}

	// More than 100 messages
	if chat.MessageCount >= 100 {
		return true
	}

	// Key older than 1 week and at least 1 message sent
	if time.Since(chat.KeyCreatedAt) > 7*24*time.Hour && chat.MessageCount > 0 {
		return true
	}

	return false
}

// GetNextSeqNo returns the next outgoing sequence number
func (chat *SecretChat) GetNextSeqNo() int32 {
	chat.mu.Lock()
	defer chat.mu.Unlock()

	seqNo := chat.OutSeqNo
	chat.OutSeqNo++
	return seqNo
}

// UpdateLayer updates the remote party's layer
func (chat *SecretChat) UpdateLayer(layer int32) {
	chat.mu.Lock()
	defer chat.mu.Unlock()

	if layer > chat.Layer {
		chat.Layer = layer
	}
}

// GenerateRandomID generates a random message ID
func GenerateRandomID() (int64, error) {
	bytes := make([]byte, 8)
	_, err := rand.Read(bytes)
	if err != nil {
		return 0, err
	}

	// Convert bytes to int64
	randomID := int64(0)
	for i := 0; i < 8; i++ {
		randomID = (randomID << 8) | int64(bytes[i])
	}

	return randomID, nil
}

// RemoveSecretChat removes a secret chat
func (m *SecretChatManager) RemoveSecretChat(chatID int32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.chats, chatID)
}

// UpdateChatID updates a chat's ID (used when server assigns actual ID)
func (m *SecretChatManager) UpdateChatID(oldID, newID int32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if chat, exists := m.chats[oldID]; exists {
		delete(m.chats, oldID)
		chat.ID = newID
		m.chats[newID] = chat
	}
}

// Close closes a secret chat
func (m *SecretChatManager) Close(chatID int32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.chats, chatID)
}

// GetAllChats returns all active secret chats
func (m *SecretChatManager) GetAllChats() []*SecretChat {
	m.mu.RLock()
	defer m.mu.RUnlock()

	chats := make([]*SecretChat, 0, len(m.chats))
	for _, chat := range m.chats {
		chats = append(chats, chat)
	}

	return chats
}

// SerializeDecryptedMessage serializes a DecryptedMessage for encryption
// It wraps the message in a DecryptedMessageLayer with the specified layer version
func SerializeDecryptedMessage(msg DecryptedMessage, layer int32, inSeqNo, outSeqNo int32) ([]byte, error) {
	// Wrap message in layer
	layeredMsg := &DecryptedMessageLayer{
		RandomBytes: utils.RandomBytes(15),
		Layer:       layer,
		InSeqNo:     inSeqNo,
		OutSeqNo:    outSeqNo,
		Message:     msg,
	}

	return tl.Marshal(layeredMsg)
}

// DeserializeDecryptedMessage deserializes a DecryptedMessage from decrypted data
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

// SerializeDecryptedMessageService serializes a DecryptedMessageService
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

// EncryptedFileKey holds the encryption parameters for a file
type EncryptedFileKey struct {
	Key         []byte // 256-bit AES key
	IV          []byte // 256-bit initialization vector
	Fingerprint int32  // 32-bit key fingerprint
}

// GenerateFileEncryptionKey generates a random key and IV for file encryption
// Returns key, IV, and fingerprint
func GenerateFileEncryptionKey() (*EncryptedFileKey, error) {
	// Generate 256-bit (32 bytes) random key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	// Generate 256-bit (32 bytes) random IV
	iv := make([]byte, 32)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	// Compute fingerprint: md5(key + iv), then XOR first and second half
	// digest = md5(key + iv)
	// fingerprint = substr(digest, 0, 4) XOR substr(digest, 4, 4)
	h := md5.New()
	h.Write(key)
	h.Write(iv)
	digest := h.Sum(nil)

	// XOR first 4 bytes with second 4 bytes
	fingerprint := binary.LittleEndian.Uint32(digest[0:4]) ^ binary.LittleEndian.Uint32(digest[4:8])

	return &EncryptedFileKey{
		Key:         key,
		IV:          iv,
		Fingerprint: int32(fingerprint),
	}, nil
}

// EncryptFile encrypts file data using AES-256-IGE with the provided key and IV
func EncryptFile(data []byte, key, iv []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	if len(iv) != 32 {
		return nil, fmt.Errorf("IV must be 32 bytes, got %d", len(iv))
	}

	// Pad data to 16-byte boundary if needed
	paddingLen := 0
	if len(data)%16 != 0 {
		paddingLen = 16 - (len(data) % 16)
	}

	paddedData := make([]byte, len(data)+paddingLen)
	copy(paddedData, data)
	if paddingLen > 0 {
		// Add random padding
		if _, err := rand.Read(paddedData[len(data):]); err != nil {
			return nil, fmt.Errorf("failed to generate padding: %w", err)
		}
	}

	// Encrypt using AES-256-IGE
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

// DecryptFile decrypts file data using AES-256-IGE with the provided key and IV
func DecryptFile(encryptedData []byte, key, iv []byte, originalSize int) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	if len(iv) != 32 {
		return nil, fmt.Errorf("IV must be 32 bytes, got %d", len(iv))
	}

	// Decrypt using AES-256-IGE
	cipher, err := aes.NewCipher(key, iv)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	decrypted := make([]byte, len(encryptedData))
	if err := cipher.DoAES256IGEdecrypt(encryptedData, decrypted); err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	// Remove padding if original size is known
	if originalSize > 0 && originalSize <= len(decrypted) {
		return decrypted[:originalSize], nil
	}

	return decrypted, nil
}

// VerifyFileFingerprint verifies the fingerprint of a file encryption key
func VerifyFileFingerprint(key, iv []byte, fingerprint int32) bool {
	h := md5.New()
	h.Write(key)
	h.Write(iv)
	digest := h.Sum(nil)

	expectedFingerprint := binary.LittleEndian.Uint32(digest[0:4]) ^ binary.LittleEndian.Uint32(digest[4:8])
	return int32(expectedFingerprint) == fingerprint
}
