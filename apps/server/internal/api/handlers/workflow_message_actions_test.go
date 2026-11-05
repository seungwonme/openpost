package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
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

type dmGate struct{}

func (dmGate) IsEffectiveEnabled(context.Context, string, string) (bool, error) { return true, nil }

type dmToken struct{}

func (dmToken) GetValidAccessToken(context.Context, string) (string, error) { return "token", nil }

type dmProvider struct {
	sent *[]platform.SendMessageRequest
	send func(platform.SendMessageRequest) (platform.SendMessageResult, error)
}

func (dmProvider) MessagingSupport() platform.MessagingSupport {
	return platform.MessagingSupport{Enabled: true, CanSend: true, ReplyWindow: 24 * time.Hour}
}
func (dmProvider) FetchMessages(context.Context, string, platform.FetchMessagesRequest) (platform.FetchMessagesResult, error) {
	return platform.FetchMessagesResult{}, nil
}
func (dmProvider) FetchMessageHistory(context.Context, string, platform.FetchMessageHistoryRequest) (platform.FetchMessageHistoryResult, error) {
	return platform.FetchMessageHistoryResult{}, nil
}
func (p dmProvider) SendMessage(_ context.Context, _ string, request platform.SendMessageRequest) (platform.SendMessageResult, error) {
	*p.sent = append(*p.sent, request)
	if p.send != nil {
		return p.send(request)
	}
	return platform.SendMessageResult{RemoteMessageID: "sent-remote-id"}, nil
}

type dmHarness struct {
	db       *bun.DB
	inbox    *messaging.Service
	service  *workflows.Service
	sent     *[]platform.SendMessageRequest
	actor    workspaceaccess.ActorFacts
	workflow workflows.Workflow
}

func newDMHarness(t *testing.T, steps []workflows.Step) *dmHarness {
	t.Helper()
	db := workflowHandlerDB(t)
	now := time.Now().UTC()
	account := models.SocialAccount{ID: "ig", WorkspaceID: "ws", Platform: "instagram", AccountID: "owner", Slug: "ig", AccessTokenEnc: []byte("token"), IsActive: true, CreatedAt: now}
	_, err := db.NewInsert().Model(&account).Exec(t.Context())
	require.NoError(t, err)
	inbox := messaging.NewService(db, dmToken{}, nil)
	inbox.SetFeatureGate(dmGate{})
	sent := []platform.SendMessageRequest{}
	inbox.SetProvider("instagram", dmProvider{sent: &sent})
	actions := NewWorkflowActionsWithInbox(NewPublicationHandler(db, workflowSession{}, nil), nil, nil, nil, inbox)
	service := workflows.NewService(db, actions, nil)
	inbox.SetAutomationRunStopper(service.CancelMessageAutomationRuns)
	h := &dmHarness{db: db, inbox: inbox, service: service, sent: &sent, actor: workspaceaccess.ActorFacts{UserID: "user", SessionID: "session"}}
	def := workflows.Definition{Schema: 1, Source: workflows.Source{Kind: "message_received", AccountIDs: []string{"ig"}, Keywords: []string{"guide"}, MatchMode: "word", DailyLimit: 20}, Steps: steps}
	saved, err := service.Save(t.Context(), h.actor, "ws", "", workflows.SaveRequest{Name: "Guide", Definition: def})
	require.NoError(t, err)
	h.workflow, err = service.Publish(t.Context(), h.actor, "ws", saved.ID, saved.Revision)
	require.NoError(t, err)
	return h
}

func dmReplyStep(delivery string) workflows.Step {
	return workflows.Step{ID: "reply", Kind: workflows.KindMessageReply, Name: "Reply", Inputs: map[string]workflows.Value{"message_id": {Reference: "source.message_id"}, "text": {Literal: "Here is the guide, {{source.author_name}}"}, "delivery": {Literal: delivery}}}
}

