#!/usr/bin/env python3
"""Own the isolated Linux amd64/arm64 GaussDB developer media certification."""
from __future__ import annotations

import importlib.util
import json
import os
from pathlib import Path
import platform
import secrets
import shutil
import signal
import subprocess
import tempfile
import time
import uuid


ROOT = Path(__file__).resolve().parents[2]
PROBE_SOURCE = ROOT / "common/engine/certification/gaussdb/testdata/certification_integration_test.go"
LABEL = "com.addp.gaussdb-certification"
IMAGE_SPEC = importlib.util.spec_from_file_location("gaussdb_image", ROOT / "business/gaussdb/image.py")
IMAGE = importlib.util.module_from_spec(IMAGE_SPEC)
IMAGE_SPEC.loader.exec_module(IMAGE)


def command(args: list[str], *, output: Path | None = None, **kwargs) -> subprocess.CompletedProcess:
    check = kwargs.pop("check", True)
    if output is None:
        return subprocess.run(args, check=check, **kwargs)
    with output.open("w") as stream:
        return subprocess.run(args, stdout=stream, stderr=subprocess.STDOUT, check=check, **kwargs)


def verify_driver(facts: dict[str, str], directory: Path, environment: dict) -> None:
    identity = facts["GAUSSDB_GO_MODULE"] + "@" + facts["GAUSSDB_GO_VERSION"]
    downloaded = json.loads(command(["go", "mod", "download", "-json", identity],
                                   cwd=directory, env=environment, capture_output=True, text=True, timeout=180).stdout)
    expected = {"Path": facts["GAUSSDB_GO_MODULE"], "Version": facts["GAUSSDB_GO_VERSION"],
                "Sum": facts["GAUSSDB_GO_SUM"], "GoModSum": facts["GAUSSDB_GO_MOD_SUM"]}
    if downloaded.get("Error") or any(downloaded.get(key) != value for key, value in expected.items()):
        raise RuntimeError("candidate driver identity or checksum differs from pinned facts")


def prepare_probe(work_dir: Path, facts: dict[str, str]) -> Path:
    # Ordinary build tags are still loaded by tidy. Keep the source AND the
    # driver in an owner-created module outside the production module graph.
    probe = work_dir / "probe"
    probe.mkdir()
    environment = {**os.environ, "GOWORK": "off"}
    common = json.loads(command(["go", "mod", "edit", "-json"],
                               cwd=ROOT / "common", env=environment,
                               capture_output=True, text=True).stdout)
    postgres = [item["Version"] for item in common["Require"] if item["Path"] == "github.com/lib/pq"]
    if not common.get("Go") or len(postgres) != 1:
        raise RuntimeError("Common Go and PostgreSQL driver versions must be explicit")
    (probe / "go.mod").write_text("module addp.local/gaussdb-certification\n\ngo " + common["Go"] + "\n")
    shutil.copyfile(ROOT / "common/go.sum", probe / "go.sum")
    shutil.copyfile(PROBE_SOURCE, probe / "certification_integration_test.go")
    verify_driver(facts, probe, environment)
    command(["go", "mod", "edit", "-require=github.com/lib/pq@" + postgres[0],
             "-require=" + facts["GAUSSDB_GO_MODULE"] + "@" + facts["GAUSSDB_GO_VERSION"]], cwd=probe, env=environment)
    return probe


