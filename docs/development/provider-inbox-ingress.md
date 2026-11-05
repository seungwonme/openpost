# Connect official Inbox ingress

`platform` owns provider normalization. `services/providerinbox` owns signed Meta intake, encrypted receipts, and account-wide transport leases. The messaging service applies normalized batches and checkpoints atomically. Provider callbacks and streams never write canonical conversations directly.

## Register Meta callbacks

Register public GET and POST `/api/v1/provider-inbox/meta/{appID}` with `Service.MetaHandler`. The server constructs handlers from its normalized active provider configurations. `{appID}` is `platform.AppConfigMergeKey(app) + ":" + app.ClientID`, not the database row ID. For example, the Facebook callback ends in `facebook:CLIENT_ID`; direct Instagram Login ends in `instagram:instagram_login:CLIENT_ID`; Facebook Login and Unified Login for Instagram share `instagram:CLIENT_ID`, because Unified Login is the same oauth provider app with a `config_id`. Use the exact configured client ID and login mode. Callback handlers capture startup configuration. Restart after adding or changing an app, client secret, or verification token, including edits in administrator settings. Load these values from the credential owner:

| `MetaApp` field  | Source                                              |
| ---------------- | --------------------------------------------------- |
| `ID`             | Configured provider, login mode and client identity |
| `ClientID`       | Provider app client ID                              |
| `Platform`       | `facebook` or `instagram`                           |
| `ConnectionMode` | Stored connection mode, including `instagram_login` |
| `AppSecret`      | Decrypted `ClientSecretEnc`                         |
| `VerifyToken`    | Decrypted `WebhookSecretEnc`                        |

Facebook Login and Instagram Login use their respective application credentials. Do not substitute a Facebook app secret for a direct Instagram Login application. Configure the matching callback URL and webhook fields in the provider dashboard. Meta also delivers an Instagram account's events only after the app subscribes that account, so `services/instagramwebhook` posts `comments,messages` to the account's `/subscribed_apps` edge when Comments or Direct messages is turned on and again after a reconnect: Instagram Login uses `graph.instagram.com/{ig-id}`, Facebook Login uses the linked Page on `graph.facebook.com/{page-id}`, and Unified Login uses `graph.facebook.com/{ig-id}` with the System User token from the code exchange. Attempts are stored in `instagram_webhook_subscriptions`; only failures surface, as sync states in the Comments inbox (Comments on) and the Messages inbox (Direct messages on), and a failure never disconnects the account or reverts the switch. A connected publishing account does not establish messaging approval.

GET answers `hub.challenge` only when `hub.mode=subscribe` and the verification token matches. POST verifies `X-Hub-Signature-256` over the exact raw body, with a 1 MiB input limit. Keep the route outside session authentication and JSON body transformations. Meta uses subscription verification, rather than X's CRC protocol.

Intake resolves `entry.id` through active accounts and their current canonical OAuth grants. The grant must belong to the same Workspace, provider, and app client ID. Instagram's saved login mode must match. Unified Login accounts carry `connection_type: unified_login` with no Page, resolve by the Instagram account ID in `entry.id`, and match the oauth app. Message intake also requires the canonical messaging scopes for the account's connection type. All matched receipts commit in one transaction before acknowledgement. Receipt payloads are encrypted; neither signatures nor private message bodies belong in logs.

Install `SetReceiptScheduler` before serving callbacks. Its callback durably enqueues the coalesced `messages_sync` job with subject `provider-receipts:pending` after every acknowledged batch, including duplicate deliveries. The active-job index merges duplicate wakeups. An enqueue failure returns 503 after preserving the receipts, so a provider retry can enqueue again. A one-second stored-receipt scheduler also checks for pending work, so a receipt arriving while the drain job finishes cannot lose its wakeup. The regular sweep remains crash recovery. No configured scheduler means a bound receipt is persisted but the request returns 503.

## Drain receipts in worker roles

In `worker` and `all`, the durable drain job calls `ProcessPending(ctx, 100, processor)` and the X receipt owner. The one-second scheduler continues draining pending pages without keeping a worker occupied indefinitely. Failed receipts retain their encrypted payload and a bounded exponential retry delay. The periodic sweep covers delayed retries and abandoned processing leases. Call `Prune` daily to remove acknowledged receipt keys after seven days. Successful processing erases the payload immediately.

The processor receives `Event` with local account ID, grant ID, app ID, kind, and decrypted payload. Decode the payload as `providerinbox.MetaEvent`. Recheck account feature opt-in and current authorization through the owning services.

