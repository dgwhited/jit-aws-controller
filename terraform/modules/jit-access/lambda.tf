########################################
# CloudWatch Log Groups (created before Lambdas)
########################################
resource "aws_cloudwatch_log_group" "api_lambda" {
  name              = "/aws/lambda/${var.environment}-jit-api"
  retention_in_days = var.log_retention_days

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-api"
  })
}

resource "aws_cloudwatch_log_group" "reconciler_lambda" {
  name              = "/aws/lambda/${var.environment}-jit-reconciler"
  retention_in_days = var.log_retention_days

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-reconciler"
  })
}

########################################
# S3 object lookups (used to detect code changes via ETag)
########################################
data "aws_s3_object" "api_lambda" {
  bucket = var.lambda_artifact_bucket
  key    = var.api_lambda_s3_key
}

data "aws_s3_object" "reconciler_lambda" {
  bucket = var.lambda_artifact_bucket
  key    = var.reconciler_lambda_s3_key
}

########################################
# API Lambda
########################################
resource "aws_lambda_function" "jit_api" {
  function_name = "${var.environment}-jit-api"
  description   = "JIT Access Controller API – handles access requests, approvals, grants, and revocations."

  s3_bucket        = var.lambda_artifact_bucket
  s3_key           = var.api_lambda_s3_key
  source_code_hash = data.aws_s3_object.api_lambda.etag

  handler = "bootstrap"
  runtime = "provided.al2023"

  memory_size = 256
  timeout     = 30

  role = aws_iam_role.api_lambda.arn

  environment {
    variables = {
      TABLE_CONFIG                = aws_dynamodb_table.jit_config.name
      TABLE_REQUESTS              = aws_dynamodb_table.jit_requests.name
      TABLE_AUDIT                 = aws_dynamodb_table.jit_audit.name
      TABLE_NONCES                = aws_dynamodb_table.jit_nonces.name
      SSO_INSTANCE_ARN            = var.sso_instance_arn
      IDENTITY_STORE_ID           = var.identity_store_id
      PERMISSION_SET_ARN          = var.permission_set_arn
      SIGNING_SECRET_ARN          = aws_secretsmanager_secret.signing_key.arn
      PLUGIN_WEBHOOK_URL          = var.plugin_webhook_url
      CALLBACK_SIGNING_SECRET_ARN = aws_secretsmanager_secret.callback_signing_key.arn
      STEP_FUNCTION_ARN           = "arn:aws:states:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:stateMachine:${var.environment}-jit-grant-revoke"
    }
  }

  dynamic "vpc_config" {
    for_each = var.lambda_vpc_config != null ? [var.lambda_vpc_config] : []
    content {
      subnet_ids         = vpc_config.value.subnet_ids
      security_group_ids = vpc_config.value.security_group_ids
    }
  }

  depends_on = [
    aws_iam_role_policy.api_lambda,
    aws_cloudwatch_log_group.api_lambda,
  ]

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-api"
  })
}

########################################
# Reconciler Lambda
########################################
resource "aws_lambda_function" "jit_reconciler" {
  function_name = "${var.environment}-jit-reconciler"
  description   = "JIT Access Reconciler – periodically checks and revokes expired access assignments."

  s3_bucket        = var.lambda_artifact_bucket
  s3_key           = var.reconciler_lambda_s3_key
  source_code_hash = data.aws_s3_object.reconciler_lambda.etag

  handler = "bootstrap"
  runtime = "provided.al2023"

  memory_size = 256
  timeout     = 300

  role = aws_iam_role.reconciler_lambda.arn

  environment {
    variables = {
      TABLE_CONFIG                = aws_dynamodb_table.jit_config.name
      TABLE_REQUESTS              = aws_dynamodb_table.jit_requests.name
      TABLE_AUDIT                 = aws_dynamodb_table.jit_audit.name
      TABLE_NONCES                = aws_dynamodb_table.jit_nonces.name
      SSO_INSTANCE_ARN            = var.sso_instance_arn
      IDENTITY_STORE_ID           = var.identity_store_id
      PERMISSION_SET_ARN          = var.permission_set_arn
      SIGNING_SECRET_ARN          = aws_secretsmanager_secret.signing_key.arn
      PLUGIN_WEBHOOK_URL          = var.plugin_webhook_url
      CALLBACK_SIGNING_SECRET_ARN = aws_secretsmanager_secret.callback_signing_key.arn
    }
  }

  dynamic "vpc_config" {
    for_each = var.lambda_vpc_config != null ? [var.lambda_vpc_config] : []
    content {
      subnet_ids         = vpc_config.value.subnet_ids
      security_group_ids = vpc_config.value.security_group_ids
    }
  }

  depends_on = [
    aws_iam_role_policy.reconciler_lambda,
    aws_cloudwatch_log_group.reconciler_lambda,
  ]

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-reconciler"
  })
}
