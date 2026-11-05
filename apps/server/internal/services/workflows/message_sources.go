package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/uptrace/bun"
)

// The provider window is 24 hours from the person's message. A longer authored
// delay can never reach a send, so it is rejected before activation.
const messageWindowMinutes = 24 * 60

// MessageSourceValidator checks connected-account readiness at activation.
type MessageSourceValidator interface {
	ValidateMessageSource(context.Context, workspaceaccess.StoredAuthority, Source) error
}

func isInteractionSource(kind string) bool {
	return kind == "comment_received" || kind == "message_received"
}

func isMessageAction(kind string) bool {
	return kind == KindMessageReply || kind == KindMessageSequence
}

func isInboxAction(kind string) bool {
	return isCommentAction(kind) || isMessageAction(kind)
}

func isReviewedReply(kind string) bool {
	return kind == KindCommentReply || kind == KindPrivateReply || isMessageAction(kind)
}

func validateInboxStep(step Step) error {
	if isCommentAction(step.Kind) {
		return validateCommentStep(step)
	}
	if step.Kind == KindMessageSequence {
		return validateMessageSequenceStep(step)
	}
	if isMessageAction(step.Kind) {
		return validateMessageStep(step)
	}
	return nil
}

func messageResponseSteps(steps []Step) bool {
	for _, step := range steps {
		if isMessageAction(step.Kind) || messageResponseSteps(step.Then) || messageResponseSteps(step.Else) {
			return true
		}
	}
	return false
}

type messageSourceRow struct {
	ID                string    `bun:"id"`
	ConversationID    string    `bun:"conversation_id"`
	SocialAccountID   string    `bun:"social_account_id"`
	Body              string    `bun:"body"`
	RemoteCreatedAt   time.Time `bun:"remote_created_at"`
	CreatedAt         time.Time `bun:"created_at"`
	CounterpartName   string    `bun:"counterpart_name"`
	CounterpartHandle string    `bun:"counterpart_handle"`
	WindowExpiresAt   time.Time `bun:"window_expires_at"`
}

func messageSourceQuery(db bun.IDB, workspaceID string) *bun.SelectQuery {
	return db.NewSelect().TableExpr("direct_messages AS d").
		ColumnExpr("d.id, d.conversation_id, c.social_account_id, d.body, d.remote_created_at, d.created_at, c.counterpart_name, c.counterpart_handle, c.messaging_window_expires_at AS window_expires_at").
		Join("JOIN conversations AS c ON c.id = d.conversation_id").
		Join("JOIN social_accounts AS a ON a.id = c.social_account_id").
		Where("d.workspace_id = ? AND c.platform = 'instagram' AND d.direction = 'inbound' AND d.deleted_at IS NULL AND d.remote_message_id <> ''", workspaceID).
		// A connected account's own message is never a person's request, even when
		// it reaches another connected account and arrives as inbound.
		Where("c.counterpart_remote_id <> '' AND c.counterpart_remote_id <> a.account_id AND d.author_remote_id <> a.account_id").
		Where("NOT EXISTS (SELECT 1 FROM social_accounts own WHERE own.workspace_id = d.workspace_id AND own.platform = 'instagram' AND own.account_id = c.counterpart_remote_id)").
		Where("NOT EXISTS (SELECT 1 FROM engagement_automation_optouts o WHERE o.social_account_id = c.social_account_id AND o.author_remote_id = c.counterpart_remote_id)")
}

// Read canonical Inbox inventory. Both the provider creation time and the first
// local storage must follow activation, so history imports never start a run.
func (s *Service) messageItems(ctx context.Context, workspaceID string, source Source, since time.Time, record *workflowRecord) ([]SourceItem, error) {
	var rows []messageSourceRow
	query := messageSourceQuery(s.db, workspaceID).
		Where("c.social_account_id IN (?)", bun.List(source.AccountIDs)).
		Where("d.remote_created_at >= ? AND d.created_at >= ?", since, since).
		Limit(maxSourceItems)
	if record == nil {
		query = query.OrderExpr("d.created_at DESC, d.id DESC")
	} else {
		query = query.OrderExpr("d.created_at ASC, d.id ASC").
			Where("NOT EXISTS (SELECT 1 FROM workflow_events e WHERE e.workflow_id = ? AND e.source_fingerprint = ? AND e.event_key = d.id)", record.ID, record.SourceFingerprint)
	}
	if err := query.Scan(ctx, &rows); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	items := make([]SourceItem, 0, len(rows))
	for _, row := range rows {
		// The ledger records excluded messages, so bounded pages keep progressing.
		// A closed window cannot be answered, so it never spends the daily limit.
		excluded := !messageSourceMatches(source, row.Body) || !row.WindowExpiresAt.After(now)
		if excluded && record == nil {
			continue
		}
		name := row.CounterpartName
		if name == "" {
			name = row.CounterpartHandle
		}
		items = append(items, SourceItem{Excluded: excluded, ID: row.ID, MessageID: row.ID, ConversationID: row.ConversationID, AccountID: row.SocialAccountID, Body: boundedText(row.Body), Title: name, AuthorName: name, Platform: "instagram", CreatedAt: row.RemoteCreatedAt.UTC().Format(time.RFC3339)})
	}
	if record != nil {
		slices.Reverse(items)
	}
	return items, nil
}

