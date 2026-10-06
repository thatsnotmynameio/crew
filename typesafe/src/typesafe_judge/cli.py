"""``typesafe-judge serve --root <dir>``: run the judge for a repository until a stop signal.

stdout carries one line, the JSON ready record ``service.json`` also holds; logs and
failures go to stderr. The exit codes are crew's (KTD20): 0 a clean stop, 1 a runtime
failure or a forced stop, 2 a configuration or environment error, each failure with
one stderr line naming its cause.

SIGTERM, SIGINT and SIGHUP stop the service: it stops accepting, waits up to the
longest ask for requests in flight, then removes its instance files (KTD19). A second
signal exits at once with 1, leaving the instance directory for the next start to
remove.
"""

import argparse
import json
import logging
import os
import signal
import sys
import time
from pathlib import Path
from typing import TYPE_CHECKING

from typesafe_judge.bank import BankError
from typesafe_judge.client import TypeSafe
from typesafe_judge.ledger import LedgerError
from typesafe_judge.service import LONGEST_ASK, ConfigError, Service

if TYPE_CHECKING:
    from collections.abc import Sequence
    from types import FrameType

PROG = "typesafe-judge"
EXIT_CLEAN = 0
EXIT_FAILURE = 1
EXIT_CONFIG = 2
STOP_SIGNALS = (signal.SIGTERM, signal.SIGINT, signal.SIGHUP)
POLL_INTERVAL = 0.1
"""Seconds between checks for a stop signal."""

# Named, not __name__: run with -m, this module is __main__.
_log = logging.getLogger("typesafe_judge.cli")


class _Signals:
    """Counts stop signals; the second exits at once."""

    def __init__(self) -> None:
        self.received = 0

    def __call__(self, signum: int, frame: FrameType | None) -> None:
        del signum, frame
        self.received += 1
        if self.received > 1:
            os.write(2, f"{PROG}: forced to stop; requests in flight were abandoned\n".encode())
            os._exit(EXIT_FAILURE)


def main(argv: Sequence[str] | None = None) -> int:
    """Run the command line and return its exit code."""
    parser = argparse.ArgumentParser(
        prog=PROG, description="crew's local TypeSafe judge.", color=False
    )
    commands = parser.add_subparsers(dest="command", required=True)
    serve = commands.add_parser(
        "serve",
        help="serve the judge for a repository until a stop signal",
        description="Serve the judge on 127.0.0.1 until SIGTERM, SIGINT or SIGHUP.",
        color=False,
    )
    serve.add_argument(
        "--root", required=True, type=Path, help="the repository's main checkout, holding .crew/"
    )
    args = parser.parse_args(argv)
    return _serve(args.root.resolve())


def _serve(root: Path) -> int:
    handler = logging.StreamHandler(sys.stderr)
    handler.setFormatter(logging.Formatter(f"{PROG}: %(message)s"))
    logger = logging.getLogger("typesafe_judge")
    logger.addHandler(handler)
    logger.setLevel(logging.INFO)
    signals = _Signals()
    previous = {signum: signal.signal(signum, signals) for signum in STOP_SIGNALS}
    try:
        return _run(root, signals)
    except (ConfigError, BankError, LedgerError) as error:
        return _fail(EXIT_CONFIG, str(error))
    except Exception as error:  # noqa: BLE001 - any other failure exits 1 with one line
        return _fail(EXIT_FAILURE, f"{type(error).__name__}: {error}")
    finally:
        for signum, before in previous.items():
            signal.signal(signum, before)
        logger.removeHandler(handler)


def _run(root: Path, signals: _Signals) -> int:
    with TypeSafe() as client:
        service = Service(root, client)
        ready = service.start()
        sys.stdout.write(json.dumps(ready) + "\n")
        sys.stdout.flush()
        while not signals.received:
            time.sleep(POLL_INTERVAL)
        _log.info("stopping; a second signal stops at once")
        if not service.stop(LONGEST_ASK):
            msg = f"stopped with requests still in flight after {LONGEST_ASK:.0f} s"
            return _fail(EXIT_FAILURE, msg)
    return EXIT_CLEAN


def _fail(code: int, message: str) -> int:
    sys.stderr.write(f"{PROG}: {' '.join(message.split())}\n")
    return code


if __name__ == "__main__":
    sys.exit(main())
