"""Runtime API client for one deployment-owned execution container."""
import json
import os
import re
import shutil
import stat
import time
from pathlib import Path

import httpx


def launcher_client():
    return httpx.Client(transport=httpx.HTTPTransport(uds="/run/notebook-launcher/control.sock"),
                        base_url="http://launcher", timeout=60)


class ExecutionContainer:
    def __init__(self, root, key):
        self.key = key
        self.workspace = Path(root) / key
        self.http = launcher_client()
        self.resource = "/jobs/" + key

    def prepare(self, name, contents, options):
        self.workspace.mkdir(mode=0o700)
        try:
            files = self.workspace / "files"
            files.mkdir(mode=0o700)
            (files / name).write_bytes(contents)
            (self.workspace / "job.json").write_text(json.dumps(options), encoding="utf-8")
        except Exception:
            shutil.rmtree(self.workspace)
            self.http.close()
            raise

    def start(self, mode, ttl):
        self.http.put(self.resource, json={"mode": mode, "ttl": ttl}).raise_for_status()

    def poll(self):
        response = self.http.get(self.resource)
        response.raise_for_status()
        state = response.json()
        return None if state["running"] else state["exit_code"]

    def wait(self, timeout):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            code = self.poll()
            if code is not None:
                if code:
                    raise RuntimeError("Notebook execution container failed")
                return
            time.sleep(0.5)
        raise TimeoutError("Notebook execution deadline exceeded")

    def read(self, name):
        # Only regular files directly inside this job may cross back to the API.
        # Pin the directory as well as the file; the executor can edit both.
        flags = os.O_RDONLY | os.O_NOFOLLOW
        directory = os.open(self.workspace / "files", flags | os.O_DIRECTORY)
        try:
            fd = os.open(name, flags | os.O_NONBLOCK, dir_fd=directory)
            with os.fdopen(fd, "rb") as stream:
                if not stat.S_ISREG(os.fstat(fd).st_mode):
                    raise ValueError("Notebook result must be a regular file")
                return stream.read()
        finally:
            os.close(directory)

    def stop(self):
        self.http.delete(self.resource).raise_for_status()

    def discard_files(self):
        shutil.rmtree(self.workspace)

    def close(self):
        try:
            self.stop()
            self.discard_files()
        finally:
            self.http.close()

def recover_previous_jobs(root):
    # One API instance owns this private deployment volume. Prior in-memory sessions
    # have expired; stop their workloads before removing the corresponding files.
    for directory in Path(root).iterdir():
        if re.fullmatch(r"[a-f0-9][a-f0-9-]{0,59}", directory.name) and directory.is_dir() and not directory.is_symlink():
            ExecutionContainer(root, directory.name).close()
