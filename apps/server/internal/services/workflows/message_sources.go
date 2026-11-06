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

// One person cannot use up a workflow's daily limit for everyone else, or keep paid
// steps running. Every run counts toward this, whatever it went on to do.
const maxMessageRunsPerSender = 5

const (
	stopReasonSenderLimit = "This person reached this workflow's daily limit, so this message did not start it."
	messageDailyLimitNote = "This workflow reached its daily run limit. Later messages wait until tomorrow."
)

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
	TakenOver         bool      `bun:"taken_over"`
}

func messageSourceQuery(db bun.IDB, workspaceID string) *bun.SelectQuery {
	return db.NewSelect().TableExpr("direct_messages AS d").
		ColumnExpr("d.id, d.conversation_id, c.social_account_id, d.body, d.remote_created_at, d.created_at, c.counterpart_name, c.counterpart_handle, c.messaging_window_expires_at AS window_expires_at, EXISTS (SELECT 1 FROM message_automation_stops s WHERE s.conversation_id = d.conversation_id AND (s.lapses_at IS NULL OR s.lapses_at > ?)) AS taken_over", time.Now().UTC()).
		Join("JOIN conversations AS c ON c.id = d.conversation_id").
		Join("JOIN social_accounts AS a ON a.id = c.social_account_id").
		Where("d.workspace_id = ? AND c.platform = 'instagram' AND d.direction = 'inbound' AND d.deleted_at IS NULL AND d.remote_message_id <> ''", workspaceID).
		// A connected account's own message is never a person's request, even when
		// it reaches another connected account and arrives as inbound.
		Where("c.counterpart_remote_id <> '' AND c.counterpart_remote_id <> a.account_id AND d.author_remote_id <> a.account_id").
		Where("NOT EXISTS (SELECT 1 FROM social_accounts own WHERE own.workspace_id = d.workspace_id AND own.platform = 'instagram' AND own.account_id = c.counterpart_remote_id)").
		Where("NOT EXISTS (SELECT 1 FROM engagement_automation_optouts o WHERE o.social_account_id = c.social_account_id AND o.author_remote_id = c.counterpart_remote_id)").
		Where("NOT EXISTS (SELECT 1 FROM message_history_imports h WHERE h.message_id = d.id)")
}

// Read canonical Inbox inventory. Both the provider creation time and the first
// local storage must follow activation, which excludes anything older. A recent
// message first stored from a history page passes those times, so the source
// query excludes it by its import record.
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
		// A closed window or a taken-over conversation cannot be answered, so neither
		// spends the daily limit. Recording them means Resume applies only to later messages.
		excluded := !messageSourceMatches(source, row.Body) || !row.WindowExpiresAt.After(now) || row.TakenOver
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
	if row.TakenOver {
		return false, nil
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
// opt-out. It stops every run for that conversation, including one in a wait step
// with no receipt yet, so nothing sends after a later resume. The reason stays on
// the run for inspection.
func (s *Service) CancelMessageAutomationRuns(ctx context.Context, tx bun.Tx, workspaceID, conversationID string, ids []string, reason string) error {
	var runs []runRecord
	query := tx.NewSelect().Model(&runs).Where("workspace_id = ? AND state IN (?)", workspaceID, bun.List([]string{StateQueued, StateRunning, StateWaiting, StateApproval})).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			q = q.Where("source_json LIKE ?", conversationSourcePattern(conversationID))
			if len(ids) > 0 {
				q = q.WhereOr("id IN (?)", bun.List(ids))
			}
			// A comment campaign waiting for this person has no source conversation yet.
			return q.WhereOr("id IN (SELECT w.run_id FROM message_sequence_waits w JOIN conversations c ON c.social_account_id = w.account_id AND c.counterpart_remote_id = w.author_remote_id WHERE c.id = ?)", conversationID)
		})
	if err := query.Scan(ctx); err != nil {
		return err
	}
	for _, run := range runs {
		if err := s.cancelMessageRun(ctx, tx, run, reason); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) cancelMessageRun(ctx context.Context, tx bun.Tx, run runRecord, reason string) error {
	if _, err := tx.NewUpdate().Model(&run).Set("state = ?, error = ?, revision = revision + 1, wake_at = NULL, updated_at = ?", StateCancelled, reason, time.Now().UTC()).WherePK().Exec(ctx); err != nil {
		return err
	}
	if step, ok := parkedApproval(decodeRunResults(run)); ok {
		run.State = StateCancelled
		return s.enqueueApprovalNotice(ctx, tx, run, step.StepID, "resolved")
	}
	return nil
}

