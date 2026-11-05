package messaging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
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
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("sent-remote-id", "Here is the guide", *f.now)))
		*f.now = f.now.Add(time.Minute)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.typed", "Here is the guide", *f.now)))
		assert.True(t, f.takenOver(t))
	})
	t.Run("the same phrase counts as a person when a later run checks", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("sent-remote-id", "Here is the guide", *f.now)))
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
	t.Run("another provider ID matches a private reply once, near its time", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.seedPrivateReply(t, "sent", *f.now)
		f.seedAttempt(t, "engagement:private", "private-job", "mid.private")
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.other", "Your guide", f.now.Add(time.Minute))))
		assert.False(t, f.takenOver(t))
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.typed", "Your guide", f.now.Add(2*time.Minute))))
		assert.True(t, f.takenOver(t))
	})
}

func TestOptOutTiers(t *testing.T) {
	tests := map[optOutTier][]string{
		optOutDefinite: {
			"STOP", "stop", "Stop!", "stop.", " Stop. ", "unsubscribe", "stop replying", "STOP ❤️", "stop 🙏", "❤️ stop", "ＳＴＯＰ",
			"stop please", "please stop", "stop it", "stop now please", "stop messaging me", "stop texting", "stop texting me", "stop sending me messages",
			"opt out", "optout", "opt me out", "unsub", "unsubscribe me", "do not message me", "don't message me", "dont text me", "no more messages", "leave me alone", "remove me",
			"그만", "그만!", "그만 해", "그만해", "그만 해요", "그만해요", "그만요", "그만 해주세요", "그만해주세요", "그만해줘", "그만보내", "그만 보내", "그만 보내요", "그만 보내줘", "그만 보내세요", "그만 보내주세요", "이제 그만", "제발 그만해 🙏", "그만 좀", "그만 🙏", "메시지 그만 보내주세요",
			"보내지 마세요", "보내지마", "보내지 마", "보내지 말아주세요", "연락하지 마세요", "메시지 보내지 마세요",
			"연락 주지 마세요", "연락주지마", "연락 주지 마", "연락 주지 마요", "연락 주지 말아주세요", "연락 마세요", "연락 말아주세요", "제발 연락 주지 마세요", "문자 주지 마세요", "메시지 주지 마세요", "메세지 주지 마세요", "디엠 주지 마세요",
			"수신거부", "수신 거부", "수신거부합니다", "수신거부요", "수신 거부 합니다", "수신 거부할게요", "수신거절", "구독취소", "구독 취소", "구독취소요", "구독 해지", "스탑", "스톱",
		},
		optOutPossible: {
			"수신거부 어떻게 하나요", "수신 거부 해제해주세요", "구독취소 환불 되나요", "이제 그만 고민할게요", "그만 웃겨요 ㅋㅋㅋ", "how do i unsubscribe", "알림 끄는 법 알려주세요",
			"이런 메시지는 이제 그만 보내주세요", "can you stop",
			"stop?", "그만?", "수신거부요?", "unsubscribe?", "could you please stop messaging me", "stop the music", "when do you stop serving lunch", "unsubscribe me from the other list",
			"cancel", "Cancel it", "please cancel it", "cancel it please", "cancel this", "cancel all", "cancel please", "cancel?", "거부", "차단", "차단합니다", "제발 차단", "알림 꺼주세요", "알림 끄는 법", "알림 끄면 못 받나요",
			"stop this", "stop all", "stop me", "stop to", "stop text",
			"please cancel", "연락 주지 말고 문자로 해주세요", "아직 연락 주지 마세요", "연락 안 주셔도 돼요", "수신거부 했는데 왜 또 오죠",
		},
		optOutNone: {
			"", "   ", "❤️", "stop by the shop", "don't stop", "can't stop", "non stop", "stop sign", "bus stop", "guide please", "hello",
			"그만큼 좋아요", "그만두셨나요", "그만한 가격이면 살게요", "가이드 보내주세요", "구독 어떻게 하나요", "안녕하세요", "버스탑승 안내", "거부권이 뭔가요", "차단기 가격",
			"주문 취소 cancel 가능한가요", "차단 해제 어떻게 해요", "차단 어떻게 풀어요", "cancel my order", "cancel my order please", "can i cancel", "cancel to", "cancel send", "취소", "취소해주세요",
			"차단됐어요", "차단 당했어요", "거부 당했어요", "카드 거부", "결제 거부",
			"next stop is seoul", "the last stop was great", "i love your bus stop photo", "we got off at the wrong stop",
			"thanks for the guide, we took the train and got off at the wrong stop but it was fine and we will be back next year with friends",
			"i read your guide last night and it helped so much that i could not stop reading it again and again",
			"제 친구가 수신거부를 어떻게 하는지 물어봐서요 혹시 아시나요", "어제 받은 메시지가 너무 좋아서 이제 그만 고민하고 바로 결제하려고 합니다",
			"연락 안 와요",
			"알림 꺼졌어요", "알림 꺼져서 못봤어요", "알림 꺼놨어요", "카톡 알림 꺼놨어요", "알림꺼짐", "알림 꺼둘게요", "알림 꺼뒀어요", "알림이 꺼져있어요",
		},
	}
	for tier, bodies := range tests {
		for _, body := range bodies {
			assert.Equal(t, tier, classifyOptOut(body), "%q", body)
		}
	}
}

