"""The judge's HTTP service on 127.0.0.1: endpoints, tokens and instance files (KTD1, KTD18).

Every request names its token as ``Authorization: Bearer <token>``. The service checks,
in order, before it reads a body:

- the ``Host`` header names the loopback address with the service's port, else 400
  ``bad_host`` (a page in a browser reaching the port through DNS rebinding sends its
  own host name);
- the token is the ask or the admin token, compared in constant time, else 401;
- the endpoint exists, else 404, and the token's scope allows it, else 403;
- a body declares at most 1 MiB, else 413.

Every success is a JSON object carrying ``instance`` and ``bank_error``, the error of
the last bank reload or null. Every failure is ``{"error": {"code", "message"}}``; a
request refused with 400 never touches the ledger.

Each start writes ``.crew/typesafe/run/<instance>/`` (0700) with the two token files
and ``instance.json`` (0600), and names the instance in ``.crew/typesafe/service.json``.
A stop removes the directory and points ``service.json`` at the newest remaining live
instance, or removes it (KTD19).
"""

import fcntl
import hmac
import json
import logging
import os
import re
import secrets
import shutil
import socketserver
import tempfile
import threading
import time
from contextlib import contextmanager
from dataclasses import dataclass
from enum import Enum
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import TYPE_CHECKING, TypeAlias, cast
from urllib.parse import parse_qs, urlsplit

from typesafe_judge.asking import UnknownQuestionError, ask, effective_stage, probabilities, verdict
from typesafe_judge.bank import BankFile
from typesafe_judge.calibrate import calibrate, parse_calibration
from typesafe_judge.keys import StateError, canonical_identifiers
from typesafe_judge.ledger import DIRECTORY, Ledger
from typesafe_judge.recheck import recheck
from typesafe_judge.records import (
    InvalidRequestError,
    decide,
    parse_decision,
    parse_outcome,
    record_outcome,
)
from typesafe_judge.stages import stage_report

if TYPE_CHECKING:
    import socket
    from collections.abc import Callable, Iterator

    from typesafe_judge.bank import Bank, Question
    from typesafe_judge.client import TypeSafe
    from typesafe_judge.keys import JSON
    from typesafe_judge.ledger import AskRecord

LONGEST_ASK = 190.0
"""Seconds an ask that reaches TypeSafe can take (KTD10); a stop waits this long."""

MAX_BODY = 1 << 20
SOCKET_TIMEOUT = 30.0
"""Seconds each read or write on a connection may wait."""

POLL_INTERVAL = 0.1
"""Seconds between the server's checks for a stop."""

BANK_FILE = Path(".crew/typesafe.yaml")
RUN_DIRECTORY = DIRECTORY / "run"
SERVICE_FILE = DIRECTORY / "service.json"
INSTANCE_FILE = "instance.json"
LOCK_FILE = ".lock"
SERVICE_FIELDS = ("instance", "pid", "url", "ask_token_file", "admin_token_file")
LOOPBACK_HOSTS = ("127.0.0.1", "localhost", "[::1]")

_log = logging.getLogger(__name__)


class ConfigError(Exception):
    """A root or an environment the service cannot run in."""


class Scope(Enum):
    """What a token allows: the ask scope, or the admin scope that also allows it."""

    ASK = "ask"
    ADMIN = "admin"


class RequestError(Exception):
    """A request refused with an HTTP status, an error code and a message."""

    def __init__(self, status: HTTPStatus, code: str, message: str) -> None:
        """Keep the status and the error the response carries."""
        super().__init__(message)
        self.status = status
        self.body: dict[str, JSON] = {"error": {"code": code, "message": message}}


def bad_request(message: str, code: str = "bad_request") -> RequestError:
    """Return a 400 refusal."""
    return RequestError(HTTPStatus.BAD_REQUEST, code, message)


@dataclass(frozen=True, slots=True)
class Call:
    """One authorised request, as a route's handler sees it."""

    bank: Bank
    ledger: Ledger
    client: TypeSafe
    body: object
    query: dict[str, list[str]]


Handler: TypeAlias = "Callable[[Call], dict[str, JSON]]"


@dataclass(frozen=True, slots=True)
class Route:
    """An endpoint: the scope it needs and its handler. A POST's handler gets the JSON body."""

    scope: Scope
    handle: Handler


def _health(call: Call) -> dict[str, JSON]:
    status = "present" if call.bank.present else "absent"
    return {
        "bank": {"status": status, "questions": len(call.bank)},
        "has_key": call.client.has_key,
    }


