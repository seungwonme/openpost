package messaging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func inboundEvent(id, body string, at time.Time) platform.FetchMessageEventsResult {
	return platform.FetchMessageEventsResult{Conversations: []platform.ProviderConversation{{ID: "remote-thread", CounterpartRemoteID: "reader", LastMessageAt: at, Messages: []platform.ProviderMessage{{ID: id, Direction: "inbound", AuthorRemoteID: "reader", Body: body, RemoteCreatedAt: at}}}}}
}

func outboundEvent(id, body string, at time.Time) platform.FetchMessageEventsResult {
	return platform.FetchMessageEventsResult{Conversations: []platform.ProviderConversation{{ID: "remote-thread", CounterpartRemoteID: "reader", LastMessageAt: at, Messages: []platform.ProviderMessage{{ID: id, Direction: "outbound", AuthorRemoteID: "owner", Body: body, RemoteCreatedAt: at}}}}}
}

func (f automationFixture) stored(t *testing.T, messageID string) AutomationMessage {
	t.Helper()
	var reply AutomationMessage
	require.NoError(t, f.db.NewSelect().Model(&reply).Where("message_id = ?", messageID).Scan(t.Context()))
	return reply
}

func (f automationFixture) takenOver(t *testing.T) bool {
	t.Helper()
	state, err := f.s.ConversationAutomationState(t.Context(), f.actor, "workspace", "conversation")
	require.NoError(t, err)
	return state.StoppedAt != nil
}

func (f automationFixture) optedOut(t *testing.T) bool {
	t.Helper()
	exists, err := f.db.NewSelect().Table("engagement_automation_optouts").Where("social_account_id = 'account' AND author_remote_id = 'reader'").Exists(t.Context())
	require.NoError(t, err)
	return exists
}

func TestAutomatedSendRefreshesTheConversationFirst(t *testing.T) {
	t.Run("an opt-out no webhook delivered stops the send", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		*f.history = []platform.ProviderMessage{{ID: "mid.stop", Direction: "inbound", AuthorRemoteID: "reader", Body: "그만", RemoteCreatedAt: f.now.Add(time.Second)}}
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		assert.Zero(t, *f.sends)
		assert.Equal(t, AutomationCancelled, f.stored(t, reply.MessageID).State)
		assert.Equal(t, stopReasonOptedOut, f.stored(t, reply.MessageID).Reason)
		assert.True(t, f.optedOut(t))
	})
	t.Run("a reply in the Instagram app stops the send", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		*f.history = []platform.ProviderMessage{{ID: "mid.human", Direction: "outbound", AuthorRemoteID: "owner", Body: "I'll handle this", RemoteCreatedAt: f.now.Add(time.Second)}}
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		assert.Zero(t, *f.sends)
		assert.True(t, f.takenOver(t))
	})
	t.Run("a quiet conversation sends once", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		assert.Equal(t, 1, *f.sends)
		assert.Equal(t, 1, *f.refreshes)
	})
	t.Run("a failed refresh holds, then stops with a reason", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		*f.historyErr = errors.New("provider unavailable")
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		assert.Zero(t, *f.sends)
		assert.Equal(t, "queued", f.message(t, reply.MessageID).SendStatus, "a held message stays unsent")
		assert.Equal(t, AutomationSending, f.stored(t, reply.MessageID).State)
		retries, err := f.db.NewSelect().Model((*models.Job)(nil)).Where("type = ? AND run_at > ?", JobTypeMessageSend, *f.now).Count(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, retries, "the hold schedules its own retry")

		*f.now = f.now.Add(10 * time.Minute)
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		assert.Zero(t, *f.sends)
		stopped := f.stored(t, reply.MessageID)
		assert.Equal(t, AutomationCancelled, stopped.State)
		assert.Contains(t, stopped.Reason, "could not confirm")
		assert.Equal(t, "failed", f.message(t, reply.MessageID).SendStatus)
	})
	t.Run("a refresh that recovers within the bound sends", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		*f.historyErr = errors.New("provider unavailable")
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		*f.historyErr = nil
		*f.now = f.now.Add(time.Minute)
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		assert.Equal(t, 1, *f.sends)
	})
}

