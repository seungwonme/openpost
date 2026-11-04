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

## Fork-only paths

| Path | Purpose |
| --- | --- |
| `FORK.md` | This file |
| `docker-compose.macmini.yml` | Mac mini production overlay |
| `apps/docs/content/docs/self-hosting/macbook-macmini.mdx` | Mac mini runbook |
| `apps/server/internal/services/trackedlinks/` | Tracked redirect links (`/go/:slug`) |
