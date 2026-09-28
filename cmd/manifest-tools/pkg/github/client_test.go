package github

import (
	"net/http"
	"testing"
)

func TestNewClient_RequiresToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	_, err := NewClient()
	if err == nil {
		t.Fatal("expected error when GITHUB_TOKEN is empty")
	}
}

func TestNewClient_WithToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-123")

	c, err := NewClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.token != "test-token-123" {
		t.Errorf("token = %q, want %q", c.token, "test-token-123")
	}
}

func TestSetHeaders(t *testing.T) {
	client := &Client{httpClient: http.DefaultClient, token: "test-token"}

	req, _ := http.NewRequest("GET", "https://example.com", nil)
	client.setHeaders(req)

	if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer test-token")
	}
	if got := req.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
		t.Errorf("X-GitHub-Api-Version = %q, want %q", got, "2022-11-28")
	}
}
