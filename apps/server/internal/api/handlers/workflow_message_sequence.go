package handlers

import (
	"context"
	"errors"

	"github.com/openpost/backend/internal/services/messaging"
	"github.com/openpost/backend/internal/services/workflows"
)

func (a workflowActions) messageSequence(ctx context.Context, in workflows.EffectRequest) (workflows.EffectResult, error) {
	if a.messaging == nil {
		return workflows.EffectResult{}, errors.New("message automation is unavailable")
	}
	authored, err := workflows.ParseMessageSequence(in.Inputs["messages"])
	if err != nil {
		return workflows.EffectResult{}, err
	}
	approve, _ := in.Inputs["approved"].(bool)
	request := messaging.SequenceRequest{Authority: in.Authority, WorkflowID: workflowText(in.Inputs, "workflow_id"), RunID: in.RunID, StepID: in.StepID, TriggerMessageID: workflowText(in.Inputs, "message_id"), Delivery: workflowText(in.Inputs, "delivery"), ApprovedBody: workflowText(in.Inputs, "text"), Approve: approve}
	for _, message := range authored {
		request.Messages = append(request.Messages, messaging.SequenceMessage{Text: message.Text, DelayMinutes: message.DelayMinutes})
	}
	if itemID := workflowText(in.Inputs, "engagement_id"); itemID != "" {
		if request.Comment, err = a.commentSequenceAnchor(ctx, in, itemID); err != nil {
			return workflows.EffectResult{}, err
		}
	}
	progress, err := a.messaging.AdvanceMessageSequence(ctx, request)
	if err != nil {
		return workflows.EffectResult{}, err
	}
	return sequenceEffect(progress, approve)
}

func (a workflowActions) commentSequenceAnchor(ctx context.Context, in workflows.EffectRequest, itemID string) (*messaging.CommentSequenceAnchor, error) {
	if a.engagement == nil {
		return nil, errors.New("comment reply automation is unavailable")
	}
	window, err := workflows.MessageResponseWindow(in.Inputs["response_window_minutes"])
	if err != nil {
		return nil, err
	}
	reply, err := a.engagement.RunPrivateReply(ctx, in.Authority, in.RunID, itemID)
	if err != nil {
		return nil, err
	}
	return &messaging.CommentSequenceAnchor{ItemID: reply.ItemID, AccountID: reply.AccountID, AuthorRemoteID: reply.AuthorRemoteID, RepliedAt: reply.SentAt, RespondBy: reply.SentAt.Add(window)}, nil
}

// A policy stop or delivery failure ends the run as failed so its reason stays on
// the run. No response is an ordinary outcome: the run ends, and says why.
func sequenceEffect(progress messaging.SequenceProgress, approve bool) (workflows.EffectResult, error) {
	reply := progress.Reply
	status := progress.State
	if status == messaging.SequenceComplete {
		status = messaging.AutomationSent
	}
	output := map[string]any{"message_id": reply.TriggerMessageID, "conversation_id": reply.ConversationID, "status": status, "text": reply.Body, "reason": progress.Reason, "sent_message_id": reply.MessageID, "sent": progress.Sent, "total": progress.Total}
	switch progress.State {
	case messaging.SequenceComplete:
		return workflows.EffectResult{Output: output}, nil
	case messaging.SequenceNoResponse, messaging.AutomationSkipped:
		return workflows.EffectResult{Stop: true, Output: output}, nil
	case messaging.AutomationAwaitingReview:
		return workflows.EffectResult{Review: true, Output: output}, nil
	case messaging.SequenceWaitingResponse, messaging.AutomationScheduled:
		wake := progress.WakeAt
		return workflows.EffectResult{Pending: true, WakeAt: &wake, Output: output}, nil
	case messaging.AutomationReady, messaging.AutomationSending, messaging.AutomationSent:
		return workflows.EffectResult{Pending: true, Output: output}, nil
	}
	if approve {
		return workflows.EffectResult{Stop: true, Output: output}, nil
	}
	return workflows.EffectResult{Output: output}, errors.New(progress.Reason)
}
