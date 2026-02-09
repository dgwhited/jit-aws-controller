package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"

	"github.com/dgwhited/jit-aws-controller/internal/models"
)

// SFNClient implements SFNStarter using the real AWS Step Functions client.
type SFNClient struct {
	Client          *sfn.Client
	StateMachineARN string
}

// StartExecution starts a Step Functions execution for the grant-wait-revoke workflow.
func (s *SFNClient) StartExecution(ctx context.Context, input models.StepFunctionInput) error {
	return StartGrantWorkflow(ctx, s.Client, s.StateMachineARN, input)
}

// StartGrantWorkflow starts a Step Functions execution for the grant-wait-revoke workflow.
func StartGrantWorkflow(ctx context.Context, sfnClient *sfn.Client, stateMachineARN string, input models.StepFunctionInput) error {
	// Convert duration to seconds for the Step Functions Wait state.
	type sfnPayload struct {
		RequestID           string `json:"request_id"`
		AccountID           string `json:"account_id"`
		ChannelID           string `json:"channel_id"`
		IdentityStoreUserID string `json:"identity_store_user_id"`
		DurationSeconds     int    `json:"duration_seconds"`
		RequesterEmail      string `json:"requester_email"`
	}

	payload := sfnPayload{
		RequestID:           input.RequestID,
		AccountID:           input.AccountID,
		ChannelID:           input.ChannelID,
		IdentityStoreUserID: input.IdentityStoreUserID,
		DurationSeconds:     input.DurationMinutes * 60,
		RequesterEmail:      input.RequesterEmail,
	}

	inputJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal step function input: %w", err)
	}

	execName := input.RequestID

	_, err = sfnClient.StartExecution(ctx, &sfn.StartExecutionInput{
		StateMachineArn: &stateMachineARN,
		Name:            &execName,
		Input:           aws.String(string(inputJSON)),
	})
	if err != nil {
		return fmt.Errorf("start step function execution: %w", err)
	}

	slog.Info("step function execution started",
		"request_id", input.RequestID,
		"state_machine", stateMachineARN,
	)
	return nil
}
