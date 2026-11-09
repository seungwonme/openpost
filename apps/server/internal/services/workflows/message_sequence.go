package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/openpost/backend/internal/services/inboxchanges"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/uptrace/bun"
)

// KindMessageSequence sends ordered follow-ups after a person's response.
const KindMessageSequence = "message_sequence"

const (
	maxSequenceMessages = 5
	// How long a comment campaign waits for a response before it stops. The
	// provider sets no such limit; this keeps a run from waiting indefinitely.
	maxResponseWindowMinutes = 7 * 24 * 60
	maxMessageBytes          = 1000
)

// SequenceMessage is one authored follow-up.
type SequenceMessage struct {
	Text         string `json:"text"`
	DelayMinutes int    `json:"delay_minutes"`
}

// countActiveRuns is the workspace limit. A run waiting for a person's response
// can wait for days and uses no worker until it wakes, so people who never respond
// cannot block every other workflow.
func countActiveRuns(ctx context.Context, db bun.IDB, workspaceID string) (int, error) {
	return db.NewSelect().Model((*runRecord)(nil)).Where("workspace_id = ? AND state IN (?)", workspaceID, bun.List([]string{StateQueued, StateRunning, StateWaiting, StateApproval})).
		Where("id NOT IN (SELECT run_id FROM message_sequence_waits)").Count(ctx)
}

// wakeRespondedRuns is one batched check from the workflow sweep. It wakes a run
// only when something it waits for has happened, so waiting costs no workspace
// lock and no provider call per run.
func (s *Service) wakeRespondedRuns(ctx context.Context, now time.Time) error {
	_, err := s.db.NewUpdate().Model((*runRecord)(nil)).Set("wake_at = ?", now).
		Where("state = ? AND wake_at > ?", StateWaiting, now).
		Where(`id IN (SELECT w.run_id FROM message_sequence_waits w WHERE
			EXISTS (SELECT 1 FROM conversations c JOIN direct_messages d ON d.conversation_id = c.id WHERE c.social_account_id = w.account_id AND c.counterpart_remote_id = w.author_remote_id AND c.platform = 'instagram' AND d.direction = 'inbound' AND d.deleted_at IS NULL AND d.remote_created_at > w.replied_at AND d.remote_created_at <= w.respond_by)
			OR EXISTS (SELECT 1 FROM engagement_automation_optouts o WHERE o.social_account_id = w.account_id AND o.author_remote_id = w.author_remote_id)
			OR EXISTS (SELECT 1 FROM engagement_items i WHERE i.id = w.item_id AND (i.automation_stopped_at IS NOT NULL OR i.deleted_at IS NOT NULL)))`).Exec(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.NewRaw(`DELETE FROM message_sequence_waits WHERE run_id IN (SELECT id FROM workflow_runs WHERE state NOT IN (?))`, bun.List([]string{StateQueued, StateRunning, StateWaiting, StateApproval})).Exec(ctx)
	return err
}

// An approval that can no longer produce a message would otherwise wait for the run
// lifetime and, for a sequence, hold the one active sequence the person can have.
const stopReasonApprovalLate = "The Instagram messaging window closed before this message was approved, so it was not sent."

type lateMessageApproval struct {
	ReceiptID      string `bun:"receipt_id"`
	RunID          string `bun:"run_id"`
	WorkspaceID    string `bun:"workspace_id"`
	ConversationID string `bun:"conversation_id"`
}

// endLateMessageApprovals is one read from the workflow sweep. The conversation's
// window closed, or a sequence's 24 hours from the response passed, which a later
// message does not extend.
func (s *Service) endLateMessageApprovals(ctx context.Context, now time.Time) error {
	var late []lateMessageApproval
	if err := s.db.NewRaw(`SELECT a.id AS receipt_id, a.run_id, a.workspace_id, a.conversation_id FROM message_automation_replies a
		JOIN workflow_runs r ON r.id = a.run_id
		JOIN conversations c ON c.id = a.conversation_id
		JOIN direct_messages d ON d.id = a.trigger_message_id
		WHERE a.state = 'awaiting_review' AND r.state = ?
		AND (c.messaging_window_expires_at IS NULL OR c.messaging_window_expires_at <= ? OR (a.sequence_length > 0 AND d.remote_created_at <= ?))
		LIMIT 100`, StateApproval, now, now.Add(-messageWindowMinutes*time.Minute)).Scan(ctx, &late); err != nil {
		return err
	}
	for _, approval := range late {
		if err := s.endLateMessageApproval(ctx, approval, now); err != nil {
			// One run that cannot be ended must not hold up the rest of the batch.
			log.Printf("Failed to end late message approval %s: %v", approval.RunID, err)
		}
	}
	return nil
}

func (s *Service) endLateMessageApproval(ctx context.Context, approval lateMessageApproval, now time.Time) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(ctx, tx, approval.WorkspaceID); err != nil {
			return err
		}
		var run runRecord
		if err := tx.NewSelect().Model(&run).Where("id = ? AND state = ?", approval.RunID, StateApproval).Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if _, err := tx.NewRaw(`UPDATE message_automation_replies SET state = 'cancelled', reason = ?, updated_at = ? WHERE id = ? AND state = 'awaiting_review'`, stopReasonApprovalLate, now, approval.ReceiptID).Exec(ctx); err != nil {
			return err
		}
		if err := s.cancelMessageRun(ctx, tx, run, stopReasonApprovalLate); err != nil {
			return err
		}
		return inboxchanges.Append(ctx, tx, approval.WorkspaceID, inboxchanges.Change{Kind: "conversation", ConversationID: approval.ConversationID})
	})
}