def _questions(call: Call) -> dict[str, JSON]:
    with call.ledger.read() as reads:
        listed: list[JSON] = [
            {
                "name": q.name,
                "primitive": q.primitive,
                "model": q.model,
                "declared_stage": q.stage.value,
                "effective_stage": effective_stage(reads, q).value,
                "version": q.version_id,
            }
            for q in call.bank.values()
        ]
    return {"questions": listed}


def _ask(call: Call) -> dict[str, JSON]:
    body = _fields(call.body, required={"questions", "state"}, optional={"identifiers", "fresh"})
    names = body["questions"]
    if not isinstance(names, list) or not names or not all(isinstance(n, str) for n in names):
        msg = "questions: must be a non-empty list of question names"
        raise bad_request(msg)
    identifiers = body.get("identifiers", {})
    if not isinstance(identifiers, dict):
        msg = "identifiers: must be an object"
        raise bad_request(msg)
    fresh = body.get("fresh", False)
    if not isinstance(fresh, bool):
        msg = "fresh: must be true or false"
        raise bad_request(msg)
    results = ask(
        call.bank,
        call.ledger,
        call.client,
        cast("list[str]", names),
        body["state"],
        cast("dict[str, str | int]", identifiers),
        fresh=fresh,
    )
    return {"answers": {name: result.to_json() for name, result in results.items()}}


def _asks(call: Call) -> dict[str, JSON]:
    """Look up a question's asks by the identifiers they hold (``identifiers``, a JSON object)."""
    unknown = sorted(set(call.query) - {"question", "identifiers"})
    if unknown:
        msg = f"unknown query parameters: {', '.join(unknown)}"
        raise bad_request(msg)
    question = _parameter(call.query, "question")
    if question is None:
        msg = "question: required"
        raise bad_request(msg)
    try:
        identifiers = json.loads(_parameter(call.query, "identifiers") or "{}")
    except json.JSONDecodeError as error:
        msg = "identifiers: must be a JSON object"
        raise bad_request(msg) from error
    canonical_identifiers(identifiers)
    with call.ledger.read() as reads:
        records = reads.asks(question, identifiers)
    current = call.bank.get(question)
    return {"asks": [_ask_record(record, current) for record in records]}


def _ask_record(record: AskRecord, current: Question | None) -> JSON:
    answer = record.answer
    read: JSON = None
    # The current bands read only answers to the current content and model (R8).
    if answer is not None and current is not None and record.content_key == current.content_key:
        read = verdict(current, answer)
    return {
        "ask_id": record.id,
        "version": record.version,
        "state_hash": record.state_hash,
        "identifiers": cast("JSON", record.identifiers),
        "status": "cannot_judge" if answer is None else "answered",
        "replayed": record.replayed,
        "effective_stage": record.effective_stage,
        "verdict": read,
        "probabilities": None if answer is None else cast("JSON", probabilities(answer)),
        "reason": record.reason,
    }


def _decisions(call: Call) -> dict[str, JSON]:
    body = _fields(call.body, required={"ask_id", "decision"}, optional=set())
    return decide(call.ledger, *parse_decision(body)).to_json()


def _outcomes(call: Call) -> dict[str, JSON]:
    body = _fields(
        call.body,
        required={"value", "source", "strength"},
        optional={"ask_id", "question", "identifiers"},
    )
    return record_outcome(call.ledger, parse_outcome(body)).to_json()


def _rechecks(call: Call) -> dict[str, JSON]:
    body = _fields(call.body, required={"question"}, optional={"from_version"})
    name, earlier = body["question"], body.get("from_version")
    if not isinstance(name, str) or not isinstance(earlier, str | None):
        msg = "question and from_version: must be text"
        raise bad_request(msg)
    return {"recheck": recheck(call.bank, call.ledger, call.client, name, earlier).to_json()}


def _calibrations(call: Call) -> dict[str, JSON]:
    body = _fields(call.body, required={"question"}, optional={"split_key", "strong_only"})
    name, split_key, strong_only = parse_calibration(body)
    result = calibrate(call.bank, call.ledger, name, split_key, strong_only=strong_only)
    return {"calibration": result.to_json()}


def _stages(call: Call) -> dict[str, JSON]:
    return {"questions": stage_report(call.bank, call.ledger)}


