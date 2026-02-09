package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/dgwhited/jit-aws-controller/internal/models"
)

// Handler contains all dependencies for API request processing.
type Handler struct {
	DB       DBStore
	Identity IdentityProvider
	Webhook  WebhookNotifier
	Audit    AuditLogger
	SFN      SFNStarter
}

// HandleCreateRequest processes POST /requests.
// Validates the binding, duration, jira/reason, looks up the user, creates the request, and audits.
func (h *Handler) HandleCreateRequest(ctx context.Context, input models.CreateRequestInput) (*models.JitRequest, error) {
	// Validate required fields.
	if input.AccountID == "" || input.ChannelID == "" {
		return nil, fmt.Errorf("account_id and channel_id are required")
	}
	if input.RequesterMMUserID == "" || input.RequesterEmail == "" {
		return nil, fmt.Errorf("requester_mm_user_id and requester_email are required")
	}
	if input.Jira == "" && input.Reason == "" {
		return nil, fmt.Errorf("either jira or reason must be provided")
	}
	if input.RequestedDurationMinutes <= 0 {
		return nil, fmt.Errorf("requested_duration_minutes must be positive")
	}

	// Validate binding exists.
	cfg, err := h.DB.GetConfig(ctx, input.ChannelID, input.AccountID)
	if err != nil {
		return nil, fmt.Errorf("lookup config: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("no binding found for channel %s and account %s", input.ChannelID, input.AccountID)
	}

	// Validate duration against max.
	maxMinutes := cfg.MaxRequestHours * 60
	if maxMinutes > 0 && input.RequestedDurationMinutes > maxMinutes {
		return nil, fmt.Errorf("requested duration %d minutes exceeds maximum %d minutes", input.RequestedDurationMinutes, maxMinutes)
	}

	// Look up identity store user.
	userID, err := h.Identity.LookupUserByEmail(ctx, input.RequesterEmail)
	if err != nil {
		return nil, fmt.Errorf("identity lookup: %w", err)
	}

	now := time.Now().UTC()
	requestID := uuid.New().String()
	endTime := now.Add(time.Duration(input.RequestedDurationMinutes) * time.Minute)

	req := &models.JitRequest{
		RequestID:                requestID,
		AccountID:                input.AccountID,
		ChannelID:                input.ChannelID,
		RequesterMMUserID:        input.RequesterMMUserID,
		RequesterEmail:           input.RequesterEmail,
		Jira:                     input.Jira,
		Reason:                   input.Reason,
		RequestedDurationMinutes: input.RequestedDurationMinutes,
		Status:                   models.StatusPending,
		CreatedAt:                now.Format(time.RFC3339),
		EndTime:                  endTime.Format(time.RFC3339),
		IdentityStoreUserID:      userID,
	}

	if err := h.DB.CreateRequest(ctx, req); err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	slog.Info("request created",
		"request_id", requestID,
		"account_id", input.AccountID,
		"requester", input.RequesterEmail,
	)

	// Audit the creation.
	_ = h.Audit.Log(ctx, requestID, models.EventRequested, input.AccountID, input.ChannelID,
		input.RequesterMMUserID, input.RequesterEmail,
		map[string]string{
			"jira":                       input.Jira,
			"reason":                     input.Reason,
			"requested_duration_minutes": fmt.Sprintf("%d", input.RequestedDurationMinutes),
		},
	)

	return req, nil
}

// HandleApproveRequest processes POST /requests/{id}/approve.
func (h *Handler) HandleApproveRequest(ctx context.Context, input models.ApproveRequestInput) (*models.JitRequest, error) {
	if input.RequestID == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	if input.ApproverMMUserID == "" || input.ApproverEmail == "" {
		return nil, fmt.Errorf("approver_mm_user_id and approver_email are required")
	}

	req, err := h.DB.GetRequest(ctx, input.RequestID)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("request %s not found", input.RequestID)
	}

	// Verify status is PENDING.
	if req.Status != models.StatusPending {
		return nil, fmt.Errorf("request %s is in status %s, expected PENDING", input.RequestID, req.Status)
	}

	// Load config for self-approval check.
	cfg, err := h.DB.GetConfig(ctx, req.ChannelID, req.AccountID)
	if err != nil {
		return nil, fmt.Errorf("lookup config for approval: %w", err)
	}

	// Verify approver is authorized.
	if cfg != nil {
		isApprover := false
		for _, uid := range cfg.ApproverMMUserIDs {
			if uid == input.ApproverMMUserID {
				isApprover = true
				break
			}
		}
		if !isApprover {
			return nil, fmt.Errorf("user %s is not an authorized approver", input.ApproverMMUserID)
		}

		// Self-approval check.
		if !cfg.AllowSelfApproval && input.ApproverMMUserID == req.RequesterMMUserID {
			return nil, fmt.Errorf("self-approval is not allowed")
		}
	}

	now := time.Now().UTC()

	// Conditional update to APPROVED.
	updates := map[string]interface{}{
		"status":              models.StatusApproved,
		"approved_at":         now.Format(time.RFC3339),
		"approver_mm_user_id": input.ApproverMMUserID,
		"approver_email":      input.ApproverEmail,
	}
	if err := h.DB.ConditionalUpdateStatus(ctx, input.RequestID, models.StatusPending, updates); err != nil {
		return nil, fmt.Errorf("update to APPROVED: %w", err)
	}

	slog.Info("request approved",
		"request_id", input.RequestID,
		"approver", input.ApproverEmail,
	)

	// Audit the approval.
	_ = h.Audit.Log(ctx, input.RequestID, models.EventApproved, req.AccountID, req.ChannelID,
		input.ApproverMMUserID, input.ApproverEmail, nil)

	// Start the Step Functions grant workflow.
	sfInput := models.StepFunctionInput{
		RequestID:           req.RequestID,
		AccountID:           req.AccountID,
		ChannelID:           req.ChannelID,
		IdentityStoreUserID: req.IdentityStoreUserID,
		DurationMinutes:     req.RequestedDurationMinutes,
		RequesterEmail:      req.RequesterEmail,
	}
	if h.SFN != nil {
		if err := h.SFN.StartExecution(ctx, sfInput); err != nil {
			slog.Error("failed to start grant workflow",
				"request_id", input.RequestID,
				"error", err,
			)
			// Don't fail the approval — the reconciler will catch it.
		}
	}

	// Refresh and return.
	req, _ = h.DB.GetRequest(ctx, input.RequestID)
	return req, nil
}

