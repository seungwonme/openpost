### Added

- Instagram accounts with no Facebook Page can connect through Unified Login. OpenPost reads the token type after the code exchange, lists the accounts the business shared, and uses the Instagram account ID for publishing, comments, private replies, messages, analytics, and webhook subscription.

### Changed

- A `config_id` connection now identifies its token before continuing. A user token keeps the linked Page flow, and the connection stops with an error if Meta does not confirm the type.
- Refresh jobs for grants whose provider has no refresh flow now end instead of retrying. The account asks for a reconnect when its token stops working.
- The Instagram setup guide has a Unified Login section covering configuration permissions, token type, and the business portfolio requirement.
