package messaging

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/uptrace/bun"
)

const (
	FollowCheckFollowing    = "following"
	FollowCheckNotFollowing = "not_following"
	FollowCheckFailed       = "failed"

	// SequenceNotFollowing is an ordinary outcome: the person replied every time
	// but never followed, so the link was not sent.
	SequenceNotFollowing = "not_following"

	// DefaultFollowChecks bounds how often one person is asked to follow.
	DefaultFollowChecks = 3

	// linkButtonAttachment marks the button an outbound message is sent with.
	linkButtonAttachment = "link_button"
	// quickReplyAttachment marks the quick reply an outbound message offers.
	quickReplyAttachment = "quick_reply"

	stopReasonNoFollowReply = "The person did not reply after being asked to follow, so the link was not sent."
	followReadFailed        = "OpenPost could not read whether this person follows the account, so the link was not sent"
	followReadHint          = "Instagram shares this only after the person messages the account, and not after they block it."
)

// FollowGate holds a comment campaign's first follow-up, which carries the link
// button, until the person who responded follows the account. Each response is
// checked once; a person who does not follow gets NotFollowingText and can reply
// again, up to MaxChecks checks.
type FollowGate struct {
	ButtonTitle, ButtonURL, NotFollowingText string
	MaxChecks                                int
}

func (g FollowGate) button() *platform.MessageButton {
	return &platform.MessageButton{Title: strings.TrimSpace(g.ButtonTitle), URL: strings.TrimSpace(g.ButtonURL)}
}

func (g FollowGate) checks() int {
	if g.MaxChecks < 1 {
		return DefaultFollowChecks
	}
	return g.MaxChecks
}

// followCheck is one profile read for one response. A recorded check is final,
// so a replayed run never reads again or reaches a different answer.
type followCheck struct {
	bun.BaseModel     `bun:"table:message_follow_checks"`
	RunID             string `bun:",pk"`
	StepID            string `bun:",pk"`
	Attempt           int    `bun:",pk"`
	ResponseMessageID string
	Outcome           string
	Reason            string
	CheckedAt         time.Time
}

// pendingFollowCheck is a response that still needs its profile read, which
// runs outside any transaction.
type pendingFollowCheck struct {
	attempt     int
	response    string
	account     models.SocialAccount
	counterpart string
}

func stopReasonNotFollowing(checks int) string {
	if checks == 1 {
		return "The person did not follow the account when they replied, so the link was not sent."
	}
	return fmt.Sprintf("The person still did not follow the account after %d replies, so the link was not sent.", checks)
}

func waitingReasonFollow(attempt, checks int) string {
	return fmt.Sprintf("Asked the person to follow the account. Waiting for them to reply again (check %d of %d).", attempt+1, checks)
}

// followCheckStepID identifies the request to follow that answers one response,
// so each response keeps its own receipt beside the sequence's positions.
func followCheckStepID(stepID string, attempt int) string {
	return fmt.Sprintf("%s#follow-check-%d", stepID, attempt)
}

// advanceFollowGate decides whether the sequence may start. passed reports the
// response the link answers, or an empty response when the sequence already
// started. A provider read never runs inside a transaction.
func (s *Service) advanceFollowGate(ctx context.Context, in SequenceRequest) (SequenceProgress, bool, string, error) {
	if in.Comment == nil {
		return SequenceProgress{}, false, "", errors.New("a follow check needs a comment campaign")
	}
	// A recorded check makes the second pass decide without reading again.
	for range 2 {
		var progress SequenceProgress
		var passed bool
		var response string
		var pending *pendingFollowCheck
		err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			if err := organizationguard.LockWorkspace(ctx, tx, in.Authority.WorkspaceID); err != nil {
				return err
			}
			if err := s.authorizeAutomationRun(ctx, tx, in.request("", "")); err != nil {
				return err
			}
			var err error
			progress, passed, response, pending, err = s.followGateState(ctx, tx, in)
			if err != nil || passed || pending != nil || progress.State == SequenceWaitingResponse {
				return err
			}
			_, err = tx.NewRaw(`DELETE FROM message_sequence_waits WHERE run_id = ?`, in.RunID).Exec(ctx)
			return err
		})
		if err != nil || pending == nil {
			progress.Total = len(in.Messages)
			return progress, passed, response, err
		}
		check := s.readFollowStatus(ctx, *pending)
		check.RunID, check.StepID, check.Attempt, check.ResponseMessageID = in.RunID, in.StepID, pending.attempt, pending.response
		if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			if err := organizationguard.LockWorkspace(ctx, tx, in.Authority.WorkspaceID); err != nil {
				return err
			}
			if err := s.authorizeAutomationRun(ctx, tx, in.request("", "")); err != nil {
				return err
			}
			_, err := tx.NewInsert().Model(&check).On("CONFLICT DO NOTHING").Exec(ctx)
			return err
		}); err != nil {
			return SequenceProgress{}, false, "", err
		}
	}
	return SequenceProgress{}, false, "", errors.New("the follow check did not settle")
}

