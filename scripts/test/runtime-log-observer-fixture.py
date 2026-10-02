#!/usr/bin/env python3
"""Owned HTTP fixture for the real observer executable; no production control API."""
import base64
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import sys
import subprocess
import threading
from urllib.parse import parse_qs

root = Path(sys.argv[1])
secret = os.environ['LOKI_TEST_SECRET']
token = 'addp_at_observer_fixture'
class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass
    def do_POST(self):
        size = int(self.headers.get('Content-Length', '0'))
        if size < 1 or size > 524288:
            self.send_error(400); return
        body = self.rfile.read(size)
        if self.path == '/api/v1/system/oauth/token':
            expected = 'Basic ' + base64.b64encode(('addp-log-observer:'+secret).encode()).decode()
            data = parse_qs(body.decode())
            if self.headers.get('Authorization') != expected or data != {'grant_type':['client_credentials'], 'scope':['addp.api'], 'audience':['addp.api'], 'context_type':['platform']}:
                self.send_error(403); return
            reply = {'access_token':token, 'token_type':'Bearer', 'expires_in':300, 'scope':'addp.api'}
        elif self.path == '/api/v1/system/runtime/module-log-source-observations':
            if self.headers.get('Authorization') != 'Bearer '+token:
                self.send_error(403); return
            data = json.loads(body)
            assert data['schema'] == 'addp.log-sources/v2' and data['node'] == 'observer-t2'
            assert data['sequence'] == 1
            if os.environ.get('LOKI_TEST_DISCOVERY_CASE') == 'metadata_missing':
                assert not data['complete'] and data['issues'] == [{'code':'metadata_missing','count':1}], data['issues']
            else:
                assert data['complete'] and data['issues'] == []
            assert all(s['host_node_name'] == 'observer-t2' and s['capture_started_at'] for s in data['sources'])
            assert 'message' not in json.dumps(data) and 'token' not in data
            (root / ('sources-'+data['boot_id']+'.json')).write_text(json.dumps(data))
            reply = {'accepted':True}
        elif self.path == '/api/v1/monitor/platform/log-observations':
            if self.headers.get('Authorization') != 'Bearer '+token:
                self.send_error(403); return
            data = json.loads(body)
            assert data['schema'] == 'addp.log-observation/v1' and data['node'] == 'observer-t2'
            assert data['sequence'] == 1 and data['sources_valid'] and data['housekeeping_valid']
            assert 'message' not in data and 'token' not in data
            name = root / ('observation-'+data['boot_id']+'.json')
            name.write_text(json.dumps(data))
            reply = {'accepted':True}
        else:
            self.send_error(404); return
        self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers()
        self.wfile.write(json.dumps(reply).encode())
server = ThreadingHTTPServer(('0.0.0.0',0), Handler)
(root/'observer-port').write_text(str(server.server_port))
thread = threading.Thread(target=server.serve_forever, daemon=True)
thread.start()
endpoint = f'http://host.docker.internal:{server.server_port}'
environment = dict(os.environ, ADDP_HOST_NODE_NAME='observer-t2', LOG_OBSERVER_SERVICE_CLIENT_SECRET=secret, SYSTEM_URL=endpoint, MONITOR_URL=endpoint, ALLOY_URL='http://alloy:12345', LOKI_URL='http://runtime-log-api:3100')
command = ['docker', 'compose', '--env-file', '/dev/null', '-p', os.environ['LOKI_TEST_PROJECT'], '-f', os.environ['LOKI_TEST_COMPOSE'], 'run', '--rm', '-T', '--no-deps']
for key in ['ADDP_HOST_NODE_NAME','LOG_OBSERVER_SERVICE_CLIENT_SECRET','SYSTEM_URL','MONITOR_URL','ALLOY_URL','LOKI_URL','LOKI_READ_TOKEN']:
    command.extend(['-e', key])
command.extend(['runtime-log-pruner','observe','--once'])
try:
    result = subprocess.run(command, env=environment, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60, text=True)
    if result.returncode:
        print(result.stdout.replace(secret, '[REDACTED]').replace(os.environ['LOKI_READ_TOKEN'], '[REDACTED]')[-2000:], file=sys.stderr)
    sys.exit(result.returncode)
finally:
    server.shutdown(); server.server_close(); thread.join(timeout=5)
