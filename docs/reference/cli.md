# CLI Reference

This page is generated from the Cobra command tree. Do not edit it by hand.

Regenerate with:

```sh
cd apps/cli && go run ./cmd/openpost-docs ../docs/reference/cli.md
```

## `openpost`

OpenPost CLI — control an OpenPost workspace from the terminal

openpost is a command-line client for the OpenPost social media scheduler. It talks to a running OpenPost instance over HTTPS, authenticates with a revocable API token, and exposes the most common posting, scheduling, account, and media workflows for use from scripts, CI, and power-user shells.

**Usage**

```text
openpost [flags]
```

**Flags**

| Flag            | Default | Description                                                        |
| --------------- | ------- | ------------------------------------------------------------------ |
| `-h, --help`    | `false` | help for openpost                                                  |
| `--instance`    | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`        | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`    | `false` | disable ANSI colors                                                |
| `--profile`     | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`       | `false` | suppress non-error output                                          |
| `--token`       | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `-v, --version` | `false` | version for openpost                                               |
| `--workspace`   | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`         | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                | Description                                                      |
| ---------------------- | ---------------------------------------------------------------- |
| `openpost account`     | Manage connected social accounts                                 |
| `openpost auth`        | Authenticate with an OpenPost instance                           |
| `openpost billing`     | Manage Hosted service billing                                    |
| `openpost completion`  | Generate shell completion script                                 |
| `openpost doctor`      | Check local CLI configuration without contacting an instance     |
| `openpost inbox`       | Read and act on comments, incoming mentions and business reviews |
| `openpost instance`    | Manage OpenPost instance profiles                                |
| `openpost jobs`        | List background jobs                                             |
| `openpost media`       | Upload and list media attachments                                |
| `openpost post`        | Create, list, view, update, and delete posts                     |
| `openpost provider`    | Inspect provider availability and publishing support             |
| `openpost publication` | Create, list, validate, and publish publications                 |
| `openpost schedule`    | Manage reusable posting schedule slots                           |
| `openpost thread`      | Create multi-post threads                                        |
| `openpost version`     | Print the openpost CLI version                                   |
| `openpost workflow`    | Create, publish, pause and inspect workflows                     |
| `openpost workspace`   | Manage the active OpenPost workspace                             |

### `openpost account`

Manage connected social accounts

List, rename, and disconnect social accounts. Account slugs are the preferred selector for --accounts. New accounts are connected in the OpenPost web UI at &lt;instance&gt;/settings?tab=accounts.

**Usage**

```text
openpost account
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                       | Description                                   |
| ----------------------------- | --------------------------------------------- |
| `openpost account disconnect` | Disconnect a social account                   |
| `openpost account list`       | List connected social accounts                |
| `openpost account mentions`   | Find native mention identities for an account |
| `openpost account rename`     | Rename a social account slug                  |

### `openpost account disconnect`

Disconnect a social account

**Usage**

```text
openpost account disconnect &lt;account-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost account list`

List connected social accounts

List connected social accounts for the active workspace. Use the SLUG column as the preferred selector for --accounts and account rename.

**Usage**

```text
openpost account list [flags]
```

**Flags**

| Flag         | Default | Description        |
| ------------ | ------- | ------------------ |
| `--platform` | `-`     | filter by platform |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost account mentions`

Find native mention identities for an account

**Usage**

```text
openpost account mentions &lt;account&gt; [flags]
```

**Flags**

| Flag      | Default | Description                                           |
| --------- | ------- | ----------------------------------------------------- |
| `--query` | `-`     | Account search, or exact LinkedIn company vanity name |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost account rename`

Rename a social account slug

Rename a connected account's slug. The selector can be an account id, slug, platform:username value, bare platform when unambiguous, or mastodon host.

**Usage**

```text
openpost account rename &lt;selector&gt; --slug &lt;new-slug&gt; [flags]
```

**Flags**

| Flag     | Default | Description      |
| -------- | ------- | ---------------- |
| `--slug` | `-`     | new account slug |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost auth`

Authenticate with an OpenPost instance

**Usage**

```text
openpost auth
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                | Description                                       |
| ---------------------- | ------------------------------------------------- |
| `openpost auth login`  | Log in to an OpenPost instance                    |
| `openpost auth logout` | Delete the stored token for the active profile    |
| `openpost auth status` | Show authentication status for the active profile |
| `openpost auth token`  | Manage API tokens                                 |

### `openpost auth login`

Log in to an OpenPost instance

**Usage**

```text
openpost auth login &lt;instance&gt; [flags]
```

**Flags**

| Flag                 | Default | Description                                                   |
| -------------------- | ------- | ------------------------------------------------------------- |
| `--device`           | `false` | print the device code and poll without opening a browser      |
| `--insecure-storage` | `false` | store the token in credentials.json instead of the OS keyring |
| `--no-browser`       | `false` | skip automatically opening the browser                        |
| `--with-token`       | `false` | read a raw API token from stdin                               |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost auth logout`

Delete the stored token for the active profile

**Usage**

