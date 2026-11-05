package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

type messageSourceFixture struct {
	s     *Service
	actor workspaceaccess.ActorFacts
	now   time.Time
}

func newMessageSourceFixture(t *testing.T, backend string) *messageSourceFixture {
	t.Helper()
	s, actor := workflowTestServiceWithDB(t, nil, workflowRunListTestDB(t, backend))
	now := time.Now().UTC()
	for _, account := range []models.SocialAccount{
		{ID: "ig", WorkspaceID: "ws", Platform: "instagram", AccountID: "owner", Slug: "ig", AccessTokenEnc: []byte("test"), IsActive: true, CreatedAt: now},
		{ID: "ig-other", WorkspaceID: "ws", Platform: "instagram", AccountID: "owner-2", Slug: "ig-other", AccessTokenEnc: []byte("test"), IsActive: true, CreatedAt: now},
		{ID: "bsky", WorkspaceID: "ws", Platform: "bluesky", AccountID: "did:plc:self", Slug: "bsky", AccessTokenEnc: []byte("test"), IsActive: true, CreatedAt: now},
	} {
		_, err := s.db.NewInsert().Model(&account).Exec(t.Context())
		require.NoError(t, err)
	}
	for _, conversation := range []models.Conversation{
		{ID: "c-reader", WorkspaceID: "ws", SocialAccountID: "ig", Platform: "instagram", RemoteConversationID: "t-reader", CounterpartRemoteID: "reader", CounterpartName: "Sample reader", MessagingWindowExpiresAt: now.Add(24 * time.Hour), CreatedAt: now, UpdatedAt: now},
		{ID: "c-closed", WorkspaceID: "ws", SocialAccountID: "ig", Platform: "instagram", RemoteConversationID: "t-closed", CounterpartRemoteID: "late-reader", MessagingWindowExpiresAt: now.Add(-time.Minute), CreatedAt: now, UpdatedAt: now},
		{ID: "c-own-account", WorkspaceID: "ws", SocialAccountID: "ig", Platform: "instagram", RemoteConversationID: "t-own", CounterpartRemoteID: "owner-2", MessagingWindowExpiresAt: now.Add(24 * time.Hour), CreatedAt: now, UpdatedAt: now},
		{ID: "c-bsky", WorkspaceID: "ws", SocialAccountID: "bsky", Platform: "bluesky", RemoteConversationID: "t-bsky", CounterpartRemoteID: "reader", MessagingWindowExpiresAt: now.Add(24 * time.Hour), CreatedAt: now, UpdatedAt: now},
	} {
		_, err := s.db.NewInsert().Model(&conversation).Exec(t.Context())
		require.NoError(t, err)
	}
	return &messageSourceFixture{s: s, actor: actor, now: now}
}

func (f *messageSourceFixture) publish(t *testing.T, source Source, steps []Step) Workflow {
	t.Helper()
	if steps == nil {
		steps = []Step{{ID: "wait", Kind: KindWait, Inputs: map[string]Value{"minutes": literal(0)}}}
	}
	workflow, err := f.s.Save(t.Context(), f.actor, "ws", "", SaveRequest{Name: "DM campaign", Definition: Definition{Schema: 1, Source: source, Steps: steps}})
	require.NoError(t, err)
	workflow, err = f.s.Publish(t.Context(), f.actor, "ws", workflow.ID, workflow.Revision)
	require.NoError(t, err)
	return workflow
}

func (f *messageSourceFixture) message(t *testing.T, row models.DirectMessage) {
	t.Helper()
	row.WorkspaceID = "ws"
	if row.ConversationID == "" {
		row.ConversationID = "c-reader"
	}
	if row.Direction == "" {
		row.Direction = "inbound"
	}
	if row.RemoteMessageID == "" {
		row.RemoteMessageID = "mid." + row.ID
	}
	if row.RemoteCreatedAt.IsZero() {
		row.RemoteCreatedAt = time.Now().UTC().Add(time.Minute)
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC().Add(time.Minute)
	}
	row.UpdatedAt = row.CreatedAt
	row.AttachmentsJSON = "[]"
	_, err := f.s.db.NewInsert().Model(&row).Exec(t.Context())
	require.NoError(t, err)
}

