package messaging

import (
	"context"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/providerwrite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (f automationFixture) sequenceRequest() SequenceRequest {
	return SequenceRequest{Authority: f.request.Authority, WorkflowID: "workflow", RunID: "run", StepID: "sequence", TriggerMessageID: "trigger", Messages: []SequenceMessage{{Text: "First"}, {Text: "Second", DelayMinutes: 30}}, Delivery: "automatic"}
}

func (f automationFixture) advanceSequence(t *testing.T, in SequenceRequest) SequenceProgress {
	t.Helper()
	progress, err := f.s.AdvanceMessageSequence(t.Context(), in)
	require.NoError(t, err)
	return progress
}

// deliverFirst leaves the first message delivered and the second one scheduled.
func (f automationFixture) deliverFirst(t *testing.T, in SequenceRequest) SequenceProgress {
	t.Helper()
	first := f.advanceSequence(t, in)
	require.Equal(t, AutomationSending, first.State)
	require.NoError(t, f.runSendJob(t, first.Reply.MessageID))
	second := f.advanceSequence(t, in)
	require.Equal(t, AutomationScheduled, second.State)
	require.Equal(t, 1, *f.sends)
	return second
}

func (f automationFixture) commentSequenceRequest(t *testing.T) SequenceRequest {
	t.Helper()
	repliedAt := f.now.Add(-time.Hour)
	item := models.EngagementItem{ID: "comment", WorkspaceID: "workspace", SocialAccountID: "account", RemoteID: "remote-comment", Body: "guide", AuthorRemoteID: "reader", Platform: "instagram", RemoteCreatedAt: repliedAt, CreatedAt: repliedAt, UpdatedAt: repliedAt}
	_, err := f.db.NewInsert().Model(&item).Exec(t.Context())
	require.NoError(t, err)
	_, err = f.db.NewDelete().Model((*models.DirectMessage)(nil)).Where("id = 'trigger'").Exec(t.Context())
	require.NoError(t, err)
	in := f.sequenceRequest()
	in.TriggerMessageID = ""
	in.Comment = &CommentSequenceAnchor{ItemID: "comment", AccountID: "account", AuthorRemoteID: "reader", RepliedAt: repliedAt, RespondBy: f.now.Add(6 * 24 * time.Hour)}
	return in
}

func (f automationFixture) replyRows(t *testing.T) []AutomationMessage {
	t.Helper()
	var rows []AutomationMessage
	require.NoError(t, f.db.NewSelect().Model(&rows).Order("sequence_position ASC").Scan(t.Context()))
	return rows
}

func (f automationFixture) closeWindow(t *testing.T) {
	t.Helper()
	_, err := f.db.NewUpdate().Model((*models.Conversation)(nil)).Set("messaging_window_expires_at = ?", f.now.Add(-time.Minute)).Where("id = 'conversation'").Exec(t.Context())
	require.NoError(t, err)
}

func TestSequenceDeliversInOrderAndRepeatsNothing(t *testing.T) {
	f := newAutomationFixture(t)
	in := f.sequenceRequest()
	second := f.deliverFirst(t, in)
	assert.Equal(t, 1, second.Sent)
	assert.Equal(t, f.replyRows(t)[0].UpdatedAt.Add(30*time.Minute), second.WakeAt)

	replay := f.advanceSequence(t, in)
	assert.Equal(t, second.Reply.ID, replay.Reply.ID, "a replay before the delay keeps the same receipt")
	assert.Empty(t, replay.Reply.MessageID)

	*f.now = f.now.Add(30 * time.Minute)
	sending := f.advanceSequence(t, in)
	require.Equal(t, AutomationSending, sending.State)
	assert.Equal(t, sending.Reply.MessageID, f.advanceSequence(t, in).Reply.MessageID, "a retry while sending queues nothing new")
	require.NoError(t, f.runSendJob(t, sending.Reply.MessageID))

	done := f.advanceSequence(t, in)
	assert.Equal(t, SequenceComplete, done.State)
	assert.Equal(t, 2, done.Sent)
	assert.Equal(t, SequenceComplete, f.advanceSequence(t, in).State)
	assert.Equal(t, 2, *f.sends)
	assert.Len(t, f.replyRows(t), 2)
}

func TestSequenceStopsWhenTheWindowExpiredAtDispatch(t *testing.T) {
	t.Run("the stored window closes during a delay", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.sequenceRequest()
		f.deliverFirst(t, in)
		*f.now = f.now.Add(30 * time.Minute)
		f.closeWindow(t)
		stopped := f.advanceSequence(t, in)
		assert.Equal(t, SequenceStopped, stopped.State)
		assert.Equal(t, stopReasonWindow, stopped.Reason)
		assert.Equal(t, 1, *f.sends)
	})
	t.Run("a queued follow-up cannot pass the send fence", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.sequenceRequest()
		f.deliverFirst(t, in)
		*f.now = f.now.Add(30 * time.Minute)
		sending := f.advanceSequence(t, in)
		require.Equal(t, AutomationSending, sending.State)
		f.closeWindow(t)
		require.NoError(t, f.runSendJob(t, sending.Reply.MessageID))
		assert.Equal(t, 1, *f.sends)
		assert.Equal(t, stopReasonWindow, f.advanceSequence(t, in).Reason)
	})
	t.Run("a later message does not extend the response's 24 hours", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.sequenceRequest()
		f.deliverFirst(t, in)
		*f.now = f.now.Add(24 * time.Hour)
		_, err := f.db.NewUpdate().Model((*models.Conversation)(nil)).Set("messaging_window_expires_at = ?", f.now.Add(23*time.Hour)).Where("id = 'conversation'").Exec(t.Context())
		require.NoError(t, err)
		stopped := f.advanceSequence(t, in)
		assert.Equal(t, stopReasonWindow, stopped.Reason)
		assert.Equal(t, 1, *f.sends)
	})
	t.Run("a delay past the deadline never schedules a send", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.sequenceRequest()
		in.Messages[1].DelayMinutes = 24 * 60
		first := f.advanceSequence(t, in)
		require.NoError(t, f.runSendJob(t, first.Reply.MessageID))
		stopped := f.advanceSequence(t, in)
		assert.Equal(t, SequenceStopped, stopped.State)
		assert.Equal(t, stopReasonWindow, stopped.Reason)
	})
}

