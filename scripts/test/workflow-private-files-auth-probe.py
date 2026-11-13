#!/usr/bin/env python3
"""Actual Runtime guards against a live disposable System OAuth/AuthContext server.

Only opaque credentials on stdin are used. No spatial operators, business source,
Runtime process, or deployment is created by this component integration probe.
"""
import json
import os
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path[:0] = [str(ROOT / 'common-python'), str(ROOT / 'engines/geopython-workflow')]

from flask import Flask
from private_file_api import PrivateFileAPI


def main():
    value = json.load(sys.stdin)
    os.environ['SYSTEM_URL'] = value['system_url']
    app = Flask('workflow-private-files-real-auth')
    PrivateFileAPI(app)
    headers = {'Authorization': 'Bearer ' + value['token']}
    base = '/api/private-files/91000000-0000-4000-8000-000000000293'
    expected = value['expected_status']
    with app.test_client() as client:
        # An authorized GET reaches lookup and returns 404; unauthorized traffic
        # is rejected by the actual creator guard before touching private files.
        for method, path in [('GET', base), ('DELETE', base), ('PUT', base + '/inputs/0'),
                             ('GET', base + '/outputs/0')]:
            response = client.open(path, method=method, headers=headers)
            assert response.status_code == expected, (method, response.status_code, response.get_json())
        response = client.put(base, json={}, headers=headers)
        assert response.status_code == (400 if expected == 404 else expected), response.get_json()
    if expected == 404:
        with app.test_request_context(headers=headers):
            assert PrivateFileAPI.creator() == (value['principal_id'], value['tenant_id'])
    print(json.dumps({'result': 'passed', 'expected_status': expected, 'routes_checked': 5}))


if __name__ == '__main__':
    main()
