from __future__ import annotations

import tomllib
from pathlib import Path

import typesafe_judge

PYPROJECT = Path(__file__).parents[1] / "pyproject.toml"


def test_package_exposes_the_project_version() -> None:
    project = tomllib.loads(PYPROJECT.read_text(encoding="utf-8"))["project"]
    assert typesafe_judge.__version__ == project["version"]
