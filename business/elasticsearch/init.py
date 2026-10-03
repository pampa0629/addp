#!/usr/bin/env python3
"""Initialize authenticated sample indices and a read-only Business principal."""
import base64
import json
import http.client
import os
import time
import urllib.error
import urllib.request


def request(endpoint, password, method, path, body=None, content_type='application/json'):
    payload = body if isinstance(body, bytes) else (json.dumps(body).encode() if body is not None else None)
    headers = {'Authorization': 'Basic ' + base64.b64encode(('elastic:' + password).encode()).decode(),
               'Content-Type': content_type}
    req = urllib.request.Request(endpoint.rstrip('/') + path, data=payload, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=10) as response:
        return json.load(response)


def initialize(endpoint, password, user, reader_password):
    for attempt in range(90):
        try:
            request(endpoint, password, 'GET', '/_cluster/health')
            break
        except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.RemoteDisconnected):
            if attempt == 89:
                raise RuntimeError('Elasticsearch did not become ready') from None
            time.sleep(2)
    mapping = {'settings': {'number_of_shards': 1, 'number_of_replicas': 0}, 'mappings': {'properties': {
        'order_id': {'type': 'long'}, 'customer': {'type': 'object', 'properties': {'name': {'type': 'keyword'}}},
        'items': {'type': 'nested', 'properties': {'sku': {'type': 'keyword'}, 'quantity': {'type': 'integer'}}},
        'description': {'type': 'text', 'fields': {'keyword': {'type': 'keyword'}}},
        'amount': {'type': 'double'}, 'tags': {'type': 'keyword'}, 'optional': {'type': 'keyword'}}}}
    for name in ('addp_orders.v1', 'addp_empty.v1'):
        try:
            request(endpoint, password, 'PUT', '/' + name, mapping)
        except urllib.error.HTTPError as error:
            # A rerun may update sample records, but must never delete an existing index.
            details = json.load(error)
            if error.code != 400 or details.get('error', {}).get('type') != 'resource_already_exists_exception':
                raise RuntimeError('Elasticsearch sample index initialization failed') from None
    lines = []
    for number in range(25):
        lines.append(json.dumps({'index': {'_index': 'addp_orders.v1', '_id': str(number)}}))
        document = {'order_id': 9007199254740993 + number, 'customer': {'name': 'customer-' + str(number)},
                    'items': [{'sku': 'SKU-001', 'quantity': number + 1}], 'amount': 10.5 + number,
                    'tags': ['sample', 'business'], 'description': 'Business sample order ' + str(number)}
        if number == 0:
            document['optional'] = None
        lines.append(json.dumps(document))
    result = request(endpoint, password, 'POST', '/_bulk?refresh=true',
                     ('\n'.join(lines) + '\n').encode(), 'application/x-ndjson')
    if result.get('errors'):
        raise RuntimeError('Elasticsearch sample bulk write failed')
    request(endpoint, password, 'PUT', '/_security/role/addp_business_reader',
            {'cluster': [], 'indices': [{'names': ['addp_*'], 'privileges': ['read', 'view_index_metadata']}]})
    request(endpoint, password, 'PUT', '/_security/user/' + user,
            {'password': reader_password, 'roles': ['addp_business_reader']})
    print('Elasticsearch samples and read-only principal initialized')


if __name__ == '__main__':
    initialize(os.environ['ELASTICSEARCH_ENDPOINT'], os.environ['ELASTICSEARCH_PASSWORD'],
               os.environ['ELASTICSEARCH_READER_USER'], os.environ['ELASTICSEARCH_READER_PASSWORD'])
