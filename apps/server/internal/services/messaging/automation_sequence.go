package messaging

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/inboxchanges"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/uptrace/bun"
)

const (
	// AutomationScheduled holds a follow-up until its delay has passed.
	AutomationScheduled = "scheduled"

	SequenceWaitingResponse = "waiting_response"
	SequenceNoResponse      = "no_response"
	SequenceComplete        = "complete"
	SequenceStopped         = "stopped"

	// A follow-up is allowed only within this long of the person's response.
	sequenceResponseWindow = 24 * time.Hour
	sequenceResponsePoll   = 5 * time.Minute

	stopReasonComment    = "A person took over the comment that started this sequence."
	stopReasonNoResponse = "The person did not respond to the private reply in time, so no follow-up was sent."
)

// SequenceMessage is one authored follow-up. Its delay counts from the person's
// response for the first message and from the previous delivery for later ones.
type SequenceMessage struct {
	Text         string `json:"text"`
	DelayMinutes int    `json:"delay_minutes"`
}

// CommentSequenceAnchor continues a comment campaign after its private reply.
type CommentSequenceAnchor struct {
	ItemID, AccountID, AuthorRemoteID string
	RepliedAt, RespondBy              time.Time
}

// SequenceRequest advances one ordered follow-up step. Exactly one of
// TriggerMessageID and Comment says which response the sequence answers.
type SequenceRequest struct {
	Authority                                   workspaceaccess.StoredAuthority
	WorkflowID, RunID, StepID, TriggerMessageID string
	Comment                                     *CommentSequenceAnchor
	Messages                                    []SequenceMessage
	Delivery, ApprovedBody                      string
	Approve                                     bool
}

// SequenceProgress is the stored position of a sequence after one advance.
type SequenceProgress struct {
	State  string
	Reason string
	Reply  AutomationMessage
	Sent   int
	Total  int
	WakeAt time.Time
}

// AdvanceMessageSequence converges a step onto one owned delivery per position.
// Every position rechecks window, opt-out and takeover, so nothing authored before
// a delay can reach a person after one of those changed. A failure is final: the
// sequence never skips ahead and never repeats an uncertain delivery.
func (s *Service) AdvanceMessageSequence(ctx context.Context, in SequenceRequest) (SequenceProgress, error) {
	var progress SequenceProgress
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(ctx, tx, in.Authority.WorkspaceID); err != nil {
			return err
		}
		if err := s.authorizeAutomationRun(ctx, tx, in.request("", "")); err != nil {
			return err
		}
		var err error
		progress, err = s.advanceSequence(ctx, tx, in)
		progress.Total = len(in.Messages)
		return err
	})
	return progress, err
}

func (in SequenceRequest) request(triggerID, body string) AutomationRequest {
	return AutomationRequest{Authority: in.Authority, WorkflowID: in.WorkflowID, RunID: in.RunID, StepID: in.StepID, TriggerMessageID: triggerID, Body: body, Delivery: in.Delivery, Approve: in.Approve}
}

func (s *Service) advanceSequence(ctx context.Context, tx bun.Tx, in SequenceRequest) (SequenceProgress, error) {
	var rows []AutomationMessage
	if err := tx.NewSelect().Model(&rows).Where("run_id = ? AND step_id = ?", in.RunID, in.StepID).Order("sequence_position ASC").Scan(ctx); err != nil {
		return SequenceProgress{}, err
	}
	if len(rows) == 0 {
		return s.startSequence(ctx, tx, in)
	}
	current := rows[len(rows)-1]
	trigger, conversation, err := s.loadAutomationTrigger(ctx, tx, in.request(current.TriggerMessageID, ""))
	if err != nil {
		return SequenceProgress{}, err
	}
	if current.State != AutomationSent {
		return s.advanceSequenceRow(ctx, tx, trigger, conversation, in, current)
	}
	next := current.SequencePosition + 1
	if next >= len(in.Messages) {
		return SequenceProgress{State: SequenceComplete, Reply: current, Sent: len(in.Messages)}, nil
	}
	reply, err := s.claimSequenceRow(ctx, tx, trigger, conversation, in, next, current.UpdatedAt)
	if err != nil {
		return SequenceProgress{}, err
	}
	return s.advanceSequenceRow(ctx, tx, trigger, conversation, in, reply)
}