```text
openpost auth logout
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost auth status`

Show authentication status for the active profile

**Usage**

```text
openpost auth status
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost auth token`

Manage API tokens

**Usage**

```text
openpost auth token
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                      | Description         |
| ---------------------------- | ------------------- |
| `openpost auth token list`   | List API tokens     |
| `openpost auth token revoke` | Revoke an API token |

### `openpost auth token list`

List API tokens

**Usage**

```text
openpost auth token list
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost auth token revoke`

Revoke an API token

**Usage**

```text
openpost auth token revoke &lt;id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost billing`

Manage Hosted service billing

Inspect billing status and create hosted checkout or customer portal URLs for the active workspace.

**Usage**

```text
openpost billing
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                     | Description                                              |
| --------------------------- | -------------------------------------------------------- |
| `openpost billing checkout` | Create an OpenPost checkout URL for the active workspace |
| `openpost billing portal`   | Open Paddle billing management for the active workspace  |
| `openpost billing status`   | Show billing plan and usage for the active workspace     |

### `openpost billing checkout`

Create an OpenPost checkout URL for the active workspace

Create an OpenPost checkout URL that opens Paddle's embedded payment form. Plan IDs are founder (Solo), team, or agency.

**Usage**

```text
openpost billing checkout &lt;plan&gt; [flags]
```

**Flags**

| Flag               | Default   | Description                       |
| ------------------ | --------- | --------------------------------- |
| `--billing-period` | `monthly` | Billing period: monthly or annual |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost billing portal`

Open Paddle billing management for the active workspace

**Usage**

```text
openpost billing portal
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost billing status`

Show billing plan and usage for the active workspace

**Usage**

```text
openpost billing status
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost completion`

Generate shell completion script

Output a shell completion script for the given shell. To load completions: Bash: $ source &lt;(openpost completion bash)    # To load completions for each session, execute once:   # Linux:   $ openpost completion bash &gt; /etc/bash_completion.d/openpost # macOS: $ openpost completion bash &gt; $(brew --prefix)/etc/bash_completion.d/openpost Zsh: # If shell completion is not already enabled in your environment, # you will need to enable it. You can execute the following once: $ echo "autoload -U compinit; compinit" &gt;&gt; ~/.zshrc    # To load completions for each session, execute once:   $ openpost completion zsh &gt; "${fpath[1]}/_openpost"    # You will need to start a new shell for this setup to take effect.  Fish:   $ openpost completion fish \| source # To load completions for each session, execute once: $ openpost completion fish &gt; ~/.config/fish/completions/openpost.fish PowerShell: PS&gt; openpost completion powershell \| Out-String \| Invoke-Expression # To load completions for every new session, run: PS&gt; openpost completion powershell &gt; openpost.ps1 # and source this file from your PowerShell profile.

**Usage**

```text
openpost completion &lt;bash\|zsh\|fish\|powershell&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost doctor`

Check local CLI configuration without contacting an instance

**Usage**

```text
openpost doctor
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost inbox`

Read and act on comments, incoming mentions and business reviews

**Usage**

```text
openpost inbox
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                    | Description                                                     |
| -------------------------- | --------------------------------------------------------------- |
| `openpost inbox act`       | Queue a supported reply or moderation action                    |
| `openpost inbox get`       | Read an Inbox item, native context and review reply receipt     |
| `openpost inbox list`      | Read stored Inbox items and feed health, without provider calls |
| `openpost inbox mark-read` | Mark every matching unread item read across all pages           |
| `openpost inbox refresh`   | Queue durable collection for enabled accounts                   |
| `openpost inbox state`     | Update shared Workspace read and archive state                  |

### `openpost inbox act`

Queue a supported reply or moderation action

Queue an action supported by the item's can_* capabilities. For a Google review, reply replaces the owner reply and delete_reply removes only that reply. Reviews cannot be deleted. Inspect the returned job and the item's moderation state before replacing a review reply again. Reuse --operation-id only for the same intent.

**Usage**

```text
openpost inbox act &lt;item-id&gt; [flags]
```

**Flags**

| Flag             | Default | Description                                                |
| ---------------- | ------- | ---------------------------------------------------------- |
| `--action`       | `reply` | reply, delete_reply, hide, delete, like or unlike          |
| `--message`      | `-`     | reply text; Google owner replies allow 4096 UTF-8 bytes    |
| `--operation-id` | `-`     | stable ID for this exact intent, required for safe retries |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost inbox get`

Read an Inbox item, native context and review reply receipt

**Usage**

```text
openpost inbox get &lt;item-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost inbox list`

Read stored Inbox items and feed health, without provider calls

**Usage**

```text
openpost inbox list [flags]
```

**Flags**

| Flag               | Default | Description                                                     |
| ------------------ | ------- | --------------------------------------------------------------- |
| `--account-id`     | `-`     | connected account ID                                            |
| `--archived`       | `false` | read archived items instead of active items                     |
| `--cursor`         | `-`     | next_cursor from the previous response                          |
| `--kind`           | `-`     | comment, mention or review; omit for all                        |
| `--limit`          | `50`    | page size, from 1 to 100                                        |
| `--platform`       | `-`     | filter by provider                                              |
| `--publication-id` | `-`     | filter by an OpenPost post, excluding unrelated native activity |
| `--unread`         | `false` | only unread items                                               |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost inbox mark-read`