def _fields(body: object, *, required: set[str], optional: set[str]) -> dict[str, object]:
    if not isinstance(body, dict):
        msg = "the body must be a JSON object"
        raise bad_request(msg)
    fields = cast("dict[str, object]", body)
    missing = sorted(required - set(fields))
    if missing:
        msg = f"required: {', '.join(missing)}"
        raise bad_request(msg)
    unknown = sorted(set(fields) - required - optional)
    if unknown:
        msg = f"unknown fields: {', '.join(unknown)}"
        raise bad_request(msg)
    return fields


def _parameter(query: dict[str, list[str]], name: str) -> str | None:
    values = query.get(name)
    return None if values is None else values[-1]


ROUTES: dict[tuple[str, str], Route] = {
    ("GET", "/v1/health"): Route(Scope.ASK, _health),
    ("GET", "/v1/questions"): Route(Scope.ASK, _questions),
    ("POST", "/v1/ask"): Route(Scope.ASK, _ask),
    ("GET", "/v1/asks"): Route(Scope.ASK, _asks),
    ("POST", "/v1/decisions"): Route(Scope.ASK, _decisions),
    ("POST", "/v1/outcomes"): Route(Scope.ADMIN, _outcomes),
    ("POST", "/v1/rechecks"): Route(Scope.ADMIN, _rechecks),
    ("POST", "/v1/calibrations"): Route(Scope.ADMIN, _calibrations),
    ("GET", "/v1/stages"): Route(Scope.ADMIN, _stages),
}
"""The endpoints by method and path, each with the scope it needs (KTD18)."""


@dataclass(frozen=True, slots=True)
class _Judge:
    """What every request of one instance shares."""

    instance: str
    tokens: dict[Scope, bytes]
    bank: BankFile
    ledger: Ledger
    client: TypeSafe


class _Handler(BaseHTTPRequestHandler):
    server: _Server
    timeout = SOCKET_TIMEOUT
    server_version = "typesafe-judge"
    sys_version = ""

    def do_GET(self) -> None:
        """Serve a GET."""
        self._dispatch()

    def do_POST(self) -> None:
        """Serve a POST."""
        self._dispatch()

    def _dispatch(self) -> None:
        path = urlsplit(self.path).path
        try:
            status, body = HTTPStatus.OK, self._serve(path)
        except RequestError as error:
            status, body = error.status, error.body
        except Exception as error:  # noqa: BLE001 - any failure answers 500, logged without the body
            # Only the type: a message or a traceback could quote the request.
            _log.error("%s %s failed: %s", self.command, path, type(error).__name__)
            status = HTTPStatus.INTERNAL_SERVER_ERROR
            body = RequestError(status, "internal", "internal error").body
        self._send(status, body)

    def _serve(self, path: str) -> dict[str, JSON]:
        judge = self.server.judge
        if not self._loopback_host():
            msg = "the Host header must name 127.0.0.1, localhost or [::1] with the service's port"
            raise bad_request(msg, "bad_host")
        scope = self._scope(judge)
        if scope is None:
            raise RequestError(HTTPStatus.UNAUTHORIZED, "unauthorized", "unauthorized")
        route = ROUTES.get((self.command, path))
        if route is None:
            msg = f"no endpoint {self.command} {path}"
            raise RequestError(HTTPStatus.NOT_FOUND, "not_found", msg)
        if route.scope is Scope.ADMIN and scope is not Scope.ADMIN:
            raise RequestError(HTTPStatus.FORBIDDEN, "forbidden", "forbidden")
        body = self._body() if self.command == "POST" else None
        bank, bank_error = judge.bank.current()
        judge.ledger.record_bank(bank)
        query = parse_qs(urlsplit(self.path).query, keep_blank_values=True)
        try:
            result = route.handle(Call(bank, judge.ledger, judge.client, body, query))
        except UnknownQuestionError as error:
            raise bad_request(str(error), "unknown_question") from error
        except StateError as error:
            raise bad_request(str(error), "invalid_state") from error
        except InvalidRequestError as error:
            raise bad_request(str(error), error.code) from error
        error_text = None if bank_error is None else str(bank_error)
        return {"instance": judge.instance, "bank_error": error_text, **result}

    def _loopback_host(self) -> bool:
        host = (self.headers.get("Host") or "").lower()
        port = self.server.server_port
        return host in {f"{name}:{port}" for name in LOOPBACK_HOSTS}

    def _scope(self, judge: _Judge) -> Scope | None:
        scheme, _, token = (self.headers.get("Authorization") or "").partition(" ")
        given = token.strip().encode()
        # Compared with both tokens every time, so the time taken tells nothing.
        matches = {scope: hmac.compare_digest(given, judge.tokens[scope]) for scope in Scope}
        if scheme.lower() != "bearer" or not given:
            return None
        if matches[Scope.ADMIN]:
            return Scope.ADMIN
        return Scope.ASK if matches[Scope.ASK] else None

    def _body(self) -> object:
        length = self.headers.get("Content-Length") or "0"
        if not re.fullmatch(r"[0-9]+", length):
            msg = "Content-Length: must be a number of bytes"
            raise bad_request(msg)
        if int(length) > MAX_BODY:
            msg = "the body is above 1 MiB"
            raise RequestError(HTTPStatus.REQUEST_ENTITY_TOO_LARGE, "too_large", msg)
        try:
            return json.loads(self.rfile.read(int(length)))
        except (UnicodeDecodeError, json.JSONDecodeError) as error:
            msg = "the body is not valid JSON"
            raise bad_request(msg, "malformed_json") from error

    def _send(self, status: HTTPStatus, body: dict[str, JSON]) -> None:
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        if status is HTTPStatus.UNAUTHORIZED:
            self.send_header("WWW-Authenticate", "Bearer")
        self.end_headers()
        self.wfile.write(data)

    def send_error(self, code: int, message: str | None = None, explain: str | None = None) -> None:
        """Answer http.server's own refusals, such as an unknown method, in the error shape."""
        del explain
        self.close_connection = True
        status = HTTPStatus(code)
        code_name = status.phrase.lower().replace(" ", "_")
        self._send(status, RequestError(status, code_name, message or status.phrase).body)

    def log_request(self, code: int | str = "-", size: int | str = "-") -> None:
        """Log the method, the path without its query, and the status."""
        del size
        path = urlsplit(getattr(self, "path", "")).path
        _log.info("%s %s %s", self.command or "-", path, int(code) if code != "-" else code)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A002 - http.server's name
        """Log http.server's own messages, such as a timed-out request."""
        _log.info(format, *args)


