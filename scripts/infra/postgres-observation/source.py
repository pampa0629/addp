"""Fixed deployment-owned SQL Exporter configuration; no editable SQL route."""
import ipaddress
import json
import re
from urllib.parse import urlencode, urlunsplit

HBA_MARKER = 'ADDP PostgreSQL observation v1'
HBA_BEGIN = '# BEGIN ' + HBA_MARKER + ' '
HBA_END = '# END ' + HBA_MARKER + '\n'
HBA_LIMIT = 256 * 1024
HBA_NETWORKS = tuple(ipaddress.ip_network(value) for value in (
    '10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', 'fc00::/7',
    '127.0.0.0/8', '::1/128',
))


def _authentication_block(scope, database, networks):
    if (scope not in ('infra', 'business') or type(database) is not str
            or not re.fullmatch(r'[a-zA-Z0-9_]{1,63}', database)
            or type(networks) not in (list, tuple) or not 1 <= len(networks) <= 16):
        raise ValueError('Invalid PostgreSQL observation authentication declaration')
    parsed = []
    for value in networks:
        if type(value) is not str:
            raise ValueError('Invalid PostgreSQL observation source network')
        try:
            network = ipaddress.ip_network(value, strict=True)
        except ValueError:
            raise ValueError('Invalid PostgreSQL observation source network') from None
        if (str(network) != value or not any(network.version == parent.version and network.subnet_of(parent)
                                             for parent in HBA_NETWORKS)
                or any(network.version == other.version and network.overlaps(other) for other in parsed)):
            raise ValueError('Invalid PostgreSQL observation source network')
        parsed.append(network)
    canonical = sorted(parsed, key=lambda value: (value.version, int(value.network_address), value.prefixlen))
    declaration = {'scope': scope, 'database': database, 'networks': list(map(str, canonical))}
    user = 'addp_' + scope + '_observer'
    lines = [HBA_BEGIN + json.dumps(declaration, sort_keys=True, separators=(',', ':'))]
    # Quoting makes reserved database words literal names. "all" must never
    # become permission to connect to every database.
    lines += [f'host "{database}" {user} {network} scram-sha-256' for network in canonical]
    # PostgreSQL's unquoted all does not cover physical replication.
    lines += [f'local all {user} reject', f'local replication {user} reject']
    for network in ('0.0.0.0/0', '::/0'):
        lines += [f'host all {user} {network} reject', f'host replication {user} {network} reject']
    return '\n'.join(lines) + '\n' + HBA_END


def authentication_configuration(original, scope, networks, *, database='postgres'):
    """Render only a verified role-specific HBA prefix; never write or reload.

    Deployment owners must verify database role ownership, write atomically,
    reload and check real authentication before enabling a collector.
    """
    if type(original) is not str or '\x00' in original or len(original.encode('utf-8')) > HBA_LIMIT:
        raise ValueError('Invalid PostgreSQL authentication input')
    block = _authentication_block(scope, database, networks)
    rest = original
    if HBA_MARKER in original:
        if not original.startswith(HBA_BEGIN) or original.count(HBA_MARKER) != 2 or HBA_END not in original:
            raise ValueError('Unrecognized PostgreSQL observation authentication block')
        end = original.index(HBA_END) + len(HBA_END)
        try:
            declaration = json.loads(original.split('\n', 1)[0][len(HBA_BEGIN):])
            if type(declaration) is not dict or set(declaration) != {'scope', 'database', 'networks'}:
                raise ValueError()
            expected = _authentication_block(**declaration)
        except (TypeError, ValueError):
            raise ValueError('Invalid PostgreSQL observation authentication block') from None
        if (original[:end] != expected or declaration['scope'] != scope or declaration['database'] != database):
            raise ValueError('PostgreSQL observation authentication ownership or policy drift')
        rest = original[end:]
    # Do not silently take over explicit operator records for either account.
    logical = rest.replace('\\\r\n', '').replace('\\\n', '')
    for line in logical.splitlines():
        record, quoted = [], False
        for character in line:
            if character == '#' and not quoted:
                break
            if character == '"':
                quoted = not quoted
            record.append(character)
        if quoted:
            raise ValueError('Invalid PostgreSQL authentication quoting')
        if re.search(r'\baddp_(?:infra|business)_observer\b', ''.join(record)):
            raise ValueError('Unmanaged PostgreSQL observation authentication record')
    result = block + rest
    if len(result.encode('utf-8')) > HBA_LIMIT:
        raise ValueError('PostgreSQL authentication configuration exceeds budget')
    return result