func (f *messageSourceFixture) runSources(t *testing.T, workflowID string) []string {
	t.Helper()
	runs, err := f.s.Runs(t.Context(), f.actor, "ws", workflowID)
	require.NoError(t, err)
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		id, _ := run.Source["message_id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func messageSource(keywords ...string) Source {
	return Source{Kind: "message_received", AccountIDs: []string{"ig"}, Keywords: keywords, MatchMode: "word", DailyLimit: 20}
}

func TestMessageSourceAdmitsOnlyNewInboundMessagesOnce(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := newMessageSourceFixture(t, backend)
			workflow := f.publish(t, messageSource("GUIDE"), nil)
			old := time.Now().UTC().Add(-time.Hour)
			f.message(t, models.DirectMessage{ID: "match", AuthorRemoteID: "reader", Body: "guide please!"})
			f.message(t, models.DirectMessage{ID: "substring", AuthorRemoteID: "reader", Body: "guidelines"})
			// History imported after activation keeps its provider creation time.
			f.message(t, models.DirectMessage{ID: "history", AuthorRemoteID: "reader", Body: "guide", RemoteCreatedAt: old})
			f.message(t, models.DirectMessage{ID: "pre-activation", AuthorRemoteID: "reader", Body: "guide", RemoteCreatedAt: old, CreatedAt: old})
			// An echo, and the account's own message delivered as inbound.
			f.message(t, models.DirectMessage{ID: "echo", Direction: "outbound", AuthorRemoteID: "owner", Body: "guide"})
			f.message(t, models.DirectMessage{ID: "own", AuthorRemoteID: "owner", Body: "guide"})
			f.message(t, models.DirectMessage{ID: "outbound-no-author", Direction: "outbound", Body: "guide"})
			f.message(t, models.DirectMessage{ID: "other-connected-account", ConversationID: "c-own-account", AuthorRemoteID: "owner-2", Body: "guide"})
			f.message(t, models.DirectMessage{ID: "other-platform", ConversationID: "c-bsky", AuthorRemoteID: "reader", Body: "guide"})
			f.message(t, models.DirectMessage{ID: "closed-window", ConversationID: "c-closed", AuthorRemoteID: "late-reader", Body: "guide"})
			f.message(t, models.DirectMessage{ID: "deleted", AuthorRemoteID: "reader", Body: "guide", DeletedAt: time.Now().UTC()})

			require.NoError(t, f.s.poll(t.Context(), workflow.ID))
			// A redelivered webhook converges on the stored row; a repeat poll is a no-op.
			require.NoError(t, f.s.poll(t.Context(), workflow.ID))
			assert.Equal(t, []string{"match"}, f.runSources(t, workflow.ID))
			runs, err := f.s.Runs(t.Context(), f.actor, "ws", workflow.ID)
			require.NoError(t, err)
			require.Len(t, runs, 1)
			assert.Equal(t, "c-reader", runs[0].Source["conversation_id"])
			assert.Equal(t, "ig", runs[0].Source["account_id"])
			assert.Equal(t, "Sample reader", runs[0].Source["author_name"])
		})
	}
}

func TestMessageSourceSkipsOptedOutSenders(t *testing.T) {
	f := newMessageSourceFixture(t, "sqlite")
	workflow := f.publish(t, messageSource(), nil)
	_, err := f.s.db.NewRaw(`INSERT INTO engagement_automation_optouts(social_account_id,author_remote_id,created_at) VALUES('ig','reader',?)`, f.now).Exec(t.Context())
	require.NoError(t, err)
	f.message(t, models.DirectMessage{ID: "opted-out", AuthorRemoteID: "reader", Body: "hello"})
	require.NoError(t, f.s.poll(t.Context(), workflow.ID))
	assert.Empty(t, f.runSources(t, workflow.ID))
}

func TestMessageDailyLimitLeavesLaterMessagesUnadmitted(t *testing.T) {
	f := newMessageSourceFixture(t, "sqlite")
	source := messageSource()
	source.DailyLimit = 1
	workflow := f.publish(t, source, nil)
	f.message(t, models.DirectMessage{ID: "first", AuthorRemoteID: "reader", Body: "one", CreatedAt: time.Now().UTC().Add(time.Minute)})
	f.message(t, models.DirectMessage{ID: "second", AuthorRemoteID: "reader", Body: "two", CreatedAt: time.Now().UTC().Add(2 * time.Minute)})
	require.NoError(t, f.s.poll(t.Context(), workflow.ID))
	assert.Equal(t, []string{"first"}, f.runSources(t, workflow.ID))
}

func TestOverlappingMessageWorkflowsAnswerOnceByPriority(t *testing.T) {
	f := newMessageSourceFixture(t, "sqlite")
	reply := func() []Step {
		return []Step{{ID: "reply", Kind: KindMessageReply, Name: "Reply", Inputs: map[string]Value{"message_id": reference("source.message_id"), "text": literal("Hello"), "delivery": literal("automatic")}}}
	}
	general := messageSource()
	general.Priority = 50
	specific := messageSource("guide")
	specific.Priority = 10
	broad := f.publish(t, general, reply())
	narrow := f.publish(t, specific, reply())
	observer := f.publish(t, messageSource(), nil)
	f.message(t, models.DirectMessage{ID: "both", AuthorRemoteID: "reader", Body: "guide"})
	for _, id := range []string{broad.ID, narrow.ID, observer.ID} {
		require.NoError(t, f.s.poll(t.Context(), id))
	}
	assert.Empty(t, f.runSources(t, broad.ID))
	assert.Equal(t, []string{"both"}, f.runSources(t, narrow.ID))
	assert.Equal(t, []string{"both"}, f.runSources(t, observer.ID), "a workflow that sends nothing stays independent")
}

func TestPausedMessageWorkflowRestartsAtReactivation(t *testing.T) {
	f := newMessageSourceFixture(t, "sqlite")
	workflow := f.publish(t, messageSource(), nil)
	paused, err := f.s.Pause(t.Context(), f.actor, "ws", workflow.ID, workflow.Revision)
	require.NoError(t, err)
	f.message(t, models.DirectMessage{ID: "while-paused", AuthorRemoteID: "reader", Body: "hello", RemoteCreatedAt: time.Now().UTC(), CreatedAt: time.Now().UTC()})
	time.Sleep(5 * time.Millisecond)
	resumed, err := f.s.Publish(t.Context(), f.actor, "ws", paused.ID, paused.Revision)
	require.NoError(t, err)
	require.NoError(t, f.s.poll(t.Context(), resumed.ID))
	assert.Empty(t, f.runSources(t, resumed.ID))
}

func replyStep() Step {
	return Step{ID: "reply", Kind: KindMessageReply, Name: "Reply", Inputs: map[string]Value{"message_id": reference("source.message_id"), "text": literal("Hello"), "delivery": literal("review")}}
}

func waitStep(id string, minutes any) Step {
	return Step{ID: id, Kind: KindWait, Name: "Wait", Inputs: map[string]Value{"minutes": literal(minutes)}}
}

