package handlers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/openpost/backend/internal/jobregistry"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/engagement"
	"github.com/openpost/backend/internal/services/messaging"
	"github.com/openpost/backend/internal/services/workflows"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// wakeCommentProvider answers the private reply in place of the Instagram
// adapter, whose wire format the platform tests own.
type wakeCommentProvider struct{ private *[]string }

func (wakeCommentProvider) EngagementSupport() platform.EngagementSupport {
	return platform.EngagementSupport{Enabled: true, CanReply: true, RequiredScopes: []string{"instagram_manage_comments"}}
}
func (wakeCommentProvider) ListComments(context.Context, string, string, string) ([]platform.Comment, error) {
	return nil, nil
}
func (wakeCommentProvider) ReplyToComment(context.Context, string, string, string, string) (string, error) {
	return "public-reply", nil
}
func (wakeCommentProvider) HideComment(context.Context, string, string, string) error   { return nil }
func (wakeCommentProvider) DeleteComment(context.Context, string, string, string) error { return nil }
func (p wakeCommentProvider) ReplyPrivatelyToComment(_ context.Context, _, _, _, message string) (string, error) {
	*p.private = append(*p.private, message)
	return "private-reply", nil
}
func (p wakeCommentProvider) ReplyPrivatelyToCommentWithQuickReplies(_ context.Context, _, _, _, message string, _ []platform.QuickReply) (string, error) {
	*p.private = append(*p.private, message)
	return "private-reply", nil
}

// wakeInboxProvider stores what the person sent the way the messages sync job
// reads it while the Meta app has no webhooks, and answers the profile read.
type wakeInboxProvider struct {
	dmProvider
	polled   *[]platform.ProviderConversation
	profiles *[]string
}

func (p wakeInboxProvider) FetchMessages(context.Context, string, platform.FetchMessagesRequest) (platform.FetchMessagesResult, error) {
	return platform.FetchMessagesResult{Conversations: *p.polled}, nil
}
func (p wakeInboxProvider) FetchMessagingUserProfile(_ context.Context, _, userID string) (platform.MessagingUserProfile, error) {
	*p.profiles = append(*p.profiles, userID)
	return platform.MessagingUserProfile{FollowsAccount: true}, nil
}

type followWakeHarness struct {
	db         *bun.DB
	inbox      *messaging.Service
	engagement *engagement.Service
	service    *workflows.Service
	sent       *[]platform.SendMessageRequest
	private    *[]string
	polled     *[]platform.ProviderConversation
	profiles   *[]string
	actor      workspaceaccess.ActorFacts
	runID      string
}

