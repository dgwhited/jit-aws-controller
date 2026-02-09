package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/identitystore"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssoadmin"

	"github.com/dgwhited/jit-aws-controller/internal/audit"
	"github.com/dgwhited/jit-aws-controller/internal/config"
	"github.com/dgwhited/jit-aws-controller/internal/dynamo"
	"github.com/dgwhited/jit-aws-controller/internal/identity"
	"github.com/dgwhited/jit-aws-controller/internal/models"
	"github.com/dgwhited/jit-aws-controller/internal/secrets"
	"github.com/dgwhited/jit-aws-controller/internal/webhook"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}

	ddbClient := dynamodb.NewFromConfig(awsCfg)
	ssoAdminClient := ssoadmin.NewFromConfig(awsCfg)
	identityStoreClient := identitystore.NewFromConfig(awsCfg)
	smClient := secretsmanager.NewFromConfig(awsCfg)

	// Fetch callback signing key for webhook notifications.
	callbackKeys, err := secrets.FetchSigningKeys(ctx, smClient, cfg.CallbackSigningSecretARN)
	if err != nil {
		slog.Error("failed to fetch callback signing keys", "error", err)
		os.Exit(1)
	}

	db := dynamo.NewClient(ddbClient, cfg.TableConfig, cfg.TableRequests, cfg.TableAudit, cfg.TableNonces)
	identityClient := identity.NewClient(ssoAdminClient, identityStoreClient, cfg.SSOInstanceARN, cfg.IdentityStoreID, cfg.PermissionSetARN)

	var callbackKeyID, callbackSecret string
	for k, v := range callbackKeys {
		callbackKeyID = k
		callbackSecret = v
		break
	}
	webhookClient := webhook.NewClient(cfg.PluginWebhookURL, callbackKeyID, callbackSecret)
	auditLogger := audit.NewLogger(db)

	reconciler := &Reconciler{
		DB:       db,
		Identity: identityClient,
		Webhook:  webhookClient,
		Audit:    auditLogger,
	}

	slog.Info("starting JIT Reconciler Lambda")
	lambda.Start(reconciler.Handle)
}

// Reconciler processes expired GRANTED requests.
type Reconciler struct {
	DB       *dynamo.Client
	Identity *identity.Client
	Webhook  *webhook.Client
	Audit    *audit.Logger
}

// Handle is the Lambda handler invoked by EventBridge on a schedule.
func (r *Reconciler) Handle(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339)

	slog.Info("reconciler run starting", "now", now)

	// Query all GRANTED requests whose end_time has passed.
	requests, err := r.DB.QueryRequestsByStatus(ctx, models.StatusGranted, now, 0)
	if err != nil {
		slog.Error("failed to query expired grants", "error", err)
		return fmt.Errorf("query expired grants: %w", err)
	}

	slog.Info("found expired grants", "count", len(requests))

	var errCount int
	for _, req := range requests {
		if err := r.revokeExpired(ctx, req); err != nil {
			slog.Error("failed to revoke expired grant",
				"request_id", req.RequestID,
				"account_id", req.AccountID,
				"error", err,
			)
			errCount++
			// Continue processing remaining requests.
			continue
		}
	}

	if errCount > 0 {
		slog.Warn("reconciler completed with errors",
			"total", len(requests),
			"errors", errCount,
		)
		return fmt.Errorf("reconciler completed with %d errors out of %d", errCount, len(requests))
	}

	slog.Info("reconciler run completed", "processed", len(requests))
	return nil
}

func (r *Reconciler) revokeExpired(ctx context.Context, req models.JitRequest) error {
	// Revoke IAM Identity Center access.
	if err := r.Identity.RevokeAccess(ctx, req.AccountID, req.IdentityStoreUserID); err != nil {
		// Record error but continue.
		errUpdates := map[string]interface{}{
			"status":        models.StatusError,
			"error_details": fmt.Sprintf("reconciler revoke failed: %s", err.Error()),
		}
		_ = r.DB.ConditionalUpdateStatus(ctx, req.RequestID, models.StatusGranted, errUpdates)

		_ = r.Audit.Log(ctx, req.RequestID, models.EventError, req.AccountID, req.ChannelID,
			"", "reconciler",
			map[string]string{"error": err.Error()},
		)
		return fmt.Errorf("revoke access for %s: %w", req.RequestID, err)
	}

	// Update status to EXPIRED with conditional check.
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"status":     models.StatusExpired,
		"expired_at": now.Format(time.RFC3339),
	}
	if err := r.DB.ConditionalUpdateStatus(ctx, req.RequestID, models.StatusGranted, updates); err != nil {
		// If conditional update fails, the request was likely already updated (e.g., manually revoked).
		slog.Warn("conditional update to EXPIRED failed, may have been revoked already",
			"request_id", req.RequestID,
			"error", err,
		)
		return nil
	}

	// Audit the expiration.
	_ = r.Audit.Log(ctx, req.RequestID, models.EventExpired, req.AccountID, req.ChannelID,
		"", "reconciler", nil)

	// Webhook notify.
	_ = r.Webhook.Notify(ctx, models.WebhookPayload{
		RequestID: req.RequestID,
		Status:    models.StatusExpired,
		AccountID: req.AccountID,
		ChannelID: req.ChannelID,
		Actor:     "reconciler",
	})

	slog.Info("expired grant revoked",
		"request_id", req.RequestID,
		"account_id", req.AccountID,
		"requester", req.RequesterEmail,
	)
	return nil
}
