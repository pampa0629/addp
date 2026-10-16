"""Doris-specific staged publication; never use JDBC overwrite on a target."""
import logging
import re
import time
import uuid

from mysql_publication import PublicationUncertain, close_safely, identifier, query, table_ref, validate_names

logger = logging.getLogger(__name__)
VISIBILITY_TIMEOUT_SECONDS = 60


def http_endpoint(conn_info):
    port = conn_info.get('http_port')
    if isinstance(port, bool) or not isinstance(port, int) or not 1 <= port <= 65535:
        raise ValueError('Doris Spark save requires an explicit FE http_port in the Engine connection')
    host = conn_info['host']
    return ('[' + host + ']' if ':' in host else host) + ':' + str(port)


def validate_target(connection, schema, table, df, adapter):
    rows = query(connection,
                 'SELECT TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?',
                 (schema, table))
    if not rows:
        return False
    if rows != [('BASE TABLE',)]:
        raise ValueError('Doris save requires an ordinary OLAP table target')
    ddl = query(connection, f'SHOW CREATE TABLE {table_ref(schema, table)}')[0][1]
    if not re.search(r'\bENGINE\s*=\s*OLAP\b', ddl, re.I) or not re.search(r'\bDUPLICATE\s+KEY\s*\(', ddl, re.I):
        raise ValueError('Doris Spark save requires a Duplicate Key OLAP target')
    described = query(connection, f'DESCRIBE {table_ref(schema, table)}')
    actual = {row[0]: row[1].lower().replace(' ', '') for row in described}
    expected = {field.name: adapter._doris_field_type(field).lower().replace(' ', '') for field in df.schema.fields}
    # Doris reports DECIMALV3 as DECIMAL and BOOLEAN as BOOLEAN/TINYINT.
    actual = {name: re.sub(r'decimal(?:v3|32|64|128)', 'decimal', kind) for name, kind in actual.items()}
    if actual != expected:
        raise ValueError('Doris target fields or types differ from the saved result')
    return True


def wait_stage(connection, stage_ref, expected):
    deadline = time.monotonic() + VISIBILITY_TIMEOUT_SECONDS
    while True:
        visible = int(query(connection, f'SELECT COUNT(*) FROM {stage_ref}')[0][0])
        if visible == expected:
            return visible
        if visible > expected:
            raise ValueError('Doris stage contains more rows than the prepared result')
        if time.monotonic() >= deadline:
            raise RuntimeError('Doris staging commits are not fully visible; target was not published')
        time.sleep(0.2)


def wait_publication(connection, schema, label):
    deadline = time.monotonic() + VISIBILITY_TIMEOUT_SECONDS
    while True:
        statement = connection.prepareStatement(f'SHOW LOAD FROM {identifier(schema)} WHERE LABEL = ?')
        try:
            statement.setString(1, label)
            result = statement.executeQuery()
            try:
                columns = {result.getMetaData().getColumnLabel(index).lower(): index
                           for index in range(1, result.getMetaData().getColumnCount() + 1)}
                states = []
                while result.next():
                    states.append(result.getString(columns['state']).upper())
            finally:
                result.close()
        finally:
            statement.close()
        if states == ['FINISHED']:
            return
        if states and any(state in {'CANCELLED', 'FAILED'} for state in states):
            raise RuntimeError('Doris publication failed; inspect the original load label')
        if time.monotonic() >= deadline:
            raise PublicationUncertain(f'Doris publication not confirmed visible; do not retry; label={label}')
        time.sleep(0.2)


def write_stage(df, conn_info, schema, stage, label):
    # V2 commit coordinator selects a single successful task attempt; Driver
    # commits its partition transactions. JDBC cannot expose that boundary.
    (df.write.format('doris').options(**{
        'doris.fenodes': http_endpoint(conn_info),
        'doris.table.identifier': schema + '.' + stage,
        'user': conn_info.get('user', conn_info.get('username', '')),
        'password': conn_info.get('password', ''),
        'doris.sink.enable-2pc': 'true',
        'doris.sink.max-retries': '0',
        'doris.sink.auto-redirect': 'true',
        'doris.sink.label.prefix': label,
        'doris.sink.properties.format': 'json',
        'doris.sink.properties.read_json_by_line': 'true',
        'doris.sink.properties.strict_mode': 'true',
        'doris.sink.properties.max_filter_ratio': '0',
        'doris.write.fields': ','.join(identifier(name) for name in df.columns),
    }).mode('append').save())


def save(df, params, jdbc_url, driver, conn_info, adapter):
    schema, target = params.get('schema', conn_info['database']), params['table']
    validate_names(schema, target, df.columns)
    if '.' in schema or any(',' in name or '`' in name for name in df.columns):
        raise ValueError('Doris connector does not support ambiguous database or field identifiers')
    http_endpoint(conn_info)  # Reject incomplete deployment facts before Spark actions.
    if adapter._geometry_column_names(df):
        raise ValueError('Doris Spark save does not support geometry columns')
    stage = '__addp_spark_stage_' + uuid.uuid4().hex
    stage_ref, target_ref = table_ref(schema, stage), table_ref(schema, target)
    connection = adapter._open_jdbc_connection(df.sparkSession, jdbc_url, conn_info)
    statement = connection.createStatement()
    uncertain, own_cache, stage_created = False, not df.is_cached, False
    try:
        statement.execute('SET enable_insert_strict = true')
        validate_target(connection, schema, target, df, adapter)
        statement.execute(adapter._doris_create_table_sql(df, schema, stage))
        stage_created = True
        if own_cache:
            from pyspark import StorageLevel
            df.persist(StorageLevel.DISK_ONLY)
        expected = df.count()
        write_stage(df, conn_info, schema, stage, 'addp_stage_' + uuid.uuid4().hex)
        staged = wait_stage(connection, stage_ref, expected)
        exists = validate_target(connection, schema, target, df, adapter)
        if not exists:
            uncertain = True
            statement.execute(f'ALTER TABLE {stage_ref} RENAME {identifier(target)}')
            uncertain = False
            return staged
        label = 'addp_publish_' + uuid.uuid4().hex
        projection = ','.join(identifier(name) for name in df.columns)
        operation = 'INSERT OVERWRITE TABLE' if params.get('mode', 'overwrite') == 'overwrite' else 'INSERT INTO'
        uncertain = True
        statement.execute(f'{operation} {target_ref} WITH LABEL {identifier(label)} ({projection}) '
                          f'SELECT {projection} FROM {stage_ref}')
        affected = int(statement.getLargeUpdateCount())
        if affected != staged:
            raise PublicationUncertain(f'Doris affected row count not confirmed; do not retry; label={label}')
        if staged:
            wait_publication(connection, schema, label)
        # An empty INSERT has no import transaction. INSERT OVERWRITE still
        # completes its synchronous partition replacement before returning.
        uncertain = False
        return affected
    except Exception as error:
        if uncertain and not isinstance(error, PublicationUncertain):
            raise PublicationUncertain('Doris publication response uncertain; staging evidence retained; do not retry') from error
        raise
    finally:
        if own_cache:
            df.unpersist()
        if stage_created and not uncertain:
            try:
                statement.execute(f'DROP TABLE IF EXISTS {stage_ref}')
            except Exception:
                logger.warning('Doris staging cleanup unverified: %s', stage)
        close_safely(statement)
        close_safely(connection)