func messageSourceMatches(source Source, body string) bool {
	if !commentKeywordMatches(body, source.Keywords, source.MatchMode) {
		return false
	}
	return len(source.ExcludedKeywords) == 0 || !commentKeywordMatches(body, source.ExcludedKeywords, source.MatchMode)
}

// Resolve overlap before admitting work, the same rule comment campaigns use:
// explicit priority wins and equal priority uses the older workflow.
func (s *Service) messageWorkflowWins(ctx context.Context, db bun.IDB, current workflowRecord, messageID string) (bool, error) {
	var own Definition
	if err := json.Unmarshal([]byte(current.PublishedJSON), &own); err != nil {
		return false, err
	}
	if !messageResponseSteps(own.Steps) {
		return true, nil
	}
	var row messageSourceRow
	if err := messageSourceQuery(db, current.WorkspaceID).Where("d.id = ?", messageID).Scan(ctx, &row); err != nil {
		// The person opted out or deleted the message after this poll read it.
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	var candidates []workflowRecord
	if err := db.NewSelect().Model(&candidates).Where("workspace_id = ? AND enabled = ?", current.WorkspaceID, true).Order("created_at ASC", "id ASC").Limit(maxWorkflows).Scan(ctx); err != nil {
		return false, err
	}
	winner, priority := "", 101
	for _, candidate := range candidates {
		var def Definition
		if err := json.Unmarshal([]byte(candidate.PublishedJSON), &def); err != nil {
			return false, err
		}
		if !messageResponseSteps(def.Steps) || !messageCandidateMatches(candidate, def.Source, row) {
			continue
		}
		candidatePriority := def.Source.Priority
		if candidatePriority == 0 {
			candidatePriority = 50
		}
		if candidatePriority < priority {
			winner, priority = candidate.ID, candidatePriority
		}
	}
	return winner == current.ID, nil
}

func messageCandidateMatches(candidate workflowRecord, source Source, row messageSourceRow) bool {
	if source.Kind != "message_received" || !slices.Contains(source.AccountIDs, row.SocialAccountID) || candidate.SourceStartedAt == nil {
		return false
	}
	if row.RemoteCreatedAt.Before(*candidate.SourceStartedAt) || row.CreatedAt.Before(*candidate.SourceStartedAt) {
		return false
	}
	return messageSourceMatches(source, row.Body)
}

func validateMessageSource(source Source) error {
	if source.Priority < 0 || source.Priority > 100 {
		return invalid("reply priority must be between 1 and 100")
	}
	if len(source.AccountIDs) == 0 {
		return invalid("choose at least one Instagram account")
	}
	if len(source.AccountIDs) > 50 || len(source.Keywords) > 20 || len(source.ExcludedKeywords) > 20 {
		return invalid("message trigger configuration is too large")
	}
	for _, keyword := range append(slices.Clone(source.Keywords), source.ExcludedKeywords...) {
		if strings.TrimSpace(keyword) == "" || len(keyword) > 100 {
			return invalid("keywords must contain 1 to 100 bytes")
		}
	}
	if source.DailyLimit < 1 || source.DailyLimit > 1000 {
		return invalid("set a daily message run limit between 1 and 1000")
	}
	if source.MatchMode != "" && !slices.Contains([]string{"word", "exact", "contains"}, source.MatchMode) {
		return invalid("choose a supported keyword match")
	}
	return nil
}

func validateMessageStep(step Step) error {
	for name := range step.Inputs {
		if !slices.Contains([]string{"message_id", "text", "delivery"}, name) {
			return invalid("unsupported message reply field: " + name)
		}
	}
	delivery := step.Inputs["delivery"]
	if delivery.Reference != "" {
		return invalid("choose message delivery directly")
	}
	if delivery.Literal != nil && !slices.Contains([]string{"review", "automatic"}, fmt.Sprint(delivery.Literal)) {
		return invalid("choose review or automatic delivery")
	}
	return nil
}

// validateMessageAutomation binds a reply to the message that started this run,
// which rules out reaching anyone who has not messaged, and rejects a delay the
// provider window can never accommodate.
func validateMessageAutomation(def Definition) error {
	walker := messageStepWalker{source: def.Source}
	_, err := walker.walk(def.Steps, messagePath{})
	return err
}

type messageStepWalker struct{ source Source }

// messagePath is what is known about every run reaching a step.
type messagePath struct {
	elapsed  float64
	private  bool
	answered bool
}

func (w messageStepWalker) walk(steps []Step, path messagePath) (messagePath, error) {
	for _, step := range steps {
		switch {
		case step.Kind == KindWait:
			if minutes, err := number(step.Inputs["minutes"].Literal); step.Inputs["minutes"].Reference == "" && err == nil {
				path.elapsed += minutes
			}
		case step.Kind == KindPrivateReply || (step.Kind == KindCommentReply && step.Inputs["channel"].Literal == "private"):
			path.private = true
		case isMessageAction(step.Kind):
			if err := w.validateSend(step, path); err != nil {
				return path, err
			}
			path.answered = true
		}
		yes, err := w.walk(step.Then, path)
		if err != nil {
			return path, err
		}
		no, err := w.walk(step.Else, path)
		if err != nil {
			return path, err
		}
		path = messagePath{elapsed: max(yes.elapsed, no.elapsed), private: yes.private && no.private, answered: yes.answered || no.answered}
	}
	return path, nil
}

func (w messageStepWalker) validateSend(step Step, path messagePath) error {
	if path.answered {
		return invalid(step.Name + ": a run answers one message once, so use one reply or sequence on each path")
	}
	if step.Kind == KindMessageSequence {
		return w.validateSequence(step, path)
	}
	if w.source.Kind != "message_received" {
		return invalid(step.Name + ": message replies need the incoming message trigger")
	}
	if step.Inputs["message_id"].Reference != "source.message_id" {
		return invalid(step.Name + ": a message reply must answer the message that started this run")
	}
	if path.elapsed >= messageWindowMinutes {
		return invalid(step.Name + ": this delay outlasts Instagram's 24 hour messaging window")
	}
	return nil
}

func (s *Service) validateMessageAutomationAccess(ctx context.Context, authority workspaceaccess.StoredAuthority, definition Definition) error {
	if definition.Source.Kind != "message_received" && !messageResponseSteps(definition.Steps) {
		return nil
	}
	validator, ok := s.actions.(MessageSourceValidator)
	if !ok {
		return nil
	}
	if err := validator.ValidateMessageSource(ctx, authority, definition.Source); err != nil {
		return invalid(err.Error())
	}
	return nil
}

// CancelMessageAutomationRuns is the workflow half of a conversation takeover or
// opt-out. The messaging owner supplies only runs holding receipts in that
// conversation. The reason stays on the run for inspection.
func (s *Service) CancelMessageAutomationRuns(ctx context.Context, tx bun.Tx, workspaceID string, ids []string, reason string) error {
	var runs []runRecord
	if err := tx.NewSelect().Model(&runs).Where("workspace_id = ? AND id IN (?) AND state IN (?)", workspaceID, bun.List(ids), bun.List([]string{StateQueued, StateRunning, StateWaiting, StateApproval})).Scan(ctx); err != nil {
		return err
	}
	for _, run := range runs {
		if _, err := tx.NewUpdate().Model(&run).Set("state = ?, error = ?, revision = revision + 1, wake_at = NULL, updated_at = ?", StateCancelled, reason, time.Now().UTC()).WherePK().Exec(ctx); err != nil {
			return err
		}
		if step, ok := parkedApproval(decodeRunResults(run)); ok {
			run.State = StateCancelled
			if err := s.enqueueApprovalNotice(ctx, tx, run, step.StepID, "resolved"); err != nil {
				return err
			}
		}
	}
	return nil
}

// A reviewed message that can no longer be sent ends its run with that reason,
// so the reviewer sees why their approval did not produce a message.
func (s *Service) failReviewedMessageRun(ctx context.Context, actor workspaceaccess.ActorFacts, record runRecord, reason string) (Run, error) {
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(ctx, tx, record.WorkspaceID); err != nil {
			return err
		}
		updated, err := tx.NewUpdate().Model((*runRecord)(nil)).Set("state = ?, error = ?, wake_at = NULL, revision = revision + 1, updated_at = ?", StateFailed, reason, time.Now().UTC()).Where("id = ? AND workspace_id = ? AND revision = ? AND state = ?", record.ID, record.WorkspaceID, record.Revision, StateApproval).Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := updated.RowsAffected(); n != 1 {
			return ErrState
		}
		record.Revision++
		return s.enqueueApprovalNotice(ctx, tx, record, record.CurrentStepID, "resolved")
	})
	if err != nil {
		return Run{}, err
	}
	s.resolveApprovalNoticesAfterDecision(ctx, record)
	return s.GetRun(ctx, actor, record.WorkspaceID, record.ID)
}
