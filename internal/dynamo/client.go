package dynamo

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/dgwhited/jit-aws-controller/internal/models"
)

// Client provides DynamoDB operations for all JIT tables.
type Client struct {
	db            *dynamodb.Client
	tableConfig   string
	tableRequests string
	tableAudit    string
	tableNonces   string
}

// NewClient creates a new DynamoDB client wrapper.
func NewClient(db *dynamodb.Client, tableConfig, tableRequests, tableAudit, tableNonces string) *Client {
	return &Client{
		db:            db,
		tableConfig:   tableConfig,
		tableRequests: tableRequests,
		tableAudit:    tableAudit,
		tableNonces:   tableNonces,
	}
}

// ---------------------------------------------------------------------------
// Config operations
// ---------------------------------------------------------------------------

// GetConfig retrieves a config entry by channel_id and account_id.
func (c *Client) GetConfig(ctx context.Context, channelID, accountID string) (*models.JitConfig, error) {
	out, err := c.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &c.tableConfig,
		Key: map[string]types.AttributeValue{
			"channel_id": &types.AttributeValueMemberS{Value: channelID},
			"account_id": &types.AttributeValueMemberS{Value: accountID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("GetConfig: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}
	var cfg models.JitConfig
	if err := attributevalue.UnmarshalMap(out.Item, &cfg); err != nil {
		return nil, fmt.Errorf("GetConfig unmarshal: %w", err)
	}
	return &cfg, nil
}

// GetConfigsByChannel returns all config entries for a channel.
func (c *Client) GetConfigsByChannel(ctx context.Context, channelID string) ([]models.JitConfig, error) {
	out, err := c.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              &c.tableConfig,
		KeyConditionExpression: aws.String("channel_id = :cid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":cid": &types.AttributeValueMemberS{Value: channelID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("GetConfigsByChannel: %w", err)
	}
	var configs []models.JitConfig
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &configs); err != nil {
		return nil, fmt.Errorf("GetConfigsByChannel unmarshal: %w", err)
	}
	return configs, nil
}

// PutConfig creates or updates a config entry.
func (c *Client) PutConfig(ctx context.Context, cfg *models.JitConfig) error {
	item, err := attributevalue.MarshalMap(cfg)
	if err != nil {
		return fmt.Errorf("PutConfig marshal: %w", err)
	}
	_, err = c.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &c.tableConfig,
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("PutConfig: %w", err)
	}
	return nil
}

// GetChannelForAccount looks up the channel binding for an account using gsi_account.
func (c *Client) GetChannelForAccount(ctx context.Context, accountID string) (*models.JitConfig, error) {
	out, err := c.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              &c.tableConfig,
		IndexName:              aws.String("gsi_account"),
		KeyConditionExpression: aws.String("account_id = :aid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":aid": &types.AttributeValueMemberS{Value: accountID},
		},
		Limit: aws.Int32(1),
	})
	if err != nil {
		return nil, fmt.Errorf("GetChannelForAccount: %w", err)
	}
	if len(out.Items) == 0 {
		return nil, nil
	}
	var cfg models.JitConfig
	if err := attributevalue.UnmarshalMap(out.Items[0], &cfg); err != nil {
		return nil, fmt.Errorf("GetChannelForAccount unmarshal: %w", err)
	}
	return &cfg, nil
}

// ---------------------------------------------------------------------------
// Request operations
// ---------------------------------------------------------------------------

// CreateRequest stores a new JIT request.
func (c *Client) CreateRequest(ctx context.Context, req *models.JitRequest) error {
	item, err := attributevalue.MarshalMap(req)
	if err != nil {
		return fmt.Errorf("CreateRequest marshal: %w", err)
	}
	_, err = c.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           &c.tableRequests,
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(request_id)"),
	})
	if err != nil {
		return fmt.Errorf("CreateRequest: %w", err)
	}
	return nil
}

