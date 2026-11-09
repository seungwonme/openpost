### Added

- Collect Instagram comments on posts uploaded directly in the Instagram app, before Meta comment webhooks are available. OpenPost checks the 25 most recent posts about every five minutes and reads comments only where the comment count changed. The comments reach Inbox and comment workflows the same way webhook comments do, without duplicates when a webhook arrives later.

### Changed

- Instagram accounts watched by an active direct-message workflow are checked for new messages at least every 15 minutes instead of backing off to every six hours.
- An Instagram account connected without basic media access shows a reconnect notice under Comments, because collecting comments on posts uploaded outside OpenPost needs it.
