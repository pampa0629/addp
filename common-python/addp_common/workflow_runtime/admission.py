"""Process-local, bounded execution admission shared by workflows and direct calls."""
from __future__ import annotations

import logging
import threading
from collections import Counter, deque
from contextlib import contextmanager
from dataclasses import dataclass
from typing import Callable


class RuntimeBusy(RuntimeError):
    pass


@dataclass(frozen=True)
class AdmissionLimits:
    running: int
    waiting: int

    def __post_init__(self):
        if type(self.running) is not int or self.running < 1:
            raise ValueError("running must be a positive integer")
        if type(self.waiting) is not int or self.waiting < 0:
            raise ValueError("waiting must be a nonnegative integer")


class ExecutionAdmission:
    def __init__(self, limits: AdmissionLimits, tenant_default: AdmissionLimits):
        self._lock = threading.RLock()
        self._limits, self._default = limits, tenant_default
        self._tenants: dict[int, AdmissionLimits] = {}
        self._active: Counter = Counter()
        self._queue: deque = deque()
        self._enabled = True

    def configure(self, limits, tenant_default, tenants):
        with self._lock:
            self._limits, self._default, self._tenants = limits, tenant_default, dict(tenants)
            self._enabled = True
            self._drain()

    def pause(self):
        with self._lock:
            self._enabled = False

    def snapshot(self):
        with self._lock:
            return {"running": sum(self._active.values()), "waiting": len(self._queue),
                    "running_limit": self._limits.running, "waiting_limit": self._limits.waiting,
                    "enabled": self._enabled}

    def _quota(self, tenant):
        quota = self._tenants.get(tenant, self._default)
        return AdmissionLimits(min(quota.running, self._limits.running), min(quota.waiting, self._limits.waiting))

    def _available(self, tenant):
        return self._enabled and sum(self._active.values()) < self._limits.running and self._active[tenant] < self._quota(tenant).running

    def submit(self, tenant: int, job: Callable, *, thread_factory=threading.Thread, on_start_failure=None):
        if type(tenant) is not int or tenant <= 0:
            raise ValueError("tenant must be a positive integer")
        with self._lock:
            if not self._enabled:
                raise RuntimeBusy("Runtime policy is unavailable")
            entry = (tenant, job, thread_factory, on_start_failure)
            self._drain()
            if self._available(tenant):
                self._start(entry)
                return
            if len(self._queue) >= self._limits.waiting or sum(item[0] == tenant for item in self._queue) >= self._quota(tenant).waiting:
                raise RuntimeBusy("Runtime capacity is busy")
            self._queue.append(entry)

    def _start(self, entry):
        tenant, job, factory, _ = entry
        self._active[tenant] += 1

        def run():
            try:
                job()
            finally:
                with self._lock:
                    self._active[tenant] -= 1
                    self._drain()
        try:
            factory(target=run, args=(), daemon=True).start()
        except BaseException:
            self._active[tenant] -= 1
            raise

    def _drain(self):
        # Oldest eligible execution first; a full tenant must not block others.
        while self._enabled:
            eligible = next((entry for entry in self._queue if self._available(entry[0])), None)
            if eligible is None:
                return
            self._queue.remove(eligible)
            try:
                self._start(eligible)
            except Exception as exc:
                if eligible[3] is not None:
                    eligible[3](exc)
                else:
                    logging.getLogger(__name__).exception("Queued execution thread could not start")

    @contextmanager
    def direct(self, tenant):
        if type(tenant) is not int or tenant <= 0:
            raise ValueError("tenant must be a positive integer")
        with self._lock:
            self._drain()
            if not self._available(tenant):
                raise RuntimeBusy("Runtime capacity is busy")
            self._active[tenant] += 1
        try:
            yield
        finally:
            with self._lock:
                self._active[tenant] -= 1
                self._drain()
