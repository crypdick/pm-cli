# MCP server

The optional Python server uses FastMCP 4.0.10 to expose all 36 CLI operations
as individual typed tools. The Go CLI's `--help-json` grammar drives discovery;
new commands and flags do not need a second implementation in Python.
Two additional tools, `file_upload` and `file_read`, transfer attachments,
templates, and batch files as base64 within the server's workspace.

## Local stdio

Requires Go 1.25.3 or newer, Python 3.13 or newer, and uv:

```sh
go build -o pm-cli ./cmd/pm-cli
uv sync --frozen --no-dev
uv run pm-cli-mcp --binary ./pm-cli --workspace ./pm-cli-files
```

Configure an MCP client to launch that command from the repository directory.
The CLI uses its existing configuration and system keyring. Headless deployments
can supply `PM_CLI_BRIDGE_PASSWORD` or `PM_CLI_BRIDGE_PASSWORD_FILE`. The environment
password takes precedence, then the file, then the keyring. An unreadable or empty
configured file fails instead of falling back. Password files may have a trailing
newline. Keep them outside the workspace, accessible only to the service user.

Noninteractive initialization uses the existing credential source:

```sh
pm-cli config init --email me@example.com --json
```

## Private HTTP

Create a bearer-token file outside the workspace and run:

```sh
uv run pm-cli-mcp --binary ./pm-cli --transport http \
  --token-file /run/secrets/mcp-token --host 127.0.0.1 --port 8000 \
  --workspace ./pm-cli-files
```

The MCP endpoint is `/mcp`. HTTP requires a nonempty token file; clients send
`Authorization: Bearer <token>`. `/healthz` reports process liveness without
checking Bridge connectivity. Verify a `mailbox_list` call to test the mail path.
For Docker, build `Dockerfile.mcp`. Share Bridge's network namespace so the CLI
can use loopback IMAP/SMTP; it deliberately refuses remote mail servers.
Bind HTTP only to the private Docker interface and keep the listener unpublished.
Persist `HOME` and `XDG_CONFIG_HOME` for contacts, configuration, and send
idempotency records. Run a single server instance per state directory.

## Tool behavior

Tool names mirror command paths: `mail_search`, `mail_send`, `mail_draft_create`,
`mail_delete`, `config_set`, and so on. Parameters mirror CLI flags with hyphens
replaced by underscores. The `stdin` parameter supplies message bodies or the
JSON array for `mail_batch`; `config_init` requires `email` and uses already
provisioned credentials. It never prompts for a password.

The full tool catalog includes sends, replies, configuration changes, mailbox
deletion, and `mail_delete(permanent=true)`. Tool annotations identify destructive
operations; clients decide their own confirmation policy. Reading a message can
mark it read, so `mail_read` is not annotated as read-only. Email bodies and
attachments are untrusted content, not instructions to execute.

All subprocess calls use argument arrays, never a shell. The `mail_watch` shell
callback (`--exec`) is intentionally unavailable through MCP. Watching defaults
to `once=true`; every operation has a 60-second timeout (set `--timeout` to change
it). Cancellation and timeout kill the CLI process group. A timed-out write may
have committed; check mailbox state before retrying. Calls are serialized to
avoid conflicting CLI state writes.

File paths for attachments, templates, batch inputs, and downloads must stay
inside the workspace, including resolved symlinks. Uploads refuse existing
files; downloads default to a generated workspace filename. Use `file_read` to
retrieve the downloaded bytes. Inputs, outputs, and transferred files are limited
to 10 MiB. CLI errors become MCP tool errors; successful JSON output remains
structured data, while diagnostic/watch text is returned under `output`.

Use `uid:<uid>` together with the source mailbox. Use `mail_batch` for related
mailbox operations that must share an IMAP session. Contacts tools operate on
pm-cli's local address book, not Proton's server contacts.

## Verification

```sh
go test ./...
go vet ./...
uv run pytest -q
uv run ruff check pm_cli_mcp tests
uv run ruff format --check pm_cli_mcp tests
```

Tests exercise real CLI configuration and contacts through an MCP client,
complete discovery, structured errors, argument handling, file confinement,
timeout/cancellation cleanup, and HTTP authentication requirements.
