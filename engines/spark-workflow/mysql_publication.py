"""Publish a distributed JDBC stage without replacing an existing MySQL table."""
import hashlib
import logging
import uuid
from urllib.parse import urlencode

logger = logging.getLogger(__name__)


def close_safely(resource):
    try:
        resource.close()
    except Exception:
        logger.warning('Unable to close publication resource; preserving original result')


class PublicationUncertain(RuntimeError):
    """The server may have published; keep the stage and never replay implicitly."""


def identifier(value):
    return '`' + value.replace('`', '``') + '`'


def table_ref(schema, table):
    return identifier(schema) + '.' + identifier(table)


def validate_names(schema, table, columns):
    names = [schema, table, *columns]
    if (not columns or len({name.casefold() for name in columns}) != len(columns)
            or any(not isinstance(name, str) or not name or '\x00' in name
                   or len(name) > 64 for name in names)):
        raise ValueError('MySQL save requires unique nonempty identifiers of at most 64 characters')


def query(connection, sql, params=()):
    statement = connection.prepareStatement(sql)
    try:
        for index, value in enumerate(params, 1):
            statement.setString(index, value)
        rows = statement.executeQuery()
        try:
            width = rows.getMetaData().getColumnCount()
            result = []
            while rows.next():
                result.append(tuple(rows.getString(index) for index in range(1, width + 1)))
            return result
        finally:
            rows.close()
    finally:
        close_safely(statement)


def columns(connection, schema, table):
    return query(connection,
                 'SELECT COLUMN_NAME, DATA_TYPE, COLUMN_TYPE, EXTRA, NUMERIC_PRECISION, '
                 'NUMERIC_SCALE, DATETIME_PRECISION FROM information_schema.COLUMNS '
                 'WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION',
                 (schema, table))


def validate_columns(stage, target):
    source = {row[0].casefold(): row[1:] for row in stage}
    destination = {row[0].casefold(): row[1:] for row in target}
    if source.keys() != destination.keys():
        raise ValueError('MySQL target column set differs from the saved result')
    strings = {'char', 'varchar', 'tinytext', 'text', 'mediumtext', 'longtext'}
    binaries = {'binary', 'varbinary', 'tinyblob', 'blob', 'mediumblob', 'longblob'}
    for name, (kind, native, extra, precision, scale, temporal_precision) in destination.items():
        src_kind, src_native, _, src_precision, src_scale, src_temporal = source[name]
        if any(marker in (extra or '').lower() for marker in ('stored generated', 'virtual generated', 'auto_increment')):
            raise ValueError('MySQL save does not support generated or auto-increment target columns')
        same_family = (kind == src_kind or kind in strings and src_kind in strings
                       or kind in binaries and src_kind in binaries)
        if (not same_family or ('unsigned' in native.lower()) != ('unsigned' in src_native.lower())
                or kind == 'decimal' and (precision, scale) != (src_precision, src_scale)
                or kind in {'datetime', 'timestamp'} and temporal_precision != src_temporal):
            raise ValueError(f'MySQL target column type differs from the saved result: {name}')


def validate_target(connection, schema, target, stage, mode):
    facts = query(connection,
                  'SELECT TABLE_TYPE, ENGINE FROM information_schema.TABLES '
                  'WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?', (schema, target))
    if not facts:
        return False
    if facts != [('BASE TABLE', 'InnoDB')]:
        raise ValueError('MySQL save requires an ordinary InnoDB table target')
    validate_columns(columns(connection, schema, stage), columns(connection, schema, target))
    return True


def finalize(connection, schema, stage, target, field_names, mode):
    """Only stage DDL is allowed before the target's one publication transaction."""
    statement = connection.createStatement()
    lock_name = 'addp.spark.' + hashlib.sha256((schema.casefold() + '\x00' + target.casefold()).encode()).hexdigest()[:50]
    locked = False
    publishing = False
    transaction_started = False
    try:
        statement.execute("SET SESSION sql_mode = CONCAT_WS(',', @@sql_mode, 'STRICT_ALL_TABLES')")
        statement.execute('SET SESSION innodb_lock_wait_timeout = 30')
        statement.execute('SET SESSION lock_wait_timeout = 30')
        if query(connection, 'SELECT GET_LOCK(?, 30)', (lock_name,)) != [('1',)]:
            raise RuntimeError('MySQL result publication lock unavailable')
        locked = True
        stage_ref, target_ref = table_ref(schema, stage), table_ref(schema, target)
        staged = int(query(connection, f'SELECT COUNT(*) FROM {stage_ref}')[0][0])
        if not validate_target(connection, schema, target, stage, mode):
            publishing = True
            statement.execute(f'RENAME TABLE {stage_ref} TO {target_ref}')
            return staged
        # SERIALIZABLE COUNT takes range locks even for an empty target. Keep
        # the transaction's metadata lock before validating the actual columns.
        connection.setTransactionIsolation(8)  # java.sql.Connection.TRANSACTION_SERIALIZABLE
        connection.setAutoCommit(False)
        transaction_started = True
        previous = int(query(connection, f'SELECT COUNT(*) FROM {target_ref}')[0][0])
        validate_target(connection, schema, target, stage, mode)
        if mode == 'overwrite':
            if int(statement.executeLargeUpdate(f'DELETE FROM {target_ref}')) != previous:
                raise ValueError('MySQL deleted row count differs from target row count')
        projection = ', '.join(identifier(name) for name in field_names)
        inserted = int(statement.executeLargeUpdate(
            f'INSERT INTO {target_ref} ({projection}) SELECT {projection} FROM {stage_ref}'))
        if inserted != staged:
            raise ValueError('MySQL published row count differs from staging row count')
        expected = staged + (previous if mode == 'append' else 0)
        if int(query(connection, f'SELECT COUNT(*) FROM {target_ref}')[0][0]) != expected:
            raise ValueError('MySQL final row count differs from expected row count')
        publishing = True
        connection.commit()
        return inserted
    except Exception as error:
        if publishing:
            raise PublicationUncertain('MySQL publication response uncertain; do not retry; staging evidence retained') from error
        if transaction_started:
            connection.rollback()
        raise
    finally:
        if locked:
            try:
                query(connection, 'SELECT RELEASE_LOCK(?)', (lock_name,))
            except Exception:
                logger.warning('Unable to release MySQL publication lock; closing its connection')
        close_safely(statement)


