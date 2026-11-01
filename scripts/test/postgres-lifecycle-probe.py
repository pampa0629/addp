"""Real product lifecycle in the standard gate's already-owned PG15 only."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import ssl
import subprocess
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[2]


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


module = load('postgres_lifecycle_under_test', ROOT / 'scripts/infra/postgres-observation/lifecycle.py')
legacy = load('owned_postgres_probe', ROOT / 'scripts/test/postgres-observation-probe.py')
probe = legacy.Probe()  # Validates the disposable project, Compose path and PG.
work = Path(os.environ['METRICS_T2_WORK']).resolve()
docker = module.Docker(os.environ)
tls = work / 'lifecycle-tls'
tls.mkdir(mode=0o700, exist_ok=True)
for name, original in [('ca.crt', work/'deployment/source-ca.crt'), ('server.crt', work/'source-server.crt'), ('server.key', work/'source-server.key')]:
    shutil.copyfile(original, tls/name)
context = ssl.create_default_context(cafile=str(tls/'ca.crt'))
context.load_cert_chain(str(work/'deployment/collector.crt'), str(work/'deployment/collector.key'))
# Reuse this gate's already assigned publication intent, without auto-avoidance.
port = subprocess.check_output(probe.compose + ['port', 'postgres-metrics', '9443'], text=True).strip().rsplit(':', 1)[1]
subprocess.run(probe.compose + ['rm', '-sf', 'postgres-metrics-web', 'postgres-metrics'],
               check=True, capture_output=True, text=True, timeout=20)


def sample():
    deadline = time.monotonic()+20
    while True:
        try:
            requested = time.time()
            with urllib.request.urlopen('https://127.0.0.1:'+port+'/metrics', context=context, timeout=6) as response:
                value = response.read(65537).decode()
            assert len(value) <= 65536 and 'addp_postgresql_sample_time_seconds ' in value
            sampled = float(next(line.split()[-1] for line in value.splitlines()
                                 if line.startswith('addp_postgresql_sample_time_seconds ')))
            assert requested-1 <= sampled <= time.time()+1, 'Source returned an old database snapshot'
            return value
        except Exception:
            if time.monotonic() >= deadline:
                raise AssertionError('Product PostgreSQL source never provided a fresh snapshot') from None
            time.sleep(.2)


try:
    for scope in (['business'] if probe.auth_directory else ['infra', 'business']):
        original = probe.read_hba()
        state = work / 'lifecycle' / scope

        def database():
            return module.Database(docker, probe.project, ROOT/'scripts/test', scope, 'addp', maintenance='addp_test',
                                   authentication=probe.auth_directory)

        manager = module.SourceLifecycle(docker, scope, state, database, project=probe.project)
        environment = dict(ADDP_POSTGRES_METRICS_LISTEN='127.0.0.1:'+port, ADDP_POSTGRES_METRICS_TLS_DIR=str(tls))
        user = 'addp_'+scope+'_observer'
        try:
            manager.up(environment)
            password = (state/'password').read_text()
            assert len(password) == 64 and (state/'password').stat().st_mode & 0o777 == 0o600
            assert password not in sample()
            probe.password(user, password)
            assert probe.sql('SELECT current_user;', user) == user
            recovery = module.SourceLifecycle(docker, scope, state, lambda: module.ObservationDatabase(
                docker, probe.project, ROOT/'scripts/test', scope, maintenance='addp_test'), project=probe.project)
            def account_state():
                return probe.sql(f"SELECT json_build_object('canlogin',r.rolcanlogin,'inherit',r.rolinherit,'limit',r.rolconnlimit,'config',r.rolconfig, 'password',md5(a.rolpassword)) FROM pg_catalog.pg_authid a JOIN pg_catalog.pg_roles r USING(oid) WHERE a.rolname='{user}';")
            account_before = account_state()
            hba_before = probe.read_hba()
            ids_before = manager.source_containers()
            files_before = {str(p.relative_to(state)):(p.read_bytes(),p.stat().st_ino,p.stat().st_mtime_ns)
                            for p in state.rglob('*') if p.is_file()}
            assert recovery.recover(environment).startswith('healthy')
            assert manager.source_containers() == ids_before, 'healthy recovery recreated sources'
            assert all((state/name).read_bytes()==facts[0] and (state/name).stat().st_ino==facts[1]
                       and (state/name).stat().st_mtime_ns==facts[2] for name,facts in files_before.items())
            manager.stop_sources()
            assert recovery.recover(environment).startswith('source recovered')
            assert password not in sample()
            assert account_state()==account_before and probe.read_hba()==hba_before, 'recovery changed account or HBA'
            # An existing login with expanded permissions must not be repaired or reused.
            probe.sql(f'GRANT pg_read_all_stats TO {user};')
            try:
                try:
                    recovery.recover(environment)
                except module.LifecycleError as error:
                    assert 'installation drift' in str(error), str(error)
                else:
                    raise AssertionError('Recovery accepted observer membership drift')
                assert probe.sql(f"SELECT pg_has_role('{user}','pg_read_all_stats','MEMBER');")=='t'
            finally:
                probe.sql(f'REVOKE pg_read_all_stats FROM {user};')
            # Restart only this disposable database; the same container ID must
            # receive a new startup binding without any account/HBA installation.
            before = database().facts
            subprocess.run(probe.compose+['restart','postgres'],check=True,capture_output=True,text=True,timeout=30)
            deadline=time.monotonic()+20
            while True:
                ready=docker.run(['exec',before['id'],'pg_isready','-U','postgres'],check=False)
                if ready.returncode==0:break
                assert time.monotonic()<deadline, 'Disposable PostgreSQL did not return ready'
                time.sleep(.1)
            after=database().facts
            assert before['id']==after['id'] and before['started_at']!=after['started_at']
            assert recovery.recover(environment).startswith('source recovered')
            assert password not in sample()
            assert account_state()==account_before and probe.read_hba()==hba_before, 'restart recovery changed account or HBA'
            assert all(docker.fact(identity)['labels']['io.addp.postgres-metrics.database-started-at']==after['started_at']
                       for _,identity in manager.source_containers())
            print('PostgreSQL recovery T2: '+scope+' healthy no-op, stopped source, same-container restart, current binding and unchanged account/HBA passed', flush=True)
            manager.up(environment)
            assert (state/'password').read_text() == password, 'up silently rotated the credential'
            assert password not in sample()
            try:
                manager.up(dict(environment, ADDP_POSTGRES_METRICS_TLS_DIR=str(work/'absent-tls')))
            except module.LifecycleError as error:
                assert 'TLS requires' in str(error), str(error)
            else:
                raise AssertionError('Invalid TLS must not keep the old source active')
            assert probe.sql(f"SELECT rolcanlogin FROM pg_roles WHERE rolname='{user}';") == 'f'
            assert not manager.source_containers()
            manager.up(environment)
            # Down does not need usable TLS inputs, does not remove history/DDL.
            manager.down()
            assert probe.sql(f"SELECT rolcanlogin FROM pg_roles WHERE rolname='{user}';") == 'f'
            assert not manager.source_containers(), 'Owned test project retained observation sources'
            try:
                recovery.recover(environment)
            except module.LifecycleError:
                pass
            else:
                raise AssertionError('Recovery re-enabled a deliberately disabled observer')
            assert not manager.source_containers()
            assert probe.sql(f"SELECT rolcanlogin FROM pg_roles WHERE rolname='{user}';")=='f'

            refused = subprocess.run(probe.command(user), input='SELECT 1;', capture_output=True, text=True, timeout=10)
            assert refused.returncode != 0 and 'not permitted to log in' in refused.stderr
            manager.up(environment)
            assert password not in sample()
            # An unrelated caller's live observation session is not killed.
            peer = subprocess.Popen(probe.command(user), stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                    stderr=subprocess.DEVNULL, text=True)
            peer_name = 'addp-lifecycle-fixture-'+scope
            peer.stdin.write("SET application_name='"+peer_name+"'; SET statement_timeout='30s'; SELECT pg_sleep(25);\n")
            peer.stdin.flush()
            deadline = time.monotonic()+4
            peer_pid = ''
            while not peer_pid:
                peer_pid = probe.sql(f"SELECT pid FROM pg_stat_activity WHERE usename='{user}' AND application_name='{peer_name}';")
                assert time.monotonic() < deadline
                time.sleep(.05)
            try:
                try:
                    manager.up(environment)
                except module.LifecycleError as error:
                    assert str(error).startswith('Existing observation sessions'), str(error)
                else:
                    raise AssertionError('Existing observer session must refuse activation')
                assert peer.poll() is None, 'Lifecycle killed an unknown database session'
                assert probe.sql(f"SELECT rolcanlogin FROM pg_roles WHERE rolname='{user}';") == 'f'
            finally:
                # Killing the local Docker client does not stop its server-side
                # backend. Only this fixture's recorded session may be cleaned.
                assert peer_pid.isdecimal()
                probe.sql(f"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid={peer_pid} AND usename='{user}' AND application_name='{peer_name}';")
                peer.terminate()
                peer.communicate(timeout=5)
            manager.up(environment)
            assert password not in sample()
            manager.down()
            (state/'password').unlink()
            try:
                manager.up(environment)
            except module.LifecycleError as error:
                assert 'no private credential' in str(error), str(error)
            else:
                raise AssertionError('Lost credentials must not silently reset an existing account')
            assert probe.sql(f"SELECT rolcanlogin FROM pg_roles WHERE rolname='{user}';") == 'f'
            print('PostgreSQL lifecycle T2: '+scope+' install, mTLS, healthy no-op, stopped-source and same-container restart recovery, unchanged account/HBA, permission-drift and disabled-account refusal, idempotence, NOLOGIN, reenable, session refusal and lost-secret rejection passed', flush=True)
        finally:
            manager.down()
            current = probe.read_hba()
            if current != original:
                database().write_hba(current, original)
            if probe.sql(f"SELECT count(*) FROM pg_roles WHERE rolname='{user}';") == '1':
                probe.sql(f'DROP SCHEMA IF EXISTS addp_observation CASCADE; REVOKE CONNECT ON DATABASE addp_test FROM {user}; DROP ROLE {user}; DROP ROLE addp_observation_owner;')
finally:
    subprocess.run(probe.compose + ['exec', '-T', 'postgres', 'rm', '-f', probe.pgpass], capture_output=True, check=True, timeout=10)
