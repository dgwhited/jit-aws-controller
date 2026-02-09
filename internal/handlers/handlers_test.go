package handlers

import (
	"context"
	"fmt"
	"testing"

	"github.com/dgwhited/jit-aws-controller/internal/models"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockDB struct {
	configs          map[string]*models.JitConfig // key: "channelID|accountID"
	configsByChannel map[string][]models.JitConfig
	channelForAcct   map[string]*models.JitConfig
	requests         map[string]*models.JitRequest
	putConfigErr     error
	createReqErr     error
	condUpdateErr    error
	queryReqResult   []models.JitRequest
	queryReqToken    string
	queryReqErr      error
}

func newMockDB() *mockDB {
	return &mockDB{
		configs:          map[string]*models.JitConfig{},
		configsByChannel: map[string][]models.JitConfig{},
		channelForAcct:   map[string]*models.JitConfig{},
		requests:         map[string]*models.JitRequest{},
	}
}

func (m *mockDB) GetConfig(_ context.Context, channelID, accountID string) (*models.JitConfig, error) {
	return m.configs[channelID+"|"+accountID], nil
}

func (m *mockDB) GetConfigsByChannel(_ context.Context, channelID string) ([]models.JitConfig, error) {
	return m.configsByChannel[channelID], nil
}

func (m *mockDB) PutConfig(_ context.Context, cfg *models.JitConfig) error {
	if m.putConfigErr != nil {
		return m.putConfigErr
	}
	m.configs[cfg.ChannelID+"|"+cfg.AccountID] = cfg
	return nil
}

func (m *mockDB) GetChannelForAccount(_ context.Context, accountID string) (*models.JitConfig, error) {
	return m.channelForAcct[accountID], nil
}

func (m *mockDB) CreateRequest(_ context.Context, req *models.JitRequest) error {
	if m.createReqErr != nil {
		return m.createReqErr
	}
	m.requests[req.RequestID] = req
	return nil
}

func (m *mockDB) GetRequest(_ context.Context, requestID string) (*models.JitRequest, error) {
	return m.requests[requestID], nil
}

func (m *mockDB) UpdateRequestStatus(_ context.Context, requestID string, updates map[string]interface{}) error {
	if req, ok := m.requests[requestID]; ok {
		if s, ok := updates["status"].(string); ok {
			req.Status = s
		}
	}
	return nil
}

func (m *mockDB) ConditionalUpdateStatus(_ context.Context, requestID, expectedStatus string, updates map[string]interface{}) error {
	if m.condUpdateErr != nil {
		return m.condUpdateErr
	}
	req, ok := m.requests[requestID]
	if !ok {
		return fmt.Errorf("request %s not found", requestID)
	}
	if req.Status != expectedStatus {
		return fmt.Errorf("status mismatch: got %s, expected %s", req.Status, expectedStatus)
	}
	if s, ok := updates["status"].(string); ok {
		req.Status = s
	}
	return nil
}

func (m *mockDB) QueryRequests(_ context.Context, _ models.ReportingInput) ([]models.JitRequest, string, error) {
	return m.queryReqResult, m.queryReqToken, m.queryReqErr
}

type mockIdentity struct {
	users     map[string]string // email -> userID
	grantErr  error
	revokeErr error
}

func (m *mockIdentity) LookupUserByEmail(_ context.Context, email string) (string, error) {
	if uid, ok := m.users[email]; ok {
		return uid, nil
	}
	return "", fmt.Errorf("no user found for %s", email)
}

func (m *mockIdentity) GrantAccess(_ context.Context, _, _ string) error {
	return m.grantErr
}

func (m *mockIdentity) RevokeAccess(_ context.Context, _, _ string) error {
	return m.revokeErr
}

type mockWebhook struct {
	payloads []models.WebhookPayload
	err      error
}

func (m *mockWebhook) Notify(_ context.Context, payload models.WebhookPayload) error {
	m.payloads = append(m.payloads, payload)
	return m.err
}

type mockAudit struct {
	events []auditCall
}

type auditCall struct {
	requestID string
	eventType string
}

func (m *mockAudit) Log(_ context.Context, requestID, eventType, _, _, _, _ string, _ map[string]string) error {
	m.events = append(m.events, auditCall{requestID: requestID, eventType: eventType})
	return nil
}

type mockSFN struct {
	started []models.StepFunctionInput
	err     error
}

func (m *mockSFN) StartExecution(_ context.Context, input models.StepFunctionInput) error {
	m.started = append(m.started, input)
	return m.err
}

// helper to build a Handler with mocks
func newTestHandler() (*Handler, *mockDB, *mockIdentity, *mockWebhook, *mockAudit, *mockSFN) {
	db := newMockDB()
	id := &mockIdentity{users: map[string]string{"user@example.com": "uid-123"}}
	wh := &mockWebhook{}
	au := &mockAudit{}
	sf := &mockSFN{}
	h := &Handler{
		DB:       db,
		Identity: id,
		Webhook:  wh,
		Audit:    au,
		SFN:      sf,
	}
	return h, db, id, wh, au, sf
}

// ---------------------------------------------------------------------------
// HandleCreateRequest tests
// ---------------------------------------------------------------------------

func TestHandleCreateRequest_Success(t *testing.T) {
	h, db, _, _, au, _ := newTestHandler()
	db.configs["ch1|acct1"] = &models.JitConfig{
		ChannelID:       "ch1",
		AccountID:       "acct1",
		MaxRequestHours: 4,
	}

	input := models.CreateRequestInput{
		AccountID:                "acct1",
		ChannelID:                "ch1",
		RequesterMMUserID:        "mm-user-1",
		RequesterEmail:           "user@example.com",
		Jira:                     "JIRA-123",
		Reason:                   "need access",
		RequestedDurationMinutes: 60,
	}

	req, err := h.HandleCreateRequest(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Status != models.StatusPending {
		t.Errorf("expected status PENDING, got %s", req.Status)
	}
	if req.AccountID != "acct1" {
		t.Errorf("expected account_id acct1, got %s", req.AccountID)
	}
	if req.IdentityStoreUserID != "uid-123" {
		t.Errorf("expected identity store user uid-123, got %s", req.IdentityStoreUserID)
	}
	if len(au.events) != 1 || au.events[0].eventType != models.EventRequested {
		t.Errorf("expected 1 REQUESTED audit event, got %+v", au.events)
	}
}

func TestHandleCreateRequest_MissingFields(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler()

	_, err := h.HandleCreateRequest(context.Background(), models.CreateRequestInput{})
	if err == nil {
		t.Fatal("expected error for missing fields")
	}
}

func TestHandleCreateRequest_NoBinding(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler()
	input := models.CreateRequestInput{
		AccountID:                "acct1",
		ChannelID:                "ch1",
		RequesterMMUserID:        "mm-user-1",
		RequesterEmail:           "user@example.com",
		Reason:                   "need access",
		RequestedDurationMinutes: 60,
	}

	_, err := h.HandleCreateRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for missing binding")
	}
}

func TestHandleCreateRequest_DurationExceedsMax(t *testing.T) {
	h, db, _, _, _, _ := newTestHandler()
	db.configs["ch1|acct1"] = &models.JitConfig{
		ChannelID:       "ch1",
		AccountID:       "acct1",
		MaxRequestHours: 1,
	}

	input := models.CreateRequestInput{
		AccountID:                "acct1",
		ChannelID:                "ch1",
		RequesterMMUserID:        "mm-user-1",
		RequesterEmail:           "user@example.com",
		Reason:                   "test",
		RequestedDurationMinutes: 120,
	}

	_, err := h.HandleCreateRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for duration exceeding max")
	}
}

