# Runbook: Audit Export

## Purpose

Export JIT access audit logs for compliance, security review, or incident investigation.

## Data Sources

All audit events are stored in the `jit_audit` DynamoDB table with the following structure:

| Field | Description |
|-------|-------------|
| `request_id` (PK) | The JIT request this event belongs to |
| `event_time#event_id` (SK) | Composite sort key for ordering |
| `event_type` | REQUESTED, APPROVED, DENIED, GRANTED, REVOKED, EXPIRED, ERROR |
| `account_id` | AWS account ID |
| `channel_id` | Mattermost channel ID |
| `actor_mm_user_id` | Who performed the action |
| `actor_email` | Email of the actor |
| `details` | Additional context (map) |

### GSIs Available

- `gsi_account_event`: Query by account_id + event_time
- `gsi_channel_event`: Query by channel_id + event_time

## Method 1: Query via API

Use the reporting endpoint for basic queries:

```bash
curl -H "X-JIT-KeyID: $KEY_ID" \
     -H "X-JIT-Timestamp: $TIMESTAMP" \
     -H "X-JIT-Nonce: $NONCE" \
     -H "X-JIT-Signature: $SIGNATURE" \
     "$API_URL/requests?channel_id=CH1&start_date=2026-01-01T00:00:00Z&end_date=2026-02-01T00:00:00Z"
```

This returns request summaries with latest status, not full audit trails.

## Method 2: DynamoDB Query (Full Audit Trail)

### Export All Events for a Request

```bash
aws dynamodb query \
  --table-name jit_audit \
  --key-condition-expression 'request_id = :rid' \
  --expression-attribute-values '{":rid":{"S":"REQUEST_ID"}}' \
  --output json
```

### Export Events for an Account (Date Range)

```bash
aws dynamodb query \
  --table-name jit_audit \
  --index-name gsi_account_event \
  --key-condition-expression 'account_id = :aid AND begins_with(event_time_event_id, :prefix)' \
  --expression-attribute-values '{
    ":aid":{"S":"123456789012"},
    ":prefix":{"S":"2026-01"}
  }' \
  --output json > audit_export.json
```

### Export Events for a Channel (Date Range)

```bash
aws dynamodb query \
  --table-name jit_audit \
  --index-name gsi_channel_event \
  --key-condition-expression 'channel_id = :cid AND begins_with(event_time_event_id, :prefix)' \
  --expression-attribute-values '{
    ":cid":{"S":"CHANNEL_ID"},
    ":prefix":{"S":"2026-01"}
  }' \
  --output json > audit_export.json
```

## Method 3: Full Table Export to S3

For large-scale exports or periodic compliance snapshots:

### Using DynamoDB Export to S3

```bash
aws dynamodb export-table-to-point-in-time \
  --table-arn arn:aws:dynamodb:us-east-1:ACCOUNT:table/jit_audit \
  --s3-bucket jit-audit-exports \
  --s3-prefix "exports/$(date +%Y-%m-%d)" \
  --export-format DYNAMODB_JSON
```

### Convert to JSON Lines

After export, convert to JSON lines format:

```python
#!/usr/bin/env python3
import json
import sys

for line in sys.stdin:
    record = json.loads(line)
    item = record.get("Item", {})
    flat = {}
    for key, value in item.items():
        for type_key, type_value in value.items():
            flat[key] = type_value
    print(json.dumps(flat))
```

Usage:

```bash
cat export_data.json | python3 convert.py > audit_flat.jsonl
```

## Method 4: Scan with Pagination (Ad-Hoc)

For ad-hoc queries when GSIs don't cover the filter:

```bash
#!/bin/bash
# Export all ERROR events
LAST_KEY=""
while true; do
  CMD="aws dynamodb scan --table-name jit_audit"
  CMD="$CMD --filter-expression 'event_type = :et'"
  CMD="$CMD --expression-attribute-values '{\":et\":{\"S\":\"ERROR\"}}'"
  if [ -n "$LAST_KEY" ]; then
    CMD="$CMD --exclusive-start-key '$LAST_KEY'"
  fi
  CMD="$CMD --output json"

  RESULT=$(eval $CMD)
  echo "$RESULT" | jq -c '.Items[]'

  LAST_KEY=$(echo "$RESULT" | jq -r '.LastEvaluatedKey // empty')
  if [ -z "$LAST_KEY" ]; then
    break
  fi
done
```

## Output Format

All exports should produce JSON lines (one JSON object per line):

```json
{"request_id":"req-123","event_type":"REQUESTED","event_time":"2026-01-15T10:00:00Z","account_id":"123456789012","channel_id":"ch1","actor_email":"user@company.com"}
{"request_id":"req-123","event_type":"APPROVED","event_time":"2026-01-15T10:05:00Z","account_id":"123456789012","channel_id":"ch1","actor_email":"approver@company.com"}
{"request_id":"req-123","event_type":"GRANTED","event_time":"2026-01-15T10:05:05Z","account_id":"123456789012","channel_id":"ch1","actor_email":"system"}
```

## Retention

- DynamoDB Point-in-Time Recovery is enabled (35 days)
- For longer retention, schedule periodic S3 exports (Method 3)
- S3 lifecycle policy should retain audit exports per your compliance requirements (typically 1-7 years)

## Access Control

- DynamoDB table access: Restricted to Lambda execution roles and admin IAM roles
- S3 export bucket: Restrict to security/compliance team IAM roles
- All queries should be logged (CloudTrail is enabled on DynamoDB and S3)
