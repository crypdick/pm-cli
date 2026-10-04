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
