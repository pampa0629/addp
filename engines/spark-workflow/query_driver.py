"""One owner-started Spark query Driver; private stdin/result-file contract.

This is not an HTTP endpoint or a service. Its parent must authenticate the
caller and consume exact cluster/source execution grants before starting it.
"""
from __future__ import annotations

import base64
from decimal import Decimal
import json
import math
import os
from pathlib import Path
import re
import sys
import time
from urllib.parse import urlsplit

from query_guard import MAX_QUERY_BYTES, QueryValidationError, validate_query
from spark_dependencies import JDBC_MAVEN_PACKAGES
from query_jdbc import verified_artifact


def validate_request(request):
    if not isinstance(request, dict) or set(request) != {
            'master_url', 'driver_host', 'catalogs', 'query', 'args', 'limit'}:
        raise ValueError('query_driver_request_invalid')
    if (not isinstance(request['master_url'], str) or not isinstance(request['query'], str)
            or not request['query'].strip()
            or len(request['query'].encode('utf-8')) > MAX_QUERY_BYTES):
        raise ValueError('query_driver_request_invalid')
    endpoint = urlsplit(request['master_url'])
    if (endpoint.scheme != 'spark' or not endpoint.hostname or not endpoint.port
            or endpoint.username is not None or endpoint.password is not None
            or endpoint.path or endpoint.query or endpoint.fragment):
        raise ValueError('query_driver_request_invalid')
    if (not isinstance(request['driver_host'], str) or not request['driver_host']
            or any(c in request['driver_host'] for c in '/\\@?#, \t\r\n')):
        raise ValueError('query_driver_request_invalid')
    if (type(request['limit']) is not int or not 1 <= request['limit'] <= 10000
            or not isinstance(request['args'], list) or len(request['args']) > 1024
            or any(type(value) not in {str, int, float, Decimal, bool, type(None)}
                   or isinstance(value, float) and not math.isfinite(value)
                   or isinstance(value, Decimal) and not value.is_finite()
                   for value in request['args'])):
        raise ValueError('query_driver_request_invalid')
    catalogs = request['catalogs']
    if not isinstance(catalogs, list) or not 1 <= len(catalogs) <= 16:
        raise ValueError('query_driver_request_invalid')
    seen = set()
    for catalog in catalogs:
        if not isinstance(catalog, dict) or set(catalog) != {
                'alias', 'url', 'driver', 'user', 'password', 'engine_id', 'engine_type'}:
            raise ValueError('query_driver_request_invalid')
        if (type(catalog['engine_id']) is not int or catalog['engine_id'] <= 0
                or catalog['engine_type'] not in {'postgresql', 'mysql'}
                or catalog['driver'] != {'postgresql': 'org.postgresql.Driver', 'mysql': 'com.mysql.cj.jdbc.Driver'}[catalog['engine_type']]):
            raise ValueError('query_driver_request_invalid')
        alias = catalog['alias']
        if (not isinstance(alias, str) or not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', alias)
                or alias.lower() in seen or alias.lower() in {'spark_catalog', 'global_temp'}):
            raise ValueError('query_driver_request_invalid')
        seen.add(alias.lower())
        prefix = {'org.postgresql.Driver': 'jdbc:postgresql://',
                  'com.mysql.cj.jdbc.Driver': 'jdbc:mysql://'}.get(catalog['driver'])
        if (not prefix or not isinstance(catalog['url'], str)
                or not catalog['url'].startswith(prefix)
                or any(not isinstance(catalog[key], str) for key in ('user', 'password'))):
            raise ValueError('query_driver_request_invalid')


def result_field(field, ordinal=0):
    """Describe the actual DataFrame output, never its sampled row values."""
    from pyspark.sql import types
    native = field.dataType
    names = {
        types.NullType: 'unknown', types.StringType: 'string', types.BooleanType: 'bool',
        types.ByteType: 'int', types.ShortType: 'int', types.IntegerType: 'int',
        types.LongType: 'bigint', types.FloatType: 'float', types.DoubleType: 'double',
        types.BinaryType: 'bytes', types.DateType: 'date',
        types.TimestampType: 'timestamp', types.TimestampNTZType: 'timestamp',
        types.DecimalType: 'decimal', types.ArrayType: 'array', types.StructType: 'json',
    }
    kind = names.get(type(native))
    if kind is None:
        raise ValueError('query_result_type_not_supported')
    result = {'name': field.name, 'type': kind, 'native_type': native.simpleString(),
              'nullable': field.nullable, 'ordinal_position': ordinal}
    if kind == 'decimal':
        result.update(precision=native.precision, scale=native.scale)
    elif kind == 'array':
        result['element'] = result_field(types.StructField('', native.elementType, native.containsNull))
        result['element_type'] = result['element']['type']
    elif kind == 'json':
        _, result['fields'] = result_schema(native)
    return result


def result_value(field, value):
    if value is None:
        return None
    kind = field['type']
    if kind == 'bigint':
        return str(value)
    if kind == 'decimal':
        return format(value, 'f')
    if kind == 'bytes':
        return base64.b64encode(value).decode('ascii')
    if kind in {'date', 'timestamp'}:
        return value.isoformat()
    if kind == 'array':
        return [result_value(field['element'], item) for item in value]
    if kind == 'json':
        return {child['name']: result_value(child, value[child['name']]) for child in field['fields']}
    if isinstance(value, float) and not math.isfinite(value):
        raise ValueError('query_result_value_not_supported')
    return value