// HandleDenyRequest processes POST /requests/{id}/deny.
func (h *Handler) HandleDenyRequest(ctx context.Context, input models.DenyRequestInput) (*models.JitRequest, error) {
	if input.RequestID == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	if input.DenierMMUserID == "" || input.DenierEmail == "" {
		return nil, fmt.Errorf("denier_mm_user_id and denier_email are required")
	}

	req, err := h.DB.GetRequest(ctx, input.RequestID)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("request %s not found", input.RequestID)
	}

	if req.Status != models.StatusPending {
		return nil, fmt.Errorf("request %s is in status %s, expected PENDING", input.RequestID, req.Status)
	}

	// Verify denier is an authorized approver.
	cfg, err := h.DB.GetConfig(ctx, req.ChannelID, req.AccountID)
	if err != nil {
		return nil, fmt.Errorf("lookup config for deny: %w", err)
	}
	if cfg != nil {
		isApprover := false
		for _, uid := range cfg.ApproverMMUserIDs {
			if uid == input.DenierMMUserID {
				isApprover = true
				break
			}
		}
		if !isApprover {
			return nil, fmt.Errorf("user %s is not an authorized approver", input.DenierMMUserID)
		}
	}

	now := time.Now().UTC()
	updates := map[string]interface{}{
		"status":              models.StatusDenied,
		"denied_at":           now.Format(time.RFC3339),
		"approver_mm_user_id": input.DenierMMUserID,
		"approver_email":      input.DenierEmail,
	}
	if err := h.DB.ConditionalUpdateStatus(ctx, input.RequestID, models.StatusPending, updates); err != nil {
		return nil, fmt.Errorf("update to DENIED: %w", err)
	}

	slog.Info("request denied",
		"request_id", input.RequestID,
		"denier", input.DenierEmail,
	)

	// Audit the denial.
	_ = h.Audit.Log(ctx, input.RequestID, models.EventDenied, req.AccountID, req.ChannelID,
		input.DenierMMUserID, input.DenierEmail, nil)

	// No webhook notification for denials — the plugin updates the approval
	// card in-place when the deny dialog is submitted.

	req, _ = h.DB.GetRequest(ctx, input.RequestID)
	return req, nil
}

