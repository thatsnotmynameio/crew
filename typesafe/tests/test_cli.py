from __future__ import annotations

import http.client
import json
import os
import queue
import signal
import sqlite3
import subprocess  # nosec B404  # the tests start processes of their own
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import IO, TYPE_CHECKING, TypeAlias, cast
from urllib.parse import urlsplit

import ledger_tables
import pytest

from typesafe_judge.cli import main

if TYPE_CHECKING:
    from collections.abc import Iterator

QUESTION = "issue_needs_candidate"
BANK = f"""\
questions:
  {QUESTION}:
    primitive: noul
    instructions: Does `issue` build on `candidate`?
    model: jev-1.13.0
    bands: {{yes_at: 0.7, no_at: 0.3}}
    default: no
    stage: act
    bar: {{max_error_yes: 0.1, max_error_no: 0.1, min_examples: 30, max_flip_rate: 0.05}}
"""
STATE = {"issue": "Add a ledger", "candidate": "Add a bank"}
READY_KEYS = {"instance", "pid", "url", "ask_token_file", "admin_token_file"}
WAIT = 30.0

Record: TypeAlias = dict[str, object]


@pytest.fixture
def root(tmp_path: Path) -> Path:
    (tmp_path / ".crew").mkdir()
    (tmp_path / ".crew/typesafe.yaml").write_text(BANK, encoding="utf-8")
    return tmp_path


def service_json(root: Path) -> Record | None:
    path = root / ".crew/typesafe/service.json"
    if not path.exists():
        return None
    return cast("Record", json.loads(path.read_text(encoding="utf-8")))


def call(record: Record, path: str, body: object = None) -> tuple[int, Record]:
    token = Path(cast("str", record["ask_token_file"])).read_text(encoding="utf-8")
    data = None if body is None else json.dumps(body).encode()
    url = urlsplit(cast("str", record["url"]))
    con = http.client.HTTPConnection(cast("str", url.hostname), url.port, timeout=WAIT)
    try:
        method = "GET" if data is None else "POST"
        con.request(method, path, body=data, headers={"Authorization": f"Bearer {token}"})
        response = con.getresponse()
        return response.status, cast("Record", json.loads(response.read()))
    finally:
        con.close()


def asks(root: Path, table: str = "asks") -> int:
    con = sqlite3.connect(root / ".crew/typesafe/ledger.sqlite")
    try:
        return ledger_tables.count(con, table)
    finally:
        con.close()


def _lines(stream: IO[str], into: queue.Queue[str]) -> None:
    for line in stream:
        into.put(line)


class Child:
    """``typesafe-judge serve`` as a child process, its output read line by line."""

    def __init__(self, root: Path, env: dict[str, str] | None = None) -> None:
        """Start the judge on root, with env added to this process's environment."""
        self.process = subprocess.Popen(  # noqa: S603  # nosec B603  # this interpreter and the test's own root
            [sys.executable, "-m", "typesafe_judge.cli", "serve", "--root", str(root)],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            env={**os.environ, **(env or {})},
        )
        self.stdout: queue.Queue[str] = queue.Queue()
        self.stderr: queue.Queue[str] = queue.Queue()
        for stream, into in (
            (self.process.stdout, self.stdout),
            (self.process.stderr, self.stderr),
        ):
            threading.Thread(target=_lines, args=(stream, into), daemon=True).start()
        self.ready = cast("Record", json.loads(self.stdout.get(timeout=WAIT)))

    def directory(self) -> Path:
        return Path(cast("str", self.ready["ask_token_file"])).parent

    def send(self, signum: int) -> None:
        self.process.send_signal(signum)

    def wait(self) -> int:
        return self.process.wait(WAIT)

    def await_stderr(self, text: str) -> None:
        deadline = time.monotonic() + WAIT
        while time.monotonic() < deadline:
            if text in self.stderr.get(timeout=WAIT):
                return
        pytest.fail(f"no stderr line with {text!r}")

    def rest_of_stdout(self) -> list[str]:
        self.wait()
        time.sleep(0.1)
        return list(self.stdout.queue)


