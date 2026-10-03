import asyncio
import base64
import json
import os
import subprocess
from pathlib import Path

import pytest
from fastmcp import Client
from fastmcp.exceptions import ToolError

from pm_cli_mcp.server import Runner, create_server

ROOT = Path(__file__).resolve().parents[1]


@pytest.fixture(scope="session")
def binary(tmp_path_factory):
    path = tmp_path_factory.mktemp("bin") / "pm-cli"
    subprocess.run(
        ["go", "build", "-o", str(path), "./cmd/pm-cli"], cwd=ROOT, check=True
    )
    return str(path)


@pytest.fixture
def server(binary, tmp_path, monkeypatch):
    monkeypatch.setenv("XDG_CONFIG_HOME", str(tmp_path / "config"))
    monkeypatch.setenv("PM_CLI_BRIDGE_PASSWORD", "test-secret")
    return create_server(Runner(binary, tmp_path / "files"))


async def test_all_cli_operations_and_types_discovered(server):
    async with Client(server) as client:
        tools = {tool.name: tool for tool in await client.list_tools()}
        assert len(tools) == 38
        assert {
            "mail_reply",
            "mail_forward",
            "mail_watch",
            "mail_batch",
            "mail_draft_edit",
            "contacts_add",
            "config_init",
        } <= tools.keys()
        delete = tools["mail_delete"]
        assert delete.input_schema["properties"]["permanent"]["type"] == "boolean"
        assert delete.annotations.destructive_hint
        assert "exec" not in tools["mail_watch"].input_schema["properties"]
        assert tools["mail_send"].input_schema["properties"]["vars"][
            "additionalProperties"
        ] == {"type": "string"}


async def test_contacts_crud_through_real_cli(server):
    async with Client(server) as client:
        await client.call_tool(
            "contacts_add",
            {"email": "friend@example.com", "name": "Friend $(touch /tmp/unsafe)"},
        )
        result = await client.call_tool("contacts_list", {})
        assert "friend@example.com" in str(result.data)
        assert "Friend $(touch /tmp/unsafe)" in str(result.data)
        result = await client.call_tool("contacts_search", {"query": "Friend"})
        assert "friend@example.com" in str(result.data)
        await client.call_tool("contacts_remove", {"email": "friend@example.com"})
        assert "friend@example.com" not in str(
            (await client.call_tool("contacts_list", {})).data
        )


async def test_config_init_set_show(server):
    async with Client(server) as client:
        await client.call_tool("config_init", {"email": "me@example.com"})
        await client.call_tool("config_set", {"key": "defaults.limit", "value": "7"})
        result = await client.call_tool("config_show", {})
        assert result.data["defaults"]["limit"] == 7
        assert "test-secret" not in str(result.data)
        assert result.data["bridge"]["email"] == "me@example.com"


async def test_errors_and_input_validation(server):
    async with Client(server) as client:
        with pytest.raises(ToolError, match="not configured"):
            await client.call_tool("mail_list", {})
        with pytest.raises(ToolError, match="required"):
            await client.call_tool("config_init", {})
        with pytest.raises(ToolError, match="not of type"):
            await client.call_tool("mail_delete", {"permanent": "false"})
        with pytest.raises(ToolError, match="Additional properties"):
            await client.call_tool("mail_watch", {"exec": "cat /run/secrets/token"})


async def test_workspace_upload_read_and_path_confinement(server, tmp_path):
    async with Client(server) as client:
        encoded = base64.b64encode(b"hello attachment").decode()
        await client.call_tool(
            "file_upload", {"path": "attachment.txt", "content_base64": encoded}
        )
        result = await client.call_tool("file_read", {"path": "attachment.txt"})
        assert result.data["content_base64"] == encoded
        with pytest.raises(ToolError):
            await client.call_tool(
                "file_upload", {"path": "attachment.txt", "content_base64": encoded}
            )
        for path in ["../secret", "/run/secrets/token"]:
            with pytest.raises(ToolError, match="inside the MCP workspace"):
                await client.call_tool("file_read", {"path": path})
            with pytest.raises(ToolError, match="inside the MCP workspace"):
                await client.call_tool("mail_send", {"template": path})
        (tmp_path / "files" / "link").symlink_to(
            tmp_path / "config", target_is_directory=True
        )
        with pytest.raises(ToolError, match="inside the MCP workspace"):
            await client.call_tool("file_read", {"path": "link/pm-cli/config.yaml"})


