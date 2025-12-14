// Copyright (c) 2025 @AmarnathCJD
// Secret Chat methods for the Telegram Client

package telegram

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"

	"github.com/amarnathcjd/gogram/internal/utils"
	"github.com/amarnathcjd/gogram/telegram/e2e"
)

// RequestSecretChat requests a new secret chat with a user
// Returns the chat ID and g_a for the DH exchange
func (c *Client) RequestSecretChat(userID any) (*EncryptedChatObj, error) {
	// Get DH config
	dhConfig, err := c.MessagesGetDhConfig(0, 256)
	if err != nil {
		return nil, fmt.Errorf("failed to get DH config: %w", err)
	}

	var prime []byte
	var g int32
	var random []byte

	switch config := dhConfig.(type) {
	case *MessagesDhConfigObj:
		prime = config.P
		g = config.G
		random = config.Random
	case *MessagesDhConfigNotModified:
		// Use cached DH parameters if available
		return nil, fmt.Errorf("DH config not modified, need to use cached parameters")
	default:
		return nil, fmt.Errorf("unexpected DH config type: %T", dhConfig)
	}

	// Combine server random with client random if provided
	// This is important for additional entropy
	if len(random) > 0 {
		clientRandom := utils.RandomBytes(len(random))
		for i := range random {
			random[i] ^= clientRandom[i]
		}
	}

	// Initialize secret chat manager if not exists
	if c.secretChats == nil {
		c.secretChats = e2e.NewSecretChatManager()
	}

	// Get user peer
	user, err := c.GetSendableUser(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve user: %w", err)
	}

	randomId, err := e2e.GenerateRandomID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate random ID: %w", err)
	}

	// Create secret chat locally FIRST to generate g_a
	// Use a temporary chat ID (will be replaced with server-assigned ID)
	tempChatID := int32(randomId) // Temporary ID
	chat, gA, err := c.secretChats.CreateSecretChat(tempChatID, user.(*InputUserObj).UserID, prime, g)
	if err != nil {
		return nil, fmt.Errorf("failed to create secret chat: %w", err)
	}

	// Both clients must check that g_a > 1 and g_a < p-1
	// and that 2^{2048-64} < g_a < p - 2^{2048-64}
	if !e2e.IsValidGAOrGB(chat.DH.GA, chat.DH.Prime) {
		return nil, fmt.Errorf("generated invalid g_a")
	}

	// Request encrypted chat - send g_a (NOT prime!)
	resp, err := c.MessagesRequestEncryption(user, int32(randomId), gA)
	if err != nil {
		c.secretChats.RemoveSecretChat(tempChatID)
		return nil, fmt.Errorf("failed to request encryption: %w", err)
	}

	switch encChat := resp.(type) {
	case *EncryptedChatRequested:
		// Update chat ID with server-assigned ID
		c.secretChats.UpdateChatID(tempChatID, encChat.ID)
		chat.ID = encChat.ID
		chat.AccessHash = encChat.AccessHash

		return &EncryptedChatObj{
			ID:         encChat.ID,
			AccessHash: encChat.AccessHash,
			Date:       encChat.Date,
			AdminID:    encChat.AdminID,
			// ParticipantID will be set once accepted
		}, nil
	case *EncryptedChatWaiting:
		// Update chat ID with server-assigned ID
		c.secretChats.UpdateChatID(tempChatID, encChat.ID)
		chat.ID = encChat.ID
		chat.AccessHash = encChat.AccessHash

		return &EncryptedChatObj{
			ID:         encChat.ID,
			AccessHash: encChat.AccessHash,
			Date:       encChat.Date,
			AdminID:    encChat.AdminID,
		}, nil

	default:
		return nil, fmt.Errorf("unexpected response type: %T", resp)
	}
}