def compile_probe() -> None:
    """Check native driver registration and four builds, without T5 evidence."""
    facts = IMAGE.media_facts()
    with tempfile.TemporaryDirectory(prefix="addp-gaussdb-compile-") as work:
        work_dir = Path(work)
        probe = prepare_probe(work_dir, facts)
        environment = {**os.environ, "GOWORK": "off", "CGO_ENABLED": "0"}
        if "GOENV_REGIST_ALL" in environment:
            raise RuntimeError("candidate driver verification forbids GOENV_REGIST_ALL")
        for system in ("linux", "darwin"):
            for architecture in ("amd64", "arm64"):
                env = {**environment, "GOOS": system, "GOARCH": architecture}
                command(["go", "test", "-c", "-mod=mod", "-o", str(work_dir / (system + "-" + architecture + ".test")), "."],
                        cwd=probe, env=env, timeout=180)
                print("GaussDB probe compiled: " + system + "/" + architecture, flush=True)
        command(["go", "test", "-mod=mod", ".", "-run=^TestGaussDBDriverCoexistence$", "-count=1", "-v"],
                cwd=probe, env=environment, timeout=180)
    print("Four probe builds and host driver coexistence passed; native database T5 NOT RUN.")


def inspect(resource: str, name: str) -> dict | None:
    result = subprocess.run(["docker", resource, "inspect", name], capture_output=True, text=True)
    if result.returncode:
        if "no such" in result.stderr.lower() or "not found" in result.stderr.lower():
            return None
        raise RuntimeError("Docker resource state is unknown: " + resource)
    return json.loads(result.stdout)[0]


def clean_owned(run_id: str, container: str, network: str, image: str) -> None:
    errors = []
    for kind, name, delete in (("container", container, ["docker", "rm", "-fv", container]),
                               ("network", network, ["docker", "network", "rm", network]),
                               ("volume", container, ["docker", "volume", "rm", container]),
                               ("image", image, ["docker", "image", "rm", image])):
        try:
            state = inspect(kind, name)
            if state is None:
                continue
            labels = state.get("Config", {}).get("Labels", {}) if kind not in {"network", "volume"} else state.get("Labels", {})
            if (labels or {}).get(LABEL) != run_id:
                raise RuntimeError("refusing cleanup of resource with different owner: " + kind)
            command(delete, capture_output=True)
            if inspect(kind, name) is not None:
                raise RuntimeError("certification resource remains after cleanup: " + kind)
        except Exception as error:
            errors.append(str(error))
    if errors:
        raise RuntimeError("; ".join(errors))


def terminate(signum, _frame):
    raise SystemExit(128 + signum)


def probe_sql(container: str, statement: str, log_path: Path) -> subprocess.CompletedProcess:
    # Initial-user password SQL must never enter process arguments or reports.
    args = ["docker", "exec", "-i", container, "gsql", "-q", "-v", "ON_ERROR_STOP=1",
            "-h", "/data/run", "-d", "postgres", "-p", "5432", "-At"]
    try:
        result = subprocess.run(args, input=statement, capture_output=True, text=True, timeout=15)
    except subprocess.TimeoutExpired as error:
        def text(value):
            return value.decode(errors="replace") if isinstance(value, bytes) else value
        log_path.write_text(json.dumps({"timed_out": True, "stdout": text(error.stdout),
                                        "stderr": text(error.stderr)}, ensure_ascii=False) + "\n")
        raise
    log_path.write_text(json.dumps({"returncode": result.returncode, "stdout": result.stdout,
                                    "stderr": result.stderr}, ensure_ascii=False) + "\n")
    return result


