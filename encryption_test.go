package main

import (
	"testing"
)

func setupTestDaemon(token string) *Daemon {
	d := &Daemon{token: token}
	d.initEncryption()
	return d
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	d := setupTestDaemon("test-token-32-chars-long-enough!")

	plaintext := []byte("Hello, mobile!")
	encrypted, err := d.encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := d.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("roundtrip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	d1 := setupTestDaemon("token-one")
	d2 := setupTestDaemon("token-two")

	encrypted, err := d1.encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	_, err = d2.decrypt(encrypted)
	if err == nil {
		t.Error("expected decryption to fail with wrong key")
	}
}

func TestEncryptNotInitialized(t *testing.T) {
	d := &Daemon{}
	_, err := d.encrypt([]byte("data"))
	if err == nil {
		t.Error("expected error when encryption not initialized")
	}
}

func TestDecryptNotInitialized(t *testing.T) {
	d := &Daemon{}
	_, err := d.decrypt("dGVzdA==")
	if err == nil {
		t.Error("expected error when encryption not initialized")
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	d := setupTestDaemon("test-token")
	plaintext := []byte("same data")

	e1, err := d.encrypt(plaintext)
	if err != nil {
		t.Fatalf("first encrypt failed: %v", err)
	}

	e2, err := d.encrypt(plaintext)
	if err != nil {
		t.Fatalf("second encrypt failed: %v", err)
	}

	if e1 == e2 {
		t.Error("expected different ciphertexts due to random nonce")
	}
}

func TestEncryptDecryptEmptyData(t *testing.T) {
	d := setupTestDaemon("test-token")

	encrypted, err := d.encrypt([]byte{})
	if err != nil {
		t.Fatalf("encrypt empty failed: %v", err)
	}

	decrypted, err := d.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt empty failed: %v", err)
	}

	if len(decrypted) != 0 {
		t.Errorf("expected empty result, got %d bytes", len(decrypted))
	}
}

func TestEncryptDecryptLargeData(t *testing.T) {
	d := setupTestDaemon("test-token")

	plaintext := make([]byte, 1024*1024) // 1MB
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	encrypted, err := d.encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt large failed: %v", err)
	}

	decrypted, err := d.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt large failed: %v", err)
	}

	if len(decrypted) != len(plaintext) {
		t.Fatalf("size mismatch: got %d, want %d", len(decrypted), len(plaintext))
	}
	for i := range plaintext {
		if decrypted[i] != plaintext[i] {
			t.Fatalf("data mismatch at byte %d", i)
		}
	}
}
