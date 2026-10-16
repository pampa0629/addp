#!/usr/bin/env python3
"""Owner-invoked SGM T2. Only Docker and the Python standard library on the host."""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import uuid


ROOT = Path(__file__).resolve().parents[2]
LABEL = "com.addp.sgm-t2"
BASE = "addp-supermap-workflow-base:local"


class GateError(RuntimeError):
    pass


def require(condition, message):
    if not condition:
        raise GateError(message)


def command(args, *, data=None, timeout=180, environment=None):
    """Never expose arguments/output: either can contain credentials or SDK data."""
    try:
        process = subprocess.Popen(args, stdin=subprocess.PIPE if data is not None else subprocess.DEVNULL,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, cwd=ROOT,
                                   env=environment, start_new_session=True)
        try:
            stdout, stderr = process.communicate(data, timeout=timeout)
        except BaseException:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.communicate(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.communicate()
            raise
    except (subprocess.SubprocessError, OSError) as error:
        raise GateError(f"{args[0]} invocation failed ({type(error).__name__})") from None
    if process.returncode:
        check = re.search(rb"SGM_PROBE_CHECK:([a-zA-Z0-9_ /.-]{1,160})", stderr)
        suffix = f"; probe check: {check[1].decode()}" if check else ""
        raise GateError(f"{args[0]} failed (exit {process.returncode}); private output discarded{suffix}")
    return stdout.decode()


def source_digest():
    names = command(["git", "ls-files", "-co", "--exclude-standard", "-z", "--",
                     "engines/supermap-workflow", "engines/model3d-workflow", "common-python", "scripts/infra/Dockerfile.minio"])
    digest = hashlib.sha256()
    for name in sorted(set(names.rstrip("\0").split("\0"))):
        path = ROOT / name
        if path.is_file() and path.suffix != ".md":
            digest.update(name.encode() + b"\0" + hashlib.sha256(path.read_bytes()).digest())
    return digest.hexdigest()


def image_identity(reference):
    info = json.loads(command(["docker", "image", "inspect", reference]))[0]
    require((info["Os"], info["Architecture"]) == ("linux", "arm64"), "SGM T2 requires Linux ARM64 images")
    require(re.fullmatch(r"sha256:[0-9a-f]{64}", info["Id"]), "invalid immutable image identity")
    return info


def report_path(value):
    path = Path(value)
    require(path.is_absolute(), "SGM_T2_REPORT must be absolute")
    path = path.resolve()
    require(path != ROOT and ROOT not in path.parents, "report must be outside the repository")
    require(not path.exists(), "refusing to overwrite an existing report")
    require(path.parent.is_dir(), "report parent must already exist")
    return path


class Lifecycle:
    def __init__(self):
        self.run_id = "addp-sgm-t2-" + uuid.uuid4().hex
        self.work = Path(tempfile.mkdtemp(prefix=self.run_id + "-"))
        if self.work.resolve() == ROOT or ROOT in self.work.resolve().parents:
            self.work.rmdir()
            raise GateError("private test directory must be outside the repository")
        self.images = []
        self.network = None
        self.volume = None
        self.trace = None
        self.trace_file = None

    def docker(self, *args, **kwargs):
        return command(["docker", *args], **kwargs)

    def create(self, image, name, *args, cmd=()):
        full_name = self.run_id + "-" + name
        return self.docker("create", "--name", full_name, "--label", f"{LABEL}={self.run_id}",
                           *args, image, *cmd).strip()

    def copy(self, source, target):
        self.docker("cp", str(source), str(target))

    def private_json(self, name, value):
        path = self.work / name
        with open(path, "x", opener=lambda p, f: os.open(p, f, 0o600)) as stream:
            json.dump(value, stream)
        return path

    def owned(self, kind):
        flags = ("--all",) if kind == "container" else ()
        return self.docker(kind, "ls", *flags, "-q", "--filter", f"label={LABEL}={self.run_id}").split()

    def cleanup(self):
        failures = []
        if self.trace is not None:
            self.trace.terminate()
            try:
                self.trace.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.trace.kill()
                self.trace.wait()
        if self.trace_file is not None:
            self.trace_file.close()
        # Use labels to recover resources even after create returned an unknown outcome.
        for kind, operation in (("container", ["rm", "--force", "--volumes"]),
                                ("volume", ["rm"]), ("network", ["rm"])):
            try:
                identifiers = self.owned(kind)
                if identifiers:
                    self.docker(kind, *operation, *identifiers)
                require(not self.owned(kind), f"{kind} residue")
            except GateError:
                failures.append(kind)
        # Tags are unique to this run. Removing tags never forces removal of shared layers.
        for tag in reversed(self.images):
            try:
                found = self.docker("image", "ls", "-q", tag).strip()
                if found:
                    self.docker("image", "rm", tag)
                require(not self.docker("image", "ls", "-q", tag).strip(), "image tag residue")
            except GateError:
                failures.append("image")
        try:
            shutil.rmtree(self.work)
            require(not self.work.exists(), "private directory residue")
        except (OSError, GateError):
            failures.append("private_directory")
        return failures


def build_images(life):
    require(platform.system() in {"Darwin", "Linux"} and platform.machine() in {"arm64", "aarch64"},
            "SGM local T2 requires a macOS/Linux ARM64 host")
    endpoint = os.environ.get("DOCKER_HOST") or json.loads(command(["docker", "context", "inspect"]))[0]["Endpoints"]["docker"]["Host"]
    require(endpoint.startswith("unix://"), "SGM local T2 refuses remote Docker endpoints")
    require(command(["docker", "info", "--format", "{{.Architecture}}"]).strip() in {"aarch64", "arm64"},
            "Docker daemon must be ARM64; no emulation fallback")
    base = image_identity(BASE)
    require(base.get("Config", {}).get("Labels", {}).get("addp.supermap.base") == "true",
            "private SuperMap SDK base label is missing")
    tag = life.run_id
    references = [f"localhost:5001/addp-{service}:{tag}" for service in
                  ("supermap-workflow-engine", "model3d-workflow-engine", "model3d-converter")]
    references.append(f"{life.run_id}-minio:local")
    for reference in references:
        require(not life.docker("image", "ls", "-q", reference).strip(), "unique image tag already exists")
    life.images.extend(references)
    print(f"SGM T2 {life.run_id}: build current-source product images (private, no push)", flush=True)
    inputs = source_digest()
    environment = dict(os.environ, SUPERMAP_WORKFLOW_BASE_IMAGE=BASE)
    environment.pop("THREE_DTILES_REF", None)
    command(["make", "build-images", "IMAGE_BUILD_ARGS=--services supermap-workflow-engine,model3d-workflow-engine "
             f"--verify --jobs 1 --registry localhost:5001 --tag {tag}"], timeout=3600, environment=environment)
    life.docker("build", "-f", str(ROOT / "scripts/infra/Dockerfile.minio"), "-t", references[3],
                str(ROOT / "scripts/infra"), timeout=900)
    identities = [image_identity(reference)["Id"] for reference in references]
    require(image_identity(BASE)["Id"] == base["Id"], "SDK base changed during image build")
    # Pin subsequent operations to IDs, never mutable tags.
    require(inputs == source_digest(), "source inputs changed during image build; rerun on stable inputs")
    return base["Id"], identities, inputs


def provision(life, base_id, images):
    fixture = life.create(base_id, "sdk-fixture", "--network", "none")
    for remote, name in (("Resource/compass.SGM", "compass.SGM"),
                         ("Resource/Textures/compass.png", "compass.png")):
        life.copy(f"{fixture}:/opt/supermap/bin/bin/{remote}", life.work / name)
        os.chmod(life.work / name, 0o600)
    life.network = life.run_id
    life.docker("network", "create", "--internal", "--label", f"{LABEL}={life.run_id}", life.network)
    life.volume = life.run_id
    life.docker("volume", "create", "--label", f"{LABEL}={life.run_id}", life.volume)
    root_user, root_secret = "root-" + secrets.token_hex(8), secrets.token_hex(32)
    user, secret = "runtime-" + secrets.token_hex(8), secrets.token_hex(32)
    env = life.work / "minio.env"
    env.write_text(f"MINIO_ROOT_USER={root_user}\nMINIO_ROOT_PASSWORD={root_secret}\n")
    os.chmod(env, 0o600)
    minio = life.create(images[3], "minio", "--network", life.network, "--network-alias", "minio",
                        "--env-file", str(env), "--mount", f"type=volume,source={life.volume},target=/data",
                        cmd=("server", "/data"))
    supermap = life.create(images[0], "supermap", "--network", life.network, "--network-alias", "supermap")
    model = life.create(images[1], "model3d", "--network", life.network, "--network-alias", "model3d",
                        "--env", "SYSTEM_URL=http://127.0.0.1:1")
    for container in (minio, supermap, model):
        life.docker("start", container)
    ready = False
    for _ in range(120):
        try:
            life.docker("exec", minio, "curl", "-fsS", "http://127.0.0.1:9000/minio/health/ready", timeout=5)
            ready = True
            break
        except GateError:
            time.sleep(0.5)
    require(ready, "disposable MinIO did not become ready")
    mc_env = life.work / "runtime.env"
    mc_env.write_text(f"SGM_USER={user}\nSGM_SECRET={secret}\n")
    os.chmod(mc_env, 0o600)
    life.copy(mc_env, f"{minio}:/tmp/runtime.env")
    policy = {"Version": "2012-10-17", "Statement": [
        {"Effect": "Allow", "Action": ["s3:GetBucketLocation", "s3:ListBucket"],
         "Resource": ["arn:aws:s3:::sgm-t2"]},
        {"Effect": "Allow", "Action": ["s3:GetObject"],
         "Resource": ["arn:aws:s3:::sgm-t2/source/*", "arn:aws:s3:::sgm-t2/stage/*", "arn:aws:s3:::sgm-t2/result/*"]},
        {"Effect": "Allow", "Action": ["s3:PutObject"],
         "Resource": ["arn:aws:s3:::sgm-t2/stage/*", "arn:aws:s3:::sgm-t2/result/*"]},
        {"Effect": "Deny", "Action": ["s3:GetObject"],
         "Resource": ["arn:aws:s3:::sgm-t2/source/denied/compass.png"]}]}
    life.copy(life.private_json("policy.json", policy), f"{minio}:/tmp/policy.json")
    life.docker("exec", minio, "sh", "-ec", '''
        . /tmp/runtime.env
        mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null
        mc mb local/sgm-t2 >/dev/null
        mc admin user add local "$SGM_USER" "$SGM_SECRET" >/dev/null
        mc admin policy create local sgm-exact /tmp/policy.json >/dev/null
        mc admin policy attach local sgm-exact --user "$SGM_USER" >/dev/null
        rm /tmp/runtime.env /tmp/policy.json
    ''')
    license_lines = life.docker("exec", supermap, "sh", "-ec",
                               "sha256sum /opt/supermap/bin/bin/*.lic12").splitlines()
    license_hashes = sorted(line.split()[0] for line in license_lines)
    require(license_hashes and all(re.fullmatch("[0-9a-f]{64}", item) for item in license_hashes), "license SHA evidence missing")
    for name in ("compass.SGM", "compass.png"):
        life.copy(life.work / name, f"{model}:/tmp/{name}")
    config = {"root_user": root_user, "root_secret": root_secret, "user": user, "secret": secret}
    life.copy(life.private_json("probe.json", config), f"{model}:/tmp/sgm-probe.json")
    life.trace_file = (life.work / "minio-trace.jsonl").open("wb")
    life.trace = subprocess.Popen(["docker", "exec", minio, "mc", "admin", "trace", "--json", "--all", "--verbose", "local"],
                                  stdout=life.trace_file, stderr=subprocess.DEVNULL)
    time.sleep(1)
    require(life.trace.poll() is None, "MinIO request trace did not start")
    return model, license_hashes


def audit_trace(path):
    """Allowlist only non-secret fields; raw headers/bodies are never returned."""
    events = []
    for line in path.read_text().splitlines():
        entry = json.loads(line)
        call = entry.get("api", {})
        request = entry.get("request", {})
        response = entry.get("response", {})
        name = call.get("name", "") if isinstance(call, dict) else str(call)
        method, resource = request.get("method", ""), request.get("path", "")
        authorization = json.dumps(request.get("headers", {}))
        runtime_read = "Credential=runtime-" in authorization
        if method == "GET" and resource.startswith("/sgm-t2/source/") and runtime_read:
            events.append({"path": resource, "status": response.get("statusCode"), "api": name})
        require(not ("ListObjects" in name), "conversion unexpectedly listed an object directory")
    require(events, "real MinIO GET evidence missing")
    return events


def verify_candidate_requests(events):
    expected = [
        ("adjacent/compass.SGM", 200), ("adjacent/compass.png", 200),
        ("fallback/compass.SGM", 200), ("fallback/compass.png", 404), ("fallback/Textures/compass.png", 200),
        ("denied/compass.SGM", 200), ("denied/compass.png", 403),
        ("corrupt/compass.SGM", 200), ("corrupt/compass.png", 200),
        ("missing/compass.SGM", 200), ("missing/compass.png", 404), ("missing/Textures/compass.png", 404),
        ("adjacent/compass.SGM", 200), ("adjacent/compass.png", 200),
    ]
    observed = [(e["path"].removeprefix("/sgm-t2/source/"), e["status"]) for e in events]
    require(observed == expected, "exact candidate order/status differs or forbidden fallback was read")


def main():
    os.umask(0o077)
    destination = report_path(os.environ["SGM_T2_REPORT"]) if os.environ.get("SGM_T2_REPORT") else None
    life = Lifecycle()
    result = {"schema_version": "addp.supermap-sgm-t2/v1", "scope": "local-isolated-runtime-t2", "result": "failed"}
    result["run_id"] = life.run_id
    result["host"] = {"os": platform.system(), "architecture": platform.machine()}
    stage = "preflight"
    failure = None
    try:
        require(not os.environ.get("GITHUB_ACTIONS"), "licensed local SGM gate must not run in GitHub Actions")
        base_id, images, inputs = build_images(life)
        result.update({"MEDIA_SHA256": base_id.removeprefix("sha256:"), "images": images,
                       "source_commit": command(["git", "rev-parse", "HEAD"]).strip(),
                       "source_input_sha256": inputs, "platform": "linux/arm64"})
        stage = "provision"
        model, licenses = provision(life, base_id, images)
        result["LICENSE_SHA256"] = licenses
        stage = "real storage and runtime assertions"
        probe = (ROOT / "scripts/test/supermap-sgm-probe.py").read_bytes()
        result["checks"] = json.loads(life.docker("exec", "-i", model, "python", "-", data=probe, timeout=360))
        stage = "request trace"
        time.sleep(0.5)
        life.trace.terminate()
        life.trace.wait(timeout=10)
        life.trace_file.flush()
        result["source_gets"] = audit_trace(life.work / "minio-trace.jsonl")
        verify_candidate_requests(result["source_gets"])
        result["result"] = "passed"
    except (GateError, ValueError, OSError, KeyboardInterrupt) as error:
        failure = type(error).__name__
        result["failure_stage"] = stage
        print(f"SGM T2 failed at {stage} ({failure}); private output discarded", file=sys.stderr)
        if isinstance(error, GateError):
            print(str(error), file=sys.stderr)
    finally:
        failures = life.cleanup()
        result["owned_image_tags"] = life.images
        result["zero_residue"] = not failures
        if failures:
            result["cleanup_failures"] = failures
            result["result"] = "failed"
        if destination:
            with open(destination, "x", opener=lambda p, f: os.open(p, f, 0o600)) as stream:
                json.dump(result, stream, indent=2)
                stream.write("\n")
    print(f"SGM T2: {result['result']}; zero residue={result['zero_residue']}", flush=True)
    return 0 if result["result"] == "passed" and failure is None else 1


if __name__ == "__main__":
    def interrupted(signum, _frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupted)
    sys.exit(main())
