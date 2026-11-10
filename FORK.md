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

| Path                                                                                                                                                                                               | Purpose                                                                                                             |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `FORK.md`                                                                                                                                                                                          | This file                                                                                                           |
| `docker-compose.macmini.yml`                                                                                                                                                                       | Mac mini production overlay                                                                                         |
| `scripts/fork/`                                                                                                                                                                                    | Mac mini deployment, backup, and restore                                                                            |
| `apps/docs/content/docs/self-hosting/macbook-macmini.mdx`                                                                                                                                          | Mac mini runbook                                                                                                    |
| `apps/server/internal/services/trackedlinks/`                                                                                                                                                      | Tracked redirect links (`/go/:slug`)                                                                                |
| `apps/server/internal/database/migrations/9003_message_automation.sql`                                                                                                                             | Instagram DM automation receipts and takeovers, and `message_history_imports` so history never starts a run         |
| `apps/server/internal/database/migrations/9004_message_automation_sequences.sql`                                                                                                                   | Instagram DM follow-up positions on the same receipts, and `message_sequence_waits` for runs waiting for a response |
| `apps/server/internal/services/automationoptout/`                                                                                                                                                  | Opt-out record shared by comments and direct messages, and its removal                                              |
| `apps/server/internal/services/messaging/automation*.go`                                                                                                                                           | Instagram DM automation: claim, dispatch checks, opt-out, takeover, collection cadence while a DM workflow is active |
| `apps/server/internal/services/workflows/message_sources.go`                                                                                                                                       | `message_received` source and `message_reply` validation                                                            |
| `apps/server/internal/services/engagement/automation_followup.go`, `apps/server/internal/services/workflows/message_sequence.go`, `apps/server/internal/api/handlers/workflow_message_sequence.go` | DM follow-up sequences: private reply anchor, validation, workflow effect                                           |
| `apps/server/internal/api/handlers/message_automation.go`, `workflow_message_actions.go`                                                                                                           | DM automation routes and workflow effects                                                                           |
| `apps/web/src/lib/workflows/message-*`, `apps/web/src/lib/query/message-automation.ts`, `packages/query-catalog/src/message-automation.ts`                                                         | DM automation editor fields, review and takeover panel                                                              |
| `apps/docs/content/docs/workflows/examples/instagram-dm-reply.mdx`                                                                                                                                 | DM automation guide                                                                                                 |
| `apps/server/internal/services/instagramwebhook/`                                                                                                                                                  | Subscribes Instagram accounts to Meta webhooks and records readiness                                                |
| `apps/server/internal/platform/instagram_webhook_subscription.go`                                                                                                                                  | Instagram `/subscribed_apps` request per login mode                                                                 |
| `apps/server/internal/platform/instagram_unified.go`                                                                                                                                               | Instagram Unified Login token detection and account discovery                                                       |
| `apps/server/internal/models/instagram_webhook_subscription.go`                                                                                                                                    | Subscription attempt model                                                                                          |
| `apps/server/internal/database/migrations/9002_instagram_webhook_subscriptions.sql`                                                                                                                | Subscription attempts table                                                                                         |
| `apps/web/static/about.html`, `apps/web/static/privacy.html`, `apps/web/static/terms.html`, `apps/web/static/tiktokkIio8XHwfOvrD5N1h5XwSPi5Vw824ajM.txt`, `apps/web/static/tiktokGvky3zADrNAANClBnIu3fXarJcPaGoSa.txt`, `apps/web/static/tiktok4UuSDPdOEw0kxnPejH0hzTN6zOFnSo3V.txt`, `apps/server/cmd/openpost/privacy_page_test.go` | Public bilingual about page (`/about`), privacy policy (`/privacy`) and terms of service (`/terms`), and the TikTok URL-prefix verification files |
| `apps/server/internal/platform/instagram_native_comments*.go` | Instagram recent media with comment counts, and the Instagram `comments` account feed |
| `apps/server/internal/services/engagement/instagram_native_comments*.go` | Polling comments on Instagram media published outside OpenPost, and the comment persistence shared with the webhook |
| `apps/server/internal/database/migrations/9005_instagram_comment_feed.sql`, `apps/server/internal/database/migrations/instagram_comment_feed_test.go` | Allows the `comments` feed in `engagement_account_sync_states` |
| `apps/server/internal/platform/instagram_follow_gate*.go`, `apps/server/internal/services/workflows/message_follow_gate*.go`, `apps/server/internal/services/engagement/automation_quick_reply*.go`, `apps/server/internal/database/migrations/9006_message_follow_checks.sql` | Follow check before a comment campaign's link: quick reply on the private reply, User Profile read, button template, validation and recorded checks |
| `apps/server/internal/services/workflows/comment_backfill*.go`, `apps/web/src/lib/workflows/comment-recent-hours-field.svelte`, `apps/server/internal/services/messaging/automation_cadence*.go` | Comment campaigns answering recent comments from before activation; two-minute message collection while a campaign waits for a reply |
| `apps/server/internal/services/workflows/run_job_wake*.go`, `apps/server/internal/api/handlers/workflow_follow_gate_wake_test.go` | Moves a woken run's already scheduled job forward, so a reply wakes a waiting comment campaign on the next sweep |
| `apps/server/internal/services/workflows/connected_accounts.go` | Connected-account commenter match shared by comment admission, the engagement write fence and comment-anchored DM follow-ups |
