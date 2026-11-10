package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/stretchr/testify/require"
)

func TestCommentCampaignAdmitsOnlyNewMatchingCommentsOnce(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			s, actor := workflowTestServiceWithDB(t, nil, workflowRunListTestDB(t, backend))
			now := time.Now().UTC()
			_, err := s.db.NewInsert().Model(&models.SocialAccount{ID: "ig", WorkspaceID: "ws", Platform: "instagram", AccountID: "owner", AccessTokenEnc: []byte("test"), CreatedAt: now}).Exec(t.Context())
			require.NoError(t, err)
			var def Definition
			require.NoError(t, json.Unmarshal([]byte(`{"schema":1,"source":{"kind":"comment_received","account_ids":["ig"],"keywords":["GRIND"],"match_mode":"word","daily_limit":20},"steps":[{"id":"wait","kind":"wait","inputs":{"minutes":{"literal":0}}}]}`), &def))
			workflow, err := s.Save(t.Context(), actor, "ws", "", SaveRequest{Name: "Playbook", Definition: def})
			require.NoError(t, err)
			workflow, err = s.Publish(t.Context(), actor, "ws", workflow.ID, workflow.Revision)
			require.NoError(t, err)
			for _, item := range []models.EngagementItem{
				{ID: "match", Body: "GRIND please!", RemoteCreatedAt: now.Add(time.Minute)},
				{ID: "substring", Body: "grinding", RemoteCreatedAt: now.Add(time.Minute)},
				{ID: "old", Body: "GRIND", RemoteCreatedAt: now.Add(-time.Hour)},
				{ID: "own", Body: "GRIND", IsOurs: true, RemoteCreatedAt: now.Add(time.Minute)},
			} {
				item.WorkspaceID = "ws"
				item.SocialAccountID = "ig"
				item.Platform = "instagram"
				item.Kind = "comment"
				item.RemoteID = item.ID
				item.CanReply = true
				item.CreatedAt = now.Add(time.Minute)
				item.LastSeenAt = now
				_, err := s.db.NewInsert().Model(&item).Exec(t.Context())
				require.NoError(t, err)
			}
			require.NoError(t, s.poll(t.Context(), workflow.ID))
			require.NoError(t, s.poll(t.Context(), workflow.ID))
			runs, err := s.Runs(t.Context(), actor, "ws", workflow.ID)
			require.NoError(t, err)
			require.Len(t, runs, 1)
			require.Equal(t, "match", runs[0].Source["engagement_id"])
		})
	}
}

func TestPrivateReplyReviewPreservesItsTargetKind(t *testing.T) {
	var kinds []string
	actions := actionFunc(func(_ context.Context, in EffectRequest) (EffectResult, error) {
		kinds = append(kinds, in.Kind)
		return EffectResult{Review: true, Output: map[string]any{"text": "Playbook link", "channel": "private"}}, nil
	})
	s, actor := workflowTestService(t, actions)
	workflow := saveTestWorkflow(t, s, actor, []Step{{ID: "private", Kind: KindPrivateReply, Name: "Send privately", Inputs: map[string]Value{"engagement_id": literal("comment"), "text": literal("Playbook link"), "delivery": literal("review")}}})
	run, err := s.Start(t.Context(), actor, "ws", workflow.ID, ModeLive, map[string]any{}, workflow.Revision)
	require.NoError(t, err)
	runJob(t, s, run.ID)
	run, err = s.GetRun(t.Context(), actor, "ws", run.ID)
	require.NoError(t, err)
	require.Equal(t, StateApproval, run.State)
	_, err = s.Approve(t.Context(), actor, "ws", run.ID, run.Revision, 0, "Approved private link")
	require.NoError(t, err)
	require.Equal(t, []string{KindPrivateReply, KindPrivateReply}, kinds)
}

func TestCommentPreviewUsesRecentInventoryBeyondOldNonmatches(t *testing.T) {
	s, actor := workflowTestService(t, nil)
	now := time.Now().UTC()
	_, err := s.db.NewInsert().Model(&models.SocialAccount{ID: "ig", WorkspaceID: "ws", Platform: "instagram", AccountID: "owner", AccessTokenEnc: []byte("test"), CreatedAt: now}).Exec(t.Context())
	require.NoError(t, err)
	rows := make([]models.EngagementItem, 501)
	for index := range rows {
		id := fmt.Sprintf("comment-%04d", index)
		body := "Hello"
		if index == 500 {
			body = "GRIND"
		}
		rows[index] = models.EngagementItem{ID: id, WorkspaceID: "ws", SocialAccountID: "ig", Platform: "instagram", Kind: "comment", RemoteID: id, Body: body, CreatedAt: now.Add(time.Duration(index) * time.Second), RemoteCreatedAt: now, LastSeenAt: now}
	}
	_, err = s.db.NewInsert().Model(&rows).Exec(t.Context())
	require.NoError(t, err)
	items, err := s.Sample(t.Context(), actor, "ws", Source{Kind: "comment_received", AccountIDs: []string{"ig"}, Keywords: []string{"GRIND"}, DailyLimit: 20})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "comment-0500", items[0].ID)
}

