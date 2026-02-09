package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/dgwhited/jit-aws-controller/internal/models"
)

func newTestActionHandler() (*ActionHandler, *mockDB, *mockIdentity, *mockWebhook, *mockAudit) {
	h, db, id, wh, au, _ := newTestHandler()
	return NewActionHandler(h), db, id, wh, au
}

func marshalPayload(t *testing.T, p StepFunctionActionPayload) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	return b
}

// ---------------------------------------------------------------------------
// Handle dispatch tests
// ---------------------------------------------------------------------------

func TestActionHandle_UnknownAction(t *testing.T) {
	ah, _, _, _, _ := newTestActionHandler()

	raw := marshalPayload(t, StepFunctionActionPayload{Action: "bogus"})
	_, err := ah.Handle(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestActionHandle_InvalidJSON(t *testing.T) {
	ah, _, _, _, _ := newTestActionHandler()

	_, err := ah.Handle(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// handleValidate tests
// ---------------------------------------------------------------------------

func TestHandleValidate_Success(t *testing.T) {
	ah, db, _, _, _ := newTestActionHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID: "req-1",
		AccountID: "acct1",
		Status:    models.StatusApproved,
	}

	raw := marshalPayload(t, StepFunctionActionPayload{
		Action:    "validate",
		RequestID: "req-1",
	})

	result, err := ah.Handle(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "validated" {
		t.Errorf("expected validated, got %s", result.Status)
	}
}

func TestHandleValidate_WrongStatus(t *testing.T) {
	ah, db, _, _, _ := newTestActionHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID: "req-1",
		Status:    models.StatusPending,
	}

	raw := marshalPayload(t, StepFunctionActionPayload{
		Action:    "validate",
		RequestID: "req-1",
	})

	_, err := ah.Handle(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error for non-APPROVED status")
	}
}

func TestHandleValidate_NotFound(t *testing.T) {
	ah, _, _, _, _ := newTestActionHandler()

	raw := marshalPayload(t, StepFunctionActionPayload{
		Action:    "validate",
		RequestID: "nonexistent",
	})

	_, err := ah.Handle(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error for missing request")
	}
}

// ---------------------------------------------------------------------------
// handleGrant tests
// ---------------------------------------------------------------------------

func TestHandleGrant_Success(t *testing.T) {
	ah, db, _, _, au := newTestActionHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID:           "req-1",
		AccountID:           "acct1",
		ChannelID:           "ch1",
		IdentityStoreUserID: "uid-123",
		Status:              models.StatusApproved,
	}

	raw := marshalPayload(t, StepFunctionActionPayload{
		Action:    "grant",
		RequestID: "req-1",
	})

	result, err := ah.Handle(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "granted" {
		t.Errorf("expected granted, got %s", result.Status)
	}
	if db.requests["req-1"].Status != models.StatusGranted {
		t.Errorf("expected GRANTED status in DB, got %s", db.requests["req-1"].Status)
	}
	if len(au.events) != 1 || au.events[0].eventType != models.EventGranted {
		t.Errorf("expected GRANTED audit event")
	}
}

func TestHandleGrant_IdentityError(t *testing.T) {
	ah, db, id, _, _ := newTestActionHandler()
	id.grantErr = fmt.Errorf("SSO error")
	db.requests["req-1"] = &models.JitRequest{
		RequestID:           "req-1",
		AccountID:           "acct1",
		ChannelID:           "ch1",
		IdentityStoreUserID: "uid-123",
		Status:              models.StatusApproved,
	}

	raw := marshalPayload(t, StepFunctionActionPayload{
		Action:    "grant",
		RequestID: "req-1",
	})

	_, err := ah.Handle(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error when identity grant fails")
	}
}

// ---------------------------------------------------------------------------
// handleNotifyGranted tests
// ---------------------------------------------------------------------------

func TestHandleNotifyGranted_Success(t *testing.T) {
	ah, db, _, wh, _ := newTestActionHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID:                "req-1",
		AccountID:                "acct1",
		ChannelID:                "ch1",
		RequesterEmail:           "user@example.com",
		RequestedDurationMinutes: 60,
		Status:                   models.StatusGranted,
	}

	raw := marshalPayload(t, StepFunctionActionPayload{
		Action:    "notify_granted",
		RequestID: "req-1",
	})

	result, err := ah.Handle(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "notified" {
		t.Errorf("expected notified, got %s", result.Status)
	}
	if len(wh.payloads) != 1 || wh.payloads[0].Status != models.StatusGranted {
		t.Errorf("expected GRANTED webhook notification")
	}
}

// ---------------------------------------------------------------------------
// handleRevoke tests
// ---------------------------------------------------------------------------

func TestHandleRevoke_Success(t *testing.T) {
	ah, db, _, _, au := newTestActionHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID:           "req-1",
		AccountID:           "acct1",
		ChannelID:           "ch1",
		IdentityStoreUserID: "uid-123",
		Status:              models.StatusGranted,
	}

	raw := marshalPayload(t, StepFunctionActionPayload{
		Action:    "revoke",
		RequestID: "req-1",
	})

	result, err := ah.Handle(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "expired" {
		t.Errorf("expected expired, got %s", result.Status)
	}
	if db.requests["req-1"].Status != models.StatusExpired {
		t.Errorf("expected EXPIRED status, got %s", db.requests["req-1"].Status)
	}
	if len(au.events) != 1 || au.events[0].eventType != models.EventExpired {
		t.Errorf("expected EXPIRED audit event")
	}
}

func TestHandleRevoke_AlreadyRevoked(t *testing.T) {
	ah, db, _, _, _ := newTestActionHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID: "req-1",
		Status:    models.StatusRevoked,
	}

	raw := marshalPayload(t, StepFunctionActionPayload{
		Action:    "revoke",
		RequestID: "req-1",
	})

	result, err := ah.Handle(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != models.StatusRevoked {
		t.Errorf("expected REVOKED status, got %s", result.Status)
	}
}

// ---------------------------------------------------------------------------
// handleNotifyRevoked tests
// ---------------------------------------------------------------------------

func TestHandleNotifyRevoked_Success(t *testing.T) {
	ah, db, _, wh, _ := newTestActionHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID: "req-1",
		AccountID: "acct1",
		ChannelID: "ch1",
		Status:    models.StatusExpired,
	}

	raw := marshalPayload(t, StepFunctionActionPayload{
		Action:    "notify_revoked",
		RequestID: "req-1",
	})

	result, err := ah.Handle(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "notified" {
		t.Errorf("expected notified, got %s", result.Status)
	}
	if len(wh.payloads) != 1 || wh.payloads[0].Status != models.StatusExpired {
		t.Errorf("expected EXPIRED webhook notification")
	}
}

// ---------------------------------------------------------------------------
// handleGrantError tests
// ---------------------------------------------------------------------------

func TestHandleGrantError_Success(t *testing.T) {
	ah, db, _, wh, au := newTestActionHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID: "req-1",
		AccountID: "acct1",
		ChannelID: "ch1",
		Status:    models.StatusApproved,
	}

	raw := marshalPayload(t, StepFunctionActionPayload{
		Action:    "handle_grant_error",
		RequestID: "req-1",
		Error:     json.RawMessage(`"CreateAccountAssignment failed"`),
	})

	result, err := ah.Handle(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "error_handled" {
		t.Errorf("expected error_handled, got %s", result.Status)
	}
	if db.requests["req-1"].Status != models.StatusError {
		t.Errorf("expected ERROR status, got %s", db.requests["req-1"].Status)
	}
	if len(au.events) != 1 || au.events[0].eventType != models.EventError {
		t.Errorf("expected ERROR audit event")
	}
	if len(wh.payloads) != 1 || wh.payloads[0].Status != models.StatusError {
		t.Errorf("expected ERROR webhook notification")
	}
}

// ---------------------------------------------------------------------------
// handleRevokeError tests
// ---------------------------------------------------------------------------

func TestHandleRevokeError_Success(t *testing.T) {
	ah, db, _, wh, au := newTestActionHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID: "req-1",
		AccountID: "acct1",
		ChannelID: "ch1",
		Status:    models.StatusGranted,
	}

	raw := marshalPayload(t, StepFunctionActionPayload{
		Action:    "handle_revoke_error",
		RequestID: "req-1",
		Error:     json.RawMessage(`"DeleteAccountAssignment failed"`),
	})

	result, err := ah.Handle(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "error_handled" {
		t.Errorf("expected error_handled, got %s", result.Status)
	}
	if len(au.events) != 1 || au.events[0].eventType != models.EventError {
		t.Errorf("expected ERROR audit event")
	}
	if len(wh.payloads) != 1 || wh.payloads[0].Status != models.StatusError {
		t.Errorf("expected ERROR webhook notification")
	}
}