func TestSequenceStopsForAnOptOutBetweenMessages(t *testing.T) {
	f := newAutomationFixture(t)
	in := f.sequenceRequest()
	f.deliverFirst(t, in)
	f.optOut(t)
	*f.now = f.now.Add(30 * time.Minute)
	stopped := f.advanceSequence(t, in)
	assert.Equal(t, SequenceStopped, stopped.State)
	assert.Equal(t, stopReasonOptedOut, stopped.Reason)
	assert.Equal(t, 1, *f.sends)
}

func TestSequenceStopsForATakeoverDuringADelay(t *testing.T) {
	f := newAutomationFixture(t)
	in := f.sequenceRequest()
	f.deliverFirst(t, in)
	require.NoError(t, f.s.StopConversationAutomation(t.Context(), f.actor, "workspace", "conversation"))
	assert.Equal(t, AutomationCancelled, f.replyRows(t)[1].State, "takeover settles a scheduled follow-up immediately")
	*f.now = f.now.Add(30 * time.Minute)
	stopped := f.advanceSequence(t, in)
	assert.Equal(t, SequenceStopped, stopped.State)
	assert.Equal(t, stopReasonTakeover, stopped.Reason)
	assert.Equal(t, 1, *f.sends)
}

func TestSequenceNeverContinuesPastAnUncertainDelivery(t *testing.T) {
	f := newAutomationFixture(t)
	*f.sendFunc = func(context.Context) (platform.SendMessageResult, error) {
		return platform.SendMessageResult{}, context.DeadlineExceeded
	}
	in := f.sequenceRequest()
	first := f.advanceSequence(t, in)
	err := f.runSendJob(t, first.Reply.MessageID)
	require.Error(t, err)
	require.True(t, providerwrite.IsAmbiguous(err))
	stopped := f.advanceSequence(t, in)
	assert.Equal(t, SequenceStopped, stopped.State)
	assert.Equal(t, 0, stopped.Sent)
	assert.NotEmpty(t, stopped.Reason)
	require.NoError(t, f.runSendJob(t, first.Reply.MessageID))
	assert.Equal(t, 1, *f.sends)
	assert.Len(t, f.replyRows(t), 1)
}

