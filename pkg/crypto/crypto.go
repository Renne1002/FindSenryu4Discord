package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"
	"sync"

	"github.com/cockroachdb/errors"
)

var (
	mu      sync.Mutex
	gcm     cipher.AEAD
	enabled bool

	ErrNotInitialized = errors.New("crypto: not initialized")
	ErrInvalidKey     = errors.New("crypto: key must be 64 hex characters (32 bytes)")
	ErrDecryptFailed  = errors.New("crypto: decryption failed")
)

// Init initializes the encryption module with a hex-encoded 32-byte key.
// If hexKey is empty, encryption is disabled.
// Init may be called multiple times to re-initialize with a different key.
// It must be called before any concurrent use of Encrypt/Decrypt.
func Init(hexKey string) error {
	mu.Lock()
	defer mu.Unlock()

	// Reset state regardless of outcome
	gcm = nil
	enabled = false

	if hexKey == "" {
		return nil
	}

	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return ErrInvalidKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return errors.Wrap(err, "crypto: failed to create cipher")
	}

	g, err := cipher.NewGCM(block)
	if err != nil {
		return errors.Wrap(err, "crypto: failed to create GCM")
	}

	gcm = g
	enabled = true
	return nil
}

// IsEnabled returns true if encryption is initialized and enabled.
func IsEnabled() bool {
	return enabled
}

// Encrypt encrypts plaintext using AES-256-GCM and returns a base64-encoded string.
// The nonce is prepended to the ciphertext before encoding.
func Encrypt(plaintext string) (string, error) {
	if !enabled {
		return plaintext, nil
	}
	if gcm == nil {
		return "", ErrNotInitialized
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", errors.Wrap(err, "crypto: failed to generate nonce")
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decodes a base64-encoded string and decrypts it using AES-256-GCM.
func Decrypt(encoded string) (string, error) {
	if !enabled {
		return encoded, nil
	}
	if gcm == nil {
		return "", ErrNotInitialized
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.Wrap(ErrDecryptFailed, "invalid base64")
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.Wrap(ErrDecryptFailed, "ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.Wrap(ErrDecryptFailed, err.Error())
	}

	return string(plaintext), nil
}

// IsEncrypted attempts to detect whether a string is an encrypted value.
// It tries base64 decoding and AES-GCM decryption; returns true only if both succeed.
func IsEncrypted(s string) bool {
	if !enabled || gcm == nil {
		return false
	}

	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return false
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return false
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	_, err = gcm.Open(nil, nonce, ciphertext, nil)
	return err == nil
}