// AcceptSecretChat accepts a secret chat request
func (c *Client) AcceptSecretChat(chat InputEncryptedChat, gA []byte) error {
	dhConfig, err := c.MessagesGetDhConfig(0, 256)
	if err != nil {
		return fmt.Errorf("failed to get DH config: %w", err)
	}

	var prime []byte
	var g int32
	var random []byte

	switch config := dhConfig.(type) {
	case *MessagesDhConfigObj:
		prime = config.P
		g = config.G
		random = config.Random
	case *MessagesDhConfigNotModified:
		return fmt.Errorf("DH config not modified, need to use cached parameters")
	default:
		return fmt.Errorf("unexpected DH config type: %T", dhConfig)
	}

	// Combine server random with client random for additional entropy
	if len(random) > 0 {
		clientRandom := utils.RandomBytes(len(random))
		for i := range random {
			random[i] ^= clientRandom[i]
		}
	}

	// Initialize secret chat manager if not exists
	if c.secretChats == nil {
		c.secretChats = e2e.NewSecretChatManager()
	}

	// Both clients must check that g_a > 1 and g_a < p-1
	// and that 2^{2048-64} < g_a < p - 2^{2048-64}
	primeInt := new(big.Int).SetBytes(prime)
	gAInt := new(big.Int).SetBytes(gA)

	if !e2e.IsValidGAOrGB(gAInt, primeInt) {
		return fmt.Errorf("received invalid g_a from originator")
	}

	// Accept the chat and generate our g_b
	chatAc, gB, fingerprint, err := c.secretChats.AcceptSecretChat(
		chat.ChatID,
		chat.AccessHash,
		0,
		0,
		gA,
		prime,
		g,
	)
	if err != nil {
		return fmt.Errorf("failed to accept secret chat: %w", err)
	}

	if !e2e.IsValidGAOrGB(chatAc.DH.GB, chatAc.DH.Prime) {
		return fmt.Errorf("generated invalid g_b")
	}

	// Send acceptance to server
	_, err = c.MessagesAcceptEncryption(&InputEncryptedChat{
		ChatID:     chatAc.ID,
		AccessHash: chatAc.AccessHash,
	}, gB, fingerprint)
	if err != nil {
		return fmt.Errorf("failed to accept encryption: %w", err)
	}

	// Send layer notification
	return c.SendSecretChatLayerNotification(chatAc.ID)
}

// SendSecretMessage sends an encrypted message in a secret chat
func (c *Client) SendSecretMessage(chatID int32, message string, ttl int32) error {
	chat, err := c.secretChats.GetSecretChat(chatID)
	if err != nil {
		return fmt.Errorf("failed to get secret chat: %w", err)
	}

	// Generate random ID
	randomID, err := e2e.GenerateRandomID()
	if err != nil {
		return fmt.Errorf("failed to generate random ID: %w", err)
	}

	// Create decrypted message
	msg := &e2e.DecryptedMessageObj{
		RandomID: randomID,
		TTL:      ttl,
		Message:  message,
	}

	// Get sequence numbers
	inSeqNo := chat.InSeqNo
	outSeqNo := chat.GetNextSeqNo()

	// Serialize message with sequence numbers
	serialized, err := e2e.SerializeDecryptedMessage(msg, chat.Layer, inSeqNo, outSeqNo)
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	// Encrypt message
	msgKey, encrypted, err := chat.EncryptMessage(serialized)
	if err != nil {
		return fmt.Errorf("failed to encrypt message: %w", err)
	}

	// Prepare encrypted file (combine key_fingerprint + msg_key + encrypted)
	data := make([]byte, 8+16+len(encrypted))
	// Key fingerprint (8 bytes)
	for i := range 8 {
		data[i] = byte(chat.KeyFingerprint >> (i * 8))
	}
	// msg_key (16 bytes)
	copy(data[8:], msgKey)
	// Encrypted data
	copy(data[24:], encrypted)

	// Send encrypted message
	_, err = c.MessagesSendEncrypted(false, &InputEncryptedChat{
		ChatID:     chatID,
		AccessHash: chat.AccessHash,
	}, randomID, data)
	if err != nil {
		return fmt.Errorf("failed to send encrypted message: %w", err)
	}

	return nil
}