func sequenceStep(messages any, inputs map[string]Value) Step {
	step := Step{ID: "sequence", Kind: KindMessageSequence, Name: "Follow up", Inputs: map[string]Value{"messages": literal(messages), "delivery": literal("review")}}
	for name, value := range inputs {
		step.Inputs[name] = value
	}
	return step
}

func TestMessageSequenceValidation(t *testing.T) {
	message := messageSource()
	comment := Source{Kind: "comment_received", AccountIDs: []string{"ig"}, Keywords: []string{"guide"}, MatchMode: "word", DailyLimit: 20}
	private := Step{ID: "private", Kind: KindPrivateReply, Name: "Private", Inputs: map[string]Value{"engagement_id": reference("source.engagement_id"), "text": literal("Hi"), "delivery": literal("review")}}
	fromMessage := map[string]Value{"message_id": reference("source.message_id")}
	fromComment := map[string]Value{"engagement_id": reference("source.engagement_id")}
	two := `[{"text":"One","delay_minutes":0},{"text":"Two","delay_minutes":60}]`
	cases := []struct {
		name   string
		source Source
		steps  []Step
		want   string
	}{
		{"answers the triggering message", message, []Step{sequenceStep(two, fromMessage)}, ""},
		{"accepts a stored list", message, []Step{sequenceStep([]any{map[string]any{"text": "One", "delay_minutes": float64(5)}}, fromMessage)}, ""},
		{"continues a private reply", comment, []Step{private, sequenceStep(two, fromComment)}, ""},
		{"needs an earlier private reply", comment, []Step{sequenceStep(two, fromComment)}, "private reply earlier"},
		{"a private reply on one branch is not enough", comment, []Step{{ID: "branch", Kind: KindCondition, Name: "If", Inputs: map[string]Value{"left": literal("a"), "operator": literal("equals"), "right": literal("a")}, Then: []Step{private}}, sequenceStep(two, fromComment)}, "private reply earlier"},
		{"cannot target another message", message, []Step{sequenceStep(two, map[string]Value{"message_id": literal("someone-else")})}, "started this run"},
		{"cannot target another comment", comment, []Step{private, sequenceStep(two, map[string]Value{"engagement_id": literal("someone-else")})}, "started this run"},
		{"no other trigger can start one", Source{Kind: "manual"}, []Step{sequenceStep(two, fromMessage)}, "comment or incoming message"},
		{"delays that outlast the window", message, []Step{sequenceStep(`[{"text":"One","delay_minutes":800},{"text":"Two","delay_minutes":800}]`, fromMessage)}, "24 hour"},
		{"an earlier wait counts for a message trigger", message, []Step{waitStep("wait", 1000), sequenceStep(`[{"text":"One","delay_minutes":500}]`, fromMessage)}, "24 hour"},
		{"a wait before a comment's response does not count", comment, []Step{private, waitStep("wait", 1000), sequenceStep(`[{"text":"One","delay_minutes":500}]`, fromComment)}, ""},
		{"one delay at the window", message, []Step{sequenceStep(`[{"text":"One","delay_minutes":1440}]`, fromMessage)}, "under 24 hours"},
		{"a negative delay", message, []Step{sequenceStep(`[{"text":"One","delay_minutes":-1}]`, fromMessage)}, "under 24 hours"},
		{"a fractional delay", message, []Step{sequenceStep(`[{"text":"One","delay_minutes":1.5}]`, fromMessage)}, "whole-minute"},
		{"empty text", message, []Step{sequenceStep(`[{"text":"  "}]`, fromMessage)}, "needs text"},
		{"text over the byte limit", message, []Step{sequenceStep([]any{map[string]any{"text": strings.Repeat("가", 334)}}, fromMessage)}, "byte limit"},
		{"no messages", message, []Step{sequenceStep(`[]`, fromMessage)}, "between 1 and 5"},
		{"too many messages", message, []Step{sequenceStep(`[{"text":"1"},{"text":"2"},{"text":"3"},{"text":"4"},{"text":"5"},{"text":"6"}]`, fromMessage)}, "between 1 and 5"},
		{"delivery is chosen directly", message, []Step{sequenceStep(two, map[string]Value{"message_id": reference("source.message_id"), "delivery": literal("broadcast")})}, "review or automatic"},
		{"the response window is bounded", comment, []Step{private, sequenceStep(two, map[string]Value{"engagement_id": reference("source.engagement_id"), "response_window_minutes": literal(10081)})}, "7 days"},
		{"a run answers once", message, []Step{replyStep(), sequenceStep(two, fromMessage)}, "answers one message once"},
		{"unknown fields are rejected", message, []Step{sequenceStep(two, map[string]Value{"message_id": reference("source.message_id"), "tag": literal("HUMAN_AGENT")})}, "unsupported follow-up field"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(Definition{Schema: 1, Source: test.source, Steps: test.steps}, true)
			if test.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.want)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestMessageReplyValidation(t *testing.T) {
	source := messageSource()
	cases := []struct {
		name   string
		source Source
		steps  []Step
		want   string
	}{
		{"delivers within the window", source, []Step{waitStep("wait", 60), replyStep()}, ""},
		{"delay outlasts the window", source, []Step{waitStep("wait", 1440), replyStep()}, "24 hour"},
		{"delays accumulate", source, []Step{waitStep("first", 1000), waitStep("second", 1000), replyStep()}, "24 hour"},
		{"a branch delay carries past the branch", source, []Step{{ID: "branch", Kind: KindCondition, Name: "If", Inputs: map[string]Value{"left": literal("a"), "operator": literal("equals"), "right": literal("a")}, Then: []Step{waitStep("wait", 1500)}}, replyStep()}, "24 hour"},
		{"an unknowable delay is left for dispatch", source, []Step{{ID: "wait", Kind: KindWait, Name: "Wait", Inputs: map[string]Value{"minutes": reference("source.delay")}}, replyStep()}, ""},
		{"needs the incoming message trigger", Source{Kind: "manual"}, []Step{replyStep()}, "incoming message trigger"},
		{"cannot target another message", source, []Step{{ID: "reply", Kind: KindMessageReply, Name: "Reply", Inputs: map[string]Value{"message_id": literal("someone-else"), "text": literal("Hello"), "delivery": literal("automatic")}}}, "started this run"},
		{"delivery is chosen directly", source, []Step{{ID: "reply", Kind: KindMessageReply, Name: "Reply", Inputs: map[string]Value{"message_id": reference("source.message_id"), "text": literal("Hello"), "delivery": literal("broadcast")}}}, "review or automatic"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(Definition{Schema: 1, Source: test.source, Steps: test.steps}, true)
			if test.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.want)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
	for name, invalidSource := range map[string]Source{
		"no account":  {Kind: "message_received", DailyLimit: 1},
		"no limit":    {Kind: "message_received", AccountIDs: []string{"ig"}},
		"blank match": {Kind: "message_received", AccountIDs: []string{"ig"}, DailyLimit: 1, MatchMode: "regex"},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, Validate(Definition{Schema: 1, Source: invalidSource, Steps: []Step{replyStep()}}, true), ErrInvalid)
		})
	}
}

