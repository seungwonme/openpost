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

func TestCommentRefusalAfterARemoval(t *testing.T) {
	const instagramLayout = "2006-01-02T15:04:05-0700"
	setup := func(t *testing.T) (*Service, func(string, string), func() bool) {
		t.Helper()
		s, _, _ := campaignTest(t)
		var account models.SocialAccount
		var rendition models.Rendition
		require.NoError(t, s.db.NewSelect().Model(&account).Where("id = 'a'").Scan(t.Context()))
		require.NoError(t, s.db.NewSelect().Model(&rendition).Where("id = 'r'").Scan(t.Context()))
		see := func(id, createdAt string) {
			t.Helper()
			require.NoError(t, s.db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
				_, _, err := persistEngagementComment(ctx, tx, rendition, account, platform.Comment{ID: id, AuthorID: "reader", Text: "stop", CreatedAt: createdAt}, s.now())
				return err
			}))
		}
		optedOut := func() bool {
			t.Helper()
			exists, err := s.db.NewSelect().Table("engagement_automation_optouts").Where("social_account_id = 'a' AND author_remote_id = 'reader'").Exists(t.Context())
			require.NoError(t, err)
			return exists
		}
		return s, see, optedOut
	}
	remove := func(t *testing.T, s *Service) {
		t.Helper()
		require.NoError(t, s.RemoveAutomationOptOut(t.Context(), Actor{UserID: "u"}, "w", "c"))
	}
	t.Run("a new stop in Instagram's time format opts out again", func(t *testing.T) {
		s, see, optedOut := setup(t)
		base := time.Now().UTC().Truncate(time.Second)
		see("first", base.Add(-time.Hour).Format(instagramLayout))
		require.True(t, optedOut())
		remove(t, s)
		require.False(t, optedOut())
		see("first", base.Add(-time.Hour).Format(instagramLayout))
		assert.False(t, optedOut(), "the same comment seen again with its known old time does not undo the removal")
		see("second", base.Add(time.Hour).Format(instagramLayout))
		assert.True(t, optedOut())
	})
	t.Run("a new stop with no time opts out again, and seeing it again does not undo a removal", func(t *testing.T) {
		s, see, optedOut := setup(t)
		base := time.Now().UTC().Truncate(time.Second)
		see("first", base.Add(-time.Hour).Format(instagramLayout))
		remove(t, s)
		see("no-time", "")
		assert.True(t, optedOut(), "wrongly honoring is the safe direction")
		remove(t, s)
		see("no-time", "")
		assert.False(t, optedOut(), "first stored before the removal")
	})
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
