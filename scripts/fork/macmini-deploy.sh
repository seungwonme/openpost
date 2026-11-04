#!/usr/bin/env bash
# Back up, deploy, rehearse, and restore the Mac mini OpenPost instance.
# Run it on the Mac mini from this checkout. From the MacBook:
#   ssh macmini '<checkout>/scripts/fork/macmini-deploy.sh --status'
# Every deploy and restore takes a checked backup first. It installs no schedules.
# Written for the macOS system bash (3.2): no associative arrays or mapfile.
set -euo pipefail

usage() {
	cat <<'EOF'
Usage: scripts/fork/macmini-deploy.sh <command>

  --status             Show the configured image, the running version, and readiness.
  --backup-only        Back up and rehearse a restore, then start the same container again.
  --image <ref>        Back up, then run a published image. Pin release images by digest.
  --build [<commit>]   Build <commit> (default: origin/main) here, back up, then run it.
  --rehearse <dir>     Restore a backup into a scratch volume and check its database.
  --restore <dir>      Back up the current state, then restore <dir> with the image it recorded.

Backups go to ${OPENPOST_BACKUP_ROOT:-~/Backups/openpost}/<timestamp>/ and hold the data
volume, .env, both Compose files, the image reference, and SHA256SUMS. Copy them off the
Mac mini afterwards; see FORK.md.
EOF
}

export PATH="/opt/homebrew/bin:/usr/local/bin:$HOME/.orbstack/bin:$PATH"
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

CONTAINER=openpost
VOLUME=openpost_openpost_data
COMPOSE_FILES=docker-compose.yml:docker-compose.macmini.yml
BACKUP_ROOT=${OPENPOST_BACKUP_ROOT:-$HOME/Backups/openpost}
BASE_URL=http://127.0.0.1:18081
DB=/data/db/openpost.db
COUNT_SQL="select 'users', count(*) from users;
select 'workspaces', count(*) from workspaces;
select 'social_accounts', count(*) from social_accounts;
select 'publications', count(*) from publications;
select 'renditions', count(*) from renditions;
select 'media_attachments', count(*) from media_attachments;"
BACKUP_DIR=

log() {
	printf '[%s] %s\n' "$(date '+%H:%M:%S')" "$*"
	if [ -n "$BACKUP_DIR" ] && [ -d "$BACKUP_DIR" ]; then
		printf '[%s] %s\n' "$(date '+%F %T')" "$*" >>"$BACKUP_DIR/deploy.log"
	fi
}

die() {
	log "ERROR: $*"
	exit 1
}

compose() {
	docker compose -f docker-compose.yml -f docker-compose.macmini.yml "$@"
}

env_value() {
	sed -n "s/^$1=//p" .env | tail -n 1
}

# Rewrites one key in .env without printing any other value.
set_env_value() {
	local tmp
	tmp=$(mktemp "$ROOT/.env.tmp.XXXXXX")
	{
		grep -v "^$1=" .env || true
		printf '%s=%s\n' "$1" "$2"
	} >"$tmp"
	chmod 600 "$tmp"
	mv "$tmp" .env
}

running_image_ref() {
	local id digest
	id=$(docker inspect "$CONTAINER" --format '{{.Image}}')
	digest=$(docker image inspect "$id" --format '{{range .RepoDigests}}{{.}}{{"\n"}}{{end}}' | head -n 1)
	if [ -n "$digest" ]; then
		printf '%s\n' "$digest"
	else
		docker inspect "$CONTAINER" --format '{{.Config.Image}}'
	fi
}

preflight() {
	local mounted
	[ -f .env ] || die ".env is missing in $ROOT"
	docker info >/dev/null 2>&1 || die "Docker is not running"
	docker inspect "$CONTAINER" >/dev/null 2>&1 || die "No $CONTAINER container exists; install it first"
	mounted=$(docker inspect "$CONTAINER" --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}')
	[ "$mounted" = "$VOLUME" ] || die "$CONTAINER mounts '$mounted' at /data, expected $VOLUME"
	if [ -z "$(env_value DEPLOY_IMAGE)" ]; then
		set_env_value DEPLOY_IMAGE "$(running_image_ref)"
		log "Recorded the running image as DEPLOY_IMAGE in .env"
	fi
	if [ "$(env_value COMPOSE_FILE)" != "$COMPOSE_FILES" ]; then
		set_env_value COMPOSE_FILE "$COMPOSE_FILES"
	fi
}

