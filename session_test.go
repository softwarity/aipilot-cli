package main

import "testing"

func TestGenerateRandomToken(t *testing.T) {
	token := generateRandomToken()
	if len(token) != 32 {
		t.Fatalf("expected 32 hex chars, got %d", len(token))
	}
	// Ensure two tokens are different
	token2 := generateRandomToken()
	if token == token2 {
		t.Fatal("two generated tokens should not be identical")
	}
}
