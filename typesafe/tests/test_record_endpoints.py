import http.client
import json
import sqlite3
from pathlib import Path
from typing import TYPE_CHECKING, TypeAlias, cast
from urllib.parse import urlsplit

import pytest

from typesafe_judge.service import Service

if TYPE_CHECKING:
    from collections.abc import Iterator

    from conftest import FakeTypeSafe

    from typesafe_judge.keys import JSON

BANK = """\
questions:
  issue_needs_candidate:
    primitive: noul
    instructions: Does `issue` build on `candidate`?
    model: jev-1.13.0
    bands: {{yes_at: {yes_at}, no_at: 0.3}}
    default: no
    stage: act
    bar: {{max_error_yes: 0.1, max_error_no: 0.1, min_examples: 1, max_flip_rate: 0.5}}
"""
Q1 = "issue_needs_candidate"
STATE = {"issue": "Add a ledger", "candidate": "Add a bank"}

Body: TypeAlias = dict[str, object]


def write_bank(root: Path, yes_at: float) -> None:
    (root / ".crew/typesafe.yaml").write_text(BANK.format(yes_at=yes_at), encoding="utf-8")


def rows(root: Path) -> int:
    """Count the rows of every ledger table."""
    con = sqlite3.connect(root / ".crew/typesafe/ledger.sqlite")
    try:
        tables = [n for (n,) in con.execute("SELECT name FROM sqlite_master WHERE type = 'table'")]
        return sum(
            con.execute(f"SELECT count(*) FROM {name}").fetchone()[0]  # noqa: S608 - a known table
            for name in tables
        )
    finally:
        con.close()


class Running:
    def __init__(self, ready: dict[str, JSON], root: Path) -> None:
        self.url = urlsplit(cast("str", ready["url"]))
        self.tokens = {
            scope: Path(cast("str", ready[f"{scope}_token_file"])).read_text(encoding="utf-8")
            for scope in ("ask", "admin")
        }
        self.root = root

    def post(self, path: str, body: object, *, scope: str = "ask") -> tuple[int, Body]:
        con = http.client.HTTPConnection(cast("str", self.url.hostname), self.url.port, timeout=30)
        try:
            headers = {"Authorization": f"Bearer {self.tokens[scope]}"}
            con.request("POST", path, body=json.dumps(body).encode(), headers=headers)
            response = con.getresponse()
            return response.status, cast("Body", json.loads(response.read()))
        finally:
            con.close()

    def ask(self, identifiers: dict[str, object] | None = None) -> int:
        body = {"questions": [Q1], "state": STATE, "identifiers": identifiers or {}}
        status, answered = self.post("/v1/ask", body)
        assert status == 200
        return cast("int", cast("dict[str, Body]", answered["answers"])[Q1]["ask_id"])


@pytest.fixture
def running(tmp_path: Path, typesafe: FakeTypeSafe) -> Iterator[Running]:
    (tmp_path / ".crew").mkdir()
    write_bank(tmp_path, 0.7)
    with typesafe.client() as client:
        service = Service(tmp_path, client)
        ready = service.start()
        yield Running(ready, tmp_path)
        service.stop(grace=5)


def test_a_decision_is_recorded_with_the_ask_token(running: Running) -> None:
    ask_id = running.ask()

    status, body = running.post("/v1/decisions", {"ask_id": ask_id, "decision": "acted"})

    assert status == 200
    assert {k: body[k] for k in ("ask_id", "decision", "effective_stage")} == {
        "ask_id": ask_id,
        "decision": "acted",
        "effective_stage": "act",
    }
    assert isinstance(body["decision_id"], int)
    assert "instance" in body


@pytest.mark.parametrize(
    ("body", "code"),
    [
        ({"ask_id": 999, "decision": "acted"}, "unknown_ask"),
        ({"ask_id": "1", "decision": "acted"}, "bad_request"),
        ({"ask_id": True, "decision": "acted"}, "bad_request"),
        ({"ask_id": 1, "decision": "maybe"}, "bad_request"),
        ({"ask_id": 1}, "bad_request"),
        ({"ask_id": 1, "decision": "acted", "why": "x"}, "bad_request"),
        ([], "bad_request"),
    ],
)
def test_a_bad_decision_gets_400_and_adds_no_row(running: Running, body: object, code: str) -> None:
    running.ask()
    before = rows(running.root)

    status, error = running.post("/v1/decisions", body)

    assert status == 400
    assert cast("Body", error["error"])["code"] == code
    assert rows(running.root) == before


