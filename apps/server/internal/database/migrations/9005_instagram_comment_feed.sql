-- Fork migration. Adds the Instagram `comments` account feed, which polls
-- comments on media published outside OpenPost. The feed CHECK cannot be
-- altered in SQLite, so the table is rebuilt. No table references it.
CREATE TABLE engagement_account_sync_states_9005 (
  social_account_id TEXT NOT NULL REFERENCES social_accounts(id) ON DELETE CASCADE,
  feed TEXT NOT NULL CHECK (feed IN ('mentions', 'reviews', 'comments')),
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  platform TEXT NOT NULL,
  generation TEXT NOT NULL,
  grant_id TEXT NOT NULL DEFAULT '',
  grant_consented_at TIMESTAMP,
  feature_decided_at TIMESTAMP,
  cursor TEXT NOT NULL DEFAULT '',
  cycle_started_at TIMESTAMP,
  watermark TIMESTAMP,
  pending_watermark TIMESTAMP,
  status TEXT NOT NULL DEFAULT 'pending',
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  last_attempted_at TIMESTAMP,
  last_success_at TIMESTAMP,
  next_sync_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(social_account_id, feed)
);

INSERT INTO engagement_account_sync_states_9005 (
  social_account_id, feed, workspace_id, platform, generation, grant_id, grant_consented_at, feature_decided_at,
  cursor, cycle_started_at, watermark, pending_watermark, status, error_code, error_message,
  last_attempted_at, last_success_at, next_sync_at, created_at, updated_at
)
SELECT
  social_account_id, feed, workspace_id, platform, generation, grant_id, grant_consented_at, feature_decided_at,
  cursor, cycle_started_at, watermark, pending_watermark, status, error_code, error_message,
  last_attempted_at, last_success_at, next_sync_at, created_at, updated_at
FROM engagement_account_sync_states;

DROP TABLE engagement_account_sync_states;

ALTER TABLE engagement_account_sync_states_9005 RENAME TO engagement_account_sync_states;

CREATE INDEX IF NOT EXISTS engagement_account_sync_workspace_idx
  ON engagement_account_sync_states (workspace_id, next_sync_at);
