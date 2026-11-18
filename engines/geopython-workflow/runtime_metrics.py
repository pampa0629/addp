"""Process-local GeoPython facts and the privately renewed source declaration."""

import ipaddress
import logging
import math
import os
import socket
import threading
import time
from urllib.parse import urlsplit
from uuid import uuid4

import httpx

from addp_common.client.service_token import SyncOAuthServiceTokenSource
from addp_common.httpmetrics.source import start_private_source

logger = logging.getLogger(__name__)
METRICS = {
    'effective_cpu': 'effective_cpu_cores',
    'memory_limit_bytes': 'memory_limit_bytes',
    'running': 'raster_running',
    'waiting': 'raster_waiting',
    'enabled': 'raster_admission_enabled',
    'running_limit': 'raster_running_limit',
    'waiting_limit': 'raster_waiting_limit',
}


def render_metrics(snapshot):
    values = snapshot()
    rows = [f'addp_geopython_observed_at_seconds {int(time.time())}']
    for field, metric in METRICS.items():
        value = values[field]
        if value is None:
            continue
        if not math.isfinite(value) or value < 0:
            raise ValueError('invalid GeoPython metric')
        rows.append(f'addp_geopython_{metric} {int(value) if isinstance(value, bool) else value}')
    return ('\n'.join(rows)+'\n').encode()


def start(system_url, client_secret, engine_id, started_at, snapshot):
    endpoint = os.getenv('ADDP_GEOPYTHON_METRICS_ENDPOINT', '')
    if not endpoint:
        return
    try:
        listen = os.getenv('ADDP_GEOPYTHON_METRICS_LISTEN', '')
        if os.getenv('ADDP_GEOPYTHON_METRICS_CONTAINER_PORT'):
            # Compose starts the worker on the ADDP network. Bind its own address, not a wildcard.
            port = int(os.environ['ADDP_GEOPYTHON_METRICS_CONTAINER_PORT'])
            listen = f'{socket.gethostbyname(socket.gethostname())}:{port}'
        listen_ip = ipaddress.ip_address(urlsplit('http://' + listen).hostname or '')
        if not (listen_ip.is_private or listen_ip.is_loopback):
            raise ValueError('GeoPython source must bind a private address')
        parsed = urlsplit(endpoint)
        if (parsed.scheme != 'https' or not parsed.hostname or not parsed.port or parsed.path != '/metrics'
                or parsed.username is not None or parsed.query or parsed.fragment):
            raise ValueError('invalid GeoPython source endpoint')
        source = start_private_source(listen, endpoint, os.environ['ADDP_GEOPYTHON_METRICS_TLS_DIR'],
                                      lambda: render_metrics(snapshot))
    except (OSError, KeyError, ValueError):
        logger.warning('geopython_metrics_source_unavailable')
        return

    claim = {'instance_id': str(uuid4()), 'started_at': started_at, 'endpoint': endpoint}

    def lease():
        tokens = SyncOAuthServiceTokenSource(system_url, 'addp-geopython', client_secret, timeout=5.)
        url = f'{system_url.rstrip("/")}/api/v1/system/runtime/engines/{engine_id}/metrics'
        try:
            previous = True
            with httpx.Client(timeout=5., trust_env=False) as client:
                while source.thread.is_alive():
                    try:
                        token = tokens.platform_token()
                        response = client.put(url, json=claim, headers={'Authorization': 'Bearer '+token})
                        available = response.status_code == 204
                        if response.status_code == 401:
                            tokens.invalidate_platform(token)
                    except Exception:
                        available = False
                    if previous and not available:
                        logger.warning('geopython_metrics_lease_unavailable')
                    previous = available
                    source.thread.join(timeout=30)
        finally:
            tokens.close()

    threading.Thread(target=lease, daemon=True, name='geopython-metrics-lease').start()
