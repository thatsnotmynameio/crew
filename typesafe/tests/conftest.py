"""Fixtures shared by the tests: TypeSafe's API faked at the transport, and retry sleeps.

Tests import nothing from here at run time (pytest imports them in importlib mode);
they take the fixtures by name and import the types only for annotations.
"""

from __future__ import annotations

import json
import threading
import time
from typing import TYPE_CHECKING, TypeAlias

import httpx2
import pytest

from typesafe_judge.client import TypeSafe

if TYPE_CHECKING:
    from collections.abc import Callable, Mapping

Fault: TypeAlias = int | httpx2.Response | type[httpx2.TransportError]
"""What one attempt gets instead of an answer: a status, a whole response, or an error."""

MODEL = "jev-1.13.0"


def noul(p: float) -> dict[str, object]:
    """Return a noul answer."""
    return {"type": "noul", "noul": p}


def choice(probabilities: Mapping[str, float]) -> dict[str, object]:
    """Return a choice answer, its confidence computed as TypeSafe does."""
    n = len(probabilities)
    best = max(probabilities, key=lambda option: probabilities[option])
    confidence = (probabilities[best] - 1 / n) / (1 - 1 / n)
    return {
        "type": "choice",
        "choice": best,
        "confidence": confidence,
        "probabilities": dict(probabilities),
    }


def score(probabilities: list[float], legend: list[str]) -> dict[str, object]:
    """Return a score answer, its confidence measured around the most likely level."""
    n = len(probabilities)
    m = max(range(n), key=lambda level: probabilities[level])
    uniform = sum(abs(i - m) for i in range(n)) / n
    spread = sum(p * abs(i - m) for i, p in enumerate(probabilities))
    return {
        "type": "score",
        "score": sum(i * p for i, p in enumerate(probabilities)),
        "confidence": max(0.0, 1 - spread / uniform),
        "legend": {str(i): text for i, text in enumerate(legend)},
        "probabilities": {str(i): p for i, p in enumerate(probabilities)},
    }


def default_answer(question: Mapping[str, object]) -> dict[str, object]:
    """Return a confident answer to a question in the wire form."""
    criteria = question.get("criteria")
    match question["type"]:
        case "choice" if isinstance(criteria, dict):
            options = list(criteria)
            rest = 0.1 / (len(options) - 1) if len(options) > 1 else 0.0
            return choice({o: 0.9 if i == 0 else rest for i, o in enumerate(options)})
        case "score" if isinstance(criteria, list):
            levels = [str(level) for level in criteria]
            return score([1.0 if i == 0 else 0.0 for i in range(len(levels))], levels)
        case _:
            return noul(0.9)


class FakeTypeSafe:
    """TypeSafe's ``POST /v1/systemone`` behind an ``httpx2.MockTransport``.

    Each attempt first takes the next fault, if any. Otherwise it answers every question
    in the request from ``answers`` by name, or with a confident default, as ``model``.
    """

    def __init__(self) -> None:
        """Answer every question confidently, with no faults."""
        self.answers: dict[str, dict[str, object]] = {}
        self.faults: list[Fault] = []
        self.model = MODEL
        self.request_id: str | None = "req_1"
        self.bodies: list[dict[str, object]] = []
        self.headers: list[httpx2.Headers] = []
        self.timeouts: list[object] = []
        self.before_answer: Callable[[], object] | None = None
        self._lock = threading.Lock()

    def client(self, *, api_key: str | None = "test-key") -> TypeSafe:
        """Return the judge's client, sending to this fake."""
        return TypeSafe(api_key=api_key, transport=httpx2.MockTransport(self.handle))

    def requests(self) -> list[dict[str, object]]:
        """Return the bodies received, one per attempt."""
        with self._lock:
            return list(self.bodies)

    def sent(self) -> list[tuple[str, list[str]]]:
        """Return the model and the question names of each attempt."""
        sent: list[tuple[str, list[str]]] = []
        for body in self.requests():
            model, questions = body["model"], body["questions"]
            assert isinstance(model, str)
            assert isinstance(questions, dict)
            sent.append((model, list(questions)))
        return sent

    def handle(self, request: httpx2.Request) -> httpx2.Response:
        """Answer one attempt."""
        body = json.loads(request.content)
        with self._lock:
            self.bodies.append(body)
            self.headers.append(request.headers)
            self.timeouts.append(request.extensions.get("timeout"))
            fault = self.faults.pop(0) if self.faults else None
        if isinstance(fault, int):
            return httpx2.Response(fault, json={"detail": "fault"})
        if isinstance(fault, httpx2.Response):
            return fault
        if fault is not None:
            message = "fault"
            raise fault(message, request=request)
        if self.before_answer is not None:
            self.before_answer()
        headers = {} if self.request_id is None else {"x-typesafe-request-id": self.request_id}
        answers = {
            name: self.answers.get(name) or default_answer(question)
            for name, question in body["questions"].items()
        }
        return httpx2.Response(
            200,
            headers=headers,
            json={
                "model": self.model,
                "usage": {"input_tokens": 10, "output_tokens": 2},
                "answers": answers,
            },
        )


@pytest.fixture
def typesafe() -> FakeTypeSafe:
    """Return a fake TypeSafe API."""
    return FakeTypeSafe()


@pytest.fixture
def sleeps(monkeypatch: pytest.MonkeyPatch) -> list[float]:
    """Record the SDK's retry sleeps instead of sleeping (tenacity sleeps through time.sleep)."""
    slept: list[float] = []
    monkeypatch.setattr(time, "sleep", slept.append)
    return slept


@pytest.fixture(autouse=True)
def no_typesafe_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep the person's TypeSafe key and base URL out of every test."""
    for name in ("TYPESAFE_API_KEY", "TYPESAFE_BASE_URL", "TYPESAFE_DEFAULT_MODEL"):
        monkeypatch.delenv(name, raising=False)