func (s *Service) followGateState(ctx context.Context, tx bun.Tx, in SequenceRequest) (SequenceProgress, bool, string, *pendingFollowCheck, error) {
	started, err := tx.NewSelect().Model((*AutomationMessage)(nil)).Where("run_id = ? AND step_id = ?", in.RunID, in.StepID).Exists(ctx)
	if err != nil || started {
		return SequenceProgress{}, started, "", nil, err
	}
	var checks []followCheck
	if err := tx.NewSelect().Model(&checks).Where("run_id = ? AND step_id = ?", in.RunID, in.StepID).Order("attempt ASC").Scan(ctx); err != nil {
		return SequenceProgress{}, false, "", nil, err
	}
	limit := in.FollowGate.checks()
	anchor := *in.Comment
	if len(checks) > 0 {
		last := checks[len(checks)-1]
		switch last.Outcome {
		case FollowCheckFollowing:
			return SequenceProgress{}, true, last.ResponseMessageID, nil, nil
		case FollowCheckFailed:
			return SequenceProgress{State: SequenceStopped, Reason: last.Reason}, false, "", nil, nil
		}
		if len(checks) >= limit {
			return SequenceProgress{State: SequenceNotFollowing, Reason: stopReasonNotFollowing(len(checks))}, false, "", nil, nil
		}
		progress, sentAt, err := s.advanceFollowRequest(ctx, tx, in, last)
		if err != nil || sentAt.IsZero() {
			return progress, false, "", nil, err
		}
		var response models.DirectMessage
		if err := tx.NewSelect().Model(&response).Where("id = ?", last.ResponseMessageID).Scan(ctx); err != nil {
			return SequenceProgress{}, false, "", nil, err
		}
		// The next reply must come while the request to follow can still be
		// answered: within the messaging window the last response opened.
		anchor.RepliedAt, anchor.RespondBy = sentAt, automationMessageTime(response).Add(sequenceResponseWindow)
	}
	probe := in
	probe.Comment, probe.ResponseMessageID = &anchor, ""
	progress, response, err := s.findSequenceResponse(ctx, tx, probe)
	if err != nil || response.ID == "" {
		if len(checks) > 0 {
			switch progress.State {
			case SequenceNoResponse:
				progress.Reason = stopReasonNoFollowReply
			case SequenceWaitingResponse:
				progress.Reason = waitingReasonFollow(len(checks), limit)
			}
		}
		return progress, false, "", nil, err
	}
	trigger, conversation, err := s.loadAutomationTrigger(ctx, tx, in.request(response.ID, ""))
	if err != nil {
		return SequenceProgress{}, false, "", nil, err
	}
	owned, err := tx.NewSelect().Model((*AutomationMessage)(nil)).Where("trigger_message_id = ? AND sequence_position = 0", trigger.ID).Exists(ctx)
	if err != nil {
		return SequenceProgress{}, false, "", nil, err
	}
	if owned {
		return SequenceProgress{State: AutomationSkipped, Reason: stopReasonOwned, Reply: AutomationMessage{TriggerMessageID: trigger.ID, ConversationID: conversation.ID}}, false, "", nil, nil
	}
	stopped, err := s.stopForEarlierHumanReply(ctx, tx, conversation, in.Comment.RepliedAt)
	if err != nil || stopped.State != "" {
		return stopped, false, "", nil, err
	}
	var account models.SocialAccount
	if err := tx.NewSelect().Model(&account).Where("id = ? AND is_active = ?", conversation.SocialAccountID, true).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SequenceProgress{State: SequenceStopped, Reason: "The Instagram account is no longer connected."}, false, "", nil, nil
		}
		return SequenceProgress{}, false, "", nil, err
	}
	return SequenceProgress{}, false, "", &pendingFollowCheck{attempt: len(checks) + 1, response: trigger.ID, account: account, counterpart: conversation.CounterpartRemoteID}, nil
}

