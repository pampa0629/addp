import hashlib
import json
import os
import zipfile
from pathlib import Path

import pytest

import native_setup


def test_verified_download_cache_and_failed_download_cleanup(tmp_path, monkeypatch):
    source = tmp_path / "source.zip"
    source.write_bytes(b"verified release")
    asset = {"url": source.as_uri(), "sha256": hashlib.sha256(source.read_bytes()).hexdigest()}
    cache = tmp_path / "cache"
    downloaded = native_setup.download(asset, cache)
    assert downloaded.read_bytes() == source.read_bytes()
    monkeypatch.setattr(native_setup.urllib.request, "urlopen", lambda *a, **k: pytest.fail("cache redownloaded"))
    assert native_setup.download(asset, cache) == downloaded
    monkeypatch.undo()
    with pytest.raises(RuntimeError, match="checksum mismatch"):
        native_setup.download({**asset, "sha256": "0" * 64}, cache)
    assert list(cache.iterdir()) == [downloaded]


def test_integrity_rejects_changed_missing_or_added_runtime_library(tmp_path, monkeypatch):
    prefix = tmp_path / "private"
    monkeypatch.setattr(native_setup, "native_paths", lambda root: ({}, "identity", prefix))
    for name in ("bin/_3dtile", "bin/assimp", "bin/IfcConvert", "bin/osgb-smoke", "bin/proj/proj.db", "deps/lib/libassimp.dylib"):
        path = prefix / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b"verified")
        path.chmod(0o755)
    state = {"identity": "identity", "files": {str(p.relative_to(prefix)): native_setup.digest(p) for p in native_setup.bundle_files(prefix)}}
    (prefix / "ready.json").write_text(json.dumps(state))
    assert native_setup.current(tmp_path)
    library = prefix / "deps/lib/libassimp.dylib"
    library.write_bytes(b"upgraded")
    assert not native_setup.current(tmp_path)
    library.write_bytes(b"verified")
    library.unlink()
    assert not native_setup.current(tmp_path)
    library.write_bytes(b"verified")
    (prefix / "deps/lib/foreign.dylib").write_bytes(b"foreign")
    assert not native_setup.current(tmp_path)


def test_ifc_install_extracts_only_declared_executable(tmp_path, monkeypatch):
    archive = tmp_path / "release.zip"
    with zipfile.ZipFile(archive, "w") as output:
        output.writestr("IfcConvert", b"native executable")
        output.writestr("../escaped", b"unexpected")
    monkeypatch.setattr(native_setup, "download", lambda *a: archive)
    native_setup.install_ifc("linux64", tmp_path / "tools", tmp_path / "cache")
    assert (tmp_path / "tools/IfcConvert").read_bytes() == b"native executable"
    assert os.access(tmp_path / "tools/IfcConvert", os.X_OK)
    assert not (tmp_path / "escaped").exists()


def test_native_environment_drops_global_resources_and_linker_flags(tmp_path, monkeypatch):
    for name in ("GDAL_DATA", "PROJ_DATA", "PROJ_LIB", "DYLD_LIBRARY_PATH", "PYTHONPATH", "CMAKE_PREFIX_PATH", "LDFLAGS"):
        monkeypatch.setenv(name, "/foreign")
    env = native_setup.tool_environment(tmp_path)
    assert env["GDAL_DATA"] == str(tmp_path / "bin/gdal")
    assert env["PROJ_DATA"] == str(tmp_path / "bin/proj")
    for name in ("PROJ_LIB", "DYLD_LIBRARY_PATH", "PYTHONPATH", "CMAKE_PREFIX_PATH", "LDFLAGS"):
        assert name not in env


def test_unsupported_platform_rejects_without_docker_fallback(tmp_path, monkeypatch):
    monkeypatch.setattr(native_setup.platform, "system", lambda: "Linux")
    monkeypatch.setattr(native_setup.platform, "machine", lambda: "x86_64")
    with pytest.raises(RuntimeError, match="not verified"):
        native_setup.native_paths(tmp_path)
    assert not list(tmp_path.iterdir())


def test_product_and_native_share_ifc_assets_and_cargo_lock():
    manifest = json.loads(native_setup.MANIFEST.read_text())
    assert manifest["ifc_version"] == "0.8.4-e8eb5e4"
    assert set(manifest["ifc_packages"]) == {"macosm164", "linux64", "linuxarm64"}
    assert all(len(asset["sha256"]) == 64 for asset in manifest["ifc_packages"].values())
    dockerfile = (native_setup.CONVERTER / "Dockerfile").read_text()
    assert "native_setup.py install-ifc /out" in dockerfile
    assert "COPY engines/model3d-workflow/docker/converter/Cargo.lock" in dockerfile
    assert "cargo build --locked --release" in dockerfile
    assert "rust:" + manifest["rust"] + "-bookworm" in dockerfile
    assert "THREE_DTILES_REF=" + manifest["3dtiles"]["commit"] in dockerfile
    assert "VCPKG_COMMIT=" + manifest["vcpkg"]["commit"] in dockerfile


def test_corrupt_ready_cache_cannot_be_rebuilt_while_bound(tmp_path, monkeypatch):
    prefix = tmp_path / "private"
    prefix.mkdir()
    (prefix / "ready.json").write_text("{}")
    monkeypatch.setattr(native_setup, "native_paths", lambda root: ({}, "identity", prefix))
    monkeypatch.setattr(native_setup, "build", lambda *a: pytest.fail("damaged cache was overwritten"))
    with pytest.raises(RuntimeError, match="cache is damaged"):
        native_setup.prepare(tmp_path)
