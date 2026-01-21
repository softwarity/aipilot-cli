package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// initEncryption derives AES-256-GCM key from token
func (d *Daemon) initEncryption() error {
	// Derive 32-byte key from token using SHA256
	hash := sha256.Sum256([]byte(d.token))

	block, err := aes.NewCipher(hash[:])
	if err != nil {
		return fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to create GCM: %w", err)
	}

	d.aesGCM = gcm
	return nil
}

// encrypt encrypts data using AES-GCM
// Returns base64(nonce || ciphertext)
func (d *Daemon) encrypt(plaintext []byte) (string, error) {
	if d.aesGCM == nil {
		return "", fmt.Errorf("encryption not initialized")
	}

	// Generate random nonce
	nonce := make([]byte, d.aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and append to nonce
	ciphertext := d.aesGCM.Seal(nonce, nonce, plaintext, nil)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts base64(nonce || ciphertext) using AES-GCM
func (d *Daemon) decrypt(encoded string) ([]byte, error) {
	if d.aesGCM == nil {
		return nil, fmt.Errorf("encryption not initialized")
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	nonceSize := d.aesGCM.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := d.aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}
