"""Install immutable Blender/MAX tools owned by the Model3D Runtime."""
from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile
import zipfile

from native_setup import download, digest, tool_environment

MANIFEST = Path(__file__).with_name("blender-assets.json")


def paths(root):
    if platform.system() + "-" + platform.machine() != "Darwin-arm64":
        raise RuntimeError("Model3D native Blender requires Darwin-arm64; Linux uses the product Runtime")
    identity = hashlib.sha256(MANIFEST.read_bytes() + Path(__file__).read_bytes()).hexdigest()
    return identity, Path(root).resolve() / identity[:16]


def files(prefix):
    return {str(p.relative_to(prefix)): digest(p) for folder in ("Blender.app", "max-importer")
            for p in sorted((prefix / folder).rglob("*")) if p.is_file()}


def current(root):
    try:
        identity, prefix = paths(root)
        state = json.loads((prefix / "ready.json").read_text())
        executable = prefix / "Blender.app/Contents/MacOS/Blender"
        return (state["identity"] == identity and state["files"] == files(prefix)
                and os.access(executable, os.X_OK)
                and (prefix / "max-importer/source/import_max.py").is_file())
    except (OSError, ValueError, KeyError):
        return False


def install_addon(destination, cache):
    spec = json.loads(MANIFEST.read_text())["max_importer"]
    archive = download(spec, cache)
    prefix = "io_scene_max-" + spec["commit"] + "/"
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(archive) as source:
        # Only package source and the upstream licence; reject path traversal.
        for member in source.namelist():
            if not member.startswith(prefix):
                continue
            relative = Path(member[len(prefix):])
            if relative.is_absolute() or ".." in relative.parts:
                raise RuntimeError("MAX importer archive contains an unsafe path")
            if member.endswith("/") or not (relative.parts[0] == "source" or relative == Path("LICENSE")):
                continue
            target = destination / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(source.read(member))
    manifest = (destination / "source/blender_manifest.toml").read_text()
    if 'version = "' + spec["version"] + '"' not in manifest:
        raise RuntimeError("MAX importer version mismatch")
    if not (destination / "LICENSE").is_file():
        raise RuntimeError("MAX importer licence is missing")


def verify(prefix):
    spec = json.loads(MANIFEST.read_text())["blender_macos"]
    executable = prefix / "Blender.app/Contents/MacOS/Blender"
    env = tool_environment(prefix)
    result = subprocess.run([str(executable), "--background", "--factory-startup", "--disable-autoexec",
                             "--python-exit-code", "7", "--python-expr",
                             "import sys; sys.dont_write_bytecode = True; sys.path.insert(0," + repr(str(prefix / "max-importer")) + "); import source; source.register(); import bpy; assert hasattr(bpy.ops.import_scene, 'max')"],
                            env=env, capture_output=True, text=True, timeout=60, check=True)
    if "Blender " + spec["version"] not in result.stdout:
        raise RuntimeError("Blender version mismatch")


def prepare(root):
    root = Path(root).resolve()
    root.mkdir(parents=True, exist_ok=True)
    with (root / "install.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        identity, prefix = paths(root)
        if current(root):
            verify(prefix)
            return
        if (prefix / "ready.json").exists():
            raise RuntimeError("Model3D Blender cache is damaged; inspect " + str(prefix))
        prefix.mkdir(parents=True, exist_ok=True)
        spec = json.loads(MANIFEST.read_text())["blender_macos"]
        archive = download(spec, root / "cache")
        with tempfile.TemporaryDirectory(prefix="addp-model3d-blender-mount-") as temporary:
            mount = Path(temporary) / "volume"
            subprocess.run(["hdiutil", "attach", "-readonly", "-nobrowse", "-mountpoint", str(mount), str(archive)], check=True)
            try:
                target = prefix / "Blender.app"
                if target.exists():
                    shutil.rmtree(target)
                shutil.copytree(mount / "Blender.app", target, symlinks=True)
            finally:
                subprocess.run(["hdiutil", "detach", str(mount)], check=True)
        install_addon(prefix / "max-importer", root / "cache")
        verify(prefix)
        if paths(root)[0] != identity:
            raise RuntimeError("Blender setup inputs changed during installation")
        (prefix / "ready.json").write_text(json.dumps({"identity": identity, "files": files(prefix)}))
        if not current(root):
            raise RuntimeError("Model3D Blender integrity check failed")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("prepare", "current", "environment", "install-addon"))
    parser.add_argument("root", type=Path)
    args = parser.parse_args()
    if args.command == "install-addon":
        with tempfile.TemporaryDirectory(prefix="addp-max-addon-download-") as temporary:
            install_addon(args.root, Path(temporary))
    elif args.command == "prepare":
        prepare(args.root)
    elif args.command == "current":
        raise SystemExit(0 if current(args.root) else 1)
    else:
        print(paths(args.root)[1])


if __name__ == "__main__":
    main()
