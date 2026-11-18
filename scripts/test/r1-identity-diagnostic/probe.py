"""One-shot Hosted diagnosis; no product processes, Gateway, Redis or T4 dispatcher."""
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import subprocess as sp
import tempfile
import time

PRODUCT = "690754af4890f493e5ec3fe560e7a5d12ba60c9d"
HARNESS = "4bab7f6ea29a9ca3d163237cd0e3b8a355c85ed2"
IMAGE = "postgres:15@sha256:74e110c41804365e3915fcc09d5e7a1eff50161aaa94d5da0e58e0cd75ae509c"
repo = Path(__file__).resolve().parents[3]
report = {"product": PRODUCT, "fixture": HARNESS, "cases": {}, "stages": []}


def require(condition, check):
    if not condition:
        raise RuntimeError(check)


def cmd(args, **kwargs):
    return sp.run(args, capture_output=True, check=True, timeout=90, **kwargs).stdout.decode().strip()


def timed(name, args, env, cwd, budget):
    started = time.monotonic()
    child = sp.Popen(args, cwd=cwd, env=env, stdout=sp.DEVNULL, stderr=sp.PIPE, start_new_session=True)
    expired = False
    try:
        _, err = child.communicate(timeout=budget)
    except sp.TimeoutExpired:
        expired = True
        os.killpg(child.pid, signal.SIGKILL)
        _, err = child.communicate()
    # Only fixed diagnostic categories can leave the runner.
    clues = [word for word in ("context deadline exceeded", "User principals", "current migration",
             "create reserve tenant", "create consumer role", "observer operation failed",
             "permission denied", "no space left on device", "connection refused") if word.encode() in err]
    result = dict(stage=name, seconds=round(time.monotonic()-started, 3), budget=budget,
                  exit_code=child.returncode, timed_out=expired, stderr_categories=clues)
    report["stages"].append(result)
    print(json.dumps(result), flush=True)
    return result


def count_tables(sql):
    tables = ("principals", "users", "local_accounts", "tenants", "tenant_memberships", "roles",
              "role_assignments", "role_permissions", "access_tokens", "refresh_token_families",
              "oauth_clients", "audit_logs")
    return json.loads(sql("SELECT json_build_object(" + ",".join(
        f"'{t}', (select count(*) from system.{t})" for t in tables) + ")"))


def verify_complete(sql, path, case, expected, env, cwd, observer):
    data = json.loads(path.read_text())
    require(path.stat().st_mode & 0o777 == 0o600, "private identity mode")
    tenant = int(data["ADDP_ONLINE_TEST_TENANT_ID"])
    actor = int(data["ADDP_REDIS_HTTP_PREPARER_ID"])
    require(tenant > 1 and data["ADDP_ONLINE_TEST_RUN_ID"] == env["ADDP_ONLINE_TEST_RUN_ID"], "identity run binding")
    require(int(sql(f"select count(*) from system.tenants where id={tenant} and code='external-online'")) == 1,
            "identity tenant binding")
    users = sql("select normalized_username from system.local_accounts order by 1").splitlines()
    names = ["administrator", "consumer", "engine-provisioner", "reserve", "source-initializer"]
    require(users == ["external-online-"+n for n in names], "five user accounts")
    require(sql(f"select normalized_username from system.local_accounts where user_id={actor}") ==
            "external-online-administrator", "preparer actor binding")
    for key, user in (("TEST_USER", "consumer"), ("FIXTURE_SOURCE", "source-initializer"),
                      ("FIXTURE_ENGINE", "engine-provisioner")):
        digest = hashlib.sha256(data["ADDP_ONLINE_"+key+"_ACCESS_TOKEN"].encode()).hexdigest()
        actual = sql(f"""select l.normalized_username from system.access_tokens a
            join system.refresh_token_families f on f.id=a.family_id
            join system.local_accounts l on l.user_id=f.principal_id
            join system.tenant_memberships m on m.id=f.tenant_membership_id and m.principal_id=f.principal_id
            where a.token_hash='{digest}' and a.revoked_at is null and a.expires_at>now()
            and f.revoked_at is null and m.tenant_id={tenant} and m.status='active'""")
        require(actual == "external-online-"+user, "session binding "+user)
    bindings = sql("""select l.normalized_username||'/'||t.code||'/'||r.role_key
        from system.local_accounts l join system.role_assignments a on a.principal_id=l.user_id
        join system.roles r on r.id=a.role_id join system.tenants t on t.id=a.tenant_id
        where a.status='active' order by 1""").splitlines()
    require(bindings == sorted([
        "external-online-reserve/external-online-reserve/tenant.administrator",
        "external-online-administrator/external-online/tenant.administrator",
        "external-online-consumer/external-online/online.infra_redis_core",
        "external-online-source-initializer/external-online/online.external_online_source_initializer",
        "external-online-source-initializer/external-online/tenant.engine_access_delegation_administrator",
        "external-online-engine-provisioner/external-online/tenant.infrastructure_administrator",
    ]), "exact tenant role bindings")
    permissions = sql(f"""select distinct p.permission_key from system.permissions p
        join system.role_permissions rp on rp.permission_id=p.id join system.role_assignments a on a.role_id=rp.role_id
        join system.local_accounts l on l.user_id=a.principal_id where a.tenant_id={tenant} and a.status='active'
        and l.normalized_username='external-online-consumer' order by 1""").splitlines()
    require(sorted(expected)==permissions, "exact consumer permissions")
    require(timed("verify_oauth_account", [observer, "verify", path], env, cwd, 20)["exit_code"]==0,
            "OAuth account credential, membership and zero roles")
    case["complete"] = {"users":len(users), "tenant":tenant, "matched_user_tokens":3, "user_role_bindings":len(bindings),
                        "consumer_permissions":len(permissions), "zero_role_oauth_credential_verified":True}


