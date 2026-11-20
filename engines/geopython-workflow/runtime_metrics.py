"""Process-local GeoPython facts and the privately renewed source declaration."""

import math
import time

from addp_common.httpmetrics.runtime import start_runtime_metrics

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
    start_runtime_metrics("geopython", system_url, client_secret, engine_id, started_at, lambda: render_metrics(snapshot))
