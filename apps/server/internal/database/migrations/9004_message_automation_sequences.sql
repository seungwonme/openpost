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