func TestSequenceReviewsEachPositionSeparately(t *testing.T) {
	f := newAutomationFixture(t)
	in := f.sequenceRequest()
	in.Delivery = "review"
	parked := f.advanceSequence(t, in)
	require.Equal(t, AutomationAwaitingReview, parked.State)
	require.Zero(t, *f.sends)

	approval := in
	approval.Approve, approval.ApprovedBody = true, "Edited first"
	assert.Equal(t, AutomationReady, f.advanceSequence(t, approval).State)
	sending := f.advanceSequence(t, in)
	require.Equal(t, AutomationSending, sending.State)
	require.NoError(t, f.runSendJob(t, sending.Reply.MessageID))
	assert.Equal(t, "Edited first", f.replyRows(t)[0].Body)

	assert.Equal(t, AutomationScheduled, f.advanceSequence(t, in).State)
	*f.now = f.now.Add(30 * time.Minute)
	next := f.advanceSequence(t, in)
	require.Equal(t, AutomationAwaitingReview, next.State)
	assert.Equal(t, "Second", next.Reply.Body, "an earlier edit stays with its own position")

	approval.ApprovedBody = ""
	assert.Equal(t, AutomationReady, f.advanceSequence(t, approval).State, "approving without text keeps the stored message")
	assert.Equal(t, "Second", f.replyRows(t)[1].Body)
}

func TestSequenceClaimsTheTriggerAgainstOtherOwners(t *testing.T) {
	f := newAutomationFixture(t)
	f.seedRun(t, "second-run", "second-workflow")
	_, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
	require.NoError(t, err)
	in := f.sequenceRequest()
	in.RunID, in.WorkflowID = "second-run", "second-workflow"
	skipped := f.advanceSequence(t, in)
	assert.Equal(t, AutomationSkipped, skipped.State)
	assert.Equal(t, stopReasonOwned, skipped.Reason)
	assert.Empty(t, skipped.Reply.ID)
	assert.Len(t, f.replyRows(t), 1)
}

func TestSequenceMessagesAllCountTowardTheHourlyLimit(t *testing.T) {
	t.Run("one full sequence fits", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.sequenceRequest()
		in.Messages = []SequenceMessage{{Text: "1"}, {Text: "2"}, {Text: "3"}, {Text: "4"}, {Text: "5"}}
		for range in.Messages {
			progress := f.advanceSequence(t, in)
			require.Equal(t, AutomationSending, progress.State)
			require.NoError(t, f.runSendJob(t, progress.Reply.MessageID))
		}
		assert.Equal(t, 5, *f.sends)
		assert.Equal(t, SequenceComplete, f.advanceSequence(t, in).State)
	})
	t.Run("an earlier reply leaves room for fewer", func(t *testing.T) {
		f := newAutomationFixture(t)
		earlier, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		require.NoError(t, f.runSendJob(t, earlier.MessageID))
		f.receive(t, "second", *f.now)
		f.seedRun(t, "second-run", "workflow")
		in := f.sequenceRequest()
		in.RunID, in.TriggerMessageID = "second-run", "second"
		in.Messages = []SequenceMessage{{Text: "1"}, {Text: "2"}, {Text: "3"}, {Text: "4"}, {Text: "5"}}
		var last SequenceProgress
		for range in.Messages {
			last = f.advanceSequence(t, in)
			if last.State != AutomationSending {
				break
			}
			require.NoError(t, f.runSendJob(t, last.Reply.MessageID))
		}
		assert.Equal(t, SequenceStopped, last.State)
		assert.Equal(t, stopReasonLoop, last.Reason)
		assert.Equal(t, 5, *f.sends, "one reply and four follow-ups fill the hour")
	})
}

