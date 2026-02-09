package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/dgwhited/jit-aws-controller/internal/auth"
	"github.com/dgwhited/jit-aws-controller/internal/models"
)

// Client sends signed webhook notifications to the plugin.
type Client struct {
	webhookURL string
	keyID      string
	secret     string
	httpClient *http.Client
}

// NewClient creates a new webhook client.
func NewClient(webhookURL, keyID, secret string) *Client {
	return &Client{
		webhookURL: webhookURL,
		keyID:      keyID,
		secret:     secret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// retryBackoffs for webhook delivery attempts.
var retryBackoffs = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
}

// Notify sends a webhook payload to the plugin with HMAC signing and retry.
func (c *Client) Notify(ctx context.Context, payload models.WebhookPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook marshal: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= len(retryBackoffs); attempt++ {
		if attempt > 0 {
			slog.Warn("retrying webhook notification",
				"attempt", attempt,
				"request_id", payload.RequestID,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryBackoffs[attempt-1]):
			}
		}

		err := c.send(ctx, body)
		if err == nil {
			slog.Info("webhook notification sent",
				"request_id", payload.RequestID,
				"status", payload.Status,
			)
			return nil
		}
		lastErr = err
		slog.Error("webhook send failed",
			"attempt", attempt,
			"error", err,
		)
	}
	return fmt.Errorf("webhook notify failed after retries: %w", lastErr)
}

func (c *Client) send(ctx context.Context, body []byte) error {
	method := "POST"
	path := "/jit/webhook"

	// Sign the payload.
	hmacHeaders, err := auth.SignPayload(c.keyID, c.secret, method, path, body)
	if err != nil {
		return fmt.Errorf("sign webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hmacHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook HTTP error: %w", err)
	}
	defer resp.Body.Close()

	// Read and discard body to allow connection reuse.
	_, _ = io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