// newFollowWakeHarness admits one comment campaign run through the comment
// source, delivers its private reply, and leaves it waiting for the person's
// response with the job its wait scheduled for the end of the response window.
func newFollowWakeHarness(t *testing.T, followGate bool) *followWakeHarness {
	t.Helper()
	db := workflowHandlerDB(t)
	now := time.Now().UTC()
	account := models.SocialAccount{ID: "ig", WorkspaceID: "ws", Platform: "instagram", AccountID: "owner", Slug: "ig", AccessTokenEnc: []byte("token"), GrantedScopes: "instagram_manage_comments", IsActive: true, CreatedAt: now}
	_, err := db.NewInsert().Model(&account).Exec(t.Context())
	require.NoError(t, err)
	sent, private, polled, profiles := []platform.SendMessageRequest{}, []string{}, []platform.ProviderConversation{}, []string{}
	inbox := messaging.NewService(db, dmToken{}, nil)
	inbox.SetFeatureGate(dmGate{})
	inbox.SetProvider("instagram", wakeInboxProvider{dmProvider: dmProvider{sent: &sent}, polled: &polled, profiles: &profiles})
	comments := engagement.NewService(db, dmToken{}, nil)
	comments.SetFeatureGate(dmGate{})
	comments.SetProvider("instagram", wakeCommentProvider{private: &private})
	actions := NewWorkflowActionsWithInbox(NewPublicationHandler(db, workflowSession{}, nil), nil, nil, comments, inbox)
	service := workflows.NewService(db, actions, nil)
	inbox.SetAutomationRunStopper(service.CancelMessageAutomationRuns)
	comments.SetAutomationRunStopper(service.CancelCommentReplyRuns)
	h := &followWakeHarness{db: db, inbox: inbox, engagement: comments, service: service, sent: &sent, private: &private, polled: &polled, profiles: &profiles, actor: workspaceaccess.ActorFacts{UserID: "user", SessionID: "session"}}

	ask := workflows.Step{ID: "ask", Kind: workflows.KindPrivateReply, Name: "Ask", Inputs: map[string]workflows.Value{"engagement_id": {Reference: "source.engagement_id"}, "text": {Literal: "Follow us, then tap below."}, "delivery": {Literal: "automatic"}}}
	link := workflows.Step{ID: "link", Kind: workflows.KindMessageSequence, Name: "Link", Inputs: map[string]workflows.Value{"engagement_id": {Reference: "source.engagement_id"}, "messages": {Literal: `[{"text":"Here is your guide"}]`}, "delivery": {Literal: "automatic"}}}
	if followGate {
		ask.Inputs["quick_reply_title"] = workflows.Value{Literal: "팔로우했어요"}
		link.Inputs["follow_gate"] = workflows.Value{Literal: `{"button_title":"Open guide","button_url":"https://example.com/guide","not_following_text":"Follow us, then reply again.","max_checks":3}`}
	}
	def := workflows.Definition{Schema: 1, Source: workflows.Source{Kind: "comment_received", AccountIDs: []string{"ig"}, Keywords: []string{"guide"}, MatchMode: "word", DailyLimit: 20}, Steps: []workflows.Step{ask, link}}
	saved, err := service.Save(t.Context(), h.actor, "ws", "", workflows.SaveRequest{Name: "Guide", Definition: def})
	require.NoError(t, err)
	published, err := service.Publish(t.Context(), h.actor, "ws", saved.ID, saved.Revision)
	require.NoError(t, err)

	at := time.Now().UTC().Add(time.Second)
	_, err = db.NewInsert().Model(&models.EngagementItem{ID: "comment", WorkspaceID: "ws", SocialAccountID: "ig", Platform: "instagram", Kind: "comment", RemoteID: "remote-comment", Body: "guide please", AuthorRemoteID: "reader", ConversationRemoteID: "sample-reel", CanReply: true, RemoteCreatedAt: at, CreatedAt: at, UpdatedAt: at, LastSeenAt: at}).Exec(t.Context())
	require.NoError(t, err)
	require.NoError(t, service.HandleJob(t.Context(), jobregistry.TypeWorkflowPoll, `{"workflow_id":"`+published.ID+`"}`))
	runs, err := service.Runs(t.Context(), h.actor, "ws", published.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1, "the comment starts one run")
	h.runID = runs[0].ID

	h.drain(t)
	require.Equal(t, []string{"Follow us, then tap below."}, *h.private, "the private reply was delivered")
	run := h.run(t)
	require.Equal(t, workflows.StateWaiting, run.State)
	require.Equal(t, "link", run.CurrentStepID, "the run waits for the person's response")
	job := h.pendingRunJob(t)
	require.True(t, job.RunAt.After(time.Now().Add(6*24*time.Hour)), "the wait scheduled its job for the end of the response window, got %s", job.RunAt)
	return h
}

func (h *followWakeHarness) run(t *testing.T) workflows.Run {
	t.Helper()
	run, err := h.service.GetRun(t.Context(), h.actor, "ws", h.runID)
	require.NoError(t, err)
	return run
}

// pendingRunJob is the run's only active job.
func (h *followWakeHarness) pendingRunJob(t *testing.T) models.Job {
	t.Helper()
	var jobs []models.Job
	require.NoError(t, h.db.NewSelect().Model(&jobs).Where("type = ? AND status IN (?)", jobregistry.TypeWorkflowRun, bun.List([]string{jobregistry.StatusPending, jobregistry.StatusProcessing})).Where("payload LIKE ?", "%"+h.runID+"%").Scan(t.Context()))
	require.Len(t, jobs, 1, "the run has exactly one active job")
	return jobs[0]
}