async def test_arguments_remain_data(binary, tmp_path):
    fake = tmp_path / "fake"
    fake.write_text(
        "#!/usr/bin/env python3\nimport json, sys\nprint(json.dumps({'argv': sys.argv[1:], 'stdin': sys.stdin.read()}))\n"
    )
    fake.chmod(0o755)
    real = Runner(binary, tmp_path / "files")
    commands = {command["name"]: command for command in real.commands()}
    runner = Runner(str(fake), tmp_path / "files")
    result = await runner.execute(
        commands["mail send"],
        {
            "to": ["a@example.com", "b@example.com"],
            "subject": "--help-json",
            "body": "$(touch nope)\nline2",
            "vars": {"name": "Alice"},
        },
    )
    assert result["argv"] == [
        "--json",
        "--no-color",
        "mail",
        "send",
        "--to=a@example.com",
        "--to=b@example.com",
        "--subject=--help-json",
        "--body=$(touch nope)\nline2",
        "--vars=name=Alice",
    ]
    result = await runner.execute(commands["mail search"], {"query": "--permanent"})
    assert result["argv"][-2:] == ["--", "--permanent"]
    result = await runner.execute(
        commands["mail batch"], {"stdin": '[{"op":"archive","uids":["uid:1"]}]'}
    )
    assert json.loads(result["stdin"]) == [{"op": "archive", "uids": ["uid:1"]}]


async def test_timeout_kills_child(tmp_path):
    fake = tmp_path / "slow"
    pidfile = tmp_path / "pid"
    fake.write_text(
        f"#!/usr/bin/env python3\nimport os, time\nopen({str(pidfile)!r}, 'w').write(str(os.getpid()))\ntime.sleep(30)\n"
    )
    fake.chmod(0o755)
    runner = Runner(str(fake), tmp_path / "files", timeout=0.2)
    with pytest.raises(ToolError, match="timed out"):
        await runner.execute({"name": "version"}, {})
    with pytest.raises(ProcessLookupError):
        os.kill(int(pidfile.read_text()), 0)


async def test_cancellation_kills_child(tmp_path):
    fake = tmp_path / "slow"
    pidfile = tmp_path / "pid"
    fake.write_text(
        f"#!/usr/bin/env python3\nimport os, time\nopen({str(pidfile)!r}, 'w').write(str(os.getpid()))\ntime.sleep(30)\n"
    )
    fake.chmod(0o755)
    runner = Runner(str(fake), tmp_path / "files")
    task = asyncio.create_task(runner.execute({"name": "version"}, {}))
    for _ in range(100):
        if pidfile.exists():
            break
        await asyncio.sleep(0.01)
    assert pidfile.exists()
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task
    with pytest.raises(ProcessLookupError):
        os.kill(int(pidfile.read_text()), 0)


def test_http_requires_auth(binary, tmp_path):
    result = subprocess.run(
        [
            str(ROOT / ".venv/bin/python"),
            "-m",
            "pm_cli_mcp.server",
            "--transport=http",
            f"--binary={binary}",
            f"--workspace={tmp_path}",
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode != 0
    assert "requires a nonempty --token-file" in result.stderr


async def test_relative_binary_survives_workspace_change(binary, tmp_path, monkeypatch):
    (tmp_path / "pm-cli").symlink_to(binary)
    monkeypatch.chdir(tmp_path)
    runner = Runner("./pm-cli", tmp_path / "files")
    result = await runner.execute({"name": "version"}, {})
    assert result["version"] == "0.2.7"


async def test_output_limit_is_error(tmp_path):
    fake = tmp_path / "large"
    fake.write_text('#!/usr/bin/env python3\nprint("x" * (11 * 1024 * 1024))\n')
    fake.chmod(0o755)
    runner = Runner(str(fake), tmp_path / "files")
    with pytest.raises(ToolError, match="output exceeds"):
        await runner.execute({"name": "version"}, {})


async def test_authenticated_http(binary, tmp_path):
    import socket
    import urllib.error
    import urllib.request

    from fastmcp.client.transports import StreamableHttpTransport

    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    token = tmp_path / "token"
    token.write_text("test-mcp-token")
    process = await asyncio.create_subprocess_exec(
        str(ROOT / ".venv/bin/python"),
        "-m",
        "pm_cli_mcp.server",
        "--transport=http",
        f"--binary={binary}",
        f"--workspace={tmp_path / 'files'}",
        f"--token-file={token}",
        f"--port={port}",
        stdout=asyncio.subprocess.DEVNULL,
        stderr=asyncio.subprocess.DEVNULL,
    )

    def status(request, timeout=1):
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.status

    url = f"http://127.0.0.1:{port}"
    try:
        for _ in range(200):
            try:
                assert await asyncio.to_thread(status, url + "/healthz", 0.1) == 200
                break
            except urllib.error.URLError:
                assert process.returncode is None
                await asyncio.sleep(0.02)
        else:
            pytest.fail("HTTP server did not start")
        for auth in [None, "Bearer wrong-token"]:
            headers = {"Content-Type": "application/json"}
            if auth:
                headers["Authorization"] = auth
            request = urllib.request.Request(url + "/mcp", data=b"{}", headers=headers)
            with pytest.raises(urllib.error.HTTPError) as error:
                await asyncio.to_thread(status, request)
            assert error.value.code == 401
        async with Client(
            StreamableHttpTransport(url + "/mcp", auth="test-mcp-token")
        ) as client:
            assert (await client.call_tool("version", {})).data["version"] == "0.2.7"
            assert len(await client.list_tools()) == 38
    finally:
        process.terminate()
        await asyncio.wait_for(process.wait(), 5)