// SendSecretChatLayerNotification sends a layer notification to the secret chat
func (c *Client) SendSecretChatLayerNotification(chatID int32) error {
	chat, err := c.secretChats.GetSecretChat(chatID)
	if err != nil {
		return fmt.Errorf("failed to get secret chat: %w", err)
	}

	// Generate random ID
	randomID, err := e2e.GenerateRandomID()
	if err != nil {
		return fmt.Errorf("failed to generate random ID: %w", err)
	}

	// Create layer notification action
	action := &e2e.DecryptedMessageActionNotifyLayer{
		Layer: e2e.CurrentLayer,
	}

	// Create service message
	serviceMsg := &e2e.DecryptedMessageService{
		RandomID: randomID,
		Action:   action,
	}

	// Serialize message
	inSeqNo := chat.InSeqNo
	outSeqNo := chat.GetNextSeqNo()
	serialized, err := e2e.SerializeDecryptedMessageService(serviceMsg, chat.Layer, inSeqNo, outSeqNo)
	if err != nil {
		return fmt.Errorf("failed to serialize service message: %w", err)
	}

	// Encrypt message
	msgKey, encrypted, err := chat.EncryptMessage(serialized)
	if err != nil {
		return fmt.Errorf("failed to encrypt message: %w", err)
	}

	// Prepare encrypted data
	data := make([]byte, 8+16+len(encrypted))
	for i := 0; i < 8; i++ {
		data[i] = byte(chat.KeyFingerprint >> (i * 8))
	}
	copy(data[8:], msgKey)
	copy(data[24:], encrypted)

	// Send encrypted service message
	_, err = c.MessagesSendEncryptedService(&InputEncryptedChat{
		ChatID:     chatID,
		AccessHash: chat.AccessHash,
	}, randomID, data)
	if err != nil {
		return fmt.Errorf("failed to send encrypted service: %w", err)
	}

	return nil
}

// DiscardSecretChat discards/deletes a secret chat
func (c *Client) DiscardSecretChat(chatID int32, revoke ...bool) error {
	_, err := c.secretChats.GetSecretChat(chatID)
	if err != nil {
		return fmt.Errorf("failed to get secret chat: %w", err)
	}

	// Discard on server
	_, err = c.MessagesDiscardEncryption(len(revoke) > 0 && revoke[0], chatID)
	if err != nil {
		return fmt.Errorf("failed to discard encryption: %w", err)
	}

	// Remove from local manager
	c.secretChats.Close(chatID)
	return nil
}

// handleSecretChatUpdate processes incoming secret chat updates and dispatches to handlers
func (c *Client) handleSecretChatUpdate(update Update) {
	c.dispatcher.RLock()
	defer c.dispatcher.RUnlock()

	for _, handlers := range c.dispatcher.e2eHandles {
		for _, handler := range handlers {
			handler.Handler(update, c)
		}
	}
}

