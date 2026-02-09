########################################
# jit_config table
########################################
resource "aws_dynamodb_table" "jit_config" {
  name         = "${var.environment}-jit-config"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "channel_id"
  range_key    = "account_id"

  attribute {
    name = "channel_id"
    type = "S"
  }

  attribute {
    name = "account_id"
    type = "S"
  }

  global_secondary_index {
    name            = "gsi_account"
    hash_key        = "account_id"
    projection_type = "ALL"
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-config"
  })
}

########################################
# jit_requests table
########################################
resource "aws_dynamodb_table" "jit_requests" {
  name         = "${var.environment}-jit-requests"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "request_id"

  attribute {
    name = "request_id"
    type = "S"
  }

  attribute {
    name = "channel_id"
    type = "S"
  }

  attribute {
    name = "created_at"
    type = "S"
  }

  attribute {
    name = "account_id"
    type = "S"
  }

  attribute {
    name = "requester_email"
    type = "S"
  }

  attribute {
    name = "status"
    type = "S"
  }

  attribute {
    name = "end_time"
    type = "S"
  }

  global_secondary_index {
    name            = "gsi_channel_created"
    hash_key        = "channel_id"
    range_key       = "created_at"
    projection_type = "ALL"
  }

  global_secondary_index {
    name            = "gsi_account_created"
    hash_key        = "account_id"
    range_key       = "created_at"
    projection_type = "ALL"
  }

  global_secondary_index {
    name            = "gsi_requester_created"
    hash_key        = "requester_email"
    range_key       = "created_at"
    projection_type = "ALL"
  }

  global_secondary_index {
    name            = "gsi_status_endtime"
    hash_key        = "status"
    range_key       = "end_time"
    projection_type = "ALL"
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-requests"
  })
}

########################################
# jit_audit table
########################################
resource "aws_dynamodb_table" "jit_audit" {
  name         = "${var.environment}-jit-audit"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "request_id"
  range_key    = "event_time_event_id"

  attribute {
    name = "request_id"
    type = "S"
  }

  attribute {
    name = "event_time_event_id"
    type = "S"
  }

  attribute {
    name = "account_id"
    type = "S"
  }

  attribute {
    name = "channel_id"
    type = "S"
  }

  global_secondary_index {
    name            = "gsi_account_event"
    hash_key        = "account_id"
    range_key       = "event_time_event_id"
    projection_type = "ALL"
  }

  global_secondary_index {
    name            = "gsi_channel_event"
    hash_key        = "channel_id"
    range_key       = "event_time_event_id"
    projection_type = "ALL"
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-audit"
  })
}

########################################
# jit_nonces table
########################################
resource "aws_dynamodb_table" "jit_nonces" {
  name         = "${var.environment}-jit-nonces"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "key_id"
  range_key    = "nonce"

  attribute {
    name = "key_id"
    type = "S"
  }

  attribute {
    name = "nonce"
    type = "S"
  }

  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-nonces"
  })
}
