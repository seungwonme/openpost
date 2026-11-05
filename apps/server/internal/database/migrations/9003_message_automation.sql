-- Fork migration. A workflow effect converges onto one owned outbound message.
-- trigger_message_id and (run_id, step_id) are identities: a retry, a replayed
-- job or a second workflow can never create another send for the same message.
CREATE TABLE IF NOT EXISTS message_automation_replies (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  trigger_message_id TEXT NOT NULL,
  workflow_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  step_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  body TEXT NOT NULL,
  state TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  message_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS message_automation_trigger_idx
  ON message_automation_replies (trigger_message_id);

CREATE UNIQUE INDEX IF NOT EXISTS message_automation_step_idx
  ON message_automation_replies (run_id, step_id);

CREATE INDEX IF NOT EXISTS message_automation_conversation_idx
  ON message_automation_replies (conversation_id, state, created_at);

CREATE INDEX IF NOT EXISTS message_automation_workflow_idx
  ON message_automation_replies (workflow_id, state);

CREATE INDEX IF NOT EXISTS message_automation_message_idx
  ON message_automation_replies (message_id);

-- Takeover is a conversation fact, so it stops automation that has not been
-- claimed yet. It stays until someone explicitly resumes automation there.
CREATE TABLE IF NOT EXISTS message_automation_stops (
  conversation_id TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  stopped_by TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  stopped_at TIMESTAMP NOT NULL,
  -- NULL stays until someone resumes. A takeover inferred from a person's message
  -- lapses when no further human message follows.
  lapses_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS direct_messages_automation_source_idx
  ON direct_messages (workspace_id, direction, created_at, id);

-- A message first stored from a history page is not a new event, whatever its
-- timestamps say, so it never starts a workflow.
CREATE TABLE IF NOT EXISTS message_history_imports (
  message_id TEXT PRIMARY KEY REFERENCES direct_messages(id) ON DELETE CASCADE
);

-- One automated send claims at most one echo stored under a different provider ID,
-- so a person repeating the same text afterwards still counts as a person.
-- A message can be merged away, so the claim outlives it.
CREATE TABLE IF NOT EXISTS message_automation_echoes (
  receipt_id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  message_id TEXT NOT NULL,
  receipt_state TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS message_automation_echoes_message_idx
  ON message_automation_echoes (message_id);

-- A message that may be asking to opt out hands the conversation to a person once.
-- Reading the message again after someone resumes does not hand it over again.
CREATE TABLE IF NOT EXISTS message_optout_reviews (
  message_id TEXT PRIMARY KEY REFERENCES direct_messages(id) ON DELETE CASCADE,
  created_at TIMESTAMP NOT NULL
);

-- Removing an opt-out is an operator decision, kept with who made it and when.
CREATE TABLE IF NOT EXISTS automation_optout_removals (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  social_account_id TEXT NOT NULL REFERENCES social_accounts(id) ON DELETE CASCADE,
  author_remote_id TEXT NOT NULL,
  removed_by TEXT NOT NULL,
  opted_out_at TIMESTAMP NOT NULL,
  removed_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS automation_optout_removals_author_idx
  ON automation_optout_removals (social_account_id, author_remote_id, removed_at);
