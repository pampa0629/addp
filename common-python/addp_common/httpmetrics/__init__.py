"""Process-local full HTTP facts; source identity and queries have separate owners."""
from __future__ import annotations

import math
import os
import sys
import threading
import time
from prometheus_client import CollectorRegistry, generate_latest
from prometheus_client.core import CounterMetricFamily, GaugeMetricFamily, HistogramMetricFamily

BUCKETS = (.005, .01, .025, .05, .075, .1, .25, .5, .75, 1., 2.5, 5., 7.5, 10.)
MAX_COMBINATIONS = 512
MAX_OUTPUT = 4 << 20
MAX_APPLICATION = 16 << 20


class MetricsUnavailable(Exception):
    pass


class HTTPMetrics:
    def __init__(self):
        self.lock = threading.RLock()
        self.started = 0.
        self.rows = {}
        self.active = {}
        self.total = [0, 0., [0] * len(BUCKETS)]
        self.failures = self.client_errors = 0
        self.failed = False
        self.registry = CollectorRegistry(auto_describe=False)
        self.registry.register(self)

    def activate(self):
        with self.lock:
            if not self.started:
                self.started = time.time()

    def enter(self, method, scheme):
        with self.lock:
            key = (method, scheme)
            self.active[key] = self.active.get(key, 0) + 1

    def finish(self, method, scheme, route, status, error, elapsed):
        with self.lock:
            key = (method, scheme)
            self.active[key] -= 1
            self.failures += bool(error)
            self.client_errors += 400 <= status <= 499
            self._observe(self.total, elapsed)
            detailed = (method, scheme, route, str(status) if status else '', error)
            if len(route.encode('utf-8'))>512 or detailed not in self.rows and len(self.rows) >= MAX_COMBINATIONS:
                self.failed = True
            if not self.failed:
                self._observe(self.rows.setdefault(detailed, [0, 0., [0] * len(BUCKETS)]), elapsed)

    @staticmethod
    def _observe(row, elapsed):
        if not math.isfinite(elapsed) or elapsed < 0:
            raise MetricsUnavailable('http_metrics_duration_invalid')
        row[0] += 1
        row[1] += elapsed
        for index, bound in enumerate(BUCKETS):
            row[2][index] += elapsed <= bound

    def collect(self):
        with self.lock:
            if self.failed:
                raise MetricsUnavailable('http_metrics_source_incomplete')
            rows = [(key, (row[0], row[1], list(row[2]))) for key, row in self.rows.items()]
            total = (self.total[0], self.total[1], list(self.total[2]))
            active = list(self.active.items())
            failures, client_errors, started = self.failures, self.client_errors, self.started
        histogram = HistogramMetricFamily('http_server_request_duration_seconds', 'Completed HTTP exchange durations.',
            labels=['http_request_method', 'url_scheme', 'http_route', 'http_response_status_code', 'error_type'])
        def buckets(row):
            return [(str(bound), value) for bound, value in zip(BUCKETS, row[2])] + [('+Inf', row[0])]
        for labels, row in rows:
            histogram.add_metric(labels, buckets(row), row[1])
        aggregate = HistogramMetricFamily('addp_http_server_duration_seconds', 'All completed HTTP durations.')
        aggregate.add_metric([], buckets(total), total[1])
        activity = GaugeMetricFamily('http_server_active_requests', 'Currently active HTTP exchanges.', labels=['http_request_method','url_scheme'])
        for labels, value in active:
            activity.add_metric(labels, value)
        yield histogram
        yield aggregate
        yield activity
        for name, value in [('addp_http_server_requests', total[0]), ('addp_http_server_failures', failures),
                            ('addp_http_server_client_errors', client_errors)]:
            yield CounterMetricFamily(name, 'Completed transport facts.', value=value)
        yield GaugeMetricFamily('addp_http_server_instrumentation_start_time_seconds', 'Instrumentation start.', value=started)

    def render(self):
        with self.lock:
            # Bound the closed in-memory data and a conservative textual estimate
            # before the library allocates its serialization buffers.
            cache = sum(sys.getsizeof(k) + sum(sys.getsizeof(v) for v in k) +
                        sys.getsizeof(row) + sys.getsizeof(row[2]) + 512 for k, row in self.rows.items())
            estimate = sum((sum(len(value.encode('utf-8')) for value in k) + 256) * 18 for k in self.rows) + 16384
            if cache + 3 * estimate > MAX_APPLICATION or estimate > MAX_OUTPUT:
                self.failed = True
            if self.failed:
                raise MetricsUnavailable('http_metrics_source_incomplete')
        # Snapshotting takes the short data lock in collect(); encoding never
        # holds that lock across the business request's completion update.
        data = generate_latest(self.registry)
        with self.lock:
            if self.failed or len(data) > MAX_OUTPUT or cache + 3 * len(data) > MAX_APPLICATION:
                self.failed = True
                raise MetricsUnavailable('http_metrics_output_limit')
        return data


_lock = threading.Lock()
_process = None
_pid = None


def process():
    global _process, _pid
    with _lock:
        if _process is None or _pid != os.getpid():
            _process, _pid = HTTPMetrics(), os.getpid()
        return _process


def start_optional(registration):
    # Server-side transport is imported only by a server lifecycle, never CLI import.
    from .source import start_optional as start
    return start(registration)
