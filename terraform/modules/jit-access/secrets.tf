########################################
# Signing secret – HMAC validation of plugin → backend requests
########################################
resource "aws_secretsmanager_secret" "signing_key" {
  name                    = "${var.environment}/jit-access/signing-key"
  description             = "HMAC signing key used to validate inbound requests from the Mattermost plugin."
  recovery_window_in_days = 30

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-signing-key"
  })
}

resource "aws_secretsmanager_secret_version" "signing_key" {
  secret_id = aws_secretsmanager_secret.signing_key.id

  # Generate a random initial value; operators should rotate via console / CLI.
  secret_string = random_password.signing_key.result

  lifecycle {
    ignore_changes = [secret_string]
  }
}

resource "random_password" "signing_key" {
  length  = 64
  special = false
}

########################################
# Callback signing secret – HMAC signing of backend → plugin webhooks
########################################
resource "aws_secretsmanager_secret" "callback_signing_key" {
  name                    = "${var.environment}/jit-access/callback-signing-key"
  description             = "HMAC signing key used to sign outbound webhook callbacks to the Mattermost plugin."
  recovery_window_in_days = 30

  tags = merge(var.tags, {
    Name = "${var.environment}-jit-callback-signing-key"
  })
}

resource "aws_secretsmanager_secret_version" "callback_signing_key" {
  secret_id = aws_secretsmanager_secret.callback_signing_key.id

  secret_string = random_password.callback_signing_key.result

  lifecycle {
    ignore_changes = [secret_string]
  }
}

resource "random_password" "callback_signing_key" {
  length  = 64
  special = false
}
