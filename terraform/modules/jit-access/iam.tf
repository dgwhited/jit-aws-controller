data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id
  region     = data.aws_region.current.name

  permission_set_arn = var.create_permission_set ? aws_ssoadmin_permission_set.jit[0].arn : var.permission_set_arn

  dynamodb_table_arns = [
    aws_dynamodb_table.jit_config.arn,
    aws_dynamodb_table.jit_requests.arn,
    aws_dynamodb_table.jit_audit.arn,
    aws_dynamodb_table.jit_nonces.arn,
  ]

  dynamodb_index_arns = [
    "${aws_dynamodb_table.jit_config.arn}/index/*",
    "${aws_dynamodb_table.jit_requests.arn}/index/*",
    "${aws_dynamodb_table.jit_audit.arn}/index/*",
    "${aws_dynamodb_table.jit_nonces.arn}/index/*",
  ]
}

########################################
# Lambda assume-role policy (shared)
########################################
data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

########################################
# API Lambda execution role
########################################
resource "aws_iam_role" "api_lambda" {
  name               = "${var.environment}-jit-api-lambda"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-api-lambda"
  })
}

data "aws_iam_policy_document" "api_lambda" {
  # DynamoDB — Requests table: read, write, update, query (no Scan, no Delete)
  statement {
    sid    = "DynamoDBRequests"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:Query",
    ]
    resources = [
      aws_dynamodb_table.jit_requests.arn,
      "${aws_dynamodb_table.jit_requests.arn}/index/*",
    ]
  }

  # DynamoDB — Config table: read, write, query
  statement {
    sid    = "DynamoDBConfig"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:Query",
    ]
    resources = [
      aws_dynamodb_table.jit_config.arn,
      "${aws_dynamodb_table.jit_config.arn}/index/*",
    ]
  }

  # DynamoDB — Audit table: write and query only
  statement {
    sid    = "DynamoDBAudit"
    effect = "Allow"
    actions = [
      "dynamodb:PutItem",
      "dynamodb:Query",
    ]
    resources = [
      aws_dynamodb_table.jit_audit.arn,
      "${aws_dynamodb_table.jit_audit.arn}/index/*",
    ]
  }

  # DynamoDB — Nonces table: HMAC replay protection (conditional put + get)
  statement {
    sid    = "DynamoDBNonces"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
    ]
    resources = [
      aws_dynamodb_table.jit_nonces.arn,
    ]
  }

  # SSO account assignment management
  statement {
    sid    = "SSOAdmin"
    effect = "Allow"
    actions = [
      "sso:CreateAccountAssignment",
      "sso:DeleteAccountAssignment",
      "sso:DescribeAccountAssignmentCreationStatus",
      "sso:DescribeAccountAssignmentDeletionStatus",
    ]
    resources = ["*"]
  }

  # Identity Store user lookups
  statement {
    sid    = "IdentityStore"
    effect = "Allow"
    actions = [
      "identitystore:ListUsers",
      "identitystore:GetUserId",
    ]
    resources = ["*"]
  }

  # Secrets Manager read
  statement {
    sid    = "SecretsManager"
    effect = "Allow"
    actions = [
      "secretsmanager:GetSecretValue",
    ]
    resources = [
      aws_secretsmanager_secret.signing_key.arn,
      aws_secretsmanager_secret.callback_signing_key.arn,
    ]
  }

  # S3 artifact bucket read
  statement {
    sid    = "S3Artifacts"
    effect = "Allow"
    actions = [
      "s3:GetObject",
    ]
    resources = [
      "arn:aws:s3:::${var.lambda_artifact_bucket}/*",
    ]
  }

  # CloudWatch Logs
  statement {
    sid    = "CloudWatchLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = [
      "arn:aws:logs:${local.region}:${local.account_id}:log-group:/aws/lambda/${var.environment}-jit-api:*",
    ]
  }

  # Step Functions — start the grant/revoke workflow after approval.
  # Use a constructed ARN to avoid a circular dependency:
  # SFN -> Lambda (invokes) and Lambda IAM -> SFN (start execution).
  statement {
    sid    = "StepFunctions"
    effect = "Allow"
    actions = [
      "states:StartExecution",
    ]
    resources = [
      "arn:aws:states:${local.region}:${local.account_id}:stateMachine:${var.environment}-jit-grant-revoke",
    ]
  }
}

resource "aws_iam_role_policy" "api_lambda" {
  name   = "${var.environment}-jit-api-lambda"
  role   = aws_iam_role.api_lambda.id
  policy = data.aws_iam_policy_document.api_lambda.json
}