func dmEvent(id, direction, author, body string, at time.Time) platform.FetchMessageEventsResult {
	conversation := platform.ProviderConversation{ID: "thread", CounterpartRemoteID: "reader", CounterpartName: "Sample reader", LastMessageAt: at, Messages: []platform.ProviderMessage{{ID: id, Direction: direction, AuthorRemoteID: author, Body: body, RemoteCreatedAt: at}}}
	if direction == "inbound" {
		conversation.ReplyWindowExpiresAt = at.Add(24 * time.Hour)
	}
	return platform.FetchMessageEventsResult{Conversations: []platform.ProviderConversation{conversation}}
}

func (h *dmHarness) poll(t *testing.T) {
	t.Helper()
	require.NoError(t, h.service.HandleJob(t.Context(), jobregistry.TypeWorkflowPoll, `{"workflow_id":"`+h.workflow.ID+`"}`))
}

func (h *dmHarness) runs(t *testing.T) []workflows.Run {
	t.Helper()
	runs, err := h.service.Runs(t.Context(), h.actor, "ws", h.workflow.ID)
	require.NoError(t, err)
	return runs
}

// advance runs one workflow job, ignoring the timer the test has no reason to wait for.
func (h *dmHarness) advance(t *testing.T, runID string) workflows.Run {
	t.Helper()
	_, err := h.db.NewRaw(`UPDATE workflow_runs SET wake_at = NULL WHERE id = ?`, runID).Exec(t.Context())
	require.NoError(t, err)
	require.NoError(t, h.service.HandleJob(t.Context(), jobregistry.TypeWorkflowRun, `{"run_id":"`+runID+`"}`))
	run, err := h.service.GetRun(t.Context(), h.actor, "ws", runID)
	require.NoError(t, err)
	return run
}

func (h *dmHarness) sendQueued(t *testing.T) {
	t.Helper()
	var ids []string
	require.NoError(t, h.db.NewSelect().Model((*models.DirectMessage)(nil)).Column("id").Where("direction = 'outbound' AND send_status = 'queued'").Scan(t.Context(), &ids))
	for _, id := range ids {
		require.NoError(t, h.inbox.HandleJob(t.Context(), messaging.JobTypeMessageSend, `{"id":"`+id+`"}`))
	}
}

func TestDMCampaignAnswersDuplicateWebhooksOnce(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmReplyStep("automatic")})
	at := time.Now().UTC().Add(time.Second)
	event := dmEvent("mid.1", "inbound", "reader", "guide please", at)
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", event))
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", event))
	h.poll(t)
	h.poll(t)
	runs := h.runs(t)
	require.Len(t, runs, 1)

	run := h.advance(t, runs[0].ID)
	require.Equal(t, workflows.StateWaiting, run.State, "the run waits for its durable delivery")
	h.sendQueued(t)
	require.Len(t, *h.sent, 1)
	assert.Equal(t, "reader", (*h.sent)[0].CounterpartRemoteID)
	assert.Equal(t, "Here is the guide, Sample reader", (*h.sent)[0].Body)

	run = h.advance(t, run.ID)
	assert.Equal(t, workflows.StateSucceeded, run.State)
	assert.Equal(t, "sent", run.Steps[0].Output["status"])
	h.poll(t)
	assert.Len(t, h.runs(t), 1)
	assert.Len(t, *h.sent, 1)
}

func TestDMCampaignReviewEditsThenSends(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmReplyStep("review")})
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.1", "inbound", "reader", "guide", time.Now().UTC().Add(time.Second))))
	h.poll(t)
	run := h.advance(t, h.runs(t)[0].ID)
	require.Equal(t, workflows.StateApproval, run.State)
	require.Empty(t, *h.sent)
	run, err := h.service.Approve(t.Context(), h.actor, "ws", run.ID, run.Revision, 0, "Edited guide")
	require.NoError(t, err)
	require.Equal(t, workflows.StateQueued, run.State)
	require.Empty(t, *h.sent, "approval hands the send to the durable queue")
	run = h.advance(t, run.ID)
	require.Equal(t, workflows.StateWaiting, run.State)
	h.sendQueued(t)
	require.Len(t, *h.sent, 1)
	assert.Equal(t, "Edited guide", (*h.sent)[0].Body)
	assert.Equal(t, workflows.StateSucceeded, h.advance(t, run.ID).State)
}

