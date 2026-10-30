import hashlib
import io
import json
import plistlib
import subprocess
from pathlib import Path
from unittest.mock import patch

import pytest

import native_setup


def test_startup_check_only_probes_native_version(tmp_path, monkeypatch):
    monkeypatch.setattr(native_setup, "current", lambda root: True)
    monkeypatch.setattr(native_setup, "native_paths", lambda root: (
        {"version": "26.8.0.3"}, "identity", tmp_path, tmp_path / "soffice", tmp_path / "fonts"))
    with patch.object(native_setup.subprocess, "run", return_value=subprocess.CompletedProcess(
        [], 0, stdout="LibreOffice 26.8.0.3 build\n")) as run:
        native_setup.check(tmp_path)
    run.assert_called_once_with([str(tmp_path / "soffice"), "--version"], check=True,
                                capture_output=True, text=True, timeout=20)


def test_startup_check_rejects_dependency_drift_before_version_probe(tmp_path, monkeypatch):
    monkeypatch.setattr(native_setup, "current", lambda root: False)
    with patch.object(native_setup.subprocess, "run") as run:
        with pytest.raises(RuntimeError, match="dependencies changed"):
            native_setup.check(tmp_path)
    run.assert_not_called()


def test_startup_check_rejects_wrong_office_version(tmp_path, monkeypatch):
    monkeypatch.setattr(native_setup, "current", lambda root: True)
    monkeypatch.setattr(native_setup, "native_paths", lambda root: (
        {"version": "26.8.0.3"}, "identity", tmp_path, tmp_path / "soffice", tmp_path / "fonts"))
    with patch.object(native_setup.subprocess, "run", return_value=subprocess.CompletedProcess(
        [], 0, stdout="LibreOffice wrong-version\n")):
        with pytest.raises(RuntimeError, match="Unexpected LibreOffice version"):
            native_setup.check(tmp_path)


def test_private_package_identity_font_and_configuration_changes(tmp_path, monkeypatch):
    executable_bytes = b"private executable"
    font_bytes = b"private font"
    manifest = tmp_path / "assets.json"
    manifest.write_text(json.dumps({"packages": {"Darwin-arm64": {}},
                                    "font": {"sha256": hashlib.sha256(font_bytes).hexdigest()}}))
    monkeypatch.setattr(native_setup, "MANIFEST", manifest)
    monkeypatch.setattr(native_setup.platform, "system", lambda: "Darwin")
    monkeypatch.setattr(native_setup.platform, "machine", lambda: "arm64")
    _, identity, prefix, executable, fonts = native_setup.native_paths(tmp_path)
    executable.parent.mkdir(parents=True)
    executable.write_bytes(executable_bytes)
    (prefix / "fonts").mkdir()
    font = prefix / "fonts/NotoSansCJKsc-Regular.otf"
    font.write_bytes(font_bytes)
    config = prefix / "fonts.conf"
    config.write_text(native_setup.font_config(prefix, fonts))
    (prefix / "ready.json").write_text(json.dumps({"identity": identity,
                                                   "executable_sha256": native_setup.sha256(executable)}))
    assert native_setup.current(tmp_path)
    font.write_bytes(b"wrong font")
    assert not native_setup.current(tmp_path)
    font.write_bytes(font_bytes)
    config.write_text("system font configuration")
    assert not native_setup.current(tmp_path)
    config.write_text(native_setup.font_config(prefix, fonts))
    executable.write_bytes(b"wrong binary")
    assert not native_setup.current(tmp_path)


def test_download_requires_hash_and_cleans_partial_file(tmp_path):
    payload = b"verified official package"
    asset = {"url": "https://example.test/package", "sha256": hashlib.sha256(payload).hexdigest()}
    with patch.object(native_setup.urllib.request, "urlopen", return_value=io.BytesIO(b"bad download")):
        with pytest.raises(RuntimeError, match="SHA-256 mismatch"):
            native_setup.download(asset, tmp_path)
    assert not list(tmp_path.iterdir())
    with patch.object(native_setup.urllib.request, "urlopen", return_value=io.BytesIO(payload)):
        package = native_setup.download(asset, tmp_path)
    with patch.object(native_setup.urllib.request, "urlopen", side_effect=AssertionError("no redownload")):
        assert native_setup.download(asset, tmp_path) == package


