"""Synchronous query adapter in the existing Spark Runtime, never a new service."""
from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from decimal import Decimal
import json
import math
import os
from pathlib import Path
import re
import sys
import threading
import time
from uuid import UUID, uuid4

from addp_common.auth import resolve_authorization_context
from flask import jsonify, request
import httpx
import requests

from query_guard import MAX_QUERY_BYTES
from query_messages import MESSAGES, message
from query_process import QueryDriverError, PreparedDriver
from spark_connector import cluster_master_url
from storage_adapters import DatabaseAdapter, resolve_storage_host
from system_client import ExecutionAccessError, get_execution_engine_access

MAX_BODY_BYTES = 1024 * 1024
MAX_TIMEOUT = 120
_slots = threading.BoundedSemaphore(2)
_queries = {}
_queries_lock = threading.Lock()


class QueryRequestError(ValueError):
    def __init__(self, code, status=400):
        self.code, self.status = code, status
        super().__init__(code)


def _positive_id(value):
    return type(value) is int and 0 < value <= 18446744073709551615


def _object(pairs):
    result = {}
    for name, value in pairs:
        if name in result:
            raise QueryRequestError('query_invalid')
        result[name] = value
    return result


def parse_request(raw):
    if len(raw) > MAX_BODY_BYTES:
        raise QueryRequestError('query_invalid', 413)
    try:
        def reject_constant(_value):
            raise ValueError('query_invalid')
        body = json.loads(raw, object_pairs_hook=_object, parse_constant=reject_constant)
        required = {'target_engine_id', 'execution_id', 'execution_authorization_id',
                    'source_engine_ids', 'query', 'language', 'options'}
        if not isinstance(body, dict) or not required <= set(body) <= required | {'object_tables'}:
            raise ValueError()
        if not _positive_id(body['target_engine_id']) or body.get('object_tables') not in (None, {}):
            raise ValueError()
        execution = UUID(body['execution_id'])
        authorization = body['execution_authorization_id']
        if (not execution.int or str(execution) != body['execution_id']
                or not isinstance(authorization, str)
                or not re.fullmatch(r'[1-9][0-9]{0,19}', authorization)
                or int(authorization) > 18446744073709551615):
            raise ValueError()
        sources = body['source_engine_ids']
        if (not isinstance(sources, list) or not 1 <= len(sources) <= 16
                or any(not _positive_id(source) for source in sources)
                or len(set(sources)) != len(sources) or body['target_engine_id'] in sources):
            raise ValueError()
        if (body['language'] != 'sql' or not isinstance(body['query'], str)
                or not body['query'].strip() or len(body['query'].encode('utf-8')) > MAX_QUERY_BYTES):
            raise ValueError()
        options = body['options']
        if (not isinstance(options, dict) or not set(options) <= {'read_only', 'limit', 'timeout', 'args'}
                or options.get('read_only') is not True):
            raise ValueError()
        limit, timeout = options.get('limit', 1000), options.get('timeout', MAX_TIMEOUT * 10**9)
        if type(limit) is not int or not 1 <= limit <= 10000 or type(timeout) is not int or timeout <= 0:
            raise ValueError()
        args = options.get('args', [])
        # The workbench does not advertise query parameters yet. The runtime's
        # verified primitive positional binding remains separate from named UI parameters.
        if (not isinstance(args, list) or len(args) > 1024
                or any(type(value) not in {str, int, bool, type(None)} for value in args)):
            raise ValueError()
        return body, limit, min(timeout / 10**9, MAX_TIMEOUT), args
    except QueryRequestError:
        raise
    except (ValueError, TypeError, AttributeError, UnicodeError, OverflowError):
        raise QueryRequestError('query_invalid') from None


def _source_alias(name):
    # Same protocol identifier rule as common/federatedquery.SanitizeIdentifier.
    if not isinstance(name, str):
        raise QueryRequestError('query_source_not_supported')
    alias = re.sub('[^a-zA-Z0-9_]', '_', name) or 'engine'
    return '_' + alias if alias[0].isdigit() else alias


def _identity(engine):
    return {key: engine[key] for key in ('id', 'tenant_id', 'engine_type', 'name', 'connection_info')}


