# Account mentions and business reviews

Incoming mentions and Google Business reviews belong to Engagement. They can
exist without a Publication or Rendition. `engagement_items.kind` distinguishes
comments, mentions and reviews; `subject_json` stores normalized native reply
references or review context. Existing item IDs, account/remote identity, read
state, archive state and action receipts remain authoritative.

Account feeds use `engagement_account_sync_states`, separately from owned-post
comment checkpoints. A job reads at most three pages and persists each page,
continuation, notifications and Inbox journal atomically. Completed watermarks
advance only after exhaustion. Initial discovery retains the last 30 days;
subsequent scans overlap completed coverage by one minute. Interrupted cursors
survive budget exhaustion and failed later pages. Missing items never imply
deletion. Generation, current OAuth grant consent, feature activation and worker
lease fence commits. Page reads use stored state. X has no account-activity reader.

Bluesky retains URI/CID reply roots and stable author DIDs. Mastodon excludes
direct visibility from Engagement and preserves visibility when replying.
Threads stores opaque continuation tokens, never upstream URLs containing
credentials. Account feature readiness includes the required read scopes.

Google reviews use full account/location/review resource names and require
`business.manage`. Verified locations may support reviews without supporting
local posts. Anonymous and rating-only reviews are valid. Customer reviews
cannot be deleted. An owner reply is one replaceable value, limited to 4096
UTF-8 bytes, with moderation state independent of submission acceptance.
Unknown moderation and policy values remain inspectable.

Review action jobs resume through their provider-write fence after a worker crash.
Review writes use durable admission and provider-write receipts, serializing
replacement and removal per review. Ambiguous writes reconcile through a read
before any resend. A short propagation window preserves just-accepted local
receipts against older listing echoes. Pending moderation is rechecked
independently of review update timestamps, after listing pages, at most two
reviews per job. Read reconciliation can record an accepted write after the
initiating actor loses edit permission; fresh writes still require permission.

Migrations 171 and 172 preserve existing comment foreign keys and dependent receipts.
Native object identities deduplicate Bluesky comments and mention notifications
without replacing existing item IDs or opaque reply references.
PostgreSQL drops the Rendition non-null constraint. SQLite rebuilds the installed
table on one connection with foreign keys disabled outside the transaction,
preserves columns/indexes/triggers and verifies foreign keys before commit.
Migration 173 adds a stable job lease generation. Acquiring a job increments it;
heartbeats update only the liveness timestamp. Provider fences and local review
commits compare the acquisition generation, including when retry counters reset.
Both fresh chains and upgrades are covered by migration tests; PostgreSQL tests
use `OPENPOST_TEST_POSTGRES_URL` with an isolated schema.

## Instagram comments on native media (fork)

Instagram accounts also have a `comments` account feed for media published
outside OpenPost, until Meta delivers comment webhooks. One request lists the 25
most recent media with `comments_count`; comments are read only for media whose
count differs from the last count read, at most ten media per job. The feed
cursor stores those per-media counts, so a generation reset reads them again.
Media of published Renditions stay with owned-post collection, and media older
than 90 days are skipped. Comments are stored through the webhook persistence
path, with the media as conversation and subject, so a later webhook finds the
same item. Comments written before the feed started are stored without
notifications. The feed needs `instagram_basic` (or `instagram_business_basic`)
in addition to the comment scopes. Migration 9005 rebuilds
`engagement_account_sync_states` to allow the feed. Deleted comments are not
detected; missing items never imply deletion.
