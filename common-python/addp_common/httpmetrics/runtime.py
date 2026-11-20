"""Private metrics and renewable System identity of a running compute process."""

import ipaddress
import json
import time
import logging
import os
import socket
import threading
from urllib.parse import urlsplit
from uuid import uuid4
from datetime import timezone

import httpx

from addp_common.client.service_token import SyncOAuthServiceTokenSource
from addp_common.httpmetrics.source import start_private_source

logger = logging.getLogger(__name__)


def start_runtime_metrics(kind, system_url, client_secret, engine_id, started_at, render):
    prefix = f'ADDP_{kind.upper()}_METRICS_' if kind != 'spark' else 'ADDP_SPARK_WORKFLOW_METRICS_'
    endpoint = os.getenv(prefix + 'ENDPOINT', '')
    if not endpoint:
        return
    try:
        listen = os.getenv(prefix + 'LISTEN', '')
        if os.getenv(prefix + 'CONTAINER_PORT'):
            # Compose starts the worker on the ADDP network. Bind its own address, not a wildcard.
            port = int(os.environ[prefix + 'CONTAINER_PORT'])
            listen = f'{socket.gethostbyname(socket.gethostname())}:{port}'
        listen_ip = ipaddress.ip_address(urlsplit('http://' + listen).hostname or '')
        if listen_ip.is_unspecified or listen_ip.is_multicast or not (listen_ip.is_private or listen_ip.is_loopback):
            raise ValueError('runtime source must bind a private address')
        parsed = urlsplit(endpoint)
        if (parsed.scheme != 'https' or not parsed.hostname or not parsed.port or parsed.path != '/metrics'
                or parsed.username is not None or parsed.query or parsed.fragment):
            raise ValueError('invalid runtime source endpoint')
        source = start_private_source(listen, endpoint, os.environ[prefix + 'TLS_DIR'],
                                      render)
    except (OSError, KeyError, ValueError):
        logger.warning('%s_metrics_source_unavailable', kind)
        return

    claim = {'instance_id': str(uuid4()), 'started_at': started_at, 'endpoint': endpoint}

    def lease():
        tokens = SyncOAuthServiceTokenSource(system_url, 'addp-' + kind, client_secret, timeout=5.)
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
                        logger.warning('%s_metrics_lease_unavailable', kind)
                    previous = available
                    source.thread.join(timeout=30)
        finally:
            tokens.close()

    threading.Thread(target=lease, daemon=True, name=kind+'-metrics-lease').start()

def start_workflow_metrics(kind, system_url, client_secret, registration, started_at, executions):
    if not os.getenv("ADDP_SPARK_WORKFLOW_METRICS_ENDPOINT" if kind == "spark" else f"ADDP_{kind.upper()}_METRICS_ENDPOINT"):
        return
    try:
        engine_id = int(json.loads(registration)["engine_id"])
    except (KeyError, TypeError, ValueError):
        logger.warning("%s_metrics_missing_registered_engine", kind)
        return
    started = started_at.astimezone(timezone.utc)

    def render():
        running, pending = executions.active_counts()
        now = time.time()
        return (f"addp_{kind}_observed_at_seconds {int(now)}\n"
                f"addp_{kind}_uptime_seconds {int(now - started.timestamp())}\n"
                f"addp_{kind}_executions_running {running}\n"
                f"addp_{kind}_executions_pending {pending}\n").encode()

    start_runtime_metrics(kind, system_url, client_secret, engine_id, started.isoformat(), render)
