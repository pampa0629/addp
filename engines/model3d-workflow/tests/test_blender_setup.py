import json
import os
from pathlib import Path
import zipfile

import pytest

import blender_setup


def test_importer_archive_is_pinned_and_cannot_extract_outside_package(tmp_path, monkeypatch):
    spec = json.loads(blender_setup.MANIFEST.read_text())["max_importer"]
    prefix = "io_scene_max-" + spec["commit"] + "/"
    archive = tmp_path / "source.zip"
    with zipfile.ZipFile(archive, "w") as output:
        output.writestr(prefix + "LICENSE", "upstream licence")
        output.writestr(prefix + "source/blender_manifest.toml", 'version = "1.9.2"')
        output.writestr(prefix + "source/import_max.py", "# source")
        output.writestr(prefix + "source/../../escaped", "unsafe")
    monkeypatch.setattr(blender_setup, "download", lambda asset, cache: archive)
    with pytest.raises(RuntimeError, match="unsafe path"):
        blender_setup.install_addon(tmp_path / "package", tmp_path / "cache")
    assert not (tmp_path / "escaped").exists()


def test_blender_cache_detects_changed_added_or_missing_tool_files(tmp_path, monkeypatch):
    prefix = tmp_path / "private"
    monkeypatch.setattr(blender_setup, "paths", lambda root: ("identity", prefix))
    for name in ("Blender.app/Contents/MacOS/Blender", "max-importer/source/import_max.py"):
        p = prefix / name
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_bytes(b"engine-owned")
        p.chmod(0o755)
    (prefix / "ready.json").write_text(json.dumps({"identity": "identity", "files": blender_setup.files(prefix)}))
    assert blender_setup.current(tmp_path)
    importer = prefix / "max-importer/source/import_max.py"
    importer.write_bytes(b"modified")
    assert not blender_setup.current(tmp_path)
    importer.write_bytes(b"engine-owned")
    extra = importer.with_name("foreign.py")
    extra.write_text("foreign")
    assert not blender_setup.current(tmp_path)
    extra.unlink()
    importer.unlink()
    assert not blender_setup.current(tmp_path)


def test_blender_damage_is_not_silently_reinstalled(tmp_path, monkeypatch):
    prefix = tmp_path / "private"
    prefix.mkdir()
    (prefix / "ready.json").write_text("{}")
    monkeypatch.setattr(blender_setup, "paths", lambda root: ("identity", prefix))
    monkeypatch.setattr(blender_setup, "download", lambda *args: pytest.fail("damaged cache overwritten"))
    with pytest.raises(RuntimeError, match="damaged"):
        blender_setup.prepare(tmp_path)


def test_linux_product_pins_same_importer_and_complete_blender_dependencies():
    root = Path(blender_setup.__file__).parent
    manifest = json.loads(blender_setup.MANIFEST.read_text())
    source = (root / "docker/runtime/Dockerfile").read_text()
    assert "blender=" + manifest["blender_debian"] in source
    assert "python3-numpy=" + manifest["numpy_debian"] in source
    assert "blender_setup.py install-addon" in source
    assert "MODEL3D_MAX_ADDON_PATH=" in source
    assert manifest["max_importer"]["commit"] in manifest["max_importer"]["url"]
    assert len(manifest["max_importer"]["sha256"]) == 64
