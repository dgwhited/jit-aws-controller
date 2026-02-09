package config

import (
	"strings"
	"testing"
)

// requiredEnvVars lists all required environment variables for config.Load().
var requiredEnvVars = map[string]string{
	"TABLE_CONFIG":                "jit-config",
	"TABLE_REQUESTS":              "jit-requests",
	"TABLE_AUDIT":                 "jit-audit",
	"TABLE_NONCES":                "jit-nonces",
	"SSO_INSTANCE_ARN":            "arn:aws:sso:::instance/ssoins-1234",
	"IDENTITY_STORE_ID":           "d-1234567890",
	"PERMISSION_SET_ARN":          "arn:aws:sso:::permissionSet/ssoins-1234/ps-abcdef",
	"SIGNING_SECRET_ARN":          "arn:aws:secretsmanager:us-east-1:123456789012:secret:signing",
	"CALLBACK_SIGNING_SECRET_ARN": "arn:aws:secretsmanager:us-east-1:123456789012:secret:callback",
	"PLUGIN_WEBHOOK_URL":          "https://example.com/webhook",
}

// setAllRequiredEnvVars sets all required env vars on the test using t.Setenv.
func setAllRequiredEnvVars(t *testing.T) {
	t.Helper()
	for k, v := range requiredEnvVars {
		t.Setenv(k, v)
	}
}

func TestLoad_AllRequiredSet(t *testing.T) {
	setAllRequiredEnvVars(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.TableConfig != "jit-config" {
		t.Errorf("expected TableConfig 'jit-config', got %q", cfg.TableConfig)
	}
	if cfg.TableRequests != "jit-requests" {
		t.Errorf("expected TableRequests 'jit-requests', got %q", cfg.TableRequests)
	}
	if cfg.PluginWebhookURL != "https://example.com/webhook" {
		t.Errorf("expected PluginWebhookURL, got %q", cfg.PluginWebhookURL)
	}
}

func TestLoad_MissingRequiredVar(t *testing.T) {
	// For each required var, test that omitting it causes an error mentioning its name.
	for envVar := range requiredEnvVars {
		t.Run("missing_"+envVar, func(t *testing.T) {
			// Set all vars except the one under test.
			for k, v := range requiredEnvVars {
				if k != envVar {
					t.Setenv(k, v)
				}
			}
			// Explicitly clear the var under test.
			t.Setenv(envVar, "")

			_, err := Load()
			if err == nil {
				t.Fatalf("expected error when %s is missing, got nil", envVar)
			}
			if !strings.Contains(err.Error(), envVar) {
				t.Errorf("expected error to mention %q, got: %v", envVar, err)
			}
		})
	}
}

func TestLoad_StepFunctionARNOptional(t *testing.T) {
	setAllRequiredEnvVars(t)
	// Do NOT set STEP_FUNCTION_ARN — it should still load successfully.

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error without STEP_FUNCTION_ARN, got: %v", err)
	}
	if cfg.StepFunctionARN != "" {
		t.Errorf("expected empty StepFunctionARN, got %q", cfg.StepFunctionARN)
	}
}

func TestLoad_StepFunctionARNLoadedWhenSet(t *testing.T) {
	setAllRequiredEnvVars(t)
	t.Setenv("STEP_FUNCTION_ARN", "arn:aws:states:us-east-1:123456789012:stateMachine:grant")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.StepFunctionARN != "arn:aws:states:us-east-1:123456789012:stateMachine:grant" {
		t.Errorf("expected StepFunctionARN to be set, got %q", cfg.StepFunctionARN)
	}
}