METRICS = (
    ('sample_time_seconds', 'gauge', 'Database observation statement time, Unix seconds.'),
    ('server_start_time_seconds', 'gauge', 'PostgreSQL server start time, Unix seconds.'),
    ('track_counts_enabled', 'gauge', 'Whether cumulative PostgreSQL statistics are enabled.'),
    ('connections', 'gauge', 'Client connections excluding this observation session.'),
    ('active_connections', 'gauge', 'Client connections executing a query.'),
    ('idle_connections', 'gauge', 'Idle client connections outside transactions.'),
    ('idle_in_transaction_connections', 'gauge', 'Idle client connections inside transactions.'),
    ('active_waiting_connections', 'gauge', 'Active client connections waiting on an event.'),
    ('active_lock_waiting_connections', 'gauge', 'Active client connections waiting for heavyweight locks.'),
    ('active_io_waiting_connections', 'gauge', 'Active client connections waiting for IO.'),
    ('max_connections', 'gauge', 'Configured maximum PostgreSQL connections, including reserved slots.'),
    ('transactions_committed', 'counter', 'Non-template database transaction commits, including observation traffic.'),
    ('transactions_rolled_back', 'counter', 'Non-template database transaction rollbacks.'),
    ('deadlocks', 'counter', 'Non-template database deadlocks.'),
    ('database_bytes', 'gauge', 'Total non-template database size in bytes, without tenant attribution.'),
)


def configuration(scope, host, port, password, *, database='postgres'):
    if (scope not in ('infra', 'business') or type(password) is not str
            or not re.fullmatch('[0-9a-f]{64}', password) or type(host) is not str):
        raise ValueError('Invalid PostgreSQL observation credentials')
    try:
        address = ipaddress.ip_address(host)
    except ValueError:
        if not re.fullmatch(r'[a-z0-9]+(?:[-.][a-z0-9]+)*', host) or len(host) > 253:
            raise ValueError('Invalid PostgreSQL observation host') from None
        authority = host
    else:
        if (not address.is_private or address.is_unspecified or address.is_multicast
                or address.is_link_local or address.is_loopback):
            raise ValueError('PostgreSQL observation requires a private deployment-network target')
        authority = f'[{address}]' if address.version == 6 else str(address)
    if (type(port) is not int or not 1 <= port <= 65535
            or type(database) is not str or not re.fullmatch(r'[a-zA-Z0-9_]{1,63}', database)):
        raise ValueError('Invalid PostgreSQL observation target')
    uri = urlunsplit(('postgresql', f'addp_{scope}_observer:{password}@{authority}:{port}',
                     '/' + database, urlencode({'sslmode': 'disable', 'connect_timeout': 3}), ''))
    metrics = []
    for field, kind, help_text in METRICS:
        item = {'metric_name': 'addp_postgresql_' + field + ('_total' if kind == 'counter' else ''),
                'type': kind, 'help': help_text, 'values': [field], 'query_ref': 'snapshot'}
        if kind == 'counter':
            item['key_labels'] = ['counter_epoch']
        metrics.append(item)
    metrics.append({'metric_name': 'addp_postgresql_statistics_info', 'type': 'gauge',
                    'help': 'Non-template database set and statistics reset generation.',
                    'key_labels': ['statistics_generation'], 'static_value': 1, 'query_ref': 'snapshot'})
    # Avoid per-target synthetic health metrics. Upstream may still return 200
    # with scrape_errors_total on query failure; only a fresh fixed snapshot is
    # observation evidence. Do not cache, set sample timestamps or enable ping.
    return {
        'global': {'scrape_timeout': '4s', 'scrape_timeout_offset': '500ms', 'min_interval': '0s',
                   'max_connections': 1, 'max_idle_connections': 0},
        'target': {'data_source_name': uri, 'enable_ping': False, 'collectors': ['addp_postgresql']},
        'collectors': [{'collector_name': 'addp_postgresql', 'metrics': metrics, 'queries': [{
            'query_name': 'snapshot',
            'query': '''SELECT s.connections, s.active_connections, s.idle_connections,
 s.idle_in_transaction_connections, s.active_waiting_connections,
 s.active_lock_waiting_connections, s.active_io_waiting_connections, s.max_connections,
 s.transactions_committed, s.transactions_rolled_back, s.deadlocks, s.database_bytes,
 s.track_counts_enabled::integer AS track_counts_enabled, s.statistics_generation,
 EXTRACT(EPOCH FROM s.observed_at) AS sample_time_seconds,
 EXTRACT(EPOCH FROM s.server_started_at) AS server_start_time_seconds,
 pg_catalog.md5(s.statistics_generation || ':' || EXTRACT(EPOCH FROM s.server_started_at)::text) AS counter_epoch
FROM addp_observation.snapshot() s''',
        }]}],
    }