def run_case(kind, root, cwd, fixture, observer, base, expected):
    case = report["cases"][kind] = {}
    name = "r1-id-"+os.environ["GITHUB_RUN_ID"]+"-"+os.environ["GITHUB_RUN_ATTEMPT"]+"-"+kind
    label = "io.addp.r1-identity="+name
    volume = name+"-pgdata"
    network = name+"-net"
    output = root/(kind+".json")
    env = dict(base, POSTGRES_PASSWORD=secrets.token_hex(24), POSTGRES_USER="postgres", POSTGRES_DB="addp_online",
               POSTGRES_HOST="127.0.0.1", ADDP_ONLINE_HOSTED="1", ADDP_ONLINE_TEST_RUN_ID=name)
    args = [fixture, "--suite", "infra-redis-core", "--output", output]
    def listed(resource):
        return cmd(["docker", *({"container":["ps","-aq"]}.get(resource,[resource,"ls","-q"])),
                    "--filter", "label="+label]).splitlines()
    require(all(not listed(r) for r in ("container","volume","network")), "fresh diagnostic scope")
    try:
        cmd(["docker","volume","create","--label",label,volume])
        cmd(["docker","network","create","--label",label,network])
        cmd(["docker","run","-d","--name",name,"--label",label,"--network",network,"-p","127.0.0.1::5432",
             "-v",volume+":/var/lib/postgresql/data","-e","POSTGRES_DB","-e","POSTGRES_PASSWORD",
             "-e","POSTGRES_INITDB_ARGS=--auth-host=scram-sha-256",IMAGE],env=env)
        env["POSTGRES_PORT"] = cmd(["docker","port",name,"5432/tcp"]).rsplit(":",1)[1]
        pg = ["docker","exec","-i","-e","PGPASSWORD",name,"psql","-XqwAt","-h","127.0.0.1",
              "-U","postgres","-d","addp_online","-v","ON_ERROR_STOP=1"]
        pg_env = dict(env,PGPASSWORD=env["POSTGRES_PASSWORD"])
        def sql(statement):
            return cmd(pg,env=pg_env,input=statement.encode()+b"\n")
        deadline=time.monotonic()+60
        while True:
            try:
                require(sql("select current_database()")=="addp_online","actual DB")
                break
            except sp.CalledProcessError:
                require(time.monotonic()<deadline,"PG readiness")
                time.sleep(.2)
        require(timed(kind+"_migration",[observer,"migrate"],env,cwd,180)["exit_code"]==0,"migration")
        before=count_tables(sql)
        require(before["users"]==0,"fresh pre-fixture users")
        if kind=="normal":
            outcome=timed("first_execution",args,env,cwd,180)
        else:
            barrier = sp.Popen(pg,stdin=sp.PIPE, stdout=sp.DEVNULL, stderr=sp.DEVNULL,env=pg_env)
            try:
                barrier.stdin.write(b"SET application_name='r1-identity-barrier'; BEGIN; LOCK TABLE system.tenants IN SHARE MODE;\n")
                barrier.stdin.flush()
                held = """select count(*) from pg_locks l join pg_stat_activity a using(pid)
                    where a.application_name='r1-identity-barrier' and l.relation='system.tenants'::regclass
                    and l.granted and l.mode='ShareLock'"""
                deadline=time.monotonic()+15
                while sql(held)!="1":
                    require(time.monotonic()<deadline,"waiting for barrier")
                    time.sleep(.1)
                with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
                    future=pool.submit(timed,"execution_with_held_tenant_lock",args,env,cwd,12)
                    while not future.done():
                        waiting=sql("""select count(*) from pg_stat_activity a where a.wait_event_type='Lock'
                            and exists (select 1 from pg_stat_activity b where b.application_name='r1-identity-barrier'
                            and b.pid=any(pg_blocking_pids(a.pid)))""")
                        if waiting=="1":
                            case["observed_blocked_writer"]=True
                            break
                        time.sleep(.1)
                    outcome=future.result()
            finally:
                barrier.stdin.close()
                barrier.wait(timeout=10)
        after=count_tables(sql)
        case["delta"]={k:after[k]-v for k,v in before.items()}
        case["identity_file"]=output.exists()
        if kind=="normal":
            require(outcome["exit_code"]==0 and not outcome["timed_out"],"normal first execution")
            verify_complete(sql,output,case,expected,env,cwd,observer)
        else:
            require(outcome["timed_out"] and outcome["exit_code"]==-signal.SIGKILL,"observed external timeout")
            require(case.get("observed_blocked_writer"),"blocked writer evidence")
            require(sql("select normalized_username from system.local_accounts")=="external-online-reserve",
                    "committed reserve account")
            require(case["delta"]["tenants"]==0 and not output.exists(),"partial execution boundary")
        case["passed"]=True
    finally:
        output.unlink(missing_ok=True)
        for resource in ("container","volume","network"):
            for owned in listed(resource):
                cmd(["docker",resource,"rm",*(["-f"] if resource=="container" else []),owned])
        case["residue"]={resource:len(listed(resource)) for resource in ("container","volume","network")}
        case["residue"]["identity_files"]=int(output.exists())
        require(not any(case["residue"].values()),"diagnostic cleanup")