def stage_definition(df, schema, stage, aliases):
    definitions = []
    kinds = {'byte': 'TINYINT', 'short': 'SMALLINT', 'integer': 'INT', 'long': 'BIGINT',
             'float': 'FLOAT', 'double': 'DOUBLE', 'boolean': 'BIT(1)', 'string': 'LONGTEXT',
             'binary': 'LONGBLOB', 'date': 'DATE', 'timestamp': 'TIMESTAMP(6)',
             'timestamp_ntz': 'DATETIME(6)'}
    for alias, field in zip(aliases, df.schema.fields):
        kind = field.dataType.typeName()
        native = kinds.get(kind)
        if kind == 'decimal':
            native = f'DECIMAL({field.dataType.precision},{field.dataType.scale})'
        if native is None:
            raise ValueError(f'MySQL Spark save does not support type: {kind}')
        definitions.append(identifier(alias) + ' ' + native + ('' if field.nullable else ' NOT NULL'))
    return f'CREATE TABLE {table_ref(schema, stage)} (' + ','.join(definitions) + ') ENGINE=InnoDB DEFAULT CHARSET=utf8mb4'


def save(df, params, jdbc_url, driver, conn_info, adapter):
    # Restrict UTC to this publication, leaving source JDBC reading unchanged.
    jdbc_url += ('&' if '?' in jdbc_url else '?') + 'connectionTimeZone=UTC&forceConnectionTimeZoneToSession=true'
    schema, target = params.get('schema', conn_info['database']), params['table']
    validate_names(schema, target, df.columns)
    if adapter._geometry_column_names(df):
        raise ValueError('MySQL Spark save does not support geometry columns')
    stage = '__addp_spark_stage_' + uuid.uuid4().hex
    prefix = '__addp_col_' + uuid.uuid4().hex[:16] + '_'
    aliases = [prefix + str(index) for index in range(len(df.columns))]
    uncertain = False
    stage_created = False
    connection = None
    try:
        connection = adapter._open_jdbc_connection(df.sparkSession, jdbc_url, conn_info)
        statement = connection.createStatement()
        try:
            statement.execute(stage_definition(df, schema, stage, aliases))
            stage_created = True
            # Each Worker opens its own session. Do not depend on a server's
            # optional strict mode, or out-of-range values can be coerced before
            # the Driver ever validates/publishes the completed stage.
            worker_url = jdbc_url + '&' + urlencode({'sessionVariables': "sql_mode='STRICT_ALL_TABLES'"})
            adapter._write_jdbc(df.toDF(*aliases), worker_url, driver, conn_info, schema, stage, 'append')
            # Alias first to avoid Spark's unescaped MySQL column identifiers.
            for alias, name in zip(aliases, df.columns):
                statement.execute(f'ALTER TABLE {table_ref(schema, stage)} RENAME COLUMN {identifier(alias)} TO {identifier(name)}')
        finally:
            close_safely(statement)
        return finalize(connection, schema, stage, target, df.columns, params.get('mode', 'overwrite'))
    except PublicationUncertain:
        uncertain = True
        raise
    finally:
        if connection is not None:
            close_safely(connection)
        if stage_created and not uncertain:
            cleanup = None
            try:
                cleanup = adapter._open_jdbc_connection(df.sparkSession, jdbc_url, conn_info)
                statement = cleanup.createStatement()
                try:
                    statement.execute(f'DROP TABLE IF EXISTS {table_ref(schema, stage)}')
                finally:
                    close_safely(statement)
            except Exception:
                logger.warning('MySQL staging cleanup unverified: %s', stage)
            finally:
                if cleanup is not None:
                    close_safely(cleanup)