// drain plays the worker: it acquires due jobs the way the queue does (status,
// lease generation) and runs them through their owners until none are due.
// A run polling a delivery in flight waits five seconds; drain skips only such
// short polls, never a wait for the person's response.
func (h *followWakeHarness) drain(t *testing.T) {
	t.Helper()
	handlers := map[string]func(context.Context, string, string) error{
		jobregistry.TypeWorkflowRun:     h.service.HandleJob,
		engagement.JobTypeEngagementAct: h.engagement.HandleJob,
		messaging.JobTypeMessageSend:    h.inbox.HandleJob,
	}
	types := []string{jobregistry.TypeWorkflowRun, engagement.JobTypeEngagementAct, messaging.JobTypeMessageSend}
	for range 50 {
		now, poll := time.Now().UTC(), time.Now().UTC().Add(time.Minute)
		_, err := h.db.NewRaw(`UPDATE workflow_runs SET wake_at = ? WHERE state = ? AND wake_at > ? AND wake_at <= ?`, now, workflows.StateWaiting, now, poll).Exec(t.Context())
		require.NoError(t, err)
		_, err = h.db.NewUpdate().Model((*models.Job)(nil)).Set("run_at = ?", now).Where("type = ? AND status = ? AND run_at > ? AND run_at <= ?", jobregistry.TypeWorkflowRun, jobregistry.StatusPending, now, poll).Exec(t.Context())
		require.NoError(t, err)
		var job models.Job
		err = h.db.NewSelect().Model(&job).Where("status = ? AND run_at <= ? AND type IN (?)", jobregistry.StatusPending, now, bun.List(types)).Order("run_at ASC", "id ASC").Limit(1).Scan(t.Context())
		if err != nil {
			return
		}
		_, err = h.db.NewUpdate().Model((*models.Job)(nil)).Set("status = ?, lease_generation = lease_generation + 1", jobregistry.StatusProcessing).Where("id = ?", job.ID).Exec(t.Context())
		require.NoError(t, err)
		require.NoError(t, handlers[job.Type](t.Context(), job.Type, job.Payload), "job %s", job.Type)
		_, err = h.db.NewUpdate().Model((*models.Job)(nil)).Set("status = ?", jobregistry.StatusCompleted).Where("id = ?", job.ID).Exec(t.Context())
		require.NoError(t, err)
	}
	t.Fatal("jobs kept coming due")
}

// reply stores the person's message through the messages sync job.
func (h *followWakeHarness) reply(t *testing.T, id, body string) {
	t.Helper()
	at := time.Now().UTC()
	*h.polled = []platform.ProviderConversation{{
		ID: "remote-thread", CounterpartRemoteID: "reader", LastMessageAt: at, LastRemoteMessageID: id, ReplyWindowExpiresAt: at.Add(24 * time.Hour),
		Messages: []platform.ProviderMessage{{ID: id, Direction: "inbound", AuthorRemoteID: "reader", Body: body, RemoteCreatedAt: at}},
	}}
	require.NoError(t, h.inbox.HandleJob(t.Context(), messaging.JobTypeMessagesSync, `{"id":"ig"}`))
	stored, err := h.db.NewSelect().Model((*models.DirectMessage)(nil)).Where("remote_message_id = ?", id).Exists(t.Context())
	require.NoError(t, err)
	require.True(t, stored, "the polled reply is stored")
}

func (h *followWakeHarness) sweep(t *testing.T) {
	t.Helper()
	require.NoError(t, h.service.HandleJob(t.Context(), jobregistry.TypeWorkflowSweep, "{}"))
}

// requireDueAfterSweep is the contract the worker relies on: a woken run's
// only active job is due, so the next worker poll runs it.
func (h *followWakeHarness) requireDueAfterSweep(t *testing.T) {
	t.Helper()
	h.sweep(t)
	job := h.pendingRunJob(t)
	require.Equal(t, jobregistry.StatusPending, job.Status)
	require.False(t, job.RunAt.After(time.Now().UTC()), "the woken run's job is due after one sweep, got %s", job.RunAt)
}

func TestRepliedFollowCheckRunsOnTheNextSweep(t *testing.T) {
	h := newFollowWakeHarness(t, true)
	h.reply(t, "mid.tap", "팔로우했어요")

	h.requireDueAfterSweep(t)
	h.drain(t)

	assert.Equal(t, []string{"reader"}, *h.profiles, "the follow check read the sender's profile")
	require.Len(t, *h.sent, 1)
	assert.Equal(t, "Here is your guide", (*h.sent)[0].Body)
	assert.Equal(t, []platform.MessageButton{{Title: "Open guide", URL: "https://example.com/guide"}}, (*h.sent)[0].Buttons)
	assert.Equal(t, workflows.StateSucceeded, h.run(t).State)
}

func TestRepliedSequenceRunsOnTheNextSweep(t *testing.T) {
	h := newFollowWakeHarness(t, false)
	h.reply(t, "mid.reply", "yes please")

	h.requireDueAfterSweep(t)
	h.drain(t)

	require.Len(t, *h.sent, 1)
	assert.Equal(t, "Here is your guide", (*h.sent)[0].Body)
	assert.Equal(t, workflows.StateSucceeded, h.run(t).State)
}