Mark every matching unread item read across all pages

**Usage**

```text
openpost inbox mark-read [flags]
```

**Flags**

| Flag               | Default | Description                                                     |
| ------------------ | ------- | --------------------------------------------------------------- |
| `--account-id`     | `-`     | connected account ID                                            |
| `--archived`       | `false` | read archived items instead of active items                     |
| `--kind`           | `-`     | comment, mention or review; omit for all                        |
| `--platform`       | `-`     | filter by provider                                              |
| `--publication-id` | `-`     | filter by an OpenPost post, excluding unrelated native activity |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost inbox refresh`

Queue durable collection for enabled accounts

**Usage**

```text
openpost inbox refresh
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost inbox state`

Update shared Workspace read and archive state

**Usage**

```text
openpost inbox state &lt;item-id&gt;... [flags]
```

**Flags**

| Flag         | Default | Description                               |
| ------------ | ------- | ----------------------------------------- |
| `--archived` | `false` | archive, or --archived=false to reopen    |
| `--read`     | `false` | mark read, or --read=false to mark unread |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost instance`

Manage OpenPost instance profiles

**Usage**

```text
openpost instance
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                         | Description                                              |
| ------------------------------- | -------------------------------------------------------- |
| `openpost instance add`         | Add or update an instance profile                        |
| `openpost instance diagnostics` | Collect a safe support snapshot for an OpenPost instance |
| `openpost instance health`      | Check the active instance liveness and readiness         |
| `openpost instance list`        | List configured instances                                |
| `openpost instance remove`      | Remove an instance profile                               |
| `openpost instance use`         | Set the active instance profile                          |

### `openpost instance add`

Add or update an instance profile

**Usage**

```text
openpost instance add &lt;name&gt; &lt;url&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost instance diagnostics`

Collect a safe support snapshot for an OpenPost instance

**Usage**

```text
openpost instance diagnostics [flags]
```

**Flags**

| Flag           | Default | Description                                                           |
| -------------- | ------- | --------------------------------------------------------------------- |
| `--deployment` | `-`     | deployment method being checked (docker, binary, nixos, cloud, other) |
| `--logs-file`  | `-`     | local OpenPost log file to include as a redacted last-100-line tail   |
| `--provider`   | `-`     | social provider being tested, such as x, mastodon, youtube, or tiktok |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost instance health`

Check the active instance liveness and readiness

**Usage**

```text
openpost instance health
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost instance list`

List configured instances

**Usage**

```text
openpost instance list
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost instance remove`

Remove an instance profile

**Usage**

```text
openpost instance remove &lt;name&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost instance use`

Set the active instance profile

**Usage**

```text
openpost instance use &lt;name&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost jobs`

List background jobs

**Usage**

```text
openpost jobs
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command              | Description          |
| -------------------- | -------------------- |
| `openpost jobs list` | List background jobs |

### `openpost jobs list`

List background jobs

**Usage**

```text
openpost jobs list [flags]
```

**Flags**

| Flag       | Default | Description                                  |
| ---------- | ------- | -------------------------------------------- |
| `--limit`  | `0`     | maximum number of jobs to return             |
| `--offset` | `0`     | number of jobs to skip                       |
| `--status` | `-`     | filter by status: pending, failed, completed |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost media`

Upload and list media attachments

**Usage**

```text
openpost media
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                        | Description                                   |
| ------------------------------ | --------------------------------------------- |
| `openpost media delete`        | Delete an unused media attachment             |
| `openpost media list`          | List media attachments                        |
| `openpost media prepare-image` | Fit a separate image copy for one destination |
| `openpost media storage`       | Show media storage usage                      |
| `openpost media update`        | Update media alt text                         |
| `openpost media upload`        | Upload a media file                           |
| `openpost media usage`         | List content that uses a media attachment     |

### `openpost media delete`

Delete an unused media attachment

**Usage**

```text
openpost media delete &lt;media-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost media list`

List media attachments

**Usage**

```text
openpost media list [flags]
```

**Flags**

| Flag      | Default | Description                             |
| --------- | ------- | --------------------------------------- |
| `--limit` | `0`     | maximum number of media items to return |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost media prepare-image`

Fit a separate image copy for one destination

**Usage**

```text
openpost media prepare-image &lt;media-id&gt; [flags]
```

**Flags**

