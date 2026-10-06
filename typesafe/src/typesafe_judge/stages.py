"""The stage report: what each question of the bank has earned (R19, KTD11, KTD17).

``GET /v1/stages``, admin scope::

    response: {"questions": [{name, version, declared_stage, effective_stage,
               declared_on_first_sight, latest_recheck: {from_version, stage, result} | null,
               latest_calibration: {stage, result} | null, acted_in_shadow, next_stage},
               ...]}

- ``declared_on_first_sight``: the version is the first of its name the ledger recorded,
  its stage not raised since, so it keeps its declared stage without evidence. A fresh
  clone or a renamed question starts there.
- ``latest_recheck`` and ``latest_calibration``: the latest recorded to, or of, the
  current version.
- ``acted_in_shadow``: the ``acted`` decisions on the asks, of any version, answered at
  shadow.
- ``next_stage``: the stage above the effective one when the latest calibration of the
  current version passed its bar, else null. A recheck earns no stage above the earlier
  version's, so it never moves this. Raising the stage is a person's edit to the bank,
  and the raised stage needs a passing recheck or calibration recorded for it (KTD11).

The report never writes the bank.
"""

from typing import TYPE_CHECKING

from typesafe_judge.asking import effective_stage
from typesafe_judge.bank import Stage

if TYPE_CHECKING:
    from typesafe_judge.bank import Bank, Question
    from typesafe_judge.evidence import Evidence
    from typesafe_judge.keys import JSON
    from typesafe_judge.ledger import Ledger, Reads


def stage_report(bank: Bank, ledger: Ledger) -> list[JSON]:
    """Return, for each question of the bank, its stages and the evidence behind them."""
    ledger.record_bank(bank)
    with ledger.read() as reads:
        return [_entry(reads, question) for question in bank.values()]


def _entry(reads: Reads, question: Question) -> JSON:
    name, version = question.name, question.version_id
    effective = effective_stage(reads, question)
    rechecks = reads.rechecks(name, version)
    calibrations = [c for c in reads.calibrations(name) if c.version == version]
    recheck = rechecks[-1] if rechecks else None
    calibration = calibrations[-1] if calibrations else None
    return {
        "name": name,
        "version": version,
        "declared_stage": question.stage.value,
        "effective_stage": effective.value,
        "declared_on_first_sight": _first_sight(reads, question),
        "latest_recheck": None if recheck is None else _evidence(recheck),
        "latest_calibration": None if calibration is None else _evidence(calibration),
        "acted_in_shadow": reads.acted_in_shadow(name),
        "next_stage": _next_stage(effective, calibration),
    }


def _first_sight(reads: Reads, question: Question) -> bool:
    versions = reads.versions(question.name)
    first = Stage(reads.observed_stages(question.name, question.version_id)[0])
    return versions[0] == question.version_id and question.stage <= first


def _evidence(evidence: Evidence) -> JSON:
    found: dict[str, JSON] = {"stage": evidence.stage, "result": evidence.result}
    if evidence.from_version is not None:
        found["from_version"] = evidence.from_version
    return found


def _next_stage(effective: Stage, calibration: Evidence | None) -> str | None:
    stages = list(Stage)
    if calibration is None or calibration.result != "passed" or effective is Stage.ACT:
        return None
    return stages[stages.index(effective) + 1].value
