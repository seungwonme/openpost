package workflows

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