| Flag               | Default | Description                                                |
| ------------------ | ------- | ---------------------------------------------------------- |
| `--account`        | `-`     | Connected account ID or slug                               |
| `--alt`            | `-`     | Alt text for the fitted copy; pass an empty value to clear |
| `--output-profile` | `-`     | Explicit destination format from capabilities              |
| `--settings-file`  | `-`     | Destination settings JSON file, or - for stdin             |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost media storage`

Show media storage usage

**Usage**

```text
openpost media storage
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost media update`

Update media alt text

**Usage**

```text
openpost media update &lt;media-id&gt; [flags]
```

**Flags**

| Flag    | Default | Description                               |
| ------- | ------- | ----------------------------------------- |
| `--alt` | `-`     | alt text; pass an empty value to clear it |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost media upload`

Upload a media file

**Usage**

```text
openpost media upload &lt;file&gt; [flags]
```

**Flags**

| Flag    | Default | Description                     |
| ------- | ------- | ------------------------------- |
| `--alt` | `-`     | alt text for the uploaded media |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost media usage`

List content that uses a media attachment

**Usage**

```text
openpost media usage &lt;media-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost post`

Create, list, view, update, and delete posts

Create, list, view, update, and delete posts. The post command is the friendly name for Publication workflows; every command creates, reads, and mutates Publications and Renditions through the canonical API.

**Usage**

```text
openpost post
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                  | Description                      |
| ------------------------ | -------------------------------- |
| `openpost post create`   | Create a draft or scheduled post |
| `openpost post delete`   | Delete a draft or scheduled post |
| `openpost post list`     | List posts                       |
| `openpost post schedule` | Schedule an existing publication |
| `openpost post update`   | Update a draft or scheduled post |
| `openpost post view`     | View a post                      |

### `openpost post create`

Create a draft or scheduled post

**Usage**

```text
openpost post create [flags]
```

**Flags**

| Flag              | Default | Description                                                                        |
| ----------------- | ------- | ---------------------------------------------------------------------------------- |
| `--accounts`      | `-`     | comma-separated account selectors                                                  |
| `--content`       | `-`     | post content                                                                       |
| `--file`          | `-`     | read post content from a file                                                      |
| `--media`         | `[]`    | media id or local file path; repeatable                                            |
| `--media-alt`     | `[]`    | alt text for the matching uploaded --media                                         |
| `--random-delay`  | `0`     | random delay in minutes                                                            |
| `--schedule`      | `-`     | natural-language, RFC3339, next-slot, now, or draft                                |
| `--segments-file` | `-`     | Ordered segment JSON with settings.mentions and UTF-8 byte ranges; use - for stdin |
| `--thread-draft`  | `-`     | encoded thread draft to attach                                                     |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost post delete`

Delete a draft or scheduled post

**Usage**

```text
openpost post delete &lt;post-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost post list`

List posts

**Usage**

```text
openpost post list [flags]
```

**Flags**

| Flag       | Default | Description                                           |
| ---------- | ------- | ----------------------------------------------------- |
| `--limit`  | `0`     | maximum number of posts to return                     |
| `--offset` | `0`     | number of posts to skip                               |
| `--status` | `-`     | filter by status: draft, scheduled, published, failed |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost post schedule`

Schedule an existing publication

**Usage**

```text
openpost post schedule &lt;publication-id&gt; [flags]
```

**Flags**

| Flag   | Default | Description                                   |
| ------ | ------- | --------------------------------------------- |
| `--at` | `-`     | schedule time, natural language, or next-slot |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost post update`

Update a draft or scheduled post

**Usage**

```text
openpost post update &lt;post-id&gt; [flags]
```

**Flags**

| Flag              | Default | Description                                                                        |
| ----------------- | ------- | ---------------------------------------------------------------------------------- |
| `--accounts`      | `-`     | comma-separated account selectors                                                  |
| `--content`       | `-`     | post content                                                                       |
| `--media`         | `[]`    | replacement media id or local file path; repeatable; pass an empty value to clear  |
| `--media-alt`     | `[]`    | alt text for the matching uploaded --media                                         |
| `--random-delay`  | `0`     | random delay in minutes                                                            |
| `--schedule`      | `-`     | natural-language, RFC3339, next-slot, now, or draft; empty string unschedules      |
| `--segments-file` | `-`     | Ordered segment JSON with settings.mentions and UTF-8 byte ranges; use - for stdin |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost post view`

View a post

**Usage**

```text
openpost post view &lt;post-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost provider`

Inspect provider availability and publishing support

**Usage**

```text
openpost provider
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                          | Description                                     |
| -------------------------------- | ----------------------------------------------- |
| `openpost provider capabilities` | List provider publishing capabilities           |
| `openpost provider list`         | List social providers available on the instance |
| `openpost provider readiness`    | Inspect provider setup and blocking issues      |

### `openpost provider capabilities`

List provider publishing capabilities

**Usage**

```text
openpost provider capabilities [flags]
```

**Flags**

| Flag                | Default | Description                               |
| ------------------- | ------- | ----------------------------------------- |
| `--content-profile` | `-`     | filter by input or output content profile |
| `--provider`        | `-`     | filter by provider key                    |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost provider list`

List social providers available on the instance

**Usage**

```text
openpost provider list
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost provider readiness`

Inspect provider setup and blocking issues

**Usage**

