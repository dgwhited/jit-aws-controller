locals {
  alarm_actions = var.alarm_sns_topic_arn != "" ? [var.alarm_sns_topic_arn] : []
}

########################################
# Metric filters
########################################

# Failed grant/revoke operations – filter on API Lambda logs
resource "aws_cloudwatch_log_metric_filter" "failed_grant_revoke" {
  name           = "${var.environment}-jit-failed-grant-revoke"
  log_group_name = aws_cloudwatch_log_group.api_lambda.name
  pattern        = "{ $.level = \"ERROR\" && ($.action = \"grant\" || $.action = \"revoke\") }"

  metric_transformation {
    name          = "FailedGrantRevokeOperations"
    namespace     = "${var.environment}/JITAccess"
    value         = "1"
    default_value = "0"
  }
}

# HMAC validation failures
resource "aws_cloudwatch_log_metric_filter" "hmac_validation_failures" {
  name           = "${var.environment}-jit-hmac-validation-failure"
  log_group_name = aws_cloudwatch_log_group.api_lambda.name
  pattern        = "{ $.level = \"ERROR\" && $.message = \"*HMAC*validation*\" }"

  metric_transformation {
    name          = "HMACValidationFailures"
    namespace     = "${var.environment}/JITAccess"
    value         = "1"
    default_value = "0"
  }
}

# Reconciler errors
resource "aws_cloudwatch_log_metric_filter" "reconciler_errors" {
  name           = "${var.environment}-jit-reconciler-errors"
  log_group_name = aws_cloudwatch_log_group.reconciler_lambda.name
  pattern        = "{ $.level = \"ERROR\" }"

  metric_transformation {
    name          = "ReconcilerErrors"
    namespace     = "${var.environment}/JITAccess"
    value         = "1"
    default_value = "0"
  }
}

########################################
# Alarms – Failed grant/revoke operations
########################################
resource "aws_cloudwatch_metric_alarm" "failed_grant_revoke" {
  alarm_name          = "${var.environment}-jit-failed-grant-revoke"
  alarm_description   = "One or more JIT grant/revoke operations have failed."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "FailedGrantRevokeOperations"
  namespace           = "${var.environment}/JITAccess"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-failed-grant-revoke"
  })
}

########################################
# Alarms – Reconciler errors
########################################
resource "aws_cloudwatch_metric_alarm" "reconciler_errors" {
  alarm_name          = "${var.environment}-jit-reconciler-errors"
  alarm_description   = "The JIT reconciler Lambda has encountered errors."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ReconcilerErrors"
  namespace           = "${var.environment}/JITAccess"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-reconciler-errors"
  })
}

########################################
# Alarms – HMAC validation failures
########################################
resource "aws_cloudwatch_metric_alarm" "hmac_validation_failures" {
  alarm_name          = "${var.environment}-jit-hmac-validation-failures"
  alarm_description   = "HMAC validation failures detected – possible unauthorized access attempts."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "HMACValidationFailures"
  namespace           = "${var.environment}/JITAccess"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-hmac-validation-failures"
  })
}

########################################
# Alarms – Step Function execution failures
########################################
resource "aws_cloudwatch_metric_alarm" "sfn_execution_failures" {
  alarm_name          = "${var.environment}-jit-sfn-execution-failures"
  alarm_description   = "JIT grant-revoke Step Function executions are failing."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ExecutionsFailed"
  namespace           = "AWS/States"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions

  dimensions = {
    StateMachineArn = aws_sfn_state_machine.jit_grant_revoke.arn
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-sfn-execution-failures"
  })
}

########################################
# Alarms – Step Function execution timeouts
########################################
resource "aws_cloudwatch_metric_alarm" "sfn_execution_timeouts" {
  alarm_name          = "${var.environment}-jit-sfn-execution-timeouts"
  alarm_description   = "JIT grant-revoke Step Function executions are timing out."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ExecutionsTimedOut"
  namespace           = "AWS/States"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions

  dimensions = {
    StateMachineArn = aws_sfn_state_machine.jit_grant_revoke.arn
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-sfn-execution-timeouts"
  })
}

########################################
# Alarms – API Lambda error rate
########################################
resource "aws_cloudwatch_metric_alarm" "api_lambda_errors" {
  alarm_name          = "${var.environment}-jit-api-lambda-errors"
  alarm_description   = "JIT API Lambda error rate is elevated."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 5
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions

  dimensions = {
    FunctionName = aws_lambda_function.jit_api.function_name
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-api-lambda-errors"
  })
}

########################################
# Alarms – API Lambda duration
########################################
resource "aws_cloudwatch_metric_alarm" "api_lambda_duration" {
  alarm_name          = "${var.environment}-jit-api-lambda-duration"
  alarm_description   = "JIT API Lambda p99 duration is approaching the timeout."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "Duration"
  namespace           = "AWS/Lambda"
  period              = 300
  extended_statistic  = "p99"
  threshold           = 25000 # 25s against 30s timeout
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions

  dimensions = {
    FunctionName = aws_lambda_function.jit_api.function_name
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-api-lambda-duration"
  })
}

########################################
# Alarms – Reconciler Lambda error rate
########################################
resource "aws_cloudwatch_metric_alarm" "reconciler_lambda_errors" {
  alarm_name          = "${var.environment}-jit-reconciler-lambda-errors"
  alarm_description   = "JIT Reconciler Lambda error rate is elevated."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 2
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions

  dimensions = {
    FunctionName = aws_lambda_function.jit_reconciler.function_name
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-reconciler-lambda-errors"
  })
}

########################################
# Alarms – Reconciler Lambda duration
########################################
resource "aws_cloudwatch_metric_alarm" "reconciler_lambda_duration" {
  alarm_name          = "${var.environment}-jit-reconciler-lambda-duration"
  alarm_description   = "JIT Reconciler Lambda p99 duration is approaching the timeout."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "Duration"
  namespace           = "AWS/Lambda"
  period              = 300
  extended_statistic  = "p99"
  threshold           = 250000 # 250s against 300s timeout
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions

  dimensions = {
    FunctionName = aws_lambda_function.jit_reconciler.function_name
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-reconciler-lambda-duration"
  })
}

########################################
# Alarms – API Gateway 4xx rate
########################################
resource "aws_cloudwatch_metric_alarm" "apigw_4xx" {
  alarm_name          = "${var.environment}-jit-apigw-4xx-rate"
  alarm_description   = "JIT API Gateway 4xx error rate is elevated."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "4xx"
  namespace           = "AWS/ApiGateway"
  period              = 300
  statistic           = "Sum"
  threshold           = 50
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions

  dimensions = {
    ApiId = aws_apigatewayv2_api.jit.id
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-apigw-4xx-rate"
  })
}

########################################
# Alarms – API Gateway 5xx rate
########################################
resource "aws_cloudwatch_metric_alarm" "apigw_5xx" {
  alarm_name          = "${var.environment}-jit-apigw-5xx-rate"
  alarm_description   = "JIT API Gateway 5xx error rate is elevated – backend failures."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "5xx"
  namespace           = "AWS/ApiGateway"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions

  dimensions = {
    ApiId = aws_apigatewayv2_api.jit.id
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-apigw-5xx-rate"
  })
}
