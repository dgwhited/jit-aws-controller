########################################
# HTTP API
########################################
resource "aws_apigatewayv2_api" "jit" {
  name          = "${var.environment}-jit-access"
  protocol_type = "HTTP"
  description   = "JIT Access Controller HTTP API"

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-access-api"
  })
}

########################################
# Lambda integration
########################################
resource "aws_apigatewayv2_integration" "api_lambda" {
  api_id                 = aws_apigatewayv2_api.jit.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.jit_api.invoke_arn
  integration_method     = "POST"
  payload_format_version = "2.0"
}

########################################
# Routes
########################################
resource "aws_apigatewayv2_route" "post_requests" {
  api_id    = aws_apigatewayv2_api.jit.id
  route_key = "POST /requests"
  target    = "integrations/${aws_apigatewayv2_integration.api_lambda.id}"
}

resource "aws_apigatewayv2_route" "post_approve" {
  api_id    = aws_apigatewayv2_api.jit.id
  route_key = "POST /requests/{id}/approve"
  target    = "integrations/${aws_apigatewayv2_integration.api_lambda.id}"
}

resource "aws_apigatewayv2_route" "post_deny" {
  api_id    = aws_apigatewayv2_api.jit.id
  route_key = "POST /requests/{id}/deny"
  target    = "integrations/${aws_apigatewayv2_integration.api_lambda.id}"
}

resource "aws_apigatewayv2_route" "post_revoke" {
  api_id    = aws_apigatewayv2_api.jit.id
  route_key = "POST /requests/{id}/revoke"
  target    = "integrations/${aws_apigatewayv2_integration.api_lambda.id}"
}

resource "aws_apigatewayv2_route" "get_requests" {
  api_id    = aws_apigatewayv2_api.jit.id
  route_key = "GET /requests"
  target    = "integrations/${aws_apigatewayv2_integration.api_lambda.id}"
}

resource "aws_apigatewayv2_route" "get_request_by_id" {
  api_id    = aws_apigatewayv2_api.jit.id
  route_key = "GET /requests/{id}"
  target    = "integrations/${aws_apigatewayv2_integration.api_lambda.id}"
}

resource "aws_apigatewayv2_route" "post_config_bind" {
  api_id    = aws_apigatewayv2_api.jit.id
  route_key = "POST /config/bind"
  target    = "integrations/${aws_apigatewayv2_integration.api_lambda.id}"
}

resource "aws_apigatewayv2_route" "post_config_approvers" {
  api_id    = aws_apigatewayv2_api.jit.id
  route_key = "POST /config/approvers"
  target    = "integrations/${aws_apigatewayv2_integration.api_lambda.id}"
}

resource "aws_apigatewayv2_route" "get_config_accounts" {
  api_id    = aws_apigatewayv2_api.jit.id
  route_key = "GET /config/accounts"
  target    = "integrations/${aws_apigatewayv2_integration.api_lambda.id}"
}

########################################
# Default stage with auto-deploy
########################################
resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.jit.id
  name        = "$default"
  auto_deploy = true

  default_route_settings {
    throttling_burst_limit = var.api_throttle_burst_limit
    throttling_rate_limit  = var.api_throttle_rate_limit
  }

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.api_gateway.arn
    format = jsonencode({
      requestId        = "$context.requestId"
      ip               = "$context.identity.sourceIp"
      requestTime      = "$context.requestTime"
      httpMethod       = "$context.httpMethod"
      routeKey         = "$context.routeKey"
      status           = "$context.status"
      protocol         = "$context.protocol"
      responseLength   = "$context.responseLength"
      integrationError = "$context.integrationErrorMessage"
    })
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-access-api-default"
  })
}

########################################
# API Gateway access log group
########################################
resource "aws_cloudwatch_log_group" "api_gateway" {
  name              = "/aws/apigateway/${var.environment}-jit-access"
  retention_in_days = var.log_retention_days

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-access-api-logs"
  })
}