```text
openpost provider readiness
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication`

Create, list, validate, and publish publications

**Usage**

```text
openpost publication
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                                 | Description                                          |
| --------------------------------------- | ---------------------------------------------------- |
| `openpost publication cancel`           | Cancel a scheduled publication                       |
| `openpost publication comments`         | List comments for a published rendition              |
| `openpost publication create`           | Create a format-first publication                    |
| `openpost publication delete`           | Permanently delete an editable publication           |
| `openpost publication delete-comment`   | Delete a provider comment                            |
| `openpost publication delete-rendition` | Permanently delete one saved publication destination |
| `openpost publication events`           | List publication lifecycle events                    |
| `openpost publication hide-comment`     | Hide a provider comment                              |
| `openpost publication list`             | List publications                                    |
| `openpost publication publish-now`      | Queue a publication for immediate publishing         |
| `openpost publication renditions`       | Replace destination-specific renditions from JSON    |
| `openpost publication reply`            | Queue an explicit reply to a published rendition     |
| `openpost publication reply-comment`    | Reply to a provider comment                          |
| `openpost publication retry`            | Retry one failed publication destination             |
| `openpost publication schedule`         | Schedule an existing publication                     |
| `openpost publication update`           | Update an editable publication                       |
| `openpost publication validate`         | Validate a publication                               |
| `openpost publication view`             | View a publication                                   |

### `openpost publication cancel`

Cancel a scheduled publication

**Usage**

```text
openpost publication cancel &lt;publication-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication comments`

List comments for a published rendition

**Usage**

```text
openpost publication comments &lt;rendition-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication create`

Create a format-first publication

**Usage**

```text
openpost publication create [flags]
```

**Flags**

| Flag                  | Default       | Description                                                                                           |
| --------------------- | ------------- | ----------------------------------------------------------------------------------------------------- |
| `--accounts`          | `-`           | comma-separated account IDs/slugs/platforms                                                           |
| `--caption`           | `-`           | caption for image, carousel, story, or social video outputs                                           |
| `--content`           | `-`           | post text or fallback source text                                                                     |
| `--content-profile`   | `short_text`  | content profile: short_text, thread, link_share, image_post, carousel, story, short_video, long_video |
| `--description`       | `-`           | description field for link/video outputs                                                              |
| `--file`              | `-`           | read post/source text from file or '-' for stdin                                                      |
| `--media`             | `[]`          | media ID or local file path to attach; repeatable                                                     |
| `--media-alt`         | `[]`          | alt text for uploaded media                                                                           |
| `--privacy`           | `-`           | YouTube privacy status: private, unlisted, or public                                                  |
| `--schedule`          | `-`           | schedule time                                                                                         |
| `--segments-file`     | `-`           | Ordered segment JSON with settings.mentions and UTF-8 byte ranges; use - for stdin                    |
| `--tiktok-method`     | `DIRECT_POST` | TikTok content posting method                                                                         |
| `--tiktok-privacy`    | `SELF_ONLY`   | TikTok privacy level                                                                                  |
| `--title`             | `-`           | publication title                                                                                     |
| `--url`               | `-`           | source URL for link shares                                                                            |
| `--video-description` | `-`           | YouTube video description                                                                             |
| `--video-title`       | `-`           | YouTube video title                                                                                   |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication delete`

Permanently delete an editable publication

**Usage**

```text
openpost publication delete &lt;publication-id&gt; [flags]
```

**Flags**

| Flag        | Default | Description                            |
| ----------- | ------- | -------------------------------------- |
| `--confirm` | `false` | confirm permanent publication deletion |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication delete-comment`

Delete a provider comment

**Usage**

```text
openpost publication delete-comment &lt;comment-id&gt; [flags]
```

**Flags**

| Flag        | Default | Description                         |
| ----------- | ------- | ----------------------------------- |
| `--confirm` | `false` | confirm permanent provider deletion |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication delete-rendition`

Permanently delete one saved publication destination

**Usage**

```text
openpost publication delete-rendition &lt;publication-id&gt; &lt;account-id&gt; [flags]
```

**Flags**

| Flag        | Default | Description                            |
| ----------- | ------- | -------------------------------------- |
| `--confirm` | `false` | confirm permanent destination deletion |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication events`

List publication lifecycle events

**Usage**

```text
openpost publication events &lt;publication-id&gt; [flags]
```

**Flags**

| Flag      | Default | Description                        |
| --------- | ------- | ---------------------------------- |
| `--limit` | `0`     | maximum number of events to return |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication hide-comment`

Hide a provider comment

**Usage**

```text
openpost publication hide-comment &lt;comment-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication list`

List publications

**Usage**

```text
openpost publication list [flags]
```

**Flags**

