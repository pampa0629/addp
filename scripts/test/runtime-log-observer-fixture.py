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
repeated = os.environ.get('LOKI_TEST_OBSERVER_CASE') == 'repeated'
finished = threading.Event()
source_sequence = 0
health_sequence = 0
class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass
    def do_POST(self):
        global source_sequence, health_sequence
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
            assert data['sequence'] == source_sequence + 1
            source_sequence = data['sequence']
            if os.environ.get('LOKI_TEST_DISCOVERY_CASE') == 'metadata_missing':
                assert not data['complete'] and data['issues'] == [{'code':'metadata_missing','count':1}], data['issues']
            else:
                assert data['complete'] and data['issues'] == []
            assert all(s['host_node_name'] == 'observer-t2' and s['capture_started_at'] for s in data['sources'])
            assert 'message' not in json.dumps(data) and 'token' not in data
            suffix = '-'+str(data['sequence']) if repeated else ''
            (root / ('sources-'+data['boot_id']+suffix+'.json')).write_text(json.dumps(data))
            reply = {'accepted':True}
        elif self.path == '/api/v1/monitor/platform/log-observations':
            if self.headers.get('Authorization') != 'Bearer '+token:
                self.send_error(403); return
            data = json.loads(body)
            assert data['schema'] == 'addp.log-observation/v1' and data['node'] == 'observer-t2'
            assert data['sequence'] == health_sequence + 1 and data['sources_valid'] and data['housekeeping_valid']
            health_sequence = data['sequence']
            assert 'message' not in data and 'token' not in data
            suffix = '-'+str(data['sequence']) if repeated else ''
            name = root / ('observation-'+data['boot_id']+suffix+'.json')
            name.write_text(json.dumps(data))
            reply = {'accepted':True}
        else:
            self.send_error(404); return
        self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers()
        self.wfile.write(json.dumps(reply).encode())
        if repeated and self.path == '/api/v1/monitor/platform/log-observations' and health_sequence == 2:
            finished.set()
server = ThreadingHTTPServer(('0.0.0.0',0), Handler)
(root/'observer-port').write_text(str(server.server_port))
thread = threading.Thread(target=server.serve_forever, daemon=True)
thread.start()
endpoint = f'http://host.docker.internal:{server.server_port}'
environment = dict(os.environ, ADDP_HOST_NODE_NAME='observer-t2', LOG_OBSERVER_SERVICE_CLIENT_SECRET=secret, SYSTEM_URL=endpoint, MONITOR_URL=endpoint, ALLOY_URL='http://alloy:12345', LOKI_URL='http://runtime-log-api:3100')
command = ['docker', 'compose', '--env-file', '/dev/null', '-p', os.environ['LOKI_TEST_PROJECT'], '-f', os.environ['LOKI_TEST_COMPOSE'], 'run', '--rm', '-T', '--no-deps']
for key in ['ADDP_HOST_NODE_NAME','LOG_OBSERVER_SERVICE_CLIENT_SECRET','SYSTEM_URL','MONITOR_URL','ALLOY_URL','LOKI_URL','LOKI_READ_TOKEN']:
    command.extend(['-e', key])
owned_name = os.environ['LOKI_TEST_PROJECT']+'-observer-repeat'
if repeated:
    command.extend(['--name', owned_name])
command.extend(['runtime-log-pruner','observe'])
if not repeated:
    command.append('--once')
try:
    if repeated:
        before = set((Path(os.environ['LOKI_TEST_SOURCE'])/'runtime-probe').glob('*'))
        process = subprocess.Popen(command, env=environment, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        try:
            assert finished.wait(90), 'continuous observer did not report two samples'
            subprocess.run(['docker','stop','--time','10',owned_name], check=True, stdout=subprocess.DEVNULL, timeout=20)
            output, _ = process.communicate(timeout=20)
            result = subprocess.CompletedProcess(command, process.returncode, output)
            assert result.returncode == 0, 'continuous observer did not exit cleanly'
            new_sources = set((Path(os.environ['LOKI_TEST_SOURCE'])/'runtime-probe').glob('*')) - before
            assert len(new_sources) == 1, 'detection created a new receiver source'
            source = new_sources.pop()
            entries = [json.loads(line) for path in sorted(source.glob('*.jsonl')) for line in path.read_text().splitlines()]
            assert len(entries) == 2 and len({e['message'] for e in entries}) == 2
            assert [e['entry_id'] for e in entries] == [source.name+':1',source.name+':2']
            status = json.loads((source/'status.json').read_text())
            assert status['received'] == status['written'] == 2 and status['dropped'] == 0
            samples = [json.loads(path.read_text()) for path in root.glob('observation-*-*.json') if path.name.endswith(('-1.json','-2.json'))]
            assert len(samples) == 2 and all(s['probe_delivered'] and s['collector']['valid'] for s in samples)
            assert len({s['boot_id'] for s in samples}) == 1
            print('Real continuous observer: one source, two distinct delivered events, sequence 1/2 and clean stop passed')
        finally:
            if process.poll() is None:
                subprocess.run(['docker','stop','--time','5',owned_name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15)
                process.communicate(timeout=15)
    else:
        result = subprocess.run(command, env=environment, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60, text=True)
    if result.returncode:
        print(result.stdout.replace(secret, '[REDACTED]').replace(os.environ['LOKI_READ_TOKEN'], '[REDACTED]')[-2000:], file=sys.stderr)
    sys.exit(result.returncode)
finally:
    server.shutdown(); server.server_close(); thread.join(timeout=5)
