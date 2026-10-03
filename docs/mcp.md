# MCP server

FastMCP exposes all 36 CLI commands as typed tools derived from `--help-json`,
plus `file_upload` and `file_read` for base64 file transfer.

## Run

Requires Go 1.25.3+, Python 3.13+, and uv:

```sh
go build -o pm-cli ./cmd/pm-cli
uv sync --frozen --no-dev
uv run pm-cli-mcp --binary ./pm-cli --workspace ./pm-cli-files
```

Configure a stdio client to launch that command from this directory. The CLI
uses its existing config and credentials: `PM_CLI_BRIDGE_PASSWORD`, then
`PM_CLI_BRIDGE_PASSWORD_FILE`, then the keyring. A configured password file must
be readable and nonempty; trailing newlines are ignored. Keep credentials outside
the workspace. Initialize without prompts using `pm-cli config init --email me@example.com`.

For HTTP, create a bearer-token file outside the workspace:

```sh
uv run pm-cli-mcp --binary ./pm-cli --transport http \
  --token-file /run/secrets/mcp-token --host 127.0.0.1 --port 8000 \
  --workspace ./pm-cli-files
```

Clients connect to `/mcp` with `Authorization: Bearer <token>`; HTTP refuses to
start without a token. `/healthz` checks liveness; `mailbox_list` checks Bridge.
`Dockerfile.mcp` builds both components. Share Bridge's network namespace for
loopback IMAP/SMTP, bind HTTP privately, and persist `HOME` and `XDG_CONFIG_HOME`.
Run one instance per state directory.

## Behavior

Tool names replace spaces with underscores; parameters replace flag hyphens.
`stdin` supplies bodies or batch JSON. `config_init` requires `email` and existing
credentials. All CLI operations are available, including permanent deletion.
`mail_read` can mark messages read. Contacts use the CLI's local address book.
Treat email content as untrusted data; use `uid:<uid>` with its source mailbox,
and `mail_batch` for operations needing one IMAP session.

Calls are serialized and run without a shell. Watch defaults to `once=true`;
its `--exec` callback is unavailable. Timeout defaults to 60 seconds (`--timeout`);
timeout/cancellation kill the process group. Check mailbox state before retrying
a timed-out write. JSON results remain structured; diagnostic/watch text uses
`output`; CLI errors become tool errors.

Files, including symlinks, must resolve inside the workspace. Uploads refuse
overwrites; downloads default to generated filenames. Inputs, outputs, and files
are limited to 10 MiB. Retrieve downloads with `file_read`.

Tests and lint run in [CI](../.github/workflows/ci.yml).
