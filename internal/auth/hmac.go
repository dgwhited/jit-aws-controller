package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// maxTimestampSkew is the maximum age of a request timestamp before rejection.
	maxTimestampSkew = 5 * time.Minute

	// HeaderKeyID is the header carrying the signing key identifier.
	HeaderKeyID = "X-JIT-KeyID"
	// HeaderTimestamp is the header carrying the Unix epoch request timestamp.
	HeaderTimestamp = "X-JIT-Timestamp"
	// HeaderNonce is the header carrying the unique request nonce.
	HeaderNonce = "X-JIT-Nonce"
	// HeaderSignature is the header carrying the HMAC-SHA256 hex-encoded signature.
	HeaderSignature = "X-JIT-Signature"
)

// NonceStore abstracts nonce persistence for replay protection.
type NonceStore interface {
	// StoreNonce persists a nonce with a TTL. Returns error if already exists.
	StoreNonce(ctx context.Context, keyID, nonce string, ttlSeconds int64) error
	// CheckNonce returns true if the nonce already exists for the given key.
	CheckNonce(ctx context.Context, keyID, nonce string) (bool, error)
}

// HMACValidator validates inbound HMAC-signed requests and signs outbound payloads.
type HMACValidator struct {
	// SigningKeys maps key IDs to their secret values. Supports rotation by
	// containing both current and previous keys simultaneously.
	SigningKeys map[string]string
	NonceStore  NonceStore
}

// NewHMACValidator creates a validator with the provided signing keys and nonce store.
func NewHMACValidator(signingKeys map[string]string, store NonceStore) *HMACValidator {
	return &HMACValidator{
		SigningKeys: signingKeys,
		NonceStore:  store,
	}
}

// ValidateRequest verifies the HMAC signature on an inbound request.
// It checks the timestamp freshness, nonce uniqueness, and signature validity.
func (v *HMACValidator) ValidateRequest(ctx context.Context, method, path string, headers map[string]string, body []byte) error {
	keyID := headerValue(headers, HeaderKeyID)
	timestamp := headerValue(headers, HeaderTimestamp)
	nonce := headerValue(headers, HeaderNonce)
	signature := headerValue(headers, HeaderSignature)

	if keyID == "" || timestamp == "" || nonce == "" || signature == "" {
		return fmt.Errorf("missing required HMAC headers")
	}

	// Validate timestamp freshness (Unix epoch seconds).
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}
	skew := time.Since(time.Unix(ts, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > maxTimestampSkew {
		return fmt.Errorf("timestamp outside allowed skew: %v", skew)
	}

	// Check nonce for replay.
	exists, err := v.NonceStore.CheckNonce(ctx, keyID, nonce)
	if err != nil {
		return fmt.Errorf("nonce check failed: %w", err)
	}
	if exists {
		return fmt.Errorf("nonce already used")
	}

	// Compute expected signature and try all keys matching the key ID.
	// During rotation, the caller might present a key ID that maps to either
	// the current or previous secret.
	signingMessage := buildSigningMessage(timestamp, nonce, method, path, body)

	matched := false
	for kid, secret := range v.SigningKeys {
		if kid != keyID {
			continue
		}
		expected := computeHMAC(secret, signingMessage)
		if hmac.Equal([]byte(expected), []byte(signature)) {
			matched = true
			break
		}
	}

	// If key ID didn't match directly, try all keys (rotation support).
	if !matched {
		for _, secret := range v.SigningKeys {
			expected := computeHMAC(secret, signingMessage)
			if hmac.Equal([]byte(expected), []byte(signature)) {
				matched = true
				break
			}
		}
	}

	if !matched {
		return fmt.Errorf("invalid signature")
	}

	// Store nonce to prevent replay. TTL slightly longer than skew window.
	ttl := int64(math.Ceil(maxTimestampSkew.Seconds() * 2))
	if err := v.NonceStore.StoreNonce(ctx, keyID, nonce, ttl); err != nil {
		return fmt.Errorf("failed to store nonce: %w", err)
	}

	return nil
}

// SignPayload generates HMAC headers for an outbound request.
func SignPayload(keyID, secret string, method, path string, body []byte) (map[string]string, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := uuid.New().String()

	signingMessage := buildSigningMessage(timestamp, nonce, method, path, body)
	sig := computeHMAC(secret, signingMessage)

	headers := map[string]string{
		HeaderKeyID:     keyID,
		HeaderTimestamp: timestamp,
		HeaderNonce:     nonce,
		HeaderSignature: sig,
	}
	return headers, nil
}

// buildSigningMessage constructs the canonical message to be signed.
// Format: timestamp\nnonce\nMETHOD\npath\nhex(sha256(body))
// This matches the plugin's canonical format for interoperability.
func buildSigningMessage(timestamp, nonce, method, path string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	bodyHashHex := hex.EncodeToString(bodyHash[:])
	return strings.Join([]string{
		timestamp,
		nonce,
		strings.ToUpper(method),
		path,
		bodyHashHex,
	}, "\n")
}

// computeHMAC computes an HMAC-SHA256 and returns the hex-encoded string.
func computeHMAC(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// headerValue performs a case-insensitive header lookup.
func headerValue(headers map[string]string, key string) string {
	if v, ok := headers[key]; ok {
		return v
	}
	lower := strings.ToLower(key)
	for k, v := range headers {
		if strings.ToLower(k) == lower {
			return v
		}
	}
	return ""
}
