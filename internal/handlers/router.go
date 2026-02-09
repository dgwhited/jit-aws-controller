package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/aws/aws-lambda-go/events"

	"github.com/dgwhited/jit-aws-controller/internal/auth"
	"github.com/dgwhited/jit-aws-controller/internal/models"
)

// Router handles API Gateway V2 HTTP events and dispatches to the appropriate handler.
type Router struct {
	Handler   *Handler
	Validator *auth.HMACValidator
}

// NewRouter creates a new Lambda event router.
func NewRouter(handler *Handler, validator *auth.HMACValidator) *Router {
	return &Router{
		Handler:   handler,
		Validator: validator,
	}
}

// Route processes an API Gateway V2 HTTP request event.
func (r *Router) Route(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	method := event.RequestContext.HTTP.Method
	path := event.RequestContext.HTTP.Path

	slog.Info("routing request",
		"method", method,
		"path", path,
	)

	// Validate HMAC signature.
	headers := make(map[string]string)
	for k, v := range event.Headers {
		headers[k] = v
	}

	body := []byte(event.Body)
	if err := r.Validator.ValidateRequest(ctx, method, path, headers, body); err != nil {
		slog.Warn("HMAC validation failed",
			"method", method,
			"path", path,
			"error", err,
		)
		return errorResponse(http.StatusUnauthorized, "unauthorized: "+err.Error()), nil
	}

	// Route to appropriate handler based on method + path.
	switch {
	case method == "POST" && path == "/requests":
		return r.handleCreateRequest(ctx, body)

	case method == "POST" && matchPath(path, "/requests/", "/approve"):
		requestID := extractPathParam(path, "/requests/", "/approve")
		return r.handleApproveRequest(ctx, requestID, body)

	case method == "POST" && matchPath(path, "/requests/", "/deny"):
		requestID := extractPathParam(path, "/requests/", "/deny")
		return r.handleDenyRequest(ctx, requestID, body)

	case method == "POST" && matchPath(path, "/requests/", "/revoke"):
		requestID := extractPathParam(path, "/requests/", "/revoke")
		return r.handleRevokeRequest(ctx, requestID, body)

	case method == "GET" && path == "/requests":
		return r.handleListRequests(ctx, event.QueryStringParameters)

	case method == "GET" && strings.HasPrefix(path, "/requests/") && !strings.Contains(path[len("/requests/"):], "/"):
		requestID := path[len("/requests/"):]
		return r.handleGetRequest(ctx, requestID)

	case method == "POST" && path == "/config/bind":
		return r.handleBindAccount(ctx, body)

	case method == "POST" && path == "/config/approvers":
		return r.handleSetApprovers(ctx, body)

	case method == "GET" && path == "/config/accounts":
		return r.handleGetBoundAccounts(ctx, event.QueryStringParameters)

	default:
		return errorResponse(http.StatusNotFound, "not found"), nil
	}
}

func (r *Router) handleCreateRequest(ctx context.Context, body []byte) (events.APIGatewayV2HTTPResponse, error) {
	var input models.CreateRequestInput
	if err := json.Unmarshal(body, &input); err != nil {
		return errorResponse(http.StatusBadRequest, "invalid request body: "+err.Error()), nil
	}

	req, err := r.Handler.HandleCreateRequest(ctx, input)
	if err != nil {
		slog.Error("create request failed", "error", err)
		return errorResponse(http.StatusBadRequest, err.Error()), nil
	}
	return jsonResponse(http.StatusCreated, req), nil
}

func (r *Router) handleApproveRequest(ctx context.Context, requestID string, body []byte) (events.APIGatewayV2HTTPResponse, error) {
	var input models.ApproveRequestInput
	if err := json.Unmarshal(body, &input); err != nil {
		return errorResponse(http.StatusBadRequest, "invalid request body: "+err.Error()), nil
	}
	input.RequestID = requestID

	req, err := r.Handler.HandleApproveRequest(ctx, input)
	if err != nil {
		slog.Error("approve request failed", "error", err)
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		return errorResponse(code, err.Error()), nil
	}
	return jsonResponse(http.StatusOK, req), nil
}

func (r *Router) handleDenyRequest(ctx context.Context, requestID string, body []byte) (events.APIGatewayV2HTTPResponse, error) {
	var input models.DenyRequestInput
	if err := json.Unmarshal(body, &input); err != nil {
		return errorResponse(http.StatusBadRequest, "invalid request body: "+err.Error()), nil
	}
	input.RequestID = requestID

	req, err := r.Handler.HandleDenyRequest(ctx, input)
	if err != nil {
		slog.Error("deny request failed", "error", err)
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		return errorResponse(code, err.Error()), nil
	}
	return jsonResponse(http.StatusOK, req), nil
}