func conversationSourcePattern(conversationID string) string {
	return `%"conversation_id":"` + conversationID + `"%`
}

// countMessageRuns leaves out runs that sent nothing, so a stopped or skipped
// message does not spend a workflow's daily limit. A workflow with a paid step spent
// money whatever the run went on to do, so all of its runs count.
func countMessageRuns(query *bun.SelectQuery, def Definition) *bun.SelectQuery {
	query = query.Where("error <> ?", stopReasonSenderLimit)
	if paidSteps(def.Steps) {
		return query
	}
	return query.Where("state NOT IN (?)", bun.List([]string{StateFailed, StateCancelled})).Where(`results_json NOT LIKE '%"status":"skipped"%'`)
}

func paidSteps(steps []Step) bool {
	for _, step := range steps {
		if step.Kind == KindAIText || step.Kind == KindAIDecision || paidSteps(step.Then) || paidSteps(step.Else) {
			return true
		}
	}
	return false
}

func messageSenderLimitReached(ctx context.Context, db bun.IDB, record workflowRecord, conversationID string, now time.Time) (bool, error) {
	count, err := db.NewSelect().Model((*runRecord)(nil)).Where("workflow_id = ? AND mode = ? AND created_at >= ?", record.ID, ModeLive, now.Truncate(24*time.Hour)).Where("source_json LIKE ?", conversationSourcePattern(conversationID)).Where("error <> ?", stopReasonSenderLimit).Count(ctx)
	return count >= maxMessageRunsPerSender, err
}

// admitMessageEvent runs before any step, so the limit stops paid steps too. A
// message over the limit becomes a stopped run with its reason, which is how
// someone looking at the workflow learns it was not answered.
func (s *Service) admitMessageEvent(ctx context.Context, tx bun.Tx, record workflowRecord, def Definition, authority workspaceaccess.StoredAuthority, item SourceItem, now time.Time) (bool, error) {
	wins, err := s.messageWorkflowWins(ctx, tx, record, item.ID)
	if err != nil || !wins {
		return false, err
	}
	limited, err := messageSenderLimitReached(ctx, tx, record, item.ConversationID, now)
	if err != nil || !limited {
		return !limited, err
	}
	source, err := sourceItemValues(item)
	if err != nil {
		return false, err
	}
	run, err := s.createRun(ctx, tx, record, def, authority, ModeLive, source)
	if err != nil {
		return false, err
	}
	_, err = tx.NewUpdate().Model(&run).Set("state = ?, error = ?, revision = revision + 1, wake_at = NULL, updated_at = ?", StateCancelled, stopReasonSenderLimit, now).WherePK().Exec(ctx)
	return false, err
}

// noteMessageDailyLimit shows why later messages are waiting. The poll clears
// source_error first, so this lasts while the limit does.
func noteMessageDailyLimit(ctx context.Context, tx bun.Tx, record workflowRecord, def Definition) error {
	if def.Source.Kind != "message_received" {
		return nil
	}
	_, err := tx.NewUpdate().Model((*workflowRecord)(nil)).Set("source_error = ?", messageDailyLimitNote).Where("id = ?", record.ID).Exec(ctx)
	return err
}

// messageRunStoppedDuringReview reports a run that a person's reply or an opt-out
// cancelled while it waited for approval. Approving it finds that outcome, with its
// reason, rather than a conflict the reviewer cannot act on.
func messageRunStoppedDuringReview(record runRecord) (Run, bool) {
	if record.State != StateCancelled {
		return Run{}, false
	}
	run, err := decodeRun(record)
	if err != nil {
		return Run{}, false
	}
	for _, step := range run.Steps {
		if step.StepID == record.CurrentStepID && isMessageAction(step.Kind) {
			return run, true
		}
	}
	return Run{}, false
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
			// A person's reply detected during this approval already cancelled the
			// run with its reason. That is the outcome, not a conflict.
			var state string
			if err := tx.NewSelect().Model((*runRecord)(nil)).Column("state").Where("id = ?", record.ID).Scan(ctx, &state); err != nil {
				return err
			}
			if state != StateCancelled {
				return ErrState
			}
			return nil
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
