import http.client
import json
from pathlib import Path
from typing import TYPE_CHECKING, cast
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
    bands: {yes_at: 0.7, no_at: 0.3}
    default: no
    stage: act
    bar: {max_error_yes: 1.0, max_error_no: 1.0, min_examples: 1, max_flip_rate: 0.5}
"""
Q = "issue_needs_candidate"

type Body = dict[str, JSON]


class Running:
    def __init__(self, ready: dict[str, JSON]) -> None:
        self.url = urlsplit(cast("str", ready["url"]))
        self.tokens = {
            scope: Path(cast("str", ready[f"{scope}_token_file"])).read_text(encoding="utf-8")
            for scope in ("ask", "admin")
        }

    def request(
        self, method: str, path: str, body: object = None, *, scope: str = "admin"
    ) -> tuple[int, Body]:
        con = http.client.HTTPConnection(cast("str", self.url.hostname), self.url.port, timeout=30)
        try:
            headers = {"Authorization": f"Bearer {self.tokens[scope]}"}
            data = None if body is None else json.dumps(body).encode()
            con.request(method, path, body=data, headers=headers)
            response = con.getresponse()
            return response.status, cast("Body", json.loads(response.read()))
        finally:
            con.close()


@pytest.fixture
def running(tmp_path: Path, typesafe: FakeTypeSafe) -> Iterator[Running]:
    (tmp_path / ".crew").mkdir()
    (tmp_path / ".crew/typesafe.yaml").write_text(BANK, encoding="utf-8")
    with typesafe.client() as client:
        service = Service(tmp_path, client)
        ready = service.start()
        yield Running(ready)
        service.stop(grace=5)


@pytest.mark.parametrize(
    ("method", "path", "body"),
    [("POST", "/v1/calibrations", {"question": Q}), ("GET", "/v1/stages", None)],
)
def test_the_ask_token_gets_403_on_calibrations_and_stages(
    running: Running, method: str, path: str, body: object
) -> None:
    status, error = running.request(method, path, body, scope="ask")

    assert status == 403
    assert error == {"error": {"code": "forbidden", "message": "forbidden"}}


def test_a_calibration_runs_with_the_admin_token(running: Running) -> None:
    status, body = running.request("POST", "/v1/calibrations", {"question": Q, "strong_only": True})

    assert status == 200
    calibration = cast("Body", body["calibration"])
    assert (calibration["question"], calibration["strong_only"]) == (Q, True)
    assert calibration["result"] == "insufficient"
    assert isinstance(calibration["id"], int)


@pytest.mark.parametrize(
    ("body", "code"),
    [
        ({"question": "unknown"}, "unknown_question"),
        ({"question": Q, "split_key": ""}, "bad_request"),
        ({"question": Q, "why": "x"}, "bad_request"),
    ],
)
def test_a_bad_calibration_gets_400(running: Running, body: Body, code: str) -> None:
    status, error = running.request("POST", "/v1/calibrations", body)

    assert status == 400
    assert cast("Body", error["error"])["code"] == code


def test_the_stage_report_runs_with_the_admin_token(running: Running) -> None:
    status, body = running.request("GET", "/v1/stages")

    assert status == 200
    (question,) = cast("list[Body]", body["questions"])
    assert (question["name"], question["effective_stage"]) == (Q, "act")
    assert question["declared_on_first_sight"] is True