func TestDispatchStopFailsTheRunWithItsReason(t *testing.T) {
	const reason = "The Instagram messaging window closed before this message was sent."
	actions := actionFunc(func(context.Context, EffectRequest) (EffectResult, error) {
		return EffectResult{Output: map[string]any{"status": "cancelled", "reason": reason}}, assertErr(reason)
	})
	s, actor := workflowTestService(t, actions)
	workflow, err := s.Save(t.Context(), actor, "ws", "", SaveRequest{Name: "DM", Definition: Definition{Schema: 1, Source: messageSource(), Steps: []Step{replyStep()}}})
	require.NoError(t, err)
	run, err := s.Start(t.Context(), actor, "ws", workflow.ID, ModeLive, map[string]any{"message_id": "m"}, workflow.Revision)
	require.NoError(t, err)
	runJob(t, s, run.ID)
	run, err = s.GetRun(t.Context(), actor, "ws", run.ID)
	require.NoError(t, err)
	assert.Equal(t, StateFailed, run.State)
	assert.Equal(t, reason, run.Error)
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestReviewedMessageThatCanNoLongerSendFailsTheRunVisibly(t *testing.T) {
	const reason = "This person opted out of automatic messages."
	var approved []bool
	actions := actionFunc(func(_ context.Context, in EffectRequest) (EffectResult, error) {
		value, _ := in.Inputs["approved"].(bool)
		approved = append(approved, value)
		if value {
			return EffectResult{Stop: true, Output: map[string]any{"status": "cancelled", "reason": reason}}, nil
		}
		return EffectResult{Review: true, Output: map[string]any{"status": "awaiting_review", "text": "Hello"}}, nil
	})
	s, actor := workflowTestService(t, actions)
	workflow, err := s.Save(t.Context(), actor, "ws", "", SaveRequest{Name: "DM", Definition: Definition{Schema: 1, Source: messageSource(), Steps: []Step{replyStep()}}})
	require.NoError(t, err)
	run, err := s.Start(t.Context(), actor, "ws", workflow.ID, ModeLive, map[string]any{"message_id": "m"}, workflow.Revision)
	require.NoError(t, err)
	runJob(t, s, run.ID)
	run, err = s.GetRun(t.Context(), actor, "ws", run.ID)
	require.NoError(t, err)
	require.Equal(t, StateApproval, run.State)
	run, err = s.Approve(t.Context(), actor, "ws", run.ID, run.Revision, 0, "Edited")
	require.NoError(t, err)
	assert.Equal(t, []bool{false, true}, approved)
	assert.Equal(t, StateFailed, run.State)
	assert.Equal(t, reason, run.Error)
}

func TestPreviewOfMessageReplyNeverDispatches(t *testing.T) {
	actions := actionFunc(func(context.Context, EffectRequest) (EffectResult, error) {
		t.Error("a preview must not reach native effects")
		return EffectResult{}, nil
	})
	s, actor := workflowTestService(t, actions)
	workflow, err := s.Save(t.Context(), actor, "ws", "", SaveRequest{Name: "DM", Definition: Definition{Schema: 1, Source: messageSource(), Steps: []Step{replyStep()}}})
	require.NoError(t, err)
	run, err := s.Start(t.Context(), actor, "ws", workflow.ID, ModePreview, map[string]any{"message_id": "m"}, workflow.Revision)
	require.NoError(t, err)
	runJob(t, s, run.ID)
	run, err = s.GetRun(t.Context(), actor, "ws", run.ID)
	require.NoError(t, err)
	assert.Equal(t, StateSucceeded, run.State)
	raw, err := json.Marshal(run.Steps[0].Output)
	require.NoError(t, err)
	assert.JSONEq(t, `{"preview":true,"message_id":"m","text":"Hello","status":"preview"}`, string(raw))
}

func TestStopRepliesAcceptsMessageCampaigns(t *testing.T) {
	var stopped []string
	s, actor := workflowTestService(t, stopRecorder{stopped: &stopped})
	workflow, err := s.Save(t.Context(), actor, "ws", "", SaveRequest{Name: "DM", Definition: Definition{Schema: 1, Source: messageSource(), Steps: []Step{replyStep()}}})
	require.NoError(t, err)
	raw, err := json.Marshal(workflow.Definition)
	require.NoError(t, err)
	_, err = s.db.NewUpdate().Model((*workflowRecord)(nil)).Set("published_json=?,enabled=?", string(raw), true).Where("id=?", workflow.ID).Exec(t.Context())
	require.NoError(t, err)
	current, err := s.Get(t.Context(), actor, "ws", workflow.ID)
	require.NoError(t, err)
	assert.True(t, current.HasCommentReplies)
	result, err := s.StopReplies(t.Context(), actor, "ws", workflow.ID, current.Revision)
	require.NoError(t, err)
	assert.False(t, result.Enabled)
	assert.Equal(t, []string{workflow.ID}, stopped)
}

type stopRecorder struct{ stopped *[]string }

func (stopRecorder) Execute(context.Context, EffectRequest) (EffectResult, error) {
	return EffectResult{}, nil
}
func (r stopRecorder) StopReplies(_ context.Context, _ workspaceaccess.StoredAuthority, workflowID, _ string) error {
	*r.stopped = append(*r.stopped, workflowID)
	return nil
}

func (f *messageSourceFixture) conversation(t *testing.T, id, counterpart string) {
	t.Helper()
	now := time.Now().UTC()
	row := models.Conversation{ID: id, WorkspaceID: "ws", SocialAccountID: "ig", Platform: "instagram", RemoteConversationID: "t-" + id, CounterpartRemoteID: counterpart, MessagingWindowExpiresAt: now.Add(24 * time.Hour), CreatedAt: now, UpdatedAt: now}
	_, err := f.s.db.NewInsert().Model(&row).Exec(t.Context())
	require.NoError(t, err)
}

func TestMessageDailyLimitCountsOnlyRunsThatCouldAnswer(t *testing.T) {
	f := newMessageSourceFixture(t, "sqlite")
	source := messageSource()
	source.DailyLimit = 2
	workflow := f.publish(t, source, nil)
	f.conversation(t, "c-a", "reader-a")
	f.conversation(t, "c-b", "reader-b")
	f.conversation(t, "c-c", "reader-c")
	f.message(t, models.DirectMessage{ID: "failed", ConversationID: "c-a", AuthorRemoteID: "reader-a", Body: "hi"})
	f.message(t, models.DirectMessage{ID: "skipped", ConversationID: "c-b", AuthorRemoteID: "reader-b", Body: "hi"})
	require.NoError(t, f.s.poll(t.Context(), workflow.ID))
	require.Len(t, f.runSources(t, workflow.ID), 2)
	_, err := f.s.db.NewRaw(`UPDATE workflow_runs SET state = 'failed' WHERE workflow_id = ? AND source_json LIKE '%failed%'`, workflow.ID).Exec(t.Context())
	require.NoError(t, err)
	_, err = f.s.db.NewRaw(`UPDATE workflow_runs SET state = 'succeeded', results_json = '[{"output":{"status":"skipped"}}]' WHERE workflow_id = ? AND source_json LIKE '%skipped%'`, workflow.ID).Exec(t.Context())
	require.NoError(t, err)
	f.message(t, models.DirectMessage{ID: "answered", ConversationID: "c-c", AuthorRemoteID: "reader-c", Body: "hi"})
	require.NoError(t, f.s.poll(t.Context(), workflow.ID))
	assert.Contains(t, f.runSources(t, workflow.ID), "answered")
}

func TestOneSenderCannotExhaustAWorkflowsDailyLimit(t *testing.T) {
	f := newMessageSourceFixture(t, "sqlite")
	workflow := f.publish(t, messageSource(), nil)
	f.conversation(t, "c-other", "another-reader")
	for i := range maxMessageRunsPerSender + 3 {
		f.message(t, models.DirectMessage{ID: fmt.Sprintf("same-%d", i), AuthorRemoteID: "reader", Body: "hi", CreatedAt: time.Now().UTC().Add(time.Duration(i+1) * time.Minute)})
	}
	f.message(t, models.DirectMessage{ID: "other", ConversationID: "c-other", AuthorRemoteID: "another-reader", Body: "hi", CreatedAt: time.Now().UTC().Add(time.Hour)})
	require.NoError(t, f.s.poll(t.Context(), workflow.ID))
	runs, err := f.s.Runs(t.Context(), f.actor, "ws", workflow.ID)
	require.NoError(t, err)
	started := 0
	for _, run := range runs {
		if run.State != StateCancelled {
			started++
		}
	}
	assert.Equal(t, maxMessageRunsPerSender+1, started)
	assert.Contains(t, f.runSources(t, workflow.ID), "other")
}

func TestTakenOverConversationStartsNoRunEvenAfterResume(t *testing.T) {
	f := newMessageSourceFixture(t, "sqlite")
	workflow := f.publish(t, messageSource(), nil)
	_, err := f.s.db.NewRaw(`INSERT INTO message_automation_stops(conversation_id,workspace_id,stopped_by,reason,stopped_at) VALUES('c-reader','ws','provider','person',?)`, time.Now().UTC()).Exec(t.Context())
	require.NoError(t, err)
	f.message(t, models.DirectMessage{ID: "during", AuthorRemoteID: "reader", Body: "hi"})
	require.NoError(t, f.s.poll(t.Context(), workflow.ID))
	assert.Empty(t, f.runSources(t, workflow.ID))

	_, err = f.s.db.NewRaw(`DELETE FROM message_automation_stops WHERE conversation_id = 'c-reader'`).Exec(t.Context())
	require.NoError(t, err)
	require.NoError(t, f.s.poll(t.Context(), workflow.ID))
	assert.Empty(t, f.runSources(t, workflow.ID), "resume applies to future messages")
	f.message(t, models.DirectMessage{ID: "after", AuthorRemoteID: "reader", Body: "hi"})
	require.NoError(t, f.s.poll(t.Context(), workflow.ID))
	assert.Equal(t, []string{"after"}, f.runSources(t, workflow.ID))
}

func (f *messageSourceFixture) parkedRun(t *testing.T, workflowID string, wakeAt time.Time) {
	t.Helper()
	_, err := f.s.db.NewRaw(`INSERT INTO workflow_runs(id,workflow_id,workspace_id,workflow_name,workflow_revision,mode,state,revision,definition_json,authority_json,source_json,remaining_json,results_json,wake_at,created_at,updated_at) VALUES('parked',?,'ws','DM',1,'live','waiting',1,'{}','{}','{}','[]','[]',?,?,?)`, workflowID, wakeAt, f.now, f.now).Exec(t.Context())
	require.NoError(t, err)
	_, err = f.s.db.NewRaw(`INSERT INTO message_sequence_waits(run_id,workspace_id,item_id,account_id,author_remote_id,replied_at,respond_by) VALUES('parked','ws','comment','ig','reader',?,?)`, f.now, wakeAt).Exec(t.Context())
	require.NoError(t, err)
}

func (f *messageSourceFixture) parkedWake(t *testing.T) time.Time {
	t.Helper()
	var wake time.Time
	require.NoError(t, f.s.db.NewSelect().Model((*runRecord)(nil)).Column("wake_at").Where("id = 'parked'").Scan(t.Context(), &wake))
	return wake
}

func eachMessageBackend(t *testing.T, test func(*testing.T, *messageSourceFixture)) {
	t.Helper()
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) { test(t, newMessageSourceFixture(t, backend)) })
	}
}