// advanceFollowRequest converges the request to follow that answers one
// response onto one owned delivery, through the same checks as any automated
// message. sentAt is set once that request was delivered.
func (s *Service) advanceFollowRequest(ctx context.Context, tx bun.Tx, in SequenceRequest, check followCheck) (SequenceProgress, time.Time, error) {
	trigger, conversation, err := s.loadAutomationTrigger(ctx, tx, in.request(check.ResponseMessageID, ""))
	if err != nil {
		return SequenceProgress{}, time.Time{}, err
	}
	request := in.request(trigger.ID, strings.TrimSpace(in.FollowGate.NotFollowingText))
	// The request to follow offers the same quick reply again, so the person
	// can confirm by tapping instead of typing.
	if title := in.Comment.ResponseText; title != "" {
		request.QuickReplies = []platform.QuickReply{{Title: title, Payload: platform.FollowCheckQuickReplyPayload}}
	}
	request.StepID = followCheckStepID(in.StepID, check.Attempt)
	var reply AutomationMessage
	err = tx.NewSelect().Model(&reply).Where("run_id = ? AND step_id = ? AND sequence_position = 0", in.RunID, request.StepID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		stopped, err := s.stopForEarlierHumanReply(ctx, tx, conversation, in.Comment.RepliedAt)
		if err != nil || stopped.State != "" {
			return stopped, time.Time{}, err
		}
		if reply, err = s.claimAutomationReply(ctx, tx, trigger, conversation, request); err != nil {
			return SequenceProgress{}, time.Time{}, err
		}
		if reply.ID == "" {
			return SequenceProgress{State: AutomationSkipped, Reason: reply.Reason, Reply: reply}, time.Time{}, nil
		}
		// The comment stays the campaign's subject, so its takeover stops this too.
		reply.CommentItemID = in.Comment.ItemID
		if err := s.saveAutomationReply(ctx, tx, &reply); err != nil {
			return SequenceProgress{}, time.Time{}, err
		}
	} else if err != nil {
		return SequenceProgress{}, time.Time{}, err
	}
	if body := strings.TrimSpace(in.ApprovedBody); body != "" {
		request.Body = body
	} else {
		request.Body = reply.Body
	}
	if err := s.advanceClaimedReply(ctx, tx, trigger, conversation, request, &reply); err != nil {
		return SequenceProgress{}, time.Time{}, err
	}
	progress := SequenceProgress{State: reply.State, Reason: reply.Reason, Reply: reply}
	switch reply.State {
	case AutomationSent:
		var message models.DirectMessage
		if err := tx.NewSelect().Model(&message).Where("id = ?", reply.MessageID).Scan(ctx); err != nil {
			return SequenceProgress{}, time.Time{}, err
		}
		return progress, automationMessageTime(message), nil
	case AutomationAwaitingReview, AutomationReady, AutomationSending:
	case AutomationSkipped:
		if reply.Reason != StopReasonSequenceActive && reply.Reason != stopReasonOwned {
			progress.State = SequenceStopped
		}
	default:
		progress.State = SequenceStopped
	}
	return progress, time.Time{}, nil
}