def main():
    require(os.environ.get("GITHUB_ACTIONS")=="true" and os.environ.get("RUNNER_ENVIRONMENT")=="github-hosted"
            and os.environ.get("RUNNER_OS")=="Linux" and os.environ.get("GITHUB_EVENT_NAME")=="workflow_dispatch",
            "real dedicated hosted job required")
    tmp=Path(os.environ["RUNNER_TEMP"])
    archive=tmp/"r1-identity-report.json"
    private=tempfile.TemporaryDirectory(prefix="r1-id-diag-",dir=tmp)
    root=Path(private.name)
    try:
        checkout=root/"pinned"
        cmd(["git","clone","--shared","--no-checkout",repo,checkout])
        cmd(["git","checkout","--detach",PRODUCT],cwd=checkout)
        require(cmd(["git","rev-parse","HEAD"],cwd=checkout)==PRODUCT,"fixed product")
        def harness(file):
            return sp.check_output(["git","show",HARNESS+":scripts/test/"+file],cwd=repo)
        cmd(["git","apply","--unidiff-zero","-"],cwd=checkout,input=harness("redis-core-identity.patch"))
        cwd=checkout/"system/backend"
        (cwd/"cmd/online-test-fixture/redis_core_fixture.go").write_bytes(harness("redis-core-identity.go"))
        expected=json.loads(harness("redis-core-http-contract.json"))["permissions"]
        fixture=root/"online-test-fixture"
        observer=root/"observer"
        env=dict(os.environ,GOWORK="off",GOCACHE=str(root/"go-cache"),GOMODCACHE=str(root/"modules"))
        report["go_version"]=cmd(["go","version"],cwd=cwd,env=env)
        require(timed("cold_fixture_build",["go","build","-o",fixture,"./cmd/online-test-fixture"],env,cwd,900)["exit_code"]==0,"cold build")
        dest=cwd/"cmd/r1-identity-observer"
        dest.mkdir()
        shutil.copyfile(Path(__file__).with_name("observer.go"),dest/"main.go")
        require(timed("observer_build",["go","build","-o",observer,"./cmd/r1-identity-observer"],env,cwd,180)["exit_code"]==0,"observer build")
        cmd(["docker","pull",IMAGE])
        for kind in ("normal","timeout"):
            try:
                run_case(kind,root,cwd,fixture,observer,env,expected)
            except (RuntimeError,sp.SubprocessError,KeyError) as error:
                report["cases"][kind]["failure"]=str(error) if isinstance(error,RuntimeError) else type(error).__name__
        require(all(c.get("passed") and not any(c["residue"].values()) for c in report["cases"].values()),"identity cases")
        report["result"]="passed"
    finally:
        report.setdefault("result","failed")
        try:
            private.cleanup()
        finally:
            report["private_directory_removed"]=not root.exists()
            archive.write_text(json.dumps(report,indent=2)+"\n")


if __name__=="__main__":
    os.umask(0o077)
    main()
