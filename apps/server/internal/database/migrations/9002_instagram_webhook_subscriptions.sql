CREATE TABLE IF NOT EXISTS instagram_webhook_subscriptions (
  social_account_id TEXT PRIMARY KEY REFERENCES social_accounts(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  fields TEXT NOT NULL DEFAULT '',
  last_attempted_at TIMESTAMP,
  last_success_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS instagram_webhook_subscriptions_workspace_idx
  ON instagram_webhook_subscriptions (workspace_id);