// ---------------------------------------------------------------------------
// HandleApproveRequest tests
// ---------------------------------------------------------------------------

func TestHandleApproveRequest_Success(t *testing.T) {
	h, db, _, _, au, sf := newTestHandler()
	db.configs["ch1|acct1"] = &models.JitConfig{
		ChannelID:         "ch1",
		AccountID:         "acct1",
		ApproverMMUserIDs: []string{"approver-1"},
	}
	db.requests["req-1"] = &models.JitRequest{
		RequestID:           "req-1",
		AccountID:           "acct1",
		ChannelID:           "ch1",
		RequesterMMUserID:   "mm-user-1",
		Status:              models.StatusPending,
		IdentityStoreUserID: "uid-123",
	}

	input := models.ApproveRequestInput{
		RequestID:        "req-1",
		ApproverMMUserID: "approver-1",
		ApproverEmail:    "approver@example.com",
	}

	_, err := h.HandleApproveRequest(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if db.requests["req-1"].Status != models.StatusApproved {
		t.Errorf("expected APPROVED status, got %s", db.requests["req-1"].Status)
	}
	if len(au.events) != 1 || au.events[0].eventType != models.EventApproved {
		t.Errorf("expected APPROVED audit event, got %+v", au.events)
	}
	if len(sf.started) != 1 {
		t.Errorf("expected 1 SFN execution started, got %d", len(sf.started))
	}
}

func TestHandleApproveRequest_NotPending(t *testing.T) {
	h, db, _, _, _, _ := newTestHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID: "req-1",
		AccountID: "acct1",
		ChannelID: "ch1",
		Status:    models.StatusGranted,
	}

	input := models.ApproveRequestInput{
		RequestID:        "req-1",
		ApproverMMUserID: "approver-1",
		ApproverEmail:    "approver@example.com",
	}

	_, err := h.HandleApproveRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for non-PENDING request")
	}
}

