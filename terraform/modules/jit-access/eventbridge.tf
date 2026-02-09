########################################
# EventBridge rule – Reconciler schedule (every 15 minutes)
########################################
resource "aws_cloudwatch_event_rule" "reconciler_schedule" {
  name                = "${var.environment}-jit-reconciler-schedule"
  description         = "Triggers the JIT reconciler Lambda every 15 minutes to clean up expired access."
  schedule_expression = "rate(15 minutes)"

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-reconciler-schedule"
  })
}

resource "aws_cloudwatch_event_target" "reconciler" {
  rule      = aws_cloudwatch_event_rule.reconciler_schedule.name
  target_id = "${var.environment}-jit-reconciler"
  arn       = aws_lambda_function.jit_reconciler.arn
}

resource "aws_lambda_permission" "eventbridge_reconciler" {
  statement_id  = "AllowEventBridgeInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.jit_reconciler.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.reconciler_schedule.arn
}
