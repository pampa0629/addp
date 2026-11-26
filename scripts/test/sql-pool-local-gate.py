#!/usr/bin/env python3
"""Run the MySQL timeout page checks on a disposable local Compose project."""
from __future__ import annotations

import argparse
import base64
import importlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shlex
import shutil
import signal
import socket
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
CORE = importlib.import_module("scripts.test.supermap-sgm-gate")
POOL = importlib.import_module("scripts.test.sql-pool-consumer-flow-online")
COMPOSE = importlib.import_module("scripts.utils.disposable-compose")
LABEL = POOL.LOCAL_LABEL
BACKENDS = ("system-backend", "meta-backend", "meta-worker", "security-backend",
            "security-worker", "manager-backend", "develop-backend", "gateway")
FRONTENDS = ("console", "system-frontend", "meta-frontend", "manager-frontend", "develop-frontend", "nginx")
INFRA = ("postgres", "redis", "minio", "meilisearch")
require = CORE.require


def validate_local(environment, endpoint, machine, daemon):
    require(not any(environment.get(key) for key in ("GITHUB_ACTIONS", "ADDP_ONLINE_HOSTED",
                "ADDP_ONLINE_OWNER_MANAGED", POOL.LOCAL_RUN)), "local SQL pool refuses foreign profiles")
    require(endpoint.startswith("unix://"), "local SQL pool requires a local Docker socket")
    aliases = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64", "amd64": "amd64"}
    require(machine in aliases and aliases.get(daemon) == aliases[machine], "local Docker must run the native architecture")
    return aliases[machine]


def deployment(documents, run_id, images, public_port, proxy, gate, checkout):
    selected = INFRA + BACKENDS + FRONTENDS
    config = COMPOSE.isolate_compose({"services": documents}, selected, run_id, images, label=LABEL)
    services = config["services"]
    immutable_mounts = {"postgres": "/addp/infra-postgresql.conf", "redis": "/addp/redis-authentication.sh"}
    for name, target in immutable_mounts.items():
        source, = (m for m in documents[name]["volumes"] if m["target"] == target)
        require(source["type"] == "bind" and source["read_only"]
                and Path(source["source"]).resolve().is_relative_to(checkout.resolve()),
                "Infra configuration must come from the read-only source snapshot")
        for i, mount in enumerate(services[name]["volumes"]):
            if mount["target"] == target:
                del config["volumes"][mount["source"]]
                services[name]["volumes"][i] = source
    services["postgres"]["command"] = [*services["postgres"]["command"], "-c", f"cluster_name={run_id}"]
    services["nginx"]["ports"][0]["published"] = str(public_port)
    services["manager-backend"]["environment"]["META_URL"] = "http://pool-meta:8082"
    services["pool-meta"] = {
        "container_name": f"{run_id}-pool-meta",
        "image": images["gateway"],
        "entrypoint": ["/pool-proxy", "/gate", "--compose"],
        "command": [],
        "user": str(os.getuid()),
        "environment": {},
        "restart": "no",
        "labels": {LABEL: run_id},
        "networks": {"isolated": {"aliases": ["pool-meta"]}},
        "volumes": [
            {"type": "bind", "source": str(proxy), "target": "/pool-proxy", "read_only": True},
            {"type": "bind", "source": str(gate), "target": "/gate"},
        ],
    }
    return config


def validate_local_browser(evidence, expected):
    require(evidence.get("schema_version") == "addp.sql-pool-console-local/v1"
            and evidence.get("run_id") == expected["run_id"]
            and evidence.get("consumer_principal_id") == expected["principal_id"]
            and evidence.get("operator_principal_id") == expected["operator_principal_id"]
            and expected["principal_id"] != expected["operator_principal_id"], "local browser identity does not match")
    cases = evidence.get("cases")
    require(isinstance(cases, list) and len(cases) == 1 and len(expected["cases"]) == 1, "expected one local MySQL case")
    case, = cases
    sample, = expected["cases"]
    require(case.get("engine_type") == "mysql" and all(case.get(key) == value for key, value in sample.items()),
            "local MySQL browser evidence differs from the prepared source")
    POOL.validate_saturation(case, expected["run_id"], set(), local=True)


