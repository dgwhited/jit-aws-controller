package models

// Status constants
const (
	StatusPending  = "PENDING"
	StatusApproved = "APPROVED"
	StatusDenied   = "DENIED"
	StatusGranted  = "GRANTED"
	StatusRevoked  = "REVOKED"
	StatusExpired  = "EXPIRED"
	StatusError    = "ERROR"
)

// Event type constants
const (
	EventRequested = "REQUESTED"
	EventApproved  = "APPROVED"
	EventDenied    = "DENIED"
	EventGranted   = "GRANTED"
	EventRevoked   = "REVOKED"
	EventExpired   = "EXPIRED"
	EventError     = "ERROR"
)

// JitConfig represents an account binding configuration
type JitConfig struct {
	ChannelID              string   `dynamodbav:"channel_id" json:"channel_id"`
	AccountID              string   `dynamodbav:"account_id" json:"account_id"`
	ApproverMMUserIDs      []string `dynamodbav:"approver_mm_user_ids,stringset" json:"approver_mm_user_ids"`
	ApprovalPolicy         string   `dynamodbav:"approval_policy" json:"approval_policy"`
	AllowSelfApproval      bool     `dynamodbav:"allow_self_approval" json:"allow_self_approval"`
	MaxRequestHours        int      `dynamodbav:"max_request_hours" json:"max_request_hours"`
	SessionDurationMinutes int      `dynamodbav:"session_duration_minutes" json:"session_duration_minutes"`
	UpdatedAt              string   `dynamodbav:"updated_at" json:"updated_at"`
}

// JitRequest represents an access request
type JitRequest struct {
	RequestID                string `dynamodbav:"request_id" json:"request_id"`
	AccountID                string `dynamodbav:"account_id" json:"account_id"`
	ChannelID                string `dynamodbav:"channel_id" json:"channel_id"`
	RequesterMMUserID        string `dynamodbav:"requester_mm_user_id" json:"requester_mm_user_id"`
	RequesterEmail           string `dynamodbav:"requester_email" json:"requester_email"`
	Jira                     string `dynamodbav:"jira" json:"jira"`
	Reason                   string `dynamodbav:"reason" json:"reason"`
	RequestedDurationMinutes int    `dynamodbav:"requested_duration_minutes" json:"requested_duration_minutes"`
	Status                   string `dynamodbav:"status" json:"status"`
	CreatedAt                string `dynamodbav:"created_at" json:"created_at"`
	ApprovedAt               string `dynamodbav:"approved_at,omitempty" json:"approved_at,omitempty"`
	DeniedAt                 string `dynamodbav:"denied_at,omitempty" json:"denied_at,omitempty"`
	GrantTime                string `dynamodbav:"grant_time,omitempty" json:"grant_time,omitempty"`
	RevokedAt                string `dynamodbav:"revoked_at,omitempty" json:"revoked_at,omitempty"`
	ExpiredAt                string `dynamodbav:"expired_at,omitempty" json:"expired_at,omitempty"`
	EndTime                  string `dynamodbav:"end_time" json:"end_time"`
	ApproverMMUserID         string `dynamodbav:"approver_mm_user_id,omitempty" json:"approver_mm_user_id,omitempty"`
	ApproverEmail            string `dynamodbav:"approver_email,omitempty" json:"approver_email,omitempty"`
	IdentityStoreUserID      string `dynamodbav:"identity_store_user_id" json:"identity_store_user_id"`
	AssignmentStatus         string `dynamodbav:"assignment_status,omitempty" json:"assignment_status,omitempty"`
	ErrorDetails             string `dynamodbav:"error_details,omitempty" json:"error_details,omitempty"`
}