func TestOrdinaryOrderMessagesLeaveAutomationRunning(t *testing.T) {
	for i, body := range []string{"주문 취소 cancel 가능한가요", "차단 해제 어떻게 해요", "cancel my order", "can i cancel", "알림 꺼져서 못봤어요", "next stop is seoul",
		"thanks for the guide, we took the train and got off at the wrong stop but it was fine and we will be back next year with friends"} {
		f := newAutomationFixture(t)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent(fmt.Sprintf("mid.%d", i), body, *f.now)))
		assert.False(t, f.optedOut(t), "%q", body)
		assert.False(t, f.takenOver(t), "%q", body)
	}
}

func TestOldRefusalDoesNotCreateAPermanentOptOut(t *testing.T) {
	page := func(f automationFixture, age time.Duration) platform.FetchMessageHistoryResult {
		return platform.FetchMessageHistoryResult{Messages: []platform.ProviderMessage{{ID: "mid.old", Direction: "inbound", AuthorRemoteID: "reader", Body: "그만", RemoteCreatedAt: f.now.Add(-age)}}}
	}
	conversation := func(f automationFixture) models.Conversation {
		var c models.Conversation
		require.NoError(t, f.db.NewSelect().Model(&c).Where("id = 'conversation'").Scan(t.Context()))
		return c
	}
	t.Run("on a history page", func(t *testing.T) {
		f := newAutomationFixture(t)
		require.NoError(t, f.s.persistHistoryPage(t.Context(), f.account, conversation(f), page(f, 300*24*time.Hour)))
		assert.False(t, f.optedOut(t))
	})
	t.Run("in the read before a send", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		*f.history = page(f, 300*24*time.Hour).Messages
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		assert.False(t, f.optedOut(t))
		assert.Equal(t, 1, *f.sends)
	})
	t.Run("inside the documented bound", func(t *testing.T) {
		f := newAutomationFixture(t)
		require.NoError(t, f.s.persistHistoryPage(t.Context(), f.account, conversation(f), page(f, 29*24*time.Hour)))
		assert.True(t, f.optedOut(t))
	})
	t.Run("past the bound", func(t *testing.T) {
		f := newAutomationFixture(t)
		require.NoError(t, f.s.persistHistoryPage(t.Context(), f.account, conversation(f), page(f, 31*24*time.Hour)))
		assert.False(t, f.optedOut(t))
	})
}

func TestPossibleOptOutHandsTheConversationToAPerson(t *testing.T) {
	f := newAutomationFixture(t)
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, AutomationSending, reply.State)
	f.seedPrivateReply(t, "sent", f.now.Add(-time.Hour))
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.ask", "수신거부 어떻게 하나요", *f.now)))
	assert.False(t, f.optedOut(t), "no permanent opt-out, so comment automation keeps working")
	assert.True(t, f.takenOver(t))
	state, err := f.s.ConversationAutomationState(t.Context(), f.actor, "workspace", "conversation")
	require.NoError(t, err)
	assert.Equal(t, StopReasonPossibleOptOut, state.StopReason)
	assert.Nil(t, state.LapsesAt, "a person has to decide, so it does not lapse")
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	assert.Zero(t, *f.sends, "nothing further is sent")
	assert.Equal(t, AutomationCancelled, f.stored(t, reply.MessageID).State)

	require.NoError(t, f.s.ResumeConversationAutomation(t.Context(), f.actor, "workspace", "conversation"))
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.ask", "수신거부 어떻게 하나요", *f.now)))
	assert.False(t, f.takenOver(t), "reading the same message again does not undo Resume")
}

