"""TypeSafe's SDK client as the judge uses it: retries, attempts and failure reasons (KTD10).

One ``TypeSafe`` serves a process and its threads. Each ``send`` is one SDK request
for one model over one state. It returns the answers with what the ledger records,
or a ``Failure`` whose reason is one of a fixed vocabulary: ``no key``,
``HTTP <status>``, ``timeout``, ``unreachable`` or ``invalid response``.
"""

import json
import logging
import threading
from dataclasses import dataclass
from typing import TYPE_CHECKING, Self, cast

import httpx2
from typesafe_sdk import (
    Choice,
    ChoiceAnswer,
    Noul,
    NoulAnswer,
    RetryPolicy,
    Score,
    ScoreAnswer,
    TypeSafeAPIConnectionError,
    TypeSafeAPIError,
    TypeSafeAPIResponseValidationError,
    TypeSafeAPITimeoutError,
    TypeSafeClient,
    TypeSafeError,
)

if TYPE_CHECKING:
    from collections.abc import Mapping
    from types import TracebackType

    from typesafe_sdk import JSONContent, SystemOneResponse

    from typesafe_judge.keys import JSON

STEP_TIMEOUT = httpx2.Timeout(60.0)
"""Each connect, read, write and pool wait of an attempt."""

RETRY = RetryPolicy(max_retries=2, timeout=130.0)
"""Three attempts on 408, 429, 5xx, connection errors and timeouts, the third starting
within 130 s; a ``Retry-After`` past that budget stops the retries instead of sleeping."""

SDK_LOGGER = "typesafe_sdk"
REQUEST_ID_HEADER = "x-typesafe-request-id"

NO_KEY = "no key"
TIMEOUT = "timeout"
UNREACHABLE = "unreachable"
INVALID_RESPONSE = "invalid response"

type SDKQuestion = Noul | Choice | Score

_ANSWER_TYPES: dict[type[SDKQuestion], type[NoulAnswer | ChoiceAnswer | ScoreAnswer]] = {
    Noul: NoulAnswer,
    Choice: ChoiceAnswer,
    Score: ScoreAnswer,
}


@dataclass(frozen=True, slots=True)
class Reply:
    """A request TypeSafe answered: each question's answer object, as sent, by name."""

    model: str
    usage: dict[str, JSON]
    request_id: str | None
    raw: bytes
    answers: dict[str, dict[str, JSON]]
    attempts: int


@dataclass(frozen=True, slots=True)
class Failure:
    """A request that got no answer: the reason, and the attempts it made."""

    reason: str
    attempts: int


class _Attempts(threading.local):
    """The attempts of the request this thread is sending."""

    def __init__(self) -> None:
        self.count = 0


class TypeSafe:
    """The SDK client, keyed by ``api_key`` or ``TYPESAFE_API_KEY``; keyless, it sends nothing."""

    def __init__(
        self, *, api_key: str | None = None, transport: httpx2.BaseTransport | None = None
    ) -> None:
        """Build the client; transport defaults to the network."""
        quiet_sdk_logger()
        self._attempts = _Attempts()
        http = httpx2.Client(
            timeout=STEP_TIMEOUT, transport=transport, event_hooks={"request": [self._count]}
        )
        self._client: TypeSafeClient | None
        try:
            self._client = TypeSafeClient(
                api_key=api_key, retry=RETRY, timeout=STEP_TIMEOUT, http_client=http
            )
        except TypeSafeError:
            # The constructor refuses a missing or malformed key, before any request.
            http.close()
            self._client = None

    def __enter__(self) -> Self:
        """Return the client, closed when the block ends."""
        return self

    def __exit__(
        self,
        kind: type[BaseException] | None,
        error: BaseException | None,
        traceback: TracebackType | None,
    ) -> None:
        """Close the client."""
        self.close()

    def close(self) -> None:
        """Close the HTTP connections."""
        if self._client is not None:
            self._client.close()

    def send(
        self, model: str, state: JSONContent, questions: Mapping[str, SDKQuestion]
    ) -> Reply | Failure:
        """Ask questions about state in one request to model."""
        if self._client is None:
            return Failure(NO_KEY, 0)
        self._attempts.count = 0
        try:
            response = self._client.system_one(state, questions, model=model)
        except TypeSafeAPITimeoutError:
            return Failure(TIMEOUT, self._attempts.count)
        except TypeSafeAPIConnectionError:
            return Failure(UNREACHABLE, self._attempts.count)
        except TypeSafeAPIResponseValidationError:
            return Failure(INVALID_RESPONSE, self._attempts.count)
        except TypeSafeAPIError as error:
            return Failure(f"HTTP {error.status}", self._attempts.count)
        return _reply(response, questions, self._attempts.count)

    def _count(self, _request: httpx2.Request) -> None:
        self._attempts.count += 1


def quiet_sdk_logger() -> None:
    """Keep the SDK's logger at INFO or above: at DEBUG it logs request and response bodies.

    The SDK applies an inherited ``TYPESAFE_LOG_LEVEL`` when it is imported.
    """
    logger = logging.getLogger(SDK_LOGGER)
    if logger.getEffectiveLevel() < logging.INFO:
        logger.setLevel(logging.INFO)


def _reply(
    response: SystemOneResponse, questions: Mapping[str, SDKQuestion], attempts: int
) -> Reply | Failure:
    """Return the answers to questions, or an invalid response when one is missing or misfit."""
    for name, question in questions.items():
        if not isinstance(response.answers.get(name), _ANSWER_TYPES[type(question)]):
            return Failure(INVALID_RESPONSE, attempts)
    raw = response.raw_http_response.content
    # The answers as sent, so the ledger keeps the fields this SDK does not model.
    sent = cast("dict[str, dict[str, JSON]]", json.loads(raw)["answers"])
    return Reply(
        model=response.model,
        usage=cast("dict[str, JSON]", response.usage.model_dump(mode="json")),
        # Read from the headers: the SDK's request_id property raises when it is absent.
        request_id=response.raw_http_response.headers.get(REQUEST_ID_HEADER),
        raw=raw,
        answers={name: sent[name] for name in questions},
        attempts=attempts,
    )
