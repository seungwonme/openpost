### Changed

- YouTube connections now request only `youtube.upload`, `youtube.force-ssl`, and `yt-analytics.readonly`. The broader `youtube` and `youtube.readonly` scopes are no longer requested, and every YouTube Data API call OpenPost makes accepts `youtube.force-ssl`. Channels connected earlier keep their scopes and keep working without reconnecting. Update the scope list in Google Auth Platform data access to match.
- YouTube publish readiness now requires only `youtube.upload`. Importing existing videos requires `youtube.force-ssl` on new connections and still accepts `youtube` or `youtube.readonly` on older ones.
