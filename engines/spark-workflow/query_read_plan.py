"""Native catalog proof, held against DDL until the same Driver finishes.

Only local ordinary heap/InnoDB tables establish a complete read boundary.
Views, inheritance, RLS, external relations and failed proof stay unknown.
"""
from contextlib import contextmanager


def _relation_row(connection, sql, parameters):
    statement = connection.prepareStatement(sql)
    try:
        statement.setQueryTimeout(10)
        for index, value in enumerate(parameters, 1):
            statement.setString(index, value)
        result = statement.executeQuery()
        try:
            return (result.getString(2), result.getString(3)) if result.next() and result.getBoolean(1) else None
        finally:
            result.close()
    finally:
        statement.close()


def _relation(connection, source, schema, table):
    if source['engine_type'] == 'postgresql':
        return _relation_row(connection, """SELECT c.relkind='r' AND a.amname='heap'
            AND NOT c.relrowsecurity AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute att
                JOIN pg_catalog.pg_type typ ON typ.oid=att.atttypid
                JOIN pg_catalog.pg_namespace tn ON tn.oid=typ.typnamespace
                WHERE att.attrelid=c.oid AND att.attnum>0 AND NOT att.attisdropped AND tn.nspname<>'pg_catalog') AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_inherits i
                WHERE i.inhparent=c.oid OR i.inhrelid=c.oid), n.nspname, c.relname
            FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
            LEFT JOIN pg_catalog.pg_am a ON a.oid=c.relam WHERE n.nspname=? AND c.relname=?""", [schema, table])
    return _relation_row(connection, """SELECT TABLE_TYPE='BASE TABLE' AND ENGINE='InnoDB', TABLE_SCHEMA, TABLE_NAME
        FROM information_schema.tables WHERE TABLE_SCHEMA=? AND TABLE_NAME=?""", [schema, table])


def _hold(connection, source, schema, table):
    quote = '"' if source['engine_type'] == 'postgresql' else '`'
    name = '.'.join(quote + part.replace(quote, quote + quote) + quote for part in (schema, table))
    statement = connection.createStatement()
    try:
        statement.setQueryTimeout(10)
        if source['engine_type'] == 'postgresql':
            statement.execute('LOCK TABLE ONLY ' + name + ' IN ACCESS SHARE MODE')
        else:
            # SELECT-only accounts acquire transaction-scoped metadata locks.
            result = statement.executeQuery('SELECT * FROM ' + name + ' WHERE 1=0')
            result.close()
    finally:
        statement.close()


@contextmanager
def lock_read_plan(spark, catalogs, relations):
    sources = {source['alias']: source for source in catalogs}
    connections, paths, complete = {}, [], True
    try:
        for alias, schema, table in sorted(relations):
            source = sources[alias]
            if source['engine_type'] == 'postgresql':
                # The query-only catalog constrains metadata and every Worker
                # scan to ONLY; ACCESS SHARE alone cannot exclude new children.
                if spark.conf.get('spark.sql.catalog.' + alias) != 'com.addp.spark.query.ExactIdentifierDialects$OnlyPostgresCatalog':
                    complete = False
            if alias not in connections:
                driver = (spark._jvm.org.postgresql.Driver() if source['engine_type'] == 'postgresql'
                          else spark._jvm.com.mysql.cj.jdbc.Driver())
                properties = spark._jvm.java.util.Properties()
                properties.setProperty('user', source['user'])
                properties.setProperty('password', source['password'])
                url = source['url']
                if source['engine_type'] == 'postgresql':
                    url = spark._jvm.com.addp.spark.query.ExactIdentifierDialects.withBuiltinSearchPath(url)
                connection = driver.connect(url, properties)
                connection.setReadOnly(True)
                connection.setTransactionIsolation(2)  # READ_COMMITTED: recheck after lock.
                connection.setAutoCommit(False)
                connections[alias] = connection
            connection = connections[alias]
            physical = _relation(connection, source, schema, table)
            if physical is None:
                complete = False
                continue
            schema, table = physical
            _hold(connection, source, schema, table)
            if _relation(connection, source, schema, table) != physical:
                complete = False
                continue
            branch = 'schema' if source['engine_type'] == 'postgresql' else 'database'
            paths.append({'version': 'catalog.path/v1', 'engine_id': source['engine_id'], 'segments': [
                {'term': 'server', 'kind': 'server'},
                {'term': branch, 'kind': 'namespace', 'name': schema},
                {'term': 'table', 'kind': 'table', 'name': table}]})
        # Different SQL spellings can resolve to the same physical MySQL table.
        paths = list({(path['engine_id'], tuple((segment['term'], segment['kind'], segment.get('name', ''))
                      for segment in path['segments'])): path for path in paths}.values())
        # Same ordering as the shared catalog path canonicalizer for this scope.
        paths.sort(key=lambda path: (path['engine_id'], tuple((s['term'], s['kind'], s.get('name', '')) for s in path['segments'])))
        yield {'engine_read_scope': {'engine_ids': sorted({p['engine_id'] for p in paths})} if complete else None,
               'read_set': {'paths': paths} if complete else None}
    finally:
        failure = None
        for connection in connections.values():
            try:
                try:
                    connection.rollback()
                finally:
                    connection.close()
            except Exception as error:
                failure = error
        if failure is not None:
            raise failure
