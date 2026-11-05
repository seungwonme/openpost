package messaging

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/inboxchanges"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/uptrace/bun"
)

const (
	AutomationAwaitingReview = "awaiting_review"
	AutomationReady          = "ready"
	AutomationSending        = "sending"
	AutomationSent           = "sent"
	AutomationFailed         = "failed"
	AutomationUncertain      = "uncertain"
	AutomationCancelled      = "cancelled"
	AutomationSkipped        = "skipped"

	// Instagram counts message text in UTF-8 bytes, after variables render.
	maxAutomatedMessageBytes = 1000
	// An automated reply can trigger another account's automation, which can
	// answer again. A person rarely needs more than this in one hour.
	maxAutomatedRepliesPerHour = 5
	// A send whose job vanished must not hold a run in a five-second poll forever.
	automationDeliveryTimeout = time.Hour

	stopReasonOptedOut  = "This person opted out of automatic messages."
	stopReasonTakeover  = "A person took over this conversation."
	stopReasonHuman     = "A person replied in this conversation, so automation stopped."
	stopReasonWindow    = "The Instagram messaging window closed before this message was sent."
	stopReasonDeleted   = "The person deleted the message that started this workflow."
	stopReasonLoop      = "This conversation reached the automatic reply limit for this hour."
	stopReasonOwned     = "Another workflow already answered this message."
	stopReasonUncertain = "The provider may have accepted this message. OpenPost did not send it again."
	stopReasonLost      = "The delivery receipt is unavailable. Inspect the conversation before sending again."
	stopReasonRun       = "The workflow run stopped before this message was sent."
)

