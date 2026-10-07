from types import SimpleNamespace

import pytest

import native_check


@pytest.mark.parametrize(('body', 'message'), [
    ('echo "Library not loaded: libpdal_e57" >&2; exit 1', 'Library not loaded'),
    ('echo "pdal 2.9.0"', 'PDAL must be 2.10.2'),
    ('if [ "$1" = --version ]; then echo "pdal 2.10.2"; else echo readers.las; fi', 'Missing PDAL drivers'),
])
def test_preflight_rejects_wrong_runtime_and_reports_loader_error(tmp_path, monkeypatch, body, message):
    executable = tmp_path / 'bin/pdal'
    executable.parent.mkdir()
    executable.write_text('#!/bin/sh\n' + body + '\n')
    executable.chmod(0o755)
    monkeypatch.setattr(native_check, 'sys', SimpleNamespace(version_info=(3, 12)))
    with pytest.raises(RuntimeError, match=message):
        native_check.check_native(tmp_path)