func TestRunsWaitingForAResponseDoNotHoldTheActiveRunLimit(t *testing.T) {
	eachMessageBackend(t, func(t *testing.T, f *messageSourceFixture) {
		workflow := f.publish(t, messageSource(), nil)
		f.parkedRun(t, workflow.ID, f.now.Add(7*24*time.Hour))
		count, err := countActiveRuns(t.Context(), f.s.db, "ws")
		require.NoError(t, err)
		assert.Zero(t, count)
	})
}

func TestBatchedCheckWakesOnlyRunsWithSomethingToDo(t *testing.T) {
	eachMessageBackend(t, func(t *testing.T, f *messageSourceFixture) {
		workflow := f.publish(t, messageSource(), nil)
		far := f.now.Add(7 * 24 * time.Hour).Truncate(time.Second)
		f.parkedRun(t, workflow.ID, far)

		require.NoError(t, f.s.wakeRespondedRuns(t.Context(), f.now.Add(time.Minute)))
		assert.WithinDuration(t, far, f.parkedWake(t), time.Second, "nothing happened")

		f.message(t, models.DirectMessage{ID: "older", AuthorRemoteID: "reader", Body: "hi", RemoteCreatedAt: f.now.Add(-time.Hour)})
		require.NoError(t, f.s.wakeRespondedRuns(t.Context(), f.now.Add(time.Minute)))
		assert.WithinDuration(t, far, f.parkedWake(t), time.Second, "a message from before the private reply is not a response")

		f.message(t, models.DirectMessage{ID: "response", AuthorRemoteID: "reader", Body: "hi", RemoteCreatedAt: f.now.Add(time.Minute)})
		wakeAt := f.now.Add(2 * time.Minute)
		require.NoError(t, f.s.wakeRespondedRuns(t.Context(), wakeAt))
		assert.WithinDuration(t, wakeAt, f.parkedWake(t), time.Second)
	})
}

