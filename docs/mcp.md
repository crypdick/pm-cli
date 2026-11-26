# MCP server

Connect a Model Context Protocol (MCP) client to search mail, send replies, and
manage drafts. Tools include configuration changes and permanent deletion.

## Connect a local client

Start Proton Bridge and [configure pm-cli](../README.md#setup). You need
Go 1.25.3 or later, Python 3.13 or later, and uv. From the repository checkout:

```sh
go build -o pm-cli ./cmd/pm-cli
uv run --no-dev --extra mcp pm-cli-mcp --binary ./pm-cli
```

Set your client's working directory to the checkout and use the `uv run` command
as its launch command. For headless use, set `PM_CLI_BRIDGE_PASSWORD_FILE` to a
readable, nonempty file containing your Bridge password outside the workspace.

## Connect over HTTP

Create a bearer-token file outside the workspace, then start the server:

```sh
uv run --no-dev --extra mcp pm-cli-mcp --binary ./pm-cli --transport http \
    --token-file /run/secrets/mcp-token
```

Connect to `http://127.0.0.1:8000/mcp` with `Authorization: Bearer <token>`.
Call `mailbox_list` to check the connection. Use `file_upload` and `file_read`
for attachments. File paths must stay inside the workspace, and transfers can't
exceed 10 MiB. If a write times out, check mailbox state before retrying it.
CLI stdout and stderr each have the same 10 MiB limit. Exceeding either limit
returns an error, kills the CLI process group, and drains its pipes before
accepting another command.
HTTP tool results use JSON responses so base64 attachments also work with
legacy MCP clients whose SSE event limit is smaller than the file limit.

## Mail evidence

`mail_list_evidence` lists a bounded read-only page. Its optional
`sent_since` and `sent_before` filter on the message's Date header before
pagination. `include_body` hydrates bodies and full headers in the same IMAP
session. Keep this opt-in; metadata-only listings are smaller.
Body-inclusive evidence has a separate 24 MiB CLI output ceiling so a bounded
host review can retain complete message and header evidence. Other tools
retain the standard limit.

`mail_read_evidence` takes the RFC Message-ID and mailbox, rejects ambiguous
matches, preserves read state, and converts HTML-only bodies to readable text
without fetching remote content. `mail_evidence_exists` checks presence without
loading the body. Use `mailbox_list_evidence` for display names and exact
mailbox identifiers.

`mail_send` returns the sent Message-ID after SMTP acceptance. Existing
idempotency keys remain supported. `mail_delete_evidence` permanently deletes
one exact match and requires UID-scoped expunge. It never uses mailbox-wide
expunge. Grant sends and deletions separately from read operations.
