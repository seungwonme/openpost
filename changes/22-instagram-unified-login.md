### Added

- Instagram accounts with no Facebook Page can connect through Unified Login. OpenPost reads the token type after the code exchange, lists the accounts the business shared, and uses the Instagram account ID for publishing, comments, private replies, messages, analytics, and webhook subscription.

### Changed

- A `config_id` connection now identifies its token before continuing. A user token keeps the linked Page flow. The check is repeated once after a Meta outage or network failure, and the connection stops with an error if Meta still does not confirm the type.
- A `config_id` connection that finds no Instagram account now explains both causes: no linked Page, or a configuration that issues a user token.
- The login dialog for a `config_id` connection always shows the login step, so people already logged in to Facebook can choose Instagram.
- Refresh jobs for grants whose provider has no refresh flow now end instead of retrying. The account asks for a reconnect when its token stops working.
- The Instagram setup guide has a Unified Login section covering configuration permissions, token type, and the business portfolio requirement.