| Flag                | Default | Description                              |
| ------------------- | ------- | ---------------------------------------- |
| `--content-profile` | `-`     | filter by content profile                |
| `--limit`           | `0`     | maximum number of publications to return |
| `--offset`          | `0`     | number of publications to skip           |
| `--status`          | `-`     | filter by status                         |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication publish-now`

Queue a publication for immediate publishing

**Usage**

```text
openpost publication publish-now &lt;publication-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication renditions`

Replace destination-specific renditions from JSON

**Usage**

```text
openpost publication renditions &lt;publication-id&gt; [flags]
```

**Flags**

| Flag     | Default | Description                                |
| -------- | ------- | ------------------------------------------ |
| `--file` | `-`     | JSON array of renditions, or '-' for stdin |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication reply`

Queue an explicit reply to a published rendition

**Usage**

```text
openpost publication reply &lt;rendition-id&gt; [flags]
```

**Flags**

| Flag          | Default | Description                                |
| ------------- | ------- | ------------------------------------------ |
| `--at`        | `-`     | optional reply schedule time               |
| `--body`      | `-`     | reply text                                 |
| `--file`      | `-`     | read reply text from file or '-' for stdin |
| `--parent-id` | `-`     | external provider post or comment ID       |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication reply-comment`

Reply to a provider comment

**Usage**

```text
openpost publication reply-comment &lt;comment-id&gt; [flags]
```

**Flags**

| Flag     | Default | Description                                |
| -------- | ------- | ------------------------------------------ |
| `--body` | `-`     | reply text                                 |
| `--file` | `-`     | read reply text from file or '-' for stdin |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication retry`

Retry one failed publication destination

**Usage**

```text
openpost publication retry &lt;publication-id&gt; &lt;account-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication schedule`

Schedule an existing publication

**Usage**

```text
openpost publication schedule &lt;publication-id&gt; [flags]
```

**Flags**

| Flag   | Default | Description                                   |
| ------ | ------- | --------------------------------------------- |
| `--at` | `-`     | schedule time, natural language, or next-slot |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication update`

Update an editable publication

**Usage**

```text
openpost publication update &lt;publication-id&gt; [flags]
```

**Flags**

| Flag                | Default | Description                                                                        |
| ------------------- | ------- | ---------------------------------------------------------------------------------- |
| `--content`         | `-`     | shared post or caption text                                                        |
| `--content-profile` | `-`     | content profile                                                                    |
| `--file`            | `-`     | read shared text from file or '-' for stdin                                        |
| `--force`           | `false` | overwrite after reviewing a revision conflict                                      |
| `--schedule`        | `-`     | new schedule time; use draft to clear                                              |
| `--segments-file`   | `-`     | Ordered segment JSON with settings.mentions and UTF-8 byte ranges; use - for stdin |
| `--title`           | `-`     | publication title                                                                  |
| `--url`             | `-`     | source URL; pass an empty value to clear                                           |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication validate`

Validate a publication

**Usage**

```text
openpost publication validate &lt;publication-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost publication view`

View a publication

**Usage**

```text
openpost publication view &lt;publication-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost schedule`

Manage reusable posting schedule slots

Manage the workspace-local weekly slots used by next-slot scheduling.

**Usage**

```text
openpost schedule
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                     | Description                                   |
| --------------------------- | --------------------------------------------- |
| `openpost schedule create`  | Create a weekly posting schedule slot         |
| `openpost schedule delete`  | Delete a posting schedule slot                |
| `openpost schedule list`    | List posting schedule slots                   |
| `openpost schedule next`    | Find the next available posting slot          |
| `openpost schedule suggest` | Create a suggested seven-day posting schedule |
| `openpost schedule update`  | Update a posting schedule slot                |

### `openpost schedule create`

Create a weekly posting schedule slot

**Usage**

```text
openpost schedule create [flags]
```

**Flags**

| Flag       | Default | Description                                        |
| ---------- | ------- | -------------------------------------------------- |
| `--day`    | `0`     | workspace-local day of week (0=Sunday, 6=Saturday) |
| `--hour`   | `0`     | workspace-local hour (0-23)                        |
| `--label`  | `-`     | display label                                      |
| `--minute` | `0`     | workspace-local minute (0-59)                      |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost schedule delete`

Delete a posting schedule slot

**Usage**

```text
openpost schedule delete &lt;schedule-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost schedule list`

List posting schedule slots

**Usage**

```text
openpost schedule list
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost schedule next`

Find the next available posting slot

**Usage**

```text
openpost schedule next
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost schedule suggest`

Create a suggested seven-day posting schedule

Create active workspace-local schedule slots for every day of the week.

**Usage**

```text
openpost schedule suggest [flags]
```

**Flags**

| Flag              | Default | Description                              |
| ----------------- | ------- | ---------------------------------------- |
| `--posts-per-day` | `3`     | number of slots to create per day (1-10) |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost schedule update`

Update a posting schedule slot

**Usage**

```text
openpost schedule update &lt;schedule-id&gt; [flags]
```

**Flags**

| Flag         | Default | Description                                        |
| ------------ | ------- | -------------------------------------------------- |
| `--active`   | `false` | enable the slot                                    |
| `--day`      | `0`     | workspace-local day of week (0=Sunday, 6=Saturday) |
| `--hour`     | `0`     | workspace-local hour (0-23)                        |
| `--inactive` | `false` | disable the slot                                   |
| `--label`    | `-`     | display label; pass an empty value to clear it     |
| `--minute`   | `0`     | workspace-local minute (0-59)                      |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost thread`