// HandleSecretChatUpdate handles incoming secret chat updates
func (c *Client) HandleSecretChatUpdate(update Update) error {
	if c.secretChats == nil {
		c.secretChats = e2e.NewSecretChatManager()
	}
	go c.handleSecretChatUpdate(update)
	switch u := update.(type) {
	case *UpdateEncryption:
		switch chat := u.Chat.(type) {
		case *EncryptedChatObj:
			// Secret chat is ready
			existingChat, err := c.secretChats.GetSecretChat(chat.ID)
			if err == nil {
				// Chat exists - this is the originator completing key exchange with received g_b
				// The responder already completed their key exchange in AcceptSecretChat

				// CRITICAL: Validate received g_b BEFORE completing key exchange
				// The originator must check that g_b > 1 and g_b < p-1
				// and that 2^{2048-64} < g_b < p - 2^{2048-64}
				gBInt := new(big.Int).SetBytes(chat.GAOrB)

				// Need to get the prime from somewhere - it should be stored in the temp chat
				// For now, we'll need to fetch it or store it during RequestSecretChat
				// Let's get DH config to validate
				dhConfig, err := c.MessagesGetDhConfig(0, 256)
				if err != nil {
					return fmt.Errorf("failed to get DH config for validation: %w", err)
				}

				var prime []byte
				switch config := dhConfig.(type) {
				case *MessagesDhConfigObj:
					prime = config.P
				case *MessagesDhConfigNotModified:
					return fmt.Errorf("DH config not modified, need cached parameters")
				default:
					return fmt.Errorf("unexpected DH config type: %T", dhConfig)
				}

				primeInt := new(big.Int).SetBytes(prime)
				if !e2e.IsValidGAOrGB(gBInt, primeInt) {
					// Invalid g_b, discard the chat
					c.Log.Error("received invalid g_b from responder, discarding chat")
					_, _ = c.MessagesDiscardEncryption(true, chat.ID)
					c.secretChats.RemoveSecretChat(chat.ID)
					return fmt.Errorf("received invalid g_b from responder")
				}

				// Now safe to complete key exchange with fingerprint verification
				if err := c.secretChats.CompleteKeyExchange(
					chat.ID,
					chat.GAOrB, // This is g_b
					chat.KeyFingerprint,
				); err != nil {
					// Fingerprint mismatch or other error, discard the chat
					c.Log.Error("failed to complete key exchange (fingerprint mismatch?), discarding chat:", err)
					_, _ = c.MessagesDiscardEncryption(true, chat.ID)
					c.secretChats.RemoveSecretChat(chat.ID)
					return fmt.Errorf("failed to complete key exchange: %w", err)
				}

				fmt.Println("Secret chat established with ID:", chat.ID)

				// Update the existing chat with server metadata
				existingChat.AccessHash = chat.AccessHash

				// Send layer notification
				err = c.SendSecretChatLayerNotification(chat.ID)
				if err != nil {
					c.Log.Error("failed to send layer notification:", err)
				}
				err = c.SendSecretMessage(chat.ID, "Secret chat established!", 0)
				if err != nil {
					c.Log.Error("failed to send secret message:", err)
				}

				fmt.Println("Secret chat established with ID:", chat.ID)
			} else {
				// Chat doesn't exist - this shouldn't happen
				// The chat should have been created in either RequestSecretChat or AcceptSecretChat
				return fmt.Errorf("received encryptedChat but no local chat found (ID: %d)", chat.ID)
			}

		case *EncryptedChatDiscarded:
			// Secret chat was discarded
			c.secretChats.Close(chat.ID)
			c.Log.Info("secret chat discarded:", chat.ID)

		default:
			return fmt.Errorf("unknown encrypted chat type: %T", chat)
		}
	}

	return nil
}

// DecryptSecretMessage decrypts an incoming secret chat message
func (c *Client) DecryptSecretMessage(chatID int32, encryptedData []byte) (*e2e.DecryptedMessageLayer, error) {
	if len(encryptedData) < 24 {
		return nil, fmt.Errorf("encrypted data too short")
	}

	chat, err := c.secretChats.GetSecretChat(chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret chat: %w", err)
	}

	// Extract fingerprint, msg_key, and encrypted data
	fingerprint := int64(0)
	for i := 0; i < 8; i++ {
		fingerprint |= int64(encryptedData[i]) << (i * 8)
	}

	// Verify fingerprint
	if fingerprint != chat.KeyFingerprint {
		return nil, fmt.Errorf("key fingerprint mismatch")
	}

	msgKey := encryptedData[8:24]
	encrypted := encryptedData[24:]

	// Decrypt message
	plaintext, err := chat.DecryptMessage(msgKey, encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt message: %w", err)
	} // Deserialize message
	layer, err := e2e.DeserializeDecryptedMessage(plaintext)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize message: %w", err)
	}

	// Update layer if higher
	if layer.Layer > chat.Layer {
		chat.UpdateLayer(layer.Layer)
	}

	return layer, nil
}

