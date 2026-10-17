import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


ROOT = Path(__file__).resolve().parents[2]
TABLES = load('table_consumer_test', Path(__file__).with_name('spark-table-consumer-online.py'))
ORACLE = load('table_oracle_test', ROOT / 'business/scripts/online-spark-table-oracle.py')
HDFS = load('table_hdfs_test', Path(__file__).with_name('hdfs-spark-consumer-flow-online.py'))
FIELDS = [{'name': 'id', 'type': 'bigint'}, {'name': 'label', 'type': 'string'},
          {'name': 'value', 'type': 'decimal', 'precision': 38, 'scale': 18}]
TABLE = 'spark_boundary_' + 'a' * 16


class SparkTableConsumerTest(unittest.TestCase):
    def test_boundary_assertions_reject_rounding_whitespace_null_and_missing_rows(self):
        for mutation in ('bigint', 'decimal', 'space', 'null', 'long', 'missing', 'extra', ''):
            rows = json.loads(json.dumps(TABLES.ROWS))
            if mutation == 'bigint': rows[-1]['id'] = '9007199254740992'
            if mutation == 'decimal': rows[0]['value'] = '-100.12345678901235'
            if mutation == 'space': rows[2]['label'] = rows[2]['label'].rstrip()
            if mutation == 'null': rows[1]['label'] = None
            if mutation == 'long': rows[3]['label'] = 'x' * 99
            if mutation == 'missing': rows.pop()
            if mutation == 'extra': rows[0]['other'] = 1
            with self.subTest(mutation=mutation):
                if mutation:
                    with self.assertRaises(ValueError): TABLES.validate_rows(rows, manager=True)
                else: TABLES.validate_rows(rows[::-1], manager=True)
        rows = json.loads(json.dumps(TABLES.ROWS)); rows[1]['id'] = 1
        TABLES.validate_rows(rows)
        with self.assertRaisesRegex(ValueError, 'always'): TABLES.validate_rows(rows, manager=True)

    def test_meta_requires_canonical_types_and_decimal_precision_scale(self):
        for field, key, value in ((0, 'type', 'int'), (1, 'type', 'text'), (2, 'precision', 18), (2, 'scale', 0)):
            wrong = json.loads(json.dumps(FIELDS)); wrong[field][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError): TABLES.validate_fields(wrong)
        TABLES.validate_fields(FIELDS)

    def test_definition_uses_only_formal_locator_and_preserves_sample_literals(self):
        tasks = TABLES.definition(8, TABLE)['tasks']
        self.assertEqual(tasks[1]['params']['target_parent_locator'], 'addp://engine/8/path/spark_results?type=database')
        self.assertEqual(tasks[1]['params']['mode'], 'overwrite')
        query = tasks[0]['params']['query']
        self.assertEqual(query.count('DECIMAL(38,18)'), 5)
        self.assertIn("'尾随😀  '", query)
        self.assertIn('9007199254740993', query)
        self.assertFalse(any(key in json.dumps(tasks) for key in ('password', 'connection_info', 'jdbc')))

    def test_save_requires_auto_scan_and_precise_receipt_without_manual_target_scan(self):
        locator = 'addp://engine/8/path/spark_results/' + TABLE + '?type=table'
        receipt = {'status': 'success', 'rows': 5, 'target': 'spark_results.' + TABLE}
        base = {'outputs': {'save': {'resource': {'locator': locator, 'type': 'table', 'write_mode': 'replace'}}},
                'metadata': {'result': {'final_result': receipt, 'runtime_execution_id': 'runtime',
                    'produced_targets': [{'locator': locator, 'task_id': 'save'}],
                    'meta_scan_runs': [{'status': 'submitted', 'target_locator': locator, 'execution_id': 'scan'}]}}}
        for mutation in ('rows', 'output', 'scan', 'target', ''):
            execution = json.loads(json.dumps(base))
            if mutation == 'rows': execution['metadata']['result']['final_result']['rows'] = 4
            if mutation == 'output': execution['outputs']['save']['resource']['write_mode'] = 'create'
            if mutation == 'scan': execution['metadata']['result']['meta_scan_runs'] = []
            if mutation == 'target': execution['metadata']['result']['produced_targets'] = []
            client = Mock(); client.request.return_value = SimpleNamespace(payload={'status': 'success'})
            host = Mock()
            host.submit_and_wait.return_value = ('created', execution)
            host.support._object.side_effect = lambda payload, _: payload
            host.support.find_item.return_value = {'id': 17, 'attributes': {'type_info': {'table': {'fields': FIELDS}}}}
            with self.subTest(mutation=mutation), patch.dict(os.environ, ADDP_ONLINE_SPARK_RUNTIME_URL='http://runtime'):
                if mutation:
                    with self.assertRaises(ValueError): TABLES.save(host, client, 3, 4, 8, TABLE, float('inf'))
                else:
                    result = TABLES.save(host, client, 3, 4, 8, TABLE, float('inf'))
                    self.assertEqual(result['scan_execution_id'], 'scan')
                    self.assertEqual(client.request.call_args.args[:2], ('GET', '/api/v1/meta/executions/scan'))

    def test_cleanup_attempts_both_engines_and_cannot_pass_partial_failure(self):
        with tempfile.TemporaryDirectory() as temporary, patch.dict(os.environ, ADDP_ONLINE_ARTIFACT_DIR=temporary, ADDP_ONLINE_TEST_RUN_ID='run-tables'):
            for failure in ('mysql', 'doris', ''):
                def physical(engine, table, cleanup):
                    self.assertTrue(cleanup)
                    if engine == failure: raise ValueError('unverified')
                    return {'target_absent': True, 'staging_tables': 0}
                with patch.object(TABLES, 'physical', side_effect=physical) as query:
                    if failure:
                        with self.assertRaises(ValueError): TABLES.cleanup()
                    else: TABLES.cleanup()
                    self.assertEqual(query.call_count, 2)
                evidence = json.loads(Path(temporary, 'spark-table-cleanup.json').read_text())
                self.assertEqual(set(evidence), {'mysql', 'doris'} - {failure})

    def test_overwrite_requires_same_physical_and_meta_identity_and_exact_downstream_rows(self):
        report = {'principal_id': '42', 'tenant_id': 2, 'runtime_id': 3, 'cluster_id': 4}
        native = {'table_id': '123', 'ddl_sha256': 'original', 'rows': TABLES.ROWS, 'staging_tables': 0}
        created = {'execution_id': 'created', 'item_id': 7, 'preview_locator': 'preview', 'target_locator': 'target'}
        prepared = dict(report, run_id='run-tables', tables={engine: {'engine_id': identifier,
            'created': created, 'physical_before': native} for engine, identifier in (('mysql', 8), ('doris', 9))})
        final = {'type': 'spark_dataframe', 'preview_rows': TABLES.ROWS,
                 'schema': [{'name': 'id', 'type': 'bigint'}, {'name': 'label', 'type': 'string'}, {'name': 'value', 'type': 'decimal(38,18)'}]}
        for failure in ('native-id', 'ddl', 'stage', 'item-id', 'preview', 'reuse', ''):
            host = Mock(); host.support.preview_rows.return_value = ([], TABLES.ROWS)
            reused = json.loads(json.dumps(final))
            after = json.loads(json.dumps(native))
            overwritten = dict(created, execution_id='overwrite')
            if failure == 'native-id': after['table_id'] = '124'
            if failure == 'ddl': after['ddl_sha256'] = 'changed'
            if failure == 'stage': after['staging_tables'] = 1
            if failure == 'item-id': overwritten['item_id'] = 8
            if failure == 'preview': host.support.preview_rows.return_value = ([], TABLES.ROWS[:-1])
            if failure == 'reuse': reused['preview_rows'][2]['label'] = '尾随😀'
            host.submit_and_wait.return_value = ('reuse', {'metadata': {'result': {'runtime_execution_id': 'runtime-reuse', 'final_result': reused}}})
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary, \
                 patch.dict(os.environ, ADDP_ONLINE_SECRET_DIR=temporary, ADDP_ONLINE_TEST_RUN_ID='run-tables',
                    ADDP_ONLINE_MYSQL_ENGINE_ID='8', ADDP_ONLINE_DORIS_ENGINE_ID='9', ADDP_ONLINE_SPARK_RUNTIME_URL='http://runtime'), \
                 patch.object(TABLES, 'save', return_value=overwritten), patch.object(TABLES, 'physical', return_value=after):
                Path(temporary, 'spark-table-prepared.json').write_text(json.dumps(prepared))
                if failure:
                    with self.assertRaises(ValueError): TABLES.run(host, Mock(), report, 30)
                else:
                    result = TABLES.run(host, Mock(), report, 30)
                    self.assertEqual(set(result), {'mysql', 'doris'})
                    self.assertEqual(host.submit_and_wait.call_args.args[1]['tasks'][0]['operator'], 'load')

    def test_scenario_failure_always_performs_domain_cleanup(self):
        with patch.dict(os.environ, ADDP_ONLINE_TEST='1', ADDP_ONLINE_HOSTED='1', GATEWAY_URL='http://gateway', ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer'), \
             patch.object(HDFS.sys, 'argv', ['hdfs']), patch.object(HDFS, 'run_scenario', side_effect=ValueError('browser failed')), \
             patch.object(HDFS.TABLES, 'cleanup') as cleanup:
            with self.assertRaisesRegex(ValueError, 'browser failed'): HDFS.main()
            cleanup.assert_called_once()


class PhysicalOracleTest(unittest.TestCase):
    def query(self, engine, sql, headers=False):
        if 'COUNT(*)' in sql: return '0'
        if sql.startswith('DROP '): return ''
        if sql.startswith('DESC '): return 'id\tbigint\nlabel\t' + ('longtext' if engine == 'mysql' else 'string') + '\nvalue\tdecimal(38, 18)'
        if sql.startswith('SHOW CREATE'): return 'table\tstable definition'
        if 'INNODB_TABLES' in sql: return '123'
        if sql == "SHOW PROC '/dbs'": return 'DbId\tDbName\n12\tspark_results'
        if sql == "SHOW PROC '/dbs/12'": return 'TableId\tTableName\n123\t' + TABLE
        return '\n'.join('\t'.join((row['id'], str(int(row['label'] is None)), '' if row['label'] is None else row['label'].encode().hex(),
                         str(int(row['value'] is None)), row['value'] or '')) for row in TABLES.ROWS)

    def test_both_native_protocols_keep_identity_precision_null_and_whitespace(self):
        with patch.object(ORACLE, 'owned_query', side_effect=self.query):
            for engine in ('mysql', 'doris'):
                observed = ORACLE.observe(engine, TABLE)
                self.assertEqual(observed['table_id'], '123')
                self.assertEqual(observed['rows'], TABLES.ROWS)
                self.assertEqual(ORACLE.observe(engine, TABLE, True), {'target_absent': True, 'staging_tables': 0})

    def test_foreign_container_is_rejected_before_any_sql(self):
        with patch.object(ORACLE.subprocess, 'run', return_value=SimpleNamespace(stdout='foreign')) as run:
            with self.assertRaisesRegex(ValueError, 'foreign'): ORACLE.owned_query('mysql', 'SELECT 1')
            self.assertEqual(run.call_count, 1)

    def test_invalid_identifier_never_runs_sql(self):
        with patch.object(ORACLE, 'owned_query') as query:
            for table in ('foreign', TABLE + '; DROP TABLE other', '../table'):
                with self.assertRaises(ValueError): ORACLE.observe('doris', table, True)
            query.assert_not_called()

    def test_staging_or_failed_drop_cannot_report_clean(self):
        for failure in ('staging', 'target'):
            def query(engine, sql, headers=False):
                if ('LEFT(' in sql) == (failure == 'staging') and 'COUNT(*)' in sql: return '1'
                return self.query(engine, sql, headers)
            with self.subTest(failure=failure), patch.object(ORACLE, 'owned_query', side_effect=query), self.assertRaises(ValueError):
                ORACLE.observe('mysql', TABLE, True)


if __name__ == '__main__':
    unittest.main()