Create multi-post threads

**Usage**

```text
openpost thread
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                  | Description                          |
| ------------------------ | ------------------------------------ |
| `openpost thread create` | Create a thread from a markdown file |

### `openpost thread create`

Create a thread from a markdown file

**Usage**

```text
openpost thread create &lt;file&gt; [flags]
```

**Flags**

| Flag             | Default | Description                                         |
| ---------------- | ------- | --------------------------------------------------- |
| `--accounts`     | `-`     | comma-separated account selectors                   |
| `--random-delay` | `0`     | random delay in minutes                             |
| `--schedule`     | `-`     | natural-language, RFC3339, next-slot, now, or draft |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost version`

Print the openpost CLI version

**Usage**

```text
openpost version
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow`

Create, publish, pause and inspect workflows

**Usage**

```text
openpost workflow
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                           | Description                                                      |
| --------------------------------- | ---------------------------------------------------------------- |
| `openpost workflow create`        | Create a workflow draft from a JSON file                         |
| `openpost workflow delete`        | Delete a paused workflow with no active runs                     |
| `openpost workflow get`           | Show a workflow, its revisions and definition                    |
| `openpost workflow list`          | List workflows                                                   |
| `openpost workflow pause`         | Stop new automatic runs; existing runs continue                  |
| `openpost workflow publish`       | Publish the draft revision and enable its source                 |
| `openpost workflow run`           | Show a run's steps, effects and errors; approve or cancel it     |
| `openpost workflow runs`          | List the latest runs with state and stop reason                  |
| `openpost workflow sample-source` | Read example items from a workflow's source without side effects |
| `openpost workflow save`          | Replace a workflow draft, checking its revision                  |
| `openpost workflow stop-replies`  | Stop new runs and cancel pending automatic replies               |
| `openpost workflow test-node`     | Run one data or tool step with supplied input data               |

### `openpost workflow create`

Create a workflow draft from a JSON file

Create a workflow draft. The file is a definition ({"schema":1,"source":{...},"steps":[...]}) or an object with name, description and definition. The draft is disabled and runs nothing until `workflow publish`.

**Usage**

```text
openpost workflow create --file &lt;definition.json&gt; [flags]
```

**Flags**

| Flag            | Default | Description                                                 |
| --------------- | ------- | ----------------------------------------------------------- |
| `--description` | `-`     | workflow description (default: the description in the file) |
| `--file`        | `-`     | workflow definition JSON file, or - for stdin               |
| `--name`        | `-`     | workflow name (default: the name in the file)               |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow delete`

Delete a paused workflow with no active runs

Permanently delete a paused workflow that has no active runs, together with its run history. Posts it created remain.

**Usage**

```text
openpost workflow delete &lt;workflow-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow get`

Show a workflow, its revisions and definition

Show a workflow's state, revision, published revision and last source check. With --json the output is the complete workflow; edit its definition and pass the file to `workflow save --file` to change it.

**Usage**

```text
openpost workflow get &lt;workflow-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow list`

List workflows

**Usage**

```text
openpost workflow list
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow pause`

Stop new automatic runs; existing runs continue

Stop new automatic runs. Existing runs and pending replies continue; use stop-replies for pending replies.

**Usage**

```text
openpost workflow pause &lt;workflow-id&gt; [flags]
```

**Flags**

| Flag                  | Default | Description                                                      |
| --------------------- | ------- | ---------------------------------------------------------------- |
| `--expected-revision` | `0`     | workflow revision to act on (default: read the current revision) |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow publish`

Publish the draft revision and enable its source

Publish a workflow's draft revision and enable its source. New matching activity then starts runs, which can send replies.

**Usage**

```text
openpost workflow publish &lt;workflow-id&gt; [flags]
```

**Flags**

| Flag                  | Default | Description                                                      |
| --------------------- | ------- | ---------------------------------------------------------------- |
| `--expected-revision` | `0`     | workflow revision to act on (default: read the current revision) |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow run`

Show a run's steps, effects and errors; approve or cancel it

Show one run: its state, stop reason, and each step's state, error and recorded output. With --json the output is the complete run, including every step's inputs and output.

**Usage**

```text
openpost workflow run &lt;run-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                         | Description                              |
| ------------------------------- | ---------------------------------------- |
| `openpost workflow run approve` | Approve a run that is waiting for review |
| `openpost workflow run cancel`  | Cancel the remaining steps of a run      |

### `openpost workflow run approve`

Approve a run that is waiting for review

Approve a run waiting for review and continue it. Approving a post review needs --post-revision, the post revision you reviewed (see `openpost publication view`); if the post changed since, the server refuses and nothing is approved. For a reviewed reply, --reply-text replaces the proposed text.

**Usage**

```text
openpost workflow run approve &lt;run-id&gt; [flags]
```

**Flags**

| Flag                  | Default | Description                                                 |
| --------------------- | ------- | ----------------------------------------------------------- |
| `--expected-revision` | `0`     | run revision to act on (default: read the current revision) |
| `--post-revision`     | `0`     | post revision that was reviewed (required for post reviews) |
| `--reply-text`        | `-`     | replacement text for a reviewed comment or message reply    |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow run cancel`