func (s *Service) startSequence(ctx context.Context, tx bun.Tx, in SequenceRequest) (SequenceProgress, error) {
	triggerID := in.TriggerMessageID
	if in.Comment != nil {
		progress, response, err := s.findSequenceResponse(ctx, tx, in)
		if err != nil || response.ID == "" {
			return progress, err
		}
		triggerID = response.ID
	}
	trigger, conversation, err := s.loadAutomationTrigger(ctx, tx, in.request(triggerID, ""))
	if err != nil {
		return SequenceProgress{}, err
	}
	var owner AutomationMessage
	err = tx.NewSelect().Model(&owner).Where("trigger_message_id = ? AND sequence_position = 0", trigger.ID).Scan(ctx)
	if err == nil {
		return SequenceProgress{State: AutomationSkipped, Reason: stopReasonOwned, Reply: AutomationMessage{TriggerMessageID: trigger.ID, ConversationID: conversation.ID}}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SequenceProgress{}, err
	}
	if in.Comment != nil {
		stopped, err := s.stopForEarlierHumanReply(ctx, tx, conversation, in.Comment.RepliedAt)
		if err != nil || stopped.State != "" {
			return stopped, err
		}
	}
	reply, err := s.claimSequenceRow(ctx, tx, trigger, conversation, in, 0, automationMessageTime(trigger))
	if err != nil {
		return SequenceProgress{}, err
	}
	return s.advanceSequenceRow(ctx, tx, trigger, conversation, in, reply)
}

// A person writing in the thread before the response counts as takeover, even
// though the response is what a later check measures from.
func (s *Service) stopForEarlierHumanReply(ctx context.Context, tx bun.Tx, conversation models.Conversation, after time.Time) (SequenceProgress, error) {
	var account models.SocialAccount
	if err := tx.NewSelect().Model(&account).Where("id = ?", conversation.SocialAccountID).Scan(ctx); err != nil {
		return SequenceProgress{}, err
	}
	human, err := humanRepliedAfter(ctx, tx, conversation, account, after)
	if err != nil || !human {
		return SequenceProgress{}, err
	}
	if err := s.stopConversationTx(ctx, tx, conversation, "provider", stopReasonHuman); err != nil {
		return SequenceProgress{}, err
	}
	return SequenceProgress{State: SequenceStopped, Reason: stopReasonHuman}, nil
}

// findSequenceResponse is the only place a comment campaign can become eligible
// to message someone: that person's own message, sent after the private reply.
func (s *Service) findSequenceResponse(ctx context.Context, tx bun.Tx, in SequenceRequest) (SequenceProgress, models.DirectMessage, error) {
	anchor := in.Comment
	reason, err := commentStopReason(ctx, tx, anchor.ItemID)
	if err != nil {
		return SequenceProgress{}, models.DirectMessage{}, err
	}
	if reason == "" {
		optedOut, err := tx.NewSelect().Table("engagement_automation_optouts").Where("social_account_id = ? AND author_remote_id = ?", anchor.AccountID, anchor.AuthorRemoteID).Exists(ctx)
		if err != nil {
			return SequenceProgress{}, models.DirectMessage{}, err
		}
		if optedOut {
			reason = stopReasonOptedOut
		}
	}
	if reason != "" {
		return SequenceProgress{State: SequenceStopped, Reason: reason}, models.DirectMessage{}, nil
	}
	var response models.DirectMessage
	err = tx.NewSelect().Model(&response).
		Join("JOIN conversations AS c ON c.id = direct_message.conversation_id").
		Where("direct_message.workspace_id = ? AND direct_message.direction = 'inbound' AND direct_message.deleted_at IS NULL", in.Authority.WorkspaceID).
		Where("c.social_account_id = ? AND c.counterpart_remote_id = ? AND c.platform = 'instagram'", anchor.AccountID, anchor.AuthorRemoteID).
		Where("direct_message.remote_created_at > ? AND direct_message.remote_created_at <= ?", anchor.RepliedAt, anchor.RespondBy).
		Where("NOT EXISTS (SELECT 1 FROM message_automation_replies a WHERE a.trigger_message_id = direct_message.id AND a.sequence_position = 0)").
		Order("direct_message.remote_created_at ASC", "direct_message.id ASC").Limit(1).Scan(ctx)
	if err == nil {
		return SequenceProgress{}, response, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SequenceProgress{}, response, err
	}
	now := s.now()
	if !now.Before(anchor.RespondBy) {
		return SequenceProgress{State: SequenceNoResponse, Reason: stopReasonNoResponse}, response, nil
	}
	wake := now.Add(sequenceResponsePoll)
	if wake.After(anchor.RespondBy) {
		wake = anchor.RespondBy
	}
	return SequenceProgress{State: SequenceWaitingResponse, WakeAt: wake}, response, nil
}

