### Added

- `openpost workflow` manages Workflows from the terminal: `list`, `get`, `create`, `save`, `publish`, `pause`, `stop-replies`, `delete`, `runs`, `run` (with `approve` and `cancel`), `sample-source` and `test-node`. Every command supports `--json`. Saves, publishes and run changes send the revision they were based on, and a revision conflict is reported as an error without overwriting anything. Commands that publish, send, cancel or delete ask for confirmation unless `--yes` is passed.
- MCP workflow tools: `list_workflows`, `get_workflow`, `list_workflow_runs` and `get_workflow_run` for reading, and `create_workflow`, `save_workflow`, `publish_workflow`, `pause_workflow`, `stop_workflow_replies`, `delete_workflow`, `approve_workflow_run` and `cancel_workflow_run` for changes. Change tools are marked destructive and need `mcp:full`.

### Fixed

- `openpost inbox list` prints a table, like the other list commands. `--json` still returns the complete server response.
- `openpost post list` shows `-` in the SCHEDULED column for posts without a schedule, including posts that were published immediately. It used to show `draft` for them.
