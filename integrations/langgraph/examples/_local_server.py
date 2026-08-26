# Copyright (c) 2026 OceanBase.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Start a throwaway PowerContext Go Server for the runnable examples."""

from __future__ import annotations

import os
import shutil
import socket
import subprocess
import time
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path
from tempfile import TemporaryDirectory
from urllib.error import URLError
from urllib.request import urlopen


@contextmanager
def local_powercontext_server(*, token: str | None = None) -> Iterator[str]:
    """Run the Go Server for the duration of the block and yield its base URL."""

    command, cwd = _server_command()
    port = _unused_loopback_port()
    base_url = f"http://127.0.0.1:{port}"
    with TemporaryDirectory(prefix="powercontext-langgraph-") as data_dir:
        environment = os.environ.copy()
        environment.update(
            {
                "POWERCONTEXT_HOME": data_dir,
                "POWERCONTEXT_SERVER_AUTH_ENABLED": "true"
                if token is not None
                else "false",
                "POWERCONTEXT_SERVER_AUTH_TOKEN": token or "",
                "POWERCONTEXT_SERVER_MCP_ENABLED": "false",
                "POWERCONTEXT_SERVER_DASHBOARD_ENABLED": "false",
                "POWERCONTEXT_SERVER_HANDOFF_REPORT_ENABLED": "false",
                "POWERCONTEXT_SERVER_LOGGING_ACCESS": "false",
            }
        )
        process = subprocess.Popen(  # noqa: S603 - executable and arguments are resolved locally below.
            [*command, "server", "run", "--host", "127.0.0.1", "--port", str(port)],
            cwd=cwd,
            env=environment,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        try:
            _wait_until_live(process, base_url)
            yield base_url
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)


def _server_command() -> tuple[list[str], str | None]:
    configured = os.environ.get("POWERCONTEXT_BINARY", "").strip()
    if configured:
        executable = Path(configured).expanduser().resolve()
        if not executable.is_file():
            raise RuntimeError(
                "POWERCONTEXT_BINARY does not identify a PowerContext executable"
            )
        return [str(executable)], None

    installed = shutil.which("powercontext")
    if installed:
        return [installed], None

    repository = Path(__file__).resolve().parents[3]
    go = shutil.which("go")
    if go and (repository / "go.mod").is_file():
        return [go, "run", "-tags", "sqlite_fts5", "./cmd/powercontext"], str(
            repository
        )
    raise RuntimeError(
        "PowerContext Go Server is unavailable. Install the powercontext binary or set POWERCONTEXT_BINARY."
    )


def _unused_loopback_port() -> int:
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return int(listener.getsockname()[1])


def _wait_until_live(process: subprocess.Popen[bytes], base_url: str) -> None:
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(
                f"PowerContext Go Server exited with status {process.returncode}"
            )
        try:
            with urlopen(f"{base_url}/health/live", timeout=0.5) as response:  # noqa: S310
                if response.status == 200:
                    return
        except (OSError, URLError):
            pass
        time.sleep(0.05)
    raise RuntimeError("PowerContext Go Server did not become live within 30 seconds")