func TestDMReviewApprovedAfterTheWindowClosedFailsVisibly(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmReplyStep("review")})
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.1", "inbound", "reader", "guide", time.Now().UTC().Add(time.Second))))
	h.poll(t)
	run := h.advance(t, h.runs(t)[0].ID)
	require.Equal(t, workflows.StateApproval, run.State)
	_, err := h.db.NewUpdate().Model((*models.Conversation)(nil)).Set("messaging_window_expires_at=?", time.Now().UTC().Add(-time.Minute)).Where("social_account_id='ig'").Exec(t.Context())
	require.NoError(t, err)
	run, err = h.service.Approve(t.Context(), h.actor, "ws", run.ID, run.Revision, 0, "Late answer")
	require.NoError(t, err)
	assert.Equal(t, workflows.StateFailed, run.State)
	assert.Contains(t, run.Error, "messaging window closed")
	assert.Empty(t, *h.sent)
}

func TestDMTakeoverCancelsTheRunWithItsReason(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmReplyStep("review")})
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.1", "inbound", "reader", "guide", time.Now().UTC().Add(time.Second))))
	h.poll(t)
	run := h.advance(t, h.runs(t)[0].ID)
	require.Equal(t, workflows.StateApproval, run.State)

	var conversationID string
	require.NoError(t, h.db.NewSelect().Model((*models.Conversation)(nil)).Column("id").Where("social_account_id='ig'").Scan(t.Context(), &conversationID))
	require.NoError(t, h.inbox.StopConversationAutomation(t.Context(), h.actor, "ws", conversationID))

	run, err := h.service.GetRun(t.Context(), h.actor, "ws", run.ID)
	require.NoError(t, err)
	assert.Equal(t, workflows.StateCancelled, run.State)
	assert.Contains(t, run.Error, "took over")
	_, err = h.service.Approve(t.Context(), h.actor, "ws", run.ID, run.Revision, 0, "Too late")
	require.ErrorIs(t, err, workflows.ErrState)
	assert.Empty(t, *h.sent)
}

func TestDMDelayedStepStopsWhenTheWindowClosesDuringAWait(t *testing.T) {
	wait := workflows.Step{ID: "wait", Kind: workflows.KindWait, Name: "Wait", Inputs: map[string]workflows.Value{"minutes": {Literal: 0}}}
	h := newDMHarness(t, []workflows.Step{wait, dmReplyStep("automatic")})
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.1", "inbound", "reader", "guide", time.Now().UTC().Add(time.Second))))
	h.poll(t)
	run := h.advance(t, h.runs(t)[0].ID)
	_, err := h.db.NewUpdate().Model((*models.Conversation)(nil)).Set("messaging_window_expires_at=?", time.Now().UTC().Add(-time.Minute)).Where("social_account_id='ig'").Exec(t.Context())
	require.NoError(t, err)
	run = h.advance(t, run.ID)
	assert.Equal(t, workflows.StateFailed, run.State)
	assert.Contains(t, run.Error, "messaging window closed")
	assert.Empty(t, *h.sent)
}

func TestDMActivationChecksTheAccount(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmReplyStep("review")})
	_, err := h.db.NewUpdate().Model((*models.SocialAccount)(nil)).Set("platform='bluesky'").Where("id='ig'").Exec(t.Context())
	require.NoError(t, err)
	current, err := h.service.Get(t.Context(), h.actor, "ws", h.workflow.ID)
	require.NoError(t, err)
	paused, err := h.service.Pause(t.Context(), h.actor, "ws", current.ID, current.Revision)
	require.NoError(t, err)
	_, err = h.service.Publish(t.Context(), h.actor, "ws", paused.ID, paused.Revision)
	require.ErrorContains(t, err, "Instagram only")
}

