package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

const (
	X25519PublicKeySize = 32
	NonceSize           = 24
)

// GenerateX25519KeyPair generates a new X25519 key pair
func GenerateX25519KeyPair() (privateKey, publicKey [32]byte, err error) {
	// Generate random private key
	if _, err := io.ReadFull(rand.Reader, privateKey[:]); err != nil {
		return privateKey, publicKey, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Derive public key from private key
	curve25519.ScalarBaseMult(&publicKey, &privateKey)
	return privateKey, publicKey, nil
}

// EncryptForMobile encrypts a message for a mobile device using NaCl box
// Returns hex-encoded: nonce (24 bytes) + ciphertext
func EncryptForMobile(message string, mobilePublicKeyHex string, pcPrivateKey [32]byte) (string, error) {
	// Decode mobile's public key
	mobilePublicKeyBytes, err := hex.DecodeString(mobilePublicKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid mobile public key: %w", err)
	}
	if len(mobilePublicKeyBytes) != X25519PublicKeySize {
		return "", fmt.Errorf("mobile public key wrong size: got %d, want %d", len(mobilePublicKeyBytes), X25519PublicKeySize)
	}

	var mobilePublicKey [32]byte
	copy(mobilePublicKey[:], mobilePublicKeyBytes)

	// Generate random nonce
	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt using NaCl box
	encrypted := box.Seal(nil, []byte(message), &nonce, &mobilePublicKey, &pcPrivateKey)

	// Combine nonce + ciphertext
	result := make([]byte, NonceSize+len(encrypted))
	copy(result[:NonceSize], nonce[:])
	copy(result[NonceSize:], encrypted)

	return hex.EncodeToString(result), nil
}

// GetPrivateKeyFromHex converts a hex-encoded private key to [32]byte
func GetPrivateKeyFromHex(hexKey string) ([32]byte, error) {
	var key [32]byte
	bytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return key, fmt.Errorf("invalid hex key: %w", err)
	}
	if len(bytes) != 32 {
		return key, fmt.Errorf("key wrong size: got %d, want 32", len(bytes))
	}
	copy(key[:], bytes)
	return key, nil
}
