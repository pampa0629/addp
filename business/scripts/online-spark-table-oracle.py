#!/usr/bin/env python3
"""Business-only physical oracle for the disposable HDFS/Spark Hosted T4."""
import argparse
import hashlib
import json
import os
import re
import subprocess

SUITE = 'hdfs-spark-consumer-flow'


def owned_query(engine, sql, headers=False):
    container = 'addp-hdfs-online-' + engine
    owner = subprocess.run(['docker', 'container', 'inspect', '--format',
        '{{index .Config.Labels "com.addp.online-fixture"}}', container],
        check=True, capture_output=True, text=True, timeout=15).stdout.strip()
    if owner != SUITE:
        raise ValueError('physical oracle refuses a foreign container')
    flags = ['--batch', '--raw', '--default-character-set=utf8mb4', '--connect-timeout=5']
    if not headers:
        flags.append('--skip-column-names')
    # The password remains inside the container, never in argv or artifacts.
    command = ['docker', 'exec', container, 'sh', '-c',
        'export MYSQL_PWD="${MYSQL_ROOT_PASSWORD:-}"; port=$1; shift; exec mysql -h127.0.0.1 -P"$port" -uroot "$@"',
        '_', '15438' if engine == 'mysql' else '9030', *flags, '-e', sql]
    return subprocess.run(command, check=True, capture_output=True, text=True, timeout=45).stdout.rstrip('\n')


def observe(engine, table, cleanup=False):
    if engine not in ('mysql', 'doris') or not re.fullmatch(r'spark_boundary_[a-f0-9]{16}', table):
        raise ValueError('physical oracle requires an exact owned target')
    ref = f'`spark_results`.`{table}`'
    if cleanup:
        owned_query(engine, f'DROP TABLE IF EXISTS {ref}')
        if owned_query(engine, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA='spark_results' AND TABLE_NAME='" + table + "'") != '0':
            raise ValueError('owned target remains after cleanup')
    prefix = '__addp_spark_stage_' if engine == 'mysql' else 'addp_spark_stage_'
    if owned_query(engine, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA='spark_results' AND LEFT(TABLE_NAME," + str(len(prefix)) + ")='" + prefix + "'") != '0':
        raise ValueError('Spark left staging evidence; cleanup is unverified')
    if cleanup:
        return {'target_absent': True, 'staging_tables': 0}
    cast = 'CHAR' if engine == 'mysql' else 'STRING'
    raw = owned_query(engine, f"SELECT id, IF(label IS NULL,1,0), IFNULL(HEX(label),''), IF(value IS NULL,1,0), IFNULL(CAST(value AS {cast}),'') FROM {ref} ORDER BY id")
    rows = []
    for line in raw.splitlines():
        identifier, null_label, label_hex, null_value, value = line.split('\t')
        rows.append({'id': identifier, 'label': None if null_label == '1' else bytes.fromhex(label_hex).decode(),
                     'value': None if null_value == '1' else value})
    columns = [line.split('\t')[:2] for line in owned_query(engine, 'DESC ' + ref).splitlines()]
    if [value[0] for value in columns] != ['id', 'label', 'value']:
        raise ValueError('physical target columns differ')
    kinds = [re.sub(r'\s+', '', value[1].lower()) for value in columns]
    if (not kinds[0].startswith('bigint') or kinds[1] not in ('longtext', 'text', 'string')
        or not re.fullmatch(r'decimal(?:v3|128)?\(38,18\)', kinds[2])):
        raise ValueError('physical exact field types differ')
    ddl = owned_query(engine, 'SHOW CREATE TABLE ' + ref)
    if engine == 'mysql':
        identity = owned_query(engine, "SELECT TABLE_ID FROM information_schema.INNODB_TABLES WHERE NAME='spark_results/" + table + "'")
    else:
        databases = owned_query(engine, "SHOW PROC '/dbs'", headers=True).splitlines()
        db_columns = databases[0].split('\t')
        ids = [line.split('\t')[db_columns.index('DbId')] for line in databases[1:]
               if line.split('\t')[db_columns.index('DbName')] == 'spark_results']
        if len(ids) != 1 or not ids[0].isdigit():
            raise ValueError('Doris database identity is ambiguous')
        tables = owned_query(engine, "SHOW PROC '/dbs/" + ids[0] + "'", headers=True).splitlines()
        table_columns = tables[0].split('\t')
        identities = [line.split('\t')[table_columns.index('TableId')] for line in tables[1:]
                      if line.split('\t')[table_columns.index('TableName')] == table]
        identity = identities[0] if len(identities) == 1 else ''
    if not identity.isdigit():
        raise ValueError('physical table identity is missing')
    return {'table_id': identity, 'ddl_sha256': hashlib.sha256(ddl.encode()).hexdigest(),
            'columns': columns, 'rows': rows, 'staging_tables': 0}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('engine', choices=('mysql', 'doris'))
    parser.add_argument('table')
    parser.add_argument('--cleanup', action='store_true')
    args = parser.parse_args()
    if (os.environ.get('GITHUB_ACTIONS') != 'true' or os.environ.get('RUNNER_OS') != 'Linux'
        or os.environ.get('ADDP_ONLINE_HOSTED') != '1' or os.environ.get('ADDP_ONLINE_HOST') != '1'
        or os.environ.get('ADDP_ONLINE_OWNER_MANAGED', '0') == '1' or os.uname().sysname != 'Linux'
        or os.uname().machine != 'x86_64'):
        raise ValueError('physical oracle requires disposable Hosted Linux x86_64')
    print(json.dumps(observe(args.engine, args.table, args.cleanup), ensure_ascii=False))


if __name__ == '__main__':
    main()