@pytest.fixture
def children() -> Iterator[list[Child]]:
    started: list[Child] = []
    yield started
    for child in started:
        if child.process.poll() is None:
            child.process.kill()
            child.process.wait()


def test_root_without_crew_exits_2_with_one_line(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    assert main(["serve", "--root", str(tmp_path)]) == 2
    out, err = capsys.readouterr()
    assert out == ""
    assert err.count("\n") == 1
    assert err.startswith("typesafe-judge: ")
    assert "no .crew directory" in err


def test_invalid_bank_at_start_exits_2_naming_the_question_and_the_field(
    root: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    bank = root / ".crew/typesafe.yaml"
    bank.write_text(BANK.replace("yes_at: 0.7", "yes_at: high"), encoding="utf-8")
    assert main(["serve", "--root", str(root)]) == 2
    out, err = capsys.readouterr()
    assert out == ""
    assert err.count("\n") == 1
    assert f"question {QUESTION}" in err
    assert "field bands.yes_at" in err
    assert not (root / ".crew/typesafe/service.json").exists()


def test_newer_ledger_exits_2(root: Path, capsys: pytest.CaptureFixture[str]) -> None:
    (root / ".crew/typesafe").mkdir(mode=0o700)
    con = sqlite3.connect(root / ".crew/typesafe/ledger.sqlite")
    con.execute("PRAGMA user_version = 999")
    con.close()
    assert main(["serve", "--root", str(root)]) == 2
    assert "newer than the schema" in capsys.readouterr().err


def test_serve_prints_the_ready_line_and_stops_cleanly_on_sigterm(
    root: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    def stop_when_ready() -> None:
        deadline = time.monotonic() + WAIT
        while service_json(root) is None and time.monotonic() < deadline:
            time.sleep(0.02)
        os.kill(os.getpid(), signal.SIGTERM)

    handler = signal.getsignal(signal.SIGTERM)
    stopper = threading.Thread(target=stop_when_ready)
    stopper.start()
    code = main(["serve", "--root", str(root)])
    stopper.join()
    out, err = capsys.readouterr()
    assert code == 0
    [line] = out.splitlines()
    ready = json.loads(line)
    assert set(ready) == READY_KEYS
    assert ready["pid"] == os.getpid()
    assert "stopping" in err
    assert service_json(root) is None
    assert list((root / ".crew/typesafe/run").iterdir()) == [root / ".crew/typesafe/run/.lock"]
    assert signal.getsignal(signal.SIGTERM) == handler


def test_ready_line_names_a_reachable_instance(root: Path, children: list[Child]) -> None:
    child = Child(root)
    children.append(child)
    assert set(child.ready) == READY_KEYS
    assert child.ready["pid"] == child.process.pid
    assert child.directory().stat().st_mode & 0o777 == 0o700
    for scope in ("ask", "admin"):
        token_file = Path(cast("str", child.ready[f"{scope}_token_file"]))
        assert token_file.stat().st_mode & 0o777 == 0o600
    assert service_json(root) == child.ready
    status, health = call(child.ready, "/v1/health")
    assert status == 200
    assert health["instance"] == child.ready["instance"]
    child.send(signal.SIGTERM)
    assert child.wait() == 0
    assert child.rest_of_stdout() == []


@pytest.mark.parametrize("signum", [signal.SIGTERM, signal.SIGINT, signal.SIGHUP])
def test_stop_signal_removes_the_instance_and_exits_0(
    root: Path, children: list[Child], signum: int
) -> None:
    child = Child(root)
    children.append(child)
    child.send(signum)
    assert child.wait() == 0
    assert not child.directory().exists()
    assert service_json(root) is None


class _SlowTypeSafe(BaseHTTPRequestHandler):
    """TypeSafe's API, answering once the test releases it."""

    server: _SlowServer

    def do_POST(self) -> None:
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.server.arrived.set()
        self.server.release.wait(WAIT)
        answers = {name: {"type": "noul", "noul": 0.9} for name in body["questions"]}
        usage = {"input_tokens": 10, "output_tokens": 2}
        data = json.dumps({"model": "jev-1.13.0", "usage": usage, "answers": answers}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, fmt: str, /, *args: object) -> None:
        del fmt, args


class _SlowServer(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self) -> None:
        super().__init__(("127.0.0.1", 0), _SlowTypeSafe)
        self.arrived = threading.Event()
        self.release = threading.Event()


@pytest.fixture
def slow_typesafe() -> Iterator[_SlowServer]:
    server = _SlowServer()
    thread = threading.Thread(target=server.serve_forever, args=(0.05,), daemon=True)
    thread.start()
    yield server
    server.release.set()
    server.shutdown()
    server.server_close()


Outcome: TypeAlias = tuple[int, Record] | OSError


def ask_into(child: Child, result: queue.Queue[Outcome]) -> None:
    try:
        result.put(call(child.ready, "/v1/ask", {"questions": [QUESTION], "state": STATE}))
    except OSError as error:
        result.put(error)


def start_slow_ask(
    root: Path, children: list[Child], slow: _SlowServer
) -> tuple[Child, queue.Queue[Outcome]]:
    """Start serve against the slow TypeSafe, and an ask that waits on it."""
    base = f"http://127.0.0.1:{slow.server_address[1]}"
    child = Child(root, {"TYPESAFE_API_KEY": "test-key", "TYPESAFE_BASE_URL": base})
    children.append(child)
    result: queue.Queue[Outcome] = queue.Queue()
    threading.Thread(target=ask_into, args=(child, result), daemon=True).start()
    assert slow.arrived.wait(WAIT)
    return child, result


def test_sigterm_lets_a_slow_ask_finish_and_record_its_call(
    root: Path, children: list[Child], slow_typesafe: _SlowServer
) -> None:
    child, result = start_slow_ask(root, children, slow_typesafe)
    child.send(signal.SIGTERM)
    child.await_stderr("stopping")
    time.sleep(0.3)
    assert child.process.poll() is None
    slow_typesafe.release.set()
    outcome = result.get(timeout=WAIT)
    assert not isinstance(outcome, OSError)
    status, body = outcome
    assert status == 200
    answer = cast("dict[str, Record]", body["answers"])[QUESTION]
    assert answer["status"] == "answered"
    assert child.wait() == 0
    assert asks(root, "calls") == 1
    assert asks(root) == 1


def test_second_sigterm_exits_at_once(
    root: Path, children: list[Child], slow_typesafe: _SlowServer
) -> None:
    child, result = start_slow_ask(root, children, slow_typesafe)
    child.send(signal.SIGTERM)
    child.await_stderr("stopping")
    started = time.monotonic()
    child.send(signal.SIGTERM)
    assert child.wait() == 1
    assert time.monotonic() - started < 5
    child.await_stderr("forced")
    # The ask in flight was abandoned, its call never recorded.
    assert isinstance(result.get(timeout=WAIT), OSError)
    assert asks(root, "calls") == 0


def test_second_instance_shares_the_ledger_and_service_json_falls_back(
    root: Path, children: list[Child]
) -> None:
    older = Child(root)
    children.append(older)
    newer = Child(root)
    children.append(newer)
    assert service_json(root) == newer.ready
    for child in (older, newer):
        status, _ = call(child.ready, "/v1/ask", {"questions": [QUESTION], "state": STATE})
        assert status == 200
    assert asks(root) == 2
    newer.send(signal.SIGTERM)
    assert newer.wait() == 0
    assert service_json(root) == older.ready
    assert call(older.ready, "/v1/health")[0] == 200
    older.send(signal.SIGTERM)
    assert older.wait() == 0
    assert service_json(root) is None


def test_killed_instance_directory_is_removed_by_the_next_start(
    root: Path, children: list[Child]
) -> None:
    killed = Child(root)
    children.append(killed)
    killed.send(signal.SIGKILL)
    killed.wait()
    assert killed.directory().exists()
    successor = Child(root)
    children.append(successor)
    assert not killed.directory().exists()
    assert service_json(root) == successor.ready
    successor.send(signal.SIGTERM)
    assert successor.wait() == 0
