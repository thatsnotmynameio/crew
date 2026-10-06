import http.client
import json
import logging
import sqlite3
import subprocess
import sys
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import TYPE_CHECKING, cast
from urllib.parse import quote, urlsplit

import pytest

from typesafe_judge import service as judge_service
from typesafe_judge.service import ROUTES, Route, Scope, Service

if TYPE_CHECKING:
    from collections.abc import Iterator

    from conftest import FakeTypeSafe

    from typesafe_judge.keys import JSON
    from typesafe_judge.service import Call

QUESTION = """\
  {name}:
    primitive: noul
    instructions: {instructions}
    model: jev-1.13.0
    bands: {{yes_at: 0.7, no_at: 0.3}}
    default: no
    stage: act
    bar: {{max_error_yes: 0.1, max_error_no: 0.1, min_examples: 30, max_flip_rate: 0.05}}
"""
Q1 = "issue_needs_candidate"
Q2 = "candidate_needs_issue"
BANK = "questions:\n" + QUESTION.format(name=Q1, instructions="Does `issue` build on `candidate`?")
SECOND = QUESTION.format(name=Q2, instructions="Does `candidate` build on `issue`?")
STATE = {"issue": "Add a ledger", "candidate": "Add a bank"}
READY_KEYS = {"instance", "pid", "url", "ask_token_file", "admin_token_file"}

type Body = dict[str, object]


def write_bank(root: Path, text: str) -> None:
    path = root / ".crew/typesafe.yaml"
    path.write_text(text, encoding="utf-8")


def rows(root: Path, table: str | None = None) -> int:
    """Count the ledger's rows, in one table or in all of them."""
    con = sqlite3.connect(root / ".crew/typesafe/ledger.sqlite")
    try:
        listed = [n for (n,) in con.execute("SELECT name FROM sqlite_master WHERE type = 'table'")]
        tables = listed if table is None else [table]
        total = 0
        for name in tables:
            total += con.execute(f"SELECT count(*) FROM {name}").fetchone()[0]  # noqa: S608 - a known table
        return total
    finally:
        con.close()


def request(
    url: str,
    method: str,
    path: str,
    *,
    token: str | None,
    body: bytes | None = None,
    headers: dict[str, str] | None = None,
) -> tuple[int, Body]:
    parts = urlsplit(url)
    con = http.client.HTTPConnection(cast("str", parts.hostname), parts.port, timeout=30)
    try:
        sent = {} if token is None else {"Authorization": f"Bearer {token}"}
        sent.update(headers or {})
        con.request(method, path, body=body, headers=sent)
        response = con.getresponse()
        return response.status, cast("Body", json.loads(response.read()))
    finally:
        con.close()


@dataclass
class Running:
    service: Service
    ready: dict[str, JSON]
    root: Path

    def token(self, scope: str = "ask") -> str:
        return Path(cast("str", self.ready[f"{scope}_token_file"])).read_text(encoding="utf-8")

    def get(self, path: str, *, scope: str = "ask") -> tuple[int, Body]:
        return request(cast("str", self.ready["url"]), "GET", path, token=self.token(scope))

    def post(self, path: str, body: object, *, scope: str = "ask") -> tuple[int, Body]:
        data = json.dumps(body).encode()
        return request(
            cast("str", self.ready["url"]), "POST", path, token=self.token(scope), body=data
        )

    def ask(self, body: object) -> tuple[int, Body]:
        return self.post("/v1/ask", body)


def answers(body: Body) -> dict[str, dict[str, object]]:
    return cast("dict[str, dict[str, object]]", body["answers"])


@pytest.fixture
def root(tmp_path: Path) -> Path:
    (tmp_path / ".crew").mkdir()
    write_bank(tmp_path, BANK)
    return tmp_path


@pytest.fixture
def running(root: Path, typesafe: FakeTypeSafe) -> Iterator[Running]:
    with typesafe.client() as client:
        service = Service(root, client)
        ready = service.start()
        yield Running(service, ready, root)
        service.stop(grace=5)