func TestProviderOutboundIsATakeoverAtIngest(t *testing.T) {
	t.Run("without any run", func(t *testing.T) {
		f := newAutomationFixture(t)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.human", "Hi, this is Sam", *f.now)))
		assert.True(t, f.takenOver(t))
	})
	t.Run("old history is not a takeover", func(t *testing.T) {
		f := newAutomationFixture(t)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.old", "From last week", f.now.Add(-7*24*time.Hour))))
		assert.False(t, f.takenOver(t))
	})
	t.Run("an echo of a send in progress is not", func(t *testing.T) {
		f := newAutomationFixture(t)
		_, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.echo", "Here is the guide", *f.now)))
		assert.False(t, f.takenOver(t))
	})
	t.Run("a redelivered echo after resume is not", func(t *testing.T) {
		f := newAutomationFixture(t)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.human", "Hi", *f.now)))
		require.NoError(t, f.s.ResumeConversationAutomation(t.Context(), f.actor, "workspace", "conversation"))
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.human", "Hi", *f.now)))
		assert.False(t, f.takenOver(t))
	})
}

func (f automationFixture) seedPrivateReply(t *testing.T, state string, updatedAt time.Time) {
	t.Helper()
	item := models.EngagementItem{ID: "comment", WorkspaceID: "workspace", SocialAccountID: "account", RemoteID: "remote-comment", Body: "guide", AuthorRemoteID: "reader", Platform: "instagram", RemoteCreatedAt: updatedAt, CreatedAt: updatedAt, UpdatedAt: updatedAt}
	_, err := f.db.NewInsert().Model(&item).Exec(t.Context())
	require.NoError(t, err)
	_, err = f.db.NewRaw(`INSERT INTO engagement_automation_replies(id,workspace_id,item_id,workflow_id,run_id,step_id,user_id,channel,recipient_key,body,item_body,state,job_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, uuid.NewString(), "workspace", "comment", "workflow", "run", "private", "user", "private", "recipient", "Your guide", "guide", state, "private-job", updatedAt, updatedAt).Exec(t.Context())
	require.NoError(t, err)
}

func (f automationFixture) seedAttempt(t *testing.T, operationID, jobID, externalID string) {
	t.Helper()
	now := *f.now
	attempt := models.ProviderWriteAttempt{ID: uuid.NewString(), OperationID: operationID, AttemptNumber: 1, JobID: jobID, WorkspaceID: "workspace", SocialAccountID: "account", TargetKey: "target", Provider: "instagram", Operation: "message_send", PayloadFingerprint: "sha256:fingerprint", Status: "accepted", SubmissionState: "accepted", RetrySafety: "never", ExternalID: externalID, CreatedAt: now, UpdatedAt: now}
	_, err := f.db.NewInsert().Model(&attempt).Exec(t.Context())
	require.NoError(t, err)
}

func TestOwnershipPrefersTheProviderMessageID(t *testing.T) {
	t.Run("a person typing a phrase automation once sent is a person", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		*f.now = f.now.Add(time.Minute)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.typed", "Here is the guide", *f.now)))
		assert.True(t, f.takenOver(t))
	})
	t.Run("the same phrase counts as a person when a later run checks", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		*f.now = f.now.Add(time.Minute)
		f.receive(t, "later", *f.now)
		*f.now = f.now.Add(time.Minute)
		typed := models.DirectMessage{ID: "typed", WorkspaceID: "workspace", ConversationID: "conversation", RemoteMessageID: "mid.typed", Direction: "outbound", AuthorRemoteID: "owner", Body: "Here is the guide", AttachmentsJSON: "[]", SendStatus: "sent", RemoteCreatedAt: *f.now, CreatedAt: *f.now, UpdatedAt: *f.now}
		_, err = f.db.NewInsert().Model(&typed).Exec(t.Context())
		require.NoError(t, err)
		f.seedRun(t, "later-run", "workflow")
		next := f.request
		next.RunID, next.TriggerMessageID = "later-run", "later"
		stopped, err := f.s.AdvanceAutomationReply(t.Context(), next)
		require.NoError(t, err)
		assert.Equal(t, stopReasonHuman, stopped.Reason)
	})
	t.Run("an unconfirmed private reply's echo is matched while recent", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.seedPrivateReply(t, "sending", *f.now)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.private", "Your guide", f.now.Add(time.Minute))))
		assert.False(t, f.takenOver(t))
	})
	t.Run("a private reply's echo is matched while recent", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.seedPrivateReply(t, "sent", *f.now)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.private", "Your guide", f.now.Add(time.Minute))))
		assert.False(t, f.takenOver(t))
	})
	t.Run("the same words long after a private reply are a person", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.seedPrivateReply(t, "sent", *f.now)
		*f.now = f.now.Add(2 * time.Hour)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.typed", "Your guide", *f.now)))
		assert.True(t, f.takenOver(t))
	})
	t.Run("a provider ID identifies a private reply however late its echo", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.seedPrivateReply(t, "sent", *f.now)
		f.seedAttempt(t, "engagement:private", "private-job", "mid.private")
		*f.now = f.now.Add(2 * time.Hour)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.private", "Your guide", *f.now)))
		assert.False(t, f.takenOver(t))
	})
	t.Run("another provider ID is not that private reply", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.seedPrivateReply(t, "sent", *f.now)
		f.seedAttempt(t, "engagement:private", "private-job", "mid.private")
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.typed", "Your guide", f.now.Add(time.Minute))))
		assert.True(t, f.takenOver(t))
	})
}

func TestMessageOptOutMatching(t *testing.T) {
	optOuts := []string{
		"STOP", "stop", "Stop!", "stop.", "unsubscribe", "stop replying", "STOP ❤️", "stop 🙏", "❤️ stop", "ＳＴＯＰ",
		"cancel", "don't message me", "stop texting", "거부", "차단", "알림 꺼주세요", "그만 좀", "그만 보내줘", "수신 거부할게요", " Stop. ",
		"stop please", "please stop", "stop it", "stop now please", "stop messaging me", "stop texting me", "stop sending me messages", "opt out", "optout", "opt me out", "unsub", "unsubscribe me", "do not message me", "don't message me", "remove me", "leave me alone",
		"그만", "그만!", "그만 해", "그만해", "그만 해요", "그만해요", "그만요", "그만 해주세요", "그만해주세요", "그만해줘", "그만보내", "그만 보내", "그만 보내요", "그만 보내세요", "그만 보내주세요", "이제 그만", "제발 그만해 🙏",
		"보내지 마세요", "보내지마", "보내지 마", "보내지 말아주세요", "연락하지 마세요",
		"수신거부", "수신 거부", "수신거부합니다", "수신거부요", "수신 거부 합니다", "수신거절", "구독취소", "구독 취소", "구독취소요", "구독 해지", "스탑", "스톱",
	}
	for _, body := range optOuts {
		assert.True(t, messageRequestsOptOut(body), "%q should opt out", body)
	}
	people := []string{
		"", "   ", "❤️", "stop by the shop", "don't stop", "can't stop", "non stop", "stop sign", "stop the music", "cancel my order", "거부권이 뭔가요", "차단기 가격", "bus stop", "when do you stop serving lunch", "guide please", "hello",
		"그만큼 좋아요", "그만두셨나요", "그만한 가격이면 살게요", "가이드 보내주세요", "구독 어떻게 하나요", "안녕하세요", "버스탑승 안내",
		"제 친구가 수신거부를 어떻게 하는지 물어봐서요 혹시 아시나요", "어제 받은 메시지가 너무 좋아서 이제 그만 고민하고 바로 결제하려고 합니다",
	}
	for _, body := range people {
		assert.False(t, messageRequestsOptOut(body), "%q should not opt out", body)
	}
}

func TestOptOutIsRecordedOnEveryIngestPath(t *testing.T) {
	t.Run("history import", func(t *testing.T) {
		f := newAutomationFixture(t)
		var conversation models.Conversation
		require.NoError(t, f.db.NewSelect().Model(&conversation).Where("id = 'conversation'").Scan(t.Context()))
		page := platform.FetchMessageHistoryResult{Messages: []platform.ProviderMessage{{ID: "mid.history", Direction: "inbound", AuthorRemoteID: "reader", Body: "수신거부", RemoteCreatedAt: f.now.Add(-time.Hour)}}}
		require.NoError(t, f.s.persistHistoryPage(t.Context(), f.account, conversation, page))
		assert.True(t, f.optedOut(t))
	})
	t.Run("repair of a message stored without content", func(t *testing.T) {
		f := newAutomationFixture(t)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.blank", "", *f.now)))
		assert.False(t, f.optedOut(t))
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.blank", "STOP", *f.now)))
		assert.True(t, f.optedOut(t))
	})
	t.Run("redelivery stays one opt-out", func(t *testing.T) {
		f := newAutomationFixture(t)
		for range 2 {
			require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.stop", "STOP", *f.now)))
		}
		count, err := f.db.NewSelect().Table("engagement_automation_optouts").Count(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})
}

func TestDailyCapCoversAllAutomationInAConversation(t *testing.T) {
	f := newAutomationFixture(t)
	for i := range 20 {
		row := AutomationMessage{ID: uuid.NewString(), WorkspaceID: "workspace", ConversationID: "conversation", TriggerMessageID: uuid.NewString(), WorkflowID: "workflow", RunID: "old-run", StepID: uuid.NewString(), UserID: "user", Body: "old", State: AutomationSent, CreatedAt: f.now.Add(-time.Duration(i+2) * time.Hour), UpdatedAt: *f.now}
		_, err := f.db.NewInsert().Model(&row).Exec(t.Context())
		require.NoError(t, err)
	}
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	assert.Equal(t, AutomationSkipped, reply.State)
	assert.Equal(t, stopReasonDaily, reply.Reason)
	assert.Zero(t, *f.sends)
}

// Two connected accounts that do not recognize each other's IDs can answer each
// other forever. Nothing else stops that loop, so the cap has to hold on its own.
func TestDailyCapStopsALoopBetweenAccountsThatLookLikePeople(t *testing.T) {
	f := newAutomationFixture(t)
	start := *f.now
	sent := 0
	for round := 0; f.now.Before(start.Add(24 * time.Hour)); round++ {
		id := "incoming-" + uuid.NewString()
		f.receive(t, id, *f.now)
		_, err := f.db.NewUpdate().Model((*models.Conversation)(nil)).Set("messaging_window_expires_at = ?", f.now.Add(24*time.Hour)).Where("id = 'conversation'").Exec(t.Context())
		require.NoError(t, err)
		runID := "run-" + id
		f.seedRun(t, runID, "workflow")
		request := f.request
		request.RunID, request.TriggerMessageID = runID, id
		reply, err := f.s.AdvanceAutomationReply(t.Context(), request)
		require.NoError(t, err)
		if reply.State == AutomationSending {
			require.NoError(t, f.runSendJob(t, reply.MessageID))
			sent++
		}
		*f.now = f.now.Add(12 * time.Minute)
	}
	assert.Equal(t, maxAutomatedRepliesPerDay, sent)
	assert.Equal(t, sent, *f.sends)
}

func TestAcceptedWriteIsRecordedAsSentDespiteALaterOptOut(t *testing.T) {
	f := newAutomationFixture(t)
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	f.seedAttempt(t, "messaging:"+reply.MessageID, "job", "mid.accepted")
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.stop", "STOP", *f.now)))
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	assert.Zero(t, *f.sends, "the accepted write is not repeated")
	message := f.message(t, reply.MessageID)
	assert.Equal(t, "sent", message.SendStatus)
	assert.Equal(t, "mid.accepted", message.RemoteMessageID)
	assert.Equal(t, AutomationSent, f.stored(t, reply.MessageID).State)
}

func TestAutomatedSendWorksInAnOrganizationThatRequiresSSO(t *testing.T) {
	f := newAutomationFixture(t)
	now := *f.now
	_, err := f.db.NewInsert().Model(&models.OrganizationSSOPolicy{OrganizationID: "org", Mode: models.OrganizationSSOModeRequired, CreatedAt: now, UpdatedAt: now}).Exec(t.Context())
	require.NoError(t, err)
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	assert.Equal(t, 1, *f.sends)
}

func TestAutomatedSendStillRequiresActiveMembership(t *testing.T) {
	f := newAutomationFixture(t)
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	_, err = f.db.NewDelete().Model((*models.WorkspaceMember)(nil)).Where("workspace_id = 'workspace' AND user_id = 'user'").Exec(t.Context())
	require.NoError(t, err)
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	assert.Zero(t, *f.sends)
}

func TestOperatorReplyInTheAppBeforeATriggerStopsIt(t *testing.T) {
	f := newAutomationFixture(t)
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.human", "I'm a person, let me help", f.now.Add(-time.Minute))))
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	require.NotEqual(t, AutomationSending, reply.State)
	require.NotEmpty(t, reply.Reason)
	require.Zero(t, *f.sends)
	require.True(t, f.takenOver(t))
}

func TestOptOutFirstSeenInHistoryStopsTheNextReply(t *testing.T) {
	f := newAutomationFixture(t)
	at := f.now.Add(time.Minute)
	stop := platform.ProviderMessage{ID: "mid.stop", Direction: "inbound", AuthorRemoteID: "reader", Body: "STOP", RemoteCreatedAt: at}
	var conversation models.Conversation
	require.NoError(t, f.db.NewSelect().Model(&conversation).Where("id = 'conversation'").Scan(t.Context()))
	require.NoError(t, f.s.persistHistoryPage(t.Context(), f.account, conversation, platform.FetchMessageHistoryResult{Messages: []platform.ProviderMessage{stop}}))
	require.True(t, f.optedOut(t))
	_, err := f.s.persistConversations(t.Context(), f.account, []platform.ProviderConversation{{ID: conversation.RemoteConversationID, CounterpartRemoteID: "reader", Messages: []platform.ProviderMessage{stop}}})
	require.NoError(t, err)
	*f.now = at.Add(time.Minute)
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	require.NotEqual(t, AutomationSending, reply.State)
	require.Zero(t, *f.sends)
}

func TestBlankMessageRepairedToStopOptsOut(t *testing.T) {
	f := newAutomationFixture(t)
	at := f.now.Add(time.Minute)
	blank := platform.ProviderMessage{ID: "mid.blank", Direction: "inbound", AuthorRemoteID: "reader", RemoteCreatedAt: at}
	full := blank
	full.Body = "STOP"
	var conversation models.Conversation
	require.NoError(t, f.db.NewSelect().Model(&conversation).Where("id = 'conversation'").Scan(t.Context()))
	for _, message := range []platform.ProviderMessage{blank, full} {
		_, err := f.s.persistConversations(t.Context(), f.account, []platform.ProviderConversation{{ID: conversation.RemoteConversationID, CounterpartRemoteID: "reader", Messages: []platform.ProviderMessage{message}}})
		require.NoError(t, err)
	}
	var body string
	require.NoError(t, f.db.NewSelect().Table("direct_messages").Column("body").Where("remote_message_id = 'mid.blank'").Scan(t.Context(), &body))
	require.Equal(t, "STOP", body)
	require.True(t, f.optedOut(t))
}

func TestCrashAfterAcceptanceThenOptOutStaysSent(t *testing.T) {
	f := newAutomationFixture(t)
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	require.Equal(t, 1, *f.sends)
	_, err = f.db.NewRaw(`UPDATE direct_messages SET send_status = 'queued' WHERE id = ?`, reply.MessageID).Exec(t.Context())
	require.NoError(t, err)
	_, err = f.db.NewRaw(`UPDATE message_automation_replies SET state = 'sending' WHERE message_id = ?`, reply.MessageID).Exec(t.Context())
	require.NoError(t, err)
	f.optOut(t)
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	require.Equal(t, 1, *f.sends, "the accepted write is not repeated")
	require.Equal(t, "sent", f.message(t, reply.MessageID).SendStatus)
	require.Equal(t, AutomationSent, f.stored(t, reply.MessageID).State)
}

func TestTakeoverAsksWorkflowsToStopARunWithNoReceipt(t *testing.T) {
	f := newAutomationFixture(t)
	var stopped []string
	f.s.SetAutomationRunStopper(func(_ context.Context, _ bun.Tx, _, conversationID string, ids []string, _ string) error {
		assert.Empty(t, ids)
		stopped = append(stopped, conversationID)
		return nil
	})
	require.NoError(t, f.s.StopConversationAutomation(t.Context(), f.actor, "workspace", "conversation"))
	require.Equal(t, []string{"conversation"}, stopped)
}

func TestOnlyMessagesFirstStoredFromHistoryAreMarked(t *testing.T) {
	f := newAutomationFixture(t)
	var conversation models.Conversation
	require.NoError(t, f.db.NewSelect().Model(&conversation).Where("id = 'conversation'").Scan(t.Context()))
	imported := func() []string {
		ids := []string{}
		require.NoError(t, f.db.NewSelect().TableExpr("message_history_imports AS h").Join("JOIN direct_messages AS d ON d.id = h.message_id").ColumnExpr("d.remote_message_id").Order("d.remote_message_id").Scan(t.Context(), &ids))
		return ids
	}
	inbound := func(id string) platform.ProviderMessage {
		return platform.ProviderMessage{ID: id, Direction: "inbound", AuthorRemoteID: "reader", Body: "guide", RemoteCreatedAt: *f.now}
	}
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.live", "guide", *f.now)))
	page := platform.FetchMessageHistoryResult{Messages: []platform.ProviderMessage{inbound("mid.live"), inbound("mid.history")}}
	require.NoError(t, f.s.persistHistoryPage(t.Context(), f.account, conversation, page))
	require.Equal(t, []string{"mid.history"}, imported(), "a message polling stored first stays eligible")
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.history", "guide", *f.now)))
	require.Equal(t, []string{"mid.history"}, imported(), "a later delivery does not make history new")
}