def _http_result(result, limit):
    def invalid():
        raise QueryRequestError('query_result_invalid', 502)

    def fields(value, depth=0):
        known = {'unknown', 'int', 'bigint', 'float', 'double', 'decimal', 'bool', 'string', 'bytes', 'date', 'timestamp', 'array', 'json'}
        if not isinstance(value, list) or depth > 16:
            invalid()
        names = []
        for field in value:
            if (not isinstance(field, dict) or not isinstance(field.get('name'), str)
                    or field.get('type') not in known or type(field.get('nullable')) is not bool):
                invalid()
            names.append(field['name'])
            if field['type'] == 'json':
                fields(field.get('fields'), depth + 1)
            if field['type'] == 'array':
                fields([field.get('element')], depth + 1)
        if len(set(names)) != len(names): invalid()
        return names

    def value(field, item):
        if item is None: return None
        kind = field['type']
        if kind in {'float', 'double'}:
            # The private process parser preserves JSON decimals. Restore a
            # number only from the declared floating type, never by magnitude.
            if type(item) not in {int, float, Decimal}: invalid()
            number = float(item)
            if not math.isfinite(number): invalid()
            return number
        if kind == 'int':
            if type(item) is not int: invalid()
        elif kind == 'bool':
            if type(item) is not bool: invalid()
        elif kind in {'bigint', 'decimal', 'string', 'bytes', 'date', 'timestamp'}:
            if not isinstance(item, str): invalid()
        elif kind == 'array':
            if not isinstance(item, list): invalid()
            return [value(field['element'], child) for child in item]
        elif kind == 'json':
            return row(field['fields'], item)
        else: invalid()
        return item

    def row(schema, item):
        if not isinstance(item, dict) or set(item) != {field['name'] for field in schema}: invalid()
        return {field['name']: value(field, item[field['name']]) for field in schema}

    if (not isinstance(result, dict) or set(result) != {'columns', 'fields', 'rows'}
            or not isinstance(result['rows'], list) or len(result['rows']) > limit): invalid()
    if result['columns'] != fields(result['fields']): invalid()
    return dict(result, rows=[row(result['fields'], item) for item in result['rows']])


def prepare_authorized(body, tenant_id, limit, timeout, args):
    deadline = time.monotonic() + timeout
    authorization, execution = body['execution_authorization_id'], body['execution_id']

    def access(engine_id):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise QueryRequestError('query_timeout', 504)
        return get_execution_engine_access(authorization, execution, engine_id, tenant_id,
                                           timeout=min(10, remaining))

    ids = [body['target_engine_id'], *body['source_engine_ids']]
    accesses = [access(engine_id) for engine_id in ids]
    try:
        target = accesses[0]['engine']
        if target['engine_type'] != 'spark':
            raise ValueError()
        master = cluster_master_url(target['connection_info'])
        driver_host = os.getenv('SPARK_WORKFLOW_SHARED_HOST', '').strip()
        if not driver_host:
            raise QueryRequestError('query_runtime_unavailable', 503)
        catalogs = []
        for granted in accesses[1:]:
            engine = granted['engine']
            if engine['engine_type'] not in {'postgresql', 'mysql'}:
                raise ValueError()
            connection = dict(engine['connection_info'])
            connection['host'] = resolve_storage_host(connection['host'])
            url, driver = DatabaseAdapter._jdbc_config(engine['engine_type'], connection)
            catalogs.append({'engine_id': engine['id'], 'engine_type': engine['engine_type'], 'alias': _source_alias(engine['name']), 'url': url, 'driver': driver,
                             'user': connection['user'], 'password': connection.get('password', '')})
        expires = min(datetime.fromisoformat(granted['expires_at'].replace('Z', '+00:00')) for granted in accesses)
        budget = deadline - time.monotonic()
        if budget <= 0:
            raise QueryRequestError('query_timeout', 504)
        remaining = min(budget, (expires - datetime.now(timezone.utc)).total_seconds())
        if remaining <= 0:
            raise QueryRequestError('query_permission_denied', 403)
        driver_request = {'master_url': master, 'driver_host': driver_host, 'catalogs': catalogs,
                          'query': body['query'], 'args': args, 'limit': limit}
        # Reject invalid mounts and duplicate aliases before creating any process.
        from query_driver import validate_request
        validate_request(driver_request)
        frozen = [_identity(granted['engine']) for granted in accesses]
    except QueryRequestError:
        raise
    except (KeyError, ValueError, TypeError, AttributeError):
        raise QueryRequestError('query_source_not_supported') from None
    driver = PreparedDriver([sys.executable, str(Path(__file__).with_name('query_driver.py'))],
                            driver_request, timeout=remaining)
    try:
        if set(driver.facts) != {'engine_read_scope', 'read_set'}:
            raise QueryRequestError('query_result_invalid', 502)
        return {'driver': driver, 'access': access, 'ids': ids, 'frozen': frozen,
                'tenant': tenant_id, 'limit': limit,
                'sources': [{'engine_id': source['engine_id'], 'engine_type': source['engine_type']} for source in catalogs]}
    except Exception:
        driver.close()
        raise


def _recheck(entry):
    for engine_id, identity in zip(entry['ids'], entry['frozen']):
        if _identity(entry['access'](engine_id)['engine']) != identity:
            raise QueryRequestError('query_permission_denied', 403)


