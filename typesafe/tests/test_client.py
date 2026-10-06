import json
from typing import TYPE_CHECKING

import httpx2
import pytest
from typesafe_sdk import Choice, Noul

from typesafe_judge.client import Failure, Reply, TypeSafe

if TYPE_CHECKING:
    from conftest import FakeTypeSafe

STATE = {"issue": "Add a ledger", "candidate": "Add a bank"}
QUESTIONS = {"issue_needs_candidate": Noul(instructions="Does `issue` build on `candidate`?")}


def test_an_answer_carries_model_usage_request_id_raw_bytes_and_one_attempt(
    typesafe: FakeTypeSafe,
) -> None:
    typesafe.model = "jev-1.13.0"
    typesafe.answers["issue_needs_candidate"] = {"type": "noul", "noul": 0.82, "extra": "kept"}

    reply = typesafe.client().send("jev-1.13.0", STATE, QUESTIONS)

    assert isinstance(reply, Reply)
    assert reply.model == "jev-1.13.0"
    assert reply.usage == {"input_tokens": 10, "output_tokens": 2}
    assert reply.request_id == "req_1"
    assert reply.attempts == 1
    assert json.loads(reply.raw)["model"] == "jev-1.13.0"
    # The answer object as TypeSafe sent it, fields the SDK does not model included.
    assert reply.answers == {
        "issue_needs_candidate": {"type": "noul", "noul": 0.82, "extra": "kept"}
    }
    [body] = typesafe.requests()
    assert body["model"] == "jev-1.13.0"
    assert body["state"] == STATE


def test_a_response_without_a_request_id_still_answers(typesafe: FakeTypeSafe) -> None:
    typesafe.request_id = None

    reply = typesafe.client().send("jev-1.13.0", STATE, QUESTIONS)

    assert isinstance(reply, Reply)
    assert reply.request_id is None


def test_each_step_of_an_attempt_times_out_after_60_seconds(typesafe: FakeTypeSafe) -> None:
    typesafe.client().send("jev-1.13.0", STATE, QUESTIONS)

    assert typesafe.timeouts == [{"connect": 60.0, "read": 60.0, "write": 60.0, "pool": 60.0}]


def test_two_overloads_then_an_answer_report_three_attempts(
    typesafe: FakeTypeSafe, sleeps: list[float]
) -> None:
    typesafe.faults = [529, 529]

    reply = typesafe.client().send("jev-1.13.0", STATE, QUESTIONS)

    assert isinstance(reply, Reply)
    assert reply.attempts == 3
    assert len(typesafe.requests()) == 3
    assert len(sleeps) == 2


def test_an_unauthorized_key_is_not_retried(typesafe: FakeTypeSafe, sleeps: list[float]) -> None:
    typesafe.faults = [401]

    assert typesafe.client().send("jev-1.13.0", STATE, QUESTIONS) == Failure("HTTP 401", 1)
    assert sleeps == []


def test_a_timeout_on_every_attempt_reports_timeout_and_three_attempts(
    typesafe: FakeTypeSafe, sleeps: list[float]
) -> None:
    typesafe.faults = [httpx2.ReadTimeout] * 3

    assert typesafe.client().send("jev-1.13.0", STATE, QUESTIONS) == Failure("timeout", 3)
    assert len(sleeps) == 2


def test_a_refused_connection_on_every_attempt_reports_unreachable(
    typesafe: FakeTypeSafe, sleeps: list[float]
) -> None:
    typesafe.faults = [httpx2.ConnectError] * 3

    assert typesafe.client().send("jev-1.13.0", STATE, QUESTIONS) == Failure("unreachable", 3)
    assert len(sleeps) == 2


def test_a_long_retry_after_stops_within_the_retry_budget(
    typesafe: FakeTypeSafe, sleeps: list[float]
) -> None:
    typesafe.faults = [httpx2.Response(429, headers={"Retry-After": "600"}, json={})] * 3

    failure = typesafe.client().send("jev-1.13.0", STATE, QUESTIONS)

    # Sleeping 600 s would overrun the 130 s budget, so the SDK stops instead of sleeping.
    assert failure == Failure("HTTP 429", 1)
    assert sleeps == []


@pytest.mark.parametrize(
    "response",
    [
        pytest.param(httpx2.Response(200, json={"answers": {}}), id="no model"),
        pytest.param(
            httpx2.Response(200, json={"model": "jev-1.13.0", "usage": {}, "answers": {}}),
            id="answer missing",
        ),
        pytest.param(
            httpx2.Response(
                200,
                json={
                    "model": "jev-1.13.0",
                    "usage": {},
                    "answers": {"issue_needs_candidate": {"type": "future", "x": 1}},
                },
            ),
            id="unknown answer type",
        ),
        pytest.param(
            httpx2.Response(
                200,
                json={
                    "model": "jev-1.13.0",
                    "usage": {},
                    "answers": {
                        "issue_needs_candidate": {
                            "type": "choice",
                            "choice": "a",
                            "confidence": 1.0,
                            "probabilities": {"a": 1.0},
                        }
                    },
                },
            ),
            id="answer of another primitive",
        ),
    ],
)
def test_an_answer_that_does_not_fit_the_questions_is_an_invalid_response(
    typesafe: FakeTypeSafe, response: httpx2.Response
) -> None:
    typesafe.faults = [response]

    assert typesafe.client().send("jev-1.13.0", STATE, QUESTIONS) == Failure("invalid response", 1)


def test_questions_of_every_primitive_answer_in_one_request(typesafe: FakeTypeSafe) -> None:
    questions: dict[str, Noul | Choice] = {
        **QUESTIONS,
        "kind": Choice(instructions="Which kind?", criteria={"bug": None, "feature": None}),
    }

    reply = typesafe.client().send("jev-1.13.0", STATE, questions)

    assert isinstance(reply, Reply)
    assert set(reply.answers) == {"issue_needs_candidate", "kind"}
    assert reply.answers["kind"]["choice"] == "bug"
    assert len(typesafe.requests()) == 1


def test_without_a_key_nothing_is_sent_and_the_reason_is_no_key(typesafe: FakeTypeSafe) -> None:
    client = typesafe.client(api_key=None)

    assert client.send("jev-1.13.0", STATE, QUESTIONS) == Failure("no key", 0)
    assert typesafe.requests() == []
    client.close()


def test_the_key_comes_from_the_environment(
    typesafe: FakeTypeSafe, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("TYPESAFE_API_KEY", "env-key")

    with TypeSafe(transport=httpx2.MockTransport(typesafe.handle)) as client:
        assert isinstance(client.send("jev-1.13.0", STATE, QUESTIONS), Reply)

    assert typesafe.headers[0]["authorization"] == "Bearer env-key"
