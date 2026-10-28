"""Installation identity and failure atomicity of the single SDK repair."""
import csv
import io
from pathlib import Path

import pytest
import skp_dependency as dependency


@pytest.fixture
def installation(tmp_path, monkeypatch):
    actual, record = dependency._files()
    files = {}
    for name, source in actual.items():
        path = tmp_path / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(source.read_bytes())
        files[name] = path
    copied_record = tmp_path / 'RECORD'
    copied_record.write_bytes(record.read_bytes())
    monkeypatch.setattr(dependency, '_files', lambda: (files, copied_record))
    return files, copied_record


def test_pinned_repair_is_recorded_and_idempotent(installation):
    files, record = installation
    before = {path: path.read_bytes() for path in [*files.values(), record]}
    dependency.verify()
    dependency.prepare()
    assert {path: path.read_bytes() for path in before} == before


def test_unknown_source_rejected_before_any_installation_write(installation):
    files, record = installation
    files['scene.py'].write_bytes(files['scene.py'].read_bytes() + b'\n# unknown change\n')
    before = {path: path.read_bytes() for path in [*files.values(), record]}
    with pytest.raises(ValueError, match='Unrecognized OpenSKP'):
        dependency.prepare()
    assert {path: path.read_bytes() for path in before} == before
    with pytest.raises(ValueError, match='repair missing or changed'):
        dependency.verify()


def test_record_drift_is_detected_and_prepare_repairs_it(installation):
    _, record = installation
    rows = list(csv.reader(io.StringIO(record.read_text())))
    for row in rows:
        if row[0] == 'openskp/_core.py':
            row[1] = 'sha256=wrong'
    output = io.StringIO()
    csv.writer(output).writerows(rows)
    record.write_text(output.getvalue())
    with pytest.raises(ValueError, match='RECORD mismatch'):
        dependency.verify()
    dependency.prepare()
    dependency.verify()


def test_failed_preparation_rolls_back_installation(installation, monkeypatch):
    files, record = installation
    record.write_bytes(record.read_bytes() + b'\n')
    before = {path: path.read_bytes() for path in [*files.values(), record]}
    def fail_verification():
        raise ValueError('verification interrupted')
    monkeypatch.setattr(dependency, 'verify', fail_verification)
    with pytest.raises(ValueError, match='verification interrupted'):
        dependency.prepare()
    assert {path: path.read_bytes() for path in before} == before