class _Server(ThreadingHTTPServer):
    """The HTTP server, counting the requests in flight so a stop can wait for them."""

    daemon_threads = True
    judge: _Judge

    def __init__(self) -> None:
        self._busy = 0
        self._idle = threading.Condition()
        super().__init__(("127.0.0.1", 0), _Handler)

    def server_bind(self) -> None:
        # HTTPServer's own looks the address up in DNS, which can stall a start.
        socketserver.TCPServer.server_bind(self)
        self.server_name = "127.0.0.1"
        self.server_port = cast("tuple[str, int]", self.server_address)[1]

    def process_request(
        self, request: socket.socket | tuple[bytes, socket.socket], client_address: object
    ) -> None:
        with self._idle:
            self._busy += 1
        super().process_request(request, client_address)

    def process_request_thread(
        self, request: socket.socket | tuple[bytes, socket.socket], client_address: object
    ) -> None:
        try:
            super().process_request_thread(request, client_address)
        finally:
            with self._idle:
                self._busy -= 1
                self._idle.notify_all()

    def wait_idle(self, timeout: float) -> bool:
        """Wait up to timeout for no request in flight; tell whether none is left."""
        with self._idle:
            return self._idle.wait_for(lambda: self._busy == 0, timeout)


class Service:
    """One judge instance for the repository at root."""

    def __init__(self, root: Path, client: TypeSafe) -> None:
        """Check root, load the bank and open the ledger.

        Raise ConfigError for a root without ``.crew/``, BankError for an invalid bank and
        LedgerError for a ledger the judge cannot use.
        """
        if not (root / ".crew").is_dir():
            msg = f"{root}: no .crew directory; pass the root of a repository crew runs in"
            raise ConfigError(msg)
        self._bank = BankFile(root / BANK_FILE)
        self._ledger = Ledger.open(root)
        self._ledger.record_bank(self._bank.current()[0])
        self._client = client
        self._instances = _Instances(root)
        self._server = _Server()
        self._thread = threading.Thread(
            target=self._server.serve_forever, args=(POLL_INTERVAL,), name="typesafe-judge"
        )
        self._instance = ""

    def start(self) -> dict[str, JSON]:
        """Write this instance's files, serve, and return the ready record ``service.json`` holds.

        Raise ConfigError when the instance files cannot be written.
        """
        url = f"http://127.0.0.1:{self._server.server_port}"
        record, tokens = self._instances.create(url)
        self._instance = cast("str", record["instance"])
        self._server.judge = _Judge(self._instance, tokens, self._bank, self._ledger, self._client)
        self._thread.start()
        return record

    def stop(self, grace: float = LONGEST_ASK) -> bool:
        """Stop accepting, wait up to grace for requests in flight, then remove this instance.

        Return whether no request was left in flight.
        """
        self._server.shutdown()
        idle = self._server.wait_idle(grace)
        self._server.server_close()
        self._thread.join()
        self._ledger.close()
        self._instances.remove(self._instance)
        return idle


