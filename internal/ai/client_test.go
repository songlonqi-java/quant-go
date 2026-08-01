package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompleteUsesCompatibleEndpointAndConfiguration(t *testing.T) {
	var got completionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization not configured")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"  answer  "}}]}`))
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL + "/v1/", APIKey: "secret", Model: "deepseek-chat", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := client.Complete(context.Background(), "system", "question")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "answer" || got.Model != "deepseek-chat" || len(got.Messages) != 2 {
		t.Fatalf("unexpected response/request: %q %#v", answer, got)
	}
}

func TestCompleteDoesNotExposeKeyInHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "bad gateway", http.StatusBadGateway) }))
	defer server.Close()
	client, _ := New(Config{BaseURL: server.URL, APIKey: "do-not-leak", Model: "model"})
	_, err := client.Complete(context.Background(), "s", "q")
	if err == nil || contains(err.Error(), "do-not-leak") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