func TestBatchedCheckWakesARunThatMustStop(t *testing.T) {
	for name, stop := range map[string]func(*testing.T, *messageSourceFixture){
		"the person opted out": func(t *testing.T, f *messageSourceFixture) {
			_, err := f.s.db.NewRaw(`INSERT INTO engagement_automation_optouts(social_account_id,author_remote_id,created_at) VALUES('ig','reader',?)`, f.now).Exec(t.Context())
			require.NoError(t, err)
		},
		"someone took over the comment": func(t *testing.T, f *messageSourceFixture) {
			_, err := f.s.db.NewInsert().Model(&models.EngagementItem{ID: "comment", WorkspaceID: "ws", SocialAccountID: "ig", RemoteID: "remote", Body: "guide", AuthorRemoteID: "reader", Platform: "instagram", RemoteCreatedAt: f.now, AutomationStoppedAt: f.now, CreatedAt: f.now, UpdatedAt: f.now}).Exec(t.Context())
			require.NoError(t, err)
		},
	} {
		t.Run(name, func(t *testing.T) {
			eachMessageBackend(t, func(t *testing.T, f *messageSourceFixture) {
				workflow := f.publish(t, messageSource(), nil)
				f.parkedRun(t, workflow.ID, f.now.Add(7*24*time.Hour))
				stop(t, f)
				wakeAt := f.now.Add(time.Minute)
				require.NoError(t, f.s.wakeRespondedRuns(t.Context(), wakeAt))
				assert.WithinDuration(t, wakeAt, f.parkedWake(t), time.Second)
			})
		})
	}
}

func TestBatchedCheckClearsFinishedWaits(t *testing.T) {
	eachMessageBackend(t, func(t *testing.T, f *messageSourceFixture) {
		workflow := f.publish(t, messageSource(), nil)
		f.parkedRun(t, workflow.ID, f.now.Add(time.Hour))
		_, err := f.s.db.NewRaw(`UPDATE workflow_runs SET state = 'cancelled' WHERE id = 'parked'`).Exec(t.Context())
		require.NoError(t, err)
		require.NoError(t, f.s.wakeRespondedRuns(t.Context(), f.now))
		remaining, err := f.s.db.NewSelect().Table("message_sequence_waits").Count(t.Context())
		require.NoError(t, err)
		assert.Zero(t, remaining)
	})
}

