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
  stopped_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS direct_messages_automation_source_idx
  ON direct_messages (workspace_id, direction, created_at, id);
