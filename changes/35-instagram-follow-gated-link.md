### Added

- Instagram comment campaigns can send their link only to people who follow the account. Turn on **Follow check** in **Send follow-up messages**. When the person replies to the private reply, OpenPost asks Instagram whether they follow the account. Followers receive the first follow-up with a link button that opens your page. Others receive your request to follow and can reply again, up to the number of checks you choose. If Instagram cannot report the follow status, the run stops and shows why instead of treating the person as not following. Replies collected by polling start the check the same way webhook replies do. The private reply can carry a quick reply button such as "I followed", and the request to follow offers it again. A tap or a typed reply confirms by default; the follow check can count only taps. A new **Send a link to followers** template sets this up.
- Comment campaigns can answer comments written before activation. Set **Also include comments from the last N hours** under **Configure**, up to 168 hours. Each matching comment starts one run; comments already answered, taken over, from people who opted out, or older than seven days are skipped.

### Changed

- While a comment campaign waits for someone's reply, OpenPost checks that account's Instagram messages at least every two minutes instead of backing off on a quiet inbox, and moves an already scheduled later check forward. Provider rate-limit and failure retries keep their own delays.
- An automated Instagram message sent with a link button shows the button as a link in Inbox, and a quick reply as a label.
- Database migration 9006 adds `message_follow_checks` and `message_sequence_waits.response_text`. It is additive; pause comment workflows that use a follow check before downgrading.