// GetRequest retrieves a single request by ID.
func (c *Client) GetRequest(ctx context.Context, requestID string) (*models.JitRequest, error) {
	out, err := c.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &c.tableRequests,
		Key: map[string]types.AttributeValue{
			"request_id": &types.AttributeValueMemberS{Value: requestID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("GetRequest: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}
	var req models.JitRequest
	if err := attributevalue.UnmarshalMap(out.Item, &req); err != nil {
		return nil, fmt.Errorf("GetRequest unmarshal: %w", err)
	}
	return &req, nil
}

// UpdateRequestStatus updates a request's status and associated timestamp fields.
// The update map should contain field names and their new values.
func (c *Client) UpdateRequestStatus(ctx context.Context, requestID string, updates map[string]interface{}) error {
	updateExpr := "SET"
	exprNames := map[string]string{}
	exprValues := map[string]types.AttributeValue{}

	i := 0
	for field, val := range updates {
		if i > 0 {
			updateExpr += ","
		}
		nameAlias := fmt.Sprintf("#f%d", i)
		valAlias := fmt.Sprintf(":v%d", i)
		updateExpr += fmt.Sprintf(" %s = %s", nameAlias, valAlias)
		exprNames[nameAlias] = field

		av, err := attributevalue.Marshal(val)
		if err != nil {
			return fmt.Errorf("UpdateRequestStatus marshal field %s: %w", field, err)
		}
		exprValues[valAlias] = av
		i++
	}

	_, err := c.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &c.tableRequests,
		Key: map[string]types.AttributeValue{
			"request_id": &types.AttributeValueMemberS{Value: requestID},
		},
		UpdateExpression:          &updateExpr,
		ExpressionAttributeNames:  exprNames,
		ExpressionAttributeValues: exprValues,
	})
	if err != nil {
		return fmt.Errorf("UpdateRequestStatus: %w", err)
	}
	return nil
}

// ConditionalUpdateStatus updates a request only if the current status matches expectedStatus.
func (c *Client) ConditionalUpdateStatus(ctx context.Context, requestID, expectedStatus string, updates map[string]interface{}) error {
	updateExpr := "SET"
	exprNames := map[string]string{
		"#status": "status",
	}
	exprValues := map[string]types.AttributeValue{
		":expected": &types.AttributeValueMemberS{Value: expectedStatus},
	}

	i := 0
	for field, val := range updates {
		if i > 0 {
			updateExpr += ","
		}
		nameAlias := fmt.Sprintf("#f%d", i)
		valAlias := fmt.Sprintf(":v%d", i)
		updateExpr += fmt.Sprintf(" %s = %s", nameAlias, valAlias)
		exprNames[nameAlias] = field

		av, err := attributevalue.Marshal(val)
		if err != nil {
			return fmt.Errorf("ConditionalUpdateStatus marshal field %s: %w", field, err)
		}
		exprValues[valAlias] = av
		i++
	}

	condExpr := "#status = :expected"

	_, err := c.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &c.tableRequests,
		Key: map[string]types.AttributeValue{
			"request_id": &types.AttributeValueMemberS{Value: requestID},
		},
		UpdateExpression:          &updateExpr,
		ConditionExpression:       &condExpr,
		ExpressionAttributeNames:  exprNames,
		ExpressionAttributeValues: exprValues,
	})
	if err != nil {
		return fmt.Errorf("ConditionalUpdateStatus: %w", err)
	}
	return nil
}

// QueryRequestsByChannel queries requests by channel using gsi_channel_created.
func (c *Client) QueryRequestsByChannel(ctx context.Context, channelID string, limit int32, startKey map[string]types.AttributeValue) ([]models.JitRequest, map[string]types.AttributeValue, error) {
	input := &dynamodb.QueryInput{
		TableName:              &c.tableRequests,
		IndexName:              aws.String("gsi_channel_created"),
		KeyConditionExpression: aws.String("channel_id = :cid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":cid": &types.AttributeValueMemberS{Value: channelID},
		},
		ScanIndexForward: aws.Bool(false),
		Limit:            &limit,
	}
	if startKey != nil {
		input.ExclusiveStartKey = startKey
	}

	out, err := c.db.Query(ctx, input)
	if err != nil {
		return nil, nil, fmt.Errorf("QueryRequestsByChannel: %w", err)
	}
	var requests []models.JitRequest
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &requests); err != nil {
		return nil, nil, fmt.Errorf("QueryRequestsByChannel unmarshal: %w", err)
	}
	return requests, out.LastEvaluatedKey, nil
}