func (r *Router) handleRevokeRequest(ctx context.Context, requestID string, body []byte) (events.APIGatewayV2HTTPResponse, error) {
	var input models.RevokeRequestInput
	if err := json.Unmarshal(body, &input); err != nil {
		return errorResponse(http.StatusBadRequest, "invalid request body: "+err.Error()), nil
	}
	input.RequestID = requestID

	req, err := r.Handler.HandleRevokeRequest(ctx, input)
	if err != nil {
		slog.Error("revoke request failed", "error", err)
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		return errorResponse(code, err.Error()), nil
	}
	return jsonResponse(http.StatusOK, req), nil
}

func (r *Router) handleListRequests(ctx context.Context, queryParams map[string]string) (events.APIGatewayV2HTTPResponse, error) {
	input := models.ReportingInput{
		ChannelID:      queryParams["channel_id"],
		AccountID:      queryParams["account_id"],
		RequesterEmail: queryParams["requester_email"],
		Status:         queryParams["status"],
		StartDate:      queryParams["start_date"],
		EndDate:        queryParams["end_date"],
		NextToken:      queryParams["next_token"],
	}
	if limitStr, ok := queryParams["limit"]; ok {
		if l, err := strconv.Atoi(limitStr); err == nil {
			input.Limit = l
		}
	}

	resp, err := r.Handler.HandleListRequests(ctx, input)
	if err != nil {
		slog.Error("list requests failed", "error", err)
		return errorResponse(http.StatusInternalServerError, err.Error()), nil
	}
	return jsonResponse(http.StatusOK, resp), nil
}

func (r *Router) handleGetRequest(ctx context.Context, requestID string) (events.APIGatewayV2HTTPResponse, error) {
	if requestID == "" {
		return errorResponse(http.StatusBadRequest, "request_id is required"), nil
	}

	req, err := r.Handler.DB.GetRequest(ctx, requestID)
	if err != nil {
		slog.Error("get request failed", "error", err)
		return errorResponse(http.StatusInternalServerError, err.Error()), nil
	}
	if req == nil {
		return errorResponse(http.StatusNotFound, fmt.Sprintf("request %s not found", requestID)), nil
	}
	return jsonResponse(http.StatusOK, req), nil
}

func (r *Router) handleBindAccount(ctx context.Context, body []byte) (events.APIGatewayV2HTTPResponse, error) {
	var input models.BindAccountInput
	if err := json.Unmarshal(body, &input); err != nil {
		return errorResponse(http.StatusBadRequest, "invalid request body: "+err.Error()), nil
	}

	cfg, err := r.Handler.HandleBindAccount(ctx, input)
	if err != nil {
		slog.Error("bind account failed", "error", err)
		return errorResponse(http.StatusBadRequest, err.Error()), nil
	}
	return jsonResponse(http.StatusOK, cfg), nil
}

func (r *Router) handleSetApprovers(ctx context.Context, body []byte) (events.APIGatewayV2HTTPResponse, error) {
	var input models.SetApproversInput
	if err := json.Unmarshal(body, &input); err != nil {
		return errorResponse(http.StatusBadRequest, "invalid request body: "+err.Error()), nil
	}

	configs, err := r.Handler.HandleSetApprovers(ctx, input)
	if err != nil {
		slog.Error("set approvers failed", "error", err)
		return errorResponse(http.StatusBadRequest, err.Error()), nil
	}
	return jsonResponse(http.StatusOK, configs), nil
}

func (r *Router) handleGetBoundAccounts(ctx context.Context, queryParams map[string]string) (events.APIGatewayV2HTTPResponse, error) {
	channelID := queryParams["channel_id"]
	configs, err := r.Handler.HandleGetBoundAccounts(ctx, channelID)
	if err != nil {
		slog.Error("get bound accounts failed", "error", err)
		return errorResponse(http.StatusBadRequest, err.Error()), nil
	}
	return jsonResponse(http.StatusOK, configs), nil
}

// matchPath checks if a path matches the pattern /prefix{id}/suffix.
func matchPath(path, prefix, suffix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	if !strings.HasSuffix(path, suffix) {
		return false
	}
	// Ensure there's actually an ID between prefix and suffix.
	middle := path[len(prefix) : len(path)-len(suffix)]
	return len(middle) > 0
}

// extractPathParam extracts the ID from /prefix{id}/suffix.
func extractPathParam(path, prefix, suffix string) string {
	return path[len(prefix) : len(path)-len(suffix)]
}

// jsonResponse creates an API Gateway response with JSON body.
func jsonResponse(statusCode int, body interface{}) events.APIGatewayV2HTTPResponse {
	b, err := json.Marshal(body)
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "failed to marshal response")
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: statusCode,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: string(b),
	}
}

// errorResponse creates an API Gateway error response.
func errorResponse(statusCode int, message string) events.APIGatewayV2HTTPResponse {
	body := fmt.Sprintf(`{"message":%q}`, message)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: statusCode,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: body,
	}
}