// AuditEvent records state transitions for audit trail
type AuditEvent struct {
	RequestID        string            `dynamodbav:"request_id" json:"request_id"`
	EventTimeEventID string            `dynamodbav:"event_time_event_id" json:"event_time_event_id"`
	EventID          string            `dynamodbav:"event_id" json:"event_id"`
	EventTime        string            `dynamodbav:"event_time" json:"event_time"`
	EventType        string            `dynamodbav:"event_type" json:"event_type"`
	AccountID        string            `dynamodbav:"account_id" json:"account_id"`
	ChannelID        string            `dynamodbav:"channel_id" json:"channel_id"`
	ActorMMUserID    string            `dynamodbav:"actor_mm_user_id,omitempty" json:"actor_mm_user_id,omitempty"`
	ActorEmail       string            `dynamodbav:"actor_email,omitempty" json:"actor_email,omitempty"`
	Details          map[string]string `dynamodbav:"details,omitempty" json:"details,omitempty"`
}

// NonceEntry for replay protection
type NonceEntry struct {
	KeyID     string `dynamodbav:"key_id" json:"key_id"`
	Nonce     string `dynamodbav:"nonce" json:"nonce"`
	CreatedAt string `dynamodbav:"created_at" json:"created_at"`
	ExpiresAt int64  `dynamodbav:"expires_at" json:"expires_at"`
}

// WebhookPayload for backend -> plugin notifications
type WebhookPayload struct {
	RequestID string            `json:"request_id"`
	Status    string            `json:"status"`
	AccountID string            `json:"account_id"`
	ChannelID string            `json:"channel_id"`
	Actor     string            `json:"actor"`
	Details   map[string]string `json:"details,omitempty"`
}

// ReportingResponse is the response shape for GET /requests
type ReportingResponse struct {
	Items     []JitRequest      `json:"items"`
	NextToken string            `json:"next_token,omitempty"`
	Filters   map[string]string `json:"filters,omitempty"`
}

// CreateRequestInput for POST /requests
type CreateRequestInput struct {
	AccountID                string `json:"account_id"`
	ChannelID                string `json:"channel_id"`
	RequesterMMUserID        string `json:"requester_mm_user_id"`
	RequesterEmail           string `json:"requester_email"`
	Jira                     string `json:"jira"`
	Reason                   string `json:"reason"`
	RequestedDurationMinutes int    `json:"requested_duration_minutes"`
}

// ApproveRequestInput for POST /requests/{id}/approve
type ApproveRequestInput struct {
	RequestID        string `json:"request_id"`
	ApproverMMUserID string `json:"approver_mm_user_id"`
	ApproverEmail    string `json:"approver_email"`
}

// DenyRequestInput for POST /requests/{id}/deny
type DenyRequestInput struct {
	RequestID      string `json:"request_id"`
	DenierMMUserID string `json:"denier_mm_user_id"`
	DenierEmail    string `json:"denier_email"`
	Reason         string `json:"reason,omitempty"`
}

// RevokeRequestInput for POST /requests/{id}/revoke
type RevokeRequestInput struct {
	RequestID     string `json:"request_id"`
	ActorMMUserID string `json:"actor_mm_user_id"`
	ActorEmail    string `json:"actor_email"`
}

// ReportingInput for GET /requests query parameters
type ReportingInput struct {
	ChannelID      string `json:"channel_id"`
	AccountID      string `json:"account_id"`
	RequesterEmail string `json:"requester_email"`
	Status         string `json:"status"`
	StartDate      string `json:"start_date"`
	EndDate        string `json:"end_date"`
	NextToken      string `json:"next_token"`
	Limit          int    `json:"limit"`
}

// StepFunctionInput is the input to the Step Functions state machine
type StepFunctionInput struct {
	RequestID           string `json:"request_id"`
	AccountID           string `json:"account_id"`
	ChannelID           string `json:"channel_id"`
	IdentityStoreUserID string `json:"identity_store_user_id"`
	DurationMinutes     int    `json:"duration_minutes"`
	RequesterEmail      string `json:"requester_email"`
}

// BindAccountInput for POST /config/bind
type BindAccountInput struct {
	ChannelID string `json:"channel_id"`
	AccountID string `json:"account_id"`
}

// SetApproversInput for POST /config/approvers
type SetApproversInput struct {
	ChannelID   string   `json:"channel_id"`
	ApproverIDs []string `json:"approver_ids"`
}