func TestPossibleOptOutInOldHistoryIsNotAHandoff(t *testing.T) {
	f := newAutomationFixture(t)
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.old", "수신거부 어떻게 하나요", f.now.Add(-48*time.Hour))))
	assert.False(t, f.takenOver(t))
}

func TestOperatorRemovesAnOptOut(t *testing.T) {
	stop := func(t *testing.T, f automationFixture, id string, at time.Time) {
		t.Helper()
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent(id, "STOP", at)))
	}
	t.Run("a new refusal with no provider time is honored, and its redelivery stays removed", func(t *testing.T) {
		f := newAutomationFixture(t)
		stop(t, f, "mid.first", *f.now)
		*f.now = f.now.Add(time.Hour)
		require.NoError(t, f.s.RemoveConversationOptOut(t.Context(), f.actor, "workspace", "conversation"))
		*f.now = f.now.Add(time.Hour)
		stop(t, f, "mid.unknown", time.Time{})
		assert.True(t, f.optedOut(t), "wrongly honoring is the safe direction")
		*f.now = f.now.Add(time.Hour)
		require.NoError(t, f.s.RemoveConversationOptOut(t.Context(), f.actor, "workspace", "conversation"))
		*f.now = f.now.Add(time.Hour)
		stop(t, f, "mid.unknown", time.Time{})
		assert.False(t, f.optedOut(t), "first stored before the removal, so this is the old message")
	})
	t.Run("records who and when, and a redelivery stays removed", func(t *testing.T) {
		f := newAutomationFixture(t)
		stop(t, f, "mid.stop", *f.now)
		require.True(t, f.optedOut(t))
		state, err := f.s.ConversationAutomationState(t.Context(), f.actor, "workspace", "conversation")
		require.NoError(t, err)
		require.NotNil(t, state.OptedOutAt)
		*f.now = f.now.Add(time.Hour)
		require.NoError(t, f.s.RemoveConversationOptOut(t.Context(), f.actor, "workspace", "conversation"))
		assert.False(t, f.optedOut(t))
		state, err = f.s.ConversationAutomationState(t.Context(), f.actor, "workspace", "conversation")
		require.NoError(t, err)
		assert.Nil(t, state.OptedOutAt)
		require.NotNil(t, state.RemovedAt)
		assert.WithinDuration(t, *f.now, *state.RemovedAt, time.Second)
		assert.NotEmpty(t, state.RemovedByName)
		var removedBy string
		require.NoError(t, f.db.NewSelect().Table("automation_optout_removals").Column("removed_by").Scan(t.Context(), &removedBy))
		assert.Equal(t, f.actor.UserID, removedBy)
		stop(t, f, "mid.stop", f.now.Add(-time.Hour))
		assert.False(t, f.optedOut(t), "the provider delivering the old message again changes nothing")
	})
	t.Run("a newer refusal opts out again", func(t *testing.T) {
		f := newAutomationFixture(t)
		stop(t, f, "mid.stop", *f.now)
		require.NoError(t, f.s.RemoveConversationOptOut(t.Context(), f.actor, "workspace", "conversation"))
		*f.now = f.now.Add(time.Minute)
		stop(t, f, "mid.again", *f.now)
		assert.True(t, f.optedOut(t))
	})
	t.Run("automation can reach them again", func(t *testing.T) {
		f := newAutomationFixture(t)
		stop(t, f, "mid.stop", *f.now)
		require.NoError(t, f.s.RemoveConversationOptOut(t.Context(), f.actor, "workspace", "conversation"))
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		assert.Equal(t, AutomationSending, reply.State)
	})
	t.Run("requires Edit", func(t *testing.T) {
		f := newAutomationFixture(t)
		stop(t, f, "mid.stop", *f.now)
		_, err := f.db.NewUpdate().Model((*models.WorkspaceMember)(nil)).Set("role = ?", models.WorkspaceRoleViewer).Where("user_id = 'user'").Exec(t.Context())
		require.NoError(t, err)
		require.Error(t, f.s.RemoveConversationOptOut(t.Context(), f.actor, "workspace", "conversation"))
		assert.True(t, f.optedOut(t))
	})
}

