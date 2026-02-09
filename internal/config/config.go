package config

import (
	"fmt"
	"os"
)

// Config holds all environment-sourced configuration for the JIT controller.
type Config struct {
	TableConfig              string
	TableRequests            string
	TableAudit               string
	TableNonces              string
	SSOInstanceARN           string
	IdentityStoreID          string
	PermissionSetARN         string
	SigningSecretARN         string
	CallbackSigningSecretARN string
	PluginWebhookURL         string
	StepFunctionARN          string
	AWSRegion                string
}

// Load reads configuration from environment variables and validates required fields.
func Load() (*Config, error) {
	cfg := &Config{
		TableConfig:              os.Getenv("TABLE_CONFIG"),
		TableRequests:            os.Getenv("TABLE_REQUESTS"),
		TableAudit:               os.Getenv("TABLE_AUDIT"),
		TableNonces:              os.Getenv("TABLE_NONCES"),
		SSOInstanceARN:           os.Getenv("SSO_INSTANCE_ARN"),
		IdentityStoreID:          os.Getenv("IDENTITY_STORE_ID"),
		PermissionSetARN:         os.Getenv("PERMISSION_SET_ARN"),
		SigningSecretARN:         os.Getenv("SIGNING_SECRET_ARN"),
		CallbackSigningSecretARN: os.Getenv("CALLBACK_SIGNING_SECRET_ARN"),
		PluginWebhookURL:         os.Getenv("PLUGIN_WEBHOOK_URL"),
		StepFunctionARN:          os.Getenv("STEP_FUNCTION_ARN"),
		AWSRegion:                os.Getenv("AWS_REGION"),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	required := map[string]string{
		"TABLE_CONFIG":                c.TableConfig,
		"TABLE_REQUESTS":              c.TableRequests,
		"TABLE_AUDIT":                 c.TableAudit,
		"TABLE_NONCES":                c.TableNonces,
		"SSO_INSTANCE_ARN":            c.SSOInstanceARN,
		"IDENTITY_STORE_ID":           c.IdentityStoreID,
		"PERMISSION_SET_ARN":          c.PermissionSetARN,
		"SIGNING_SECRET_ARN":          c.SigningSecretARN,
		"CALLBACK_SIGNING_SECRET_ARN": c.CallbackSigningSecretARN,
		"PLUGIN_WEBHOOK_URL":          c.PluginWebhookURL,
	}

	var missing []string
	for name, val := range required {
		if val == "" {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %v", missing)
	}
	return nil
}
