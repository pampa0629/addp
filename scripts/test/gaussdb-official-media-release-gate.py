#!/usr/bin/env python3
"""Own the isolated Linux x86_64 GaussDB developer media certification."""
from __future__ import annotations

import hashlib
import io
import json
import os
from pathlib import Path
import platform
import secrets
import shutil
import signal
import subprocess
import tarfile
import tempfile
import time
import urllib.request
import uuid
import zipfile


ROOT = Path(__file__).resolve().parents[2]
PROBE_SOURCE = ROOT / "common/engine/certification/gaussdb/testdata/certification_integration_test.go"
LABEL = "com.addp.gaussdb-certification"
FACT_NAMES = ("GAUSSDB_OFFICIAL_VERSION", "GAUSSDB_OFFICIAL_MEDIA_URL",
              "GAUSSDB_OFFICIAL_MEDIA_SHA256", "GAUSSDB_OFFICIAL_PACKAGE_ROOT",
              "GAUSSDB_OFFICIAL_DRIVER_URL", "GAUSSDB_OFFICIAL_DRIVER_SHA256",
              "GAUSSDB_OFFICIAL_BASE_IMAGE")


def media_facts() -> dict[str, str]:
    script = 'source "$1"; gaussdb_select_official_media x86_64; '
    script += 'printf "%s\\n" ' + " ".join('"$' + name + '"' for name in FACT_NAMES)
    values = subprocess.check_output(
        ["bash", "-eu", "-c", script, "facts", str(ROOT / "scripts/lib/gaussdb-official-media.sh")],
        text=True).splitlines()
    if len(values) != len(FACT_NAMES) or not all(values):
        raise RuntimeError("incomplete official media facts")
    return dict(zip(FACT_NAMES, values, strict=True))


