package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/identitystore"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/ssoadmin"

	"github.com/dgwhited/jit-aws-controller/internal/audit"
	"github.com/dgwhited/jit-aws-controller/internal/auth"
	"github.com/dgwhited/jit-aws-controller/internal/config"
	"github.com/dgwhited/jit-aws-controller/internal/dynamo"
	"github.com/dgwhited/jit-aws-controller/internal/handlers"
	"github.com/dgwhited/jit-aws-controller/internal/identity"
	"github.com/dgwhited/jit-aws-controller/internal/secrets"
	"github.com/dgwhited/jit-aws-controller/internal/webhook"
)

func main() {
	// Set up structured logging.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Load configuration.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// Initialize AWS SDK config.
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}

	// Initialize AWS service clients.
	ddbClient := dynamodb.NewFromConfig(awsCfg)
	sfnClient := sfn.NewFromConfig(awsCfg)
	ssoAdminClient := ssoadmin.NewFromConfig(awsCfg)
	identityStoreClient := identitystore.NewFromConfig(awsCfg)
	smClient := secretsmanager.NewFromConfig(awsCfg)

	// Fetch signing keys from Secrets Manager.
	signingKeys, err := secrets.FetchSigningKeys(ctx, smClient, cfg.SigningSecretARN)
	if err != nil {
		slog.Error("failed to fetch signing keys", "error", err)
		os.Exit(1)
	}

	// Fetch callback signing key for webhook.
	callbackKeys, err := secrets.FetchSigningKeys(ctx, smClient, cfg.CallbackSigningSecretARN)
	if err != nil {
		slog.Error("failed to fetch callback signing keys", "error", err)
		os.Exit(1)
	}

	// Build internal clients.
	db := dynamo.NewClient(ddbClient, cfg.TableConfig, cfg.TableRequests, cfg.TableAudit, cfg.TableNonces)
	identityClient := identity.NewClient(ssoAdminClient, identityStoreClient, cfg.SSOInstanceARN, cfg.IdentityStoreID, cfg.PermissionSetARN)

	// Use the first callback key for signing webhooks.
	var callbackKeyID, callbackSecret string
	for k, v := range callbackKeys {
		callbackKeyID = k
		callbackSecret = v
		break
	}
	webhookClient := webhook.NewClient(cfg.PluginWebhookURL, callbackKeyID, callbackSecret)

	auditLogger := audit.NewLogger(db)
	hmacValidator := auth.NewHMACValidator(signingKeys, db)

	handler := &handlers.Handler{
		DB:       db,
		Identity: identityClient,
		Webhook:  webhookClient,
		Audit:    auditLogger,
		SFN: &handlers.SFNClient{
			Client:          sfnClient,
			StateMachineARN: cfg.StepFunctionARN,
		},
	}

	router := handlers.NewRouter(handler, hmacValidator)
	actionHandler := handlers.NewActionHandler(handler)
	dispatcher := handlers.NewDispatcher(router, actionHandler)

	slog.Info("starting JIT API Lambda")
	lambda.Start(dispatcher.Handle)
}
