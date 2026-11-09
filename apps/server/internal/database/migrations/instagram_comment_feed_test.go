package migrations

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestInstagramCommentFeedUpgradePreservesAccountFeeds(t *testing.T) {
	exerciseInstagramCommentFeedUpgrade(t, newMigrationsTestDB(t))
}

func TestInstagramCommentFeedUpgradePreservesAccountFeedsPostgres(t *testing.T) {
	exerciseInstagramCommentFeedUpgrade(t, newPostgresMigrationTestDB(t))
}

func exerciseInstagramCommentFeedUpgrade(t *testing.T, db *bun.DB) {
	t.Helper()
	previous := fstest.MapFS{}
	entries, err := migrationFiles.ReadDir(".")
	require.NoError(t, err)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := parseVersion(entry.Name())
		require.NoError(t, err)
		if version < 9005 {
			data, err := migrationFiles.ReadFile(entry.Name())
			require.NoError(t, err)
			previous[entry.Name()] = &fstest.MapFile{Data: data}
		}
	}
	require.NoError(t, runMigrations(db, previous))
	for _, statement := range []string{
		`INSERT INTO workspaces(id,name) VALUES('feed-ws','Feeds')`,
		`INSERT INTO social_accounts(id,workspace_id,slug,platform,account_id,access_token_encrypted) VALUES('feed-account','feed-ws','feed','instagram','owner',?)`,
		`INSERT INTO engagement_account_sync_states(social_account_id,feed,workspace_id,platform,generation,cursor,status) VALUES('feed-account','mentions','feed-ws','instagram','generation','page-2','rate_limited')`,
	} {
		var args []any
		if strings.Contains(statement, "access_token_encrypted") {
			args = []any{[]byte{0}}
		}
		_, err := db.ExecContext(t.Context(), statement, args...)
		require.NoError(t, err)
	}
	_, err = db.ExecContext(t.Context(), `INSERT INTO engagement_account_sync_states(social_account_id,feed,workspace_id,platform,generation) VALUES('feed-account','comments','feed-ws','instagram','generation')`)
	require.Error(t, err, "the comments feed is new in 9005")

	require.NoError(t, RunMigrations(db))
	require.NoError(t, RunMigrations(db))
	var cursor, status string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT cursor,status FROM engagement_account_sync_states WHERE social_account_id='feed-account' AND feed='mentions'`).Scan(&cursor, &status))
	require.Equal(t, "page-2", cursor)
	require.Equal(t, "rate_limited", status)
	_, err = db.ExecContext(t.Context(), `INSERT INTO engagement_account_sync_states(social_account_id,feed,workspace_id,platform,generation) VALUES('feed-account','comments','feed-ws','instagram','generation')`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO engagement_account_sync_states(social_account_id,feed,workspace_id,platform,generation) VALUES('feed-account','unknown','feed-ws','instagram','generation')`)
	require.Error(t, err, "the feed CHECK still rejects unknown feeds")
	_, err = db.ExecContext(t.Context(), `DELETE FROM social_accounts WHERE id='feed-account'`)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM engagement_account_sync_states`).Scan(&count))
	require.Zero(t, count, "account cascade remains enforced after the rebuild")
}