func TestDMAutomationHTTPTakeoverAndResume(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmReplyStep("review")})
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.1", "inbound", "reader", "guide", time.Now().UTC().Add(time.Second))))
	h.poll(t)
	h.advance(t, h.runs(t)[0].ID)
	var conversationID string
	require.NoError(t, h.db.NewSelect().Model((*models.Conversation)(nil)).Column("id").Where("social_account_id='ig'").Scan(t.Context(), &conversationID))

	e := echo.New()
	api := humaecho.NewWithGroup(e, e.Group("/api/v1"), huma.DefaultConfig("Test", "1"))
	NewEngagementMessagingHandler(workflowSession{}, h.inbox, nil).registerMessageAutomation(api)
	call := func(method, path string) messaging.ConversationAutomation {
		req := httptest.NewRequestWithContext(t.Context(), method, "/api/v1/messages/"+conversationID+"/automation"+path+"?workspace_id=ws", nil)
		req.Header.Set("Authorization", "Bearer session")
		response := httptest.NewRecorder()
		e.ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var state messaging.ConversationAutomation
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &state))
		return state
	}
	state := call(http.MethodGet, "")
	require.Len(t, state.Replies, 1)
	assert.Equal(t, messaging.AutomationAwaitingReview, state.Replies[0].State)
	assert.Nil(t, state.StoppedAt)
	state = call(http.MethodPost, "/stop")
	assert.NotNil(t, state.StoppedAt)
	assert.Equal(t, messaging.AutomationCancelled, state.Replies[0].State)
	state = call(http.MethodPost, "/resume")
	assert.Nil(t, state.StoppedAt)
}

func dmSequenceStep(delivery string) workflows.Step {
	return workflows.Step{ID: "sequence", Kind: workflows.KindMessageSequence, Name: "Follow up", Inputs: map[string]workflows.Value{"message_id": {Reference: "source.message_id"}, "messages": {Literal: `[{"text":"First, {{source.author_name}}","delay_minutes":0},{"text":"Second","delay_minutes":0}]`}, "delivery": {Literal: delivery}}}
}

func (h *dmHarness) startSequence(t *testing.T) string {
	t.Helper()
	// A first message has no delay here, so it must not be dated in the future.
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.1", "inbound", "reader", "guide", time.Now().UTC())))
	h.poll(t)
	require.Len(t, h.runs(t), 1)
	return h.runs(t)[0].ID
}

func TestDMSequenceRunSendsEachMessageOnce(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmSequenceStep("automatic")})
	runID := h.startSequence(t)
	require.Equal(t, workflows.StateWaiting, h.advance(t, runID).State)
	h.sendQueued(t)
	require.Len(t, *h.sent, 1)
	assert.Equal(t, "First, Sample reader", (*h.sent)[0].Body)
	require.Equal(t, workflows.StateWaiting, h.advance(t, runID).State)
	h.sendQueued(t)
	require.Len(t, *h.sent, 2)
	assert.Equal(t, "Second", (*h.sent)[1].Body)
	run := h.advance(t, runID)
	require.Equal(t, workflows.StateSucceeded, run.State)
	assert.EqualValues(t, 2, run.Steps[0].Output["sent"])
	assert.Equal(t, "sent", run.Steps[0].Output["status"])
	assert.Len(t, *h.sent, 2)
}

func TestDMSequenceReviewResumesTheSameStep(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmSequenceStep("review")})
	runID := h.startSequence(t)
	run := h.advance(t, runID)
	require.Equal(t, workflows.StateApproval, run.State)
	run, err := h.service.Approve(t.Context(), h.actor, "ws", run.ID, run.Revision, 0, "Edited first")
	require.NoError(t, err)
	require.Equal(t, workflows.StateQueued, run.State)
	require.Equal(t, workflows.StateWaiting, h.advance(t, runID).State)
	h.sendQueued(t)
	require.Len(t, *h.sent, 1)
	assert.Equal(t, "Edited first", (*h.sent)[0].Body)

	run = h.advance(t, runID)
	require.Equal(t, workflows.StateApproval, run.State, "the next position waits for its own review")
	assert.Equal(t, "Second", run.Steps[0].Output["text"])
	run, err = h.service.Approve(t.Context(), h.actor, "ws", run.ID, run.Revision, 0)
	require.NoError(t, err)
	require.Equal(t, workflows.StateWaiting, h.advance(t, run.ID).State)
	h.sendQueued(t)
	require.Len(t, *h.sent, 2)
	assert.Equal(t, "Second", (*h.sent)[1].Body, "approving without text does not reuse the earlier edit")
	assert.Equal(t, workflows.StateSucceeded, h.advance(t, runID).State)
}