func TestHandleApproveRequest_SelfApprovalDenied(t *testing.T) {
	h, db, _, _, _, _ := newTestHandler()
	db.configs["ch1|acct1"] = &models.JitConfig{
		ChannelID:         "ch1",
		AccountID:         "acct1",
		ApproverMMUserIDs: []string{"mm-user-1"},
		AllowSelfApproval: false,
	}
	db.requests["req-1"] = &models.JitRequest{
		RequestID:         "req-1",
		AccountID:         "acct1",
		ChannelID:         "ch1",
		RequesterMMUserID: "mm-user-1",
		Status:            models.StatusPending,
	}

	input := models.ApproveRequestInput{
		RequestID:        "req-1",
		ApproverMMUserID: "mm-user-1",
		ApproverEmail:    "user@example.com",
	}

	_, err := h.HandleApproveRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected self-approval error")
	}
}

func TestHandleApproveRequest_UnauthorizedApprover(t *testing.T) {
	h, db, _, _, _, _ := newTestHandler()
	db.configs["ch1|acct1"] = &models.JitConfig{
		ChannelID:         "ch1",
		AccountID:         "acct1",
		ApproverMMUserIDs: []string{"approver-1"},
	}
	db.requests["req-1"] = &models.JitRequest{
		RequestID: "req-1",
		AccountID: "acct1",
		ChannelID: "ch1",
		Status:    models.StatusPending,
	}

	input := models.ApproveRequestInput{
		RequestID:        "req-1",
		ApproverMMUserID: "random-user",
		ApproverEmail:    "random@example.com",
	}

	_, err := h.HandleApproveRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected unauthorized approver error")
	}
}

// ---------------------------------------------------------------------------
// HandleDenyRequest tests
// ---------------------------------------------------------------------------

func TestHandleDenyRequest_Success(t *testing.T) {
	h, db, _, wh, au, _ := newTestHandler()
	db.configs["ch1|acct1"] = &models.JitConfig{
		ChannelID:         "ch1",
		AccountID:         "acct1",
		ApproverMMUserIDs: []string{"approver-1"},
	}
	db.requests["req-1"] = &models.JitRequest{
		RequestID: "req-1",
		AccountID: "acct1",
		ChannelID: "ch1",
		Status:    models.StatusPending,
	}

	input := models.DenyRequestInput{
		RequestID:      "req-1",
		DenierMMUserID: "approver-1",
		DenierEmail:    "approver@example.com",
	}

	_, err := h.HandleDenyRequest(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if db.requests["req-1"].Status != models.StatusDenied {
		t.Errorf("expected DENIED, got %s", db.requests["req-1"].Status)
	}
	if len(au.events) != 1 || au.events[0].eventType != models.EventDenied {
		t.Errorf("expected DENIED audit event")
	}
	// No webhook is sent for denials — the plugin updates the card in-place.
	if len(wh.payloads) != 0 {
		t.Errorf("expected no webhook notification for deny, got %d", len(wh.payloads))
	}
}

func TestHandleDenyRequest_NotFound(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler()

	input := models.DenyRequestInput{
		RequestID:      "nonexistent",
		DenierMMUserID: "approver-1",
		DenierEmail:    "approver@example.com",
	}

	_, err := h.HandleDenyRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for missing request")
	}
}

