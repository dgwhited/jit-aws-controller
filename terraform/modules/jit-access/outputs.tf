output "api_endpoint" {
  description = "The invoke URL of the JIT Access HTTP API."
  value       = aws_apigatewayv2_stage.default.invoke_url
}

output "api_lambda_arn" {
  description = "ARN of the JIT API Lambda function."
  value       = aws_lambda_function.jit_api.arn
}

output "reconciler_lambda_arn" {
  description = "ARN of the JIT Reconciler Lambda function."
  value       = aws_lambda_function.jit_reconciler.arn
}

output "step_function_arn" {
  description = "ARN of the JIT grant-revoke Step Functions state machine."
  value       = aws_sfn_state_machine.jit_grant_revoke.arn
}

output "dynamodb_table_arns" {
  description = "Map of DynamoDB table names to their ARNs."
  value = {
    config   = aws_dynamodb_table.jit_config.arn
    requests = aws_dynamodb_table.jit_requests.arn
    audit    = aws_dynamodb_table.jit_audit.arn
    nonces   = aws_dynamodb_table.jit_nonces.arn
  }
}

output "signing_secret_arn" {
  description = "ARN of the Secrets Manager secret for inbound HMAC signing."
  value       = aws_secretsmanager_secret.signing_key.arn
}

output "callback_signing_secret_arn" {
  description = "ARN of the Secrets Manager secret for outbound callback HMAC signing."
  value       = aws_secretsmanager_secret.callback_signing_key.arn
}

output "artifact_bucket_name" {
  description = "Name of the S3 artifact bucket (empty if not created)."
  value       = var.create_artifact_bucket ? aws_s3_bucket.artifacts[0].id : ""
}

output "permission_set_arn" {
  description = "ARN of the JIT permission set (created or provided)."
  value       = local.permission_set_arn
}