// SecretFileOptions options for sending encrypted files in secret chats
type SecretFileOptions struct {
	Caption    string                  // File caption
	TTL        int32                   // Time-to-live in seconds
	MimeType   string                  // File MIME type
	Thumb      []byte                  // Thumbnail data
	ThumbW     int32                   // Thumbnail width
	ThumbH     int32                   // Thumbnail height
	Attributes []e2e.DocumentAttribute // File attributes
	Key        []byte                  // Optional: custom encryption key (32 bytes)
	IV         []byte                  // Optional: custom encryption IV (32 bytes)
	PartSize   int                     // Upload chunk size (default: 512KB)
}

// SendSecretFile sends an encrypted file to a secret chat
// The file is encrypted with a one-time key that is sent in the message body
func (c *Client) SendSecretFile(chatID int32, filePath string, opts *SecretFileOptions) error {
	if opts == nil {
		opts = &SecretFileOptions{}
	}

	if opts.MimeType == "" {
		opts.MimeType = MimeTypes.match(filePath)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	fileName := GetFileName(filePath)
	return c.SendSecretFileData(chatID, data, fileName, opts)
}

// SendSecretFileData sends encrypted file data to a secret chat
func (c *Client) SendSecretFileData(chatID int32, fileData []byte, fileName string, opts *SecretFileOptions) error {
	if opts == nil {
		opts = &SecretFileOptions{}
	}

	chat, err := c.secretChats.GetSecretChat(chatID)
	if err != nil {
		return fmt.Errorf("failed to get secret chat: %w", err)
	}

	// Use provided key/IV or generate new ones
	var fileKey *e2e.EncryptedFileKey
	if len(opts.Key) == 32 && len(opts.IV) == 32 {
		// Custom key provided
		fileKey = &e2e.EncryptedFileKey{
			Key: opts.Key,
			IV:  opts.IV,
		}
		// Compute fingerprint
		if !e2e.VerifyFileFingerprint(fileKey.Key, fileKey.IV, 0) {
			// Just compute it
			h := md5.New()
			h.Write(fileKey.Key)
			h.Write(fileKey.IV)
			digest := h.Sum(nil)
			fp := binary.LittleEndian.Uint32(digest[0:4]) ^ binary.LittleEndian.Uint32(digest[4:8])
			fileKey.Fingerprint = int32(fp)
		}
	} else {
		// Generate new key
		fileKey, err = e2e.GenerateFileEncryptionKey()
		if err != nil {
			return fmt.Errorf("failed to generate file key: %w", err)
		}
	}

	encryptedData, err := e2e.EncryptFile(fileData, fileKey.Key, fileKey.IV)
	if err != nil {
		return fmt.Errorf("failed to encrypt file: %w", err)
	}

	inputFile, err := c.uploadEncryptedFile(encryptedData, opts.PartSize)
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}

	randomID, err := e2e.GenerateRandomID()
	if err != nil {
		return fmt.Errorf("failed to generate random ID: %w", err)
	}

	attributes := opts.Attributes
	if attributes == nil {
		attributes = []e2e.DocumentAttribute{
			&e2e.DocumentAttributeFilename{
				FileName: fileName,
			},
		}
	}

	// Create decrypted message with file media
	msg := &e2e.DecryptedMessageObj{
		RandomID: randomID,
		TTL:      opts.TTL,
		Message:  opts.Caption,
		Media: &e2e.DecryptedMessageMediaDocument{
			Thumb:      opts.Thumb,
			ThumbW:     opts.ThumbW,
			ThumbH:     opts.ThumbH,
			MimeType:   opts.MimeType,
			Size:       int64(len(fileData)),
			Key:        fileKey.Key,
			Iv:         fileKey.IV,
			Attributes: attributes,
			Caption:    opts.Caption,
		},
	}

	inSeqNo := chat.InSeqNo
	outSeqNo := chat.GetNextSeqNo()

	serialized, err := e2e.SerializeDecryptedMessage(msg, chat.Layer, inSeqNo, outSeqNo)
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	msgKey, encryptedMsg, err := chat.EncryptMessage(serialized)
	if err != nil {
		return fmt.Errorf("failed to encrypt message: %w", err)
	}

	data := make([]byte, 8+16+len(encryptedMsg))
	for i := range 8 {
		data[i] = byte(chat.KeyFingerprint >> (i * 8))
	}
	copy(data[8:], msgKey)
	copy(data[24:], encryptedMsg)

	// Send encrypted file
	_, err = c.MessagesSendEncryptedFile(&MessagesSendEncryptedFileParams{
		Silent: false,
		Peer: &InputEncryptedChat{
			ChatID:     chat.ID,
			AccessHash: chat.AccessHash,
		},
		RandomID: randomID,
		Data:     data,
		File:     inputFile,
	})

	return err
}

