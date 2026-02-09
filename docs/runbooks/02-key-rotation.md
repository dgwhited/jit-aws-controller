# Runbook: Signing Key Rotation

## Purpose

Rotate the HMAC signing keys used for mutual authentication between the Mattermost plugin and the JIT backend. This procedure ensures zero-downtime rotation with a 24-hour overlap window.

## Architecture

There are two signing key pairs:

1. **Plugin-to-Backend key** (stored in `signing_secret_arn`) - Plugin signs requests to backend; backend validates.
2. **Backend-to-Plugin key** (stored in `callback_signing_secret_arn`) - Backend signs webhook callbacks; plugin validates.

Each secret in Secrets Manager stores a JSON object mapping key IDs to secrets:

```json
{
  "key-v1": "old-secret-value",
  "key-v2": "new-secret-value"
}
```

During rotation, the backend validates inbound requests against **all active keys**. The plugin signs with the **newest key**.

## Prerequisites

- AWS CLI access to Secrets Manager
- Mattermost system admin access to update plugin configuration
- Coordinate with the team: no maintenance window needed, but communicate the change

## Steps

### 1. Generate a New Key

```bash
NEW_KEY_ID="key-$(date +%Y%m%d)"
NEW_SECRET=$(openssl rand -hex 32)
echo "New key ID: $NEW_KEY_ID"
echo "New secret: $NEW_SECRET"
```

Store these values securely (you'll need them for the plugin config update).

### 2. Add the New Key to Secrets Manager

Retrieve the current secret, add the new key, and update:

```bash
# For plugin-to-backend signing key:
SECRET_ARN="arn:aws:secretsmanager:us-east-1:ACCOUNT:secret:jit-signing-key"

CURRENT=$(aws secretsmanager get-secret-value \
  --secret-id "$SECRET_ARN" \
  --query SecretString --output text)

# Add new key (uses jq):
UPDATED=$(echo "$CURRENT" | jq --arg kid "$NEW_KEY_ID" --arg secret "$NEW_SECRET" '. + {($kid): $secret}')

aws secretsmanager update-secret \
  --secret-id "$SECRET_ARN" \
  --secret-string "$UPDATED"
```

Repeat for the callback signing key if rotating both:

```bash
CALLBACK_ARN="arn:aws:secretsmanager:us-east-1:ACCOUNT:secret:jit-callback-key"
# Same process as above
```

### 3. Restart the Backend Lambda

The Lambda functions load keys at cold-start. Force a refresh:

```bash
# Update an env var to trigger a new deployment (or just wait for natural cold starts)
aws lambda update-function-configuration \
  --function-name jit-api \
  --environment "Variables={KEY_ROTATION_TS=$(date +%s)}"
```

Verify the Lambda logs show both old and new keys loaded.

### 4. Update the Plugin Configuration

In Mattermost System Console > Plugins > JIT Access:

1. Set **Signing Key ID** to the new key ID (`key-YYYYMMDD`)
2. Set **Signing Key Secret** to the new secret value
3. If rotating callback key: update **Callback Key ID** and **Callback Key Secret**
4. Save and restart the plugin

### 5. Verify

Test the full flow:

```
/jit request
```

- [ ] Request reaches backend (check API Lambda logs for successful HMAC validation)
- [ ] Webhook callback reaches plugin (check plugin logs)
- [ ] No HMAC validation errors in CloudWatch

### 6. Wait for Overlap Window (24 hours)

The old key remains valid for 24 hours. During this time, any cached Lambda instances that haven't cold-started yet will still accept the old key.

### 7. Remove the Old Key

After 24 hours:

```bash
CURRENT=$(aws secretsmanager get-secret-value \
  --secret-id "$SECRET_ARN" \
  --query SecretString --output text)

UPDATED=$(echo "$CURRENT" | jq 'del(.["old-key-id"])')

aws secretsmanager update-secret \
  --secret-id "$SECRET_ARN" \
  --secret-string "$UPDATED"
```

Force Lambda restart again:

```bash
aws lambda update-function-configuration \
  --function-name jit-api \
  --environment "Variables={KEY_ROTATION_TS=$(date +%s)}"
```

### 8. Final Verification

- [ ] Only the new key exists in Secrets Manager
- [ ] Plugin is using the new key
- [ ] End-to-end flow works
- [ ] No HMAC errors in CloudWatch for the past hour

## Rollback

If the new key causes issues:

1. Add the old key back to Secrets Manager (step 2 in reverse)
2. Update plugin config back to the old key ID and secret
3. Restart Lambda and plugin
4. Investigate the issue before retrying rotation

## Schedule

Recommend rotating keys quarterly or after any personnel change in the admin team.