class _Instances:
    """The run directory: one directory per instance, and ``service.json`` naming one."""

    def __init__(self, root: Path) -> None:
        self._run = root / RUN_DIRECTORY
        self._service_file = root / SERVICE_FILE

    def create(self, url: str) -> tuple[dict[str, JSON], dict[Scope, bytes]]:
        """Remove stale instances, write a new one and name it in ``service.json``."""
        instance = secrets.token_hex(8)
        directory = self._run / instance
        tokens = {scope: secrets.token_urlsafe(32) for scope in Scope}
        record: dict[str, JSON] = {
            "instance": instance,
            "pid": os.getpid(),
            "url": url,
            "ask_token_file": str(directory / "ask.token"),
            "admin_token_file": str(directory / "admin.token"),
        }
        try:
            self._run.mkdir(mode=0o700, exist_ok=True)
            with self._locked():
                self._remove_stale()
                # Filled under a dot name, then renamed, so no one sees it half written.
                staging = Path(tempfile.mkdtemp(prefix=".", dir=self._run))
                for scope, token in tokens.items():
                    _write(staging / f"{scope.value}.token", token)
                _write(staging / INSTANCE_FILE, json.dumps({**record, "started": time.time_ns()}))
                staging.rename(directory)
                _replace(self._service_file, json.dumps(record))
        except OSError as error:
            msg = f"cannot write the instance files in {self._run}: {error.strerror}"
            raise ConfigError(msg) from error
        return record, {scope: token.encode() for scope, token in tokens.items()}

    def remove(self, instance: str) -> None:
        """Remove an instance; if ``service.json`` names it, name the newest live one instead."""
        with self._locked():
            shutil.rmtree(self._run / instance, ignore_errors=True)
            named = _read_object(self._service_file)
            if named is None or named.get("instance") != instance:
                return
            newest = self._newest_live()
            if newest is None:
                self._service_file.unlink(missing_ok=True)
            else:
                _replace(self._service_file, json.dumps(newest))

    @contextmanager
    def _locked(self) -> Iterator[None]:
        """Hold the run directory's lock, so starts and stops update ``service.json`` in turn."""
        fd = os.open(self._run / LOCK_FILE, os.O_RDWR | os.O_CREAT | os.O_CLOEXEC, 0o600)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX)
            yield
        finally:
            os.close(fd)

    def _records(self) -> list[tuple[Path, dict[str, JSON] | None]]:
        return [
            (directory, _read_object(directory / INSTANCE_FILE))
            for directory in self._run.iterdir()
            if directory.is_dir() and not directory.name.startswith(".")
        ]

    def _remove_stale(self) -> None:
        for directory, record in self._records():
            if record is None or not _alive(record.get("pid")):
                shutil.rmtree(directory, ignore_errors=True)

    def _newest_live(self) -> dict[str, JSON] | None:
        live = [
            record
            for _, record in self._records()
            if record is not None and _alive(record.get("pid"))
        ]
        if not live:
            return None
        newest = max(live, key=lambda record: cast("int", record.get("started", 0)))
        return {field: newest.get(field) for field in SERVICE_FIELDS}


def _alive(pid: object) -> bool:
    if not isinstance(pid, int) or isinstance(pid, bool) or pid <= 0:
        return False
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


def _write(path: Path, text: str) -> None:
    """Create path at 0600 and write text to it."""
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as file:
        file.write(text)


def _replace(path: Path, text: str) -> None:
    """Replace path with text atomically: a 0600 temporary file beside it, then a rename."""
    fd, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as file:
            file.write(text)
        Path(temporary).replace(path)
    except OSError:
        Path(temporary).unlink(missing_ok=True)
        raise


def _read_object(path: Path) -> dict[str, JSON] | None:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except OSError, ValueError:
        return None
    return cast("dict[str, JSON]", value) if isinstance(value, dict) else None