func TestOneSequencePerConversation(t *testing.T) {
	t.Run("a later trigger records an outcome and sends nothing", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.sequenceRequest()
		f.deliverFirst(t, in)
		f.receive(t, "later", *f.now)
		f.seedRun(t, "second-run", "second-workflow")
		second := f.sequenceRequest()
		second.RunID, second.WorkflowID, second.TriggerMessageID = "second-run", "second-workflow", "later"
		skipped := f.advanceSequence(t, second)
		assert.Equal(t, AutomationSkipped, skipped.State)
		assert.Equal(t, StopReasonSequenceActive, skipped.Reason)
		assert.NotEmpty(t, skipped.Reply.ID, "the receipt shows why this message was not answered")
		assert.Equal(t, 1, *f.sends)
	})
	t.Run("a plain reply waits its turn too", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.deliverFirst(t, f.sequenceRequest())
		f.receive(t, "later", *f.now)
		f.seedRun(t, "reply-run", "workflow")
		reply := f.request
		reply.RunID, reply.TriggerMessageID = "reply-run", "later"
		stored, err := f.s.AdvanceAutomationReply(t.Context(), reply)
		require.NoError(t, err)
		assert.Equal(t, StopReasonSequenceActive, stored.Reason)
		assert.Equal(t, 1, *f.sends)
	})
	t.Run("a finished sequence makes room", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.sequenceRequest()
		in.Messages = in.Messages[:1]
		first := f.advanceSequence(t, in)
		require.NoError(t, f.runSendJob(t, first.Reply.MessageID))
		require.Equal(t, SequenceComplete, f.advanceSequence(t, in).State)
		f.receive(t, "later", *f.now)
		f.seedRun(t, "second-run", "workflow")
		second := f.sequenceRequest()
		second.RunID, second.TriggerMessageID = "second-run", "later"
		assert.Equal(t, AutomationSending, f.advanceSequence(t, second).State)
	})
	t.Run("a stopped run makes room", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.deliverFirst(t, f.sequenceRequest())
		f.setRunState(t, "failed")
		f.receive(t, "later", *f.now)
		f.seedRun(t, "second-run", "workflow")
		second := f.sequenceRequest()
		second.RunID, second.TriggerMessageID = "second-run", "later"
		assert.Equal(t, AutomationSending, f.advanceSequence(t, second).State)
	})
	t.Run("two comment campaigns cannot both claim one DM", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.commentSequenceRequest(t)
		f.receive(t, "response", f.now.Add(-time.Minute))
		first := f.advanceSequence(t, in)
		require.Equal(t, AutomationSending, first.State)
		f.seedRun(t, "second-run", "second-workflow")
		second := in
		second.RunID, second.WorkflowID = "second-run", "second-workflow"
		skipped := f.advanceSequence(t, second)
		assert.Equal(t, AutomationSkipped, skipped.State)
		assert.Equal(t, stopReasonOwned, skipped.Reason)
		assert.Len(t, f.replyRows(t), 1)
	})
}

func TestCommentFollowUpWaitsForAQualifyingResponse(t *testing.T) {
	t.Run("nothing is sent without a response", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.commentSequenceRequest(t)
		waiting := f.advanceSequence(t, in)
		assert.Equal(t, SequenceWaitingResponse, waiting.State)
		assert.Equal(t, in.Comment.RespondBy, waiting.WakeAt, "a batched check wakes the run sooner when something happens")
		assert.Empty(t, f.replyRows(t))
		assert.Zero(t, *f.sends)
		parked, err := f.db.NewSelect().Table("message_sequence_waits").Where("run_id = 'run'").Count(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, parked)
	})
	t.Run("a message from before the private reply does not qualify", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.commentSequenceRequest(t)
		f.receive(t, "older", in.Comment.RepliedAt.Add(-time.Minute))
		assert.Equal(t, SequenceWaitingResponse, f.advanceSequence(t, in).State)
		assert.Empty(t, f.replyRows(t))
	})
	t.Run("an outbound echo does not qualify", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.commentSequenceRequest(t)
		f.receive(t, "echo", in.Comment.RepliedAt.Add(time.Minute))
		_, err := f.db.NewUpdate().Model((*models.DirectMessage)(nil)).Set("direction = 'outbound'").Where("id = 'echo'").Exec(t.Context())
		require.NoError(t, err)
		assert.Equal(t, SequenceWaitingResponse, f.advanceSequence(t, in).State)
	})
	t.Run("someone else's message does not qualify", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.commentSequenceRequest(t)
		in.Comment.AuthorRemoteID = "another-reader"
		f.receive(t, "other", in.Comment.RepliedAt.Add(time.Minute))
		assert.Equal(t, SequenceWaitingResponse, f.advanceSequence(t, in).State)
	})
	t.Run("a response another automation answered ends the wait", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.commentSequenceRequest(t)
		f.receive(t, "trigger", in.Comment.RepliedAt.Add(time.Minute))
		_, err := f.s.AdvanceAutomationReply(t.Context(), f.request)
		require.NoError(t, err)
		skipped := f.advanceSequence(t, in)
		assert.Equal(t, AutomationSkipped, skipped.State)
		assert.Equal(t, stopReasonOwned, skipped.Reason)
		assert.Len(t, f.replyRows(t), 1)
	})
	t.Run("the wait ends without sending", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.commentSequenceRequest(t)
		*f.now = in.Comment.RespondBy
		f.receive(t, "late", in.Comment.RespondBy.Add(time.Minute))
		stopped := f.advanceSequence(t, in)
		assert.Equal(t, SequenceNoResponse, stopped.State)
		assert.Equal(t, stopReasonNoResponse, stopped.Reason)
		assert.Empty(t, f.replyRows(t))
		assert.Zero(t, *f.sends)
	})
}

