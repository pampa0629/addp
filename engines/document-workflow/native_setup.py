"""Private official LibreOffice/font installation for trusted-file development."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import plistlib
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request
import zipfile
from pathlib import Path
from xml.etree import ElementTree as ET

ENGINE = Path(__file__).resolve().parent
MANIFEST = ENGINE / "native-assets.json"


def sha256(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def native_paths(root):
    manifest = json.loads(MANIFEST.read_text())
    key = platform.system() + "-" + platform.machine()
    if key not in manifest["packages"]:
        raise RuntimeError("Document native package is unavailable for " + key)
    identity = hashlib.sha256(MANIFEST.read_bytes() + key.encode()).hexdigest()
    prefix = Path(root).resolve() / identity[:16]
    if key.startswith("Darwin-"):
        office = prefix / "LibreOffice.app/Contents"
        executable = office / "MacOS/soffice"
        fonts = office / "Resources/fonts/truetype"
    else:
        office = prefix / "opt/libreoffice26.8"
        executable = office / "program/soffice"
        fonts = office / "share/fonts/truetype"
    return manifest, identity, prefix, executable, fonts


def font_config(prefix, bundled_fonts):
    tree = ET.Element("fontconfig")
    for directory in (bundled_fonts, prefix / "fonts"):
        ET.SubElement(tree, "dir").text = str(directory)
    ET.SubElement(tree, "cachedir").text = str(prefix / "font-cache")
    for family in ("Helvetica Neue", "Calibri", "Calibri Light", "sans-serif"):
        alias = ET.SubElement(tree, "alias")
        ET.SubElement(alias, "family").text = family
        prefer = ET.SubElement(alias, "prefer")
        for name in ("Noto Sans", "Noto Sans CJK SC"):
            ET.SubElement(prefer, "family").text = name
    return ET.tostring(tree, encoding="unicode")


def current(root):
    try:
        manifest, identity, prefix, executable, fonts = native_paths(root)
        state = json.loads((prefix / "ready.json").read_text())
        return (state["identity"] == identity
                and state["executable_sha256"] == sha256(executable)
                and sha256(prefix / "fonts/NotoSansCJKsc-Regular.otf") == manifest["font"]["sha256"]
                and (prefix / "fonts.conf").read_text() == font_config(prefix, fonts))
    except (OSError, ValueError, KeyError):
        return False


def download(asset, cache):
    cache.mkdir(parents=True, exist_ok=True)
    target = cache / Path(asset["url"]).name
    if target.exists() and sha256(target) == asset["sha256"]:
        return target
    # Never publish a partial or unverified download.
    handle, filename = tempfile.mkstemp(dir=cache, prefix="download-")
    os.close(handle)
    pending = Path(filename)
    try:
        with urllib.request.urlopen(asset["url"], timeout=120) as response, pending.open("wb") as stream:
            shutil.copyfileobj(response, stream)
        if sha256(pending) != asset["sha256"]:
            raise RuntimeError("SHA-256 mismatch: " + asset["url"])
        pending.replace(target)
        return target
    finally:
        pending.unlink(missing_ok=True)


def unpack(package, destination):
    if platform.system() == "Darwin":
        with tempfile.TemporaryDirectory(prefix="addp-libreoffice-mount-") as mount:
            result = subprocess.run(["hdiutil", "attach", "-readonly", "-nobrowse", "-plist",
                                     "-mountpoint", mount, str(package)], check=True, capture_output=True)
            devices = plistlib.loads(result.stdout)["system-entities"]
            mounted = next(item["mount-point"] for item in devices if "mount-point" in item)
            try:
                subprocess.run(["ditto", str(Path(mounted) / "LibreOffice.app"),
                                str(destination / "LibreOffice.app")], check=True)
            finally:
                subprocess.run(["hdiutil", "detach", mounted], check=True, capture_output=True)
    else:
        with tempfile.TemporaryDirectory(prefix="addp-libreoffice-debs-") as directory:
            with tarfile.open(package) as archive:
                archive.extractall(directory, filter="data")
            for deb in sorted(Path(directory).rglob("*.deb")):
                subprocess.run(["dpkg-deb", "--extract", str(deb), str(destination)], check=True)


def prepare(root):
    if current(root):
        return
    manifest, identity, prefix, executable, fonts = native_paths(root)
    cache = Path(root) / "downloads"
    package = download(manifest["packages"][platform.system() + "-" + platform.machine()], cache)
    font = download(manifest["font"], cache)
    Path(root).mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="install-", dir=root) as directory:
        stage = Path(directory) / "runtime"
        stage.mkdir()
        unpack(package, stage)
        (stage / "fonts").mkdir()
        shutil.copyfile(font, stage / "fonts/NotoSansCJKsc-Regular.otf")
        (stage / "font-cache").mkdir()
        (stage / "fonts.conf").write_text(font_config(prefix, fonts))
        state = {"identity": identity, "executable_sha256": sha256(stage / executable.relative_to(prefix))}
        (stage / "ready.json").write_text(json.dumps(state))
        # Called only while this Runtime is stopped and under the platform dependency lock.
        if prefix.exists():
            shutil.rmtree(prefix)
        stage.replace(prefix)
    if not current(root):
        raise RuntimeError("Document native installation failed validation")


def verify(root):
    if not current(root):
        raise RuntimeError("Document native dependencies changed; stop the Runtime before preparing again")
    manifest, _, prefix, executable, _ = native_paths(root)
    version = subprocess.run([str(executable), "--version"], check=True, capture_output=True,
                             text=True, timeout=20).stdout
    if not version.startswith("LibreOffice " + manifest["version"] + " "):
        raise RuntimeError("Unexpected LibreOffice version: " + version)
    from operators import invoke_operator
    from pypdf import PdfReader
    fixture = ENGINE.parents[1] / "business/fixtures/manager/addp_online_preview_fixture.pptx"
    before = sha256(fixture)
    title = "ADDP 文档原生转换实验：中文与 English"
    with tempfile.TemporaryDirectory(prefix="addp-document-check-") as directory:
        directory = Path(directory)
        source, target = directory / "source.pptx", directory / "converted.pdf"
        with zipfile.ZipFile(fixture) as original, zipfile.ZipFile(source, "w") as output:
            for member in original.infolist():
                data = original.read(member.filename)
                if member.filename == "ppt/slides/slide1.xml":
                    tree = ET.fromstring(data)
                    next(tree.iter("{http://schemas.openxmlformats.org/drawingml/2006/main}t")).text = title
                    data = ET.tostring(tree, encoding="utf-8", xml_declaration=True)
                output.writestr(member, data)
        plan = {"schema_version": "addp.workflow.access-plan/v1",
                "source": {"kind": "file", "format": "pptx", "access": {"method": "mounted_path", "path": str(source)}},
                "target": {"kind": "file", "format": "pdf", "name": target.name, "write_mode": "create",
                           "access": {"method": "mounted_path", "path": str(target)}}}
        work = directory / "work"
        invoke_operator("document_to_pdf", {"access_plan": plan},
                        env={"DOCUMENT_LIBREOFFICE_BIN": str(executable), "FONTCONFIG_FILE": str(prefix / "fonts.conf"),
                             "DOCUMENT_WORK_DIR": str(work)}, timeout_seconds=60)
        reader = PdfReader(target)
        if len(reader.pages) != 3 or title not in reader.pages[0].extract_text():
            raise RuntimeError("Document Chinese PPTX conversion check failed")
        if work.exists() and any(work.iterdir()):
            raise RuntimeError("Document conversion workspace was not cleaned")
    if before != sha256(fixture):
        raise RuntimeError("Document check changed the source fixture")
    print("✓ Document 原生 LibreOffice、私有中文字体与三页 PDF 转换已验证")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("prepare", "current", "verify", "environment"))
    parser.add_argument("root")
    args = parser.parse_args()
    try:
        if args.action == "current":
            sys.exit(0 if current(args.root) else 1)
        elif args.action == "prepare":
            prepare(args.root)
        elif args.action == "verify":
            verify(args.root)
        else:
            _, _, prefix, executable, _ = native_paths(args.root)
            print(executable)
            print(prefix / "fonts.conf")
    except (RuntimeError, OSError, subprocess.SubprocessError, ValueError) as exc:
        print("✗ " + str(exc), file=sys.stderr)
        sys.exit(1)
