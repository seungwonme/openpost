package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/openpost/backend/internal/api/middleware"
	"github.com/openpost/backend/internal/services/messaging"
)

type MessageAutomationInput struct {
	ConversationID string `path:"conversation_id"`
	WorkspaceID    string `query:"workspace_id" required:"true"`
}
type MessageAutomationOutput struct {
	Body messaging.ConversationAutomation
}

func (h *EngagementMessagingHandler) registerMessageAutomation(api huma.API) {
	operation := func(id, method, path, summary string) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary, Tags: []string{tagMessaging}, MaxBodyBytes: 1024, Middlewares: huma.Middlewares{middleware.AuthMiddleware(api, h.auth)}, Errors: []int{403, 404, 500, 503}}
	}
	huma.Register(api, operation("get-message-automation", http.MethodGet, "/messages/{conversation_id}/automation", "Inspect automatic messages in a conversation"), func(ctx context.Context, in *MessageAutomationInput) (*MessageAutomationOutput, error) {
		return h.messageAutomationState(ctx, in)
	})
	huma.Register(api, operation("stop-message-automation", http.MethodPost, "/messages/{conversation_id}/automation/stop", "Take over a conversation and stop automatic messages"), func(ctx context.Context, in *MessageAutomationInput) (*MessageAutomationOutput, error) {
		if h.messaging == nil {
			return nil, huma.Error503ServiceUnavailable("messaging service is unavailable")
		}
		if err := h.messaging.StopConversationAutomation(ctx, messagingActor(ctx), in.WorkspaceID, in.ConversationID); err != nil {
			return nil, messagingHTTPError(err, "could not stop automatic messages")
		}
		return h.messageAutomationState(ctx, in)
	})
	huma.Register(api, operation("resume-message-automation", http.MethodPost, "/messages/{conversation_id}/automation/resume", "Let future messages in a conversation start automation again"), func(ctx context.Context, in *MessageAutomationInput) (*MessageAutomationOutput, error) {
		if h.messaging == nil {
			return nil, huma.Error503ServiceUnavailable("messaging service is unavailable")
		}
		if err := h.messaging.ResumeConversationAutomation(ctx, messagingActor(ctx), in.WorkspaceID, in.ConversationID); err != nil {
			return nil, messagingHTTPError(err, "could not resume automatic messages")
		}
		return h.messageAutomationState(ctx, in)
	})
}

func (h *EngagementMessagingHandler) messageAutomationState(ctx context.Context, in *MessageAutomationInput) (*MessageAutomationOutput, error) {
	if h.messaging == nil {
		return nil, huma.Error503ServiceUnavailable("messaging service is unavailable")
	}
	state, err := h.messaging.ConversationAutomationState(ctx, messagingActor(ctx), in.WorkspaceID, in.ConversationID)
	if err != nil {
		return nil, messagingHTTPError(err, "could not load automatic messages")
	}
	return &MessageAutomationOutput{Body: state}, nil
}
