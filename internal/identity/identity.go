package identity

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/identitystore"
	iddoc "github.com/aws/aws-sdk-go-v2/service/identitystore/document"
	idtypes "github.com/aws/aws-sdk-go-v2/service/identitystore/types"
	"github.com/aws/aws-sdk-go-v2/service/ssoadmin"
	ssotypes "github.com/aws/aws-sdk-go-v2/service/ssoadmin/types"
)

// Client wraps IAM Identity Center operations for JIT access.
type Client struct {
	ssoAdmin         *ssoadmin.Client
	identityStore    *identitystore.Client
	ssoInstanceARN   string
	identityStoreID  string
	permissionSetARN string
}

// NewClient creates a new Identity Center client.
func NewClient(ssoAdmin *ssoadmin.Client, identityStore *identitystore.Client, ssoInstanceARN, identityStoreID, permissionSetARN string) *Client {
	return &Client{
		ssoAdmin:         ssoAdmin,
		identityStore:    identityStore,
		ssoInstanceARN:   ssoInstanceARN,
		identityStoreID:  identityStoreID,
		permissionSetARN: permissionSetARN,
	}
}

// LookupUserByEmail finds the Identity Store user ID for the given email address.
// It first tries to match by UserName (common when UserName is set to email),
// then falls back to matching by the unique email attribute via GetUserId.
func (c *Client) LookupUserByEmail(ctx context.Context, email string) (string, error) {
	// First attempt: look up by UserName (many orgs set UserName = email).
	listOut, err := c.identityStore.ListUsers(ctx, &identitystore.ListUsersInput{
		IdentityStoreId: &c.identityStoreID,
		Filters: []idtypes.Filter{
			{
				AttributePath:  aws.String("UserName"),
				AttributeValue: aws.String(email),
			},
		},
	})
	if err != nil {
		slog.Warn("ListUsers by UserName failed, will try by email", "email", email, "error", err)
	} else if len(listOut.Users) > 0 {
		userID := aws.ToString(listOut.Users[0].UserId)
		slog.Info("looked up identity store user by UserName",
			"email", email,
			"user_id", userID,
		)
		return userID, nil
	}

	// Second attempt: look up by email attribute using GetUserId.
	getOut, err := c.identityStore.GetUserId(ctx, &identitystore.GetUserIdInput{
		IdentityStoreId: &c.identityStoreID,
		AlternateIdentifier: &idtypes.AlternateIdentifierMemberUniqueAttribute{
			Value: idtypes.UniqueAttribute{
				AttributePath:  aws.String("emails.value"),
				AttributeValue: iddoc.NewLazyDocument(email),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("no Identity Store user found for email %s (tried UserName and emails.value): %w", email, err)
	}

	userID := aws.ToString(getOut.UserId)
	slog.Info("looked up identity store user by email attribute",
		"email", email,
		"user_id", userID,
	)
	return userID, nil
}

// retryBackoffs defines the sleep durations between retries: 1s, 4s, 16s.
var retryBackoffs = []time.Duration{
	1 * time.Second,
	4 * time.Second,
	16 * time.Second,
}

// GrantAccess creates a permission set assignment for a user to an AWS account.
// It polls for completion and retries up to 3 times with exponential backoff.
func (c *Client) GrantAccess(ctx context.Context, accountID, userID string) error {
	var lastErr error
	for attempt := 0; attempt <= len(retryBackoffs); attempt++ {
		if attempt > 0 {
			slog.Warn("retrying GrantAccess",
				"attempt", attempt,
				"account_id", accountID,
				"user_id", userID,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryBackoffs[attempt-1]):
			}
		}

		err := c.grantAccessOnce(ctx, accountID, userID)
		if err == nil {
			return nil
		}
		lastErr = err
		slog.Error("GrantAccess attempt failed",
			"attempt", attempt,
			"error", err,
		)
	}
	return fmt.Errorf("GrantAccess failed after retries: %w", lastErr)
}

func (c *Client) grantAccessOnce(ctx context.Context, accountID, userID string) error {
	out, err := c.ssoAdmin.CreateAccountAssignment(ctx, &ssoadmin.CreateAccountAssignmentInput{
		InstanceArn:      &c.ssoInstanceARN,
		PermissionSetArn: &c.permissionSetARN,
		PrincipalId:      &userID,
		PrincipalType:    ssotypes.PrincipalTypeUser,
		TargetId:         &accountID,
		TargetType:       ssotypes.TargetTypeAwsAccount,
	})
	if err != nil {
		return fmt.Errorf("CreateAccountAssignment: %w", err)
	}

	if out.AccountAssignmentCreationStatus == nil {
		return fmt.Errorf("CreateAccountAssignment returned nil status")
	}

	requestID := aws.ToString(out.AccountAssignmentCreationStatus.RequestId)
	return c.pollCreationStatus(ctx, requestID)
}

func (c *Client) pollCreationStatus(ctx context.Context, requestID string) error {
	for i := 0; i < 30; i++ {
		out, err := c.ssoAdmin.DescribeAccountAssignmentCreationStatus(ctx, &ssoadmin.DescribeAccountAssignmentCreationStatusInput{
			InstanceArn:                        &c.ssoInstanceARN,
			AccountAssignmentCreationRequestId: &requestID,
		})
		if err != nil {
			return fmt.Errorf("DescribeAccountAssignmentCreationStatus: %w", err)
		}
		if out.AccountAssignmentCreationStatus == nil {
			return fmt.Errorf("nil creation status in poll response")
		}

		status := out.AccountAssignmentCreationStatus.Status
		switch status {
		case ssotypes.StatusValuesSucceeded:
			slog.Info("account assignment creation succeeded", "request_id", requestID)
			return nil
		case ssotypes.StatusValuesFailed:
			reason := aws.ToString(out.AccountAssignmentCreationStatus.FailureReason)
			return fmt.Errorf("account assignment creation failed: %s", reason)
		case ssotypes.StatusValuesInProgress:
			// Continue polling.
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("account assignment creation timed out for request %s", requestID)
}

// RevokeAccess deletes a permission set assignment for a user from an AWS account.
// It polls for completion and retries up to 3 times with exponential backoff.
// The operation is idempotent: if the assignment doesn't exist, it returns nil.
func (c *Client) RevokeAccess(ctx context.Context, accountID, userID string) error {
	var lastErr error
	for attempt := 0; attempt <= len(retryBackoffs); attempt++ {
		if attempt > 0 {
			slog.Warn("retrying RevokeAccess",
				"attempt", attempt,
				"account_id", accountID,
				"user_id", userID,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryBackoffs[attempt-1]):
			}
		}

		err := c.revokeAccessOnce(ctx, accountID, userID)
		if err == nil {
			return nil
		}
		lastErr = err
		slog.Error("RevokeAccess attempt failed",
			"attempt", attempt,
			"error", err,
		)
	}
	return fmt.Errorf("RevokeAccess failed after retries: %w", lastErr)
}

func (c *Client) revokeAccessOnce(ctx context.Context, accountID, userID string) error {
	out, err := c.ssoAdmin.DeleteAccountAssignment(ctx, &ssoadmin.DeleteAccountAssignmentInput{
		InstanceArn:      &c.ssoInstanceARN,
		PermissionSetArn: &c.permissionSetARN,
		PrincipalId:      &userID,
		PrincipalType:    ssotypes.PrincipalTypeUser,
		TargetId:         &accountID,
		TargetType:       ssotypes.TargetTypeAwsAccount,
	})
	if err != nil {
		// If the assignment doesn't exist, treat as success (idempotent).
		// AWS returns a ConflictException or ResourceNotFoundException when
		// the assignment is already deleted.
		errMsg := err.Error()
		if strings.Contains(errMsg, "ConflictException") ||
			strings.Contains(errMsg, "ResourceNotFoundException") ||
			strings.Contains(errMsg, "does not exist") {
			slog.Info("assignment already deleted, treating as success",
				"account_id", accountID,
				"user_id", userID,
			)
			return nil
		}
		return fmt.Errorf("DeleteAccountAssignment: %w", err)
	}

	if out.AccountAssignmentDeletionStatus == nil {
		return fmt.Errorf("DeleteAccountAssignment returned nil status")
	}

	requestID := aws.ToString(out.AccountAssignmentDeletionStatus.RequestId)
	return c.pollDeletionStatus(ctx, requestID)
}

func (c *Client) pollDeletionStatus(ctx context.Context, requestID string) error {
	for i := 0; i < 30; i++ {
		out, err := c.ssoAdmin.DescribeAccountAssignmentDeletionStatus(ctx, &ssoadmin.DescribeAccountAssignmentDeletionStatusInput{
			InstanceArn:                        &c.ssoInstanceARN,
			AccountAssignmentDeletionRequestId: &requestID,
		})
		if err != nil {
			return fmt.Errorf("DescribeAccountAssignmentDeletionStatus: %w", err)
		}
		if out.AccountAssignmentDeletionStatus == nil {
			return fmt.Errorf("nil deletion status in poll response")
		}

		status := out.AccountAssignmentDeletionStatus.Status
		switch status {
		case ssotypes.StatusValuesSucceeded:
			slog.Info("account assignment deletion succeeded", "request_id", requestID)
			return nil
		case ssotypes.StatusValuesFailed:
			reason := aws.ToString(out.AccountAssignmentDeletionStatus.FailureReason)
			return fmt.Errorf("account assignment deletion failed: %s", reason)
		case ssotypes.StatusValuesInProgress:
			// Continue polling.
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("account assignment deletion timed out for request %s", requestID)
}