func TestDMSequenceTakeoverCancelsTheRunWithItsReason(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmSequenceStep("automatic")})
	runID := h.startSequence(t)
	h.advance(t, runID)
	h.sendQueued(t)
	require.Len(t, *h.sent, 1)
	require.NoError(t, h.inbox.StopConversationAutomation(t.Context(), messaging.Actor{UserID: "user"}, "ws", h.conversationID(t)))
	run, err := h.service.GetRun(t.Context(), h.actor, "ws", runID)
	require.NoError(t, err)
	assert.Equal(t, workflows.StateCancelled, run.State)
	assert.NotEmpty(t, run.Error)
	h.sendQueued(t)
	assert.Len(t, *h.sent, 1)
}

func TestDMSequenceOptOutMessageEndsTheRun(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmSequenceStep("automatic")})
	runID := h.startSequence(t)
	h.advance(t, runID)
	h.sendQueued(t)
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.2", "inbound", "reader", "수신거부", time.Now().UTC().Add(2*time.Second))))
	run, err := h.service.GetRun(t.Context(), h.actor, "ws", runID)
	require.NoError(t, err)
	assert.Equal(t, workflows.StateCancelled, run.State)
	assert.NotEmpty(t, run.Error)
	h.sendQueued(t)
	assert.Len(t, *h.sent, 1)
}

func (h *dmHarness) conversationID(t *testing.T) string {
	t.Helper()
	var id string
	require.NoError(t, h.db.NewSelect().Model((*models.Conversation)(nil)).Column("id").Limit(1).Scan(t.Context(), &id))
	return id
}

func TestCommentFollowUpEffectNeedsADeliveredPrivateReply(t *testing.T) {
	db := workflowHandlerDB(t)
	now := time.Now().UTC()
	_, err := db.NewInsert().Model(&models.SocialAccount{ID: "ig", WorkspaceID: "ws", Platform: "instagram", AccountID: "owner", Slug: "ig", AccessTokenEnc: []byte("token"), IsActive: true, CreatedAt: now}).Exec(t.Context())
	require.NoError(t, err)
	_, err = db.NewInsert().Model(&models.EngagementItem{ID: "comment", WorkspaceID: "ws", SocialAccountID: "ig", RemoteID: "remote", Body: "guide", AuthorRemoteID: "reader", Platform: "instagram", RemoteCreatedAt: now, CreatedAt: now, UpdatedAt: now}).Exec(t.Context())
	require.NoError(t, err)
	inbox := messaging.NewService(db, dmToken{}, nil)
	inbox.SetFeatureGate(dmGate{})
	actions := NewWorkflowActionsWithInbox(NewPublicationHandler(db, workflowSession{}, nil), nil, nil, engagement.NewService(db, nil, nil), inbox)
	_, err = actions.Execute(t.Context(), workflows.EffectRequest{Kind: workflows.KindMessageSequence, RunID: "run", StepID: "sequence", Authority: workspaceaccess.StoredAuthority{UserID: "user", WorkspaceID: "ws", OrganizationID: "org", AssuredAt: now}, Inputs: map[string]any{"engagement_id": "comment", "messages": `[{"text":"Hi"}]`, "delivery": "automatic", "workflow_id": "wf"}})
	require.ErrorIs(t, err, engagement.ErrNoDeliveredPrivateReply)
}

func TestDMHumanReplyWithNoRunStopsLaterAdmission(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmReplyStep("automatic")})
	now := time.Now().UTC().Add(time.Second)
	// An echo of what the account sent starts nothing, and it is a person replying.
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.1", "outbound", "owner", "guide", now)))
	h.poll(t)
	assert.Empty(t, h.runs(t))
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.2", "inbound", "reader", "guide", now.Add(time.Second))))
	h.poll(t)
	assert.Empty(t, h.runs(t))
}

func TestDMOptOutMessageNeverStartsARun(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmReplyStep("automatic")})
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.1", "inbound", "reader", "guide 수신거부합니다", time.Now().UTC().Add(time.Second))))
	h.poll(t)
	assert.Empty(t, h.runs(t))
}