def result_schema(schema):
    fields = [result_field(field, i + 1) for i, field in enumerate(schema.fields)]
    columns = [field['name'] for field in fields]
    if len(set(columns)) != len(columns):
        raise ValueError('query_result_duplicate_columns')
    return columns, fields


def execute(request, approve):
    validate_request(request)
    # This process owns an independent gateway. Do not inherit the workflow's
    # submit JARs, SQL extensions, UDFs, session catalogs or cached JVM.
    import shlex
    dialect = verified_artifact()
    os.environ['PYSPARK_SUBMIT_ARGS'] = shlex.join(['--jars', str(dialect), 'pyspark-shell'])
    os.environ['TZ'] = 'UTC'
    if hasattr(time, 'tzset'):
        time.tzset()
    from pyspark.sql import SparkSession
    spark = (SparkSession.builder.appName('ADDP-federated-query')
             .master(request['master_url'])
             .config('spark.driver.host', request['driver_host'])
             .config('spark.driver.bindAddress', '0.0.0.0')
             .config('spark.ui.enabled', 'false')
             .config('spark.sql.extensions', '')
             .config('spark.plugins', 'com.addp.spark.query.ExactIdentifierDialects')
             .config('spark.sql.catalogImplementation', 'in-memory')
             .config('spark.sql.caseSensitive', 'true')
             .config('spark.sql.session.timeZone', 'UTC')
             .config('spark.sql.ansi.enabled', 'true')
             .config('spark.task.maxFailures', '1')
             .config('spark.executor.memory', '512m')
             .config('spark.driver.memory', '512m')
             .config('spark.driver.maxResultSize', '4m')
             .config('spark.cores.max', '2')
             .config('spark.jars.packages', ','.join(JDBC_MAVEN_PACKAGES))
             .getOrCreate())
    try:
        validated = validate_query(spark, request['query'], {source['alias'] for source in request['catalogs']})
        for source in request['catalogs']:
            prefix = 'spark.sql.catalog.' + source['alias']
            spark.conf.set(prefix, 'com.addp.spark.query.ExactIdentifierDialects$OnlyPostgresCatalog'
                           if source['engine_type'] == 'postgresql' else
                           'org.apache.spark.sql.execution.datasources.v2.jdbc.JDBCTableCatalog')
            for name in ('url', 'driver', 'user', 'password'):
                spark.conf.set(prefix + '.' + name, source[name])
        # Metadata guards survive the prepare/allow handshake and collect.
        # Unknown relation closure is never advertised as a complete scope.
        from query_read_plan import lock_read_plan
        with lock_read_plan(spark, request['catalogs'], validated.source_relations) as facts:
            # No Spark SQL is analyzed against a View/foreign source before the
            # owner decides whether an unknown read can be admitted at all.
            if facts['read_set'] is None:
                raise QueryValidationError('query_source_not_supported')
            if not approve(facts):
                raise QueryValidationError('query_permission_denied')
            from pyspark.errors import AnalysisException
            try:
                result = spark.sql(request['query'], args=request['args']).limit(request['limit'])
            except AnalysisException as error:
                if error.getErrorClass() in {'UNRESOLVED_COLUMN.WITH_SUGGESTION', 'UNRESOLVED_COLUMN.WITHOUT_SUGGESTION'}:
                    raise QueryValidationError('query_column_not_found') from None
                raise
            columns, fields = result_schema(result.schema)
            rows = [{field['name']: result_value(field, row[i]) for i, field in enumerate(fields)}
                    for row in result.collect()]
            return {'columns': columns, 'fields': fields, 'rows': rows}
    finally:
        spark.stop()


def main():
    try:
        raw = sys.stdin.buffer.readline(1024 * 1024 + 1)
        if len(raw) > 1024 * 1024:
            raise ValueError('query_request_too_large')
        def reject_constant(_value):
            raise ValueError('query_driver_request_invalid')
        request = json.loads(raw, parse_float=Decimal, parse_constant=reject_constant)
        def approve(facts):
            # Atomic publication; the owner cannot observe a partial read set.
            facts_path = Path(sys.argv[1] + '.facts')
            temporary = facts_path.with_suffix('.tmp')
            temporary.write_text(json.dumps(facts, ensure_ascii=False), encoding='utf-8')
            temporary.replace(facts_path)
            return sys.stdin.buffer.readline(32) == b'execute\n'
        result = execute(request, approve)
        encoded = json.dumps(result, ensure_ascii=False, allow_nan=False).encode('utf-8')
        if len(encoded) > 4 * 1024 * 1024:
            raise ValueError('query_result_too_large')
    except QueryValidationError as error:
        encoded = json.dumps({'error': str(error)}).encode('utf-8')
    except Exception:
        # A Spark/JDBC exception often contains SQL, URLs or authentication data.
        encoded = b'{"error":"query_driver_failed"}'
    Path(sys.argv[1]).write_bytes(encoded)


if __name__ == '__main__':
    main()
