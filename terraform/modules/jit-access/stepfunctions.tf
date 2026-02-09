########################################
# Step Functions state machine – Grant → Wait → Revoke
########################################
resource "aws_sfn_state_machine" "jit_grant_revoke" {
  name     = "${var.environment}-jit-grant-revoke"
  role_arn = aws_iam_role.sfn.arn
  type     = "STANDARD"

  definition = jsonencode({
    Comment = "JIT access grant-wait-revoke workflow"
    StartAt = "ValidateRequest"
    States = {
      ValidateRequest = {
        Type     = "Task"
        Resource = "arn:aws:states:::lambda:invoke"
        Parameters = {
          FunctionName = aws_lambda_function.jit_api.arn
          Payload = {
            "action"                   = "validate"
            "request_id.$"             = "$.request_id"
            "account_id.$"             = "$.account_id"
            "channel_id.$"             = "$.channel_id"
            "identity_store_user_id.$" = "$.identity_store_user_id"
            "requester_email.$"        = "$.requester_email"
            "duration_seconds.$"       = "$.duration_seconds"
          }
        }
        ResultPath = "$.validation"
        ResultSelector = {
          "payload.$" = "$.Payload"
        }
        Next = "GrantAccess"
      }

      GrantAccess = {
        Type     = "Task"
        Resource = "arn:aws:states:::lambda:invoke"
        Parameters = {
          FunctionName = aws_lambda_function.jit_api.arn
          Payload = {
            "action"                   = "grant"
            "request_id.$"             = "$.request_id"
            "account_id.$"             = "$.account_id"
            "channel_id.$"             = "$.channel_id"
            "identity_store_user_id.$" = "$.identity_store_user_id"
            "requester_email.$"        = "$.requester_email"
            "duration_seconds.$"       = "$.duration_seconds"
          }
        }
        ResultPath = "$.grant_result"
        ResultSelector = {
          "payload.$" = "$.Payload"
        }
        Retry = [
          {
            ErrorEquals     = ["States.TaskFailed", "Lambda.ServiceException", "Lambda.SdkClientException"]
            IntervalSeconds = 5
            MaxAttempts     = 3
            BackoffRate     = 2.0
          }
        ]
        Catch = [
          {
            ErrorEquals = ["States.ALL"]
            ResultPath  = "$.error"
            Next        = "HandleGrantError"
          }
        ]
        Next = "NotifyGranted"
      }

      NotifyGranted = {
        Type     = "Task"
        Resource = "arn:aws:states:::lambda:invoke"
        Parameters = {
          FunctionName = aws_lambda_function.jit_api.arn
          Payload = {
            "action"                   = "notify_granted"
            "request_id.$"             = "$.request_id"
            "account_id.$"             = "$.account_id"
            "channel_id.$"             = "$.channel_id"
            "identity_store_user_id.$" = "$.identity_store_user_id"
            "requester_email.$"        = "$.requester_email"
            "duration_seconds.$"       = "$.duration_seconds"
          }
        }
        ResultPath = "$.notify_grant_result"
        ResultSelector = {
          "payload.$" = "$.Payload"
        }
        # Notification failures must not block the wait→revoke flow.
        Catch = [
          {
            ErrorEquals = ["States.ALL"]
            ResultPath  = "$.notify_grant_error"
            Next        = "WaitForDuration"
          }
        ]
        Next = "WaitForDuration"
      }

      WaitForDuration = {
        Type        = "Wait"
        SecondsPath = "$.duration_seconds"
        Next        = "RevokeAccess"
      }

      RevokeAccess = {
        Type     = "Task"
        Resource = "arn:aws:states:::lambda:invoke"
        Parameters = {
          FunctionName = aws_lambda_function.jit_api.arn
          Payload = {
            "action"                   = "revoke"
            "request_id.$"             = "$.request_id"
            "account_id.$"             = "$.account_id"
            "channel_id.$"             = "$.channel_id"
            "identity_store_user_id.$" = "$.identity_store_user_id"
            "requester_email.$"        = "$.requester_email"
          }
        }
        ResultPath = "$.revoke_result"
        ResultSelector = {
          "payload.$" = "$.Payload"
        }
        Retry = [
          {
            ErrorEquals     = ["States.TaskFailed", "Lambda.ServiceException", "Lambda.SdkClientException"]
            IntervalSeconds = 5
            MaxAttempts     = 3
            BackoffRate     = 2.0
          }
        ]
        Catch = [
          {
            ErrorEquals = ["States.ALL"]
            ResultPath  = "$.error"
            Next        = "HandleRevokeError"
          }
        ]
        Next = "NotifyRevoked"
      }

      NotifyRevoked = {
        Type     = "Task"
        Resource = "arn:aws:states:::lambda:invoke"
        Parameters = {
          FunctionName = aws_lambda_function.jit_api.arn
          Payload = {
            "action"                   = "notify_revoked"
            "request_id.$"             = "$.request_id"
            "account_id.$"             = "$.account_id"
            "channel_id.$"             = "$.channel_id"
            "identity_store_user_id.$" = "$.identity_store_user_id"
            "requester_email.$"        = "$.requester_email"
          }
        }
        ResultPath = "$.notify_revoke_result"
        ResultSelector = {
          "payload.$" = "$.Payload"
        }
        # Notification failures should not prevent successful completion.
        Catch = [
          {
            ErrorEquals = ["States.ALL"]
            ResultPath  = "$.notify_revoke_error"
            Next        = "NotifyRevokedFallback"
          }
        ]
        End = true
      }

      # Terminal state for when revocation notification fails.
      NotifyRevokedFallback = {
        Type = "Succeed"
      }

      HandleGrantError = {
        Type     = "Task"
        Resource = "arn:aws:states:::lambda:invoke"
        Parameters = {
          FunctionName = aws_lambda_function.jit_api.arn
          Payload = {
            "action"                   = "handle_grant_error"
            "request_id.$"             = "$.request_id"
            "account_id.$"             = "$.account_id"
            "channel_id.$"             = "$.channel_id"
            "identity_store_user_id.$" = "$.identity_store_user_id"
            "requester_email.$"        = "$.requester_email"
            "error.$"                  = "$.error"
          }
        }
        ResultPath = "$.error_handler_result"
        ResultSelector = {
          "payload.$" = "$.Payload"
        }
        End = true
      }

      HandleRevokeError = {
        Type     = "Task"
        Resource = "arn:aws:states:::lambda:invoke"
        Parameters = {
          FunctionName = aws_lambda_function.jit_api.arn
          Payload = {
            "action"                   = "handle_revoke_error"
            "request_id.$"             = "$.request_id"
            "account_id.$"             = "$.account_id"
            "channel_id.$"             = "$.channel_id"
            "identity_store_user_id.$" = "$.identity_store_user_id"
            "requester_email.$"        = "$.requester_email"
            "error.$"                  = "$.error"
          }
        }
        ResultPath = "$.error_handler_result"
        ResultSelector = {
          "payload.$" = "$.Payload"
        }
        End = true
      }
    }
  })

  logging_configuration {
    log_destination        = "${aws_cloudwatch_log_group.sfn.arn}:*"
    include_execution_data = true
    level                  = "ERROR"
  }

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-grant-revoke"
  })
}

########################################
# Step Functions log group
########################################
resource "aws_cloudwatch_log_group" "sfn" {
  name              = "/aws/states/${var.environment}-jit-grant-revoke"
  retention_in_days = var.log_retention_days

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-sfn-logs"
  })
}