def _release(query_id):
    with _queries_lock:
        entry = _queries.pop(query_id, None)
    if entry is not None:
        entry['timer'].cancel()
        try:
            entry['driver'].close()
        finally:
            _slots.release()


def register_query_routes(app):
    @app.post('/api/v1/queries')
    @app.post('/api/v1/queries/<query_id>/execute')
    @app.delete('/api/v1/queries/<query_id>')
    def execute_query(query_id=None):
        def fail(code, status):
            return jsonify({'error': message(code, request.headers.get('Accept-Language', '')),
                            'error_code': code}), status

        header = request.headers.get('Authorization', '')
        if not header.startswith('Bearer ') or not header[7:].strip():
            return fail('query_authentication_required', 401)
        try:
            context = asyncio.run(resolve_authorization_context(
                os.getenv('SYSTEM_URL', 'http://localhost:8180'), header[7:].strip()))
        except httpx.HTTPStatusError as error:
            denied = error.response.status_code in {401, 403}
            return fail('query_authentication_required' if denied else 'query_authentication_unavailable',
                        401 if denied else 503)
        except Exception:
            return fail('query_authentication_unavailable', 503)
        if (context.principal_type != 'service_principal' or context.token_type != 'service_access_token'
                or context.client_id != 'addp-develop' or context.context_type != 'tenant'
                or not _positive_id(context.tenant_id) or not _positive_id(context.principal_id)):
            return fail('query_permission_denied', 403)
        acquired = False
        entry = None
        try:
            if query_id is not None:
                try:
                    if str(UUID(query_id)) != query_id:
                        raise ValueError()
                except (ValueError, TypeError):
                    raise QueryRequestError('query_permission_denied', 403)
                with _queries_lock:
                    entry = _queries.get(query_id)
                    if entry is None:
                        if request.method == 'DELETE':
                            return '', 204
                        raise QueryRequestError('query_permission_denied', 403)
                    if entry['tenant'] != context.tenant_id or entry['principal'] != context.principal_id:
                        raise QueryRequestError('query_permission_denied', 403)
                    if request.method == 'POST':
                        if request.mimetype != 'application/json' or request.stream.read(3) != b'{}':
                            raise QueryRequestError('query_invalid')
                        if entry['executing']:
                            raise QueryRequestError('query_permission_denied', 403)
                        entry['executing'] = True
                if request.method == 'DELETE':
                    _release(query_id)
                    return '', 204
                try:
                    _recheck(entry)
                    result = entry['driver'].execute()
                    if set(result) == {'error'}:
                        code = result['error']
                        raise QueryRequestError(code if code in MESSAGES else 'query_driver_failed')
                    result = _http_result(result, entry['limit'])
                    _recheck(entry)
                    return jsonify(result), 200
                finally:
                    _release(query_id)
            if request.mimetype != 'application/json':
                raise QueryRequestError('query_invalid', 415)
            body, limit, timeout, args = parse_request(request.stream.read(MAX_BODY_BYTES + 1))
            acquired = _slots.acquire(blocking=False)
            if not acquired:
                raise QueryRequestError('query_runtime_busy', 503)
            entry = prepare_authorized(body, context.tenant_id, limit, timeout, args)
            _recheck(entry)
            query_id = str(uuid4())
            entry.update(principal=context.principal_id, executing=False)
            entry['timer'] = threading.Timer(max(.001, entry['driver'].deadline - time.monotonic()), _release, args=(query_id,))
            entry['timer'].daemon = True
            with _queries_lock:
                _queries[query_id] = entry
            entry['timer'].start()
            acquired = False  # retained until execute/delete/expiry
            return jsonify(dict(entry['driver'].facts, query_id=query_id, sources=entry['sources'])), 201
        except QueryRequestError as error:
            return fail(error.code, error.status)
        except ExecutionAccessError as error:
            return fail('query_permission_denied' if error.status_code == 403 else 'query_runtime_unavailable',
                        error.status_code)
        except QueryDriverError as error:
            validation = {'query_invalid', 'query_source_not_supported', 'query_source_not_authorized',
                          'query_not_read_only', 'query_function_not_supported', 'query_complexity_exceeded'}
            if error.code in validation:
                return fail(error.code, 400)
            return fail('query_timeout' if error.code == 'query_timeout' else 'query_driver_failed',
                        504 if error.code == 'query_timeout' else 502)
        except (requests.RequestException, ValueError, OSError, KeyError, TypeError, AttributeError):
            return fail('query_runtime_unavailable', 503)
        finally:
            if acquired:
                if entry is not None:
                    entry['driver'].close()
                _slots.release()