// HandleRevokeRequest processes POST /requests/{id}/revoke.
func (h *Handler) HandleRevokeRequest(ctx context.Context, input models.RevokeRequestInput) (*models.JitRequest, error) {
	if input.RequestID == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	if input.ActorMMUserID == "" || input.ActorEmail == "" {
		return nil, fmt.Errorf("actor_mm_user_id and actor_email are required")
	}

	req, err := h.DB.GetRequest(ctx, input.RequestID)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("request %s not found", input.RequestID)
	}

	if req.Status != models.StatusGranted {
		return nil, fmt.Errorf("request %s is in status %s, expected GRANTED", input.RequestID, req.Status)
	}

	// Revoke IAM Identity Center access.
	if err := h.Identity.RevokeAccess(ctx, req.AccountID, req.IdentityStoreUserID); err != nil {
		slog.Error("failed to revoke access",
			"request_id", input.RequestID,
			"error", err,
		)
		// Update to ERROR state with details.
		errUpdates := map[string]interface{}{
			"status":        models.StatusError,
			"error_details": err.Error(),
		}
		_ = h.DB.ConditionalUpdateStatus(ctx, input.RequestID, models.StatusGranted, errUpdates)
		return nil, fmt.Errorf("revoke access: %w", err)
	}

	now := time.Now().UTC()
	updates := map[string]interface{}{
		"status":     models.StatusRevoked,
		"revoked_at": now.Format(time.RFC3339),
	}
	if err := h.DB.ConditionalUpdateStatus(ctx, input.RequestID, models.StatusGranted, updates); err != nil {
		return nil, fmt.Errorf("update to REVOKED: %w", err)
	}

	slog.Info("request revoked",
		"request_id", input.RequestID,
		"actor", input.ActorEmail,
	)

	// Audit the revocation.
	_ = h.Audit.Log(ctx, input.RequestID, models.EventRevoked, req.AccountID, req.ChannelID,
		input.ActorMMUserID, input.ActorEmail, nil)

	// Webhook notify.
	_ = h.Webhook.Notify(ctx, models.WebhookPayload{
		RequestID: input.RequestID,
		Status:    models.StatusRevoked,
		AccountID: req.AccountID,
		ChannelID: req.ChannelID,
		Actor:     input.ActorEmail,
	})

	req, _ = h.DB.GetRequest(ctx, input.RequestID)
	return req, nil
}

// HandleListRequests processes GET /requests with filters.
func (h *Handler) HandleListRequests(ctx context.Context, input models.ReportingInput) (*models.ReportingResponse, error) {
	// D5/E4: Require at least one filter to prevent unfiltered table scans.
	if input.ChannelID == "" && input.AccountID == "" && input.RequesterEmail == "" && input.Status == "" {
		return nil, fmt.Errorf("at least one filter is required (channel_id, account_id, requester_email, or status)")
	}

	if input.Limit <= 0 {
		input.Limit = 50
	}
	if input.Limit > 200 {
		input.Limit = 200
	}

	requests, nextToken, err := h.DB.QueryRequests(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("query requests: %w", err)
	}

	filters := map[string]string{}
	if input.ChannelID != "" {
		filters["channel_id"] = input.ChannelID
	}
	if input.AccountID != "" {
		filters["account_id"] = input.AccountID
	}
	if input.RequesterEmail != "" {
		filters["requester_email"] = input.RequesterEmail
	}
	if input.Status != "" {
		filters["status"] = input.Status
	}
	if input.StartDate != "" {
		filters["start_date"] = input.StartDate
	}
	if input.EndDate != "" {
		filters["end_date"] = input.EndDate
	}

	if requests == nil {
		requests = []models.JitRequest{}
	}

	return &models.ReportingResponse{
		Items:     requests,
		NextToken: nextToken,
		Filters:   filters,
	}, nil
}

