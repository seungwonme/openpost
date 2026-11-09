### Added

- Instagram comment campaigns can send their link only to people who follow the account. Turn on **Follow check** in **Send follow-up messages**. When the person replies to the private reply, OpenPost asks Instagram whether they follow the account. Followers receive the first follow-up with a link button that opens your page. Others receive your request to follow and can reply again, up to the number of checks you choose. If Instagram cannot report the follow status, the run stops and shows why instead of treating the person as not following. Replies collected by polling start the check the same way webhook replies do. The private reply can carry a quick reply button such as "I followed"; with one, only a tap on it counts as the confirmation, and the request to follow offers it again. A new **Send a link to followers** template sets this up.

### Changed

- An automated Instagram message sent with a link button shows the button as a link in Inbox, and a quick reply as a label.
- Database migration 9006 adds `message_follow_checks` and `message_sequence_waits.response_text`. It is additive; pause comment workflows that use a follow check before downgrading.