func TestStopRepliesUsesPublishedCampaignAfterDraftSourceChanges(t *testing.T) {
	s, actor := workflowTestService(t, nil)
	workflow := saveTestWorkflow(t, s, actor, []Step{{ID: "wait", Kind: KindWait, Inputs: map[string]Value{"minutes": literal(1)}}})
	// A saved draft can differ from the still-active published definition.
	published := Definition{Schema: 1, Source: Source{Kind: "comment_received", AccountIDs: []string{"ig"}, DailyLimit: 20}, Steps: workflow.Definition.Steps}
	raw, err := json.Marshal(published)
	require.NoError(t, err)
	_, err = s.db.NewUpdate().Model((*workflowRecord)(nil)).Set("published_json=?,enabled=?", string(raw), true).Where("id=?", workflow.ID).Exec(t.Context())
	require.NoError(t, err)
	current, err := s.Get(t.Context(), actor, "ws", workflow.ID)
	require.NoError(t, err)
	require.True(t, current.HasCommentReplies)
	stopped, err := s.StopReplies(t.Context(), actor, "ws", workflow.ID, current.Revision)
	require.NoError(t, err)
	require.False(t, stopped.Enabled)
}

func connectedCommenterWorkflow(t *testing.T, backend string) (*Service, workspaceActor, Workflow) {
	t.Helper()
	s, actor := workflowTestServiceWithDB(t, nil, workflowRunListTestDB(t, backend))
	now := time.Now().UTC()
	for _, account := range []models.SocialAccount{
		{ID: "ig", Slug: "ig", WorkspaceID: "ws", Platform: "instagram", AccountID: "owner", AccountUsername: "owner_handle", IsActive: true},
		{ID: "sibling", Slug: "sibling", WorkspaceID: "ws", Platform: "instagram", AccountID: "sibling-remote", AccountUsername: "Sibling_Handle", IsActive: false},
		{ID: "other-platform", Slug: "other", WorkspaceID: "ws", Platform: "threads", AccountID: "threads-person", AccountUsername: "plain_reader"},
	} {
		account.AccessTokenEnc, account.CreatedAt = []byte("test"), now
		_, err := s.db.NewInsert().Model(&account).Exec(t.Context())
		require.NoError(t, err)
	}
	var def Definition
	require.NoError(t, json.Unmarshal([]byte(`{"schema":1,"source":{"kind":"comment_received","account_ids":["ig"],"keywords":["GRIND"],"match_mode":"word","daily_limit":20},"steps":[{"id":"wait","kind":"wait","inputs":{"minutes":{"literal":0}}}]}`), &def))
	workflow, err := s.Save(t.Context(), actor, "ws", "", SaveRequest{Name: "Playbook", Definition: def})
	require.NoError(t, err)
	workflow, err = s.Publish(t.Context(), actor, "ws", workflow.ID, workflow.Revision)
	require.NoError(t, err)
	return s, actor, workflow
}

type workspaceActor = workspaceaccess.ActorFacts

func insertComment(t *testing.T, s *Service, item models.EngagementItem) {
	t.Helper()
	now := time.Now().UTC()
	item.WorkspaceID, item.SocialAccountID, item.Platform, item.Kind = "ws", "ig", "instagram", "comment"
	item.RemoteID, item.CanReply, item.Body = item.ID, true, "GRIND please"
	item.RemoteCreatedAt, item.CreatedAt, item.LastSeenAt = now.Add(time.Minute), now.Add(time.Minute), now
	_, err := s.db.NewInsert().Model(&item).Exec(t.Context())
	require.NoError(t, err)
}

