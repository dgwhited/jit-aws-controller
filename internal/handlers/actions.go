package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/dgwhited/jit-aws-controller/internal/models"
)

// StepFunctionActionPayload represents the payload sent by Step Functions to Lambda.
type StepFunctionActionPayload struct {
	Action              string          `json:"action"`
	RequestID           string          `json:"request_id"`
	AccountID           string          `json:"account_id"`
	ChannelID           string          `json:"channel_id"`
	IdentityStoreUserID string          `json:"identity_store_user_id"`
	RequesterEmail      string          `json:"requester_email"`
	DurationSeconds     int             `json:"duration_seconds"`
	Error               json.RawMessage `json:"error,omitempty"`
}

// ActionResult is the response returned to Step Functions from each action.
type ActionResult struct {
	Status    string `json:"status"`
	RequestID string `json:"request_id"`
	Message   string `json:"message,omitempty"`
}

// ActionHandler processes Step Functions action payloads.
type ActionHandler struct {
	Handler *Handler
}

// NewActionHandler creates a new action handler.
func NewActionHandler(handler *Handler) *ActionHandler {
	return &ActionHandler{Handler: handler}
}

// Handle dispatches to the appropriate action based on the payload.
func (a *ActionHandler) Handle(ctx context.Context, raw json.RawMessage) (*ActionResult, error) {
	var payload StepFunctionActionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal action payload: %w", err)
	}

	slog.Info("handling step function action",
		"action", payload.Action,
		"request_id", payload.RequestID,
	)

	switch payload.Action {
	case "validate":
		return a.handleValidate(ctx, payload)
	case "grant":
		return a.handleGrant(ctx, payload)
	case "notify_granted":
		return a.handleNotifyGranted(ctx, payload)
	case "revoke":
		return a.handleRevoke(ctx, payload)
	case "notify_revoked":
		return a.handleNotifyRevoked(ctx, payload)
	case "handle_grant_error":
		return a.handleGrantError(ctx, payload)
	case "handle_revoke_error":
		return a.handleRevokeError(ctx, payload)
	default:
		return nil, fmt.Errorf("unknown action: %s", payload.Action)
	}
}

// handleValidate verifies the request is still in APPROVED status and ready for granting.
func (a *ActionHandler) handleValidate(ctx context.Context, p StepFunctionActionPayload) (*ActionResult, error) {
	req, err := a.Handler.DB.GetRequest(ctx, p.RequestID)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("request %s not found", p.RequestID)
	}
	if req.Status != models.StatusApproved {
		return nil, fmt.Errorf("request %s is in status %s, expected APPROVED", p.RequestID, req.Status)
	}

	slog.Info("request validated for granting",
		"request_id", p.RequestID,
		"account_id", req.AccountID,
	)
	return &ActionResult{Status: "validated", RequestID: p.RequestID}, nil
}

// handleGrant creates the IAM Identity Center account assignment.
func (a *ActionHandler) handleGrant(ctx context.Context, p StepFunctionActionPayload) (*ActionResult, error) {
	req, err := a.Handler.DB.GetRequest(ctx, p.RequestID)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("request %s not found", p.RequestID)
	}

	// Grant IAM Identity Center access.
	if err := a.Handler.Identity.GrantAccess(ctx, req.AccountID, req.IdentityStoreUserID); err != nil {
		return nil, fmt.Errorf("grant access: %w", err)
	}

	// Update status to GRANTED.
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"status":     models.StatusGranted,
		"grant_time": now.Format(time.RFC3339),
	}
	if err := a.Handler.DB.ConditionalUpdateStatus(ctx, p.RequestID, models.StatusApproved, updates); err != nil {
		return nil, fmt.Errorf("update to GRANTED: %w", err)
	}

	// Audit the grant.
	_ = a.Handler.Audit.Log(ctx, p.RequestID, models.EventGranted, req.AccountID, req.ChannelID,
		"", "system", nil)

	slog.Info("access granted via step function",
		"request_id", p.RequestID,
		"account_id", req.AccountID,
		"requester", req.RequesterEmail,
	)
	return &ActionResult{Status: "granted", RequestID: p.RequestID}, nil
}

// handleNotifyGranted sends a webhook notification that access has been granted.
func (a *ActionHandler) handleNotifyGranted(ctx context.Context, p StepFunctionActionPayload) (*ActionResult, error) {
	req, err := a.Handler.DB.GetRequest(ctx, p.RequestID)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("request %s not found", p.RequestID)
	}

	_ = a.Handler.Webhook.Notify(ctx, models.WebhookPayload{
		RequestID: req.RequestID,
		Status:    models.StatusGranted,
		AccountID: req.AccountID,
		ChannelID: req.ChannelID,
		Actor:     "system",
		Details: map[string]string{
			"requester_email":  req.RequesterEmail,
			"duration_minutes": fmt.Sprintf("%d", req.RequestedDurationMinutes),
		},
	})

	slog.Info("grant notification sent",
		"request_id", p.RequestID,
	)
	return &ActionResult{Status: "notified", RequestID: p.RequestID}, nil
}