def test_mac_copy_failure_still_detaches_mount(tmp_path, monkeypatch):
    monkeypatch.setattr(native_setup.platform, "system", lambda: "Darwin")
    calls = []

    def run(command, **kwargs):
        calls.append(command)
        if command[0] == "ditto":
            raise subprocess.CalledProcessError(1, command)
        return subprocess.CompletedProcess(command, 0, stdout=plistlib.dumps({"system-entities": [{"mount-point": "/private/research"}]}))

    monkeypatch.setattr(native_setup.subprocess, "run", run)
    with pytest.raises(subprocess.CalledProcessError):
        native_setup.unpack(tmp_path / "package.dmg", tmp_path / "destination")
    assert calls[-1] == ["hdiutil", "detach", "/private/research"]


def test_unknown_platform_fails_without_download(tmp_path, monkeypatch):
    monkeypatch.setattr(native_setup.platform, "machine", lambda: "unknown-architecture")
    with pytest.raises(RuntimeError, match="unavailable"):
        native_setup.prepare(tmp_path)
    assert not list(tmp_path.iterdir())


def prepare_fixture(tmp_path, monkeypatch):
    prefix = tmp_path / "private-install"
    manifest = {"packages": {"Linux-x86_64": {"url": "https://example.test/package"}},
                "font": {"url": "https://example.test/font"}}
    monkeypatch.setattr(native_setup, "current", lambda root: False)
    monkeypatch.setattr(native_setup.platform, "system", lambda: "Linux")
    monkeypatch.setattr(native_setup.platform, "machine", lambda: "x86_64")
    monkeypatch.setattr(native_setup, "native_paths", lambda root: (
        manifest, "identity", prefix, prefix / "program/soffice", prefix / "fonts"))
    return prefix


def test_prepare_trace_is_flushed_before_blocking_download_and_preserves_first_error(tmp_path, monkeypatch):
    prefix = prepare_fixture(tmp_path, monkeypatch)

    class FlushedOutput(io.StringIO):
        visible = ""

        def flush(self):
            self.visible = self.getvalue()

    output = FlushedOutput()
    monkeypatch.setattr(native_setup.sys, "stdout", output)
    failure = ConnectionResetError("private transport detail")

    def download(asset, cache):
        events = [json.loads(line.split("=", 1)[1]) for line in output.visible.splitlines()]
        assert events[-1]["phase"] == "package_download"
        assert events[-1]["result"] == "started"
        raise failure

    monkeypatch.setattr(native_setup, "download", download)
    with pytest.raises(ConnectionResetError) as caught:
        native_setup.prepare(tmp_path)
    assert caught.value is failure
    events = [json.loads(line.split("=", 1)[1]) for line in output.visible.splitlines()]
    assert events[-1]["phase"] == "package_download"
    assert events[-1]["result"] == "failed"
    assert all(event["elapsed_ms"] >= 0 for event in events)
    assert "private transport detail" not in output.visible
    assert str(tmp_path) not in output.visible
    assert not prefix.exists()


def test_prepare_trace_distinguishes_unpack_failure_and_removes_staging(tmp_path, monkeypatch, capsys):
    prefix = prepare_fixture(tmp_path, monkeypatch)
    monkeypatch.setattr(native_setup, "download", lambda asset, cache: tmp_path / "verified-asset")
    failure = OSError("unpack failed")

    def unpack(package, destination):
        raise failure

    monkeypatch.setattr(native_setup, "unpack", unpack)
    with pytest.raises(OSError) as caught:
        native_setup.prepare(tmp_path)
    assert caught.value is failure
    events = [json.loads(line.split("=", 1)[1]) for line in capsys.readouterr().out.splitlines()]
    assert [(e["phase"], e["result"]) for e in events if e["result"] != "started"] == [
        ("current_check", "passed"), ("package_download", "passed"),
        ("font_download", "passed"), ("package_unpack", "failed")]
    assert not prefix.exists()
    assert not list(tmp_path.glob("install-*"))
