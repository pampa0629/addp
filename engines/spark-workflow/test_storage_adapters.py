import unittest
from unittest.mock import patch

from storage_adapters import DatabaseAdapter, FileAdapter
from unittest.mock import MagicMock


class StorageAdapterTest(unittest.TestCase):
    def test_unsupported_file_write_rejected_before_source_action(self):
        frame = MagicMock()
        with self.assertRaisesRegex(ValueError, 'Unsupported file format'):
            FileAdapter.save(frame, {'path': '/result', 'format': 'invalid'})
        frame.count.assert_not_called()
        frame.write.format.assert_not_called()

    def test_file_reads_require_derived_format(self):
        spark = MagicMock()
        with self.assertRaisesRegex(KeyError, 'format'):
            FileAdapter.load(spark, {'path': 's3a://bucket/orders.csv'})
        spark.read.format.assert_not_called()

    def test_file_reader_uses_each_derived_format(self):
        for file_format in ('csv', 'json', 'parquet'):
            with self.subTest(file_format=file_format):
                spark = MagicMock()
                spark.read.options.return_value = spark.read
                reader = spark.read.format.return_value
                reader.option.return_value = reader
                path = 's3a://bucket/orders.' + file_format
                with patch.object(FileAdapter, '_s3_access', return_value=(path, {})):
                    result = FileAdapter.load(spark, {'path': path, 'format': file_format})
                spark.read.format.assert_called_once_with(file_format)
                reader.load.assert_called_once_with(path)
                self.assertIs(result, reader.load.return_value)
                if file_format == 'csv':
                    reader.option.assert_any_call('header', 'true')
                    reader.option.assert_any_call('inferSchema', 'true')

    def test_minio_configuration_is_per_operation_and_validates_scope(self):
        connection = {'engine_type': 'minio', 'endpoint': 'store:9000', 'use_ssl': False,
                      'access_key': 'owned-access', 'secret_key': 'owned-secret', 'bucket': 'result'}
        params = {'path': 's3a://result/output', 'connection_info': connection}
        path, options = FileAdapter._s3_access(params)
        self.assertEqual(path, params['path'])
        self.assertEqual(options['fs.s3a.endpoint'], 'http://store:9000')
        self.assertEqual(options['fs.s3a.impl.disable.cache'], 'true')
        self.assertEqual(options['fs.s3a.bucket.result.secret.key'], 'owned-secret')
        self.assertEqual(options['fs.s3a.bucket.result.endpoint'], 'http://store:9000')
        _, default_ssl = FileAdapter._s3_access(dict(params, connection_info={k: v for k, v in connection.items() if k != 'use_ssl'}))
        self.assertEqual(default_ssl['fs.s3a.connection.ssl.enabled'], 'false')
        _, other = FileAdapter._s3_access(dict(params, connection_info=dict(connection, endpoint='other:9000', secret_key='other-secret')))
        self.assertEqual(options['fs.s3a.secret.key'], 'owned-secret')
        self.assertEqual(other['fs.s3a.secret.key'], 'other-secret')
        for changed in ({'path': 's3a://foreign/output'}, {'path': 's3://result/output'},
                        {'path': 's3a://result/a/../b'}, {'path': 's3a://result/a%2F..%2Fb'},
                        {'path': 's3a://result/output*'}, {'path': 's3a://result/'},
                        {'connection_info': dict(connection, secret_key='')},
                        {'connection_info': dict(connection, endpoint='https://store:9000')}):
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                FileAdapter._s3_access(dict(params, **changed))

    def test_minio_publication_reports_verified_count_without_source_count(self):
        frame, filesystem = MagicMock(), MagicMock()
        filesystem.exists.return_value = False
        writer = frame.write
        writer.options.return_value = writer.format.return_value = writer.mode.return_value = writer
        params = {'path': 's3a://result/owned', 'format': 'parquet', 'mode': 'create',
                  'connection_info': {'engine_type': 'minio'}}
        with patch.object(FileAdapter, '_s3_access', return_value=(params['path'], {'fs.s3a.impl.disable.cache': 'true'})), \
             patch.object(FileAdapter, '_s3_filesystem', return_value=(filesystem, MagicMock())), \
             patch.object(FileAdapter, '_verify_s3_parquet', return_value=17):
            self.assertEqual(FileAdapter.save(frame, params), 17)
        frame.count.assert_not_called()
        writer.mode.assert_called_once_with('errorifexists')
        filesystem.close.assert_called_once()

    def test_minio_rejects_existing_results_without_cleanup_and_reports_cleanup_failure(self):
        frame, filesystem = MagicMock(), MagicMock()
        params = {'path': 's3a://result/owned', 'mode': 'create', 'connection_info': {'engine_type': 'minio'}}
        with patch.object(FileAdapter, '_s3_access', return_value=(params['path'], {})), \
             patch.object(FileAdapter, '_s3_filesystem', return_value=(filesystem, MagicMock())), \
             patch.object(FileAdapter, '_cleanup_s3_result') as cleanup:
            filesystem.exists.return_value = True
            with self.assertRaisesRegex(ValueError, 'already exists'): FileAdapter.save(frame, params)
            cleanup.assert_not_called()
            filesystem.exists.return_value = False
            frame.write.options.return_value.format.return_value.mode.return_value.save.side_effect = OSError('write failed')
            with self.assertRaisesRegex(OSError, 'write failed'): FileAdapter.save(frame, params)
            cleanup.assert_called_once_with(frame.sparkSession, filesystem, params['path'])
            cleanup.side_effect = OSError('cleanup failed')
            with self.assertRaisesRegex(RuntimeError, 'cleanup unverified'): FileAdapter.save(frame, params)

    def cleanup_fixture(self, names, commits):
        spark, filesystem = MagicMock(), MagicMock()
        files = filesystem.listFiles.return_value
        files.hasNext.side_effect = [True] * len(names) + [False]
        paths = [MagicMock() for _ in names]
        for path, name in zip(paths, names):
            path.getName.return_value = name
        files.next.side_effect = [MagicMock(**{'getPath.return_value': path}) for path in paths]
        records = spark.sparkContext._jvm.org.apache.hadoop.fs.s3a.commit.files
        records.SinglePendingCommit.load.side_effect = commits
        records.PendingSet.load.return_value.getCommits.return_value = commits
        filesystem.exists.side_effect = [True, True, False]
        return spark, filesystem, records

    def pending_commit(self, key='results/owned#literal/part.parquet', bucket='bucket', upload='owned-id'):
        return MagicMock(**{'getDestinationKey.return_value': key,
                            'getBucket.return_value': bucket, 'getUploadId.return_value': upload})

    def test_minio_cleanup_aborts_recorded_upload_once_and_deletes_owned_directory(self):
        commit = self.pending_commit()
        spark, filesystem, _ = self.cleanup_fixture(['part.pending', 'task.pendingset'], [commit])
        FileAdapter._cleanup_s3_result(spark, filesystem, 's3a://bucket/results/owned#literal')
        helper = filesystem.getWriteOperationHelper.return_value
        helper.abortMultipartCommit.assert_called_once_with('results/owned#literal/part.parquet', 'owned-id')
        helper.listMultipartUploads.assert_not_called()
        helper.abortMultipartUploadsUnderPath.assert_not_called()
        filesystem.keyToQualifiedPath.assert_called_once_with('results/owned#literal')
        filesystem.delete.assert_called_once_with(filesystem.keyToQualifiedPath.return_value, True)

    def test_minio_cleanup_validates_all_records_before_mutating(self):
        for invalid in (self.pending_commit(key='results/owned#literal-sibling/part.parquet'),
                        self.pending_commit(bucket='foreign'), self.pending_commit(upload=''),
                        self.pending_commit(key='results/owned#literal/../sibling/part.parquet')):
            spark, filesystem, _ = self.cleanup_fixture(['valid.pending', 'invalid.pending'], [self.pending_commit(), invalid])
            with self.assertRaisesRegex(ValueError, 'outside'):
                FileAdapter._cleanup_s3_result(spark, filesystem, 's3a://bucket/results/owned#literal')
            filesystem.getWriteOperationHelper.assert_not_called()
            filesystem.delete.assert_not_called()

    def test_minio_cleanup_preserves_evidence_for_corrupt_missing_or_abort_failure(self):
        for failure in ('corrupt', 'missing', 'abort'):
            spark, filesystem, records = self.cleanup_fixture([] if failure == 'missing' else ['part.pending'], [self.pending_commit()])
            if failure == 'corrupt': records.SinglePendingCommit.load.side_effect = ValueError('corrupt record')
            if failure == 'missing': records.SuccessData.load.side_effect = ValueError('missing completion evidence')
            if failure == 'abort': filesystem.getWriteOperationHelper.return_value.abortMultipartCommit.side_effect = OSError('abort failed')
            with self.assertRaises((ValueError, OSError)):
                FileAdapter._cleanup_s3_result(spark, filesystem, 's3a://bucket/results/owned#literal')
            filesystem.delete.assert_not_called()

    def test_minio_cleanup_only_accepts_exact_upload_absence(self):
        for error_class in ('java.io.FileNotFoundException', 'java.io.IOException'):
            spark, filesystem, _ = self.cleanup_fixture(['part.pending'], [self.pending_commit()])
            error = RuntimeError('abort response')
            error.java_exception = MagicMock()
            error.java_exception.getClass.return_value.getName.return_value = error_class
            filesystem.getWriteOperationHelper.return_value.abortMultipartCommit.side_effect = error
            if error_class == 'java.io.FileNotFoundException':
                FileAdapter._cleanup_s3_result(spark, filesystem, 's3a://bucket/results/owned#literal')
                filesystem.delete.assert_called_once()
            else:
                with self.assertRaisesRegex(RuntimeError, 'abort response'):
                    FileAdapter._cleanup_s3_result(spark, filesystem, 's3a://bucket/results/owned#literal')
                filesystem.delete.assert_not_called()

    def test_minio_cleanup_accepts_magic_completion_when_pending_records_are_gone(self):
        spark, filesystem, records = self.cleanup_fixture([], [])
        records.SuccessData.load.return_value.getCommitter.return_value = 'magic'
        FileAdapter._cleanup_s3_result(spark, filesystem, 's3a://bucket/results/owned#literal')
        filesystem.getWriteOperationHelper.return_value.abortMultipartCommit.assert_not_called()
        filesystem.delete.assert_called_once()

    def test_minio_modes_and_other_formats_fail_before_any_source_action(self):
        for changes in ({'mode': 'overwrite'}, {'mode': 'append'}, {'format': 'geoparquet'}, {'format': 'csv'}):
            frame = MagicMock()
            with self.assertRaises(ValueError):
                FileAdapter.save(frame, {'path': 's3a://bucket/result', 'mode': 'create', 'format': 'parquet'} | changes)
            frame.count.assert_not_called()
            frame.write.options.assert_not_called()

    def test_geometry_columns_are_discovered_from_schema(self):
        class GeometryType:
            def typeName(self):
                return "geometry"

        class StringType:
            def typeName(self):
                return "string"

        class Field:
            def __init__(self, name, data_type):
                self.name = name
                self.dataType = data_type

        class DataFrame:
            class Schema:
                fields = [
                    Field("source_shape", GeometryType()),
                    Field("name", StringType()),
                    Field("buffer_shape", GeometryType()),
                ]

            schema = Schema()

        self.assertEqual(
            ["source_shape", "buffer_shape"],
            DatabaseAdapter._geometry_column_names(DataFrame()),
        )

    def test_database_adapter_requires_connection_info(self):
        with self.assertRaisesRegex(ValueError, "connection_info"):
            DatabaseAdapter._connection_context({"engine_id": 34})

    def test_database_adapter_uses_connection_info_engine_type(self):
        engine_type, conn_info = DatabaseAdapter._connection_context({
            "connection_info": {
                "engine_type": "postgresql",
                "host": "postgres",
                "port": 5432,
                "database": "addp",
            }
        })

        self.assertEqual("postgresql", engine_type)
        self.assertEqual("postgres", conn_info["host"])

    def test_database_adapter_maps_loopback_to_shared_host(self):
        params = {
            "connection_info": {
                "engine_type": "postgresql",
                "host": "localhost",
            }
        }
        with patch.dict("os.environ", {"SPARK_WORKFLOW_SHARED_HOST": "192.0.2.10"}):
            _, conn_info = DatabaseAdapter._connection_context({
                "connection_info": params["connection_info"]
            })

        self.assertEqual("192.0.2.10", conn_info["host"])
        self.assertEqual("localhost", params["connection_info"]["host"])

    def test_database_adapter_rejects_loopback_without_shared_host(self):
        with patch.dict("os.environ", {}, clear=True):
            with self.assertRaisesRegex(ValueError, "SPARK_WORKFLOW_SHARED_HOST"):
                DatabaseAdapter._connection_context({
                    "connection_info": {
                        "engine_type": "postgresql",
                        "host": "localhost",
                    }
                })

    def test_database_adapter_preserves_remote_host(self):
        with patch.dict("os.environ", {"SPARK_WORKFLOW_SHARED_HOST": "192.0.2.10"}):
            _, conn_info = DatabaseAdapter._connection_context({
                "connection_info": {
                    "engine_type": "postgresql",
                    "host": "database.example.internal",
                }
            })

        self.assertEqual("database.example.internal", conn_info["host"])

    def test_postgresql_jdbc_config_includes_sslmode(self):
        jdbc_url, driver = DatabaseAdapter._jdbc_config("postgresql", {
            "host": "database.example.internal",
            "port": 5432,
            "database": "addp",
            "sslmode": "disable",
        })

        self.assertEqual(
            "jdbc:postgresql://database.example.internal:5432/addp?sslmode=disable",
            jdbc_url,
        )
        self.assertEqual("org.postgresql.Driver", driver)

    def test_mysql_jdbc_config_uses_connector_j_driver(self):
        jdbc_url, driver = DatabaseAdapter._jdbc_config("mysql", {
            "host": "database.example.internal",
            "port": 3306,
            "database": "addp",
        })

        self.assertEqual("jdbc:mysql://database.example.internal:3306/addp", jdbc_url)
        self.assertEqual("com.mysql.cj.jdbc.Driver", driver)

    def test_doris_create_table_sql_uses_native_types_and_distribution(self):
        class StringType:
            def typeName(self):
                return "string"

        class LongType:
            def typeName(self):
                return "long"

        class BooleanType:
            def typeName(self):
                return "boolean"

        class TimestampType:
            def typeName(self):
                return "timestamp"

        class Field:
            def __init__(self, name, data_type, nullable=True):
                self.name = name
                self.dataType = data_type
                self.nullable = nullable

        class DataFrame:
            class Schema:
                fields = [
                    Field("id", LongType(), nullable=False),
                    Field("customer`name", StringType()),
                    Field("active", BooleanType()),
                    Field("created_at", TimestampType()),
                ]

            schema = Schema()

        self.assertEqual(
            (
                "CREATE TABLE IF NOT EXISTS `addp_acceptance`.`customers_copy` "
                "(`id` BIGINT NOT NULL, `customer``name` VARCHAR(65533), "
                "`active` BOOLEAN, `created_at` DATETIME) "
                "DUPLICATE KEY(`id`) DISTRIBUTED BY HASH(`id`) BUCKETS 10"
            ),
            DatabaseAdapter._doris_create_table_sql(
                DataFrame(), "addp_acceptance", "customers_copy"
            ),
        )

    def test_doris_save_prepares_table_then_appends(self):
        df = MagicMock()
        df.count.return_value = 3
        params = {
            "connection_info": {"engine_type": "doris"},
            "schema": "addp_acceptance",
            "table": "customers_copy",
            "mode": "overwrite",
        }
        conn_info = {
            "engine_type": "doris",
            "host": "database.example.internal",
            "port": 9030,
            "database": "addp_acceptance",
        }

        with patch.object(
            DatabaseAdapter, "_connection_context", return_value=("doris", conn_info)
        ), patch.object(
            DatabaseAdapter,
            "_jdbc_config",
            return_value=("jdbc:mysql://database/addp_acceptance", "driver"),
        ), patch.object(
            DatabaseAdapter, "_geometry_column_names", return_value=[]
        ), patch.object(
            DatabaseAdapter, "_prepare_doris_table"
        ) as prepare, patch.object(DatabaseAdapter, "_write_jdbc") as write:
            DatabaseAdapter.save(df, params)

        prepare.assert_called_once_with(
            df,
            "jdbc:mysql://database/addp_acceptance",
            conn_info,
            "addp_acceptance",
            "customers_copy",
            "overwrite",
        )
        write.assert_called_once_with(
            df,
            "jdbc:mysql://database/addp_acceptance",
            "driver",
            conn_info,
            "addp_acceptance",
            "customers_copy",
            "append",
        )


