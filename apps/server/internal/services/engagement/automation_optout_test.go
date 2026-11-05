package engagement

import (
	"context"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/automationoptout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestCommentAuthorOptOutCanBeRemoved(t *testing.T) {
	s, _, _ := campaignTest(t)
	actor := Actor{UserID: "u"}
	now := time.Now().UTC().Truncate(time.Second)
	flagged := func() bool {
		t.Helper()
		items := []models.EngagementItem{}
		require.NoError(t, s.db.NewSelect().Model(&items).Where("id = 'c'").Scan(t.Context()))
		require.NoError(t, s.hydrateEngagementProviderURLs(t.Context(), items))
		return items[0].AuthorOptedOut
	}
	var account models.SocialAccount
	var rendition models.Rendition
	require.NoError(t, s.db.NewSelect().Model(&account).Where("id = 'a'").Scan(t.Context()))
	require.NoError(t, s.db.NewSelect().Model(&rendition).Where("id = 'r'").Scan(t.Context()))
	seeComment := func(at time.Time) {
		t.Helper()
		require.NoError(t, s.db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
			_, _, err := persistEngagementComment(ctx, tx, rendition, account, platform.Comment{ID: "stop-comment", AuthorID: "reader", Text: "stop", CreatedAt: at.Format(time.RFC3339)}, now)
			return err
		}))
	}
	seeComment(now.Add(-time.Hour))
	require.True(t, flagged(), "Inbox can offer removal without a request per comment")

	_, err := s.db.NewUpdate().Model((*models.WorkspaceMember)(nil)).Set("role = ?", models.WorkspaceRoleViewer).Where("workspace_id = 'w' AND user_id = 'u'").Exec(t.Context())
	require.NoError(t, err)
	require.Error(t, s.RemoveAutomationOptOut(t.Context(), actor, "w", "c"), "removal needs Edit")
	_, err = s.db.NewUpdate().Model((*models.WorkspaceMember)(nil)).Set("role = ?", models.WorkspaceRoleEditor).Where("workspace_id = 'w' AND user_id = 'u'").Exec(t.Context())
	require.NoError(t, err)

	require.NoError(t, s.RemoveAutomationOptOut(t.Context(), actor, "w", "c"))
	assert.False(t, flagged())
	state, err := s.AutomationOptOut(t.Context(), actor, "w", "c")
	require.NoError(t, err)
	assert.Nil(t, state.OptedOutAt)
	require.NotNil(t, state.RemovedAt)
	assert.NotEmpty(t, state.RemovedByName)

	seeComment(now.Add(-time.Hour))
	assert.False(t, flagged(), "the provider returning the same comment does not undo it")
	seeComment(time.Now().UTC().Add(time.Hour))
	assert.True(t, flagged(), "a newer refusal counts")
}

func TestRemovalAppliesToTheSameRecordOnBothChannels(t *testing.T) {
	s, _, _ := campaignTest(t)
	now := time.Now().UTC()
	_, err := automationoptout.Record(t.Context(), s.db, "a", "reader", now, now)
	require.NoError(t, err)
	state, err := s.AutomationOptOut(t.Context(), Actor{UserID: "u"}, "w", "c")
	require.NoError(t, err)
	assert.NotNil(t, state.OptedOutAt)
}