def main() -> None:
    if platform.system() != "Linux":
        raise RuntimeError("GaussDB official-media T5 requires native Linux amd64/arm64")
    architecture = IMAGE.go_architecture(platform.machine())
    if "GOENV_REGIST_ALL" in os.environ:
        raise RuntimeError("candidate driver verification forbids GOENV_REGIST_ALL")
    for executable in ("docker", "go", "bash"):
        if not shutil.which(executable):
            raise RuntimeError(executable + " is required")
    daemon = command(["docker", "info", "--format", "{{.OSType}}/{{.Architecture}}"], capture_output=True, text=True).stdout.strip()
    IMAGE.verify_daemon_architecture(architecture, daemon)
    report_path = Path(os.environ["ADDP_GAUSSDB_CERTIFICATION_REPORT"])
    facts = IMAGE.media_facts()
    run_id = uuid.uuid4().hex
    container = "addp-gaussdb-cert-" + run_id
    network = container
    image = container + ":local"
    for kind, name in (("container", container), ("network", network), ("volume", container), ("image", image)):
        if inspect(kind, name) is not None:
            raise RuntimeError("refusing to replace preexisting resource")
    report = {"schema_version": "addp.gaussdb-official-media-certification/v1",
              "result": "failed", "phase": "media", "media": facts,
              "runner": {"os": "linux", "architecture": architecture},
              "scope": "developer-single-node-business-lifecycle-and-protocol", "cleanup": {"result": "not-run"}}
    password = ""
    app_password = ""
    previous_term = signal.signal(signal.SIGTERM, terminate)
    try:
        with tempfile.TemporaryDirectory(prefix="addp-gaussdb-certification-") as work:
            work_dir = Path(work)
            cache = Path(os.environ.get("ADDP_GAUSSDB_MEDIA_CACHE", "/tmp/addp-gaussdb-official-media"))
            IMAGE.prepare_context(work_dir, architecture, cache)
            report["phase"] = "image"
            command(["docker", "build", "--build-arg", "GAUSSDB_BASE_IMAGE=" + facts["GAUSSDB_OFFICIAL_BASE_IMAGE"],
                     "--build-arg", "GAUSSDB_VERSION=" + facts["GAUSSDB_OFFICIAL_VERSION"],
                     "--label", LABEL + "=" + run_id, "-t", image, work],
                    output=report_path.with_suffix(".build.log"), timeout=900)
            built = inspect("image", image)
            if not built or built.get("Architecture") != architecture or not built.get("Id"):
                raise RuntimeError("built image must match native architecture")
            report["image"] = {"id": built["Id"], "architecture": built["Architecture"],
                               "source": "addp-built-from-verified-official-media"}
            command(["docker", "network", "create", "--label", LABEL + "=" + run_id, network], capture_output=True)
            # Preserve 96 random bits within the kernel's default 32-character
            # maximum; the four-character prefix guarantees all four classes.
            password = "Ac9@" + secrets.token_hex(12)
            app_password = "Ab9@" + secrets.token_hex(12)
            env = {**os.environ, "GAUSSDB_PASSWORD": password, "GAUSSDB_APP_PASSWORD": app_password,
                   "GAUSSDB_USER": "addp_reader"}
            command(["docker", "volume", "create", "--label", LABEL + "=" + run_id, container], capture_output=True)
            run_args = ["docker", "run", "-d", "--name", container, "--network", network,
                        "--label", LABEL + "=" + run_id, "--shm-size=256m", "--mount", "type=volume,source=" + container + ",target=/data",
                        "-p", "127.0.0.1::5432", "-e", "GAUSSDB_PASSWORD", "-e", "GAUSSDB_APP_PASSWORD", "-e", "GAUSSDB_USER", image]

            def start_and_wait():
                command(run_args, capture_output=True, env=env)
                deadline = time.monotonic() + 240
                while time.monotonic() < deadline:
                    state = inspect("container", container)
                    if not state or not state["State"]["Running"]:
                        raise RuntimeError("official GaussDB exited during Business initialization")
                    health = command(["docker", "exec", container, "bash", "/addp/gaussdb/entrypoint.sh", "--health"],
                                     capture_output=True, text=True, check=False, timeout=15)
                    with report_path.with_suffix(".health.log").open("a") as log:
                        log.write(json.dumps({"returncode": health.returncode, "stdout": health.stdout,
                                              "stderr": health.stderr}, ensure_ascii=False) + "\n")
                    if health.returncode == 0:
                        return
                    time.sleep(2)
                raise RuntimeError("official GaussDB Business readiness timed out")

            report["phase"] = "readiness"
            start_and_wait()
            # Probe both container recreation and preservation of a user edit.
            statement = "\\connect business\nUPDATE addp_business.customers SET customer_name='持久化保留中文' WHERE id=1;\nCREATE TABLE addp_business.lifecycle_sentinel(id integer PRIMARY KEY);\nINSERT INTO addp_business.lifecycle_sentinel VALUES (42);"
            result = probe_sql(container, statement, report_path.with_suffix(".lifecycle.log"))
            if result.returncode:
                raise RuntimeError("Business persistence fixture failed")
            command(["docker", "stop", "--time", "60", container], capture_output=True, timeout=90)
            command(["docker", "rm", container], capture_output=True)
            start_and_wait()
            verify = "\\connect business\nSELECT (SELECT customer_name FROM addp_business.customers WHERE id=1) || '|' || (SELECT id::text FROM addp_business.lifecycle_sentinel);"
            result = probe_sql(container, verify, report_path.with_suffix(".lifecycle.log"))
            if result.returncode or result.stdout.strip() != "持久化保留中文|42":
                raise RuntimeError("Business volume or user edits were not preserved")
            report["business_lifecycle"] = {"container_recreated": True, "volume_preserved": True, "user_edit_preserved": True}
            # Only synthetic data in the owner disposable container; no shared Infra.
            report["phase"] = "fixture"
            sql = """CREATE SCHEMA addp_cert;
CREATE TABLE addp_cert.source(id BIGINT PRIMARY KEY,name TEXT,amount NUMERIC(38,18));
INSERT INTO addp_cert.source VALUES (1,'高斯认证中文',12345678901234567890.123456789012345678);
GRANT USAGE ON SCHEMA addp_cert TO addp_reader;
GRANT SELECT ON addp_cert.source TO addp_reader;
"""
            command(["docker", "exec", "-i", container, "gsql", "-v", "ON_ERROR_STOP=1", "-h", "/data/run", "-d", "postgres", "-p", "5432"], input=sql, text=True, capture_output=True)
            state = inspect("container", container)
            port = state["NetworkSettings"]["Ports"]["5432/tcp"][0]["HostPort"]
            report["phase"] = "driver"
            probe = prepare_probe(work_dir, facts)
            report["phase"] = "probe"
            probe_env = {**os.environ, "GOWORK": "off",
                         "ADDP_GAUSSDB_CERTIFICATION": "1",
                         "ADDP_TEST_GAUSSDB_DSN": f"host=127.0.0.1 port={port} dbname=postgres user=addp_reader password={app_password} sslmode=disable connect_timeout=10"}
            command(["go", "test", "-mod=mod", ".", "-run=^TestGaussDB(DriverCoexistence|OfficialMediaCertification)$", "-count=1", "-v"],
                    output=report_path.with_suffix(".probe.log"), env=probe_env, cwd=probe, timeout=180)
            if "--- SKIP:" in report_path.with_suffix(".probe.log").read_text():
                raise RuntimeError("native GaussDB certification refuses skipped tests")
            report["result"] = "passed"
            report["phase"] = "complete"
    except BaseException:
        report["result"] = "failed"
        raise
    finally:
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        try:
            try:
                if inspect("container", container) is not None:
                    command(["docker", "logs", container], output=report_path.with_suffix(".container.log"))
            finally:
                try:
                    clean_owned(run_id, container, network, image)
                finally:
                    if password:
                        for suffix in (".build.log", ".probe.log", ".container.log", ".lifecycle.log", ".health.log"):
                            log_path = report_path.with_suffix(suffix)
                            if log_path.exists():
                                content = log_path.read_text(errors="replace")
                                for secret in (password, app_password):
                                    if secret:
                                        content = content.replace(secret, "[REDACTED]")
                                log_path.write_text(content)
            report["cleanup"] = {"result": "passed", "residual_resources": 0}
        except Exception:
            report["result"] = "failed"
            report["cleanup"] = {"result": "failed"}
            raise
        finally:
            try:
                report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
            finally:
                signal.signal(signal.SIGTERM, previous_term)


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--compile-only", action="store_true")
    args = parser.parse_args()
    if args.compile_only:
        compile_probe()
    else:
        main()