func TestCommentFromConnectedAccountIsRecordedAsCancelledRun(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			s, actor, workflow := connectedCommenterWorkflow(t, backend)
			insertComment(t, s, models.EngagementItem{ID: "sibling-comment", AuthorRemoteID: "sibling-remote", AuthorName: "someone_else"})
			insertComment(t, s, models.EngagementItem{ID: "person-comment", AuthorRemoteID: "person", AuthorName: "plain_reader"})

			require.NoError(t, s.poll(t.Context(), workflow.ID))
			require.NoError(t, s.poll(t.Context(), workflow.ID))

			runs, err := s.Runs(t.Context(), actor, "ws", workflow.ID)
			require.NoError(t, err)
			require.Len(t, runs, 2, "each comment is admitted once")
			byComment := map[string]Run{}
			for _, run := range runs {
				byComment[run.Source["engagement_id"].(string)] = run
			}
			skipped := byComment["sibling-comment"]
			require.Equal(t, StateCancelled, skipped.State)
			require.Contains(t, skipped.Error, ConnectedAccountSkipPrefix)
			require.Contains(t, skipped.Error, "Matched by account ID")
			require.Empty(t, skipped.Steps, "a skipped comment runs no steps")
			require.NotEqual(t, StateCancelled, byComment["person-comment"].State, "a normal comment on the same post still starts a run")
			require.Empty(t, byComment["person-comment"].Error)

			var events int
			events, err = s.db.NewSelect().Table("workflow_events").Where("workflow_id = ?", workflow.ID).Count(t.Context())
			require.NoError(t, err)
			require.Equal(t, 2, events, "the ledger advanced past the skipped comment")
		})
	}
}

func TestCommentHandleOnlyMatchIsSkippedAsPossible(t *testing.T) {
	s, actor, workflow := connectedCommenterWorkflow(t, "sqlite")
	insertComment(t, s, models.EngagementItem{ID: "renamed", AuthorRemoteID: "different-remote-id", AuthorHandle: "@sibling_handle"})
	require.NoError(t, s.poll(t.Context(), workflow.ID))
	runs, err := s.Runs(t.Context(), actor, "ws", workflow.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, StateCancelled, runs[0].State)
	require.Contains(t, runs[0].Error, "Possible match by username @sibling_handle")
	require.NotContains(t, runs[0].Error, "Matched by account ID")
}

func TestCommentIDMatchBeatsHandleMatchAndPlatformsStaySeparate(t *testing.T) {
	s, _, _ := connectedCommenterWorkflow(t, "sqlite")
	accounts, err := LoadConnectedAccounts(t.Context(), s.db, "ws")
	require.NoError(t, err)
	match, found := accounts.Match("instagram", "owner", "sibling_handle")
	require.True(t, found)
	require.Equal(t, "ig", match.SocialAccountID)
	require.Equal(t, connectedMatchID, match.By)
	_, found = accounts.Match("instagram", "threads-person", "plain_reader")
	require.False(t, found, "an account on another platform is not the same person")
	_, found = accounts.Match("instagram", "", "")
	require.False(t, found)
}

func TestConnectedCommentSkipSpendsNoDailyLimit(t *testing.T) {
	s, _, workflow := connectedCommenterWorkflow(t, "sqlite")
	insertComment(t, s, models.EngagementItem{ID: "sibling-comment", AuthorRemoteID: "sibling-remote"})
	require.NoError(t, s.poll(t.Context(), workflow.ID))
	record, err := s.loadWorkflow(t.Context(), "ws", workflow.ID)
	require.NoError(t, err)
	def := workflow.Definition
	def.Source.DailyLimit = 1
	capped, err := commentDailyLimitReached(t.Context(), s.db, record, def, time.Now().UTC())
	require.NoError(t, err)
	require.False(t, capped)
}

func TestConnectedCommenterShowsAWarningInPreview(t *testing.T) {
	s, actor := workflowTestService(t, nil)
	now := time.Now().UTC()
	_, err := s.db.NewInsert().Model(&models.SocialAccount{ID: "ig", Slug: "ig", WorkspaceID: "ws", Platform: "instagram", AccountID: "owner", AccessTokenEnc: []byte("test"), CreatedAt: now}).Exec(t.Context())
	require.NoError(t, err)
	_, err = s.db.NewInsert().Model(&models.EngagementItem{ID: "own-sibling", WorkspaceID: "ws", SocialAccountID: "ig", Platform: "instagram", Kind: "comment", RemoteID: "r", AuthorRemoteID: "owner", Body: "GRIND", CreatedAt: now, RemoteCreatedAt: now, LastSeenAt: now}).Exec(t.Context())
	require.NoError(t, err)
	workflow := saveTestWorkflow(t, s, actor, []Step{{ID: "private", Kind: KindPrivateReply, Name: "Send privately", Inputs: map[string]Value{"engagement_id": literal("own-sibling"), "text": literal("Link"), "delivery": literal("automatic")}}})
	run, err := s.Start(t.Context(), actor, "ws", workflow.ID, ModePreview, map[string]any{}, workflow.Revision)
	require.NoError(t, err)
	runJob(t, s, run.ID)
	run, err = s.GetRun(t.Context(), actor, "ws", run.ID)
	require.NoError(t, err)
	require.Equal(t, StateSucceeded, run.State, "a preview is not skipped")
	require.Contains(t, run.Steps[0].Output["warning"], ConnectedAccountSkipPrefix)
}