def test_an_outcome_is_recorded_then_acknowledged_with_the_admin_token(running: Running) -> None:
    running.ask({"issue": 5})
    outcome = {
        "question": Q1,
        "identifiers": {"issue": 5},
        "value": True,
        "source": "blocked_by",
        "strength": "strong",
    }

    first = running.post("/v1/outcomes", outcome, scope="admin")
    again = running.post("/v1/outcomes", outcome, scope="admin")

    assert (first[0], first[1]["recorded"], first[1]["acknowledged"]) == (200, 1, 0)
    assert (again[0], again[1]["recorded"], again[1]["acknowledged"]) == (200, 0, 1)


VALID: Body = {"value": True, "source": "blocked_by", "strength": "strong"}


@pytest.mark.parametrize(
    ("body", "code"),
    [
        ({**VALID, "ask_id": 999}, "no_match"),
        ({**VALID, "question": Q1, "identifiers": {"issue": 6}}, "no_match"),
        ({**VALID, "question": Q1, "identifiers": {}}, "bad_request"),
        ({**VALID, "question": Q1, "identifiers": {"issue": 1.5}}, "invalid_state"),
        ({**VALID, "question": Q1, "identifiers": "issue=5"}, "bad_request"),
        ({**VALID, "question": 5, "identifiers": {"issue": 5}}, "bad_request"),
        ({**VALID, "ask_id": 1, "question": Q1}, "bad_request"),
        ({**VALID}, "bad_request"),
        ({**VALID, "ask_id": "1"}, "bad_request"),
        ({**VALID, "ask_id": 1, "value": "yes"}, "invalid_value"),
        ({**VALID, "ask_id": 1, "source": ""}, "bad_request"),
        ({**VALID, "ask_id": 1, "strength": "certain"}, "bad_request"),
        ({"ask_id": 1, "value": True, "source": "blocked_by"}, "bad_request"),
    ],
)
def test_a_bad_outcome_gets_400_and_adds_no_row(running: Running, body: Body, code: str) -> None:
    running.ask({"issue": 5})
    before = rows(running.root)

    status, error = running.post("/v1/outcomes", body, scope="admin")

    assert status == 400
    assert cast("Body", error["error"])["code"] == code
    assert rows(running.root) == before


def test_a_recheck_runs_with_the_admin_token(running: Running, typesafe: FakeTypeSafe) -> None:
    running.ask()
    write_bank(running.root, 0.95)

    status, body = running.post("/v1/rechecks", {"question": Q1}, scope="admin")

    assert status == 200
    recheck = cast("Body", body["recheck"])
    assert (recheck["result"], recheck["flipped"], recheck["flips_by_kind"]) == (
        "failed",
        1,
        {"yes->uncertain": 1},
    )
    assert recheck["effective_stage"] == "shadow"
    assert len(typesafe.requests()) == 1


@pytest.mark.parametrize(
    ("body", "code"),
    [
        ({"question": "unknown"}, "unknown_question"),
        ({"question": Q1}, "no_trusted_version"),
        ({"question": Q1, "from_version": "unknown"}, "unknown_version"),
        ({"question": Q1, "from_version": 1}, "bad_request"),
        ({"question": [Q1]}, "bad_request"),
        ({}, "bad_request"),
    ],
)
def test_a_bad_recheck_gets_400_and_adds_no_row(running: Running, body: Body, code: str) -> None:
    running.ask()
    before = rows(running.root)

    status, error = running.post("/v1/rechecks", body, scope="admin")

    assert status == 400
    assert cast("Body", error["error"])["code"] == code
    assert rows(running.root) == before


@pytest.mark.parametrize("path", ["/v1/outcomes", "/v1/rechecks"])
def test_the_ask_token_gets_403_on_outcomes_and_rechecks(running: Running, path: str) -> None:
    ask_id = running.ask({"issue": 5})
    write_bank(running.root, 0.95)
    body = {"question": Q1} if path == "/v1/rechecks" else {**VALID, "ask_id": ask_id}
    running.post("/v1/ask", {"questions": [Q1], "state": STATE})  # records the edited bank
    before = rows(running.root)

    status, error = running.post(path, body, scope="ask")

    assert status == 403
    assert error == {"error": {"code": "forbidden", "message": "forbidden"}}
    assert rows(running.root) == before
    assert running.post(path, body, scope="admin")[0] == 200