// ---------------------------------------------------------------------------
// HandleRevokeRequest tests
// ---------------------------------------------------------------------------

func TestHandleRevokeRequest_Success(t *testing.T) {
	h, db, _, wh, au, _ := newTestHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID:           "req-1",
		AccountID:           "acct1",
		ChannelID:           "ch1",
		Status:              models.StatusGranted,
		IdentityStoreUserID: "uid-123",
	}

	input := models.RevokeRequestInput{
		RequestID:     "req-1",
		ActorMMUserID: "admin-1",
		ActorEmail:    "admin@example.com",
	}

	_, err := h.HandleRevokeRequest(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if db.requests["req-1"].Status != models.StatusRevoked {
		t.Errorf("expected REVOKED, got %s", db.requests["req-1"].Status)
	}
	if len(au.events) != 1 || au.events[0].eventType != models.EventRevoked {
		t.Errorf("expected REVOKED audit event")
	}
	if len(wh.payloads) != 1 || wh.payloads[0].Status != models.StatusRevoked {
		t.Errorf("expected REVOKED webhook notification")
	}
}

func TestHandleRevokeRequest_NotGranted(t *testing.T) {
	h, db, _, _, _, _ := newTestHandler()
	db.requests["req-1"] = &models.JitRequest{
		RequestID: "req-1",
		Status:    models.StatusPending,
	}

	input := models.RevokeRequestInput{
		RequestID:     "req-1",
		ActorMMUserID: "admin-1",
		ActorEmail:    "admin@example.com",
	}

	_, err := h.HandleRevokeRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for non-GRANTED request")
	}
}

func TestHandleRevokeRequest_IdentityError(t *testing.T) {
	h, db, id, _, _, _ := newTestHandler()
	id.revokeErr = fmt.Errorf("SSO unavailable")
	db.requests["req-1"] = &models.JitRequest{
		RequestID:           "req-1",
		AccountID:           "acct1",
		ChannelID:           "ch1",
		Status:              models.StatusGranted,
		IdentityStoreUserID: "uid-123",
	}

	input := models.RevokeRequestInput{
		RequestID:     "req-1",
		ActorMMUserID: "admin-1",
		ActorEmail:    "admin@example.com",
	}

	_, err := h.HandleRevokeRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected error when identity revoke fails")
	}
}

// ---------------------------------------------------------------------------
// HandleListRequests tests
// ---------------------------------------------------------------------------

func TestHandleListRequests_Success(t *testing.T) {
	h, db, _, _, _, _ := newTestHandler()
	db.queryReqResult = []models.JitRequest{
		{RequestID: "req-1", Status: models.StatusPending},
		{RequestID: "req-2", Status: models.StatusGranted},
	}

	input := models.ReportingInput{
		ChannelID: "ch1",
		Limit:     10,
	}

	resp, err := h.HandleListRequests(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(resp.Items))
	}
	if resp.Filters["channel_id"] != "ch1" {
		t.Errorf("expected channel_id filter echoed")
	}
}

