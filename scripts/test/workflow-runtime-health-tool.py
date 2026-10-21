#!/usr/bin/env python3
"""Owned-container file fault agent, sent over docker exec stdin by the Online suite.

Usage inside an admitted product container: python - prepare|missing|restore|prepare-startup ENV_KEY
No host paths, credentials, remote sources or business conversions are accepted.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import sys
import tempfile


TOOLS = {
    "POINTCLOUD_PDAL_BIN": "/opt/conda/bin/pdal",
    "MODEL3D_IFC_CONVERTER_BIN": "/opt/addp/model3d-workflow/bin/IfcConvert",
    "DOCUMENT_LIBREOFFICE_BIN": "/usr/lib/libreoffice/program/soffice",
}
JOURNAL = Path("/tmp/addp-health-tool.json")


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def file_identity(path):
    require(not path.is_symlink(), "tool must be a regular file")
    info = path.stat()
    require(stat.S_ISREG(info.st_mode), "tool is not a regular file")
    with path.open("rb") as stream:
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
    return {"device": info.st_dev, "inode": info.st_ino, "mode": stat.S_IMODE(info.st_mode), "sha256": digest}


def materialize(path):
    before = file_identity(path)
    private = path.with_name(path.name + ".addp-health-private")
    require(not private.exists() and not private.is_symlink(), "private copy exists")
    try:
        with private.open("xb") as target, path.open("rb") as source:
            shutil.copyfileobj(source, target)
        shutil.copystat(path, private)
        private.replace(path)
    finally:
        private.unlink(missing_ok=True)
    result = file_identity(path)
    require(result["sha256"] == before["sha256"] and result["mode"] == before["mode"], "private copy changed tool")
    return result


class ToolFault:
    def __init__(self, path, journal):
        self.path, self.journal = path, journal
        self.backup = path.with_name(path.name + ".addp-health-recovery")

    def image_missing(self):
        require(not self.journal.exists() and not self.journal.is_symlink()
                and not self.backup.exists() and not self.backup.is_symlink(), "unfinished image fault")
        identity = file_identity(self.path)
        with self.journal.open("x") as stream:
            json.dump({"path": str(self.path), "image_identity": {key: identity[key] for key in ("sha256", "mode")}}, stream)
        self.path.rename(self.backup)
        return identity

    def prepare_startup(self):
        require(not self.journal.is_symlink(), "unsafe recovery journal")
        record = json.loads(self.journal.read_text())
        require(record["path"] == str(self.path), "recovery target mismatch")
        require(not self.path.exists() and not self.path.is_symlink(), "startup tool is not missing")
        if "image_identity" in record:
            before = file_identity(self.backup)
            require({key: before[key] for key in ("sha256", "mode")} == record["image_identity"], "image backup identity changed")
            # Image-layer inodes are not container inodes. Copy up before the
            # first baseline, while the product still sees the tool as absent.
            identity = materialize(self.backup)
            pending = None
            try:
                with tempfile.NamedTemporaryFile(mode="w", dir=self.journal.parent,
                                                 prefix=self.journal.name + ".", delete=False) as stream:
                    pending = Path(stream.name)
                    json.dump({"path": str(self.path), "identity": identity}, stream)
                    stream.flush()
                    os.fsync(stream.fileno())
                pending.replace(self.journal)
            finally:
                if pending is not None:
                    pending.unlink(missing_ok=True)
            return identity
        require(file_identity(self.backup) == record["identity"], "backup identity changed")
        return record["identity"]

    def missing(self):
        require(not self.backup.exists() and not self.backup.is_symlink(), "stale tool backup")
        identity = file_identity(self.path)
        with self.journal.open("x") as stream:
            json.dump({"path": str(self.path), "identity": identity}, stream)
            stream.flush()
            os.fsync(stream.fileno())
        self.path.rename(self.backup)
        return identity

    def restore(self):
        require(not self.journal.is_symlink(), "unsafe recovery journal")
        if not self.journal.exists():
            require(not self.backup.exists(), "backup without recovery journal")
            return {"result": "no_fault"}
        record = json.loads(self.journal.read_text())
        if "image_identity" in record:
            self.prepare_startup()
            record = json.loads(self.journal.read_text())
        require(record["path"] == str(self.path), "recovery target mismatch")
        if self.backup.exists():
            require(file_identity(self.backup) == record["identity"], "backup identity changed")
            require(not self.path.exists() and not self.path.is_symlink(), "tool replaced; refusing overwrite")
            self.backup.rename(self.path)
        require(file_identity(self.path) == record["identity"], "restored file identity mismatch")
        self.journal.unlink()
        return {"result": "restored", "identity": record["identity"]}


def main():
    action, variable = sys.argv[1:]
    require(variable in TOOLS, "unknown tool")
    expected = Path(TOOLS[variable])
    path = Path(os.environ[variable]).resolve()
    require(path == expected, "product tool path mismatch")
    fault = ToolFault(path, JOURNAL)
    if action == "prepare":
        require(not JOURNAL.exists() and not fault.backup.exists(), "unfinished fault")
        result = materialize(path)
    elif action == "image-missing":
        result = fault.image_missing()
    elif action == "prepare-startup":
        result = fault.prepare_startup()
    elif action == "missing":
        result = fault.missing()
    elif action == "restore":
        result = fault.restore()
    else:
        raise RuntimeError("unknown fault operation")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
