import hashlib
import io
import json
import plistlib
import subprocess
from pathlib import Path
from unittest.mock import patch

import pytest

import native_setup


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