func commentStopReason(ctx context.Context, db bun.IDB, itemID string) (string, error) {
	var item models.EngagementItem
	err := db.NewSelect().Model(&item).Where("id = ?", itemID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return stopReasonComment, nil
	}
	if err != nil {
		return "", err
	}
	if !item.AutomationStoppedAt.IsZero() || !item.DeletedAt.IsZero() {
		return stopReasonComment, nil
	}
	return "", nil
}

func automationMessageTime(message models.DirectMessage) time.Time {
	if message.RemoteCreatedAt.IsZero() {
		return message.CreatedAt
	}
	return message.RemoteCreatedAt
}

func (s *Service) claimSequenceRow(ctx context.Context, tx bun.Tx, trigger models.DirectMessage, conversation models.Conversation, in SequenceRequest, position int, base time.Time) (AutomationMessage, error) {
	authored := in.Messages[position]
	now := s.now()
	reply := AutomationMessage{ID: uuid.NewString(), WorkspaceID: conversation.WorkspaceID, ConversationID: conversation.ID, TriggerMessageID: trigger.ID, WorkflowID: in.WorkflowID, RunID: in.RunID, StepID: in.StepID, UserID: in.Authority.UserID, Body: strings.TrimSpace(authored.Text), State: AutomationScheduled, SequencePosition: position, SequenceLength: len(in.Messages), NotBefore: base.Add(time.Duration(authored.DelayMinutes) * time.Minute), CreatedAt: now, UpdatedAt: now}
	if in.Comment != nil {
		reply.CommentItemID = in.Comment.ItemID
	}
	issue := automatedBodyIssue(reply.Body)
	if issue == "" && !reply.NotBefore.Before(automationMessageTime(trigger).Add(sequenceResponseWindow)) {
		issue = stopReasonWindow
	}
	if issue != "" {
		reply.State, reply.Reason = AutomationCancelled, issue
	} else if position == 0 {
		looping, err := s.automationLooping(ctx, tx, conversation.ID)
		if err != nil {
			return reply, err
		}
		if looping {
			reply.State, reply.Reason = AutomationSkipped, stopReasonLoop
		}
	}
	if _, err := tx.NewInsert().Model(&reply).Exec(ctx); err != nil {
		return reply, err
	}
	return reply, inboxchanges.Append(ctx, tx, conversation.WorkspaceID, inboxchanges.Change{Kind: "conversation", ConversationID: conversation.ID})
}

func (s *Service) advanceSequenceRow(ctx context.Context, tx bun.Tx, trigger models.DirectMessage, conversation models.Conversation, in SequenceRequest, reply AutomationMessage) (SequenceProgress, error) {
	body := strings.TrimSpace(in.ApprovedBody)
	if body == "" {
		body = reply.Body
	}
	if err := s.advanceClaimedReply(ctx, tx, trigger, conversation, in.request(trigger.ID, body), &reply); err != nil {
		return SequenceProgress{}, err
	}
	progress := SequenceProgress{State: reply.State, Reason: reply.Reason, Reply: reply, Sent: reply.SequencePosition}
	switch reply.State {
	case AutomationSent:
		progress.Sent = reply.SequencePosition + 1
	case AutomationScheduled:
		progress.WakeAt = reply.NotBefore
	case AutomationAwaitingReview, AutomationReady, AutomationSending:
	default:
		progress.State = SequenceStopped
	}
	return progress, nil
}
