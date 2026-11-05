# Fork conventions

This repository is a personal fork of [getopenpost/openpost](https://github.com/getopenpost/openpost).
Everything outside this file and the paths listed below follows upstream rules in `AGENTS.md`.

## Keep upstream merges cheap

- Sync upstream with a merge commit: `git fetch upstream && git merge --no-ff upstream/main` on a
  branch, then a pull request into `main` merged with **Create a merge commit**. Never squash or
  rebase upstream commits; that rewrites their SHAs and the fork diverges again on the next sync.
- Prefer new files over edits to upstream files. When an upstream file must change, keep the edit
  small and close to the end of the file.
- `gh pr create` in a fork targets upstream by default. Run `gh repo set-default seungwonme/openpost`
  once per checkout, or pass `-R seungwonme/openpost`.

## Database migrations

Fork-only SQL migrations use versions **9001 and above** (`9001_tracked_links.sql`, then 9002, ...).
Upstream numbers its migrations sequentially from 1, and the runner rejects two files with the same
version (`loadMigrations` in `apps/server/internal/database/migrations/migrations.go`). A reserved
range keeps fork migrations from colliding with the next upstream release. The runner applies every
missing version in ascending order, so gaps are allowed and upstream migrations added later still run.

A database that has applied a fork migration should not be started with an upstream image. Restore
the backup taken before the fork deployment instead.

## Deploy to the Mac mini

Production runs on the Mac mini at `https://post.aidenahn.com`. The MacBook does not run the
app. Merge a change into `main`, then build and run it on the Mac mini:

```sh
ssh macmini '/Users/aiden/Dev/1-project/openpost/scripts/fork/macmini-deploy.sh --build'
rsync -a macmini:Backups/openpost/<timestamp>/ ~/Backups/openpost/<timestamp>/
```

Every deploy backs up the data volume, `.env`, and both Compose files to
`~/Backups/openpost/<timestamp>/` on the Mac mini, then restores that archive into a scratch volume
and checks the database before the new image starts. Roll back with `--restore <backup-dir>`, which
restores the data and the image it recorded. Do not schedule the script with launchd or cron. The
runbook is `apps/docs/content/docs/self-hosting/macbook-macmini.mdx`.

The overlay pins the Compose project name `openpost` and the external volume `openpost_openpost_data`, so the
checkout can move or be renamed without starting on an empty volume.

## Fork-only paths

| Path | Purpose |
| --- | --- |
| `FORK.md` | This file |
| `docker-compose.macmini.yml` | Mac mini production overlay |
| `scripts/fork/` | Mac mini deployment, backup, and restore |
| `apps/docs/content/docs/self-hosting/macbook-macmini.mdx` | Mac mini runbook |
| `apps/server/internal/services/trackedlinks/` | Tracked redirect links (`/go/:slug`) |
| `apps/server/internal/database/migrations/9003_message_automation.sql` | Instagram DM automation receipts and takeovers, and `message_history_imports` so history never starts a run |
| `apps/server/internal/services/automationoptout/` | Opt-out record shared by comments and direct messages, and its removal |
| `apps/server/internal/services/messaging/automation*.go` | Instagram DM automation: claim, dispatch checks, opt-out, takeover |
| `apps/server/internal/services/workflows/message_sources.go` | `message_received` source and `message_reply` validation |
| `apps/server/internal/api/handlers/message_automation.go`, `workflow_message_actions.go` | DM automation routes and workflow effects |
| `apps/web/src/lib/workflows/message-*`, `apps/web/src/lib/query/message-automation.ts`, `packages/query-catalog/src/message-automation.ts` | DM automation editor fields, review and takeover panel |
| `apps/docs/content/docs/workflows/examples/instagram-dm-reply.mdx` | DM automation guide |