func TestHandleListRequests_DefaultLimit(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler()

	input := models.ReportingInput{Limit: 0, ChannelID: "ch1"}
	resp, err := h.HandleListRequests(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Items == nil {
		t.Errorf("expected non-nil items slice")
	}
}

func TestHandleListRequests_MaxLimit(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler()

	input := models.ReportingInput{Limit: 500, ChannelID: "ch1"}
	_, err := h.HandleListRequests(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Limit is capped internally; no error expected.
}

// ---------------------------------------------------------------------------
// HandleBindAccount tests
// ---------------------------------------------------------------------------

func TestHandleBindAccount_Success(t *testing.T) {
	h, db, _, _, _, _ := newTestHandler()

	input := models.BindAccountInput{
		ChannelID: "ch1",
		AccountID: "123456789012",
	}

	cfg, err := h.HandleBindAccount(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ChannelID != "ch1" {
		t.Errorf("expected channel_id ch1, got %s", cfg.ChannelID)
	}
	if cfg.MaxRequestHours != 4 {
		t.Errorf("expected default max_request_hours 4, got %d", cfg.MaxRequestHours)
	}
	if _, ok := db.configs["ch1|123456789012"]; !ok {
		t.Error("expected config to be stored")
	}
}

func TestHandleBindAccount_AlreadyBoundDifferentChannel(t *testing.T) {
	h, db, _, _, _, _ := newTestHandler()
	db.channelForAcct["123456789012"] = &models.JitConfig{
		ChannelID: "other-channel",
		AccountID: "123456789012",
	}

	input := models.BindAccountInput{
		ChannelID: "ch1",
		AccountID: "123456789012",
	}

	_, err := h.HandleBindAccount(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for account bound to different channel")
	}
}

func TestHandleBindAccount_MissingFields(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler()

	_, err := h.HandleBindAccount(context.Background(), models.BindAccountInput{})
	if err == nil {
		t.Fatal("expected error for missing fields")
	}
}

// ---------------------------------------------------------------------------
// HandleSetApprovers tests
// ---------------------------------------------------------------------------

func TestHandleSetApprovers_Success(t *testing.T) {
	h, db, _, _, _, _ := newTestHandler()
	db.configsByChannel["ch1"] = []models.JitConfig{
		{ChannelID: "ch1", AccountID: "acct1"},
		{ChannelID: "ch1", AccountID: "acct2"},
	}

	input := models.SetApproversInput{
		ChannelID:   "ch1",
		ApproverIDs: []string{"user1", "user2"},
	}

	updated, err := h.HandleSetApprovers(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated) != 2 {
		t.Errorf("expected 2 updated configs, got %d", len(updated))
	}
	for _, cfg := range updated {
		if len(cfg.ApproverMMUserIDs) != 2 {
			t.Errorf("expected 2 approvers, got %d", len(cfg.ApproverMMUserIDs))
		}
	}
}

func TestHandleSetApprovers_NoAccounts(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler()

	input := models.SetApproversInput{
		ChannelID:   "ch1",
		ApproverIDs: []string{"user1"},
	}

	_, err := h.HandleSetApprovers(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for no bound accounts")
	}
}

func TestHandleSetApprovers_MissingApprovers(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler()

	_, err := h.HandleSetApprovers(context.Background(), models.SetApproversInput{
		ChannelID: "ch1",
	})
	if err == nil {
		t.Fatal("expected error for missing approvers")
	}
}

// ---------------------------------------------------------------------------
// HandleGetBoundAccounts tests
// ---------------------------------------------------------------------------

func TestHandleGetBoundAccounts_Success(t *testing.T) {
	h, db, _, _, _, _ := newTestHandler()
	db.configsByChannel["ch1"] = []models.JitConfig{
		{ChannelID: "ch1", AccountID: "acct1"},
	}

	configs, err := h.HandleGetBoundAccounts(context.Background(), "ch1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Errorf("expected 1 config, got %d", len(configs))
	}
}

func TestHandleGetBoundAccounts_EmptyChannel(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler()

	_, err := h.HandleGetBoundAccounts(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty channel_id")
	}
}

func TestHandleGetBoundAccounts_NilResult(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler()

	configs, err := h.HandleGetBoundAccounts(context.Background(), "ch-no-accounts")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configs == nil {
		t.Error("expected non-nil (empty) slice")
	}
}