func TestUnknownWriteThenOptOutIsUncertain(t *testing.T) {
	f := newAutomationFixture(t)
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	f.seedAttempt(t, "messaging:"+reply.MessageID, "job", "")
	_, err = f.db.NewRaw(`UPDATE provider_write_attempts SET status = 'sending', external_id = ''`).Exec(t.Context())
	require.NoError(t, err)
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.stop", "STOP", *f.now)))
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	assert.Zero(t, *f.sends, "an unknown write is never repeated")
	assert.Equal(t, "ambiguous", f.message(t, reply.MessageID).SendStatus)
	stored := f.stored(t, reply.MessageID)
	assert.Equal(t, AutomationUncertain, stored.State)
	assert.Equal(t, stopReasonUncertain, stored.Reason)
}

func TestEchoesOfAutomatedSendsAreNotPeople(t *testing.T) {
	send := func(t *testing.T, f automationFixture) {
		t.Helper()
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		require.NoError(t, f.runSendJob(t, reply.MessageID))
	}
	body := "Here is the guide"
	t.Run("twenty minutes late under another ID", func(t *testing.T) {
		f := newAutomationFixture(t)
		send(t, f)
		*f.now = f.now.Add(20 * time.Minute)
		var logs bytes.Buffer
		log.SetOutput(&logs)
		t.Cleanup(func() { log.SetOutput(os.Stderr) })
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.other", body, *f.now)))
		assert.False(t, f.takenOver(t))
		assert.Contains(t, logs.String(), "instagram echo id mismatch")
		assert.Contains(t, logs.String(), "total=1")
	})
	t.Run("from an uncertain receipt", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		_, err = f.db.NewUpdate().Model((*AutomationMessage)(nil)).Set("state = ?", AutomationUncertain).Where("id = ?", reply.ID).Exec(t.Context())
		require.NoError(t, err)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.other", body, f.now.Add(time.Minute))))
		assert.False(t, f.takenOver(t))
	})
	t.Run("every send under another ID leaves the conversation running", func(t *testing.T) {
		f := newAutomationFixture(t)
		send(t, f)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.other", body, *f.now)))
		*f.now = f.now.Add(time.Minute)
		f.receive(t, "later", *f.now)
		f.seedRun(t, "later-run", "workflow")
		next := f.request
		next.RunID, next.TriggerMessageID = "later-run", "later"
		reply, err := f.s.AdvanceAutomationReply(t.Context(), next)
		require.NoError(t, err)
		assert.Equal(t, AutomationSending, reply.State)
		assert.False(t, f.takenOver(t))
	})
	t.Run("one echo per send, so the same text again is a person", func(t *testing.T) {
		f := newAutomationFixture(t)
		send(t, f)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.other", body, *f.now)))
		require.False(t, f.takenOver(t))
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.typed", body, f.now.Add(time.Minute))))
		assert.True(t, f.takenOver(t))
	})
	t.Run("the same text after an hour is a person", func(t *testing.T) {
		f := newAutomationFixture(t)
		send(t, f)
		*f.now = f.now.Add(61 * time.Minute)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.typed", body, *f.now)))
		assert.True(t, f.takenOver(t))
	})
	t.Run("an echo before the send reports its ID is logged once it does", func(t *testing.T) {
		f := newAutomationFixture(t)
		reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.early", body, *f.now)))
		var logs bytes.Buffer
		log.SetOutput(&logs)
		t.Cleanup(func() { log.SetOutput(os.Stderr) })
		require.NoError(t, f.runSendJob(t, reply.MessageID))
		assert.False(t, f.takenOver(t))
		assert.Contains(t, logs.String(), "instagram echo id mismatch")
	})
	t.Run("the same ID proves the IDs match, so no message is matched by text", func(t *testing.T) {
		f := newAutomationFixture(t)
		send(t, f)
		var logs bytes.Buffer
		log.SetOutput(&logs)
		t.Cleanup(func() { log.SetOutput(os.Stderr) })
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("sent-remote-id", body, *f.now)))
		assert.False(t, f.takenOver(t))
		assert.NotContains(t, logs.String(), "instagram echo id mismatch")
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.typed", body, f.now.Add(time.Minute))))
		assert.True(t, f.takenOver(t))
	})
}