Cancel the remaining steps of a run

Cancel the remaining steps of a run. An action already dispatched may finish, and drafts or queued posts it created remain.

**Usage**

```text
openpost workflow run cancel &lt;run-id&gt; [flags]
```

**Flags**

| Flag                  | Default | Description                                                 |
| --------------------- | ------- | ----------------------------------------------------------- |
| `--expected-revision` | `0`     | run revision to act on (default: read the current revision) |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow runs`

List the latest runs with state and stop reason

List the latest 50 runs, for one workflow or for the whole workspace. STOP REASON is the reason the server recorded when it ended or refused a run, such as a sender's daily limit. Use `workflow run &lt;run-id&gt;` for step results.

**Usage**

```text
openpost workflow runs [workflow-id]
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow sample-source`

Read example items from a workflow's source without side effects

Read example items from the source of a workflow's draft. It advances no production cursor, starts no run and writes nothing to a provider.

**Usage**

```text
openpost workflow sample-source &lt;workflow-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow save`

Replace a workflow draft, checking its revision

Replace a workflow draft and print its new revision. The save fails with the server's revision conflict, and nothing is overwritten, when the workflow changed since the revision you pass. The expected revision is --expected-revision, or the "revision" of a file saved from `workflow get --json`. It is never filled in from the server, because that would overwrite changes you have not seen. A published workflow keeps running its published revision until `workflow publish`.

**Usage**

```text
openpost workflow save &lt;workflow-id&gt; --file &lt;definition.json&gt; --expected-revision &lt;n&gt; [flags]
```

**Flags**

| Flag                  | Default | Description                                                 |
| --------------------- | ------- | ----------------------------------------------------------- |
| `--description`       | `-`     | workflow description (default: the description in the file) |
| `--expected-revision` | `0`     | workflow revision this edit is based on                     |
| `--file`              | `-`     | workflow definition JSON file, or - for stdin               |
| `--name`              | `-`     | workflow name (default: the name in the file)               |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow stop-replies`

Stop new runs and cancel pending automatic replies

Stop new runs and cancel pending automatic comment replies.

**Usage**

```text
openpost workflow stop-replies &lt;workflow-id&gt; [flags]
```

**Flags**

| Flag                  | Default | Description                                                      |
| --------------------- | ------- | ---------------------------------------------------------------- |
| `--expected-revision` | `0`     | workflow revision to act on (default: read the current revision) |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workflow test-node`

Run one data or tool step with supplied input data

Run a single data or tool step of the saved draft with the JSON object you supply as its source data. Publication and reply steps are refused. The result is stored in run history.

**Usage**

```text
openpost workflow test-node &lt;workflow-id&gt; --step &lt;step-id&gt; [flags]
```

**Flags**

| Flag                  | Default | Description                                                      |
| --------------------- | ------- | ---------------------------------------------------------------- |
| `--data`              | `-`     | JSON file with the input data object, or - for stdin             |
| `--expected-revision` | `0`     | workflow revision to act on (default: read the current revision) |
| `--step`              | `-`     | ID of the step to run                                            |

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workspace`

Manage the active OpenPost workspace

**Usage**

```text
openpost workspace
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

**Subcommands**

| Command                     | Description                                      |
| --------------------------- | ------------------------------------------------ |
| `openpost workspace create` | Create a workspace                               |
| `openpost workspace list`   | List workspaces                                  |
| `openpost workspace use`    | Set the active workspace for the current profile |

### `openpost workspace create`

Create a workspace

**Usage**

```text
openpost workspace create &lt;name&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workspace list`

List workspaces

**Usage**

```text
openpost workspace list
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |

### `openpost workspace use`

Set the active workspace for the current profile

**Usage**

```text
openpost workspace use &lt;name-or-id&gt;
```

**Inherited Flags**

| Flag          | Default | Description                                                        |
| ------------- | ------- | ------------------------------------------------------------------ |
| `--instance`  | `-`     | OpenPost instance URL (default: profile or $OPENPOST_INSTANCE)     |
| `--json`      | `false` | emit machine-readable JSON instead of tables/prose                 |
| `--no-color`  | `false` | disable ANSI colors                                                |
| `--profile`   | `-`     | profile name from config (default: $OPENPOST_PROFILE or 'default') |
| `--quiet`     | `false` | suppress non-error output                                          |
| `--token`     | `-`     | API token override (default: keyring or $OPENPOST_TOKEN)           |
| `--workspace` | `-`     | workspace name or ID (default: profile or $OPENPOST_WORKSPACE)     |
| `--yes`       | `false` | skip interactive confirmations                                     |
