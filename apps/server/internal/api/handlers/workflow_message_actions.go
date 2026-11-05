package handlers

import (
	"context"
	"errors"

	"github.com/openpost/backend/internal/services/engagement"
	"github.com/openpost/backend/internal/services/messaging"
	"github.com/openpost/backend/internal/services/workflows"
	"github.com/openpost/backend/internal/services/workspaceaccess"

	analyticsservice "github.com/openpost/backend/internal/services/analytics"
)

// NewWorkflowActionsWithInbox adds the direct-message owner to workflow effects.
func NewWorkflowActionsWithInbox(publications *PublicationHandler, builds *PublicationBuildHandler, analytics *analyticsservice.Service, inbox *engagement.Service, messages *messaging.Service) workflows.Actions {
	return workflowActions{publications: publications, builds: builds, analytics: analytics, engagement: inbox, messaging: messages}
}

func (a workflowActions) messageReply(ctx context.Context, in workflows.EffectRequest) (workflows.EffectResult, error) {
	if a.messaging == nil {
		return workflows.EffectResult{}, errors.New("message automation is unavailable")
	}
	approve, _ := in.Inputs["approved"].(bool)
	reply, err := a.messaging.AdvanceAutomationReply(ctx, messaging.AutomationRequest{Authority: in.Authority, WorkflowID: workflowText(in.Inputs, "workflow_id"), RunID: in.RunID, StepID: in.StepID, TriggerMessageID: workflowText(in.Inputs, "message_id"), Body: workflowText(in.Inputs, "text"), Delivery: workflowText(in.Inputs, "delivery"), Approve: approve})
	if err != nil {
		return workflows.EffectResult{}, err
	}
	return messageEffect(reply, approve)
}

// Every stop carries its reason. A policy stop ends the run as failed so the
// reason appears on the run; losing to another workflow is an ordinary outcome.
func messageEffect(reply messaging.AutomationMessage, approve bool) (workflows.EffectResult, error) {
	output := map[string]any{"message_id": reply.TriggerMessageID, "conversation_id": reply.ConversationID, "status": reply.State, "text": reply.Body, "reason": reply.Reason, "sent_message_id": reply.MessageID}
	if reply.ID == "" {
		return workflows.EffectResult{Stop: true, Output: output}, nil
	}
	switch reply.State {
	case messaging.AutomationSent:
		return workflows.EffectResult{Output: output}, nil
	case messaging.AutomationAwaitingReview:
		return workflows.EffectResult{Review: true, Output: output}, nil
	case messaging.AutomationReady, messaging.AutomationSending:
		return workflows.EffectResult{Pending: true, Output: output}, nil
	}
	if approve {
		return workflows.EffectResult{Stop: true, Output: output}, nil
	}
	return workflows.EffectResult{Output: output}, errors.New(reply.Reason)
}

func (a workflowActions) stopWorkflowMessages(ctx context.Context, authority workspaceaccess.StoredAuthority, workflowID, runID string) error {
	if a.messaging == nil {
		return nil
	}
	return a.messaging.StopWorkflowMessages(ctx, authority, workflowID, runID)
}

func (a workflowActions) ValidateMessageSource(ctx context.Context, authority workspaceaccess.StoredAuthority, source workflows.Source) error {
	if a.messaging == nil {
		return errors.New("message automation is unavailable")
	}
	for _, id := range source.AccountIDs {
		if err := a.messaging.CheckAutomationAccount(ctx, authority, id); err != nil {
			return err
		}
	}
	return nil
}