func TestCommentFollowUpStartsFromTheResponse(t *testing.T) {
	f := newAutomationFixture(t)
	in := f.commentSequenceRequest(t)
	responseAt := f.now.Add(-time.Minute)
	f.receive(t, "response", responseAt)
	f.receive(t, "later", f.now.Add(-time.Second))
	first := f.advanceSequence(t, in)
	require.Equal(t, AutomationSending, first.State)
	row := f.replyRows(t)[0]
	assert.Equal(t, "response", row.TriggerMessageID, "the earliest response is the one the sequence answers")
	assert.Equal(t, "comment", row.CommentItemID)
	assert.Equal(t, 2, row.SequenceLength)
	require.NoError(t, f.runSendJob(t, first.Reply.MessageID))
	assert.Equal(t, 1, *f.sends)

	*f.now = responseAt.Add(24 * time.Hour)
	assert.Equal(t, stopReasonWindow, f.advanceSequence(t, in).Reason, "24 hours count from that response")
}

func TestCommentFollowUpStopsBeforeAnyResponse(t *testing.T) {
	t.Run("opt-out", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.commentSequenceRequest(t)
		f.optOut(t)
		stopped := f.advanceSequence(t, in)
		assert.Equal(t, SequenceStopped, stopped.State)
		assert.Equal(t, stopReasonOptedOut, stopped.Reason)
	})
	t.Run("comment takeover", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.commentSequenceRequest(t)
		_, err := f.db.NewUpdate().Model((*models.EngagementItem)(nil)).Set("automation_stopped_at = ?", *f.now).Where("id = 'comment'").Exec(t.Context())
		require.NoError(t, err)
		stopped := f.advanceSequence(t, in)
		assert.Equal(t, SequenceStopped, stopped.State)
		assert.Equal(t, stopReasonComment, stopped.Reason)
	})
	t.Run("a person wrote in the thread after the private reply", func(t *testing.T) {
		f := newAutomationFixture(t)
		in := f.commentSequenceRequest(t)
		f.receive(t, "response", f.now.Add(-time.Minute))
		f.receive(t, "human", f.now.Add(-30*time.Minute))
		_, err := f.db.NewUpdate().Model((*models.DirectMessage)(nil)).Set("direction = 'outbound', body = 'hand written'").Where("id = 'human'").Exec(t.Context())
		require.NoError(t, err)
		stopped := f.advanceSequence(t, in)
		assert.Equal(t, SequenceStopped, stopped.State)
		assert.Equal(t, stopReasonHuman, stopped.Reason)
		assert.Zero(t, *f.sends)
		state, err := f.s.ConversationAutomationState(t.Context(), f.actor, "workspace", "conversation")
		require.NoError(t, err)
		assert.NotNil(t, state.StoppedAt)
	})
}

func TestCommentTakeoverStopsAQueuedFollowUpInTheSendFence(t *testing.T) {
	f := newAutomationFixture(t)
	in := f.commentSequenceRequest(t)
	f.receive(t, "response", f.now.Add(-time.Minute))
	first := f.advanceSequence(t, in)
	require.Equal(t, AutomationSending, first.State)
	_, err := f.db.NewUpdate().Model((*models.EngagementItem)(nil)).Set("automation_stopped_at = ?", *f.now).Where("id = 'comment'").Exec(t.Context())
	require.NoError(t, err)
	require.NoError(t, f.runSendJob(t, first.Reply.MessageID))
	assert.Zero(t, *f.sends)
	stopped := f.advanceSequence(t, in)
	assert.Equal(t, SequenceStopped, stopped.State)
	assert.Equal(t, stopReasonComment, stopped.Reason)
}