// HandleBindAccount processes POST /config/bind.
// Binds an AWS account to a Mattermost channel.
func (h *Handler) HandleBindAccount(ctx context.Context, input models.BindAccountInput) (*models.JitConfig, error) {
	if input.ChannelID == "" || input.AccountID == "" {
		return nil, fmt.Errorf("channel_id and account_id are required")
	}

	// Check if already bound to a different channel.
	existing, err := h.DB.GetChannelForAccount(ctx, input.AccountID)
	if err != nil {
		return nil, fmt.Errorf("lookup existing binding: %w", err)
	}
	if existing != nil && existing.ChannelID != input.ChannelID {
		return nil, fmt.Errorf("account %s is already bound to channel %s", input.AccountID, existing.ChannelID)
	}

	now := time.Now().UTC()
	cfg := &models.JitConfig{
		ChannelID:       input.ChannelID,
		AccountID:       input.AccountID,
		ApprovalPolicy:  "one_of_n",
		MaxRequestHours: 4,
		UpdatedAt:       now.Format(time.RFC3339),
	}

	// If existing config exists for this channel+account, preserve its settings.
	existingCfg, err := h.DB.GetConfig(ctx, input.ChannelID, input.AccountID)
	if err != nil {
		return nil, fmt.Errorf("lookup config: %w", err)
	}
	if existingCfg != nil {
		cfg.ApproverMMUserIDs = existingCfg.ApproverMMUserIDs
		cfg.ApprovalPolicy = existingCfg.ApprovalPolicy
		cfg.AllowSelfApproval = existingCfg.AllowSelfApproval
		cfg.MaxRequestHours = existingCfg.MaxRequestHours
		cfg.SessionDurationMinutes = existingCfg.SessionDurationMinutes
	}

	if err := h.DB.PutConfig(ctx, cfg); err != nil {
		return nil, fmt.Errorf("put config: %w", err)
	}

	slog.Info("account bound to channel",
		"channel_id", input.ChannelID,
		"account_id", input.AccountID,
	)
	return cfg, nil
}

// HandleSetApprovers processes POST /config/approvers.
// Sets the approver list for all accounts bound to a channel.
func (h *Handler) HandleSetApprovers(ctx context.Context, input models.SetApproversInput) ([]models.JitConfig, error) {
	if input.ChannelID == "" {
		return nil, fmt.Errorf("channel_id is required")
	}
	if len(input.ApproverIDs) == 0 {
		return nil, fmt.Errorf("at least one approver ID is required")
	}

	configs, err := h.DB.GetConfigsByChannel(ctx, input.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("lookup configs: %w", err)
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("no accounts bound to channel %s", input.ChannelID)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	updated := make([]models.JitConfig, 0, len(configs))
	for _, cfg := range configs {
		cfg.ApproverMMUserIDs = input.ApproverIDs
		cfg.UpdatedAt = now
		if err := h.DB.PutConfig(ctx, &cfg); err != nil {
			return nil, fmt.Errorf("update config for account %s: %w", cfg.AccountID, err)
		}
		updated = append(updated, cfg)
	}

	slog.Info("approvers updated",
		"channel_id", input.ChannelID,
		"approver_count", len(input.ApproverIDs),
		"account_count", len(updated),
	)
	return updated, nil
}

// HandleGetBoundAccounts processes GET /config/accounts.
// Returns all account bindings for a given channel.
func (h *Handler) HandleGetBoundAccounts(ctx context.Context, channelID string) ([]models.JitConfig, error) {
	if channelID == "" {
		return nil, fmt.Errorf("channel_id query parameter is required")
	}

	configs, err := h.DB.GetConfigsByChannel(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("query configs: %w", err)
	}
	if configs == nil {
		configs = []models.JitConfig{}
	}
	return configs, nil
}

// Ensure json is used (it's used below in router, but keep the import clean).
var _ = json.Marshal