// QueryRequestsByStatus queries requests by status using gsi_status_endtime.
// If beforeEndTime is non-empty, only returns items with end_time <= beforeEndTime.
func (c *Client) QueryRequestsByStatus(ctx context.Context, status string, beforeEndTime string, limit int32) ([]models.JitRequest, error) {
	keyExpr := "#status = :s"
	exprNames := map[string]string{
		"#status": "status",
	}
	exprValues := map[string]types.AttributeValue{
		":s": &types.AttributeValueMemberS{Value: status},
	}

	if beforeEndTime != "" {
		keyExpr += " AND end_time <= :et"
		exprValues[":et"] = &types.AttributeValueMemberS{Value: beforeEndTime}
	}

	input := &dynamodb.QueryInput{
		TableName:                 &c.tableRequests,
		IndexName:                 aws.String("gsi_status_endtime"),
		KeyConditionExpression:    aws.String(keyExpr),
		ExpressionAttributeNames:  exprNames,
		ExpressionAttributeValues: exprValues,
		ScanIndexForward:          aws.Bool(true),
	}
	if limit > 0 {
		input.Limit = &limit
	}

	var allRequests []models.JitRequest
	for {
		out, err := c.db.Query(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("QueryRequestsByStatus: %w", err)
		}
		var page []models.JitRequest
		if err := attributevalue.UnmarshalListOfMaps(out.Items, &page); err != nil {
			return nil, fmt.Errorf("QueryRequestsByStatus unmarshal: %w", err)
		}
		allRequests = append(allRequests, page...)

		if out.LastEvaluatedKey == nil {
			break
		}
		if limit > 0 && int32(len(allRequests)) >= limit {
			break
		}
		input.ExclusiveStartKey = out.LastEvaluatedKey
	}
	return allRequests, nil
}

