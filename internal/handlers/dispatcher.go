package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/aws/aws-lambda-go/events"
)

// Dispatcher routes incoming Lambda events to the appropriate handler
// based on whether they originate from API Gateway or Step Functions.
type Dispatcher struct {
	Router        *Router
	ActionHandler *ActionHandler
}

// NewDispatcher creates a new multi-event dispatcher.
func NewDispatcher(router *Router, actionHandler *ActionHandler) *Dispatcher {
	return &Dispatcher{
		Router:        router,
		ActionHandler: actionHandler,
	}
}

// eventProbe is used to detect the event source by inspecting key fields.
type eventProbe struct {
	Action         string          `json:"action"`
	RequestContext json.RawMessage `json:"requestContext"`
}

// Handle inspects the raw Lambda event and dispatches to the correct handler.
// - Events with an "action" field are Step Functions action payloads.
// - Events with a "requestContext" field are API Gateway V2 HTTP events.
func (d *Dispatcher) Handle(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var probe eventProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("unmarshal event probe: %w", err)
	}

	// Step Functions action payload — has an "action" field.
	if probe.Action != "" {
		slog.Info("dispatching to Step Functions action handler", "action", probe.Action)
		return d.ActionHandler.Handle(ctx, raw)
	}

	// API Gateway V2 HTTP event — has a "requestContext" field.
	if probe.RequestContext != nil {
		slog.Info("dispatching to API Gateway router")
		var event events.APIGatewayV2HTTPRequest
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, fmt.Errorf("unmarshal API Gateway event: %w", err)
		}
		return d.Router.Route(ctx, event)
	}

	return nil, fmt.Errorf("unrecognized event format: no 'action' or 'requestContext' field found")
}