func TestDMTakeoverCancelsARunWithNoReceiptYet(t *testing.T) {
	wait := workflows.Step{ID: "wait", Kind: workflows.KindWait, Name: "Wait", Inputs: map[string]workflows.Value{"minutes": {Literal: 60}}}
	h := newDMHarness(t, []workflows.Step{wait, dmReplyStep("automatic")})
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.1", "inbound", "reader", "guide", time.Now().UTC().Add(time.Second))))
	h.poll(t)
	run := h.advance(t, h.runs(t)[0].ID)
	require.Equal(t, workflows.StateWaiting, run.State)
	var conversation string
	require.NoError(t, h.db.NewSelect().Model((*models.Conversation)(nil)).Column("id").Where("social_account_id = 'ig'").Scan(t.Context(), &conversation))
	actor := messaging.Actor{UserID: "user"}
	require.NoError(t, h.inbox.StopConversationAutomation(t.Context(), actor, "ws", conversation))
	run, err := h.service.GetRun(t.Context(), h.actor, "ws", run.ID)
	require.NoError(t, err)
	assert.Equal(t, workflows.StateCancelled, run.State)
	assert.NotEmpty(t, run.Error)
	require.NoError(t, h.inbox.ResumeConversationAutomation(t.Context(), actor, "ws", conversation))
	assert.Equal(t, workflows.StateCancelled, h.advance(t, run.ID).State, "a stopped run stays stopped after resume")
	assert.Empty(t, *h.sent)
}

func TestDMApprovalDuringAHumanReplyReturnsTheOutcome(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmReplyStep("review")})
	now := time.Now().UTC().Add(time.Second)
	require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent("mid.1", "inbound", "reader", "guide", now)))
	h.poll(t)
	run := h.advance(t, h.runs(t)[0].ID)
	require.Equal(t, workflows.StateApproval, run.State)
	var conversation string
	require.NoError(t, h.db.NewSelect().Model((*models.Conversation)(nil)).Column("id").Where("social_account_id = 'ig'").Scan(t.Context(), &conversation))
	human := models.DirectMessage{ID: "human", WorkspaceID: "ws", ConversationID: conversation, RemoteMessageID: "mid.human", Direction: "outbound", AuthorRemoteID: "owner", Body: "I'll handle this", AttachmentsJSON: "[]", SendStatus: "sent", RemoteCreatedAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now}
	_, err := h.db.NewInsert().Model(&human).Exec(t.Context())
	require.NoError(t, err)
	stopped, err := h.service.Approve(t.Context(), h.actor, "ws", run.ID, run.Revision, 0, "Edited")
	require.NoError(t, err)
	assert.Equal(t, workflows.StateCancelled, stopped.State)
	assert.NotEmpty(t, stopped.Error)
	assert.Empty(t, *h.sent)
}

func TestDMFiveShortMessagesGetOneSequence(t *testing.T) {
	h := newDMHarness(t, []workflows.Step{dmSequenceStep("automatic")})
	time.Sleep(5 * time.Millisecond)
	now := time.Now().UTC()
	for i, id := range []string{"mid.1", "mid.2", "mid.3", "mid.4", "mid.5"} {
		require.NoError(t, h.inbox.ApplyProviderEvents(t.Context(), "ig", dmEvent(id, "inbound", "reader", "guide", now.Add(-time.Duration(5-i)*time.Millisecond))))
	}
	h.poll(t)
	runs := h.runs(t)
	require.Len(t, runs, 5)
	for range 4 {
		for _, run := range runs {
			h.advance(t, run.ID)
		}
		h.sendQueued(t)
	}
	assert.Len(t, *h.sent, 2, "one sequence, not five")
	skipped := 0
	for _, run := range h.runs(t) {
		current, err := h.service.GetRun(t.Context(), h.actor, "ws", run.ID)
		require.NoError(t, err)
		assert.Equal(t, workflows.StateSucceeded, current.State, "a skipped trigger is not a failure")
		if current.Steps[0].Output["reason"] == messaging.StopReasonSequenceActive {
			skipped++
		}
	}
	assert.Equal(t, 4, skipped)
}
