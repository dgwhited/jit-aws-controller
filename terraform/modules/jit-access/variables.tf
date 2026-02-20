variable "environment" {
  description = "Environment name used as a prefix for all resources."
  type        = string
}

variable "permission_set_arn" {
  description = "ARN of an existing SSO permission set. Required when create_permission_set is false."
  type        = string
  default     = ""
}

variable "create_permission_set" {
  description = "Whether to create a new SSO permission set for JIT access."
  type        = bool
  default     = false
}

variable "permission_set_name" {
  description = "Name of the SSO permission set to create (only used when create_permission_set is true)."
  type        = string
  default     = "JIT-Admin"
}

variable "permission_set_session_duration" {
  description = "Maximum session duration for the JIT permission set (ISO 8601)."
  type        = string
  default     = "PT1H"
}

variable "permission_set_managed_policy_arns" {
  description = "List of AWS managed policy ARNs to attach to the JIT permission set."
  type        = list(string)
  default     = ["arn:aws:iam::aws:policy/AdministratorAccess"]
}

variable "plugin_webhook_url" {
  description = "Mattermost plugin callback endpoint for status notifications."
  type        = string
}

variable "sso_instance_arn" {
  description = "ARN of the AWS SSO (IAM Identity Center) instance."
  type        = string
}

variable "identity_store_id" {
  description = "ID of the IAM Identity Center identity store."
  type        = string
}

variable "lambda_artifact_bucket" {
  description = "Name of the S3 bucket containing Lambda deployment packages."
  type        = string
}

variable "api_lambda_s3_key" {
  description = "S3 object key for the API Lambda deployment package."
  type        = string
}

variable "reconciler_lambda_s3_key" {
  description = "S3 object key for the Reconciler Lambda deployment package."
  type        = string
}

variable "alarm_sns_topic_arn" {
  description = "SNS topic ARN for CloudWatch alarm notifications. Leave empty to disable alarm actions."
  type        = string
  default     = ""
}

variable "lambda_vpc_config" {
  description = "VPC configuration for Lambda functions to reach Mattermost in a private network."
  type = object({
    subnet_ids         = list(string)
    security_group_ids = list(string)
  })
  default = null
}

variable "tags" {
  description = "Tags to apply to all resources."
  type        = map(string)
  default     = {}
}

variable "log_retention_days" {
  description = "Number of days to retain CloudWatch log events."
  type        = number
  default     = 30
}

variable "create_artifact_bucket" {
  description = "Whether to create an S3 bucket for Lambda deployment artifacts."
  type        = bool
  default     = false
}

########################################
# API Gateway security
########################################

variable "api_throttle_burst_limit" {
  description = "API Gateway max concurrent request burst limit."
  type        = number
  default     = 50
}

variable "api_throttle_rate_limit" {
  description = "API Gateway sustained requests-per-second limit."
  type        = number
  default     = 20
}

variable "allowed_source_ips" {
  description = "List of CIDR blocks allowed to reach the API Gateway (used by WAF). Leave empty to skip WAF creation."
  type        = list(string)
  default     = []
}

variable "enable_waf" {
  description = "Whether to create and associate a WAF WebACL with the API Gateway."
  type        = bool
  default     = true
}
