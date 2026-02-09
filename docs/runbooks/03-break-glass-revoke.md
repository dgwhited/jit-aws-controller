# Runbook: Break-Glass Revoke

## Purpose

Immediately revoke JIT access for a user when the normal expiration flow is too slow or the system is in a degraded state.

## When to Use

- Security incident requiring immediate access removal
- User account compromise
- Accidental approval of a request
- System error where access was granted but the auto-revoke timer failed

## Option 1: Via Mattermost (Preferred)

### Prerequisites

- Mattermost system admin role
- The request ID (visible in the approval card or from `/jit status`)

### Steps

1. **Run the revoke command** from any channel:

   ```
   /jit revoke <request_id>
   ```

   Example:

   ```
   /jit revoke a1b2c3d4-e5f6-7890-abcd-ef1234567890
   ```

2. **Verify** the confirmation message appears in the channel.

3. **Check** the AWS SSO console to confirm the assignment is removed:

   ```bash
   aws sso-admin list-account-assignments \
     --instance-arn <sso_instance_arn> \
     --account-id <account_id> \
     --permission-set-arn <permission_set_arn>
   ```

### Expected Flow

```
/jit revoke req-123
    |
    v
Plugin sends POST /requests/req-123/revoke to backend
    |
    v
Backend calls DeleteAccountAssignment (IAM Identity Center)
    |
    v
Backend updates request status to REVOKED
    |
    v
Backend sends webhook notification to plugin
    |
    v
Channel receives "Access Revoked" notification
```

## Option 2: Direct Lambda Invocation

If the plugin or Mattermost is unavailable:

```bash
# Invoke the API Lambda directly with a revoke payload
aws lambda invoke \
  --function-name jit-api \
  --payload '{"requestContext":{"http":{"method":"POST","path":"/requests/REQUEST_ID/revoke"}},"body":"{\"actor_mm_user_id\":\"admin\",\"actor_email\":\"admin@company.com\"}","headers":{"X-JIT-KeyID":"KEY_ID","X-JIT-Timestamp":"TIMESTAMP","X-JIT-Nonce":"NONCE","X-JIT-Signature":"SIGNATURE"}}' \
  /dev/stdout
```

Note: This requires generating a valid HMAC signature. If HMAC signing is impractical in an emergency, use Option 3.

## Option 3: Direct AWS Console (Last Resort)

If the JIT system is completely unavailable:

### 3a. Remove via AWS CLI

```bash
aws sso-admin delete-account-assignment \
  --instance-arn <sso_instance_arn> \
  --target-id <account_id> \
  --target-type AWS_ACCOUNT \
  --permission-set-arn <permission_set_arn> \
  --principal-type USER \
  --principal-id <identity_store_user_id>
```

### 3b. Remove via AWS Console

1. Go to **IAM Identity Center** > **AWS accounts**
2. Select the target account
3. Find the user's permission set assignment
4. Click **Remove access**

### 3c. Update DynamoDB (Cleanup)

After manually revoking, update the request record to reflect the revocation:

```bash
aws dynamodb update-item \
  --table-name jit_requests \
  --key '{"request_id":{"S":"REQUEST_ID"}}' \
  --update-expression 'SET #s = :s, revoked_at = :t' \
  --expression-attribute-names '{"#s":"status"}' \
  --expression-attribute-values '{":s":{"S":"REVOKED"},":t":{"S":"2026-01-15T12:00:00Z"}}' \
  --condition-expression '#s = :gs' \
  --expression-attribute-values '{":s":{"S":"REVOKED"},":t":{"S":"2026-01-15T12:00:00Z"},":gs":{"S":"GRANTED"}}'
```

## Verification

After any revoke method:

1. **Confirm assignment removed:**
   ```bash
   aws sso-admin list-account-assignments \
     --instance-arn <sso_instance_arn> \
     --account-id <account_id> \
     --permission-set-arn <permission_set_arn> \
     | grep <user_id>
   ```
   Should return empty.

2. **Confirm DynamoDB status:**
   ```bash
   aws dynamodb get-item \
     --table-name jit_requests \
     --key '{"request_id":{"S":"REQUEST_ID"}}' \
     --projection-expression "#s" \
     --expression-attribute-names '{"#s":"status"}'
   ```
   Should show `REVOKED`.

3. **Check audit trail:**
   ```bash
   aws dynamodb query \
     --table-name jit_audit \
     --key-condition-expression 'request_id = :rid' \
     --expression-attribute-values '{":rid":{"S":"REQUEST_ID"}}'
   ```

## Timing

| Method | Expected Time |
|--------|--------------|
| `/jit revoke` | 5-15 seconds |
| Direct Lambda invocation | 5-15 seconds |
| AWS Console manual | 1-5 minutes |

## Post-Incident

- File an incident report if break-glass was used for a security event
- Review audit logs for the revoked request
- Investigate why normal revocation flow was insufficient