func TestTakeoverCancelsACommentRunWaitingForThatPerson(t *testing.T) {
	eachMessageBackend(t, func(t *testing.T, f *messageSourceFixture) {
		workflow := f.publish(t, messageSource(), nil)
		f.parkedRun(t, workflow.ID, f.now.Add(time.Hour))
		require.NoError(t, f.s.db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
			return f.s.CancelMessageAutomationRuns(ctx, tx, "ws", "c-reader", nil, "A person took over this conversation.")
		}))
		var state string
		require.NoError(t, f.s.db.NewSelect().Model((*runRecord)(nil)).Column("state").Where("id = 'parked'").Scan(t.Context(), &state))
		assert.Equal(t, StateCancelled, state)
	})
}

func TestMessageFirstStoredFromHistoryNeverStartsARun(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := newMessageSourceFixture(t, backend)
			workflow := f.publish(t, messageSource("GUIDE"), nil)
			f.message(t, models.DirectMessage{ID: "imported", AuthorRemoteID: "reader", Body: "guide"})
			_, err := f.s.db.NewRaw(`INSERT INTO message_history_imports(message_id) VALUES('imported')`).Exec(t.Context())
			require.NoError(t, err)
			f.message(t, models.DirectMessage{ID: "live", AuthorRemoteID: "reader", Body: "guide"})
			require.NoError(t, f.s.poll(t.Context(), workflow.ID))
			assert.Equal(t, []string{"live"}, f.runSources(t, workflow.ID))
		})
	}
}

func TestSenderOverTheLimitIsVisibleAndStartsNoStep(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := newMessageSourceFixture(t, backend)
			source := messageSource("GUIDE")
			source.DailyLimit = 100
			workflow := f.publish(t, source, nil)
			for i := range maxMessageRunsPerSender + 1 {
				f.message(t, models.DirectMessage{ID: fmt.Sprintf("m%d", i), AuthorRemoteID: "reader", Body: "guide"})
			}
			require.NoError(t, f.s.poll(t.Context(), workflow.ID))
			runs, err := f.s.Runs(t.Context(), f.actor, "ws", workflow.ID)
			require.NoError(t, err)
			require.Len(t, runs, maxMessageRunsPerSender+1, "no message disappears")
			limited := 0
			for _, run := range runs {
				if run.Error == stopReasonSenderLimit {
					limited++
					assert.Equal(t, StateCancelled, run.State)
				}
			}
			assert.Equal(t, 1, limited)
		})
	}
}

func TestSenderLimitCountsRunsWhateverTheyWentOnToDo(t *testing.T) {
	endings := map[string]string{
		"skipped": `UPDATE workflow_runs SET state = 'succeeded', results_json = '[{"status":"skipped"}]' WHERE state = 'queued'`,
		"failed":  `UPDATE workflow_runs SET state = 'failed', error = 'The AI step failed.' WHERE state = 'queued'`,
	}
	for name, ending := range endings {
		t.Run(name, func(t *testing.T) {
			f := newMessageSourceFixture(t, "sqlite")
			source := messageSource("GUIDE")
			source.DailyLimit = 100
			workflow := f.publish(t, source, nil)
			for i := range maxMessageRunsPerSender + 2 {
				f.message(t, models.DirectMessage{ID: fmt.Sprintf("m%d", i), AuthorRemoteID: "reader", Body: "guide"})
				require.NoError(t, f.s.poll(t.Context(), workflow.ID))
				_, err := f.s.db.NewRaw(ending).Exec(t.Context())
				require.NoError(t, err)
			}
			queued, err := f.s.db.NewSelect().Model((*runRecord)(nil)).Where("state = ?", StateQueued).Count(t.Context())
			require.NoError(t, err)
			assert.Zero(t, queued, "no step of a message over the limit can run")
			limited, err := f.s.db.NewSelect().Model((*runRecord)(nil)).Where("state = ? AND error = ?", StateCancelled, stopReasonSenderLimit).Count(t.Context())
			require.NoError(t, err)
			assert.Equal(t, 2, limited)
		})
	}
}

func TestDailyLimitCountsSkippedRunsWhenAStepCosts(t *testing.T) {
	ai := Step{ID: "think", Kind: KindAIText}
	assert.True(t, paidSteps([]Step{{ID: "branch", Kind: KindCondition, Then: []Step{ai}}}))
	assert.False(t, paidSteps([]Step{{ID: "reply", Kind: KindMessageReply}, {ID: "wait", Kind: KindWait}}))
	f := newMessageSourceFixture(t, "sqlite")
	source := messageSource("GUIDE")
	source.DailyLimit = 2
	workflow := f.publish(t, source, nil)
	record := workflowRecord{ID: workflow.ID}
	f.message(t, models.DirectMessage{ID: "m1", AuthorRemoteID: "reader", Body: "guide"})
	f.message(t, models.DirectMessage{ID: "m2", AuthorRemoteID: "reader", Body: "guide"})
	require.NoError(t, f.s.poll(t.Context(), workflow.ID))
	_, err := f.s.db.NewRaw(`UPDATE workflow_runs SET results_json = '[{"status":"skipped"}]'`).Exec(t.Context())
	require.NoError(t, err)
	free, err := commentDailyLimitReached(t.Context(), f.s.db, record, Definition{Source: source, Steps: []Step{{ID: "reply", Kind: KindMessageReply}}}, time.Now().UTC())
	require.NoError(t, err)
	assert.False(t, free)
	paid, err := commentDailyLimitReached(t.Context(), f.s.db, record, Definition{Source: source, Steps: []Step{ai}}, time.Now().UTC())
	require.NoError(t, err)
	assert.True(t, paid)
}