// A run an earlier sweep already woke, before this fix, has wake_at in the past
// and its job still at the end of the response window (production state on
// 2026-11-09). wakeRespondedRuns no longer matches it; the next sweep recovers it.
func TestAlreadyWokenRunWithAFarJobRunsOnTheNextSweep(t *testing.T) {
	h := newFollowWakeHarness(t, true)
	h.reply(t, "mid.tap", "팔로우했어요")
	_, err := h.db.NewRaw(`UPDATE workflow_runs SET wake_at = ? WHERE id = ?`, time.Now().UTC().Add(-30*time.Minute), h.runID).Exec(t.Context())
	require.NoError(t, err)

	h.requireDueAfterSweep(t)
	h.drain(t)

	assert.Equal(t, []string{"reader"}, *h.profiles)
	require.Len(t, *h.sent, 1)
	assert.Equal(t, workflows.StateSucceeded, h.run(t).State)
}

// A person who does not follow is asked again; the run waits for their next
// reply under a new revision, and that reply must also wake it promptly.
func TestFollowRequestReWaitRunsOnTheNextReply(t *testing.T) {
	h := newFollowWakeHarness(t, true)
	following := false
	h.inbox.SetProvider("instagram", followToggleProvider{wakeInboxProvider: wakeInboxProvider{dmProvider: dmProvider{sent: h.sent}, polled: h.polled, profiles: h.profiles}, following: &following})
	h.reply(t, "mid.first", "팔로우했어요")
	h.requireDueAfterSweep(t)
	h.drain(t)
	require.Len(t, *h.sent, 1)
	assert.Equal(t, "Follow us, then reply again.", (*h.sent)[0].Body)
	run := h.run(t)
	require.Equal(t, workflows.StateWaiting, run.State)
	require.True(t, h.pendingRunJob(t).RunAt.After(time.Now().Add(time.Hour)), "the run waits for the next reply")

	following = true
	h.reply(t, "mid.second", "팔로우했어요")
	h.requireDueAfterSweep(t)
	h.drain(t)

	assert.Len(t, *h.profiles, 2)
	require.Len(t, *h.sent, 2)
	assert.Equal(t, []platform.MessageButton{{Title: "Open guide", URL: "https://example.com/guide"}}, (*h.sent)[1].Buttons)
	assert.Equal(t, workflows.StateSucceeded, h.run(t).State)
}

type followToggleProvider struct {
	wakeInboxProvider
	following *bool
}

func (p followToggleProvider) FetchMessagingUserProfile(_ context.Context, _, userID string) (platform.MessagingUserProfile, error) {
	*p.profiles = append(*p.profiles, userID)
	return platform.MessagingUserProfile{FollowsAccount: *p.following}, nil
}

// A sweep with nothing to wake leaves the wait's schedule alone.
func TestUnansweredWaitKeepsItsJobSchedule(t *testing.T) {
	h := newFollowWakeHarness(t, true)
	before := h.pendingRunJob(t).RunAt
	h.sweep(t)
	after := h.pendingRunJob(t)
	assert.True(t, before.Equal(after.RunAt), "no response, no earlier job: %s became %s", before, after.RunAt)
	var payload map[string]string
	require.NoError(t, json.Unmarshal([]byte(after.Payload), &payload))
	assert.Equal(t, h.runID, payload["run_id"])
}

// A commenter who is an account connected to the workspace after the private
// reply was sent never receives the link: the response is matched to the wrong
// account's conversation, so the follow check and the button stay unsent.
func TestFollowGateRunWithConnectedCommenterNeverSendsTheLink(t *testing.T) {
	h := newFollowWakeHarness(t, true)
	_, err := h.db.NewInsert().Model(&models.SocialAccount{ID: "sibling", WorkspaceID: "ws", Platform: "instagram", AccountID: "reader", AccountUsername: "reader_account", Slug: "sibling", AccessTokenEnc: []byte("token"), IsActive: true, CreatedAt: time.Now().UTC()}).Exec(t.Context())
	require.NoError(t, err)
	h.reply(t, "mid.tap", "팔로우했어요")

	h.sweep(t)
	h.drain(t)

	assert.Empty(t, *h.sent, "no message and no link button was sent")
	assert.Empty(t, *h.profiles, "the follow check never read the profile")
	run := h.run(t)
	assert.NotEqual(t, workflows.StateSucceeded, run.State)
	assert.Contains(t, run.Error+stepErrors(run), workflows.ConnectedAccountSkipPrefix)
}

func stepErrors(run workflows.Run) string {
	text := ""
	for _, step := range run.Steps {
		text += step.Error
	}
	return text
}
