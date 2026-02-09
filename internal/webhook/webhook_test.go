package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgwhited/jit-aws-controller/internal/models"
)

func TestNotify_Success(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)

		// Verify method and headers.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected JSON content type, got %s", r.Header.Get("Content-Type"))
		}

		// Verify HMAC headers are present.
		if r.Header.Get("X-JIT-KeyID") == "" {
			t.Error("expected X-JIT-KeyID header")
		}
		if r.Header.Get("X-JIT-Signature") == "" {
			t.Error("expected X-JIT-Signature header")
		}

		// Verify body is valid JSON.
		body, _ := io.ReadAll(r.Body)
		var payload models.WebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("invalid JSON body: %v", err)
		}
		if payload.RequestID != "req-1" {
			t.Errorf("expected request_id req-1, got %s", payload.RequestID)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "test-secret")
	err := client.Notify(context.Background(), models.WebhookPayload{
		RequestID: "req-1",
		Status:    "GRANTED",
		AccountID: "acct1",
		ChannelID: "ch1",
		Actor:     "system",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received.Load() != 1 {
		t.Errorf("expected 1 request, got %d", received.Load())
	}
}

func TestNotify_RetryOnFailure(t *testing.T) {
	// Override retry backoffs for fast tests.
	origBackoffs := retryBackoffs
	retryBackoffs = []time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond}
	defer func() { retryBackoffs = origBackoffs }()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "test-secret")
	err := client.Notify(context.Background(), models.WebhookPayload{
		RequestID: "req-1",
		Status:    "GRANTED",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestNotify_AllRetriesFail(t *testing.T) {
	origBackoffs := retryBackoffs
	retryBackoffs = []time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond}
	defer func() { retryBackoffs = origBackoffs }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "test-secret")
	err := client.Notify(context.Background(), models.WebhookPayload{
		RequestID: "req-1",
		Status:    "GRANTED",
	})
	if err == nil {
		t.Fatal("expected error when all retries fail")
	}
}

func TestNotify_ContextCancelled(t *testing.T) {
	origBackoffs := retryBackoffs
	retryBackoffs = []time.Duration{1 * time.Second}
	defer func() { retryBackoffs = origBackoffs }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately to trigger context cancellation on retry.
	cancel()

	client := NewClient(server.URL, "test-key", "test-secret")
	err := client.Notify(ctx, models.WebhookPayload{
		RequestID: "req-1",
		Status:    "GRANTED",
	})
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("http://example.com/webhook", "key1", "secret1")
	if client.webhookURL != "http://example.com/webhook" {
		t.Errorf("unexpected URL: %s", client.webhookURL)
	}
	if client.keyID != "key1" {
		t.Errorf("unexpected key ID: %s", client.keyID)
	}
	if client.httpClient == nil {
		t.Error("expected non-nil HTTP client")
	}
}