checksums_ok() {
	(cd "$1" && shasum -a 256 -c SHA256SUMS >/dev/null)
}

# Replaces the contents of volume $1 with backup directory $2, using image $3 as the helper.
restore_volume() {
	docker run --rm --user 0 --entrypoint sh -v "$1:/data" -v "$2:/backup:ro" "$3" -c \
		'find /data -mindepth 1 -delete && tar -xzf /backup/openpost_data.tgz -C /data && chown -R 1000:1000 /data'
}

# Leaves the container stopped; the caller starts or replaces it.
backup() {
	local helper
	BACKUP_DIR="$BACKUP_ROOT/$(date +%Y%m%d-%H%M%S)"
	mkdir -p "$BACKUP_DIR"
	chmod 700 "$BACKUP_ROOT" "$BACKUP_DIR"
	helper=$(docker inspect "$CONTAINER" --format '{{.Image}}')
	{
		printf 'created_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
		printf 'image=%s\n' "$(running_image_ref)"
		printf 'image_id=%s\n' "$helper"
		printf 'version=%s\n' "$(curl -fsS "$BASE_URL/api/v1/version" 2>/dev/null || echo unavailable)"
		printf 'volume=%s\n' "$VOLUME"
		printf 'checkout=%s\n' "$(git rev-parse HEAD)"
	} >"$BACKUP_DIR/manifest.txt"
	cp .env docker-compose.yml docker-compose.macmini.yml "$BACKUP_DIR/"
	log "Stopping $CONTAINER for a consistent copy"
	docker stop "$CONTAINER" >/dev/null
	docker run --rm --user 0 --entrypoint tar -v "$VOLUME:/data:ro" -v "$BACKUP_DIR:/backup" "$helper" \
		-czf /backup/openpost_data.tgz -C /data .
	(cd "$BACKUP_DIR" && shasum -a 256 manifest.txt .env docker-compose.yml docker-compose.macmini.yml openpost_data.tgz >SHA256SUMS)
	chmod 600 "$BACKUP_DIR"/* "$BACKUP_DIR/.env"
	log "Backup written to $BACKUP_DIR ($(du -h "$BACKUP_DIR/openpost_data.tgz" | cut -f1))"
}

# Restores a backup into a scratch volume and records the database check next to it.
rehearse() {
	local src=$1 helper scratch result
	checksums_ok "$src" || die "Checksums do not match in $src"
	helper=$(docker inspect "$CONTAINER" --format '{{.Image}}')
	scratch="openpost_rehearsal_$(date +%Y%m%d%H%M%S)"
	docker volume create "$scratch" >/dev/null
	restore_volume "$scratch" "$src" "$helper"
	result=$(docker run --rm --user 0 --entrypoint sqlite3 -v "$scratch:/data" "$helper" "$DB" 'PRAGMA integrity_check;')
	docker run --rm --user 0 --entrypoint sqlite3 -v "$scratch:/data" "$helper" "$DB" "$COUNT_SQL" >"$src/counts.txt"
	case "$scratch" in
	openpost_rehearsal_*) docker volume rm "$scratch" >/dev/null ;;
	esac
	chmod 600 "$src/counts.txt"
	[ "$result" = ok ] || die "Restore rehearsal failed integrity_check: $result"
	log "Restore rehearsal passed integrity_check; counts in $src/counts.txt"
}

wait_ready() {
	local i
	for i in $(seq 1 60); do
		if curl -fsS "$BASE_URL/api/v1/ready" >/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	return 1
}

live_sql() {
	docker exec "$CONTAINER" sqlite3 -readonly "$DB" "$1"
}

run_image() {
	local image=$1
	set_env_value DEPLOY_IMAGE "$image"
	log "Starting $image"
	compose up -d openpost
	if ! wait_ready; then
		log "Not ready after 120 seconds. Recent logs follow."
		compose logs --tail=80 openpost || true
		die "Roll back with: scripts/fork/macmini-deploy.sh --restore $BACKUP_DIR"
	fi
	log "Ready: $(curl -fsS "$BASE_URL/api/v1/version")"
	live_sql "$COUNT_SQL" >"$BACKUP_DIR/counts-after.txt"
	chmod 600 "$BACKUP_DIR/counts-after.txt"
	if diff -q "$BACKUP_DIR/counts.txt" "$BACKUP_DIR/counts-after.txt" >/dev/null; then
		log "Row counts match the backup"
	else
		log "Row counts differ from the backup:"
		diff "$BACKUP_DIR/counts.txt" "$BACKUP_DIR/counts-after.txt" || true
	fi
	log "Latest applied migration: $(live_sql 'select max(version) from schema_migrations;')"
	log "Previous state: $BACKUP_DIR"
}

build_image() {
	local ref=${1:-origin/main} sha short version
	[ -z "$(git status --porcelain --untracked-files=no)" ] || die "Tracked files are modified in $ROOT"
	git fetch --quiet origin
	git fetch --quiet --tags upstream 2>/dev/null || true
	sha=$(git rev-parse --verify "$ref^{commit}")
	git merge-base --is-ancestor "$sha" origin/main || die "$sha is not on origin/main; merge it first"
	if [ "$sha" = "$(git rev-parse origin/main)" ]; then
		git checkout --quiet main 2>/dev/null || git checkout --quiet -b main --track origin/main
		git merge --ff-only --quiet origin/main
	else
		git checkout --quiet --detach "$sha"
	fi
	short=$(git rev-parse --short=9 "$sha")
	version="$(git describe --tags --abbrev=0 --match 'v*' "$sha" 2>/dev/null || echo v0.0.0)-fork.$short"
	BUILT_IMAGE="local/openpost:fork-$short"
	log "Building $BUILT_IMAGE ($version) from $sha"
	bun install --frozen-lockfile
	POSTHOG_SOURCEMAPS_ENABLED=0 bun run build -- frontend
	docker buildx build --load --platform linux/arm64 \
		--build-context frontend_artifact=apps/server/cmd/openpost/public \
		--file deploy/docker/Dockerfile \
		--tag "$BUILT_IMAGE" \
		--build-arg VERSION="$version" \
		--build-arg COMMIT="$sha" \
		.
}

status() {
	printf 'DEPLOY_IMAGE=%s\n' "$(env_value DEPLOY_IMAGE)"
	printf 'running=%s\n' "$(running_image_ref 2>/dev/null || echo none)"
	printf 'version=%s\n' "$(curl -fsS "$BASE_URL/api/v1/version" 2>/dev/null || echo unavailable)"
	printf 'ready=%s\n' "$(curl -fsS "$BASE_URL/api/v1/ready" 2>/dev/null || echo unavailable)"
}

# The whole body is parsed before it runs, so a git checkout that rewrites this
# file during --build cannot change the commands being executed.
main() {
	local command=${1:-} source_dir image
	case "$command" in
	--status)
		status
		;;
	--backup-only)
		preflight
		backup
		rehearse "$BACKUP_DIR"
		docker start "$CONTAINER" >/dev/null
		wait_ready || die "$CONTAINER did not become ready after the backup"
		log "Running again; backup at $BACKUP_DIR"
		;;
	--image)
		[ -n "${2:-}" ] || die "--image needs an image reference"
		preflight
		docker pull "$2"
		backup
		rehearse "$BACKUP_DIR"
		run_image "$2"
		;;
	--build)
		preflight
		BUILT_IMAGE=
		build_image "${2:-}"
		backup
		rehearse "$BACKUP_DIR"
		run_image "$BUILT_IMAGE"
		;;
	--rehearse)
		[ -d "${2:-}" ] || die "--rehearse needs a backup directory"
		rehearse "$2"
		;;
	--restore)
		[ -d "${2:-}" ] || die "--restore needs a backup directory"
		source_dir=$(cd "$2" && pwd)
		checksums_ok "$source_dir" || die "Checksums do not match in $source_dir"
		image=$(sed -n 's/^image=//p' "$source_dir/manifest.txt")
		[ -n "$image" ] || die "No image recorded in $source_dir/manifest.txt"
		docker image inspect "$image" >/dev/null 2>&1 || docker pull "$image"
		preflight
		backup
		rehearse "$BACKUP_DIR"
		log "Restoring $source_dir into $VOLUME"
		restore_volume "$VOLUME" "$source_dir" "$image"
		if [ -f "$source_dir/counts.txt" ]; then cp "$source_dir/counts.txt" "$BACKUP_DIR/counts.txt"; fi
		run_image "$image"
		;;
	-h | --help)
		usage
		;;
	*)
		usage >&2
		exit 2
		;;
	esac
}

main "$@"
