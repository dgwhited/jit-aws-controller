package audit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/dgwhited/jit-aws-controller/internal/dynamo"
	"github.com/dgwhited/jit-aws-controller/internal/models"
)

// Logger records audit events for JIT request state transitions.
type Logger struct {
	db *dynamo.Client
}

// NewLogger creates a new audit logger backed by DynamoDB.
func NewLogger(db *dynamo.Client) *Logger {
	return &Logger{db: db}
}

// Log records an audit event with auto-generated event ID and timestamp.
func (l *Logger) Log(ctx context.Context, requestID, eventType, accountID, channelID, actorMMUserID, actorEmail string, details map[string]string) error {
	eventID := uuid.New().String()
	eventTime := time.Now().UTC().Format(time.RFC3339)
	sortKey := eventTime + "#" + eventID

	event := &models.AuditEvent{
		RequestID:        requestID,
		EventTimeEventID: sortKey,
		EventID:          eventID,
		EventTime:        eventTime,
		EventType:        eventType,
		AccountID:        accountID,
		ChannelID:        channelID,
		ActorMMUserID:    actorMMUserID,
		ActorEmail:       actorEmail,
		Details:          details,
	}

	if err := l.db.PutAuditEvent(ctx, event); err != nil {
		slog.Error("failed to write audit event",
			"request_id", requestID,
			"event_type", eventType,
			"error", err,
		)
		return fmt.Errorf("audit log: %w", err)
	}

	slog.Info("audit event recorded",
		"request_id", requestID,
		"event_type", eventType,
		"event_id", eventID,
	)
	return nil
}
