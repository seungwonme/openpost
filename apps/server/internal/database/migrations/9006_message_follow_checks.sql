-- Fork migration. A comment campaign with a follow check reads the User Profile
-- API once per response. The recorded answer is final, so a replayed run never
-- reads again, and a failed read stays visible instead of counting as "not
-- following". Requests to follow are ordinary receipts in message_automation_replies.
CREATE TABLE IF NOT EXISTS message_follow_checks (
  run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
  step_id TEXT NOT NULL,
  attempt INTEGER NOT NULL,
  response_message_id TEXT NOT NULL,
  outcome TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  checked_at TIMESTAMP NOT NULL,
  PRIMARY KEY (run_id, step_id, attempt)
);