func TestDailyLimitIsVisibleOnTheWorkflow(t *testing.T) {
	f := newMessageSourceFixture(t, "sqlite")
	source := messageSource("GUIDE")
	source.DailyLimit = 1
	workflow := f.publish(t, source, nil)
	f.message(t, models.DirectMessage{ID: "m1", AuthorRemoteID: "reader", Body: "guide"})
	f.message(t, models.DirectMessage{ID: "m2", AuthorRemoteID: "other", Body: "guide"})
	require.NoError(t, f.s.poll(t.Context(), workflow.ID))
	var note string
	require.NoError(t, f.s.db.NewSelect().Model((*workflowRecord)(nil)).Column("source_error").Where("id = ?", workflow.ID).Scan(t.Context(), &note))
	assert.Equal(t, messageDailyLimitNote, note)
}

func TestLapsedTakeoverNoLongerHoldsAMessage(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := newMessageSourceFixture(t, backend)
			workflow := f.publish(t, messageSource("GUIDE"), nil)
			_, err := f.s.db.NewRaw(`INSERT INTO message_automation_stops(conversation_id,workspace_id,stopped_by,reason,stopped_at,lapses_at) VALUES('c-reader','ws','provider','x',?,?)`, f.now.Add(-8*24*time.Hour), f.now.Add(-time.Hour)).Exec(t.Context())
			require.NoError(t, err)
			f.message(t, models.DirectMessage{ID: "after", AuthorRemoteID: "reader", Body: "guide"})
			require.NoError(t, f.s.poll(t.Context(), workflow.ID))
			assert.Equal(t, []string{"after"}, f.runSources(t, workflow.ID))
		})
	}
}

func (f *messageSourceFixture) parkedReview(t *testing.T, workflowID string, sequenceLength int, triggerAt time.Time) {
	t.Helper()
	f.message(t, models.DirectMessage{ID: "trigger", AuthorRemoteID: "reader", Body: "guide", RemoteCreatedAt: triggerAt})
	_, err := f.s.db.NewRaw(`INSERT INTO workflow_runs(id,workflow_id,workspace_id,workflow_name,workflow_revision,mode,state,revision,definition_json,authority_json,source_json,remaining_json,results_json,created_at,updated_at) VALUES('review',?,'ws','DM',1,'live','awaiting_approval',1,'{}','{}','{}','[]','[]',?,?)`, workflowID, f.now, f.now).Exec(t.Context())
	require.NoError(t, err)
	_, err = f.s.db.NewRaw(`INSERT INTO message_automation_replies(id,workspace_id,conversation_id,trigger_message_id,workflow_id,run_id,step_id,user_id,body,state,reason,message_id,sequence_position,sequence_length,comment_item_id,created_at,updated_at) VALUES('receipt','ws','c-reader','trigger',?,'review','send','user','Hi','awaiting_review','','',0,?,'',?,?)`, workflowID, sequenceLength, f.now, f.now).Exec(t.Context())
	require.NoError(t, err)
}

func (f *messageSourceFixture) reviewOutcome(t *testing.T) (runState, runError, receiptState, receiptReason string) {
	t.Helper()
	require.NoError(t, f.s.db.NewRaw(`SELECT r.state, r.error, a.state, a.reason FROM workflow_runs r JOIN message_automation_replies a ON a.run_id = r.id WHERE r.id = 'review'`).Scan(t.Context(), &runState, &runError, &receiptState, &receiptReason))
	return runState, runError, receiptState, receiptReason
}

func TestLateMessageApprovalEndsWithItsReason(t *testing.T) {
	day := 24 * time.Hour
	tests := map[string]struct {
		sequenceLength int
		triggerAge     time.Duration
		windowLeft     time.Duration
		ended          bool
	}{
		"open window":                         {sequenceLength: 2, triggerAge: time.Hour, windowLeft: time.Hour},
		"closed window":                       {sequenceLength: 2, triggerAge: time.Hour, windowLeft: -time.Minute, ended: true},
		"closed window on a plain reply":      {triggerAge: time.Hour, windowLeft: -time.Minute, ended: true},
		"a later message extends a plain one": {triggerAge: 2 * day, windowLeft: time.Hour},
		"but not a sequence":                  {sequenceLength: 2, triggerAge: 2 * day, windowLeft: time.Hour, ended: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			eachMessageBackend(t, func(t *testing.T, f *messageSourceFixture) {
				workflow := f.publish(t, messageSource(), nil)
				f.parkedReview(t, workflow.ID, test.sequenceLength, f.now.Add(-test.triggerAge))
				_, err := f.s.db.NewUpdate().Model((*models.Conversation)(nil)).Set("messaging_window_expires_at = ?", f.now.Add(test.windowLeft)).Where("id = 'c-reader'").Exec(t.Context())
				require.NoError(t, err)
				require.NoError(t, f.s.endLateMessageApprovals(t.Context(), f.now))
				runState, runError, receiptState, receiptReason := f.reviewOutcome(t)
				if !test.ended {
					assert.Equal(t, StateApproval, runState)
					assert.Equal(t, "awaiting_review", receiptState)
					return
				}
				assert.Equal(t, StateCancelled, runState)
				assert.Equal(t, stopReasonApprovalLate, runError)
				assert.Equal(t, "cancelled", receiptState)
				assert.Equal(t, stopReasonApprovalLate, receiptReason)
			})
		})
	}
}