# Attach VPC execution role if VPC config is provided
resource "aws_iam_role_policy_attachment" "api_lambda_vpc" {
  count      = var.lambda_vpc_config != null ? 1 : 0
  role       = aws_iam_role.api_lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

########################################
# Reconciler Lambda execution role
########################################
resource "aws_iam_role" "reconciler_lambda" {
  name               = "${var.environment}-jit-reconciler-lambda"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-reconciler-lambda"
  })
}

data "aws_iam_policy_document" "reconciler_lambda" {
  # DynamoDB — Requests table: query (GSI) + conditional update only
  statement {
    sid    = "DynamoDBRequests"
    effect = "Allow"
    actions = [
      "dynamodb:Query",
      "dynamodb:UpdateItem",
    ]
    resources = [
      aws_dynamodb_table.jit_requests.arn,
      "${aws_dynamodb_table.jit_requests.arn}/index/*",
    ]
  }

  # DynamoDB — Audit table: write only
  statement {
    sid    = "DynamoDBAudit"
    effect = "Allow"
    actions = [
      "dynamodb:PutItem",
    ]
    resources = [
      aws_dynamodb_table.jit_audit.arn,
    ]
  }

  # SSO account assignment management
  statement {
    sid    = "SSOAdmin"
    effect = "Allow"
    actions = [
      "sso:CreateAccountAssignment",
      "sso:DeleteAccountAssignment",
      "sso:DescribeAccountAssignmentCreationStatus",
      "sso:DescribeAccountAssignmentDeletionStatus",
    ]
    resources = ["*"]
  }

  # Identity Store user lookups
  statement {
    sid    = "IdentityStore"
    effect = "Allow"
    actions = [
      "identitystore:ListUsers",
      "identitystore:GetUserId",
    ]
    resources = ["*"]
  }

  # Secrets Manager read
  statement {
    sid    = "SecretsManager"
    effect = "Allow"
    actions = [
      "secretsmanager:GetSecretValue",
    ]
    resources = [
      aws_secretsmanager_secret.signing_key.arn,
      aws_secretsmanager_secret.callback_signing_key.arn,
    ]
  }

  # S3 artifact bucket read
  statement {
    sid    = "S3Artifacts"
    effect = "Allow"
    actions = [
      "s3:GetObject",
    ]
    resources = [
      "arn:aws:s3:::${var.lambda_artifact_bucket}/*",
    ]
  }

  # CloudWatch Logs
  statement {
    sid    = "CloudWatchLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = [
      "arn:aws:logs:${local.region}:${local.account_id}:log-group:/aws/lambda/${var.environment}-jit-reconciler:*",
    ]
  }
}

resource "aws_iam_role_policy" "reconciler_lambda" {
  name   = "${var.environment}-jit-reconciler-lambda"
  role   = aws_iam_role.reconciler_lambda.id
  policy = data.aws_iam_policy_document.reconciler_lambda.json
}

# Attach VPC execution role if VPC config is provided
resource "aws_iam_role_policy_attachment" "reconciler_lambda_vpc" {
  count      = var.lambda_vpc_config != null ? 1 : 0
  role       = aws_iam_role.reconciler_lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

########################################
# API Gateway → Lambda invoke permission
########################################
resource "aws_lambda_permission" "api_gateway" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.jit_api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.jit.execution_arn}/*/*"
}

########################################
# Step Functions IAM role
########################################
data "aws_iam_policy_document" "sfn_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["states.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "sfn" {
  name               = "${var.environment}-jit-stepfunctions"
  assume_role_policy = data.aws_iam_policy_document.sfn_assume_role.json

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-stepfunctions"
  })
}

data "aws_iam_policy_document" "sfn" {
  statement {
    sid    = "InvokeLambda"
    effect = "Allow"
    actions = [
      "lambda:InvokeFunction",
    ]
    resources = [
      aws_lambda_function.jit_api.arn,
      "${aws_lambda_function.jit_api.arn}:*",
    ]
  }

  statement {
    sid    = "CloudWatchLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogDelivery",
      "logs:GetLogDelivery",
      "logs:UpdateLogDelivery",
      "logs:DeleteLogDelivery",
      "logs:ListLogDeliveries",
      "logs:PutResourcePolicy",
      "logs:DescribeResourcePolicies",
      "logs:DescribeLogGroups",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "sfn" {
  name   = "${var.environment}-jit-stepfunctions"
  role   = aws_iam_role.sfn.id
  policy = data.aws_iam_policy_document.sfn.json
}