def test_start_writes_instance_files_and_names_them_in_service_json(running: Running) -> None:
    ready = running.ready
    assert set(ready) == READY_KEYS
    run = running.root / ".crew/typesafe/run"
    directory = run / cast("str", ready["instance"])
    assert directory.stat().st_mode & 0o777 == 0o700
    for scope in ("ask", "admin"):
        token_file = Path(cast("str", ready[f"{scope}_token_file"]))
        assert token_file.parent == directory
        assert token_file.stat().st_mode & 0o777 == 0o600
        assert len(token_file.read_text(encoding="utf-8")) >= 32
    assert running.token("ask") != running.token("admin")
    service_file = running.root / ".crew/typesafe/service.json"
    assert json.loads(service_file.read_text(encoding="utf-8")) == ready
    assert service_file.stat().st_mode & 0o777 == 0o600
    status, health = running.get("/v1/health")
    assert status == 200
    assert health["instance"] == ready["instance"]


def test_stop_removes_its_instance_files_and_service_json(
    root: Path, typesafe: FakeTypeSafe
) -> None:
    with typesafe.client() as client:
        service = Service(root, client)
        ready = service.start()
        assert service.stop(grace=5)
    assert not (root / ".crew/typesafe/run" / cast("str", ready["instance"])).exists()
    assert not (root / ".crew/typesafe/service.json").exists()


def test_health_reports_the_bank_and_the_key(running: Running) -> None:
    status, body = running.get("/v1/health")
    assert status == 200
    assert body == {
        "instance": cast("object", running.ready["instance"]),
        "bank_error": None,
        "bank": {"status": "present", "questions": 1},
        "has_key": True,
    }


def test_health_without_a_bank_or_a_key(tmp_path: Path, typesafe: FakeTypeSafe) -> None:
    (tmp_path / ".crew").mkdir()
    with typesafe.client(api_key=None) as client:
        service = Service(tmp_path, client)
        ready = service.start()
        try:
            url, token_file = cast("str", ready["url"]), cast("str", ready["ask_token_file"])
            token = Path(token_file).read_text(encoding="utf-8")
            status, body = request(url, "GET", "/v1/health", token=token)
        finally:
            service.stop(grace=5)
    assert status == 200
    assert body["bank"] == {"status": "absent", "questions": 0}
    assert body["has_key"] is False


def test_questions_lists_each_question_with_its_stages(running: Running) -> None:
    status, body = running.get("/v1/questions")
    assert status == 200
    [listed] = cast("list[dict[str, object]]", body["questions"])
    assert listed["name"] == Q1
    assert listed["primitive"] == "noul"
    assert listed["model"] == "jev-1.13.0"
    assert listed["declared_stage"] == "act"
    # The first version of a name keeps its declared stage.
    assert listed["effective_stage"] == "act"
    assert isinstance(listed["version"], str)
    assert len(listed["version"]) == 64


def test_ask_answers_then_replays(running: Running, typesafe: FakeTypeSafe) -> None:
    status, first = running.ask({"questions": [Q1], "state": STATE})
    assert status == 200
    assert first["instance"] == running.ready["instance"]
    assert first["bank_error"] is None
    answer = answers(first)[Q1]
    assert answer["status"] == "answered"
    assert answer["replayed"] is False
    assert answer["verdict"] == "yes"
    assert answer["effective_stage"] == "act"
    status, second = running.ask({"questions": [Q1], "state": STATE})
    assert status == 200
    assert answers(second)[Q1]["replayed"] is True
    assert answers(second)[Q1]["ask_id"] != answer["ask_id"]
    assert len(typesafe.sent()) == 1
    assert rows(running.root, "asks") == 2


def test_ask_without_a_key_cannot_judge(tmp_path: Path, typesafe: FakeTypeSafe) -> None:
    (tmp_path / ".crew").mkdir()
    write_bank(tmp_path, BANK)
    with typesafe.client(api_key=None) as client:
        service = Service(tmp_path, client)
        ready = service.start()
        try:
            running = Running(service, ready, tmp_path)
            status, body = running.ask({"questions": [Q1], "state": STATE})
        finally:
            service.stop(grace=5)
    assert status == 200
    answer = answers(body)[Q1]
    assert answer["status"] == "cannot_judge"
    assert answer["reason"] == "no key"
    assert answer["default"] == "no"
    assert typesafe.sent() == []


def test_ae7_question_added_while_running_is_answered_and_recorded(running: Running) -> None:
    status, body = running.ask({"questions": [Q2], "state": STATE})
    assert status == 400
    write_bank(running.root, BANK + SECOND)
    status, body = running.ask({"questions": [Q2], "state": STATE})
    assert status == 200
    assert answers(body)[Q2]["status"] == "answered"
    assert rows(running.root, "asks") == 1
    _, listing = running.get("/v1/questions")
    assert [q["name"] for q in cast("list[dict[str, object]]", listing["questions"])] == [Q1, Q2]