// handleRevoke deletes the IAM Identity Center account assignment after the wait period.
func (a *ActionHandler) handleRevoke(ctx context.Context, p StepFunctionActionPayload) (*ActionResult, error) {
	req, err := a.Handler.DB.GetRequest(ctx, p.RequestID)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("request %s not found", p.RequestID)
	}

	// Skip if already revoked (e.g., by break-glass /jit revoke).
	if req.Status == models.StatusRevoked || req.Status == models.StatusExpired {
		slog.Info("request already revoked/expired, skipping",
			"request_id", p.RequestID,
			"status", req.Status,
		)
		return &ActionResult{Status: req.Status, RequestID: p.RequestID, Message: "already revoked or expired"}, nil
	}

	// Revoke IAM Identity Center access.
	if err := a.Handler.Identity.RevokeAccess(ctx, req.AccountID, req.IdentityStoreUserID); err != nil {
		return nil, fmt.Errorf("revoke access: %w", err)
	}

	// Update status to EXPIRED (this is an automatic expiration, not a manual revoke).
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"status":     models.StatusExpired,
		"expired_at": now.Format(time.RFC3339),
	}
	if err := a.Handler.DB.ConditionalUpdateStatus(ctx, p.RequestID, models.StatusGranted, updates); err != nil {
		// May have been revoked by break-glass in the meantime — not a fatal error.
		slog.Warn("conditional update to EXPIRED failed, may have been revoked already",
			"request_id", p.RequestID,
			"error", err,
		)
		return &ActionResult{Status: "already_handled", RequestID: p.RequestID, Message: "conditional update failed, likely already revoked"}, nil
	}

	// Audit the expiration.
	_ = a.Handler.Audit.Log(ctx, p.RequestID, models.EventExpired, req.AccountID, req.ChannelID,
		"", "system", nil)

	slog.Info("access revoked via step function",
		"request_id", p.RequestID,
		"account_id", req.AccountID,
	)
	return &ActionResult{Status: "expired", RequestID: p.RequestID}, nil
}

// handleNotifyRevoked sends a webhook notification that access has been revoked/expired.
func (a *ActionHandler) handleNotifyRevoked(ctx context.Context, p StepFunctionActionPayload) (*ActionResult, error) {
	req, err := a.Handler.DB.GetRequest(ctx, p.RequestID)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("request %s not found", p.RequestID)
	}

	_ = a.Handler.Webhook.Notify(ctx, models.WebhookPayload{
		RequestID: req.RequestID,
		Status:    req.Status, // Will be EXPIRED or REVOKED.
		AccountID: req.AccountID,
		ChannelID: req.ChannelID,
		Actor:     "system",
	})

	slog.Info("revoke notification sent",
		"request_id", p.RequestID,
	)
	return &ActionResult{Status: "notified", RequestID: p.RequestID}, nil
}

// handleGrantError marks the request as ERROR when the grant step fails.
func (a *ActionHandler) handleGrantError(ctx context.Context, p StepFunctionActionPayload) (*ActionResult, error) {
	req, err := a.Handler.DB.GetRequest(ctx, p.RequestID)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("request %s not found", p.RequestID)
	}

	errorDetail := "grant step failed"
	if p.Error != nil {
		errorDetail = string(p.Error)
	}

	// Update to ERROR status.
	updates := map[string]interface{}{
		"status":        models.StatusError,
		"error_details": errorDetail,
	}
	// Try from APPROVED (grant may not have updated status yet).
	if err := a.Handler.DB.ConditionalUpdateStatus(ctx, p.RequestID, models.StatusApproved, updates); err != nil {
		slog.Warn("conditional update to ERROR from APPROVED failed, trying from GRANTED",
			"request_id", p.RequestID,
			"error", err,
		)
		// Also try from GRANTED in case the grant partially succeeded.
		_ = a.Handler.DB.ConditionalUpdateStatus(ctx, p.RequestID, models.StatusGranted, updates)
	}

	// Audit the error.
	_ = a.Handler.Audit.Log(ctx, p.RequestID, models.EventError, req.AccountID, req.ChannelID,
		"", "system",
		map[string]string{"error": errorDetail, "phase": "grant"},
	)

	// Notify channel of the failure.
	_ = a.Handler.Webhook.Notify(ctx, models.WebhookPayload{
		RequestID: req.RequestID,
		Status:    models.StatusError,
		AccountID: req.AccountID,
		ChannelID: req.ChannelID,
		Actor:     "system",
		Details:   map[string]string{"error": errorDetail, "phase": "grant"},
	})

	slog.Error("grant error handled",
		"request_id", p.RequestID,
		"error_detail", errorDetail,
	)
	return &ActionResult{Status: "error_handled", RequestID: p.RequestID, Message: errorDetail}, nil
}

// handleRevokeError marks the request as ERROR when the revoke step fails.
func (a *ActionHandler) handleRevokeError(ctx context.Context, p StepFunctionActionPayload) (*ActionResult, error) {
	req, err := a.Handler.DB.GetRequest(ctx, p.RequestID)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("request %s not found", p.RequestID)
	}

	errorDetail := "revoke step failed"
	if p.Error != nil {
		errorDetail = string(p.Error)
	}

	// Update to ERROR status from GRANTED.
	updates := map[string]interface{}{
		"status":        models.StatusError,
		"error_details": errorDetail,
	}
	_ = a.Handler.DB.ConditionalUpdateStatus(ctx, p.RequestID, models.StatusGranted, updates)

	// Audit the error.
	_ = a.Handler.Audit.Log(ctx, p.RequestID, models.EventError, req.AccountID, req.ChannelID,
		"", "system",
		map[string]string{"error": errorDetail, "phase": "revoke"},
	)

	// Notify channel of the failure — reconciler will retry.
	_ = a.Handler.Webhook.Notify(ctx, models.WebhookPayload{
		RequestID: req.RequestID,
		Status:    models.StatusError,
		AccountID: req.AccountID,
		ChannelID: req.ChannelID,
		Actor:     "system",
		Details:   map[string]string{"error": errorDetail, "phase": "revoke"},
	})

	slog.Error("revoke error handled",
		"request_id", p.RequestID,
		"error_detail", errorDetail,
	)
	return &ActionResult{Status: "error_handled", RequestID: p.RequestID, Message: errorDetail}, nil
}
