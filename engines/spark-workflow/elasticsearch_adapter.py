"""Read a concrete ES index through the official distributed Spark connector."""
import base64
import json
import os
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlsplit, urlunsplit
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener


class _NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class ElasticsearchAdapter:
    @staticmethod
    def _connection(params):
        connection = params.get('connection_info')
        if not isinstance(connection, dict) or connection.get('engine_type') != 'elasticsearch':
            raise ValueError('Index loading requires an Elasticsearch connection')
        endpoint = connection.get('endpoint')
        if not isinstance(endpoint, str) or any(ord(c) <= 32 for c in endpoint):
            raise ValueError('Elasticsearch endpoint must be an HTTP origin')
        parsed = urlsplit(endpoint)
        if parsed.scheme == 'https':
            raise ValueError('Spark Elasticsearch loading does not support HTTPS')
        if (parsed.scheme != 'http' or not parsed.hostname or parsed.username is not None
                or parsed.password is not None or parsed.path not in ('', '/')
                or parsed.query or parsed.fragment):
            raise ValueError('Elasticsearch endpoint must be an HTTP origin')
        port = parsed.port if parsed.port is not None else 80
        if not 1 <= port <= 65535:
            raise ValueError('Elasticsearch endpoint port is invalid')
        host = parsed.hostname
        if host.lower() in {'localhost', '127.0.0.1', '::1'}:
            host = os.environ.get('SPARK_WORKFLOW_SHARED_HOST', '').strip()
            if not host or any(c in host for c in '/\\@?#, ') or any(ord(c) < 32 for c in host):
                raise ValueError('SPARK_WORKFLOW_SHARED_HOST is required for loopback Elasticsearch')
            rewritten = urlsplit('http://' + ('[' + host + ']' if ':' in host else host) + ':' + str(port))
            if rewritten.hostname != host or rewritten.port != port:
                raise ValueError('SPARK_WORKFLOW_SHARED_HOST must be a hostname or IP address')
        user, password = connection.get('user'), connection.get('password')
        if not isinstance(user, str) or not user or ':' in user or not isinstance(password, str) or not password:
            raise ValueError('Elasticsearch Basic credentials are required')
        origin = urlunsplit(('http', ('[' + host + ']' if ':' in host else host) + ':' + str(port), '', '', ''))
        return origin, host, port, user, password

    @staticmethod
    def _get(origin, path, user, password):
        authorization = base64.b64encode((user + ':' + password).encode()).decode()
        request = Request(origin + path, headers={'Authorization': 'Basic ' + authorization})
        try:
            # Do not forward source credentials through redirects or implicit proxies.
            with build_opener(ProxyHandler({}), _NoRedirect()).open(request, timeout=15) as response:
                return json.loads(response.read(32 * 1024 * 1024 + 1))
        except HTTPError as error:
            raise ValueError('Elasticsearch index preflight rejected (HTTP %d)' % error.code) from None
        except (URLError, OSError, ValueError):
            raise ValueError('Elasticsearch index preflight failed') from None

    @staticmethod
    def _array_fields(properties, fields):
        if not isinstance(fields, list) or any(not isinstance(field, str) for field in fields):
            raise ValueError('array_fields must be an array of Mapping field paths')
        if len(fields) != len(set(fields)):
            raise ValueError('array_fields must not repeat field paths')
        for field in fields:
            if not field or any(c in field for c in '*?,:/\\[]'):
                raise ValueError('array_fields requires literal Mapping field paths')
            current = properties
            definition = None
            for segment in field.split('.'):
                definition = current.get(segment)
                if not isinstance(definition, dict):
                    raise ValueError('array_fields must refer to a source field in Mapping')
                current = definition.get('properties', {})
            if definition.get('type') == 'nested':
                raise ValueError('nested Mapping fields already have array semantics')
        return ','.join(fields)

    @classmethod
    def load(cls, spark, params):
        origin, host, port, user, password = cls._connection(params)
        index = params.get('index')
        if (not isinstance(index, str) or not index or index.startswith('.')
                or index.lower() != index or any(c in index for c in '*?,:/\\#% ')
                or any(ord(c) < 32 for c in index)):
            raise ValueError('Elasticsearch requires an ordinary concrete index name')
        escaped = quote(index, safe='')
        resolved = cls._get(origin, '/_resolve/index/' + escaped + '?expand_wildcards=open', user, password)
        indices = resolved.get('indices', [])
        if (len(indices) != 1 or indices[0].get('name') != index
                or resolved.get('aliases') or resolved.get('data_streams')
                or indices[0].get('data_stream')
                or 'open' not in indices[0].get('attributes', [])
                or {'hidden', 'system', 'closed'}.intersection(indices[0].get('attributes', []))):
            raise ValueError('Elasticsearch loading requires an open concrete ordinary index')
        mapping = cls._get(origin, '/' + escaped + '/_mapping', user, password)
        if set(mapping) != {index}:
            raise ValueError('Elasticsearch Mapping does not match the concrete index')
        properties = mapping[index].get('mappings', {}).get('properties', {})
        arrays = cls._array_fields(properties, params.get('array_fields', []))
        options = {'es.nodes': host, 'es.port': str(port), 'es.nodes.wan.only': 'true',
                   'es.net.http.auth.user': user, 'es.net.http.auth.pass': password,
                   'es.index.read.allow.red.status': 'false', 'es.index.read.missing.as.empty': 'false',
                   'es.net.ssl': 'false', 'es.net.proxy.http.use.system.props': 'false',
                   'es.http.retries': '0', 'es.scroll.keepalive': '1m', 'es.scroll.size': '1000'}
        if arrays:
            options['es.read.field.as.array.include'] = arrays
        return spark.read.format('org.elasticsearch.spark.sql').options(**options).load(index)
