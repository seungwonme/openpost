package workflows

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publishCommentCampaign publishes a keyword campaign that answers privately,
// so polling also resolves overlap between campaigns.
func publishCommentCampaign(t *testing.T, s *Service, actor workspaceaccess.ActorFacts, hours int) Workflow {
	t.Helper()
	var def Definition
	require.NoError(t, json.Unmarshal(fmt.Appendf(nil, `{"schema":1,"source":{"kind":"comment_received","account_ids":["ig"],"keywords":["세팅"],"match_mode":"word","daily_limit":100,"include_recent_comments_hours":%d},"steps":[{"id":"private","kind":"private_comment_reply","name":"Private","inputs":{"engagement_id":{"reference":"source.engagement_id"},"text":{"literal":"Here is the link"},"delivery":{"literal":"review"}}}]}`, hours), &def))
	seedCommentAccount(t, s)
	workflow, err := s.Save(t.Context(), actor, "ws", "", SaveRequest{Name: "Setup link", Definition: def})
	require.NoError(t, err)
	workflow, err = s.Publish(t.Context(), actor, "ws", workflow.ID, workflow.Revision)
	require.NoError(t, err)
	return workflow
}

func seedCommentAccount(t *testing.T, s *Service) {
	t.Helper()
	_, err := s.db.NewInsert().Model(&models.SocialAccount{ID: "ig", WorkspaceID: "ws", Platform: "instagram", AccountID: "owner", AccessTokenEnc: []byte("test"), CreatedAt: time.Now().UTC()}).On("CONFLICT DO NOTHING").Exec(t.Context())
	require.NoError(t, err)
}

// collectComment stores a comment the way the Instagram feed does: written at
// written, first collected at collected.
func collectComment(t *testing.T, s *Service, id string, written, collected time.Time, edit func(*models.EngagementItem)) {
	t.Helper()
	seedCommentAccount(t, s)
	item := models.EngagementItem{ID: id, WorkspaceID: "ws", SocialAccountID: "ig", Platform: "instagram", Kind: "comment", RemoteID: id, AuthorRemoteID: "author-" + id, Body: "세팅", CanReply: true, RemoteCreatedAt: written, CreatedAt: collected, LastSeenAt: collected}
	if edit != nil {
		edit(&item)
	}
	_, err := s.db.NewInsert().Model(&item).Exec(t.Context())
	require.NoError(t, err)
}

func startedItems(t *testing.T, s *Service, actor workspaceaccess.ActorFacts, workflowID string) []string {
	t.Helper()
	runs, err := s.Runs(t.Context(), actor, "ws", workflowID)
	require.NoError(t, err)
	ids := []string{}
	for _, run := range runs {
		ids = append(ids, fmt.Sprint(run.Source["engagement_id"]))
	}
	return ids
}

func TestRecentCommentsOptionAnswersCommentsFromBeforeTheCampaign(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend+"/with the option", func(t *testing.T) {
			s, actor := workflowTestServiceWithDB(t, nil, workflowRunListTestDB(t, backend))
			now := time.Now().UTC()
			// Collected by the native feed before the campaign was published.
			collectComment(t, s, "recent", now.Add(-20*time.Hour), now.Add(-3*time.Hour), nil)
			collectComment(t, s, "outside-window", now.Add(-30*time.Hour), now.Add(-3*time.Hour), nil)
			collectComment(t, s, "answered-by-hand", now.Add(-20*time.Hour), now.Add(-3*time.Hour), func(item *models.EngagementItem) { item.RepliedAt = now.Add(-2 * time.Hour) })
			collectComment(t, s, "taken-over", now.Add(-20*time.Hour), now.Add(-3*time.Hour), func(item *models.EngagementItem) { item.AutomationStoppedAt = now.Add(-2 * time.Hour) })
			collectComment(t, s, "answered-by-a-campaign", now.Add(-20*time.Hour), now.Add(-3*time.Hour), nil)
			_, err := s.db.NewRaw(`INSERT INTO engagement_automation_replies(id,workspace_id,item_id,workflow_id,run_id,step_id,user_id,channel,body,item_body,state,created_at,updated_at) VALUES('receipt','ws','answered-by-a-campaign','earlier','earlier-run','private',?,'private','Here','세팅','sent',?,?)`, actor.UserID, now, now).Exec(t.Context())
			require.NoError(t, err)
			workflow := publishCommentCampaign(t, s, actor, 24)

			require.NoError(t, s.poll(t.Context(), workflow.ID))
			require.NoError(t, s.poll(t.Context(), workflow.ID))
			assert.Equal(t, []string{"recent"}, startedItems(t, s, actor, workflow.ID), "one run for the comment inside the window, and polling again starts none")
		})
		t.Run(backend+"/without the option", func(t *testing.T) {
			s, actor := workflowTestServiceWithDB(t, nil, workflowRunListTestDB(t, backend))
			now := time.Now().UTC()
			collectComment(t, s, "recent", now.Add(-20*time.Hour), now.Add(-3*time.Hour), nil)
			workflow := publishCommentCampaign(t, s, actor, 0)
			require.NoError(t, s.poll(t.Context(), workflow.ID))
			assert.Empty(t, startedItems(t, s, actor, workflow.ID))
		})
		t.Run(backend+"/never older than seven days", func(t *testing.T) {
			s, actor := workflowTestServiceWithDB(t, nil, workflowRunListTestDB(t, backend))
			now := time.Now().UTC()
			workflow := publishCommentCampaign(t, s, actor, 168)
			// The campaign started three days ago; its window reaches back ten.
			_, err := s.db.NewUpdate().Model((*workflowRecord)(nil)).Set("source_started_at = ?", now.Add(-72*time.Hour)).Where("id = ?", workflow.ID).Exec(t.Context())
			require.NoError(t, err)
			collectComment(t, s, "nine-days-old", now.Add(-9*24*time.Hour), now, nil)
			collectComment(t, s, "six-days-old", now.Add(-6*24*time.Hour), now, nil)
			require.NoError(t, s.poll(t.Context(), workflow.ID))
			assert.Equal(t, []string{"six-days-old"}, startedItems(t, s, actor, workflow.ID))
		})
	}
}

func TestRecentCommentsOptionIsBounded(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source Source
		issue  string
	}{
		{name: "off", source: Source{Kind: "comment_received"}},
		{name: "a week", source: Source{Kind: "comment_received", IncludeRecentCommentsHours: 168}},
		{name: "beyond a week", source: Source{Kind: "comment_received", IncludeRecentCommentsHours: 169}, issue: "168 hours"},
		{name: "negative", source: Source{Kind: "comment_received", IncludeRecentCommentsHours: -1}, issue: "168 hours"},
		{name: "another trigger", source: Source{Kind: "message_received", IncludeRecentCommentsHours: 24}, issue: "comment triggers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCommentBackfill(tc.source)
			if tc.issue == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.issue)
		})
	}
}
