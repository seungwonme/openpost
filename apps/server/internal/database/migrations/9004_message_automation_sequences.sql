-- Fork migration. An ordered follow-up sequence owns one receipt per position.
-- Position 0 still owns the triggering message, so a second workflow or a replayed
-- run cannot answer it twice. Later positions are identified by run, step and position.
ALTER TABLE message_automation_replies
  ADD COLUMN sequence_position INTEGER NOT NULL DEFAULT 0;

ALTER TABLE message_automation_replies
  ADD COLUMN sequence_length INTEGER NOT NULL DEFAULT 0;

ALTER TABLE message_automation_replies
  ADD COLUMN not_before TIMESTAMP;

ALTER TABLE message_automation_replies
  ADD COLUMN comment_item_id TEXT NOT NULL DEFAULT '';

DROP INDEX IF EXISTS message_automation_trigger_idx;

DROP INDEX IF EXISTS message_automation_step_idx;

CREATE UNIQUE INDEX IF NOT EXISTS message_automation_trigger_idx
  ON message_automation_replies (trigger_message_id)
  WHERE sequence_position = 0;

CREATE UNIQUE INDEX IF NOT EXISTS message_automation_step_idx
  ON message_automation_replies (run_id, step_id, sequence_position);

-- A run waiting for a person's response is parked until a batched check wakes it.
-- These runs can wait for days, so they do not count as active workspace runs.
CREATE TABLE IF NOT EXISTS message_sequence_waits (
  run_id TEXT PRIMARY KEY REFERENCES workflow_runs(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL,
  item_id TEXT NOT NULL,
  account_id TEXT NOT NULL,
  author_remote_id TEXT NOT NULL,
  replied_at TIMESTAMP NOT NULL,
  respond_by TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS message_sequence_waits_author_idx
  ON message_sequence_waits (account_id, author_remote_id);