// QueryRequests provides general purpose reporting queries with optional filters.
func (c *Client) QueryRequests(ctx context.Context, input models.ReportingInput) ([]models.JitRequest, string, error) {
	var queryInput *dynamodb.QueryInput
	limit := int32(input.Limit)
	if limit <= 0 {
		limit = 50
	}

	// Determine which GSI to use based on available filters.
	switch {
	case input.ChannelID != "":
		keyExpr := "channel_id = :cid"
		exprValues := map[string]types.AttributeValue{
			":cid": &types.AttributeValueMemberS{Value: input.ChannelID},
		}
		if input.StartDate != "" && input.EndDate != "" {
			keyExpr += " AND created_at BETWEEN :sd AND :ed"
			exprValues[":sd"] = &types.AttributeValueMemberS{Value: input.StartDate}
			exprValues[":ed"] = &types.AttributeValueMemberS{Value: input.EndDate}
		} else if input.StartDate != "" {
			keyExpr += " AND created_at >= :sd"
			exprValues[":sd"] = &types.AttributeValueMemberS{Value: input.StartDate}
		} else if input.EndDate != "" {
			keyExpr += " AND created_at <= :ed"
			exprValues[":ed"] = &types.AttributeValueMemberS{Value: input.EndDate}
		}

		queryInput = &dynamodb.QueryInput{
			TableName:                 &c.tableRequests,
			IndexName:                 aws.String("gsi_channel_created"),
			KeyConditionExpression:    aws.String(keyExpr),
			ExpressionAttributeValues: exprValues,
			ScanIndexForward:          aws.Bool(false),
			Limit:                     &limit,
		}

		// Add filter expressions for additional criteria.
		filterExpr, filterNames, filterValues := buildFilters(input, true)
		if filterExpr != "" {
			queryInput.FilterExpression = aws.String(filterExpr)
			queryInput.ExpressionAttributeNames = filterNames
			for k, v := range filterValues {
				queryInput.ExpressionAttributeValues[k] = v
			}
		}

	case input.AccountID != "":
		keyExpr := "account_id = :aid"
		exprValues := map[string]types.AttributeValue{
			":aid": &types.AttributeValueMemberS{Value: input.AccountID},
		}
		if input.StartDate != "" && input.EndDate != "" {
			keyExpr += " AND created_at BETWEEN :sd AND :ed"
			exprValues[":sd"] = &types.AttributeValueMemberS{Value: input.StartDate}
			exprValues[":ed"] = &types.AttributeValueMemberS{Value: input.EndDate}
		}

		queryInput = &dynamodb.QueryInput{
			TableName:                 &c.tableRequests,
			IndexName:                 aws.String("gsi_account_created"),
			KeyConditionExpression:    aws.String(keyExpr),
			ExpressionAttributeValues: exprValues,
			ScanIndexForward:          aws.Bool(false),
			Limit:                     &limit,
		}

		filterExpr, filterNames, filterValues := buildFilters(input, false)
		if filterExpr != "" {
			queryInput.FilterExpression = aws.String(filterExpr)
			queryInput.ExpressionAttributeNames = filterNames
			for k, v := range filterValues {
				queryInput.ExpressionAttributeValues[k] = v
			}
		}

	case input.RequesterEmail != "":
		keyExpr := "requester_email = :email"
		exprValues := map[string]types.AttributeValue{
			":email": &types.AttributeValueMemberS{Value: input.RequesterEmail},
		}
		if input.StartDate != "" && input.EndDate != "" {
			keyExpr += " AND created_at BETWEEN :sd AND :ed"
			exprValues[":sd"] = &types.AttributeValueMemberS{Value: input.StartDate}
			exprValues[":ed"] = &types.AttributeValueMemberS{Value: input.EndDate}
		}

		queryInput = &dynamodb.QueryInput{
			TableName:                 &c.tableRequests,
			IndexName:                 aws.String("gsi_requester_created"),
			KeyConditionExpression:    aws.String(keyExpr),
			ExpressionAttributeValues: exprValues,
			ScanIndexForward:          aws.Bool(false),
			Limit:                     &limit,
		}

		filterExpr, filterNames, filterValues := buildFilters(input, false)
		if filterExpr != "" {
			queryInput.FilterExpression = aws.String(filterExpr)
			queryInput.ExpressionAttributeNames = filterNames
			for k, v := range filterValues {
				queryInput.ExpressionAttributeValues[k] = v
			}
		}

	case input.Status != "":
		keyExpr := "#status = :st"
		exprNames := map[string]string{
			"#status": "status",
		}
		exprValues := map[string]types.AttributeValue{
			":st": &types.AttributeValueMemberS{Value: input.Status},
		}
		queryInput = &dynamodb.QueryInput{
			TableName:                 &c.tableRequests,
			IndexName:                 aws.String("gsi_status_endtime"),
			KeyConditionExpression:    aws.String(keyExpr),
			ExpressionAttributeNames:  exprNames,
			ExpressionAttributeValues: exprValues,
			ScanIndexForward:          aws.Bool(false),
			Limit:                     &limit,
		}

	default:
		// D5/E4: Reject unfiltered queries — table scans are not permitted.
		return nil, "", fmt.Errorf("QueryRequests: at least one filter (channel_id, account_id, requester_email, or status) is required")
	}

	// Apply pagination token.
	if input.NextToken != "" {
		startKey, err := deserializeStartKey(input.NextToken)
		if err != nil {
			return nil, "", fmt.Errorf("QueryRequests invalid next_token: %w", err)
		}
		queryInput.ExclusiveStartKey = startKey
	}

	out, err := c.db.Query(ctx, queryInput)
	if err != nil {
		return nil, "", fmt.Errorf("QueryRequests: %w", err)
	}
	var requests []models.JitRequest
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &requests); err != nil {
		return nil, "", fmt.Errorf("QueryRequests unmarshal: %w", err)
	}

	var nextToken string
	if out.LastEvaluatedKey != nil {
		nextToken, _ = serializeStartKey(out.LastEvaluatedKey)
	}
	return requests, nextToken, nil
}

// buildFilters constructs optional filter expressions for fields not covered by keys.
func buildFilters(input models.ReportingInput, skipChannel bool) (string, map[string]string, map[string]types.AttributeValue) {
	var parts []string
	names := map[string]string{}
	values := map[string]types.AttributeValue{}

	if input.Status != "" {
		parts = append(parts, "#fstatus = :fstatus")
		names["#fstatus"] = "status"
		values[":fstatus"] = &types.AttributeValueMemberS{Value: input.Status}
	}
	// AccountID is only a filter when it isn't the key (i.e. skipChannel is false),
	// but account-based queries are handled by the key condition in QueryRequests,
	// so no additional filter expression is needed here.

	if input.RequesterEmail != "" && input.ChannelID != "" {
		parts = append(parts, "#femail = :femail")
		names["#femail"] = "requester_email"
		values[":femail"] = &types.AttributeValueMemberS{Value: input.RequesterEmail}
	}

	if len(parts) == 0 {
		return "", nil, nil
	}

	expr := parts[0]
	for i := 1; i < len(parts); i++ {
		expr += " AND " + parts[i]
	}
	return expr, names, values
}

// ---------------------------------------------------------------------------
// Audit operations
// ---------------------------------------------------------------------------

