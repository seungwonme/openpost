package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/providerwrite"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// automationFixture is an Instagram conversation with one live inbound message
// and a running workflow run, at a clock the test controls.
type automationFixture struct {
	s        *Service
	db       *bun.DB
	account  models.SocialAccount
	actor    Actor
	now      *time.Time
	sends    *int
	trigger  models.DirectMessage
	request  AutomationRequest
	sendFunc *func(context.Context) (platform.SendMessageResult, error)

	history    *[]platform.ProviderMessage
	historyErr *error
	refreshes  *int
}

// automationProvider adds the conversation read an automated send requires.
type automationProvider struct {
	testProvider
	messages *[]platform.ProviderMessage
	failure  *error
	reads    *int
}

func (p automationProvider) FetchMessageHistory(context.Context, string, platform.FetchMessageHistoryRequest) (platform.FetchMessageHistoryResult, error) {
	*p.reads++
	return platform.FetchMessageHistoryResult{Messages: *p.messages}, *p.failure
}

func newAutomationFixture(t *testing.T) automationFixture {
	t.Helper()
	s, db, account, actor := messagingFixture(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.now = func() time.Time { return now }
	account.Platform = "instagram"
	account.AccountID = "owner"
	_, err := db.NewUpdate().Model(&account).Column("platform", "account_id").WherePK().Exec(t.Context())
	require.NoError(t, err)
	_, err = db.NewUpdate().Model((*models.Conversation)(nil)).Set("platform='instagram'").Set("counterpart_remote_id='reader'").Set("messaging_window_expires_at=?", now.Add(24*time.Hour)).Where("id='conversation'").Exec(t.Context())
	require.NoError(t, err)
	sends := 0
	send := func(context.Context) (platform.SendMessageResult, error) {
		return platform.SendMessageResult{RemoteMessageID: "sent-remote-id"}, nil
	}
	reads := 0
	history := []platform.ProviderMessage{}
	var historyErr error
	fixture := automationFixture{s: s, db: db, account: account, actor: actor, now: &now, sends: &sends, sendFunc: &send, history: &history, historyErr: &historyErr, refreshes: &reads}
	s.SetProvider("instagram", automationProvider{testProvider: testProvider{send: func(ctx context.Context) (platform.SendMessageResult, error) {
		sends++
		return send(ctx)
	}}, messages: &history, failure: &historyErr, reads: &reads})
	trigger := models.DirectMessage{ID: "trigger", WorkspaceID: "workspace", ConversationID: "conversation", RemoteMessageID: "mid.trigger", Direction: "inbound", AuthorRemoteID: "reader", Body: "guide please", AttachmentsJSON: "[]", SendStatus: "received", RemoteCreatedAt: now, CreatedAt: now, UpdatedAt: now}
	_, err = db.NewInsert().Model(&trigger).Exec(t.Context())
	require.NoError(t, err)
	fixture.trigger = trigger
	fixture.request = AutomationRequest{Authority: workspaceaccess.StoredAuthority{UserID: "user", WorkspaceID: "workspace", OrganizationID: "org", AssuredAt: now}, WorkflowID: "workflow", RunID: "run", StepID: "reply", TriggerMessageID: "trigger", Body: "Here is the guide", Delivery: "automatic"}
	fixture.seedRun(t, "run", "workflow")
	return fixture
}

func (f automationFixture) seedRun(t *testing.T, runID, workflowID string) {
	t.Helper()
	_, err := f.db.NewRaw(`INSERT INTO workflows(id,workspace_id,name,draft_json,published_json,authority_json,created_at,updated_at) SELECT ?,?,?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM workflows WHERE id=?)`, workflowID, "workspace", "Replies", "{}", "{}", "{}", *f.now, *f.now, workflowID).Exec(t.Context())
	require.NoError(t, err)
	_, err = f.db.NewRaw(`INSERT INTO workflow_runs(id,workflow_id,workspace_id,workflow_name,workflow_revision,mode,state,revision,definition_json,authority_json,source_json,remaining_json,results_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, runID, workflowID, "workspace", "Replies", 1, "live", "running", 1, "{}", "{}", "{}", "[]", "[]", *f.now, *f.now).Exec(t.Context())
	require.NoError(t, err)
}

func (f automationFixture) setRunState(t *testing.T, state string) {
	t.Helper()
	_, err := f.db.NewRaw(`UPDATE workflow_runs SET state=? WHERE id='run'`, state).Exec(t.Context())
	require.NoError(t, err)
}

func (f automationFixture) runSendJob(t *testing.T, messageID string) error {
	t.Helper()
	payload, err := json.Marshal(subjectJob{ID: messageID})
	require.NoError(t, err)
	return f.s.HandleJob(t.Context(), JobTypeMessageSend, string(payload))
}

func (f automationFixture) message(t *testing.T, id string) models.DirectMessage {
	t.Helper()
	var message models.DirectMessage
	require.NoError(t, f.db.NewSelect().Model(&message).Where("id=?", id).Scan(t.Context()))
	return message
}

func (f automationFixture) receive(t *testing.T, id string, at time.Time) {
	t.Helper()
	message := models.DirectMessage{ID: id, WorkspaceID: "workspace", ConversationID: "conversation", RemoteMessageID: "mid." + id, Direction: "inbound", AuthorRemoteID: "reader", Body: "yes please", AttachmentsJSON: "[]", SendStatus: "received", RemoteCreatedAt: at, CreatedAt: at, UpdatedAt: at}
	_, err := f.db.NewInsert().Model(&message).Exec(t.Context())
	require.NoError(t, err)
}

func (f automationFixture) optOut(t *testing.T) {
	t.Helper()
	_, err := f.db.NewRaw(`INSERT INTO engagement_automation_optouts(social_account_id,author_remote_id,created_at) VALUES('account','reader',?)`, *f.now).Exec(t.Context())
	require.NoError(t, err)
}

func TestAutomatedReplyConvergesOntoOneProviderWrite(t *testing.T) {
	f := newAutomationFixture(t)
	first, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, AutomationSending, first.State)
	require.NotEmpty(t, first.MessageID)
	// A worker replay of the effect owns the same delivery rather than queueing another.
	replay, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, first.MessageID, replay.MessageID)
	count, err := f.db.NewSelect().Model((*models.DirectMessage)(nil)).Where("direction='outbound'").Count(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, count)

	require.NoError(t, f.runSendJob(t, first.MessageID))
	require.NoError(t, f.runSendJob(t, first.MessageID))
	assert.Equal(t, 1, *f.sends)
	done, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	assert.Equal(t, AutomationSent, done.State)
	assert.Equal(t, "sent", f.message(t, first.MessageID).SendStatus)
}

func TestSecondWorkflowCannotAnswerAClaimedMessage(t *testing.T) {
	f := newAutomationFixture(t)
	_, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	f.seedRun(t, "other-run", "other-workflow")
	other := f.request
	other.RunID, other.WorkflowID = "other-run", "other-workflow"
	reply, err := f.s.AdvanceAutomationReply(t.Context(), other)
	require.NoError(t, err)
	assert.Empty(t, reply.ID, "a losing workflow must not own a receipt")
	assert.Equal(t, AutomationSkipped, reply.State)
	assert.Equal(t, stopReasonOwned, reply.Reason)
	count, err := f.db.NewSelect().Model((*models.DirectMessage)(nil)).Where("direction='outbound'").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestWindowExpiringWhileQueuedStopsTheSendWithItsReason(t *testing.T) {
	f := newAutomationFixture(t)
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	*f.now = f.now.Add(25 * time.Hour)
	require.NoError(t, f.runSendJob(t, reply.MessageID), "a policy stop is an outcome, not a retryable job failure")
	assert.Zero(t, *f.sends)
	message := f.message(t, reply.MessageID)
	assert.Equal(t, "failed", message.SendStatus)
	assert.Equal(t, stopReasonWindow, message.ErrorMessage)
	stopped, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	assert.Equal(t, AutomationCancelled, stopped.State)
	assert.Equal(t, stopReasonWindow, stopped.Reason)
}

func TestReviewDelayCannotCarryApprovalPastTheWindow(t *testing.T) {
	f := newAutomationFixture(t)
	f.request.Delivery = "review"
	parked, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, AutomationAwaitingReview, parked.State)
	f.setRunState(t, "awaiting_approval")

	*f.now = f.now.Add(25 * time.Hour)
	approve := f.request
	approve.Approve, approve.Body = true, "Edited guide"
	stopped, err := f.s.AdvanceAutomationReply(t.Context(), approve)
	require.NoError(t, err)
	assert.Equal(t, AutomationCancelled, stopped.State)
	assert.Equal(t, stopReasonWindow, stopped.Reason)
	assert.Empty(t, stopped.MessageID)
}

func TestApprovedReviewSendsTheEditedText(t *testing.T) {
	f := newAutomationFixture(t)
	f.request.Delivery = "review"
	_, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	f.setRunState(t, "awaiting_approval")
	approve := f.request
	approve.Approve, approve.Body = true, "  Edited guide  "
	ready, err := f.s.AdvanceAutomationReply(t.Context(), approve)
	require.NoError(t, err)
	require.Equal(t, AutomationReady, ready.State)
	require.Empty(t, ready.MessageID, "approval alone never reaches the provider")
	f.setRunState(t, "running")
	f.request.Body = "stale authored text"
	sending, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, AutomationSending, sending.State)
	assert.Equal(t, "Edited guide", f.message(t, sending.MessageID).Body)
}

func TestReviewRejectsTextOverTheByteLimitWithoutLosingTheClaim(t *testing.T) {
	f := newAutomationFixture(t)
	f.request.Delivery = "review"
	_, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	f.setRunState(t, "awaiting_approval")
	approve := f.request
	approve.Approve = true
	// 334 three-byte characters are under 1000 characters and over 1000 bytes.
	approve.Body = strings.Repeat("가", 334)
	_, err = f.s.AdvanceAutomationReply(t.Context(), approve)
	require.ErrorContains(t, err, "1000 bytes")
	var stored AutomationMessage
	require.NoError(t, f.db.NewSelect().Model(&stored).Where("run_id='run'").Scan(t.Context()))
	assert.Equal(t, AutomationAwaitingReview, stored.State)
}

func TestOptedOutSenderIsNeverAnsweredAtClaimOrDispatch(t *testing.T) {
	t.Run("claim", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.optOut(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		assert.Equal(t, AutomationCancelled, reply.State)
		assert.Equal(t, stopReasonOptedOut, reply.Reason)
		assert.Empty(t, reply.MessageID)
	})
	t.Run("dispatch", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		f.optOut(t)
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		assert.Zero(t, *f.sends)
		assert.Equal(t, stopReasonOptedOut, f.message(t, reply.MessageID).ErrorMessage)
	})
}

func TestOptOutMessageRecordsTheSharedOptOutAndStopsPendingWork(t *testing.T) {
	for _, body := range []string{"STOP", " stop! ", "unsubscribe", "Stop replying.", "그만", "그만 보내주세요!", "수신거부", "수신 거부"} {
		t.Run(body, func(t *testing.T) {
			f := newAutomationFixture(t)
			f.request.Delivery = "review"
			parked, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
			require.NoError(t, err)
			require.Equal(t, AutomationAwaitingReview, parked.State)
			*f.now = f.now.Add(time.Minute)
			require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", platform.FetchMessageEventsResult{Conversations: []platform.ProviderConversation{{ID: "remote-thread", CounterpartRemoteID: "reader", LastMessageAt: *f.now, ReplyWindowExpiresAt: f.now.Add(24 * time.Hour), Messages: []platform.ProviderMessage{{ID: "mid.stop", Direction: "inbound", AuthorRemoteID: "reader", Body: body, RemoteCreatedAt: *f.now}}}}}))
			optedOut, err := f.db.NewSelect().Table("engagement_automation_optouts").Where("social_account_id='account' AND author_remote_id='reader'").Exists(t.Context())
			require.NoError(t, err)
			assert.True(t, optedOut)
			var stored AutomationMessage
			require.NoError(t, f.db.NewSelect().Model(&stored).Where("run_id='run'").Scan(t.Context()))
			assert.Equal(t, AutomationCancelled, stored.State)
			assert.Equal(t, stopReasonOptedOut, stored.Reason)
		})
	}
}

func TestOrdinarySentencesAreNotOptOuts(t *testing.T) {
	for _, body := range []string{"stop by the shop", "please don't stop", "그만큼 좋아요"} {
		assert.False(t, messageRequestsOptOut(body), body)
	}
}

func TestTakeoverStopsAQueuedSendAndLaterClaims(t *testing.T) {
	f := newAutomationFixture(t)
	var cancelled []string
	var reason string
	f.s.SetAutomationRunStopper(func(_ context.Context, _ bun.Tx, _, _ string, ids []string, why string) error {
		cancelled, reason = ids, why
		return nil
	})
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	require.NoError(t, f.s.StopConversationAutomation(t.Context(), f.actor, "workspace", "conversation"))
	assert.Equal(t, []string{"run"}, cancelled)
	assert.Equal(t, stopReasonTakeover, reason)
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	assert.Zero(t, *f.sends, "a takeover before the fence must stop the queued write")

	state, err := f.s.ConversationAutomationState(t.Context(), f.actor, "workspace", "conversation")
	require.NoError(t, err)
	require.NotNil(t, state.StoppedAt)
	require.Len(t, state.Replies, 1)
	assert.Equal(t, AutomationCancelled, state.Replies[0].State)

	// A later message in a taken-over conversation claims nothing new to send.
	later := f.trigger
	later.ID, later.RemoteMessageID = "later", "mid.later"
	_, err = f.db.NewInsert().Model(&later).Exec(t.Context())
	require.NoError(t, err)
	f.seedRun(t, "later-run", "workflow")
	next := f.request
	next.RunID, next.TriggerMessageID = "later-run", "later"
	stopped, err := f.s.AdvanceAutomationReply(t.Context(), next)
	require.NoError(t, err)
	assert.Equal(t, AutomationCancelled, stopped.State)
	assert.Equal(t, stopReasonTakeover, stopped.Reason)

	require.NoError(t, f.s.ResumeConversationAutomation(t.Context(), f.actor, "workspace", "conversation"))
	state, err = f.s.ConversationAutomationState(t.Context(), f.actor, "workspace", "conversation")
	require.NoError(t, err)
	assert.Nil(t, state.StoppedAt)
}

func TestManualSendTakesOverTheConversation(t *testing.T) {
	f := newAutomationFixture(t)
	f.request.Delivery = "review"
	_, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	_, err = f.s.QueueMessage(t.Context(), f.actor, "conversation", "I can help with that", "manual-operation")
	require.NoError(t, err)
	state, err := f.s.ConversationAutomationState(t.Context(), f.actor, "workspace", "conversation")
	require.NoError(t, err)
	require.NotNil(t, state.StoppedAt)
	assert.Equal(t, AutomationCancelled, state.Replies[0].State)
}

func TestHumanReplyInInstagramStopsAutomationButOwnEchoDoesNot(t *testing.T) {
	echo := func(id, body string, at time.Time) platform.FetchMessageEventsResult {
		return platform.FetchMessageEventsResult{Conversations: []platform.ProviderConversation{{ID: "remote-thread", CounterpartRemoteID: "reader", LastMessageAt: at, Messages: []platform.ProviderMessage{{ID: id, Direction: "outbound", AuthorRemoteID: "owner", Body: body, RemoteCreatedAt: at}}}}}
	}
	t.Run("own echo before acceptance", func(t *testing.T) {
		f := newAutomationFixture(t)
		*f.sendFunc = func(ctx context.Context) (platform.SendMessageResult, error) {
			*f.now = f.now.Add(time.Second)
			return platform.SendMessageResult{RemoteMessageID: "sent-remote-id"}, f.s.ApplyProviderEvents(ctx, "account", echo("sent-remote-id", "Here is the guide", *f.now))
		}
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		*f.now = f.now.Add(time.Second)
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		assert.Equal(t, 1, *f.sends)
		done, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		assert.Equal(t, AutomationSent, done.State)
		state, err := f.s.ConversationAutomationState(t.Context(), f.actor, "workspace", "conversation")
		require.NoError(t, err)
		assert.Nil(t, state.StoppedAt, "automation's own echo is not a person replying")
	})
	t.Run("own echo stored before a later reply claims", func(t *testing.T) {
		f := newAutomationFixture(t)
		first, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		require.Equal(t, AutomationSending, first.State)
		later := f.trigger
		later.ID, later.RemoteMessageID = "later", "mid.later"
		later.RemoteCreatedAt = f.now.Add(time.Second)
		_, err = f.db.NewInsert().Model(&later).Exec(t.Context())
		require.NoError(t, err)
		// The first reply's echo arrives after the later message and before acceptance.
		*f.now = f.now.Add(2 * time.Second)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", echo("sent-remote-id", "Here is the guide", *f.now)))
		f.seedRun(t, "later-run", "workflow")
		next := f.request
		next.RunID, next.TriggerMessageID, next.Body = "later-run", "later", "Second answer"
		reply, err := f.s.AdvanceAutomationReply(t.Context(), next)
		require.NoError(t, err)
		assert.Equal(t, AutomationSending, reply.State, "an unfinalized echo is not a person replying")
	})
	t.Run("person replies while queued", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		*f.now = f.now.Add(time.Second)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", echo("human-id", "Hi, I'll handle this", *f.now)))
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		assert.Zero(t, *f.sends)
		assert.Equal(t, stopReasonHuman, f.message(t, reply.MessageID).ErrorMessage)
		state, err := f.s.ConversationAutomationState(t.Context(), f.actor, "workspace", "conversation")
		require.NoError(t, err)
		assert.NotNil(t, state.StoppedAt)
	})
}

func TestUncertainDeliveryIsNeverRepeated(t *testing.T) {
	f := newAutomationFixture(t)
	*f.sendFunc = func(context.Context) (platform.SendMessageResult, error) {
		return platform.SendMessageResult{}, context.DeadlineExceeded
	}
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	err = f.runSendJob(t, reply.MessageID)
	require.Error(t, err)
	require.True(t, providerwrite.IsAmbiguous(err))
	require.Equal(t, "ambiguous", f.message(t, reply.MessageID).SendStatus)

	// Neither a queue replay, a workflow replay nor an Inbox retry writes again.
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	replay, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	assert.Equal(t, AutomationUncertain, replay.State)
	assert.Equal(t, reply.MessageID, replay.MessageID)
	assert.NotEmpty(t, replay.Reason)
	_, err = f.s.RetryMessage(t.Context(), f.actor, "workspace", "conversation", reply.MessageID)
	require.ErrorIs(t, err, ErrRetryUnsafe)
	assert.Equal(t, 1, *f.sends)
}

func TestDefiniteRejectionIsNotRepeatedByAQueueReplay(t *testing.T) {
	f := newAutomationFixture(t)
	failure := &platform.HTTPError{StatusCode: http.StatusBadRequest}
	*f.sendFunc = func(context.Context) (platform.SendMessageResult, error) {
		return platform.SendMessageResult{RemoteMessageID: "rejected"}, failure
	}
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	require.ErrorIs(t, f.runSendJob(t, reply.MessageID), failure)
	require.NoError(t, f.runSendJob(t, reply.MessageID), "only a queued automated message is still unsent")
	assert.Equal(t, 1, *f.sends)
	failed, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	assert.Equal(t, AutomationFailed, failed.State)
	assert.NotEmpty(t, failed.Reason)
}

func TestProviderAcceptanceOutlivesARunCancelledDuringTheWrite(t *testing.T) {
	f := newAutomationFixture(t)
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	*f.sendFunc = func(ctx context.Context) (platform.SendMessageResult, error) {
		if err := f.s.StopConversationAutomation(ctx, f.actor, "workspace", "conversation"); err != nil {
			return platform.SendMessageResult{}, err
		}
		return platform.SendMessageResult{RemoteMessageID: "sent-remote-id"}, nil
	}
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	var stored AutomationMessage
	require.NoError(t, f.db.NewSelect().Model(&stored).Where("run_id='run'").Scan(t.Context()))
	assert.Equal(t, AutomationSent, stored.State, "acceptance is the cancellation boundary")
}

func TestAutomationIgnoresOwnMessagesAndOtherPlatforms(t *testing.T) {
	t.Run("own message", func(t *testing.T) {
		f := newAutomationFixture(t)
		_, err := f.db.NewUpdate().Model((*models.DirectMessage)(nil)).Set("direction='outbound'").Where("id='trigger'").Exec(t.Context())
		require.NoError(t, err)
		_, err = f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.ErrorContains(t, err, "message from the other person")
	})
	t.Run("other platform", func(t *testing.T) {
		f := newAutomationFixture(t)
		_, err := f.db.NewUpdate().Model((*models.Conversation)(nil)).Set("platform='bluesky'").Where("id='conversation'").Exec(t.Context())
		require.NoError(t, err)
		_, err = f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.ErrorContains(t, err, "Instagram only")
	})
}

func TestLoopGuardStopsARepeatingConversation(t *testing.T) {
	f := newAutomationFixture(t)
	for i := range maxAutomatedRepliesPerHour {
		id := "prior-" + string(rune('a'+i))
		row := AutomationMessage{ID: id, WorkspaceID: "workspace", ConversationID: "conversation", TriggerMessageID: id, WorkflowID: "workflow", RunID: id, StepID: "reply", UserID: "user", Body: "Automated " + id, State: AutomationSent, MessageID: id, CreatedAt: *f.now, UpdatedAt: *f.now}
		_, err := f.db.NewInsert().Model(&row).Exec(t.Context())
		require.NoError(t, err)
	}
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	assert.Equal(t, AutomationSkipped, reply.State)
	assert.Equal(t, stopReasonLoop, reply.Reason)
	assert.Empty(t, reply.MessageID)
}

func TestDuplicateWebhookStoresOneMessage(t *testing.T) {
	f := newAutomationFixture(t)
	*f.now = f.now.Add(time.Minute)
	event := platform.FetchMessageEventsResult{Conversations: []platform.ProviderConversation{{ID: "remote-thread", CounterpartRemoteID: "reader", LastMessageAt: *f.now, ReplyWindowExpiresAt: f.now.Add(24 * time.Hour), Messages: []platform.ProviderMessage{{ID: "mid.duplicate", Direction: "inbound", AuthorRemoteID: "reader", Body: "guide", RemoteCreatedAt: *f.now}}}}}
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", event))
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", event))
	count, err := f.db.NewSelect().Model((*models.DirectMessage)(nil)).Where("remote_message_id='mid.duplicate'").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