def test_invalid_bank_edit_keeps_the_last_valid_bank_and_reports_it(running: Running) -> None:
    write_bank(running.root, BANK.replace("yes_at: 0.7", "yes_at: high"))
    _, health = running.get("/v1/health")
    error = health["bank_error"]
    assert isinstance(error, str)
    assert Q1 in error
    assert "bands.yes_at" in error
    assert health["bank"] == {"status": "present", "questions": 1}
    status, body = running.ask({"questions": [Q1], "state": STATE})
    assert status == 200
    assert body["bank_error"] == error
    assert answers(body)[Q1]["status"] == "answered"
    write_bank(running.root, BANK)
    _, health = running.get("/v1/health")
    assert health["bank_error"] is None


def test_asks_finds_asks_whose_identifiers_hold_the_given_ones(running: Running) -> None:
    ids = []
    for identifiers in ({"issue": 5, "candidate": 7}, {"issue": 5, "candidate": 8}, {"issue": "5"}):
        state = {"issue": "Add a ledger", "candidate": str(identifiers.get("candidate"))}
        _, body = running.ask({"questions": [Q1], "state": state, "identifiers": identifiers})
        ids.append(answers(body)[Q1]["ask_id"])
    query = quote(json.dumps({"issue": 5}))
    status, body = running.get(f"/v1/asks?question={Q1}&identifiers={query}")
    assert status == 200
    found = cast("list[dict[str, object]]", body["asks"])
    assert [a["ask_id"] for a in found] == ids[:2]
    assert found[0]["identifiers"] == {"issue": 5, "candidate": 7}
    assert found[0]["status"] == "answered"
    assert found[0]["verdict"] == "yes"
    assert found[0]["probabilities"] == 0.9
    assert found[0]["effective_stage"] == "act"
    query = quote(json.dumps({"issue": "5"}))
    _, body = running.get(f"/v1/asks?question={Q1}&identifiers={query}")
    assert [a["ask_id"] for a in cast("list[dict[str, object]]", body["asks"])] == ids[2:]
    _, body = running.get(f"/v1/asks?question={Q1}")
    assert len(cast("list[object]", body["asks"])) == 3


@pytest.mark.parametrize(
    "query",
    ["", f"question={Q1}&identifiers=%7B", f"question={Q1}&identifiers=%5B%5D", "question=a&x=1"],
)
def test_asks_refuses_a_bad_query(running: Running, query: str) -> None:
    status, body = running.get(f"/v1/asks?{query}")
    assert status == 400
    assert set(cast("dict[str, object]", body["error"])) == {"code", "message"}


@pytest.mark.parametrize("token", [None, "wrong"])
def test_missing_or_wrong_token_gets_401_and_adds_no_row(
    running: Running, token: str | None
) -> None:
    before = rows(running.root)
    url = cast("str", running.ready["url"])
    body = json.dumps({"questions": [Q1], "state": STATE}).encode()
    status, error = request(url, "POST", "/v1/ask", token=token, body=body)
    assert status == 401
    assert error == {"error": {"code": "unauthorized", "message": "unauthorized"}}
    assert rows(running.root) == before


def test_admin_token_reaches_the_ask_routes(running: Running) -> None:
    assert running.get("/v1/health", scope="admin")[0] == 200
    status, _ = running.post("/v1/ask", {"questions": [Q1], "state": STATE}, scope="admin")
    assert status == 200


def recorded(call: Call) -> dict[str, JSON]:
    """Stand in for an admin endpoint a later unit adds; it writes a ledger row."""
    with call.ledger.write() as writes:
        writes.store_state(STATE)
    return {"done": True}


@pytest.mark.parametrize(
    ("method", "path"),
    [
        ("POST", "/v1/outcomes"),
        ("POST", "/v1/rechecks"),
        ("POST", "/v1/calibrations"),
        ("GET", "/v1/stages"),
    ],
)
def test_ask_token_on_an_admin_route_gets_403_and_adds_no_row(
    running: Running, monkeypatch: pytest.MonkeyPatch, method: str, path: str
) -> None:
    monkeypatch.setitem(ROUTES, (method, path), Route(Scope.ADMIN, recorded))
    url = cast("str", running.ready["url"])
    before = rows(running.root)
    body = b"{}" if method == "POST" else None
    status, error = request(url, method, path, token=running.token("ask"), body=body)
    assert status == 403
    assert error == {"error": {"code": "forbidden", "message": "forbidden"}}
    assert rows(running.root) == before
    status, done = request(url, method, path, token=running.token("admin"), body=body)
    assert status == 200
    assert done["done"] is True