class PostgreSQLSaveTest(unittest.TestCase):
    def setUp(self):
        self.df = MagicMock()
        self.df.columns = ['id', 'value']
        self.conn = MagicMock()
        self.statement = self.conn.createStatement.return_value
        self.params = dict(connection_info={'engine_type': 'postgresql', 'host': 'pg',
                          'port': 5432, 'database': 'business'}, schema='results', table='target')

    def finalize(self, queries, mode='overwrite'):
        if len(queries) == 3 and queries[1] == [('r',)]:
            staged = int(queries[2][0][0])
            queries = [*queries, [('99',)], [(str(staged + (99 if mode == 'append' else 0)),)]]
        with patch.object(DatabaseAdapter, '_open_jdbc_connection', return_value=self.conn), \
                patch.object(DatabaseAdapter, '_postgresql_query', side_effect=queries), \
                patch.object(DatabaseAdapter, '_validate_postgresql_columns'):
            return DatabaseAdapter._finalize_postgresql_table(
                self.df, 'jdbc', {}, 'results', 'stage', 'target', [], mode, ['staged_id', 'staged_value'])

    def test_existing_target_is_preserved_and_count_comes_from_insert(self):
        self.statement.executeLargeUpdate.side_effect = [99, 2]
        self.assertEqual(self.finalize([[], [('r',)], [('2',)]]), 2)
        calls = [call.args[0] for call in self.statement.execute.call_args_list]
        self.assertIn('LOCK TABLE "results"."target" IN SHARE ROW EXCLUSIVE MODE', calls)
        self.assertIn('DROP TABLE "results"."stage"', calls)
        self.assertFalse(any('DROP TABLE "results"."target"' in sql for sql in calls))
        self.conn.commit.assert_called_once()
        self.df.count.assert_not_called()

    def test_trigger_skipped_rows_roll_back_before_commit(self):
        self.statement.executeLargeUpdate.side_effect = [99, 1]
        with self.assertRaisesRegex(ValueError, 'row count'):
            self.finalize([[], [('r',)], [('2',)]])
        self.conn.rollback.assert_called_once()
        self.conn.commit.assert_not_called()
        self.conn.close.assert_called_once()

    def test_delete_trigger_suppression_rolls_back_before_insert(self):
        self.statement.executeLargeUpdate.return_value = 98
        with self.assertRaisesRegex(ValueError, 'deleted row count'):
            self.finalize([[], [('r',)], [('2',)]])
        self.conn.rollback.assert_called_once()
        self.conn.commit.assert_not_called()
        self.statement.executeLargeUpdate.assert_called_once_with('DELETE FROM "results"."target"')

    def test_insert_failure_rolls_back_delete(self):
        self.statement.executeLargeUpdate.side_effect = [99, RuntimeError('constraint violation')]
        with self.assertRaisesRegex(RuntimeError, 'constraint'):
            self.finalize([[], [('r',)], [('2',)]])
        self.conn.rollback.assert_called_once()
        self.conn.commit.assert_not_called()

    def test_append_never_deletes_target(self):
        self.statement.executeLargeUpdate.return_value = 2
        self.assertEqual(self.finalize([[], [('r',)], [('2',)]], 'append'), 2)
        self.statement.executeLargeUpdate.assert_called_once_with(
            'INSERT INTO "results"."target" ("id", "value") SELECT "id", "value" FROM "results"."stage"')

    def test_new_target_uses_staging_count_including_empty_results(self):
        self.assertEqual(self.finalize([[], [], [('0',)]]), 0)
        self.statement.execute.assert_any_call('ALTER TABLE "results"."stage" RENAME TO "target"')
        self.statement.executeLargeUpdate.assert_not_called()

    def test_view_target_rejected(self):
        with self.assertRaisesRegex(ValueError, 'ordinary table'):
            self.finalize([[], [('v',)]])
        self.statement.executeLargeUpdate.assert_not_called()
        self.conn.rollback.assert_called_once()

    def test_validation_rejects_missing_types_and_generated_columns(self):
        stage = [('id', '20', '-1', '', ''), ('value', '25', '-1', '', '')]
        for target in (stage[:1], [('id', '23', '-1', '', ''), stage[1]],
                       [('id', '20', '-1', '', 'a'), stage[1]],
                       [stage[0], ('value', '25', '10', '', '')]):
            with self.subTest(target=target), patch.object(DatabaseAdapter, '_postgresql_query', side_effect=[stage, target]):
                with self.assertRaises(ValueError):
                    DatabaseAdapter._validate_postgresql_columns(self.conn, 'stage', 'target', [])

    def test_plain_postgresql_save_uses_single_staging_path_and_cleans_failures(self):
        with patch.object(DatabaseAdapter, '_geometry_column_names', return_value=[]), \
                patch.object(DatabaseAdapter, '_write_jdbc') as write, \
                patch.object(DatabaseAdapter, '_finalize_postgresql_table', side_effect=RuntimeError('publish failed')), \
                patch.object(DatabaseAdapter, '_drop_postgresql_table') as cleanup:
            with self.assertRaisesRegex(RuntimeError, 'publish failed'):
                DatabaseAdapter.save(self.df, self.params)
            stage = write.call_args.args[5]
            self.assertTrue(stage.startswith('__addp_spark_stage_'))
            self.assertEqual(cleanup.call_args.args[-1], stage)
            self.assertNotEqual(stage, 'target')
            self.df.count.assert_not_called()

    def test_postgresql_identifier_quoting_and_duplicate_names(self):
        self.assertEqual(DatabaseAdapter._postgresql_table('mixed.schema', 'ta"ble'), '"mixed.schema"."ta""ble"')
        self.df.columns = ['id', 'id']
        with patch.object(DatabaseAdapter, '_geometry_column_names', return_value=[]), \
                patch.object(DatabaseAdapter, '_write_jdbc') as write:
            with self.assertRaisesRegex(ValueError, 'unique'):
                DatabaseAdapter.save(self.df, self.params)
            write.assert_not_called()


