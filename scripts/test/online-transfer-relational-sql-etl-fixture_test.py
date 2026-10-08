import json
import os
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).parents[2] / 'business/scripts/online-transfer-relational-sql-etl-fixture.sh'


class OnlineTransferRelationalSQLETLFixtureTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='addp-online-transfer-sql-etl-')
        self.root = Path(self.temporary.name)
        self.bin = self.root / 'bin'
        self.postgres_state = self.root / 'postgres-running'
        self.mongodb_state = self.root / 'mongodb-running'
        self.schema_state = self.root / 'evolved-schema'
        self.log = self.root / 'fixture.log'
        self.secrets = self.root / 'addp-online-secret-test'
        (self.root / 'business/scripts').mkdir(parents=True)
        self.bin.mkdir()
        self.secrets.mkdir(mode=0o700)
        shutil.copy2(SCRIPT, self.root / 'business/scripts/online-transfer-relational-sql-etl-fixture.sh')
        self._executable('uname', '#!/bin/bash\n[ "$1" != -s ] || { echo "${ADDP_TEST_OS:-Linux}"; exit; }\necho x86_64\n')
        self._executable('sleep', '#!/bin/bash\nexit 0\n')
        self._executable('docker', '''#!/bin/bash
printf 'docker:%s:%s\n' "$1" "${2:-}" >> "$ADDP_TEST_FIXTURE_LOG"
state=$ADDP_TEST_POSTGRES_STATE
case "$*" in *addp-transfer-mongodb-online-disposable*) state=$ADDP_TEST_MONGODB_STATE ;; esac
case "$1" in
  container)
    [ -f "$state" ]
    ;;
  inspect)
    [ -f "$state" ] || exit 1
    case "$*" in
      *com.addp.online-fixture*) cat "$state" ;;
      *"MongoDB state:"*) echo 'MongoDB state: running=false exit=14 oom=false' ;;
      *) echo true ;;
    esac
    ;;
  run)
    printf '%s\n' "$*" >> "$ADDP_TEST_FIXTURE_LOG"
    echo transfer-relational-sql-etl > "$state"
    ;;
  ps)
    [ "${ADDP_TEST_VERIFY_FAIL:-0}" != 1 ] || exit 1
    [ ! -f "$state" ] || echo fixture-container
    ;;
  rm)
    if [ "$state" = "$ADDP_TEST_MONGODB_STATE" ] && [ "${ADDP_TEST_MONGODB_REMOVE_FAIL:-0}" = 1 ]; then exit 0; fi
    [ "${ADDP_TEST_REMOVE_FAIL:-0}" = 1 ] || rm -f "$state"
    ;;
  logs)
    echo 'WiredTiger error: SECRET_MARKER'
    ;;
  exec)
    [ -f "$state" ] || exit 1
    case " $* " in
      *pg_isready*) exit 0 ;;
      *mongosh*) input=$(cat)
        printf 'mongodb-stdin:%s\n' "$input" >> "$ADDP_TEST_FIXTURE_LOG"
        if [[ "$input" == *"ping: 1"* ]] && [ "${ADDP_TEST_MONGODB_READY_FAIL:-0}" = 1 ]; then
          echo 'MongoNetworkError: ECONNREFUSED SECRET_MARKER' >&2; exit 1
        fi
        if [[ "$input" == *"createUser"* ]] && [ "${ADDP_TEST_MONGODB_SEED_FAIL:-0}" = 1 ]; then
          echo SECRET_MARKER >&2; exit 1
        fi
        [[ "$input" != *"countDocuments"* ]] || echo 3
        exit 0 ;;
      *" -Atc "*|*" -c "*) printf 'query:%s\n' "$*" >> "$ADDP_TEST_FIXTURE_LOG" ;;
      *) input=$(cat)
         printf 'stdin:%s\n' "$input" >> "$ADDP_TEST_FIXTURE_LOG"
         if [[ "$input" == *"ALTER TABLE"* ]]; then
           [ "${ADDP_TEST_EVOLVE_FAIL:-0}" != 1 ] || exit 1
           touch "$ADDP_TEST_SCHEMA_STATE"
         fi
         if [[ "$input" == *"CREATE ROLE"* ]] && [ "${ADDP_TEST_SEED_FAIL:-0}" = 1 ]; then
           echo "SECRET_MARKER: $*" >&2
           exit 1
         fi ;;
    esac
    case " $* " in
      *"information_schema.columns"*"addp_wide"*) echo "${ADDP_TEST_WIDE_COLUMNS:-500}" ;;
      *"differences"*) echo "${ADDP_TEST_WIDE_DIFFERENCES:-0}" ;;
      *"string_agg(column_name"*"addp_online_transfer_multi_query_target"*) echo 'id,combined_label,combined_amount' ;;
      *"addp_online_transfer_multi_query_target"*) echo "${ADDP_TEST_MULTI_VALUES:-3|north:active|600.50;3|north:active|600.50;4|east:active|801.50;4|east:active|801.50}" ;;
      *"string_agg(column_name"*"addp_online_transfer_mongodb_ods"*) echo 'activity_id,activity_status,activity_date_raw,activity_level_raw,leader_person_id,leader_nickname_snapshot' ;;
      *"addp_online_transfer_mongodb_ods"*) echo "${ADDP_TEST_MONGODB_VALUES:-activity-1|active|2026-01-01|easy|person-1|Alice;activity-2|inactive|2026-01-02|moderate|person-2|Bob;activity-3|active|2026-01-03|hard|person-3|Carol}" ;;

      *"string_agg(table_name"*) echo "${ADDP_TEST_DATE_TYPES:-addp_online_transfer_dim_activity:date,addp_online_transfer_dwd_activity:timestamp without time zone}" ;;
      *"string_agg(column_name ||"*)
        if [ -f "$ADDP_TEST_SCHEMA_STATE" ]; then echo 'changed'; else echo 'activity_id:text,activity_date:date,person_nickname:text,intensity:text'; fi ;;
      *"string_agg(column_name"*"addp_online_transfer_dwd_activity"*) echo "${ADDP_TEST_DWD_COLUMNS:-activity_id,activity_date,person_display_name,intensity}" ;;
      *"addp_online_transfer_dim_activity"*) echo "${ADDP_TEST_DIM_VALUES:-activity-1|2026-01-01;activity-2|2026-01-02;activity-3|2026-01-03}" ;;
      *"addp_online_transfer_dwd_activity"*)
        if [ -f "$ADDP_TEST_SCHEMA_STATE" ]; then
          echo "${ADDP_TEST_DWD_VALUES:-activity-1|2026-01-01 00:00:00|Alice|EASY;activity-3|2026-01-03 00:00:00|Carol|HARD}"
        else
          echo "${ADDP_TEST_DWD_VALUES:-activity-1|2026-01-01|Alice|EASY;activity-3|2026-01-03|Carol|HARD}"
        fi ;;
      *"numeric_precision"*) echo '8|2' ;;
      *"generated_label"*) echo "${ADDP_TEST_NATIVE_VALUES:-5|1411.50|3|2|5}" ;;
      *"WHERE area"*) echo '5|1411.50|3|2' ;;
      *"CONCAT_WS"*) echo '2|3|4|701.00' ;;
      *"string_agg(column_name"*) echo 'id,region,amount' ;;
      *"COUNT(*) FROM public.addp_online_transfer_sql_etl_source"*) echo 5 ;;
    esac
    ;;
esac
''')
        self.environment = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ['PATH'],
                                GITHUB_ACTIONS='true', RUNNER_OS='Linux', RUNNER_TEMP=str(self.root),
                                ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1',
                                ADDP_ONLINE_SECRET_DIR=str(self.secrets),
                                ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(self.secrets / 'transfer-engine.json'),
                                ADDP_ONLINE_FIXTURE_MONGODB_ENGINE_DESCRIPTOR_FILE=str(self.secrets / 'transfer-mongodb-engine.json'),
                                ADDP_TEST_MONGODB_STATE=str(self.mongodb_state), ADDP_TEST_SCHEMA_STATE=str(self.schema_state),
                                ADDP_TEST_POSTGRES_STATE=str(self.postgres_state), ADDP_TEST_FIXTURE_LOG=str(self.log))

    def tearDown(self):
        self.temporary.cleanup()

    def _executable(self, name, content):
        path = self.bin / name
        path.write_text(content)
        path.chmod(0o755)

    def run_fixture(self, action, **overrides):
        environment = dict(self.environment)
        environment.update(overrides)
        return subprocess.run(['bash', 'business/scripts/online-transfer-relational-sql-etl-fixture.sh', action],
                              cwd=self.root, env=environment, capture_output=True, text=True, timeout=20)

    def test_rejects_incomplete_wide_schema_or_rows(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        for override in ({'ADDP_TEST_WIDE_COLUMNS': '499'}, {'ADDP_TEST_WIDE_DIFFERENCES': '1'}):
            result = self.run_fixture('verify', **override)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('wide target', result.stderr)
        self.assertEqual(self.run_fixture('stop').returncode, 0)

    def test_owns_projection_filter_verification_and_cleanup(self):
        for action in ('start', 'status', 'evolve', 'verify', 'stop'):
            result = self.run_fixture(action)
            self.assertEqual(result.returncode, 0, result.stderr)
        commands = self.log.read_text()
        self.assertIn('CREATE TABLE public.addp_online_transfer_sql_etl_source', commands)
        self.assertIn("(3, 'north', 'active', 300.25, 'hidden-3')", commands)
        self.assertIn('GRANT SELECT ON public.addp_online_transfer_sql_etl_source, public.addp_wide_source', commands)
        self.assertIn('generate_series(0,499)', commands)
        self.assertIn('EXCEPT ALL', commands)
        self.assertIn('GRANT USAGE, CREATE ON SCHEMA public TO transfer_writer', commands)
        self.assertNotIn('GRANT ALL', commands)
        self.assertIn('--tmpfs /var/lib/postgresql/data', commands)
        self.assertNotIn('business-postgres', commands)
        self.assertFalse(self.postgres_state.exists())
        self.assertFalse(self.mongodb_state.exists())
        self.assertIn('--tmpfs /data/db --tmpfs /data/configdb', commands)
        self.assertIn('mongo:7.0', commands)
        self.assertIn("roles: [{role: 'read', db: 'transfer_fixture'}]", commands)
        self.assertIn("nickName: 'Alice'", commands)
        mongo_descriptor = self.secrets / 'transfer-mongodb-engine.json'
        self.assertEqual(stat.S_IMODE(mongo_descriptor.stat().st_mode), 0o600)
        mongo = json.loads(mongo_descriptor.read_text())
        self.assertEqual(mongo['engine_type'], 'mongodb')
        self.assertEqual(mongo['connection_info']['user'], 'transfer_reader')
        self.assertEqual(mongo['connection_info']['auth_source'], 'transfer_fixture')
        self.assertNotIn(mongo['connection_info']['password'], commands)
        descriptor = self.secrets / 'transfer-engine.json'
        self.assertEqual(stat.S_IMODE(descriptor.stat().st_mode), 0o600)
        engine = json.loads(descriptor.read_text())
        self.assertEqual(engine['connection_info']['user'], 'transfer_writer')
        self.assertEqual(engine['connection_info']['database'], 'transfer_fixture')
        self.assertEqual(engine['connection_info']['host'], '127.0.0.1')
        self.assertNotIn(engine['connection_info']['password'], commands)

    def test_requires_github_hosted_linux_before_docker(self):
        for flags in ({'ADDP_ONLINE_HOST': '0'}, {'ADDP_ONLINE_HOSTED': '0'},
                      {'GITHUB_ACTIONS': 'false'}, {'RUNNER_OS': 'macOS'},
                      {'ADDP_ONLINE_OWNER_MANAGED': '1'}, {'ADDP_TEST_OS': 'Darwin'}):
            result = self.run_fixture('start', **flags)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('GitHub Hosted Linux', result.stderr)
            self.assertFalse(self.log.exists())

    def test_refuses_existing_source_and_foreign_ownership(self):
        self.postgres_state.write_text('foreign-owner')
        self.assertNotEqual(self.run_fixture('start').returncode, 0)
        for action in ('stop', 'evolve', 'verify', 'status'):
            result = self.run_fixture(action)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('ownership mismatch', result.stderr)
            self.assertTrue(self.postgres_state.exists())

    def test_rejects_incorrect_multi_source_query_rows(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        result = self.run_fixture('verify', ADDP_TEST_MULTI_VALUES='3|north:active|600.50')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('multi-source CTE/JOIN/UNION rows differ', result.stderr)

    def test_rejects_native_rows_that_do_not_prove_replace_and_generation(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        result = self.run_fixture('verify', ADDP_TEST_NATIVE_VALUES='5|1411.50|5|0|5')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('native target does not prove', result.stderr)

    def test_refuses_foreign_mongodb_before_creating_or_removing_any_fixture(self):
        self.mongodb_state.write_text('foreign-owner')
        self.assertNotEqual(self.run_fixture('start').returncode, 0)
        self.assertFalse(self.postgres_state.exists())
        self.postgres_state.write_text('transfer-relational-sql-etl')
        result = self.run_fixture('stop')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('ownership mismatch', result.stderr)
        self.assertTrue(self.postgres_state.exists())
        self.assertTrue(self.mongodb_state.exists())

    def test_rejects_chain_duplicate_append_or_wrong_derived_values(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        self.assertIn('GRANT SELECT, INSERT, DELETE ON public.addp_online_transfer_dim_activity, public.addp_online_transfer_dwd_activity', self.log.read_text())
        for flags, message in (({'ADDP_TEST_DIM_VALUES': 'activity-1|2026-01-01'}, 'DIM rows differ'),
                               ({'ADDP_TEST_DWD_VALUES': 'activity-1|2026-01-01|Alice|easy'}, 'DWD rows differ')):
            result = self.run_fixture('verify', **flags)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(message, result.stderr)

    def test_physical_evolution_is_atomic_once_and_retains_least_privilege(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        self.assertEqual(self.run_fixture('evolve').returncode, 0)
        commands = self.log.read_text()
        self.assertIn('BEGIN;\nALTER TABLE public.addp_online_transfer_dwd_activity RENAME COLUMN person_nickname TO person_display_name;', commands)
        self.assertIn('TYPE timestamp without time zone USING activity_date::timestamp;\nCOMMIT;', commands)
        self.assertNotIn('GRANT ALTER', commands)
        self.assertNotIn('GRANT ALL', commands)
        self.assertNotEqual(self.run_fixture('evolve').returncode, 0)
        self.assertEqual(self.log.read_text().count('RENAME COLUMN person_nickname'), 1)
        for flags, message in (({'ADDP_TEST_DATE_TYPES': 'addp_online_transfer_dim_activity:date,addp_online_transfer_dwd_activity:date'}, 'expected physical types'),
                               ({'ADDP_TEST_DWD_COLUMNS': 'activity_id,activity_date,person_nickname,intensity'}, 'evolved DWD columns')):
            result = self.run_fixture('verify', **flags)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(message, result.stderr)

    def test_evolution_failure_and_bad_previous_rows_cannot_claim_success(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        for flags in ({'ADDP_TEST_EVOLVE_FAIL': '1'}, {'ADDP_TEST_DWD_VALUES': 'wrong-old-rows'}):
            result = self.run_fixture('evolve', **flags)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn('physical schema is evolved', result.stdout)
            self.assertFalse(self.schema_state.exists())

    def test_rejects_incorrect_nested_ods_values(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        result = self.run_fixture('verify', ADDP_TEST_MONGODB_VALUES='activity-1|active|wrong-date|easy|person-1|Alice')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('exact nested mapping', result.stderr)

    def test_partial_mongodb_initialization_is_redacted_and_cleans_both_containers(self):
        result = self.run_fixture('start', ADDP_TEST_MONGODB_SEED_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('SECRET_MARKER', result.stdout + result.stderr)
        self.assertEqual(self.run_fixture('stop').returncode, 0)
        self.assertFalse(self.postgres_state.exists())
        self.assertFalse(self.mongodb_state.exists())

    def test_readiness_failure_reports_only_safe_categories_and_cleans_both_containers(self):
        result = self.run_fixture('start', ADDP_TEST_MONGODB_READY_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('MongoDB state: running=false exit=14 oom=false', result.stderr)
        self.assertIn('mongodb-readiness-error.log: connection-refused', result.stderr)
        self.assertIn('mongodb-startup.log: storage-error', result.stderr)
        self.assertNotIn('SECRET_MARKER', result.stdout + result.stderr)
        for name in ('mongodb-readiness-error.log', 'mongodb-startup.log'):
            path = self.secrets / name
            self.assertIn('SECRET_MARKER', path.read_text())
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertEqual(self.run_fixture('stop').returncode, 0)
        self.assertFalse(self.postgres_state.exists())
        self.assertFalse(self.mongodb_state.exists())

    def test_mongodb_residual_cannot_be_hidden_by_successful_postgresql_cleanup(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        result = self.run_fixture('stop', ADDP_TEST_MONGODB_REMOVE_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('container remains after cleanup: addp-transfer-mongodb-online-disposable', result.stderr)
        self.assertFalse(self.postgres_state.exists())
        self.assertTrue(self.mongodb_state.exists())

    def test_cleanup_fails_if_container_remains(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        result = self.run_fixture('stop', ADDP_TEST_REMOVE_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('container remains', result.stderr)
        self.assertTrue(self.postgres_state.exists())

    def test_cleanup_cannot_claim_zero_residuals_when_docker_verification_fails(self):
        self.assertEqual(self.run_fixture('start').returncode, 0)
        result = self.run_fixture('stop', ADDP_TEST_VERIFY_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('cannot verify container cleanup', result.stderr)

    def test_seed_error_keeps_credentials_out_of_output(self):
        result = self.run_fixture('start', ADDP_TEST_SEED_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('SECRET_MARKER', result.stdout + result.stderr)
        self.assertEqual(self.run_fixture('stop').returncode, 0)
        self.assertFalse(self.postgres_state.exists())


if __name__ == '__main__':
    unittest.main()
