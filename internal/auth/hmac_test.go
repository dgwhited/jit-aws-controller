package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"
)

// mockNonceStore implements NonceStore for testing.
type mockNonceStore struct {
	mu     sync.Mutex
	nonces map[string]struct{}
}

func newMockNonceStore() *mockNonceStore {
	return &mockNonceStore{nonces: make(map[string]struct{})}
}

func (m *mockNonceStore) StoreNonce(_ context.Context, keyID, nonce string, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := keyID + "|" + nonce
	if _, exists := m.nonces[key]; exists {
		return fmt.Errorf("nonce already exists")
	}
	m.nonces[key] = struct{}{}
	return nil
}

func (m *mockNonceStore) CheckNonce(_ context.Context, keyID, nonce string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := keyID + "|" + nonce
	_, exists := m.nonces[key]
	return exists, nil
}

func TestSignAndValidate(t *testing.T) {
	ctx := context.Background()
	store := newMockNonceStore()
	secret := "test-secret-key-very-long-and-secure-1234567890"
	keyID := "key-1"
	keys := map[string]string{keyID: secret}

	validator := NewHMACValidator(keys, store)

	method := "POST"
	path := "/requests"
	body := []byte(`{"account_id":"123456789012","channel_id":"ch-abc"}`)

	// Sign the payload.
	headers, err := SignPayload(keyID, secret, method, path, body)
	if err != nil {
		t.Fatalf("SignPayload failed: %v", err)
	}

	// Verify headers contain expected keys.
	if headers[HeaderKeyID] != keyID {
		t.Errorf("expected key ID %q, got %q", keyID, headers[HeaderKeyID])
	}
	// Timestamp should be a Unix epoch integer.
	if _, err := strconv.ParseInt(headers[HeaderTimestamp], 10, 64); err != nil {
		t.Errorf("timestamp should be Unix epoch integer, got %q: %v", headers[HeaderTimestamp], err)
	}

	// Validate should succeed.
	err = validator.ValidateRequest(ctx, method, path, headers, body)
	if err != nil {
		t.Fatalf("ValidateRequest failed: %v", err)
	}
}

func TestExpiredTimestamp(t *testing.T) {
	ctx := context.Background()
	store := newMockNonceStore()
	secret := "test-secret-key-very-long-and-secure-1234567890"
	keyID := "key-1"
	keys := map[string]string{keyID: secret}

	validator := NewHMACValidator(keys, store)

	method := "POST"
	path := "/requests"
	body := []byte(`{"test":"data"}`)

	// Manually construct headers with an old timestamp (10 minutes ago).
	oldTimestamp := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	nonce := "test-nonce-expired"
	signingMessage := buildSigningMessage(oldTimestamp, nonce, method, path, body)
	sig := computeHMAC(secret, signingMessage)

	headers := map[string]string{
		HeaderKeyID:     keyID,
		HeaderTimestamp: oldTimestamp,
		HeaderNonce:     nonce,
		HeaderSignature: sig,
	}

	err := validator.ValidateRequest(ctx, method, path, headers, body)
	if err == nil {
		t.Fatal("expected error for expired timestamp, got nil")
	}
	t.Logf("correctly rejected expired timestamp: %v", err)
}

func TestInvalidSignature(t *testing.T) {
	ctx := context.Background()
	store := newMockNonceStore()
	secret := "test-secret-key-very-long-and-secure-1234567890"
	keyID := "key-1"
	keys := map[string]string{keyID: secret}

	validator := NewHMACValidator(keys, store)

	method := "POST"
	path := "/requests"
	body := []byte(`{"test":"data"}`)

	// Sign with the correct key, then tamper with the signature.
	headers, err := SignPayload(keyID, secret, method, path, body)
	if err != nil {
		t.Fatalf("SignPayload failed: %v", err)
	}

	// Corrupt the signature by replacing it.
	headers[HeaderSignature] = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	err = validator.ValidateRequest(ctx, method, path, headers, body)
	if err == nil {
		t.Fatal("expected error for invalid signature, got nil")
	}
	t.Logf("correctly rejected invalid signature: %v", err)
}