// readFollowStatus never turns a failed read into "not following".
func (s *Service) readFollowStatus(ctx context.Context, pending pendingFollowCheck) followCheck {
	check := followCheck{CheckedAt: s.now()}
	failed := func(detail string) followCheck {
		check.Outcome, check.Reason = FollowCheckFailed, fmt.Sprintf("%s (%s). %s", followReadFailed, detail, followReadHint)
		return check
	}
	reader, ok := s.provider(pending.account).(platform.MessagingUserProfileReader)
	if !ok {
		return failed("this connection cannot read follow status")
	}
	token, err := s.messagingToken(ctx, pending.account)
	if err != nil {
		return failed("the account's access token is unavailable")
	}
	profile, err := reader.FetchMessagingUserProfile(ctx, token, pending.counterpart)
	if err != nil {
		return failed(followReadDetail(err))
	}
	check.Outcome = FollowCheckNotFollowing
	if profile.FollowsAccount {
		check.Outcome = FollowCheckFollowing
	}
	return check
}

func followReadDetail(err error) string {
	var providerErr *platform.HTTPError
	var transportErr *platform.TransportError
	switch {
	case errors.As(err, &providerErr) && providerErr.Code != "":
		return fmt.Sprintf("Instagram answered HTTP %d, code %s", providerErr.StatusCode, providerErr.Code)
	case errors.As(err, &providerErr):
		return fmt.Sprintf("Instagram answered HTTP %d", providerErr.StatusCode)
	case errors.As(err, &transportErr):
		return transportErr.Error()
	case errors.Is(err, platform.ErrFollowStatusUnreported):
		return "Instagram's answer did not include it"
	}
	return "the profile request failed"
}

// linkButtonIssue applies the button template's own limit, which is stricter
// than a plain message's.
func linkButtonIssue(button *platform.MessageButton, body string) string {
	if button == nil {
		return ""
	}
	if utf8.RuneCountInString(strings.TrimSpace(body)) > platform.MessageButtonTextLimit {
		return fmt.Sprintf("A message with a link button supports up to %d characters. Shorten the text.", platform.MessageButtonTextLimit)
	}
	return ""
}

// attachSendOptions stores the button or quick reply with the outbound
// message, so the send job, a retry and Inbox all read the same ones.
func attachSendOptions(ctx context.Context, db bun.IDB, message *models.DirectMessage, button *platform.MessageButton, quickReplies []platform.QuickReply) error {
	var attachments []platform.MessageAttachment
	if button != nil {
		attachments = append(attachments, platform.MessageAttachment{Type: linkButtonAttachment, URL: button.URL, Name: button.Title})
	}
	for _, reply := range quickReplies {
		attachments = append(attachments, platform.MessageAttachment{Type: quickReplyAttachment, Name: reply.Title})
	}
	if len(attachments) == 0 {
		return nil
	}
	encoded, err := json.Marshal(attachments)
	if err != nil {
		return err
	}
	message.AttachmentsJSON = string(encoded)
	_, err = db.NewUpdate().Model(message).Column("attachments_json").WherePK().Exec(ctx)
	return err
}

func outboundAttachments(attachmentsJSON, kind string) []platform.MessageAttachment {
	var attachments []platform.MessageAttachment
	if json.Unmarshal([]byte(attachmentsJSON), &attachments) != nil {
		return nil
	}
	var matching []platform.MessageAttachment
	for _, attachment := range attachments {
		if attachment.Type == kind {
			matching = append(matching, attachment)
		}
	}
	return matching
}

func messageLinkButtons(attachmentsJSON string) []platform.MessageButton {
	var buttons []platform.MessageButton
	for _, attachment := range outboundAttachments(attachmentsJSON, linkButtonAttachment) {
		if len(buttons) < platform.MaxMessageButtons {
			buttons = append(buttons, platform.MessageButton{Title: attachment.Name, URL: attachment.URL})
		}
	}
	return buttons
}

// Only a follow check stores quick replies, so they carry its payload.
func messageQuickReplies(attachmentsJSON string) []platform.QuickReply {
	var replies []platform.QuickReply
	for _, attachment := range outboundAttachments(attachmentsJSON, quickReplyAttachment) {
		replies = append(replies, platform.QuickReply{Title: attachment.Name, Payload: platform.FollowCheckQuickReplyPayload})
	}
	return replies
}