func (c *Client) uploadEncryptedFile(data []byte, partSize int) (InputEncryptedFile, error) {
	if partSize <= 0 {
		partSize = 512 * 1024 // Default 512KB
	}

	fileID := GenerateRandomLong()
	totalParts := (len(data) + partSize - 1) / partSize
	isBigFile := len(data) > 10*1024*1024 // Files > 10MB use big file upload

	for i := range totalParts {
		start := i * partSize
		end := min(start+partSize, len(data))
		part := data[start:end]

		if isBigFile {
			_, err := c.UploadSaveBigFilePart(
				fileID,
				int32(i),
				int32(totalParts),
				part,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to upload big part %d: %w", i, err)
			}
		} else {
			_, err := c.UploadSaveFilePart(fileID, int32(i), part)
			if err != nil {
				return nil, fmt.Errorf("failed to upload part %d: %w", i, err)
			}
		}
	}

	if isBigFile {
		return &InputEncryptedFileBigUploaded{
			ID:             fileID,
			Parts:          int32(totalParts),
			KeyFingerprint: 0,
		}, nil
	}

	return &InputEncryptedFileUploaded{
		ID:             fileID,
		Parts:          int32(totalParts),
		Md5Checksum:    "",
		KeyFingerprint: 0,
	}, nil
}

// DecryptSecretFile decrypts an encrypted file received in a secret chat
func (c *Client) DecryptSecretFile(inputFile InputEncryptedFile, key, iv []byte, originalSize int) ([]byte, error) {
	var encryptedData []byte

	switch file := inputFile.(type) {
	case *InputEncryptedFileObj:
		media := &InputEncryptedFileLocation{
			ID:         file.ID,
			AccessHash: file.AccessHash,
		}

		// Download file in chunks
		const chunkSize = 512 * 1024
		offset := 0

	downloadLoop:
		for {
			chunk, err := c.UploadGetFile(&UploadGetFileParams{
				Location: media,
				Offset:   int64(offset),
				Limit:    chunkSize,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to download chunk at offset %d: %w", offset, err)
			}

			switch chunkData := chunk.(type) {
			case *UploadFileObj:
				chunk := chunkData.Bytes
				if len(chunk) == 0 {
					break downloadLoop
				}

				encryptedData = append(encryptedData, chunk...)
				offset += len(chunk)

				if len(chunk) < chunkSize {
					break downloadLoop
				}
			default:
				return nil, fmt.Errorf("unexpected chunk type: %T", chunk)
			}
		}

	default:
		return nil, fmt.Errorf("unsupported encrypted file type: %T", inputFile)
	}

	// Decrypt file data
	return e2e.DecryptFile(encryptedData, key, iv, originalSize)
}