func TestTakeoverLapses(t *testing.T) {
	day := 24 * time.Hour
	state := func(t *testing.T, f automationFixture) ConversationAutomation {
		t.Helper()
		result, err := f.s.ConversationAutomationState(t.Context(), f.actor, "workspace", "conversation")
		require.NoError(t, err)
		return result
	}
	t.Run("seven days after a person's last message", func(t *testing.T) {
		f := newAutomationFixture(t)
		at := *f.now
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.human", "Hi", at)))
		current := state(t, f)
		require.NotNil(t, current.LapsesAt)
		assert.WithinDuration(t, at.Add(7*day), *current.LapsesAt, time.Second)

		*f.now = at.Add(3 * day)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.again", "Still me", *f.now)))
		assert.WithinDuration(t, at.Add(10*day), *state(t, f).LapsesAt, time.Second, "a further message moves it")

		*f.now = at.Add(9 * day)
		assert.NotNil(t, state(t, f).StoppedAt)
		*f.now = at.Add(10*day + time.Minute)
		current = state(t, f)
		assert.Nil(t, current.StoppedAt)
		require.NotNil(t, current.LapsedAt, "Inbox shows that it ended")
	})
	t.Run("a lapsed takeover lets automation answer", func(t *testing.T) {
		f := newAutomationFixture(t)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.human", "Hi", f.now.Add(-time.Minute))))
		*f.now = f.now.Add(8 * day)
		f.receive(t, "later", *f.now)
		_, err := f.db.NewUpdate().Model((*models.Conversation)(nil)).Set("messaging_window_expires_at = ?", f.now.Add(day)).Where("id = 'conversation'").Exec(t.Context())
		require.NoError(t, err)
		next := f.request
		next.TriggerMessageID = "later"
		reply, err := f.s.AdvanceAutomationReply(t.Context(), next)
		require.NoError(t, err)
		assert.Equal(t, AutomationSending, reply.State)
	})
	t.Run("a new message after it lapsed starts again", func(t *testing.T) {
		f := newAutomationFixture(t)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.human", "Hi", *f.now)))
		*f.now = f.now.Add(8 * day)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.again", "Back", *f.now)))
		assert.WithinDuration(t, f.now.Add(7*day), *state(t, f).LapsesAt, time.Second)
	})
	t.Run("a manual send lapses", func(t *testing.T) {
		f := newAutomationFixture(t)
		_, err := f.s.QueueMessage(t.Context(), f.actor, "conversation", "From Inbox")
		require.NoError(t, err)
		require.NotNil(t, state(t, f).LapsesAt)
	})
	t.Run("pressing take over stays, and it outranks an inferred one", func(t *testing.T) {
		f := newAutomationFixture(t)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.human", "Hi", *f.now)))
		require.NoError(t, f.s.StopConversationAutomation(t.Context(), f.actor, "workspace", "conversation"))
		assert.Nil(t, state(t, f).LapsesAt)
		require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.again", "Hi again", f.now.Add(time.Minute))))
		assert.Nil(t, state(t, f).LapsesAt)
		*f.now = f.now.Add(30 * day)
		assert.NotNil(t, state(t, f).StoppedAt)
	})
}

func TestEveryRefreshFailureHoldsTheSend(t *testing.T) {
	f := newAutomationFixture(t)
	reply, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	valid := f.s.tokens
	f.s.tokens = failingToken{}
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	assert.Zero(t, *f.sends, "no token means no read, and no read means no send")
	assert.Equal(t, "queued", f.message(t, reply.MessageID).SendStatus)
	queued, err := f.db.NewSelect().Model((*models.Job)(nil)).Where("payload LIKE ?", "%retry_at%").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, queued)

	f.s.tokens = valid
	require.NoError(t, f.runSendJob(t, reply.MessageID))
	assert.Equal(t, 1, *f.sends)
	assert.Equal(t, 1, *f.refreshes)
}

type failingToken struct{}