if __name__ == "__main__":
    unittest.main()


class HDFSAdapterTest(unittest.TestCase):
    def setUp(self):
        self.spark = MagicMock()
        self.ugi = self.spark.sparkContext._jvm.org.apache.hadoop.security.UserGroupInformation
        self.ugi.isSecurityEnabled.return_value = False
        self.ugi.getCurrentUser.return_value.getShortUserName.return_value = 'reader'
        self.params = {'path': 'hdfs://namenode:8020/addp/orders.parquet',
                       'connection_info': {'engine_type': 'hdfs', 'authentication': 'simple',
                                           'rpc_uri': 'hdfs://namenode:8020', 'root_path': '/addp', 'user': 'reader'}}

    def test_simple_fixed_identity(self):
        FileAdapter._validate_hdfs_access(self.spark, self.params)
        self.ugi.getCurrentUser.return_value.getShortUserName.return_value = 'another'
        with self.assertRaisesRegex(ValueError, 'identity'):
            FileAdapter._validate_hdfs_access(self.spark, self.params)

    def test_root_authority_and_traversal(self):
        for path in ('hdfs://other:8020/addp/a.parquet', 'hdfs://namenode:8020/addp-other/a.parquet',
                     'hdfs://namenode:8020/addp/../secret', 'hdfs://namenode:8020/addp/%2e%2e/secret',
                     'hdfs://namenode:8020/addp//a', 'hdfs://namenode:8020/addp/a?secret=x'):
            with self.subTest(path=path), self.assertRaises(ValueError):
                FileAdapter._validate_hdfs_access(self.spark, dict(self.params, path=path))

    def test_uri_decodes_original_name_once_and_rejects_globs(self):
        source = 'hdfs://namenode:8020/addp/%E8%AE%A2%E5%8D%95%20100%25.csv'
        self.assertEqual('hdfs://namenode:8020/addp/订单 100%.csv',
                         FileAdapter._validate_hdfs_access(self.spark, dict(self.params, path=source)))
        for name in ('*.csv', '%3F.csv', '%5Ba%5D.csv', '%7Ba,b%7D.csv', '%5C.csv'):
            with self.assertRaisesRegex(ValueError, 'literal resource'):
                FileAdapter._validate_hdfs_access(self.spark, dict(self.params, path='hdfs://namenode:8020/addp/' + name))

    def test_hdfs_save_rejected_before_writer(self):
        frame = MagicMock()
        with self.assertRaisesRegex(ValueError, 'reading only'):
            FileAdapter.save(frame, self.params)
        frame.write.format.assert_not_called()

    def test_secure_or_missing_facts_are_rejected(self):
        self.ugi.isSecurityEnabled.return_value = True
        with self.assertRaises(ValueError):
            FileAdapter._validate_hdfs_access(self.spark, self.params)
        with self.assertRaises(ValueError):
            FileAdapter._validate_hdfs_access(self.spark, {'path': self.params['path']})
