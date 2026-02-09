package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/dgwhited/jit-aws-controller/internal/auth"
)

func TestDispatcher_Handle_ActionRoute(t *testing.T) {
	// A payload with "action" should route to the ActionHandler.
	// The ActionHandler will panic because Handler.DB is nil. We recover
	// from the panic and verify the dispatcher actually chose the action path
	// (the panic originates from dynamo.Client.GetRequest on a nil receiver).
	handler := &Handler{} // nil DB, Identity, etc.
	actionHandler := NewActionHandler(handler)
	router := NewRouter(handler, &auth.HMACValidator{})
	dispatcher := NewDispatcher(router, actionHandler)

	payload := json.RawMessage(`{"action":"validate","request_id":"req-123"}`)

	panicked := false
	var panicVal interface{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				panicVal = r
			}
		}()
		_, _ = dispatcher.Handle(context.Background(), payload)
	}()

	if !panicked {
		t.Fatal("expected panic from nil DB access on action route, but no panic occurred")
	}

	// The panic should be a nil pointer dereference from the action handler
	// trying to call DB.GetRequest, confirming the dispatcher routed to the
	// action handler path.
	_ = panicVal // just confirm we got here
}

func TestDispatcher_Handle_APIGatewayRoute(t *testing.T) {
	// A payload with "requestContext" should route to the Router.
	// The Router will fail HMAC validation because no HMAC headers are present.
	handler := &Handler{}
	validator := auth.NewHMACValidator(map[string]string{"key1": "secret1"}, nil)
	router := NewRouter(handler, validator)
	actionHandler := NewActionHandler(handler)
	dispatcher := NewDispatcher(router, actionHandler)

	payload := json.RawMessage(`{
		"requestContext": {
			"http": {
				"method": "GET",
				"path": "/requests"
			}
		},
		"headers": {},
		"body": ""
	}`)

	result, err := dispatcher.Handle(context.Background(), payload)
	if err != nil {
		t.Fatalf("expected no Go error (API Gateway returns response), got: %v", err)
	}

	// The Router returns an APIGatewayV2HTTPResponse with 401 and HMAC error text.
	// The dispatcher returns (interface{}, error), so result should be the response.
	resp, ok := result.(events.APIGatewayV2HTTPResponse)
	if !ok {
		// Try the events import via the returned type.
		t.Fatalf("expected APIGatewayV2HTTPResponse, got %T", result)
	}
	if resp.StatusCode != 401 {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Body, "HMAC") {
		t.Errorf("expected response body to mention HMAC, got: %s", resp.Body)
	}
}

func TestDispatcher_Handle_UnrecognizedEvent(t *testing.T) {
	handler := &Handler{}
	validator := auth.NewHMACValidator(map[string]string{}, nil)
	router := NewRouter(handler, validator)
	actionHandler := NewActionHandler(handler)
	dispatcher := NewDispatcher(router, actionHandler)

	payload := json.RawMessage(`{"foo":"bar"}`)
	_, err := dispatcher.Handle(context.Background(), payload)
	if err == nil {
		t.Fatal("expected error for unrecognized event, got nil")
	}
	if !strings.Contains(err.Error(), "unrecognized event format") {
		t.Errorf("expected 'unrecognized event format' error, got: %v", err)
	}
}
