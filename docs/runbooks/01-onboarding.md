# Runbook: Onboarding a New Account/Channel

## Purpose

Bind an AWS account to a Mattermost channel and configure approvers so that team members can request JIT access.

## Prerequisites

- Mattermost system admin access
- The AWS account ID (12-digit)
- The Mattermost channel where the team operates
- At least one designated approver (Mattermost user)

## Steps

### 1. Bind the AWS Account

In the target Mattermost channel, run:

```
/jit bind <account_id>
```

Example:

```
/jit bind 123456789012
```

**Expected response:** Confirmation that account `123456789012` is bound to this channel.

**Error cases:**
- "account is already bound to channel X" - The account is bound to a different channel. Each account can only be bound to one channel. To rebind, contact the admin of the other channel.

### 2. Set Approvers

In the same channel, run:

```
/jit approvers @user1 @user2 @user3
```

This sets the approver list for **all accounts** bound to this channel. You need at least one approver.

**Expected response:** Confirmation listing the updated approvers and affected accounts.

### 3. Verify the Configuration

Check that the binding is active:

```
/jit status
```

Or query the backend directly via the API:

```
GET /config/accounts?channel_id=<channel_id>
```

Verify:
- [ ] Account ID appears in the response
- [ ] Approver list is correct
- [ ] `max_request_hours` is appropriate (default: 4)
- [ ] `allow_self_approval` is set correctly (default: false)

### 4. Test the Flow

Have a team member run:

```
/jit request
```

Verify:
- [ ] The dialog opens with the correct account in the dropdown
- [ ] Duration validation works (cannot exceed `max_request_hours`)
- [ ] Jira field is required
- [ ] After submission, an approval card appears in the channel
- [ ] An approver can click Approve/Deny
- [ ] Non-approvers get an ephemeral rejection

### 5. Verify Webhook Connectivity

After approving a test request, verify:
- [ ] The backend webhook reaches the plugin (check Lambda logs for "webhook notification sent")
- [ ] The channel receives a "Access Granted" notification
- [ ] After the duration expires, the channel receives an "Access Expired" notification

## Troubleshooting

| Symptom | Cause | Resolution |
|---------|-------|------------|
| `/jit bind` returns error | Plugin cannot reach backend API | Check `BACKEND_API_URL` in plugin config, verify network path |
| Approval card not posted | Plugin error | Check Mattermost plugin logs for HMAC or API errors |
| No webhook notification | Lambda cannot reach MM | Check Lambda VPC config, security groups, NAT gateway |
| "unauthorized" errors | HMAC key mismatch | Verify signing key ID and secret match between plugin config and Secrets Manager |

## Rollback

To unbind an account, there is currently no `/jit unbind` command. Delete the config entry directly from the `jit_config` DynamoDB table:

```
aws dynamodb delete-item \
  --table-name jit_config \
  --key '{"channel_id":{"S":"<channel_id>"},"account_id":{"S":"<account_id>"}}'
```
