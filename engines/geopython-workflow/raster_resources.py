"""Apply System's resource policy once per process, with bounded hot admission."""
from __future__ import annotations

import logging
import math
import os
import subprocess
import threading
from datetime import datetime, timezone
from pathlib import Path

from addp_common.client.raster_policy import RasterPolicyClient
from addp_common.client.runtime_registration import runtime_advertised_port
from addp_common.workflow_runtime.admission import AdmissionLimits, ExecutionAdmission, RuntimeBusy

logger = logging.getLogger(__name__)


class RasterPolicyUnavailable(RuntimeBusy):
    pass


def _number(path):
    try:
        value = int(path.read_text().strip())
        return value if 0 < value < (1 << 60) else None
    except (OSError, ValueError):
        return None


def _directories(root, relative):
    parts = Path(relative.lstrip('/')).parts
    if '..' in parts:
        return [root]
    current = root.joinpath(*parts)
    values = [root]
    while current != root:
        values.append(current)
        current = current.parent
    return values


def resource_observation(*, cgroup_root=Path('/sys/fs/cgroup'), proc_cgroup=Path('/proc/self/cgroup')):
    cpus = [float(os.cpu_count())] if os.cpu_count() else []
    try:
        cpus.append(float(len(os.sched_getaffinity(0))))
    except (AttributeError, OSError):
        pass
    memory = []
    try:
        pages, page_size = os.sysconf('SC_PHYS_PAGES'), os.sysconf('SC_PAGE_SIZE')
        if pages > 0 and page_size > 0:
            memory.append(pages * page_size)
    except (OSError, ValueError):
        if os.uname().sysname == 'Darwin':
            try:
                memory.append(int(subprocess.check_output(['sysctl', '-n', 'hw.memsize'], timeout=1)))
            except (OSError, ValueError, subprocess.SubprocessError):
                pass
    try:
        groups = [parts for line in proc_cgroup.read_text().splitlines() if len(parts := line.split(':', 2)) == 3]
    except OSError:
        groups = []
    for _, controllers, relative in groups:
        if not controllers:
            for directory in _directories(cgroup_root, relative):
                limit = _number(directory / 'memory.max')
                if limit:
                    memory.append(limit)
                try:
                    quota, period = (directory / 'cpu.max').read_text().split()
                    if quota != 'max' and int(quota) > 0 and int(period) > 0:
                        cpus.append(int(quota) / int(period))
                except (OSError, ValueError):
                    pass
        for controller in controllers.split(','):
            if controller not in {'cpu', 'memory'}:
                continue
            mount = cgroup_root / controller
            if controller == 'cpu' and not mount.is_dir():
                mount = cgroup_root / controllers
            for directory in _directories(mount, relative):
                if controller == 'memory':
                    limit = _number(directory / 'memory.limit_in_bytes')
                    if limit:
                        memory.append(limit)
                else:
                    quota, period = _number(directory / 'cpu.cfs_quota_us'), _number(directory / 'cpu.cfs_period_us')
                    if quota and period:
                        cpus.append(quota / period)
    cpu = min((value for value in cpus if value > 0), default=None)
    memory_limit = min((value for value in memory if value > 0), default=None)
    advice = None
    if cpu is not None and memory_limit is not None and memory_limit >= 512 * (1 << 20):
        running = min(2, max(1, math.floor(cpu)))
        advice = {'running': running, 'waiting': running,
                  'cache_mib': min(256, memory_limit // (32 * (1 << 20))),
                  'basis': 'cpu-and-memory-budget-heuristic'}
    return {'observed_at': datetime.now(timezone.utc).isoformat(), 'effective_cpu': cpu,
            'memory_limit_bytes': memory_limit, 'advice': advice}


def _integer(value, name, minimum, maximum):
    if type(value) is not int or not minimum <= value <= maximum:
        raise ValueError(f'Invalid raster policy {name}')
    return value


class RasterResources:
    def __init__(self, client_factory=None, *, poll=True):
        self.admission = ExecutionAdmission(AdmissionLimits(1, 0), AdmissionLimits(1, 0))
        self.admission.pause()
        self._lock = threading.RLock()
        self._client = None
        self._factory = client_factory or self._new_client
        self._policy = None
        self._cache_mib = None
        self._poll, self._poll_started = poll, False

    @staticmethod
    def _new_client():
        return RasterPolicyClient(os.getenv('SYSTEM_URL', 'http://localhost:8180'),
            os.getenv('GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET', ''),
            {'host': os.getenv('RUNTIME_HOST', 'localhost').strip(), 'protocol': os.getenv('PROTOCOL', 'http'),
             'port': runtime_advertised_port(int(os.getenv('PORT', '8099')))})

    def refresh(self):
        with self._lock:
            try:
                if self._client is None:
                    self._client = self._factory()
                response = self._client.fetch()
                p = response['policy']
                _integer(p['engine_id'], 'engine_id', 1, 2**63-1)
                _integer(p['version'], 'version', 1, 2**63-1)
                running, waiting = _integer(p['running'], 'running', 1, 64), _integer(p['waiting'], 'waiting', 0, 1024)
                cache = _integer(p['cache_mib'], 'cache_mib', 16, 65536)
                default = AdmissionLimits(_integer(p['default_tenant_running'], 'default_tenant_running', 1, running),
                                          _integer(p['default_tenant_waiting'], 'default_tenant_waiting', 0, waiting))
                tenants = {}
                for q in response['quotas']:
                    if _integer(q['engine_id'], 'quota engine_id', 1, 2**63-1) != p['engine_id']:
                        raise ValueError('Tenant quota belongs to a different engine')
                    tenant = _integer(q['tenant_id'], 'tenant_id', 1, 2**63-1)
                    _integer(q['version'], 'quota version', 1, 2**63-1)
                    if tenant in tenants:
                        raise ValueError('Duplicate tenant quota')
                    tenants[tenant] = AdmissionLimits(
                        default.running if q['running'] is None else _integer(q['running'], 'tenant running', 1, 64),
                        default.waiting if q['waiting'] is None else _integer(q['waiting'], 'tenant waiting', 0, 1024))
                if self._policy is not None and (p['engine_id'] != self._policy['engine_id'] or p['version'] < self._policy['version']):
                    raise ValueError('Raster policy identity/version changed unexpectedly')
                if self._cache_mib is None:
                    from osgeo import gdal
                    gdal.SetCacheMax(cache * (1 << 20))
                    self._cache_mib = cache
                self._policy = dict(p)
                self.admission.configure(AdmissionLimits(running, waiting), default, tenants)
                if self._poll and not self._poll_started:
                    self._poll_started = True
                    threading.Thread(target=self._poll_policy, daemon=True).start()
                return self.admission
            except Exception as exc:
                self.admission.pause()
                raise RasterPolicyUnavailable('Authoritative raster resource policy is unavailable') from exc

    def _poll_policy(self):
        event = threading.Event()
        unavailable = False
        while not event.wait(1):
            try:
                self.refresh()
                if unavailable:
                    logger.info('Raster policy recovered; admission is enabled')
                unavailable = False
            except RasterPolicyUnavailable:
                if not unavailable:
                    logger.warning('Raster policy unavailable; admission is paused')
                unavailable = True

    def snapshot(self):
        with self._lock:
            return {**resource_observation(), **self.admission.snapshot(),
                    'running_limit': self._policy['running'] if self._policy else None,
                    'waiting_limit': self._policy['waiting'] if self._policy else None,
                    'engine_id': self._policy['engine_id'] if self._policy else None,
                    'applied_version': self._policy['version'] if self._policy else None,
                    'cache_mib': self._cache_mib,
                    'pending_restart': bool(self._policy and self._policy['cache_mib'] != self._cache_mib)}


def requires_raster(metadata):
    return 'raster' in (metadata.get('attributes') or {}).get('resource_groups', [])
