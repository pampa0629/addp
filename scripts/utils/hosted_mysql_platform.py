"""Owned resources and actors shared by the limited MySQL Hosted checks."""

import importlib
import json
import os
import re
import subprocess

http = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
SuiteError = http.SuiteError


def command(args, *, input=None, env=None):
    return subprocess.run(args, input=input, env=env, text=True, check=True,
                          stdout=subprocess.PIPE, timeout=90).stdout.strip()


def require(condition, message):
    if not condition:
        raise SuiteError(message)


def login(name, password, principal, tenant):
    origin = os.environ['ADDP_PUBLIC_ORIGIN']
    client = http.GatewayClient(origin, '', 20)
    result = client.request('POST', '/api/v1/system/login', (200,), {'username': name, 'password': password},
                            headers={'Origin': origin, 'Authorization': ''}).payload
    require(result.get('next_action') == 'session_issued', f'{name} did not receive a session')
    client.token = result['session']['access_token']
    auth = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    require(auth['principal']['type'] == 'user' and auth['principal']['id'] == str(principal)
            and auth['context']['type'] == 'tenant' and auth['context']['tenant_id'] == str(tenant),
            f'{name} signed into the wrong tenant or principal')
    return client


def owned_container(identifier, project, service, network):
    require(re.fullmatch(r'[0-9a-f]{64}', identifier), f'{service}: invalid container identity')
    info, = json.loads(command(['docker', 'inspect', identifier]))
    labels = info['Config']['Labels']
    require(info['State']['Running'] and labels.get('com.docker.compose.project') == project
            and labels.get('com.docker.compose.service') == service
            and set(info['NetworkSettings']['Networks']) == {network}, f'{service}: foreign container')
    return info


def mysql_read(container, sql):
    return command(['docker', 'exec', '-i', '-e', 'MYSQL_PWD', container, 'mysql', '--protocol=tcp',
                    '-h127.0.0.1', '-u' + os.environ['ADDP_DATABASE_RUNTIME_USER'], '-NB'],
                   input="SET SESSION TRANSACTION READ ONLY;\nSET time_zone='+00:00';\n" + sql,
                   env={**os.environ, 'MYSQL_PWD': os.environ['ADDP_DATABASE_RUNTIME_PASSWORD']})