@pytest.mark.parametrize("host", ["example.com", "example.com:{port}", "127.0.0.1:1", "", None])
def test_non_loopback_host_is_refused(running: Running, host: str | None) -> None:
    url = cast("str", running.ready["url"])
    port = urlsplit(url).port
    con = http.client.HTTPConnection("127.0.0.1", port, timeout=30)
    try:
        con.putrequest("GET", "/v1/health", skip_host=True)
        if host is not None:
            con.putheader("Host", host.format(port=port))
        con.putheader("Authorization", f"Bearer {running.token()}")
        con.endheaders()
        response = con.getresponse()
        status, body = response.status, json.loads(response.read())
    finally:
        con.close()
    assert status == 400
    assert body["error"]["code"] == "bad_host"


@pytest.mark.parametrize("host", ["localhost", "LOCALHOST", "[::1]", "127.0.0.1"])
def test_loopback_hosts_are_accepted(running: Running, host: str) -> None:
    url = cast("str", running.ready["url"])
    port = urlsplit(url).port
    status, _ = request(
        url, "GET", "/v1/health", token=running.token(), headers={"Host": f"{host}:{port}"}
    )
    assert status == 200


def test_body_above_1_mib_gets_413_and_adds_no_row(running: Running) -> None:
    before = rows(running.root)
    url = cast("str", running.ready["url"])
    # Only the length is sent: the service must answer without reading a body.
    headers = {"Content-Length": str(2 << 20)}
    status, body = request(url, "POST", "/v1/ask", token=running.token(), headers=headers)
    assert status == 413
    assert cast("dict[str, object]", body["error"])["code"] == "too_large"
    assert rows(running.root) == before


@pytest.mark.parametrize(
    ("body", "code"),
    [
        (b"{not json", "malformed_json"),
        (b"\xff", "malformed_json"),
        (json.dumps({"questions": ["nope"], "state": STATE}).encode(), "unknown_question"),
        (json.dumps({"questions": [Q1], "state": {"x": 1.0}}).encode(), "invalid_state"),
        (json.dumps({"questions": [Q1], "state": "s", "identifiers": {"a": 1.5}}).encode(), None),
        (json.dumps({"questions": [Q1], "state": "s", "identifiers": []}).encode(), None),
        (json.dumps({"questions": [Q1], "state": "s", "fresh": "yes"}).encode(), None),
        (json.dumps({"questions": [], "state": "s"}).encode(), None),
        (json.dumps({"questions": [1], "state": "s"}).encode(), None),
        (json.dumps({"state": "s"}).encode(), None),
        (json.dumps({"questions": [Q1], "state": "s", "extra": 1}).encode(), None),
        (json.dumps([Q1]).encode(), None),
    ],
)
def test_bad_ask_gets_400_and_adds_no_row(running: Running, body: bytes, code: str | None) -> None:
    before = rows(running.root)
    url = cast("str", running.ready["url"])
    status, error = request(url, "POST", "/v1/ask", token=running.token(), body=body)
    assert status == 400
    detail = cast("dict[str, object]", error["error"])
    assert detail["code"] == (code or detail["code"])
    assert isinstance(detail["message"], str)
    assert rows(running.root) == before


@pytest.mark.parametrize("length", ["abc", "-1", "1_0"])
def test_bad_content_length_gets_400(running: Running, length: str) -> None:
    url = cast("str", running.ready["url"])
    headers = {"Content-Length": length}
    status, _ = request(url, "POST", "/v1/ask", token=running.token(), headers=headers)
    assert status == 400


def test_unknown_path_or_method_gets_a_json_error(running: Running) -> None:
    url = cast("str", running.ready["url"])
    status, body = request(url, "GET", "/v1/nope", token=running.token())
    assert status == 404
    assert body == {"error": {"code": "not_found", "message": "no endpoint GET /v1/nope"}}
    status, body = request(url, "PUT", "/v1/ask", token=running.token())
    assert status == 501
    assert set(cast("dict[str, object]", body["error"])) == {"code", "message"}


