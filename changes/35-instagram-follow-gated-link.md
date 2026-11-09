### Added

- Instagram comment campaigns can send their link only to people who follow the account. Turn on **Follow check** in **Send follow-up messages**. When the person replies to the private reply, OpenPost asks Instagram whether they follow the account. Followers receive the first follow-up with a link button that opens your page. Others receive your request to follow and can reply again, up to the number of checks you choose. If Instagram cannot report the follow status, the run stops and shows why instead of treating the person as not following. Replies collected by polling start the check the same way webhook replies do.

### Changed

- An automated Instagram message sent with a link button shows the button as a link in Inbox.
- Database migration 9006 adds `message_follow_checks`. It is additive; pause comment workflows that use a follow check before downgrading.
