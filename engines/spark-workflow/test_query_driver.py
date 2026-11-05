"""Result-schema precision and the private Driver request boundary, without JVM."""
import copy
from contextlib import nullcontext
from datetime import date, datetime
from decimal import Decimal
import unittest
from unittest.mock import Mock, patch

from pyspark.errors import AnalysisException
from pyspark.sql import Row, types

from query_driver import execute, result_field, result_schema, result_value, validate_request
from query_guard import QueryValidationError


class QueryDriverTest(unittest.TestCase):
    def test_missing_column_is_only_analyzed_after_protection_allows(self):
        facts = {'read_set': {'paths': []}, 'engine_read_scope': None}
        request = dict(self.request(), query='SELECT missing_column FROM source.public.orders')
        missing = AnalysisException('private column and suggestions')
        missing.getErrorClass = Mock(return_value='UNRESOLVED_COLUMN.WITH_SUGGESTION')
        with patch('pyspark.sql.SparkSession') as session, patch('query_driver.verified_artifact'), \
             patch('query_driver.validate_query'), patch('query_read_plan.lock_read_plan', return_value=nullcontext(facts)), \
             patch.dict('os.environ'):
            spark = session.builder.appName.return_value.master.return_value
            spark.config.return_value = spark
            driver = spark.getOrCreate.return_value
            driver.sql.side_effect = missing
            deny = Mock(return_value=False)
            with self.assertRaisesRegex(QueryValidationError, '^query_permission_denied$'):
                execute(request, deny)
            deny.assert_called_once_with(facts)
            driver.sql.assert_not_called()

            def allow(proven):
                self.assertEqual(proven, facts)
                driver.sql.assert_not_called()
                return True

            with self.assertRaisesRegex(QueryValidationError, '^query_column_not_found$'):
                execute(request, allow)
            driver.sql.assert_called_once()

    def request(self):
        return {'master_url': 'spark://cluster:7077', 'driver_host': 'driver',
                'catalogs': [{'engine_id': 11, 'engine_type': 'postgresql', 'alias': 'source', 'url': 'jdbc:postgresql://db:5432/data',
                              'driver': 'org.postgresql.Driver', 'user': 'reader', 'password': 'test-only'}],
                'query': 'SELECT * FROM source.public.orders', 'args': [], 'limit': 10}

    def test_private_request_only_accepts_owned_cluster_and_jdbc_mounts(self):
        request = self.request()
        validate_request(request)
        for change in (
            {'master_url': 'local[*]'}, {'master_url': 'spark://user:secret@cluster:7077'},
            {'master_url': 'spark://cluster:7077/path'}, {'driver_host': ''},
            {'limit': True}, {'limit': 10001}, {'args': [float('nan')]},
            {'args': [Decimal('Infinity')]}, {'args': [{'x': 1}]}, {'query': ''},
            {'connection_info': {}}, {'catalogs': []},
        ):
            with self.subTest(change=change), self.assertRaises(ValueError):
                validate_request(dict(request, **change))
        for alias in ('spark_catalog', 'global_temp', 'source.url', ' source'):
            invalid = copy.deepcopy(request)
            invalid['catalogs'][0]['alias'] = alias
            with self.subTest(alias=alias), self.assertRaises(ValueError):
                validate_request(invalid)
        duplicate = copy.deepcopy(request)
        duplicate['catalogs'].append(dict(request['catalogs'][0], alias='SOURCE'))
        with self.assertRaises(ValueError):
            validate_request(duplicate)

    def test_bigint_and_decimal_encoding_uses_declared_output_schema(self):
        big = result_field(types.StructField('n', types.LongType(), False))
        integer = result_field(types.StructField('n', types.IntegerType(), False))
        decimal = result_field(types.StructField('n', types.DecimalType(38, 18), True))
        self.assertEqual(result_value(big, 1), '1')
        self.assertEqual(result_value(big, 9007199254740993), '9007199254740993')
        self.assertEqual(result_value(integer, 1), 1)
        self.assertEqual(result_value(decimal, Decimal('100.123456789012345678')), '100.123456789012345678')
        self.assertEqual((decimal['type'], decimal['precision'], decimal['scale']), ('decimal', 38, 18))
        self.assertIsNone(result_value(big, None))

    def test_nested_schema_preserves_exact_integer_and_decimal_types(self):
        native = types.StructType([
            types.StructField('ids', types.ArrayType(types.LongType())),
            types.StructField('amount', types.DecimalType(38, 18)),
        ])
        field = result_field(types.StructField('nested', native))
        value = result_value(field, Row(ids=[9007199254740993, None], amount=Decimal('1.000000000000000001')))
        self.assertEqual(value, {'ids': ['9007199254740993', None], 'amount': '1.000000000000000001'})
        self.assertEqual(field['fields'][0]['element']['type'], 'bigint')

    def test_temporal_binary_nonfinite_and_unknown_result_types(self):
        for native, value, expected in (
            (types.DateType(), date(2026, 11, 1), '2026-11-01'),
            (types.TimestampType(), datetime(2026, 11, 1, 1, 2, 3, 123456), '2026-11-01T01:02:03.123456'),
            (types.BinaryType(), b'\x00\xff', 'AP8='),
        ):
            self.assertEqual(result_value(result_field(types.StructField('v', native)), value), expected)
        with self.assertRaises(ValueError):
            result_field(types.StructField('v', types.MapType(types.StringType(), types.LongType())))
        with self.assertRaises(ValueError):
            result_value(result_field(types.StructField('v', types.DoubleType())), float('inf'))

    def test_duplicate_columns_are_rejected_even_for_empty_results(self):
        duplicate = types.StructType([types.StructField('id', types.LongType()),
                                      types.StructField('id', types.IntegerType())])
        for schema in (duplicate, types.StructType([types.StructField('nested', duplicate)])):
            with self.subTest(schema=schema), self.assertRaisesRegex(ValueError, 'query_result_duplicate_columns'):
                result_schema(schema)


if __name__ == '__main__':
    unittest.main()
