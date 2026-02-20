########################################
# Optional: Create JIT permission set
########################################

resource "aws_ssoadmin_permission_set" "jit" {
  count = var.create_permission_set ? 1 : 0

  name             = "${var.environment}-${var.permission_set_name}"
  instance_arn     = var.sso_instance_arn
  description      = "JIT Access permission set managed by Terraform"
  session_duration = var.permission_set_session_duration

  tags = merge(var.tags, {
    Name = "${var.environment}-${var.permission_set_name}"
  })
}

resource "aws_ssoadmin_managed_policy_attachment" "jit" {
  count = var.create_permission_set ? length(var.permission_set_managed_policy_arns) : 0

  instance_arn       = var.sso_instance_arn
  permission_set_arn = aws_ssoadmin_permission_set.jit[0].arn
  managed_policy_arn = var.permission_set_managed_policy_arns[count.index]
}