def test_unexpected_error_gets_500_and_logs_only_its_type_and_route(
    running: Running, monkeypatch: pytest.MonkeyPatch, caplog: pytest.LogCaptureFixture
) -> None:
    def broken(*_args: object, **_kwargs: object) -> object:
        message = "secret state"
        raise RuntimeError(message)

    monkeypatch.setattr(judge_service, "ask", broken)
    with caplog.at_level(logging.INFO, logger="typesafe_judge"):
        status, body = running.ask({"questions": [Q1], "state": {"issue": "private words"}})
    assert status == 500
    assert body == {"error": {"code": "internal", "message": "internal error"}}
    assert "POST /v1/ask failed: RuntimeError" in caplog.text
    assert "secret" not in caplog.text
    assert "private" not in caplog.text
    assert running.token() not in caplog.text


def dead_pid() -> int:
    process = subprocess.Popen([sys.executable, "-c", "pass"])
    process.wait()
    return process.pid


def test_stale_instance_directories_are_removed_on_start(
    root: Path, typesafe: FakeTypeSafe
) -> None:
    run = root / ".crew/typesafe/run"
    run.mkdir(parents=True)
    stale = run / "stale"
    stale.mkdir()
    record = {"instance": "stale", "pid": dead_pid(), "started": 1}
    (stale / "instance.json").write_text(json.dumps(record), encoding="utf-8")
    junk = run / "junk"
    junk.mkdir()
    with typesafe.client() as client:
        service = Service(root, client)
        ready = service.start()
        try:
            assert not stale.exists()
            assert not junk.exists()
            assert (run / cast("str", ready["instance"])).is_dir()
        finally:
            service.stop(grace=5)


def service_json(root: Path) -> dict[str, object] | None:
    path = root / ".crew/typesafe/service.json"
    if not path.exists():
        return None
    return cast("dict[str, object]", json.loads(path.read_text(encoding="utf-8")))


def test_newest_instance_is_named_and_the_older_one_takes_over(
    root: Path, typesafe: FakeTypeSafe
) -> None:
    with typesafe.client() as client:
        older = Service(root, client)
        first = older.start()
        newer = Service(root, client)
        second = newer.start()
        assert service_json(root) == second
        older_running = Running(older, first, root)
        assert older_running.ask({"questions": [Q1], "state": STATE})[0] == 200
        assert Running(newer, second, root).ask({"questions": [Q1], "state": STATE})[0] == 200
        assert rows(root, "asks") == 2
        newer.stop(grace=5)
        assert service_json(root) == first
        older.stop(grace=5)
        assert service_json(root) is None


def test_stopping_an_instance_service_json_does_not_name_leaves_it(
    root: Path, typesafe: FakeTypeSafe
) -> None:
    with typesafe.client() as client:
        older = Service(root, client)
        older.start()
        newer = Service(root, client)
        second = newer.start()
        older.stop(grace=5)
        assert service_json(root) == second
        newer.stop(grace=5)


def test_stop_waits_for_an_ask_in_flight(root: Path, typesafe: FakeTypeSafe) -> None:
    arrived, release = threading.Event(), threading.Event()

    def slow() -> None:
        arrived.set()
        release.wait(30)

    typesafe.before_answer = slow
    with typesafe.client() as client:
        service = Service(root, client)
        running = Running(service, service.start(), root)
        result: list[tuple[int, Body]] = []
        asker = threading.Thread(
            target=lambda: result.append(running.ask({"questions": [Q1], "state": STATE}))
        )
        asker.start()
        assert arrived.wait(30)
        stopped: list[bool] = []
        stopper = threading.Thread(target=lambda: stopped.append(service.stop(grace=30)))
        stopper.start()
        time.sleep(0.3)
        assert stopper.is_alive()
        release.set()
        stopper.join(30)
        asker.join(30)
    assert stopped == [True]
    assert result[0][0] == 200
    assert rows(root, "calls") == 1


def test_stop_gives_up_on_a_request_past_the_grace(root: Path, typesafe: FakeTypeSafe) -> None:
    arrived, release = threading.Event(), threading.Event()

    def stuck() -> None:
        arrived.set()
        release.wait(30)

    typesafe.before_answer = stuck
    with typesafe.client() as client:
        service = Service(root, client)
        running = Running(service, service.start(), root)
        asker = threading.Thread(
            target=lambda: running.ask({"questions": [Q1], "state": STATE}), daemon=True
        )
        asker.start()
        assert arrived.wait(30)
        try:
            assert service.stop(grace=0.2) is False
        finally:
            release.set()
            asker.join(30)