- For `message`, obtain the current account token and call `MetaMessagingWebhookAdapter.NormalizeMessagingWebhook` with the remote account ID and `MetaEvent.Messaging`. Pass its result to the messaging service's atomic apply method.
- For `engagement`, send `MetaEvent.Change` through the engagement owner and its durable collection path. Preserve comment identity and parent relationships there.

Callbacks must be idempotent. A process can stop after canonical application but before receipt completion. Grant replacement, revocation, account disablement, or invalid authorization prevents an old receipt from being applied to the replacement connection.

Message normalization resolves the canonical Graph conversation with `user_id`, then retains the webhook's message ID. REST history, send responses, and echoes therefore converge on the same stored identity. Echoes are outbound and do not renew the inbound reply window. Partial conversation metadata must preserve unread state, absent profile fields, a newer last message, and the maximum known reply-window expiry. Read-state actors can be the counterpart; only viewer reads clear viewer unread state.

## Run Mastodon streams under one lease

Schedule `RunLeasedStream` once per eligible account in `worker` or `all`. Its eligibility callback checks active account, messaging opt-in, and current authorization. Resolve a fresh token when its connect callback runs, then call `MessageStreamAdapter.StreamMessageEvents`.

The adapter discovers the streaming origin from instance metadata, including the legacy v1 metadata field. It requires HTTPS, validates public addresses at connection time, rejects redirects, and sends the bearer token in a header. The dedicated streaming client has a bounded handshake and response headers, a 90-second idle watchdog, and no whole-request timeout. Events and parser lines are bounded to 1 MiB.

The stream's connected callback must page REST `FetchMessages` until its cursor is empty and apply each page before returning. The stream is established first, so live traffic buffers during repair. A failed repair aborts the connection. Every reconnect repeats repair, with exponential backoff and jitter. Treat repeated or nonadvancing REST cursors as failed repair rather than a completed gap check.

The database lease lasts 60 seconds and renews every 20 seconds. A failed renewal or lost eligibility cancels the transport. An independent watchdog stops the upstream five seconds before the last confirmed expiry, even if a check stalls. Eligibility and lease operations have five-second deadlines. Connect callbacks must honor cancellation. Cancellation waits for the old consumer before releasing ownership. Stream deletes may carry only a remote message ID; resolve it inside the same account. Only direct-visibility statuses enter message history.

## Keep Bluesky discovery, live logs, and history separate

1. Call `IncrementalMessagingAdapter.FetchMessageEvents` with an empty cursor to capture a concrete head. `Bootstrap=true` contains no historical events. `ErrMessageBootstrapCursorRequired` means initialization cannot proceed safely.
2. Page `FetchMessages` for conversation discovery. It returns each conversation's latest supported message without fetching every conversation history.
3. Replay events from the captured head. Apply each batch and its `NextCursor` together. Follow `HasMore` until the log returns empty.
4. Use `MessageHistoryAdapter.FetchMessageHistory` only for older messages in a known conversation. Keep its cursor separate from the account log cursor. Facebook and both Instagram login modes also implement this history interface.

`getLog` has no limit parameter. OpenPost limits the processed prefix to at most 100 events and checkpoints its final revision, so the unprocessed tail is replayed. Deleted messages remain tombstones, viewer read state stays distinct from message authorship, and reactions retain add/remove order. Message snapshots set `ReactionsKnown` and carry complete `Reactions`. The persistence owner seeds the snapshot only on first message insertion, then applies live reaction events in order. Fill each reaction's conversation ID from its parent message. A history response started before a live removal must not resurrect the removed reaction. A confirmed `InvalidConvo` response retires an inaccessible conversation without blocking log progress; transport failures still abort the batch. Unknown log kinds may refresh conversation metadata without creating empty message bubbles. Preserve the last committed cursor on fetch or hydration failure.

The official [Bluesky log lexicon](https://github.com/bluesky-social/social-app/blob/main/lexicons/chat/bsky/convo/getLog.json) and [Mastodon streaming reference](https://docs.joinmastodon.org/methods/streaming/) document the underlying transports. These modules do not provision Bluesky or Mastodon subscriptions, prove provider approval, or enable unsupported networks.

## Verify the adapters and persistence

Run the provider and database suites in the project environment:

```sh
devenv shell -- bash -c 'cd apps/server && go test ./internal/platform ./internal/services/providerinbox ./internal/database'
```

`providerinbox` tests create fresh SQLite databases by default. Set `OPENPOST_TEST_POSTGRES_URL` to a disposable local PostgreSQL database to run the same tests in isolated temporary schemas. The tests drop those schemas on completion. No live provider account is required.