func (failingToken) GetValidAccessToken(context.Context, string) (string, error) {
	return "", errors.New("token store unavailable")
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

// windowedProvider reports the provider window the live path derives from a message.
type windowedProvider struct{ testProvider }

func (windowedProvider) MessagingSupport() platform.MessagingSupport {
	return platform.MessagingSupport{Enabled: true, CanSend: true, ReplyWindow: 24 * time.Hour}
}

func TestLateWebhookMessageOnAHistoryPageStaysNew(t *testing.T) {
	f := newAutomationFixture(t)
	f.s.SetProvider("instagram", windowedProvider{})
	known := f.now.Add(-48 * time.Hour)
	_, err := f.db.NewUpdate().Model((*models.Conversation)(nil)).Set("last_message_at = ?", known).Set("messaging_window_expires_at = ?", known.Add(24*time.Hour)).Where("id = 'conversation'").Exec(t.Context())
	require.NoError(t, err)
	var before models.Conversation
	require.NoError(t, f.db.NewSelect().Model(&before).Where("id = 'conversation'").Scan(t.Context()))
	at := f.now.Add(-time.Minute)
	arrived := platform.ProviderMessage{ID: "mid.arrived", Direction: "inbound", AuthorRemoteID: "reader", Body: "guide", RemoteCreatedAt: at}
	older := platform.ProviderMessage{ID: "mid.older", Direction: "inbound", AuthorRemoteID: "reader", Body: "guide", RemoteCreatedAt: known.Add(-time.Hour)}
	require.NoError(t, f.s.persistHistoryPage(t.Context(), f.account, before, platform.FetchMessageHistoryResult{Messages: []platform.ProviderMessage{arrived, older}}))
	imported := func() []string {
		ids := []string{}
		require.NoError(t, f.db.NewSelect().TableExpr("message_history_imports AS h").Join("JOIN direct_messages AS d ON d.id = h.message_id").ColumnExpr("d.remote_message_id").Scan(t.Context(), &ids))
		return ids
	}
	assert.Equal(t, []string{"mid.older"}, imported(), "only what the conversation already passed is history")
	var after models.Conversation
	require.NoError(t, f.db.NewSelect().Model(&after).Where("id = 'conversation'").Scan(t.Context()))
	assert.True(t, after.LastMessageAt.Equal(at))
	assert.True(t, after.MessagingWindowExpiresAt.Equal(at.Add(24*time.Hour)), "a closed window would consume the message with no run")
	assert.Equal(t, before.UnreadCount+1, after.UnreadCount)
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.arrived", "guide", at)))
	assert.Equal(t, []string{"mid.older"}, imported())
	require.NoError(t, f.db.NewSelect().Model(&after).Where("id = 'conversation'").Scan(t.Context()))
	assert.Equal(t, before.UnreadCount+1, after.UnreadCount, "the late webhook does not count it twice")
}

func TestHistoryIsNotNewWithNoKnownLastMessage(t *testing.T) {
	f := newAutomationFixture(t)
	_, err := f.db.NewUpdate().Model((*models.Conversation)(nil)).Set("last_message_at = ?", time.Time{}).Where("id = 'conversation'").Exec(t.Context())
	require.NoError(t, err)
	var conversation models.Conversation
	require.NoError(t, f.db.NewSelect().Model(&conversation).Where("id = 'conversation'").Scan(t.Context()))
	require.True(t, conversation.LastMessageAt.IsZero())
	require.NoError(t, f.s.persistHistoryPage(t.Context(), f.account, conversation, platform.FetchMessageHistoryResult{Messages: []platform.ProviderMessage{{ID: "mid.unknown", Direction: "inbound", AuthorRemoteID: "reader", Body: "guide", RemoteCreatedAt: *f.now}}}))
	marks, err := f.db.NewSelect().Table("message_history_imports").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, marks)
}

func TestPrivateReplyEchoByIDSpendsItsTextMatch(t *testing.T) {
	f := newAutomationFixture(t)
	f.seedPrivateReply(t, "sent", *f.now)
	f.seedAttempt(t, "engagement:private", "private-job", "mid.private")
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("mid.private", "Your guide", f.now.Add(time.Second))))
	require.False(t, f.takenOver(t), "the echo of the reply is not a person")
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", outboundEvent("person-copy", "Your guide", f.now.Add(2*time.Second))))
	assert.True(t, f.takenOver(t), "a person repeating the text after its echo is a person")
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
		return platform.ProviderMessage{ID: id, Direction: "inbound", AuthorRemoteID: "reader", Body: "guide", RemoteCreatedAt: f.now.Add(-time.Hour)}
	}
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.live", "guide", f.now.Add(-time.Hour))))
	page := platform.FetchMessageHistoryResult{Messages: []platform.ProviderMessage{inbound("mid.live"), inbound("mid.history")}}
	require.NoError(t, f.s.persistHistoryPage(t.Context(), f.account, conversation, page))
	require.Equal(t, []string{"mid.history"}, imported(), "a message polling stored first stays eligible")
	require.NoError(t, f.s.ApplyProviderEvents(t.Context(), "account", inboundEvent("mid.history", "guide", *f.now)))
	require.Equal(t, []string{"mid.history"}, imported(), "a later delivery does not make history new")
}