func TestReplayProtection(t *testing.T) {
	ctx := context.Background()
	store := newMockNonceStore()
	secret := "test-secret-key-very-long-and-secure-1234567890"
	keyID := "key-1"
	keys := map[string]string{keyID: secret}

	validator := NewHMACValidator(keys, store)

	method := "POST"
	path := "/requests"
	body := []byte(`{"test":"replay"}`)

	headers, err := SignPayload(keyID, secret, method, path, body)
	if err != nil {
		t.Fatalf("SignPayload failed: %v", err)
	}

	// First request should succeed.
	err = validator.ValidateRequest(ctx, method, path, headers, body)
	if err != nil {
		t.Fatalf("first request should succeed: %v", err)
	}

	// Second request with same headers should fail (nonce replay).
	err = validator.ValidateRequest(ctx, method, path, headers, body)
	if err == nil {
		t.Fatal("expected error for replayed nonce, got nil")
	}
	t.Logf("correctly rejected replay: %v", err)
}

func TestMissingHeaders(t *testing.T) {
	ctx := context.Background()
	store := newMockNonceStore()
	keys := map[string]string{"key-1": "secret"}
	validator := NewHMACValidator(keys, store)

	err := validator.ValidateRequest(ctx, "POST", "/test", map[string]string{}, []byte("body"))
	if err == nil {
		t.Fatal("expected error for missing headers, got nil")
	}
	t.Logf("correctly rejected missing headers: %v", err)
}

func TestKeyRotation(t *testing.T) {
	ctx := context.Background()
	store := newMockNonceStore()
	oldSecret := "old-secret-1234567890"
	newSecret := "new-secret-0987654321"
	keys := map[string]string{
		"key-old": oldSecret,
		"key-new": newSecret,
	}

	validator := NewHMACValidator(keys, store)

	method := "POST"
	path := "/requests"
	body := []byte(`{"test":"rotation"}`)

	// Sign with old key.
	headers, err := SignPayload("key-old", oldSecret, method, path, body)
	if err != nil {
		t.Fatalf("SignPayload failed: %v", err)
	}

	// Validate with validator that has both keys.
	err = validator.ValidateRequest(ctx, method, path, headers, body)
	if err != nil {
		t.Fatalf("should accept old key during rotation: %v", err)
	}

	// Sign with new key.
	headers2, err := SignPayload("key-new", newSecret, method, path, body)
	if err != nil {
		t.Fatalf("SignPayload failed: %v", err)
	}

	err = validator.ValidateRequest(ctx, method, path, headers2, body)
	if err != nil {
		t.Fatalf("should accept new key during rotation: %v", err)
	}
}

// TestCrossCompatibility verifies the backend signing format matches the
// plugin's expected canonical format: timestamp\nnonce\nMETHOD\npath\nbodyHash
func TestCrossCompatibility(t *testing.T) {
	secret := "shared-secret"
	timestamp := "1700000000"
	nonce := "test-nonce-123"
	method := "POST"
	path := "/requests"
	body := []byte(`{"test":"cross"}`)

	// Build the signing message the way the backend does it.
	backendMsg := buildSigningMessage(timestamp, nonce, method, path, body)

	// Build the signing message the way the plugin does it
	// (newline-delimited, uppercased method).
	bodyHash := sha256.Sum256(body)
	bodyHashHex := hex.EncodeToString(bodyHash[:])
	pluginMsg := timestamp + "\n" + nonce + "\n" + "POST" + "\n" + path + "\n" + bodyHashHex

	if backendMsg != pluginMsg {
		t.Errorf("signing message mismatch:\nbackend: %q\nplugin:  %q", backendMsg, pluginMsg)
	}

	// Verify HMAC output is hex-encoded.
	sig := computeHMAC(secret, backendMsg)
	if len(sig) != 64 {
		t.Errorf("expected 64-char hex signature, got %d chars: %q", len(sig), sig)
	}
}
