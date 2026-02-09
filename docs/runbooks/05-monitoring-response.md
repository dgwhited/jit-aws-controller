# Runbook: Monitoring and Alarm Response

## Purpose

Provide per-alarm investigation and resolution procedures for all CloudWatch alarms defined in the JIT access system.

## Alarm Index

| Alarm | Severity | Metric | Threshold |
|-------|----------|--------|-----------|
| [JIT-GrantRevokeFailed](#jit-grantrevokefailed) | High | Step Function execution failures | > 0 in 5 min |
| [JIT-ReconcilerErrors](#jit-reconcilererrors) | High | Reconciler Lambda errors | > 0 in 15 min |
| [JIT-HMACAuthFailures](#jit-hmacauthfailures) | Medium | HMAC validation rejections | > 10 in 5 min |
| [JIT-StepFunctionTimeout](#jit-stepfunctiontimeout) | High | SFN execution timeouts | > 0 in 15 min |
| [JIT-LambdaErrorRate](#jit-lambdaerrorrate) | High | API Lambda error rate | > 5% in 5 min |
| [JIT-LambdaDuration](#jit-lambdaduration) | Medium | API Lambda p99 duration | > 10s in 5 min |
| [JIT-API4xxRate](#jit-api4xxrate) | Low | API Gateway 4xx rate | > 20% in 5 min |
| [JIT-API5xxRate](#jit-api5xxrate) | High | API Gateway 5xx rate | > 1% in 5 min |

---

## JIT-GrantRevokeFailed

**What triggered:** A Step Functions execution failed during the grant or revoke phase.

**Impact:** A user's access was not properly granted or revoked. If a grant failed, the user doesn't have access. If a revoke failed, the user may retain access beyond the approved window.

**Investigation:**

1. Check Step Functions execution history:
   ```bash
   aws stepfunctions list-executions \
     --state-machine-arn <sfn_arn> \
     --status-filter FAILED \
     --max-results 10
   ```

2. Get the failed execution details:
   ```bash
   aws stepfunctions get-execution-history \
     --execution-arn <execution_arn>
   ```

3. Check the Lambda logs for the specific action:
   ```bash
   aws logs filter-log-events \
     --log-group-name /aws/lambda/jit-api \
     --filter-pattern "ERROR" \
     --start-time $(date -d '30 minutes ago' +%s000)
   ```

4. Check if IAM Identity Center is experiencing issues:
   ```bash
   aws health describe-events --filter "services=sso"
   ```

**Resolution:**

- **Grant failure:** The request should be in ERROR status. The reconciler will NOT retry grants. Either:
  - Ask the user to submit a new request
  - Investigate and fix the root cause, then manually trigger the grant via Lambda
- **Revoke failure:** The reconciler will automatically retry on its next run (every 15 minutes). If urgent, use the [break-glass revoke](03-break-glass-revoke.md).

---

## JIT-ReconcilerErrors

**What triggered:** The reconciler Lambda (runs every 15 minutes) encountered errors while processing expired grants.

**Impact:** Users may retain access beyond their approved window.

**Investigation:**

1. Check reconciler Lambda logs:
   ```bash
   aws logs filter-log-events \
     --log-group-name /aws/lambda/jit-reconciler \
     --filter-pattern "ERROR" \
     --start-time $(date -d '1 hour ago' +%s000)
   ```

2. Check for GRANTED requests with expired end_time:
   ```bash
   aws dynamodb query \
     --table-name jit_requests \
     --index-name gsi_status_endtime \
     --key-condition-expression '#s = :s AND end_time <= :now' \
     --expression-attribute-names '{"#s":"status"}' \
     --expression-attribute-values '{":s":{"S":"GRANTED"},":now":{"S":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'"}}'
   ```

3. For each stuck request, check identity center status.

**Resolution:**

- If Identity Center is down: Wait for recovery; reconciler will auto-retry
- If specific requests are stuck: Use [break-glass revoke](03-break-glass-revoke.md) for each
- If Lambda itself is failing: Check for code errors, memory limits, or timeout settings

---

## JIT-HMACAuthFailures

**What triggered:** More than 10 HMAC validation failures in 5 minutes.

**Impact:** Legitimate requests may be getting rejected, or someone is attempting unauthorized API access.

**Investigation:**

1. Check API Lambda logs for HMAC failures:
   ```bash
   aws logs filter-log-events \
     --log-group-name /aws/lambda/jit-api \
     --filter-pattern "HMAC validation failed" \
     --start-time $(date -d '30 minutes ago' +%s000)
   ```

2. Look for patterns:
   - Same source IP? Could be an attack.
   - Timestamp skew errors? Clock drift between plugin and Lambda.
   - Key ID not found? Plugin may be using a rotated-out key.
   - Signature mismatch? Key secret may be wrong.

**Resolution:**

- **Clock skew:** Check the Mattermost server's NTP configuration. Allowed skew is 5 minutes.
- **Wrong key:** Verify plugin config matches Secrets Manager. See [key rotation runbook](02-key-rotation.md).
- **Attack attempt:** If source IPs are unknown, consider adding WAF rules to the API Gateway.
- **Post-rotation:** If this started after a key rotation, the old key may have been removed too early. Re-add it to Secrets Manager.

---

## JIT-StepFunctionTimeout

**What triggered:** A Step Functions execution timed out.

**Impact:** Similar to grant/revoke failure. The timeout typically means the wait state or a Lambda invocation hung.

**Investigation:**

1. Check execution history for TIMED_OUT executions
2. Identify which state timed out (typically the Lambda task states)
3. Check if the Lambda invocation itself timed out

**Resolution:**

- If Lambda timed out: Check for Identity Center API throttling or slowness
- Increase Lambda timeout if consistently hitting limits
- Check the request status in DynamoDB and manually resolve if needed

---

## JIT-LambdaErrorRate

**What triggered:** API Lambda error rate exceeds 5% in a 5-minute window.

**Impact:** Users cannot submit requests, approve/deny, or manage configuration.

**Investigation:**

1. Check for recent deployments that may have introduced bugs
2. Check Lambda logs for stack traces or error patterns
3. Check DynamoDB for throttling (check `ConsumedReadCapacityUnits` / `ThrottledRequests` metrics)
4. Check for AWS service issues

**Resolution:**

- **Code bug:** Roll back to previous Lambda version
- **DynamoDB throttling:** Table uses on-demand; check for hot partitions
- **AWS service issue:** Monitor AWS status page, wait for resolution
- **Memory/timeout:** Check Lambda configuration metrics

---

## JIT-LambdaDuration

**What triggered:** API Lambda p99 duration exceeds 10 seconds.

**Impact:** Users experience slow responses. Not a service-down event but degrades UX.

**Investigation:**

1. Check X-Ray traces (if enabled) for bottleneck identification
2. Check Identity Center API latency
3. Check DynamoDB latency metrics
4. Check for Lambda cold starts (provisioned concurrency may help)

**Resolution:**

- If Identity Center is slow: Temporary; monitor and wait
- If cold starts: Consider provisioned concurrency
- If DynamoDB latency: Check table metrics for hot keys

---

## JIT-API4xxRate

**What triggered:** More than 20% of API requests return 4xx status codes.

**Impact:** Users are experiencing errors, likely client-side issues (bad input, auth failures).

**Investigation:**

1. Check if this correlates with HMAC failures (see [JIT-HMACAuthFailures](#jit-hmacauthfailures))
2. Check API Gateway access logs for common 4xx paths
3. Check if the plugin was recently updated with breaking changes

**Resolution:**

- Usually indicates a plugin update or configuration change
- Review recent plugin deployments
- If HMAC-related, see the HMAC alarm runbook above

---

## JIT-API5xxRate

**What triggered:** More than 1% of API requests return 5xx status codes.

**Impact:** Service is partially or fully down. Users cannot make JIT requests.

**Investigation:**

1. Same steps as [JIT-LambdaErrorRate](#jit-lambdaerrorrate)
2. Check API Gateway itself for issues (deployment errors, stage issues)

**Resolution:**

- Same as Lambda error rate resolution
- If API Gateway stage issue: Redeploy the stage
- If persistent: Check CloudFormation/Terraform for infrastructure drift

---

## General Escalation Path

1. **L1 (On-call):** Check alarm, follow this runbook, resolve if straightforward
2. **L2 (Platform team):** If root cause is unclear or requires code changes
3. **L3 (Security team):** If the alarm suggests unauthorized access or a security incident

## Contacts

| Role | Contact |
|------|---------|
| JIT system owner | _[fill in]_ |
| Platform on-call | _[fill in]_ |
| Security team | _[fill in]_ |
