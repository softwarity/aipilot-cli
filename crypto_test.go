package main

import (
	"encoding/hex"
	"testing"
)

func TestGenerateX25519KeyPair(t *testing.T) {
	priv, pub, err := GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair failed: %v", err)
	}

	allZero := true
	for _, b := range priv {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("private key is all zeros")
	}

	allZero = true
	for _, b := range pub {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("public key is all zeros")
	}
}

func TestGenerateX25519KeyPairUniqueness(t *testing.T) {
	_, pub1, _ := GenerateX25519KeyPair()
	_, pub2, _ := GenerateX25519KeyPair()

	if pub1 == pub2 {
		t.Error("expected different key pairs")
	}
}

func TestEncryptForMobile(t *testing.T) {
	pcPriv, _, err := GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("generate PC keypair failed: %v", err)
	}

	_, mobilePub, err := GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("generate mobile keypair failed: %v", err)
	}

	mobilePubHex := hex.EncodeToString(mobilePub[:])

	encrypted, err := EncryptForMobile("test-token", mobilePubHex, pcPriv)
	if err != nil {
		t.Fatalf("EncryptForMobile failed: %v", err)
	}

	if len(encrypted) == 0 {
		t.Error("encrypted output is empty")
	}

	_, err = hex.DecodeString(encrypted)
	if err != nil {
		t.Errorf("encrypted output is not valid hex: %v", err)
	}
}

func TestEncryptForMobileInvalidKey(t *testing.T) {
	pcPriv, _, _ := GenerateX25519KeyPair()

	_, err := EncryptForMobile("test", "not-hex", pcPriv)
	if err == nil {
		t.Error("expected error for invalid hex key")
	}

	_, err = EncryptForMobile("test", "abcd", pcPriv)
	if err == nil {
		t.Error("expected error for wrong-length key")
	}
}

func TestGetPrivateKeyFromHex(t *testing.T) {
	priv, _, _ := GenerateX25519KeyPair()
	hexKey := hex.EncodeToString(priv[:])

	recovered, err := GetPrivateKeyFromHex(hexKey)
	if err != nil {
		t.Fatalf("GetPrivateKeyFromHex failed: %v", err)
	}

	if recovered != priv {
		t.Error("recovered key does not match original")
	}
}

func TestGetPrivateKeyFromHexInvalid(t *testing.T) {
	_, err := GetPrivateKeyFromHex("not-hex")
	if err == nil {
		t.Error("expected error for invalid hex")
	}

	_, err = GetPrivateKeyFromHex("abcd")
	if err == nil {
		t.Error("expected error for wrong-length key")
	}
}
