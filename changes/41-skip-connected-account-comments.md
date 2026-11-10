### Fixed

- A comment campaign no longer answers comments written by an account connected to the same workspace. The comment is recorded as a cancelled run with the reason and the identity that matched (account ID, or a possible username match), Inbox shows "Skipped: connected account", and the check runs again before the reply and before any follow-up message is sent. Preview and test runs show a warning instead of skipping.
