from importlib.metadata import version
from pathlib import Path

from addp_common import __version__
from addp_common.cli import PACKAGE_VERSION


def test_package_version_has_one_runtime_source():
    assert version("addp-common") == __version__ == PACKAGE_VERSION


def test_documented_versions_match_runtime_source():
    repository_root = Path(__file__).resolve().parents[2]
    assert f"version-{__version__}-blue.svg" in (repository_root / "README.md").read_text()
    assert f"当前版本为 `{__version__}`" in (repository_root / "common-python" / "README.md").read_text()
    assert f"当前版本为 `{__version__}`" in (
        repository_root / "common-python" / "common-python实施报告.md"
    ).read_text()
    assert f"**Version**: {__version__}" in (repository_root / "scripts" / "README.md").read_text()
