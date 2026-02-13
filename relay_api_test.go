package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(handler http.HandlerFunc) (*RelayClient, *httptest.Server) {
	server := httptest.NewServer(handler)
	client := &RelayClient{
		baseURL:    server.URL,
		httpClient: server.Client(),
		pcConfig:   &PCConfig{PCID: "test-pc"},
	}
	return client, server
}

func TestDeleteSession_Success(t *testing.T) {
	client, server := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Fatalf("expected DELETE, got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/api/sessions/") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-PC-ID") != "test-pc" {
			t.Fatalf("expected X-PC-ID header test-pc, got %s", r.Header.Get("X-PC-ID"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true}`))
	})
	defer server.Close()

	err := client.DeleteSession("session-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteSession_NotFound(t *testing.T) {
	client, server := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	})
	defer server.Close()

	err := client.DeleteSession("session-404")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListAllSessions_Success(t *testing.T) {
	client, server := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if !strings.Contains(r.URL.RawQuery, "for_cli=true") {
			t.Fatalf("expected for_cli=true in query, got %s", r.URL.RawQuery)
		}
		sessions := []SessionInfo{
			{ID: "s1", AgentType: "claude", WorkingDir: "/dir1", Token: "tok1"},
			{ID: "s2", AgentType: "gemini", WorkingDir: "/dir2", Token: "tok2"},
		}
		json.NewEncoder(w).Encode(sessions)
	})
	defer server.Close()

	sessions, err := client.ListAllSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "s1" || sessions[0].Token != "tok1" {
		t.Fatalf("unexpected session[0]: %+v", sessions[0])
	}
	if sessions[1].ID != "s2" || sessions[1].Token != "tok2" {
		t.Fatalf("unexpected session[1]: %+v", sessions[1])
	}
}

func TestAddSessionTokenForMobile_Success(t *testing.T) {
	client, server := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/tokens") {
			t.Fatalf("expected path ending in /tokens, got %s", r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["mobile_id"] != "mob-1" {
			t.Fatalf("expected mobile_id=mob-1, got %s", body["mobile_id"])
		}
		if body["encrypted_token"] != "enc-tok" {
			t.Fatalf("expected encrypted_token=enc-tok, got %s", body["encrypted_token"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true}`))
	})
	defer server.Close()

	err := client.AddSessionTokenForMobile("session-1", "mob-1", "enc-tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPurgeAllSessions_Success(t *testing.T) {
	client, server := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Fatalf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/sessions" {
			t.Fatalf("expected /api/sessions, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"deleted_count":3}`))
	})
	defer server.Close()

	count, err := client.PurgeAllSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected deleted_count=3, got %d", count)
	}
}