def sha256(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def download(url: str, expected: str, directory: Path) -> Path:
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / url.rsplit("/", 1)[1]
    if path.exists() and sha256(path) == expected:
        return path
    partial = directory / (path.name + "." + uuid.uuid4().hex + ".partial")
    try:
        with urllib.request.urlopen(url, timeout=60) as response, partial.open("wb") as stream:
            shutil.copyfileobj(response, stream, 1024 * 1024)
        if sha256(partial) != expected:
            raise RuntimeError("official media SHA256 mismatch")
        partial.replace(path)
        return path
    finally:
        partial.unlink(missing_ok=True)


def unique_member(names: list[str], suffix: str) -> str:
    matches = [name for name in names if name.endswith(suffix)]
    if len(matches) != 1:
        raise RuntimeError("official media must contain exactly one " + suffix)
    return matches[0]


def extract_kernel(archive: Path, destination: Path) -> None:
    # Explicit member selection keeps both nested archives and architectures apart.
    with zipfile.ZipFile(archive) as package:
        outer = unique_member(package.namelist(), "_Server_X86_Developer.tar.gz")
        with tarfile.open(fileobj=io.BytesIO(package.read(outer))) as first:
            inner = unique_member(first.getnames(), "_64bit_Server.tar.gz")
            with tarfile.open(fileobj=first.extractfile(inner)) as second:
                name = unique_member(second.getnames(), ".bin")
                binary = second.extractfile(name).read()
                digest_name = unique_member(second.getnames(), "_64bit.sha256")
                expected = second.extractfile(digest_name).read().decode().strip()
                if hashlib.sha256(binary).hexdigest() != expected:
                    raise RuntimeError("official inner kernel SHA256 mismatch")
                destination.write_bytes(binary)


def extract_driver(archive: Path, package_root: str, destination: Path) -> Path:
    with zipfile.ZipFile(archive) as package:
        names = [n for n in package.namelist() if n.startswith(package_root + "/client/go/")]
        outer = unique_member(names, ".tar.gz")
        with tarfile.open(fileobj=io.BytesIO(package.read(outer))) as first:
            inner = unique_member(first.getnames(), ".tar.gz")
            with tarfile.open(fileobj=first.extractfile(inner)) as source:
                for member in source.getmembers():
                    if not member.isfile():
                        continue
                    target = (destination / member.name).resolve()
                    if not target.is_relative_to(destination.resolve()):
                        raise RuntimeError("driver archive path escapes its owner directory")
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_bytes(source.extractfile(member).read())
    driver = destination / "openGauss-connector-go-pq"
    if not (driver / "go.mod").is_file() or not (driver / "LICENSE.md").is_file():
        raise RuntimeError("official driver module or license is missing")
    return driver


def command(args: list[str], *, output: Path | None = None, **kwargs) -> subprocess.CompletedProcess:
    if output is None:
        return subprocess.run(args, check=True, **kwargs)
    with output.open("w") as stream:
        return subprocess.run(args, stdout=stream, stderr=subprocess.STDOUT, check=True, **kwargs)


def prepare_probe(work_dir: Path, driver: Path) -> Path:
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
    command(["go", "mod", "edit", "-require=github.com/lib/pq@" + postgres[0],
             "-require=gitee.com/opengauss/openGauss-connector-go-pq@v0.0.0",
             "-replace=gitee.com/opengauss/openGauss-connector-go-pq=" + str(driver)], cwd=probe, env=environment)
    return probe


def compile_probe() -> None:
    """Cross-compile the real probe without executing it or writing T5 evidence."""
    facts = media_facts()
    cache = Path(os.environ.get("ADDP_GAUSSDB_MEDIA_CACHE", "/tmp/addp-gaussdb-official-media"))
    archive = download(facts[FACT_NAMES[4]], facts[FACT_NAMES[5]], cache)
    with tempfile.TemporaryDirectory(prefix="addp-gaussdb-compile-") as work:
        work_dir = Path(work)
        driver = extract_driver(archive, facts[FACT_NAMES[3]], work_dir / "driver")
        probe = prepare_probe(work_dir, driver)
        env = {**os.environ, "GOWORK": "off", "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0"}
        command(["go", "test", "-c", "-mod=mod", "-o", str(work_dir / "probe.test"), "."], cwd=probe, env=env, timeout=180)
    print("GaussDB Linux x86_64 probe compiled; native startup and protocol tests NOT RUN.")


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
                               ("image", image, ["docker", "image", "rm", image])):
        try:
            state = inspect(kind, name)
            if state is None:
                continue
            labels = state.get("Config", {}).get("Labels", {}) if kind != "network" else state.get("Labels", {})
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
    args = ["docker", "exec", "-i", container, "gsql", "-v", "ON_ERROR_STOP=1",
            "-d", "postgres", "-p", "5432", "-At"]
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
    if platform.system() != "Linux" or platform.machine() != "x86_64":
        raise RuntimeError("GaussDB official-media T5 requires native Linux x86_64")
    for executable in ("docker", "go", "bash"):
        if not shutil.which(executable):
            raise RuntimeError(executable + " is required")
    daemon = command(["docker", "info", "--format", "{{.OSType}}/{{.Architecture}}"], capture_output=True, text=True).stdout.strip()
    if daemon not in {"linux/x86_64", "linux/amd64"}:
        raise RuntimeError("native Linux x86_64 Docker daemon is required")
    report_path = Path(os.environ["ADDP_GAUSSDB_CERTIFICATION_REPORT"])
    facts = media_facts()
    run_id = uuid.uuid4().hex
    container = "addp-gaussdb-cert-" + run_id
    network = container
    image = container + ":local"
    for kind, name in (("container", container), ("network", network), ("image", image)):
        if inspect(kind, name) is not None:
            raise RuntimeError("refusing to replace preexisting resource")
    report = {"schema_version": "addp.gaussdb-official-media-certification/v1",
              "result": "failed", "phase": "media", "media": facts,
              "runner": {"os": "linux", "architecture": "x86_64"},
              "scope": "developer-single-node-protocol-only", "cleanup": {"result": "not-run"}}
    password = ""
    initial_password = ""
    previous_term = signal.signal(signal.SIGTERM, terminate)
    try:
        with tempfile.TemporaryDirectory(prefix="addp-gaussdb-certification-") as work:
            work_dir = Path(work)
            cache = Path(os.environ.get("ADDP_GAUSSDB_MEDIA_CACHE", "/tmp/addp-gaussdb-official-media"))
            media = download(facts[FACT_NAMES[1]], facts[FACT_NAMES[2]], cache)
            driver_media = download(facts[FACT_NAMES[4]], facts[FACT_NAMES[5]], cache)
            extract_kernel(media, work_dir / "kernel.bin")
            driver = extract_driver(driver_media, facts[FACT_NAMES[3]], work_dir / "driver")
            report["phase"] = "image"
            (work_dir / "Dockerfile").write_text(f'''FROM {facts[FACT_NAMES[6]]}
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libaio1 libnuma1 libreadline8 libncurses6 libncurses5 libtinfo5 zlib1g libatomic1 libltdl7 libtirpc3 locales && rm -rf /var/lib/apt/lists/*
COPY kernel.bin /tmp/kernel.bin
RUN chmod 700 /tmp/kernel.bin && /tmp/kernel.bin -y -o/opt/gauss && rm /tmp/kernel.bin && useradd -m -u 10001 omm && mkdir /data && chown -R omm:omm /opt/gauss /data
ENV GAUSSHOME=/opt/gauss GAUSSLOG=/data/log LD_LIBRARY_PATH=/opt/gauss/lib PATH=/opt/gauss/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
COPY entrypoint.sh /entrypoint.sh
RUN chmod 755 /entrypoint.sh
USER omm
RUN gsql --version && gaussdb --version
ENTRYPOINT ["/entrypoint.sh"]
''')
            (work_dir / "entrypoint.sh").write_text('''#!/bin/bash
set -euo pipefail
umask 077
mkdir -p "$GAUSSLOG"
printf '%s\\n' "$GAUSS_CERT_SECRET" > /data/password
unset GAUSS_CERT_SECRET
gs_initdb -D /data/db --nodename=addp_cert -U omm --pwfile=/data/password -E UTF8 --locale=C
rm /data/password
[ -f /data/db/gs_hba.conf ]
printf 'host all all 0.0.0.0/0 sha256\\n' >> /data/db/gs_hba.conf
exec gaussdb --single_node -D /data/db -p 5432 -c listen_addresses='*' -c max_connections=32 -c shared_buffers=128MB -c max_process_memory=2GB -c logging_collector=off
''')
            command(["docker", "build", "--label", LABEL + "=" + run_id, "-t", image, work],
                    output=report_path.with_suffix(".build.log"), timeout=900)
            command(["docker", "network", "create", "--label", LABEL + "=" + run_id, network], capture_output=True)
            password = "AddpCert9@" + secrets.token_hex(12)
            initial_password = "AddpInit9@" + secrets.token_hex(12)
            env = {**os.environ, "GAUSS_CERT_SECRET": initial_password}
            command(["docker", "run", "-d", "--name", container, "--network", network,
                     "--label", LABEL + "=" + run_id, "--shm-size=256m",
                     "-p", "127.0.0.1::5432", "-e", "GAUSS_CERT_SECRET", image], capture_output=True, env=env)
            report["phase"] = "initial-password"
            deadline = time.monotonic() + 240
            while time.monotonic() < deadline:
                initialized = probe_sql(container, f"ALTER ROLE omm PASSWORD '{password}';",
                                        report_path.with_suffix(".initial-password.log"))
                if initialized.returncode == 0 and initialized.stdout.strip() == "ALTER ROLE":
                    break
                state = inspect("container", container)
                if not state or not state["State"]["Running"]:
                    raise RuntimeError("official GaussDB exited before readiness")
                time.sleep(2)
            else:
                raise RuntimeError("official GaussDB initial password not changed within 240 seconds")
            report["phase"] = "readiness"
            ready = probe_sql(container, "SELECT 1", report_path.with_suffix(".readiness.log"))
            if ready.returncode != 0 or ready.stdout.strip() != "1":
                raise RuntimeError("official GaussDB SQL readiness failed after initial password change")
            # Only synthetic data in the owner disposable container; no shared Infra.
            report["phase"] = "fixture"
            sql = f"""CREATE USER addp_reader PASSWORD '{password}';
CREATE SCHEMA addp_cert;
CREATE TABLE addp_cert.source(id BIGINT PRIMARY KEY,name TEXT,amount NUMERIC(38,18));
INSERT INTO addp_cert.source VALUES (1,'高斯认证中文',12345678901234567890.123456789012345678);
GRANT USAGE ON SCHEMA addp_cert TO addp_reader;
GRANT SELECT ON addp_cert.source TO addp_reader;
"""
            command(["docker", "exec", "-i", container, "gsql", "-v", "ON_ERROR_STOP=1", "-d", "postgres", "-p", "5432"], input=sql, text=True, capture_output=True)
            state = inspect("container", container)
            port = state["NetworkSettings"]["Ports"]["5432/tcp"][0]["HostPort"]
            probe = prepare_probe(work_dir, driver)
            report["phase"] = "probe"
            probe_env = {**os.environ, "GOWORK": "off", "GOENV_REGIST_ALL": "false",
                         "ADDP_GAUSSDB_CERTIFICATION": "1",
                         "ADDP_TEST_GAUSSDB_DSN": f"host=127.0.0.1 port={port} dbname=postgres user=addp_reader password={password} sslmode=disable connect_timeout=10"}
            command(["go", "test", "-mod=mod", ".", "-run=^TestGaussDBOfficialMediaCertification$", "-count=1", "-v"],
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
                        for suffix in (".build.log", ".initial-password.log", ".readiness.log", ".probe.log", ".container.log"):
                            log_path = report_path.with_suffix(suffix)
                            if log_path.exists():
                                content = log_path.read_text(errors="replace")
                                for secret in (password, initial_password):
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
