package automationoptout

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openpost/backend/internal/database"
	"github.com/openpost/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func optOutDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := database.InitDBWithDriver("sqlite", fmt.Sprintf("file:optout-%d?mode=memory&cache=shared", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, database.CreateSchema(db))
	now := time.Now().UTC()
	for _, row := range []any{
		&models.User{ID: "user", Email: "editor@example.test", DisplayName: "Sample editor", PasswordHash: "hash", CreatedAt: now},
		&models.Organization{ID: "org", Name: "Org", CreatedByID: "user", CreatedAt: now, UpdatedAt: now},
		&models.Workspace{ID: "ws", Name: "Workspace", OrganizationID: "org", CreatedAt: now},
		&models.SocialAccount{ID: "ig", WorkspaceID: "ws", Platform: "instagram", AccountID: "owner", Slug: "ig", AccessTokenEnc: []byte("test"), IsActive: true, CreatedAt: now},
	} {
		_, err := db.NewInsert().Model(row).Exec(t.Context())
		require.NoError(t, err)
	}
	return db
}

func TestRemovalKeepsOlderRequestsRemoved(t *testing.T) {
	db := optOutDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	recorded, err := Record(t.Context(), db, "ig", "reader", now.Add(-time.Hour), now)
	require.NoError(t, err)
	require.True(t, recorded)
	repeated, err := Record(t.Context(), db, "ig", "reader", now.Add(-time.Hour), now)
	require.NoError(t, err)
	assert.False(t, repeated)

	require.NoError(t, db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		removed, err := Remove(ctx, tx, "ws", "ig", "reader", "user", now)
		assert.True(t, removed)
		return err
	}))
	state, err := Read(t.Context(), db, "ig", "reader")
	require.NoError(t, err)
	assert.Nil(t, state.OptedOutAt)
	require.NotNil(t, state.RemovedAt)
	assert.Equal(t, "Sample editor", state.RemovedByName)

	for name, requestedAt := range map[string]time.Time{"older": now.Add(-time.Hour), "unknown time": {}} {
		recorded, err = Record(t.Context(), db, "ig", "reader", requestedAt, now)
		require.NoError(t, err)
		assert.False(t, recorded, name)
	}
	recorded, err = Record(t.Context(), db, "ig", "reader", now.Add(time.Minute), now.Add(time.Minute))
	require.NoError(t, err)
	assert.True(t, recorded, "a newer refusal counts")
}

func TestRemovingNothingRecordsNothing(t *testing.T) {
	db := optOutDB(t)
	require.NoError(t, db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		removed, err := Remove(ctx, tx, "ws", "ig", "reader", "user", time.Now().UTC())
		assert.False(t, removed)
		return err
	}))
	count, err := db.NewSelect().Table("automation_optout_removals").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
}