// AutomationMessage is the receipt that owns one workflow-sent direct message.
type AutomationMessage struct {
	bun.BaseModel    `bun:"table:message_automation_replies"`
	ID               string    `bun:",pk" json:"id"`
	WorkspaceID      string    `json:"workspace_id"`
	ConversationID   string    `json:"conversation_id"`
	TriggerMessageID string    `json:"trigger_message_id"`
	WorkflowID       string    `json:"workflow_id"`
	RunID            string    `json:"run_id"`
	StepID           string    `json:"step_id"`
	UserID           string    `json:"-"`
	Body             string    `json:"body"`
	State            string    `json:"state"`
	Reason           string    `json:"reason,omitempty"`
	MessageID        string    `json:"message_id,omitempty"`
	SequencePosition int       `json:"sequence_position"`
	SequenceLength   int       `json:"sequence_length"`
	NotBefore        time.Time `bun:",nullzero" json:"not_before,omitzero"`
	CommentItemID    string    `json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// AutomationRequest advances one workflow step toward its owned delivery.
type AutomationRequest struct {
	Authority                                                   workspaceaccess.StoredAuthority
	WorkflowID, RunID, StepID, TriggerMessageID, Body, Delivery string
	Approve                                                     bool
}

type automationStop struct {
	bun.BaseModel  `bun:"table:message_automation_stops"`
	ConversationID string `bun:",pk"`
	WorkspaceID    string
	StoppedBy      string
	Reason         string
	StoppedAt      time.Time
}

// ConversationAutomation is the Inbox view of automation in one conversation.
type ConversationAutomation struct {
	StoppedAt  *time.Time          `json:"stopped_at,omitempty"`
	StopReason string              `json:"stop_reason,omitempty"`
	Replies    []AutomationMessage `json:"replies"`
}

// AutomationAccountRequirements reports why an account cannot run DM automation.
func (s *Service) CheckAutomationAccount(ctx context.Context, authority workspaceaccess.StoredAuthority, accountID string) error {
	decision, err := workspaceaccess.NewAuthorizer(s.db).AuthorizeStored(ctx, authority, workspaceaccess.LevelAdminister)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return ErrAccessDenied
	}
	var account models.SocialAccount
	if err := s.db.NewSelect().Model(&account).Where("id = ? AND workspace_id = ? AND is_active = ?", accountID, authority.WorkspaceID, true).Scan(ctx); err != nil {
		return errors.New("choose a connected Instagram account")
	}
	if reason := s.automationAccountIssue(ctx, s.db, account); reason != "" {
		return errors.New(reason)
	}
	return nil
}

// SetAutomationRunStopper connects takeover to the workflow owner without a
// package cycle. Runs holding receipts in a stopped conversation are cancelled
// in the same transaction, and the reason stays visible on the run.
func (s *Service) SetAutomationRunStopper(stop func(context.Context, bun.Tx, string, []string, string) error) {
	s.automationRunStopper = stop
}

// AdvanceAutomationReply converges a workflow step onto one owned delivery. It
// rechecks provider and person rules on every entry, so review and queue delay
// can never carry an earlier answer past a closed window, an opt-out or a takeover.
func (s *Service) AdvanceAutomationReply(ctx context.Context, in AutomationRequest) (AutomationMessage, error) {
	var reply AutomationMessage
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(ctx, tx, in.Authority.WorkspaceID); err != nil {
			return err
		}
		if err := s.authorizeAutomationRun(ctx, tx, in); err != nil {
			return err
		}
		trigger, conversation, err := s.loadAutomationTrigger(ctx, tx, in)
		if err != nil {
			return err
		}
		reply, err = s.claimAutomationReply(ctx, tx, trigger, conversation, in)
		if err != nil || reply.ID == "" {
			return err
		}
		return s.advanceClaimedReply(ctx, tx, trigger, conversation, in, &reply)
	})
	return reply, err
}

func (s *Service) authorizeAutomationRun(ctx context.Context, tx bun.Tx, in AutomationRequest) error {
	decision, err := workspaceaccess.NewAuthorizer(tx).AuthorizeStored(ctx, in.Authority, workspaceaccess.LevelAdminister)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return ErrAccessDenied
	}
	var run struct {
		State      string
		WorkflowID string
	}
	if err := tx.NewSelect().Table("workflow_runs").Column("state", "workflow_id").Where("id = ? AND workspace_id = ?", in.RunID, in.Authority.WorkspaceID).Scan(ctx, &run); err != nil {
		return errors.New("workflow run is unavailable")
	}
	if run.WorkflowID != in.WorkflowID || (run.State != "running" && (!in.Approve || run.State != "awaiting_approval")) {
		return errors.New("workflow run stopped before this message was sent")
	}
	return nil
}

func (s *Service) loadAutomationTrigger(ctx context.Context, tx bun.Tx, in AutomationRequest) (models.DirectMessage, models.Conversation, error) {
	var trigger models.DirectMessage
	if err := tx.NewSelect().Model(&trigger).Where("id = ? AND workspace_id = ?", in.TriggerMessageID, in.Authority.WorkspaceID).Scan(ctx); err != nil {
		return trigger, models.Conversation{}, ErrNotFound
	}
	if trigger.Direction != "inbound" {
		return trigger, models.Conversation{}, errors.New("this workflow requires a message from the other person")
	}
	var conversation models.Conversation
	if err := tx.NewSelect().Model(&conversation).Where("id = ? AND workspace_id = ?", trigger.ConversationID, in.Authority.WorkspaceID).Scan(ctx); err != nil {
		return trigger, conversation, ErrNotFound
	}
	if conversation.Platform != "instagram" {
		return trigger, conversation, errors.New("automatic messages support Instagram only")
	}
	return trigger, conversation, nil
}

func (s *Service) claimAutomationReply(ctx context.Context, tx bun.Tx, trigger models.DirectMessage, conversation models.Conversation, in AutomationRequest) (AutomationMessage, error) {
	var reply AutomationMessage
	err := tx.NewSelect().Model(&reply).Where("trigger_message_id = ? AND sequence_position = 0", trigger.ID).Scan(ctx)
	if err == nil {
		if reply.RunID != in.RunID || reply.StepID != in.StepID {
			// Answered by another owner. Report it without touching that receipt.
			return AutomationMessage{TriggerMessageID: trigger.ID, ConversationID: conversation.ID, State: AutomationSkipped, Reason: stopReasonOwned}, nil
		}
		return reply, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return reply, err
	}
	now := s.now()
	reply = AutomationMessage{ID: uuid.NewString(), WorkspaceID: conversation.WorkspaceID, ConversationID: conversation.ID, TriggerMessageID: trigger.ID, WorkflowID: in.WorkflowID, RunID: in.RunID, StepID: in.StepID, UserID: in.Authority.UserID, Body: strings.TrimSpace(in.Body), CreatedAt: now, UpdatedAt: now}
	reply.State = AutomationReady
	if in.Delivery == "review" {
		reply.State = AutomationAwaitingReview
	} else if issue := automatedBodyIssue(reply.Body); issue != "" {
		reply.State, reply.Reason = AutomationSkipped, issue
	}
	if reply.State != AutomationSkipped {
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

func (s *Service) advanceClaimedReply(ctx context.Context, tx bun.Tx, trigger models.DirectMessage, conversation models.Conversation, in AutomationRequest, reply *AutomationMessage) error {
	if automationSettled(reply.State) {
		return nil
	}
	if reply.State == AutomationSending {
		return s.reconcileAutomationDelivery(ctx, tx, reply)
	}
	check, err := s.automationReplyStopCheck(ctx, tx, conversation, trigger, *reply)
	if err != nil {
		return err
	}
	if check.reason != "" {
		reply.State, reply.Reason = AutomationCancelled, check.reason
		if err := s.saveAutomationReply(ctx, tx, reply); err != nil {
			return err
		}
		if check.human {
			return s.stopConversationTx(ctx, tx, conversation, "provider", check.reason)
		}
		return nil
	}
	if reply.State == AutomationScheduled {
		if s.now().Before(reply.NotBefore) {
			return nil
		}
		reply.State = AutomationReady
		if in.Delivery == "review" {
			reply.State = AutomationAwaitingReview
			return s.saveAutomationReply(ctx, tx, reply)
		}
	}
	return s.deliverClaimedReply(ctx, tx, conversation, in, reply)
}

func (s *Service) deliverClaimedReply(ctx context.Context, tx bun.Tx, conversation models.Conversation, in AutomationRequest, reply *AutomationMessage) error {
	body := strings.TrimSpace(in.Body)
	switch {
	case reply.State == AutomationAwaitingReview && in.Approve:
		if issue := automatedBodyIssue(body); issue != "" {
			return errors.New(issue)
		}
		reply.Body, reply.State = body, AutomationReady
		return s.saveAutomationReply(ctx, tx, reply)
	case reply.State == AutomationReady && in.Approve:
		if reply.Body != body {
			return ErrOperationConflict
		}
	case reply.State == AutomationReady:
		message, err := s.queueMessageWithDB(ctx, tx, conversation.ID, reply.Body)
		if err != nil {
			return err
		}
		reply.MessageID, reply.State = message.ID, AutomationSending
		return s.saveAutomationReply(ctx, tx, reply)
	}
	return nil
}

// Hooks in the send path record provider truth first. This covers a job that
// finished its write but crashed, or one that never ran.
func (s *Service) reconcileAutomationDelivery(ctx context.Context, tx bun.Tx, reply *AutomationMessage) error {
	var message models.DirectMessage
	err := tx.NewSelect().Model(&message).Where("id = ?", reply.MessageID).Scan(ctx)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		reply.State, reply.Reason = AutomationUncertain, stopReasonLost
	case err != nil:
		return err
	case message.SendStatus == "sent":
		reply.State, reply.Reason = AutomationSent, ""
	case message.SendStatus == "failed":
		reply.State, reply.Reason = AutomationFailed, firstNonEmpty(message.ErrorMessage, "The provider rejected this message.")
	case message.SendStatus == "ambiguous":
		reply.State, reply.Reason = AutomationUncertain, stopReasonUncertain
	case s.now().Sub(reply.UpdatedAt) > automationDeliveryTimeout:
		reply.State, reply.Reason = AutomationUncertain, stopReasonLost
	default:
		return nil
	}
	return s.saveAutomationReply(ctx, tx, reply)
}

func (s *Service) saveAutomationReply(ctx context.Context, db bun.IDB, reply *AutomationMessage) error {
	reply.UpdatedAt = s.now()
	if _, err := db.NewUpdate().Model(reply).WherePK().Exec(ctx); err != nil {
		return err
	}
	return inboxchanges.Append(ctx, db, reply.WorkspaceID, inboxchanges.Change{Kind: "conversation", ConversationID: reply.ConversationID})
}

func automationSettled(state string) bool {
	switch state {
	case AutomationSent, AutomationFailed, AutomationUncertain, AutomationCancelled, AutomationSkipped:
		return true
	}
	return false
}

func automatedBodyIssue(body string) string {
	if strings.TrimSpace(body) == "" {
		return "Enter the message text before sending."
	}
	if len(strings.TrimSpace(body)) > maxAutomatedMessageBytes {
		return fmt.Sprintf("Instagram messages support up to %d bytes. Shorten the text.", maxAutomatedMessageBytes)
	}
	return ""
}

func (s *Service) automationLooping(ctx context.Context, db bun.IDB, conversationID string) (bool, error) {
	count, err := db.NewSelect().Model((*AutomationMessage)(nil)).Where("conversation_id = ? AND sequence_position = 0 AND state IN (?) AND created_at >= ?", conversationID, bun.List([]string{AutomationSending, AutomationSent, AutomationUncertain}), s.now().Add(-time.Hour)).Count(ctx)
	return count >= maxAutomatedRepliesPerHour, err
}

type automationStopResult struct {
	reason string
	human  bool
}

func (s *Service) automationAccountIssue(ctx context.Context, db bun.IDB, account models.SocialAccount) string {
	if account.Platform != "instagram" {
		return "Automatic messages support Instagram only."
	}
	if !s.isMessagingEnabledWithDB(ctx, db, account.ID) {
		return "Enable messaging for this account before automating messages."
	}
	provider := s.provider(account)
	if provider == nil || !provider.MessagingSupport().CanSend {
		return "Sending messages is unavailable for this account."
	}
	if len(platform.MissingAnalyticsScopes(account.GrantedScopes, provider.MessagingSupport().RequiredScopes)) > 0 {
		return "Reconnect this account and grant messaging access."
	}
	return ""
}

// automationStopCheck holds every rule a send must still satisfy at this moment.
// It is read-only so the fence guard can call it inside its transaction.
func (s *Service) automationStopCheck(ctx context.Context, db bun.IDB, conversation models.Conversation, trigger models.DirectMessage) (automationStopResult, error) {
	var account models.SocialAccount
	err := db.NewSelect().Model(&account).Where("id = ? AND workspace_id = ? AND is_active = ?", conversation.SocialAccountID, conversation.WorkspaceID, true).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return automationStopResult{reason: "The Instagram account is no longer connected."}, nil
	}
	if err != nil {
		return automationStopResult{}, err
	}
	if issue := s.automationAccountIssue(ctx, db, account); issue != "" {
		return automationStopResult{reason: issue}, nil
	}
	if conversation.CounterpartRemoteID == "" || conversation.CounterpartRemoteID == account.AccountID {
		return automationStopResult{reason: "The other person's identity is unavailable from Instagram."}, nil
	}
	if !trigger.DeletedAt.IsZero() {
		return automationStopResult{reason: stopReasonDeleted}, nil
	}
	optedOut, err := db.NewSelect().Table("engagement_automation_optouts").Where("social_account_id = ? AND author_remote_id = ?", account.ID, conversation.CounterpartRemoteID).Exists(ctx)
	if err != nil {
		return automationStopResult{}, err
	}
	if optedOut {
		return automationStopResult{reason: stopReasonOptedOut}, nil
	}
	stopped, err := db.NewSelect().Model((*automationStop)(nil)).Where("conversation_id = ?", conversation.ID).Exists(ctx)
	if err != nil {
		return automationStopResult{}, err
	}
	if stopped {
		return automationStopResult{reason: stopReasonTakeover}, nil
	}
	human, err := humanRepliedAfter(ctx, db, conversation, account, trigger.RemoteCreatedAt)
	if err != nil {
		return automationStopResult{}, err
	}
	if human {
		return automationStopResult{reason: stopReasonHuman, human: true}, nil
	}
	if conversation.MessagingWindowExpiresAt.IsZero() || !conversation.MessagingWindowExpiresAt.After(s.now()) {
		return automationStopResult{reason: stopReasonWindow}, nil
	}
	return automationStopResult{}, nil
}

// automationReplyStopCheck adds what one receipt must still satisfy: a follow-up
// also needs its comment to stay automated and its response to stay within 24 hours.
func (s *Service) automationReplyStopCheck(ctx context.Context, db bun.IDB, conversation models.Conversation, trigger models.DirectMessage, reply AutomationMessage) (automationStopResult, error) {
	if reply.CommentItemID != "" {
		reason, err := commentStopReason(ctx, db, reply.CommentItemID)
		if err != nil || reason != "" {
			return automationStopResult{reason: reason}, err
		}
	}
	check, err := s.automationStopCheck(ctx, db, conversation, trigger)
	if err != nil || check.reason != "" {
		return check, err
	}
	if reply.SequenceLength > 0 && !s.now().Before(automationMessageTime(trigger).Add(sequenceResponseWindow)) {
		return automationStopResult{reason: stopReasonWindow}, nil
	}
	return check, nil
}

// An outbound provider message that OpenPost automation does not own was written
// by a person, in Inbox or the Instagram app. An echo can be stored before its
// send finalizes, so its body also identifies automation's own writes.
func humanRepliedAfter(ctx context.Context, db bun.IDB, conversation models.Conversation, account models.SocialAccount, after time.Time) (bool, error) {
	return db.NewSelect().TableExpr("direct_messages AS d").
		Where("d.conversation_id = ? AND d.direction = 'outbound' AND d.deleted_at IS NULL AND d.remote_created_at > ?", conversation.ID, after).
		Where("NOT EXISTS (SELECT 1 FROM message_automation_replies a WHERE a.conversation_id = d.conversation_id AND a.message_id <> '' AND (a.message_id = d.id OR a.body = d.body))").
		Where("NOT EXISTS (SELECT 1 FROM engagement_automation_replies e JOIN engagement_items i ON i.id = e.item_id WHERE e.channel = 'private' AND e.state IN ('sending','sent') AND e.body = d.body AND i.social_account_id = ? AND i.author_remote_id = ?)", account.ID, conversation.CounterpartRemoteID).
		Exists(ctx)
}

// automationSendStopped is the check before a queued automated message reaches
// the provider. Acceptance is the cancellation boundary; this is the last gate.
type automationSendStopped struct {
	reason string
	human  bool
}

func (e *automationSendStopped) Error() string { return e.reason }

func (s *Service) automationOwnerOfMessage(ctx context.Context, db bun.IDB, messageID string) (AutomationMessage, bool, error) {
	var reply AutomationMessage
	err := db.NewSelect().Model(&reply).Where("message_id = ?", messageID).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return reply, false, nil
	}
	return reply, err == nil, err
}

func (s *Service) checkAutomatedSend(ctx context.Context, db bun.IDB, message models.DirectMessage) error {
	reply, owned, err := s.automationOwnerOfMessage(ctx, db, message.ID)
	if err != nil || !owned {
		return err
	}
	if err := s.checkAutomatedSendOwner(ctx, db, reply); err != nil {
		return err
	}
	decision, err := workspaceaccess.NewAuthorizer(db).Authorize(ctx, reply.WorkspaceID, Actor{UserID: reply.UserID}, workspaceaccess.LevelAdminister)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return &automationSendStopped{reason: "The person who activated this workflow no longer has access."}
	}
	var conversation models.Conversation
	if err := db.NewSelect().Model(&conversation).Where("id = ?", reply.ConversationID).Scan(ctx); err != nil {
		return err
	}
	var trigger models.DirectMessage
	if err := db.NewSelect().Model(&trigger).Where("id = ?", reply.TriggerMessageID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &automationSendStopped{reason: stopReasonDeleted}
		}
		return err
	}
	check, err := s.automationReplyStopCheck(ctx, db, conversation, trigger, reply)
	if err != nil {
		return err
	}
	if check.reason != "" {
		return &automationSendStopped{reason: check.reason, human: check.human}
	}
	return nil
}

// A failed receipt can only reach the send path through an explicit Inbox retry,
// which is a person's decision and does not depend on the finished run.
func (s *Service) checkAutomatedSendOwner(ctx context.Context, db bun.IDB, reply AutomationMessage) error {
	if reply.State == AutomationFailed {
		return nil
	}
	if reply.State != AutomationSending {
		return &automationSendStopped{reason: firstNonEmpty(reply.Reason, stopReasonRun)}
	}
	var state string
	err := db.NewSelect().Table("workflow_runs").Column("state").Where("id = ? AND workspace_id = ?", reply.RunID, reply.WorkspaceID).Scan(ctx, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return &automationSendStopped{reason: stopReasonRun}
	}
	if err != nil {
		return err
	}
	if state == "cancelled" || state == "failed" || state == "succeeded" {
		return &automationSendStopped{reason: stopReasonRun}
	}
	return nil
}

// sendAutomationHold decides whether the job may reach the provider. A replayed
// job never repeats an automated write: only a queued message is still unsent.
func (s *Service) sendAutomationHold(ctx context.Context, message models.DirectMessage, conversation models.Conversation) (bool, error) {
	_, owned, err := s.automationOwnerOfMessage(ctx, s.db, message.ID)
	if err != nil || !owned {
		return false, err
	}
	if message.SendStatus != "queued" {
		return true, nil
	}
	err = s.checkAutomatedSend(ctx, s.db, message)
	var stopped *automationSendStopped
	if errors.As(err, &stopped) {
		return true, s.stopAutomatedMessage(ctx, message, conversation, stopped)
	}
	return false, err
}

func (s *Service) stopAutomatedMessage(ctx context.Context, message models.DirectMessage, conversation models.Conversation, stopped *automationSendStopped) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(ctx, tx, message.WorkspaceID); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model(&message).Set("send_status = 'failed'").Set("error_message = ?", stopped.reason).Set("updated_at = ?", s.now()).WherePK().Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*AutomationMessage)(nil)).Set("state = ?, reason = ?, updated_at = ?", AutomationCancelled, stopped.reason, s.now()).Where("message_id = ? AND state <> ?", message.ID, AutomationSent).Exec(ctx); err != nil {
			return err
		}
		if stopped.human {
			if err := s.stopConversationTx(ctx, tx, conversation, "provider", stopped.reason); err != nil {
				return err
			}
		}
		return inboxchanges.Append(ctx, tx, message.WorkspaceID, inboxchanges.Change{Kind: "message", ConversationID: conversation.ID, MessageID: message.ID})
	})
}

// Provider acceptance is an outcome even when a later action cancelled the run.
func (s *Service) recordAutomationAcceptance(ctx context.Context, db bun.IDB, messageID string) error {
	_, err := db.NewUpdate().Model((*AutomationMessage)(nil)).Set("state = ?, reason = '', updated_at = ?", AutomationSent, s.now()).Where("message_id = ?", messageID).Exec(ctx)
	return err
}

func (s *Service) recordAutomationFailure(ctx context.Context, db bun.IDB, messageID, status, errorMessage string) error {
	state := AutomationFailed
	if status == "ambiguous" {
		state = AutomationUncertain
	}
	_, err := db.NewUpdate().Model((*AutomationMessage)(nil)).Set("state = ?, reason = ?, updated_at = ?", state, errorMessage, s.now()).Where("message_id = ? AND state IN (?)", messageID, bun.List([]string{AutomationSending, AutomationFailed})).Exec(ctx)
	return err
}

// stopConversationTx records takeover and stops every unsettled reply there.
func (s *Service) stopConversationTx(ctx context.Context, tx bun.Tx, conversation models.Conversation, stoppedBy, reason string) error {
	stop := automationStop{ConversationID: conversation.ID, WorkspaceID: conversation.WorkspaceID, StoppedBy: stoppedBy, Reason: reason, StoppedAt: s.now()}
	if _, err := tx.NewInsert().Model(&stop).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
		return err
	}
	return s.cancelConversationRepliesTx(ctx, tx, conversation, reason)
}

func (s *Service) cancelConversationRepliesTx(ctx context.Context, tx bun.Tx, conversation models.Conversation, reason string) error {
	var replies []AutomationMessage
	if err := tx.NewSelect().Model(&replies).Where("conversation_id = ?", conversation.ID).Scan(ctx); err != nil {
		return err
	}
	runs := make([]string, 0, len(replies))
	for i := range replies {
		runs = append(runs, replies[i].RunID)
		if automationSettled(replies[i].State) {
			continue
		}
		replies[i].State, replies[i].Reason = AutomationCancelled, reason
		if err := s.saveAutomationReply(ctx, tx, &replies[i]); err != nil {
			return err
		}
	}
	if len(runs) > 0 && s.automationRunStopper != nil {
		if err := s.automationRunStopper(ctx, tx, conversation.WorkspaceID, runs, reason); err != nil {
			return err
		}
	}
	return inboxchanges.Append(ctx, tx, conversation.WorkspaceID, inboxchanges.Change{Kind: "conversation", ConversationID: conversation.ID})
}

// StopConversationAutomation is the explicit Inbox takeover.
func (s *Service) StopConversationAutomation(ctx context.Context, actor Actor, workspaceID, conversationID string) error {
	if err := s.authorize(ctx, workspaceID, actor, workspaceaccess.LevelEdit); err != nil {
		return err
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		var conversation models.Conversation
		if err := tx.NewSelect().Model(&conversation).Where("id = ? AND workspace_id = ?", conversationID, workspaceID).Scan(ctx); err != nil {
			return ErrNotFound
		}
		return s.stopConversationTx(ctx, tx, conversation, actor.UserID, stopReasonTakeover)
	})
}

// ResumeConversationAutomation allows future messages to start automation again.
// Stopped runs stay stopped, and an outstanding opt-out remains in force.
func (s *Service) ResumeConversationAutomation(ctx context.Context, actor Actor, workspaceID, conversationID string) error {
	if err := s.authorize(ctx, workspaceID, actor, workspaceaccess.LevelEdit); err != nil {
		return err
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*automationStop)(nil)).Where("conversation_id = ? AND workspace_id = ?", conversationID, workspaceID).Exec(ctx); err != nil {
			return err
		}
		return inboxchanges.Append(ctx, tx, workspaceID, inboxchanges.Change{Kind: "conversation", ConversationID: conversationID})
	})
}

// ConversationAutomationState reads stored receipts. It never calls a provider.
func (s *Service) ConversationAutomationState(ctx context.Context, actor Actor, workspaceID, conversationID string) (ConversationAutomation, error) {
	result := ConversationAutomation{Replies: []AutomationMessage{}}
	if err := s.authorize(ctx, workspaceID, actor, workspaceaccess.LevelRead); err != nil {
		return result, err
	}
	var conversation models.Conversation
	if err := s.db.NewSelect().Model(&conversation).Where("id = ? AND workspace_id = ?", conversationID, workspaceID).Scan(ctx); err != nil {
		return result, ErrNotFound
	}
	if err := s.db.NewSelect().Model(&result.Replies).Where("conversation_id = ?", conversation.ID).Order("created_at DESC", "id DESC").Limit(50).Scan(ctx); err != nil {
		return result, err
	}
	var stop automationStop
	err := s.db.NewSelect().Model(&stop).Where("conversation_id = ?", conversation.ID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	stoppedAt := stop.StoppedAt
	result.StoppedAt, result.StopReason = &stoppedAt, stop.Reason
	return result, nil
}

// StopWorkflowMessages stops unsettled replies when a run or campaign stops.
func (s *Service) StopWorkflowMessages(ctx context.Context, authority workspaceaccess.StoredAuthority, workflowID, runID string) error {
	decision, err := workspaceaccess.NewAuthorizer(s.db).AuthorizeStored(ctx, authority, workspaceaccess.LevelAdminister)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return ErrAccessDenied
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(ctx, tx, authority.WorkspaceID); err != nil {
			return err
		}
		query := tx.NewSelect().Model((*AutomationMessage)(nil)).Where("workspace_id = ? AND state IN (?)", authority.WorkspaceID, bun.List([]string{AutomationScheduled, AutomationAwaitingReview, AutomationReady, AutomationSending}))
		if workflowID != "" {
			query = query.Where("workflow_id = ?", workflowID)
		} else {
			query = query.Where("run_id = ?", runID)
		}
		var replies []AutomationMessage
		if err := query.Scan(ctx, &replies); err != nil {
			return err
		}
		for i := range replies {
			replies[i].State, replies[i].Reason = AutomationCancelled, stopReasonRun
			if err := s.saveAutomationReply(ctx, tx, &replies[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// The same whole-message rule as comments, so "stop by the shop" stays a question.
// Korean equivalents and surrounding punctuation are accepted because a person
// who sends "그만!" expects the same result as "STOP".
var automationOptOutPhrases = map[string]bool{
	"stop": true, "unsubscribe": true, "stop replying": true,
	"그만": true, "그만해": true, "그만해요": true, "그만해주세요": true,
	"그만 보내": true, "그만 보내세요": true, "그만 보내주세요": true,
	"수신거부": true, "수신 거부": true,
}

func messageRequestsOptOut(body string) bool {
	fields := strings.Fields(strings.ToLower(body))
	normalized := strings.TrimFunc(strings.Join(fields, " "), func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsSpace(r)
	})
	return automationOptOutPhrases[normalized]
}

// recordMessageOptOut commits with the message that requested it, so no job can
// observe the message without the opt-out. It uses the table comments use.
func (s *Service) recordMessageOptOut(ctx context.Context, db bun.IDB, account models.SocialAccount, conversationID, counterpart string, message platform.ProviderMessage) error {
	if account.Platform != "instagram" || message.Direction != "inbound" || !messageRequestsOptOut(message.Body) {
		return nil
	}
	author := firstNonEmpty(message.AuthorRemoteID, counterpart)
	if author == "" || author == account.AccountID {
		return nil
	}
	if _, err := db.NewRaw("INSERT INTO engagement_automation_optouts(social_account_id,author_remote_id,created_at) VALUES(?,?,?) ON CONFLICT DO NOTHING", account.ID, author, s.now()).Exec(ctx); err != nil {
		return err
	}
	tx, ok := db.(bun.Tx)
	if !ok {
		return nil
	}
	var conversation models.Conversation
	if err := tx.NewSelect().Model(&conversation).Where("id = ?", conversationID).Scan(ctx); err != nil {
		return err
	}
	return s.cancelConversationRepliesTx(ctx, tx, conversation, stopReasonOptedOut)
}

// A manual send through Inbox or the API is a person taking over the conversation.
func (s *Service) stopAutomationForManualSend(ctx context.Context, tx bun.Tx, conversation models.Conversation, actor Actor) error {
	return s.stopConversationTx(ctx, tx, conversation, actor.UserID, stopReasonTakeover)
}