// PutAuditEvent stores an audit event.
func (c *Client) PutAuditEvent(ctx context.Context, event *models.AuditEvent) error {
	item, err := attributevalue.MarshalMap(event)
	if err != nil {
		return fmt.Errorf("PutAuditEvent marshal: %w", err)
	}
	_, err = c.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &c.tableAudit,
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("PutAuditEvent: %w", err)
	}
	return nil
}

// QueryAuditByRequest retrieves all audit events for a given request.
func (c *Client) QueryAuditByRequest(ctx context.Context, requestID string) ([]models.AuditEvent, error) {
	out, err := c.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              &c.tableAudit,
		KeyConditionExpression: aws.String("request_id = :rid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":rid": &types.AttributeValueMemberS{Value: requestID},
		},
		ScanIndexForward: aws.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("QueryAuditByRequest: %w", err)
	}
	var events []models.AuditEvent
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &events); err != nil {
		return nil, fmt.Errorf("QueryAuditByRequest unmarshal: %w", err)
	}
	return events, nil
}

// ---------------------------------------------------------------------------
// Nonce operations (implements auth.NonceStore)
// ---------------------------------------------------------------------------

// StoreNonce persists a nonce with a TTL for replay protection.
func (c *Client) StoreNonce(ctx context.Context, keyID, nonce string, ttlSeconds int64) error {
	now := time.Now().UTC()
	expiresAt := now.Unix() + ttlSeconds

	entry := models.NonceEntry{
		KeyID:     keyID,
		Nonce:     nonce,
		CreatedAt: now.Format(time.RFC3339),
		ExpiresAt: expiresAt,
	}
	item, err := attributevalue.MarshalMap(entry)
	if err != nil {
		return fmt.Errorf("StoreNonce marshal: %w", err)
	}

	// Conditional put to ensure nonce uniqueness.
	_, err = c.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           &c.tableNonces,
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(key_id) AND attribute_not_exists(nonce)"),
	})
	if err != nil {
		return fmt.Errorf("StoreNonce: %w", err)
	}
	return nil
}

// CheckNonce returns true if the nonce already exists for the given key.
func (c *Client) CheckNonce(ctx context.Context, keyID, nonce string) (bool, error) {
	out, err := c.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &c.tableNonces,
		Key: map[string]types.AttributeValue{
			"key_id": &types.AttributeValueMemberS{Value: keyID},
			"nonce":  &types.AttributeValueMemberS{Value: nonce},
		},
	})
	if err != nil {
		return false, fmt.Errorf("CheckNonce: %w", err)
	}
	return out.Item != nil, nil
}

// ---------------------------------------------------------------------------
// Pagination helpers
// ---------------------------------------------------------------------------

// serializeStartKey encodes a DynamoDB exclusive start key as a simple JSON-ish string.
// For simplicity we use a base64-encoded JSON representation.
func serializeStartKey(key map[string]types.AttributeValue) (string, error) {
	if key == nil {
		return "", nil
	}
	// Encode each string attribute as key=value pairs separated by |.
	result := ""
	for k, v := range key {
		if sv, ok := v.(*types.AttributeValueMemberS); ok {
			if result != "" {
				result += "|"
			}
			result += k + "=" + sv.Value
		}
	}
	return result, nil
}

// deserializeStartKey decodes a pagination token back to a DynamoDB key.
func deserializeStartKey(token string) (map[string]types.AttributeValue, error) {
	if token == "" {
		return nil, nil
	}
	result := map[string]types.AttributeValue{}
	parts := splitToken(token)
	for _, part := range parts {
		idx := -1
		for i, c := range part {
			if c == '=' {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("invalid token part: %s", part)
		}
		k := part[:idx]
		v := part[idx+1:]
		result[k] = &types.AttributeValueMemberS{Value: v}
	}
	return result, nil
}

func splitToken(s string) []string {
	var parts []string
	start := 0
	for i, c := range s {
		if c == '|' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// Verify at compile time that Client implements the nonce store interface expectations.
// We cannot import auth here, but the methods StoreNonce and CheckNonce have the right signatures.
var _ interface {
	StoreNonce(ctx context.Context, keyID, nonce string, ttlSeconds int64) error
	CheckNonce(ctx context.Context, keyID, nonce string) (bool, error)
} = (*Client)(nil)

// Suppress unused import warning for slog by using it in a helper.
func init() {
	_ = slog.Default()
}
