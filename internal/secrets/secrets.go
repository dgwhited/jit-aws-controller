package secrets

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// FetchSigningKeys retrieves signing secrets from Secrets Manager.
// The secret value can be either a plain string (single key) or a JSON object
// mapping key IDs to secrets (for rotation support with multiple active keys).
func FetchSigningKeys(ctx context.Context, sm *secretsmanager.Client, secretARN string) (map[string]string, error) {
	out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: &secretARN,
	})
	if err != nil {
		return nil, fmt.Errorf("get secret %s: %w", secretARN, err)
	}

	secretString := ""
	if out.SecretString != nil {
		secretString = *out.SecretString
	}

	if secretString == "" {
		return nil, fmt.Errorf("secret %s has no string value", secretARN)
	}

	// Try to parse as JSON object (multi-key).
	keys := map[string]string{}
	if err := json.Unmarshal([]byte(secretString), &keys); err == nil && len(keys) > 0 {
		return keys, nil
	}

	// Treat as a single plain-text key with a default key ID.
	return map[string]string{"default": secretString}, nil
}
