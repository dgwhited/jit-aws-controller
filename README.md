# jit-aws-controller

Serverless backend for JIT AWS access management via IAM Identity Center.

This controller is designed to work with the [mattermost-plugin-aws-jit-access](https://github.com/dgwhited/jit-mm-plugin) Mattermost plugin, which provides the chat-based interface for requesting and approving temporary AWS access. The plugin sends requests to this controller's API, and this controller sends webhook notifications back to the plugin on state transitions.

## Demo

![JIT Access Demo](demo.gif)

## Architecture

- **API Lambda** (`cmd/api`) -- Handles all HTTP requests through API Gateway V2.
- **Reconciler Lambda** (`cmd/reconciler`) -- Removes expired permission sets on a schedule.
- **Step Functions** -- Orchestrates the approval workflow and timed revocation.
- **DynamoDB** -- Stores access requests, channel-account bindings, and approver configurations.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/requests` | Create a new access request |
| POST | `/requests/{id}/approve` | Approve a pending request |
| POST | `/requests/{id}/deny` | Deny a pending request |
| POST | `/requests/{id}/revoke` | Revoke an active request |
| GET | `/requests` | List requests (with query filters) |
| POST | `/config/bind` | Bind an AWS account to a channel |
| POST | `/config/approvers` | Set approvers for a channel |
| GET | `/config/accounts` | Get bound accounts for a channel |

## Terraform Module

Infrastructure is defined in `terraform/modules/jit-access/`. This module provisions API Gateway, Lambda functions, Step Functions, DynamoDB tables, IAM roles, EventBridge rules, CloudWatch log groups, S3 buckets, and Secrets Manager entries.

## Development

```sh
# Run tests
go test -v -race ./...

# Build Lambda binaries (Linux amd64, static)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bootstrap ./cmd/api
zip jit-api.zip bootstrap

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bootstrap ./cmd/reconciler
zip jit-reconciler.zip bootstrap

# Validate Terraform
cd terraform/modules/jit-access
terraform init -backend=false
terraform validate
```

## CI/CD

GitHub Actions runs lint, test, Terraform validation, and build on every push to `main` and on pull requests. Tagged releases (`v*`) create a GitHub Release with the Lambda zip artifacts (`jit-api.zip`, `jit-reconciler.zip`) attached.

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