class LocalPool(CORE.Lifecycle):
    def __init__(self, environment):
        super().__init__(prefix="addp-sql-pool-local-", label=LABEL)
        self.work = self.work.resolve()
        self.env = environment
        self.phase = "snapshot"
        self.checkout = self.work / "source"
        self.private = self.work / "private"
        self.private.mkdir(mode=0o700)
        self.artifacts = Path(tempfile.mkdtemp(prefix=self.run_id + "-evidence-")).resolve()
        self.report = {"scope": "local-isolated-technical-page-acceptance", "formal_t4": False, "run_id": self.run_id}
        self.identities = {}

    def announce(self, phase):
        self.phase = phase
        print("Local SQL pool:", phase, flush=True)

    def execute(self, args, *, timeout=180, data=None, environment=None):
        return CORE.command(args, timeout=timeout, data=data, environment=environment or self.env,
                            failure_log=self.private / f"failure-{self.phase}.log")

    def docker(self, *args, **kwargs):
        # Cleanup can attempt several independent commands in the same phase.
        return CORE.command(["docker", *args], environment=self.env, **kwargs)

    def compose(self, *args):
        return self.execute(["docker", "compose", "--env-file", "/dev/null", "-f", str(self.work / "compose.json"), *args],
                            timeout=420)

    def port(self, service, container_port):
        info = json.loads(self.docker("inspect", f"{self.run_id}-{service}"))[0]
        require(info["Config"]["Labels"].get(LABEL) == self.run_id, "foreign container")
        bindings = info["NetworkSettings"]["Ports"].get(f"{container_port}/tcp")
        require(len(bindings or []) == 1 and bindings[0]["HostIp"] == "127.0.0.1", "loopback port required")
        return bindings[0]["HostPort"]

    def fixture(self, action, identities=True):
        environment = {**self.env, **self.identities} if identities else self.env
        self.execute([sys.executable, "-m", "scripts.test.sql-pool-consumer-flow-online", action], environment=environment)

    def defaults(self, origin):
        template = self.execute(["bash", "-c", "set -a && source .env.example && env -0"])
        values = dict(field.split("=", 1) for field in template.split("\0") if "=" in field)
        for key in values:
            if key.endswith(("_PASSWORD", "_SECRET", "_KEY", "_PEPPER")):
                values[key] = secrets.token_hex(32)
        for key in ("ENCRYPTION_KEY", "IAM_MFA_ENCRYPTION_KEY", "OAUTH_USER_CODE_PEPPER"):
            values[key] = base64.b64encode(secrets.token_bytes(32)).decode()
        values.update(self.env)
        values.update(ENV="test", POSTGRES_DB="addp_test", POSTGRES_USER="pool_fixture", POSTGRES_HOST="127.0.0.1",
                      ADDP_DATABASE_RUNTIME_USER="platform_runtime", ADDP_DATABASE_RUNTIME_PASSWORD=secrets.token_hex(32),
                      COMPOSE_DISABLE_ENV_FILE="1", ADDP_PUBLIC_ORIGIN=origin, ALLOWED_ORIGINS=origin,
                      ADDP_OBSERVABILITY_LOGS_ENABLED="false", ADDP_OBSERVABILITY_METRICS_ENABLED="false",
                      NO_PROXY="127.0.0.1,localhost,.localhost", no_proxy="127.0.0.1,localhost,.localhost")
        values.update({POOL.LOCAL_RUN: self.run_id, "ADDP_ONLINE_TEST_RUN_ID": self.run_id,
                       "ADDP_ONLINE_SECRET_DIR": str(self.private), "CONSOLE_URL": origin,
                       "ADDP_POOL_META_GATE": str(self.private / "meta"),
                       "ADDP_ONLINE_ARTIFACT_DIR": str(self.private / "browser")})
        return values

    def browser(self):
        self.announce("browser_timeout_and_recovery")
        allowed = ("ADDP_ONLINE_TEST_USER_USERNAME", "ADDP_ONLINE_TEST_USER_PASSWORD",
                   "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN", "ADDP_ONLINE_POOL_OPERATOR_USERNAME", "ADDP_ONLINE_POOL_OPERATOR_PASSWORD")
        public_keys = ("PATH", "HOME", "TMPDIR", "DOCKER_HOST", "NO_PROXY", "no_proxy", POOL.LOCAL_RUN, "ADDP_ONLINE_TEST_RUN_ID",
                       "ADDP_ONLINE_SECRET_DIR", "ADDP_ONLINE_ARTIFACT_DIR", "CONSOLE_URL", "GATEWAY_URL", "ADDP_POOL_META_GATE")
        report = self.private / "browser" / "sql-pool-console.json"
        env = {key: self.env[key] for key in public_keys if key in self.env}
        env.update({key: self.identities[key] for key in allowed})
        env.update(ADDP_ONLINE_POOL_EXPECTATIONS=str(self.private / "pool-expectations.json"),
                   ADDP_ONLINE_POOL_REPORT=str(report), ADDP_ONLINE_POOL_PRIVATE=str(self.private))
        self.execute(["bash", "-c", 'cd console/frontend && exec npm exec -- playwright test '
                      'e2e/online/sql-pool-consumer-flow.spec.js --config=playwright.online.config.js'],
                     environment=env, timeout=900)
        evidence = json.loads(report.read_text())
        expected = json.loads((self.private / "pool-expectations.json").read_text())
        validate_local_browser(evidence, expected)
        for name in ("sql-pool-console.json", "mysql-saturation-manager-queued.png", "mysql-saturation-manager-native.png",
                     "mysql-saturation-develop.png", "mysql-saturation-native.png", "sql-pool-queue-barriers.jsonl",
                     "sql-pool-native-barriers.jsonl"):
            shutil.copyfile(report.parent / name, self.artifacts / name)

    def run(self, commit, architecture):
        self.report["source_commit"] = commit
        self.execute(["git", "clone", "--shared", "--no-checkout", "--local", str(ROOT), str(self.checkout)], timeout=300)
        CORE.ROOT = self.checkout
        self.execute(["git", "checkout", "--detach", commit], timeout=300)
        require(self.execute(["git", "rev-parse", "HEAD"]).strip() == commit, "isolated source changed")
        with socket.socket() as reservation:
            reservation.bind(("127.0.0.1", 0))
            public_port = reservation.getsockname()[1]
        self.env = self.defaults(f"http://pool-{self.run_id[-32:]}.localhost:{public_port}")
        # Share only downloaded Go modules. The snapshot, compiled artifacts,
        # and writable compilation cache belong to this invocation.
        self.env["ADDP_BUILD_GOMODCACHE"] = self.execute(["go", "env", "GOMODCACHE"]).strip()
        docker = shutil.which("docker", path=self.env["PATH"])
        require(docker, "Docker executable not found")
        tools = self.private / "tools"
        tools.mkdir(mode=0o700)
        wrapper = tools / "docker"
        wrapper.write_text(
            "#!/usr/bin/env python3\nimport os, sys\n"
            f"docker = {docker!r}\n"
            "args = sys.argv[1:]\n"
            f"if args[:1] == ['run']: args[1:1] = ['--label', {LABEL + '=' + self.run_id!r}]\n"
            "os.execv(docker, [docker, *args])\n"
        )
        wrapper.chmod(0o700)
        build_env = {**self.env, "PATH": str(tools) + os.pathsep + self.env["PATH"]}
        self.announce("build_product")
        self.execute(["make", "build", f"BUILD_ARGS=--arch {architecture} --services " + ",".join(BACKENDS)],
                     environment=build_env, timeout=3600)
        names = BACKENDS + FRONTENDS
        self.images = [f"localhost:5001/addp-{name}:{self.run_id}" for name in names]
        self.execute(["make", "build-images", "IMAGE_BUILD_ARGS=--verify --jobs 2 --services " + ",".join(names)
                      + f" --registry localhost:5001 --tag {self.run_id}"], environment=build_env, timeout=4200)
        images = {key: self.docker("image", "inspect", tag, "--format", "{{.Id}}").strip() for key, tag in zip(names, self.images)}
        self.announce("build_test_tools")
        gate = self.private / "meta"
        gate.mkdir(mode=0o700)
        (self.private / "browser").mkdir(mode=0o700)
        proxy = self.private / "proxy"
        self.execute(["go", "build", "-o", str(proxy), "scripts/test/testdata/sql-pool-meta/main.go"],
                     environment={**self.env, "GOOS": "linux", "GOARCH": architecture, "CGO_ENABLED": "0"})
        self.execute(["bash", "-c", "cd console/frontend && npm ci && npm exec -- playwright install chromium"], timeout=900)
        self.announce("start_owned_deployment")
        documents = {}
        for file, services in (("docker-compose.infra.yml", INFRA), ("docker-compose.yml", names)):
            compose = json.loads(self.execute(["docker", "compose", "--env-file", "/dev/null", "-f", file, "config", "--format", "json"]))
            documents.update({name: compose["services"][name] for name in services})
        self.private_json("compose.json", deployment(documents, self.run_id, images, public_port, proxy, gate, self.checkout))
        self.compose("up", "-d", "--wait", *INFRA)
        self.execute(["bash", "scripts/infra/init-minio.sh", "--compose-file", str(self.work / "compose.json")],
                     environment={**self.env, "ADDP_INFRA_RESOLVED": "1"})
        self.execute(["docker", "compose", "--env-file", "/dev/null", "-f", str(self.work / "compose.json"),
                      "exec", "-T", "postgres", "psql", "-v", "ON_ERROR_STOP=1",
                      "-U", self.env["POSTGRES_USER"], "-d", "addp_test"],
                     data=(self.checkout / "scripts/infra/init-postgresql.sql").read_bytes())
        self.execute(["bash", "scripts/test/hosted-platform-database.sh", self.run_id, str(self.work / "compose.json"), LABEL, *BACKENDS], timeout=420)
        self.compose("up", "-d", "--wait", "system-backend")
        self.compose("up", "-d", "--wait", "meta-backend", "meta-worker", "security-backend", "security-worker")
        self.compose("up", "-d", "--wait", "pool-meta")
        deadline = time.monotonic() + 15
        while not (gate / "url").is_file():
            require(time.monotonic() < deadline, "local Meta timing fixture did not start")
            time.sleep(0.1)
        self.compose("up", "-d", "--wait", "manager-backend", "develop-backend", "gateway")
        self.compose("up", "-d", "--wait", *FRONTENDS[:-1])
        self.compose("up", "-d", "--wait", "nginx")
        self.env.update(GATEWAY_URL="http://127.0.0.1:" + self.port("gateway", 8000), POSTGRES_PORT=self.port("postgres", 5432))
        servers = ("system-backend", "manager-backend", "develop-backend", "gateway")
        def runtime_identity():
            rows = json.loads(self.docker("inspect", *(f"{self.run_id}-{name}" for name in servers)))
            return [(row["Id"], row["State"]["StartedAt"], row["Image"]) for row in rows
                    if row["State"]["Running"] and row["Config"]["Labels"].get(LABEL) == self.run_id]
        initial = runtime_identity()
        require(len(initial) == len(servers) and all(row[-1] == images[name] for name, row in zip(servers, initial)),
                "deployed runtime images differ from committed build")
        self.announce("prepare_mysql_and_identities")
        self.fixture("start", identities=False)
        identity_file = self.private / "identities.env"
        self.execute(["bash", "-c", 'cd system/backend && exec go run ./cmd/online-test-fixture '
                      '--suite sql-pool-consumer-flow --wait-protection-owners manager,develop --output "$1"', "_", str(identity_file)],
                     timeout=420)
        self.identities = dict(shlex.split(line)[-1].split("=", 1) for line in identity_file.read_text().splitlines() if line)
        self.fixture("prepare")
        self.browser()
        require(runtime_identity() == initial, "a product process restarted during the acceptance")
        self.report["processes_unchanged"] = True


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--commit", default="HEAD")
    args = parser.parse_args()
    require(platform.system() in {"Darwin", "Linux"}, "local SQL pool requires macOS or Linux")
    environment = {key: value for key, value in os.environ.items() if key in {"PATH", "HOME", "TMPDIR", "DOCKER_HOST", "DOCKER_CONTEXT"}}
    context = [environment["DOCKER_CONTEXT"]] if environment.get("DOCKER_CONTEXT") else []
    info = json.loads(CORE.command(["docker", "context", "inspect", *context], environment=environment))[0]
    endpoint = info["Endpoints"]["docker"]["Host"] if context else environment.get("DOCKER_HOST", info["Endpoints"]["docker"]["Host"])
    require(endpoint.startswith("unix://"), "local SQL pool requires a local Docker socket")
    environment.pop("DOCKER_CONTEXT", None)
    environment["DOCKER_HOST"] = endpoint
    daemon = CORE.command(["docker", "info", "--format", "{{.Architecture}}"], environment=environment).strip()
    architecture = validate_local(os.environ, endpoint, platform.machine(), daemon)
    commit = CORE.command(["git", "rev-parse", "--verify", "--end-of-options", args.commit + "^{commit}"], environment=environment).strip()
    head = CORE.command(["git", "rev-parse", "HEAD"], environment=environment).strip()
    result = CORE.command(["git", "merge-base", commit, head], environment=environment).strip()
    require(re.fullmatch(r"[0-9a-f]{40}", commit) and result == commit, "use a committed main revision")
    for shared in ("scripts/test/sql-pool-local-gate.py", "scripts/test/sql-pool-consumer-flow-online.py",
                   "console/frontend/e2e/online/sql-pool-consumer-flow.spec.js"):
        require(CORE.command(["git", "show", f"{commit}:{shared}"], environment=environment) == (ROOT / shared).read_text(),
                "runner inputs must match the selected commit")
    life = LocalPool(environment)
    print(f"Local SQL pool report directory: {life.artifacts}", flush=True)
    try:
        life.run(commit, architecture)
        life.report["result"] = "passed"
    except (Exception, KeyboardInterrupt) as error:
        life.report.update(result="failed", failed_phase=life.phase, failure_type=type(error).__name__)
        print(f"Local SQL pool failed in {life.phase}: {type(error).__name__}", flush=True)
    finally:
        life.announce("revoke_and_cleanup")
        life.report["grant_cleanup"] = "not_created"
        if (life.private / "pool-grants.json").exists():
            try:
                life.fixture("cleanup")
                grants = json.loads((life.private / "browser" / "sql-pool-grant-cleanup.json").read_text())
                require(grants["run_id"] == life.run_id and all(grant["read_denied"] for grant in grants["grants"]),
                        "revocation evidence does not match this run")
                if life.report["result"] == "passed":
                    require(len(grants["grants"]) == 1, "exactly one local grant must be revoked")
                life.report["grant_cleanup"] = "passed"
            except Exception:
                life.report.update(result="failed", grant_cleanup="failed")
        # Reset cwd before the ownership adapter deletes the committed checkout.
        CORE.ROOT = ROOT
        failures = life.cleanup()
        life.report["cleanup_failures"] = failures
        life.report["zero_residuals"] = not failures
        if failures:
            life.report["result"] = "failed"
        (life.artifacts / "report.json").write_text(json.dumps(life.report, indent=2) + "\n")
    return 0 if life.report["result"] == "passed" else 1


if __name__ == "__main__":
    def interrupted(signum, _frame):
        raise KeyboardInterrupt(signum)
    signal.signal(signal.SIGTERM, interrupted)
    raise SystemExit(main())
