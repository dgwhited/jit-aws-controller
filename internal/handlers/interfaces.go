package handlers

import (
	"context"

	"github.com/dgwhited/jit-aws-controller/internal/models"
)

// DBStore abstracts the DynamoDB operations needed by handlers.
type DBStore interface {
	GetConfig(ctx context.Context, channelID, accountID string) (*models.JitConfig, error)
	GetConfigsByChannel(ctx context.Context, channelID string) ([]models.JitConfig, error)
	PutConfig(ctx context.Context, cfg *models.JitConfig) error
	GetChannelForAccount(ctx context.Context, accountID string) (*models.JitConfig, error)

	CreateRequest(ctx context.Context, req *models.JitRequest) error
	GetRequest(ctx context.Context, requestID string) (*models.JitRequest, error)
	UpdateRequestStatus(ctx context.Context, requestID string, updates map[string]interface{}) error
	ConditionalUpdateStatus(ctx context.Context, requestID, expectedStatus string, updates map[string]interface{}) error

	QueryRequests(ctx context.Context, input models.ReportingInput) ([]models.JitRequest, string, error)
}

// IdentityProvider abstracts IAM Identity Center operations.
type IdentityProvider interface {
	LookupUserByEmail(ctx context.Context, email string) (string, error)
	GrantAccess(ctx context.Context, accountID, userID string) error
	RevokeAccess(ctx context.Context, accountID, userID string) error
}

// WebhookNotifier abstracts webhook delivery to the plugin.
type WebhookNotifier interface {
	Notify(ctx context.Context, payload models.WebhookPayload) error
}

// AuditLogger abstracts audit event recording.
type AuditLogger interface {
	Log(ctx context.Context, requestID, eventType, accountID, channelID, actorMMUserID, actorEmail string, details map[string]string) error
}

// SFNStarter abstracts Step Functions execution starting.
type SFNStarter interface {
	StartExecution(ctx context.Context, input models.StepFunctionInput) error
}
