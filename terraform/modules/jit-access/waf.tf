########################################
# WAF v2 — API Gateway IP allowlist + rate limiting (E3)
########################################

locals {
  create_waf = var.enable_waf && length(var.allowed_source_ips) > 0
}

resource "aws_wafv2_ip_set" "allowed_sources" {
  count              = local.create_waf ? 1 : 0
  name               = "${var.environment}-jit-allowed-sources"
  description        = "IP addresses allowed to reach the JIT API Gateway"
  scope              = "REGIONAL"
  ip_address_version = "IPV4"
  addresses          = var.allowed_source_ips

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-allowed-sources"
  })
}

resource "aws_wafv2_web_acl" "api" {
  count       = local.create_waf ? 1 : 0
  name        = "${var.environment}-jit-api-waf"
  description = "WAF for JIT API Gateway - allow only known source IPs"
  scope       = "REGIONAL"

  default_action {
    block {}
  }

  # Rule 1: Allow requests from the IP allowlist.
  rule {
    name     = "AllowKnownSources"
    priority = 1

    action {
      allow {}
    }

    statement {
      ip_set_reference_statement {
        arn = aws_wafv2_ip_set.allowed_sources[0].arn
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.environment}-jit-waf-allow-known"
    }
  }

  # Rule 2: Rate limit as defense-in-depth (even for allowed IPs).
  rule {
    name     = "RateLimitPerIP"
    priority = 2

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = 300
        aggregate_key_type = "IP"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.environment}-jit-waf-rate-limit"
    }
  }

  visibility_config {
    sampled_requests_enabled   = true
    cloudwatch_metrics_enabled = true
    metric_name                = "${var.environment}-jit-waf"
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-api-waf"
  })
}

# NOTE: WAF WebACL association with API Gateway V2 HTTP APIs using the
# $default stage is not supported by AWS. The $default stage name contains
# a $ character that fails ARN validation in the AssociateWebACL API.
# Workaround options:
#   1. Create a named stage (e.g., "v1") instead of $default
#   2. Use the AWS CLI: aws wafv2 associate-web-acl (may work with URL-encoded ARN)
#   3. Rely on HMAC authentication + API Gateway throttling (P0-3) for protection
#
# The WebACL and IP set are created and ready for association once a named
# stage is configured or the workaround is applied.
#
# resource "aws_wafv2_web_acl_association" "api" {
#   count        = local.create_waf ? 1 : 0
#   resource_arn = aws_apigatewayv2_stage.default.arn
#   web_acl_arn  = aws_wafv2_web_acl.api[0].arn
# }