// A sequence reads each position's text from its receipt, so one review must not
// leave text behind for the next one.
func keepReviewedText(step *StepResult, effect EffectResult) {
	if step.Kind != KindMessageSequence {
		step.Inputs["text"] = effect.Output["text"]
	}
}

// ParseMessageSequence reads the ordered messages of a message_sequence step.
func ParseMessageSequence(value any) ([]SequenceMessage, error) {
	raw, isText := value.(string)
	if !isText {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, invalid("follow-up messages must be a list")
		}
		raw = string(encoded)
	}
	var messages []SequenceMessage
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		return nil, invalid("follow-up messages must be a list of text and whole-minute delays")
	}
	if len(messages) == 0 || len(messages) > maxSequenceMessages {
		return nil, invalid(fmt.Sprintf("add between 1 and %d follow-up messages", maxSequenceMessages))
	}
	for i, message := range messages {
		text := strings.TrimSpace(message.Text)
		if text == "" {
			return nil, invalid(fmt.Sprintf("follow-up message %d needs text", i+1))
		}
		if len(text) > maxMessageBytes && !strings.Contains(text, "{{") {
			return nil, invalid(fmt.Sprintf("follow-up message %d exceeds Instagram's %d byte limit", i+1, maxMessageBytes))
		}
		if message.DelayMinutes < 0 || message.DelayMinutes >= messageWindowMinutes {
			return nil, invalid(fmt.Sprintf("follow-up message %d needs a delay under 24 hours", i+1))
		}
	}
	return messages, nil
}

// MessageResponseWindow is how long a comment campaign waits for a response.
func MessageResponseWindow(value any) (time.Duration, error) {
	if value == nil || value == "" {
		return maxResponseWindowMinutes * time.Minute, nil
	}
	minutes, err := number(value)
	if err != nil || minutes < 1 || minutes > maxResponseWindowMinutes {
		return 0, invalid("the response window must be between 1 minute and 7 days")
	}
	return time.Duration(minutes * float64(time.Minute)), nil
}

func validateMessageSequenceStep(step Step) error {
	for name := range step.Inputs {
		if !slices.Contains([]string{"engagement_id", "message_id", "messages", "delivery", "response_window_minutes", "follow_gate"}, name) {
			return invalid("unsupported follow-up field: " + name)
		}
	}
	for _, name := range []string{"messages", "delivery", "response_window_minutes", "follow_gate"} {
		if step.Inputs[name].Reference != "" {
			return invalid("choose follow-up " + strings.ReplaceAll(name, "_", " ") + " directly")
		}
	}
	if !slices.Contains([]string{"review", "automatic"}, fmt.Sprint(step.Inputs["delivery"].Literal)) {
		return invalid("choose review or automatic delivery")
	}
	if _, err := ParseMessageSequence(step.Inputs["messages"].Literal); err != nil {
		return err
	}
	if _, err := ParseFollowGate(step.Inputs["follow_gate"].Literal); err != nil {
		return err
	}
	_, err := MessageResponseWindow(step.Inputs["response_window_minutes"].Literal)
	return err
}

// validateSequence binds follow-ups to the only people they can reach: someone
// whose own message started this run, or who answered this run's private reply.
func (w messageStepWalker) validateSequence(step Step, path messagePath) error {
	messages, err := ParseMessageSequence(step.Inputs["messages"].Literal)
	if err != nil {
		return err
	}
	delay := 0.0
	for _, message := range messages {
		delay += float64(message.DelayMinutes)
	}
	switch w.source.Kind {
	case "message_received":
		if step.Inputs["message_id"].Reference != "source.message_id" || step.Inputs["engagement_id"] != (Value{}) {
			return invalid(step.Name + ": follow-ups must answer the message that started this run")
		}
		delay += path.elapsed
	case "comment_received":
		if step.Inputs["engagement_id"].Reference != "source.engagement_id" || step.Inputs["message_id"] != (Value{}) {
			return invalid(step.Name + ": follow-ups must continue the comment that started this run")
		}
		if !path.private {
			return invalid(step.Name + ": follow-ups need a private reply earlier in this path")
		}
	default:
		return invalid(step.Name + ": follow-ups need a comment or incoming message trigger")
	}
	if delay >= messageWindowMinutes {
		return invalid(step.Name + ": these delays outlast Instagram's 24 hour messaging window")
	}
	return validateFollowGate(step, w.source, messages)
}
