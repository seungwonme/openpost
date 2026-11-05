package messaging

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/inboxchanges"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/openpost/backend/internal/services/providerwrite"
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
	// A backstop across every workflow and comment campaign. It holds when two
	// accounts answer each other without recognizing each other's IDs.
	maxAutomatedRepliesPerDay = 20
	// A send whose job vanished must not hold a run in a five-second poll forever.
	automationDeliveryTimeout = time.Hour
	// How close in time an echo can be matched to an unconfirmed send by its text.
	automationEchoBound = 15 * time.Minute
	// A provider outbound message older than the reply window is not a takeover.
	automationTakeoverRecency = 24 * time.Hour

	stopReasonOptedOut  = "This person opted out of automatic messages."
	stopReasonTakeover  = "A person took over this conversation."
	stopReasonHuman     = "A person replied in this conversation, so automation stopped."
	stopReasonWindow    = "The Instagram messaging window closed before this message was sent."
	stopReasonDeleted   = "The person deleted the message that started this workflow."
	stopReasonLoop      = "This conversation reached the automatic reply limit for this hour."
	stopReasonDaily     = "This conversation reached the daily automatic message limit."
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
// package cycle. Every run for a stopped conversation is cancelled in the same
// transaction, including one in a wait step that holds no receipt yet, and the
// reason stays visible on the run.
func (s *Service) SetAutomationRunStopper(stop func(context.Context, bun.Tx, string, string, []string, string) error) {
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
	err := tx.NewSelect().Model(&reply).Where("trigger_message_id = ?", trigger.ID).Scan(ctx)
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
		reason, err := s.automationLimitReason(ctx, tx, conversation)
		if err != nil {
			return reply, err
		}
		if reason != "" {
			reply.State, reply.Reason = AutomationSkipped, reason
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
	check, err := s.automationStopCheck(ctx, tx, conversation, trigger)
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
		reason, err := s.automationLimitReason(ctx, tx, conversation)
		if err != nil {
			return err
		}
		if reason != "" {
			reply.State, reply.Reason = AutomationSkipped, reason
			return s.saveAutomationReply(ctx, tx, reply)
		}
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

// automationLimitReason counts every automated message in the conversation,
// including the comment campaign messages sent to the same person.
func (s *Service) automationLimitReason(ctx context.Context, db bun.IDB, conversation models.Conversation) (string, error) {
	now := s.now()
	delivering := bun.List([]string{AutomationSending, AutomationSent, AutomationUncertain})
	hourly, err := db.NewSelect().Model((*AutomationMessage)(nil)).Where("conversation_id = ? AND state IN (?) AND created_at >= ?", conversation.ID, delivering, now.Add(-time.Hour)).Count(ctx)
	if err != nil {
		return "", err
	}
	if hourly >= maxAutomatedRepliesPerHour {
		return stopReasonLoop, nil
	}
	daily, err := db.NewSelect().Model((*AutomationMessage)(nil)).Where("conversation_id = ? AND state IN (?) AND created_at >= ?", conversation.ID, delivering, now.Add(-24*time.Hour)).Count(ctx)
	if err != nil {
		return "", err
	}
	private, err := db.NewSelect().TableExpr("engagement_automation_replies AS e").Join("JOIN engagement_items AS i ON i.id = e.item_id").
		Where("e.channel = 'private' AND e.state IN ('sending', 'sent') AND e.created_at >= ?", now.Add(-24*time.Hour)).
		Where("i.social_account_id = ? AND i.author_remote_id = ?", conversation.SocialAccountID, conversation.CounterpartRemoteID).Count(ctx)
	if err != nil {
		return "", err
	}
	if daily+private >= maxAutomatedRepliesPerDay {
		return stopReasonDaily, nil
	}
	return "", nil
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
	human, err := s.humanRepliedAfter(ctx, db, conversation, account, trigger.RemoteCreatedAt)
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

// An outbound provider message that OpenPost automation does not own was written
// by a person, in Inbox or the Instagram app.
func (s *Service) humanRepliedAfter(ctx context.Context, db bun.IDB, conversation models.Conversation, account models.SocialAccount, after time.Time) (bool, error) {
	var outbound []models.DirectMessage
	if err := db.NewSelect().Model(&outbound).Where("conversation_id = ? AND direction = 'outbound' AND deleted_at IS NULL AND remote_created_at > ?", conversation.ID, after).Scan(ctx); err != nil {
		return false, err
	}
	for _, message := range outbound {
		owned, err := s.outboundOwnedByAutomation(ctx, db, conversation, account, message)
		if err != nil {
			return false, err
		}
		if !owned {
			return true, nil
		}
	}
	return false, nil
}

// outboundOwnedByAutomation prefers identity. Text alone only identifies an echo
// that can arrive before its send finalizes: an unconfirmed receipt, close in time.
// Anything broader would treat a person repeating a phrase as automation.
func (s *Service) outboundOwnedByAutomation(ctx context.Context, db bun.IDB, conversation models.Conversation, account models.SocialAccount, message models.DirectMessage) (bool, error) {
	at := automationMessageTime(message)
	var replies []AutomationMessage
	if err := db.NewSelect().Model(&replies).Where("conversation_id = ? AND (message_id = ? OR (state = ? AND body = ?))", conversation.ID, message.ID, AutomationSending, message.Body).Scan(ctx); err != nil {
		return false, err
	}
	for _, reply := range replies {
		if reply.MessageID == message.ID || withinEchoBound(reply.UpdatedAt, at) {
			return true, nil
		}
	}
	var private []struct {
		Body       string    `bun:"body"`
		UpdatedAt  time.Time `bun:"updated_at"`
		ExternalID string    `bun:"external_id"`
	}
	if err := db.NewRaw(`SELECT e.body, e.updated_at, COALESCE(p.external_id, '') AS external_id FROM engagement_automation_replies e JOIN engagement_items i ON i.id = e.item_id LEFT JOIN provider_write_attempts p ON e.job_id <> '' AND p.job_id = e.job_id AND p.status = 'accepted' WHERE e.channel = 'private' AND e.state IN ('sending', 'sent') AND i.social_account_id = ? AND i.author_remote_id = ?`, account.ID, conversation.CounterpartRemoteID).Scan(ctx, &private); err != nil {
		return false, err
	}
	for _, reply := range private {
		if reply.ExternalID != "" && reply.ExternalID == message.RemoteMessageID {
			return true, nil
		}
		if reply.ExternalID == "" && reply.Body == message.Body && withinEchoBound(reply.UpdatedAt, at) {
			return true, nil
		}
	}
	return false, nil
}

func withinEchoBound(a, b time.Time) bool {
	return a.Sub(b).Abs() <= automationEchoBound
}

func automationMessageTime(message models.DirectMessage) time.Time {
	if message.RemoteCreatedAt.IsZero() {
		return message.CreatedAt
	}
	return message.RemoteCreatedAt
}

// recordProviderOutbound applies at ingest, whether or not a run exists, so a
// person's reply in the Instagram app stops automation before any run is admitted.
// A message older than the provider window could not have been answering a current
// message, so a first import of old history is not a takeover.
func (s *Service) recordProviderOutbound(ctx context.Context, db bun.IDB, account models.SocialAccount, conversationID string, remote platform.ProviderMessage) error {
	if account.Platform != "instagram" || remote.Direction != "outbound" {
		return nil
	}
	if !remote.RemoteCreatedAt.IsZero() && s.now().Sub(remote.RemoteCreatedAt) > automationTakeoverRecency {
		return nil
	}
	tx, ok := db.(bun.Tx)
	if !ok {
		return errors.New("recording a takeover requires a transaction")
	}
	var conversation models.Conversation
	if err := tx.NewSelect().Model(&conversation).Where("id = ?", conversationID).Scan(ctx); err != nil {
		return err
	}
	var message models.DirectMessage
	if err := tx.NewSelect().Model(&message).Where("conversation_id = ? AND remote_message_id = ?", conversationID, remote.ID).Scan(ctx); err != nil {
		return err
	}
	owned, err := s.outboundOwnedByAutomation(ctx, tx, conversation, account, message)
	if err != nil || owned {
		return err
	}
	return s.stopConversationTx(ctx, tx, conversation, "provider", stopReasonHuman)
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
	// A job has no session. Live identity evaluation would deny every send in an
	// organization that requires SSO, so this checks current membership and role,
	// as the run's stored authority did when it claimed this receipt.
	decision, err := workspaceaccess.NewAuthorizer(db).AuthorizeStored(ctx, workspaceaccess.StoredAuthority{UserID: reply.UserID, WorkspaceID: reply.WorkspaceID}, workspaceaccess.LevelAdminister)
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
	check, err := s.automationStopCheck(ctx, db, conversation, trigger)
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
	// A job can crash after the provider accepted and before finalizing. Acceptance
	// is the outcome, whatever stopped automation since.
	var attempt models.ProviderWriteAttempt
	err = s.db.NewSelect().Model(&attempt).Where("operation_id = ? AND status = ?", "messaging:"+message.ID, providerwrite.StatusAccepted).Limit(1).Scan(ctx)
	if err == nil {
		return true, s.acceptSentMessage(ctx, message, conversation, attempt.ExternalID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
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
	if s.automationRunStopper != nil {
		if err := s.automationRunStopper(ctx, tx, conversation.WorkspaceID, conversation.ID, runs, reason); err != nil {
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
		query := tx.NewSelect().Model((*AutomationMessage)(nil)).Where("workspace_id = ? AND state IN (?)", authority.WorkspaceID, bun.List([]string{AutomationAwaitingReview, AutomationReady, AutomationSending}))
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

// A manual send through Inbox or the API is a person taking over the conversation.
func (s *Service) stopAutomationForManualSend(ctx context.Context, tx bun.Tx, conversation models.Conversation, actor Actor) error {
	return s.stopConversationTx(ctx, tx, conversation, actor.UserID, stopReasonTakeover)
}
