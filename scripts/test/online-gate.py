#!/usr/bin/env python3
"""Dispatch registered ADDP Online suites through one safety-checked entrypoint."""

from __future__ import annotations

import argparse
import fcntl
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import uuid
from contextlib import contextmanager
from dataclasses import dataclass, replace
from pathlib import Path
from typing import Iterator, Mapping
from urllib.parse import urlsplit


SUITE_PATTERN = re.compile(r"^[a-z][a-z0-9-]*$")
RUN_ID_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$")


class OnlineGateError(RuntimeError):
    pass


class OnlineStageError(OnlineGateError):
    def __init__(self, stage: str, error_code: str, message: str) -> None:
        super().__init__(message)
        self.stage = stage
        self.error_code = error_code


@dataclass(frozen=True)
class Suite:
    command: tuple[str, ...]
    services: tuple[tuple[str, str], ...]
    nightly: bool = False


# Only executable owner-maintained Online suites belong here. Do not register
# placeholders: an entry means the suite is ready for real Online acceptance.
SUITES: Mapping[str, Suite] = {
    "execution-events-retention": Suite(
        command=(sys.executable, "-m", "scripts.test.execution-events-retention-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL")),
    ),
    "execution-retention-hourly": Suite(
        command=(sys.executable, "-m", "scripts.test.execution-retention-hourly-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL")),
    ),
    "execution-events-pagination": Suite(
        command=(sys.executable, "-m", "scripts.test.execution-events-pagination-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"), ("meta", "META_URL"), ("monitor", "MONITOR_URL")),
    ),
    "execution-retention-readonly": Suite(
        command=(sys.executable, "-m", "scripts.test.execution-retention-readonly-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL")),
    ),
    "workflow-runtime-health-recovery": Suite(
        command=("bash", "-c", 'exec "$ADDP_ONLINE_SECRET_DIR/observer/bin/python" scripts/test/workflow-runtime-health-recovery-online.py'),
        services=(("system", "SYSTEM_URL"), ("gateway", "GATEWAY_URL")),
    ),
    "pg-source-authorization": Suite(
        command=(sys.executable, "-m", "scripts.test.pg-source-authorization-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("meta", "META_URL"), ("security", "SECURITY_URL"),
                  ("manager", "MANAGER_URL"), ("catalog", "CATALOG_URL"), ("transfer", "TRANSFER_URL")),
    ),
    "platform-administrator-governance": Suite(
        command=(sys.executable, "-m", "scripts.test.platform-administrator-governance-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL")),
    ),
    "platform-node-metrics": Suite(
        command=(sys.executable, "-m", "scripts.test.platform-node-metrics-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"), ("monitor", "MONITOR_URL")),
    ),
    "hdfs-spark-consumer-flow": Suite(
        command=(sys.executable, "scripts/test/hdfs-spark-consumer-flow-online.py"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"), ("meta", "META_URL"),
                  ("security", "SECURITY_URL"),
                  ("manager", "MANAGER_URL"), ("develop", "DEVELOP_URL")),
    ),
    "vector-topology-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.vector-topology-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "vector-pmtiles-workflow": Suite(
        command=(sys.executable, "-m", "scripts.test.vector-pmtiles-workflow-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-contour-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-contour-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-polygonize-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-polygonize-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "vector-zonal-statistics-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.vector-zonal-statistics-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-proximity-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-proximity-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-focal-mode-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-focal-mode-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-boundary-clean-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-boundary-clean-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-thin-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-thin-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-fill-depressions-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-fill-depressions-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-flow-direction-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-flow-direction-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-flow-accumulation-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-flow-accumulation-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-drainage-basins-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-drainage-basins-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-flow-length-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-flow-length-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-flow-accumulation-area-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-flow-accumulation-area-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-distance-to-stream-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-distance-to-stream-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-curvature-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-curvature-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-profile-curvature-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-profile-curvature-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-cut-fill-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-cut-fill-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-surface-area-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-surface-area-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-tangential-curvature-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-tangential-curvature-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-plan-curvature-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-plan-curvature-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-tpi-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-tpi-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-tri-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-tri-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-roughness-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-roughness-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-twi-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-twi-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-spi-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-spi-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-sieve-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-sieve-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-hand-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-hand-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-flow-length-upstream-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-flow-length-upstream-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-stream-order-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-stream-order-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-stream-shreve-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-stream-shreve-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-stream-to-vector-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-stream-to-vector-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-stream-link-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-stream-link-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-flow-accumulation-weighted-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-flow-accumulation-weighted-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-watershed-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-watershed-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-subcatchments-d8-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-subcatchments-d8-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-snap-pour-points-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-snap-pour-points-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-band-recommendation-workflow": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-band-recommendation-workflow-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-nibble-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-nibble-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-disk-pressure": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-disk-pressure-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-nibble-capacity": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-nibble-capacity-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-focal-statistics-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-focal-statistics-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-zonal-statistics-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-zonal-statistics-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "vector-rasterize-persistence": Suite(
        command=(sys.executable, "-m", "scripts.test.vector-rasterize-persistence-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "raster-workflow-capacity": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-workflow-capacity-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL")),
    ),
    "raster-workflow": Suite(
        command=(sys.executable, "-m", "scripts.test.raster-workflow-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("develop", "DEVELOP_URL"), ("meta", "META_URL"),
                  ("manager", "MANAGER_URL"), ("monitor", "MONITOR_URL"),
                  ("security", "SECURITY_URL")),
    ),
    "ftp-consumer-flow": Suite(
        command=(sys.executable, "-m", "scripts.test.ftp-consumer-flow-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"), ("meta", "META_URL"), ("manager", "MANAGER_URL"), ("security", "SECURITY_URL")),
    ),
    "redis-consumer-flow": Suite(
        command=(sys.executable, "scripts/test/redis-consumer-flow-online.py"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"), ("meta", "META_URL"), ("manager", "MANAGER_URL")),
    ),
    "elasticsearch-consumer-flow": Suite(
        command=(sys.executable, "scripts/test/elasticsearch-consumer-flow-online.py"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"), ("meta", "META_URL"), ("manager", "MANAGER_URL"), ("develop", "DEVELOP_URL")),
    ),
    "transfer-continuous-events": Suite(
        command=(sys.executable, "-m", "scripts.test.transfer-continuous-events-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("meta", "META_URL"), ("transfer", "TRANSFER_URL"), ("monitor", "MONITOR_URL"), ("security", "SECURITY_URL")),
    ),
    "orchestrator-execution": Suite(
        command=(sys.executable, "-m", "scripts.test.orchestrator-execution-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("meta", "META_URL"), ("orchestrator", "ORCHESTRATOR_URL"), ("monitor", "MONITOR_URL")),
    ),
    "compose-public-origin": Suite(
        command=(sys.executable, "scripts/test/compose-public-origin-online.py"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL")),
        nightly=True,
    ),
    "ontology-revision-lifecycle": Suite(
        command=(sys.executable, "-m", "scripts.test.ontology-revision-lifecycle-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"), ("ontology", "ONTOLOGY_URL")),
    ),
    "consumer-engine-recovery": Suite(
        command=(sys.executable, "scripts/test/consumer-engine-recovery-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("manager", "MANAGER_URL"),
            ("service", "SERVICE_URL"),
        ),
    ),
    "module-registry-recovery": Suite(
        command=(sys.executable, "scripts/test/module-registry-recovery-online.py"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL")),
    ),
    "oceanbase-consumer-flow": Suite(
        command=(sys.executable, "scripts/test/relational-consumer-flow-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("manager", "MANAGER_URL"),
            ("transfer", "TRANSFER_URL"),
            ("develop", "DEVELOP_URL"),
            ("service", "SERVICE_URL"),
        ),
    ),
    "starrocks-consumer-flow": Suite(
        command=(sys.executable, "-m", "scripts.test.readonly-sql-consumer-flow-online", "--engine-type", "starrocks"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"), ("meta", "META_URL"), ("manager", "MANAGER_URL"), ("security", "SECURITY_URL"), ("develop", "DEVELOP_URL")),
    ),
    "gaussdb-consumer-flow": Suite(
        command=(sys.executable, "-m", "scripts.test.readonly-sql-consumer-flow-online", "--engine-type", "gaussdb"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("meta", "META_URL"), ("manager", "MANAGER_URL"), ("security", "SECURITY_URL"), ("develop", "DEVELOP_URL")),
    ),
    "highgo-consumer-flow": Suite(
        command=(sys.executable, "-m", "scripts.test.highgo-consumer-flow-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
                  ("meta", "META_URL"), ("manager", "MANAGER_URL"), ("transfer", "TRANSFER_URL")),
    ),
    "opengauss-consumer-flow": Suite(
        command=(sys.executable, "scripts/test/relational-consumer-flow-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("manager", "MANAGER_URL"),
            ("transfer", "TRANSFER_URL"),
            ("develop", "DEVELOP_URL"),
            ("service", "SERVICE_URL"),
        ),
        nightly=True,
    ),
    "kingbase-consumer-flow": Suite(
        command=(sys.executable, "scripts/test/relational-consumer-flow-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("manager", "MANAGER_URL"),
            ("transfer", "TRANSFER_URL"),
            ("develop", "DEVELOP_URL"),
            ("service", "SERVICE_URL"),
        ),
    ),
    "tidb-consumer-flow": Suite(
        command=(sys.executable, "scripts/test/relational-consumer-flow-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("manager", "MANAGER_URL"),
            ("transfer", "TRANSFER_URL"),
            ("develop", "DEVELOP_URL"),
            ("service", "SERVICE_URL"),
        ),
    ),
    "manager-internal-artifact-lineage": Suite(
        command=(sys.executable, "-m", "scripts.test.manager-internal-artifact-lineage-online"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("security", "SECURITY_URL"),
            ("manager", "MANAGER_URL"),
            ("monitor", "MONITOR_URL"),
        ),
    ),
    "manager-hybrid-search": Suite(
        command=(sys.executable, "scripts/test/manager-hybrid-search-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("manager", "MANAGER_URL"),
            ("inference", "INFERENCE_URL"),
        ),
    ),
    "security-transfer-protection": Suite(
        command=(sys.executable, "scripts/test/security-transfer-protection-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("security", "SECURITY_URL"),
            ("graph", "GRAPH_URL"),
            ("transfer", "TRANSFER_URL"),
            ("manager", "MANAGER_URL"),
        ),
    ),
    "graph-protection": Suite(
        command=(sys.executable, "scripts/test/graph-protection-online.py"),
        services=tuple((owner, owner.upper() + "_URL") for owner in (
            "gateway", "system", "meta", "security", "graph", "manager", "develop", "service", "transfer",
        )),
    ),
    "cleanup-execution": Suite(
        command=(sys.executable, "-m", "scripts.test.cleanup-execution-online"),
        services=tuple((owner, owner.upper() + "_URL") for owner in (
            "gateway", "system", "meta", "manager", "transfer", "develop", "service",
            "quality", "graph", "asset", "model", "standard",
        )),
    ),
    "security-plaintext-access": Suite(
        command=(sys.executable, "scripts/test/security-plaintext-access-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("security", "SECURITY_URL"),
            ("graph", "GRAPH_URL"),
            ("manager", "MANAGER_URL"),
            ("develop", "DEVELOP_URL"),
        ),
    ),
    "security-mysql-owner-protection": Suite(
        command=(sys.executable, "scripts/test/security-mysql-owner-protection-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("security", "SECURITY_URL"),
            ("graph", "GRAPH_URL"),
            ("manager", "MANAGER_URL"),
            ("develop", "DEVELOP_URL"),
            ("service", "SERVICE_URL"),
            ("transfer", "TRANSFER_URL"),
            ("monitor", "MONITOR_URL"),
        ),
    ),
    "metric-service-revision-lifecycle": Suite(
        command=(sys.executable, "-m", "scripts.test.metric-service-revision-lifecycle-online"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("standard", "STANDARD_URL"),
            ("model", "MODEL_URL"),
            ("meta", "META_URL"),
            ("security", "SECURITY_URL"),
            ("service", "SERVICE_URL"),
        ),
        nightly=True,
    ),
    "quality-dynamic-binding": Suite(
        command=(sys.executable, "-m", "scripts.test.quality-dynamic-binding-online"),
        services=(
            ("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"),
            ("model", "MODEL_URL"), ("meta", "META_URL"),
            ("quality", "QUALITY_URL"), ("orchestrator", "ORCHESTRATOR_URL"),
            ("monitor", "MONITOR_URL"),
        ),
    ),
    "standard-model-reference-deletion": Suite(
        command=(sys.executable, "-m", "scripts.test.standard-model-reference-deletion-online"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("standard", "STANDARD_URL"),
            ("model", "MODEL_URL"),
        ),
    ),
    "transfer-insert-only-mysql": Suite(
        command=(sys.executable, "scripts/test/transfer-insert-only-mysql-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("transfer", "TRANSFER_URL"),
        ),
    ),
    "transfer-relational-sql-etl": Suite(
        command=(sys.executable, "scripts/test/transfer-relational-sql-etl-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("transfer", "TRANSFER_URL"),
            ("manager", "MANAGER_URL"),
            ("develop", "DEVELOP_URL"),
            ("orchestrator", "ORCHESTRATOR_URL"),
            ("monitor", "MONITOR_URL"),
            ("security", "SECURITY_URL"),
        ),
    ),
    "catalog-domain-overview": Suite(
        command=(sys.executable, "-m", "scripts.test.catalog-domain-overview-online"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"), ("standard", "STANDARD_URL"), ("model", "MODEL_URL"), ("catalog", "CATALOG_URL"), ("quality", "QUALITY_URL")),
    ),
    "enterprise-catalog-publishing": Suite(
        command=(sys.executable, "scripts/test/enterprise-catalog-publishing-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("catalog", "CATALOG_URL"),
            ("asset", "ASSET_URL"),
            ("portal", "PORTAL_URL"),
        ),
    ),
    "workbench-service-consumption": Suite(
        command=(sys.executable, "scripts/test/workbench-service-consumption-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("service", "SERVICE_URL"),
            ("workbench", "WORKBENCH_URL"),
        ),
    ),
}


def parse_positive_timeout(value: str) -> float:
    try:
        timeout = float(value)
    except ValueError as error:
        raise OnlineGateError("ADDP_ONLINE_TEST_TIMEOUT_SECONDS must be numeric") from error
    if timeout <= 0:
        raise OnlineGateError("ADDP_ONLINE_TEST_TIMEOUT_SECONDS must be greater than zero")
    return timeout


def resolve_run_id(environment: dict[str, str], *, execution: bool = False) -> str:
    run_id = environment.get("ADDP_ONLINE_TEST_RUN_ID")
    if not run_id:
        run_id = f"run-{uuid.uuid4().hex}"
        environment["ADDP_ONLINE_TEST_RUN_ID"] = run_id
    pattern = re.compile(r"[A-Za-z0-9_-]{1,100}") if execution else RUN_ID_PATTERN
    if not pattern.fullmatch(run_id):
        raise OnlineGateError("ADDP_ONLINE_TEST_RUN_ID must be filesystem-safe")
    return run_id


def _report_directory(repository: Path, environment: Mapping[str, str], run_id: str) -> Path:
    configured = environment.get("ADDP_ONLINE_ARTIFACT_DIR")
    if configured:
        directory = Path(configured)
        if not directory.is_absolute():
            raise OnlineGateError("ADDP_ONLINE_ARTIFACT_DIR must be an absolute path")
        resolved = directory.resolve()
        try:
            resolved.relative_to(repository.resolve())
        except ValueError:
            return resolved
        raise OnlineGateError("ADDP_ONLINE_ARTIFACT_DIR must be outside the repository")
    return Path(tempfile.gettempdir()) / "addp-online-reports" / run_id


def _write_report(path: Path, report: Mapping[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(".json.tmp")
    temporary.write_text(
        json.dumps(report, ensure_ascii=False, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    temporary.replace(path)


def _parse_stage_report(stage: str, stdout: str) -> dict[str, object]:
    lines = [line for line in stdout.splitlines() if line.strip()]
    if not lines:
        raise OnlineStageError(
            stage,
            f"online_{stage}_report_missing",
            f"{stage} did not emit a JSON report",
        )
    try:
        payload = json.loads(lines[-1])
    except json.JSONDecodeError as error:
        raise OnlineStageError(
            stage,
            f"online_{stage}_report_invalid",
            f"{stage} emitted invalid JSON",
        ) from error
    if not isinstance(payload, dict):
        raise OnlineStageError(
            stage,
            f"online_{stage}_report_invalid",
            f"{stage} report must be a JSON object",
        )
    return payload


def run_stage(
    stage: str,
    command: tuple[str, ...] | list[str],
    *,
    repository: Path,
    environment: Mapping[str, str],
    timeout: float,
) -> tuple[dict[str, object], int]:
    started_at = time.monotonic()
    try:
        result = subprocess.run(
            command,
            cwd=repository,
            env=environment,
            timeout=timeout,
            capture_output=True,
            text=True,
        )
    except subprocess.TimeoutExpired as error:
        raise OnlineStageError(
            stage,
            f"online_{stage}_timeout",
            f"{stage} exceeded the Online total timeout",
        ) from error
    if result.stdout:
        print(result.stdout, end="" if result.stdout.endswith("\n") else "\n")
    if result.stderr:
        print(
            result.stderr,
            end="" if result.stderr.endswith("\n") else "\n",
            file=sys.stderr,
        )
    if result.returncode != 0:
        raise OnlineStageError(
            stage,
            f"online_{stage}_failed",
            f"{stage} exited with status {result.returncode}",
        )
    duration_ms = round((time.monotonic() - started_at) * 1000)
    return _parse_stage_report(stage, result.stdout), duration_ms


@contextmanager
def online_run_lock(
    suite_name: str, run_id: str, lock_root: Path | None = None
) -> Iterator[None]:
    root = lock_root or Path(tempfile.gettempdir()) / "addp-online-locks"
    root.mkdir(parents=True, exist_ok=True)
    path = root / f"{suite_name}--{run_id}.lock"
    with path.open("a+", encoding="utf-8") as lock:
        try:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise OnlineStageError(
                "gate",
                "online_run_active",
                f"an Online run is already active for {suite_name} and {run_id}",
            ) from error
        try:
            yield
        finally:
            fcntl.flock(lock.fileno(), fcntl.LOCK_UN)


def _service_addresses(suite: Suite, environment: Mapping[str, str]) -> dict[str, str]:
    addresses: dict[str, str] = {}
    for module, variable in suite.services:
        parsed = urlsplit(environment[variable])
        addresses[module] = f"{parsed.scheme}://{parsed.hostname}:{parsed.port}"
    return addresses


EXECUTION_CHECKS = {
    "graph": {
        "pending_cancel_without_runtime", "cross_backend_cancel_durable_intent",
        "real_expiry_without_replay", "old_runtime_terminal_fenced",
        "unknown_reconcile_reopen_denied", "unknown_hold_survives_process_crash",
        "running_process_crash_without_replay",
    },
    "cleanup": {
        "ten_owner_scan_idempotence_and_private_results", "ten_owner_logical_execute_one_shot",
        "meta_only_execute_refused_without_request", "owner_outage_fails_closed_without_writes",
        "real_deadline_unclaimed_owner_rejection", "queued_original_user_revoked_before_claim",
        "browser_revocation_refresh_pagination_late_response",
    },
}
EXECUTION_OWNERS = {"meta", "manager", "transfer", "develop", "service", "quality", "graph", "asset", "model", "standard"}


def execution_profile(suite_name: str, environment: Mapping[str, str]) -> str | None:
    if suite_name == "cleanup-execution":
        return "cleanup"
    if suite_name == "graph-protection":
        case = environment.get("ADDP_ONLINE_GRAPH_CASE", "full")
        if case not in {"full", "endpoint-geometry", "endpoint-same-color", "execution-lifecycle"}:
            raise OnlineGateError("unsupported Graph acceptance case")
        if case == "execution-lifecycle":
            return "graph"
    return None


def validate_execution_report(profile: str, report: dict[str, object], environment: Mapping[str, str]) -> None:
    """Accept the frozen finite contract, never turn domain cleanup into destruction."""
    required = {"schema_version": f"addp.{profile}-execution-online/v1", "result": "passed",
                "control_cleanup": "passed", "hosted_destruction": "required"}
    if any(report.get(key) != value for key, value in required.items()):
        raise OnlineStageError("scenario", "online_execution_report_invalid", "execution report schema/result/recovery contract differs")
    checks = report.get("checks")
    if (not isinstance(checks, list) or any(type(x) is not str for x in checks)
            or len(checks) != len(set(checks)) or not EXECUTION_CHECKS[profile].issubset(checks)):
        raise OnlineStageError("scenario", "online_execution_checks_missing", "execution scenario checks are missing or duplicated")
    if profile == "graph":
        if (report.get("engine_type") != environment.get("ADDP_ONLINE_GRAPH_ENGINE")
                or report.get("engine_type") not in {"falkordb", "neo4j"}
                or report.get("controlled_extraction") is not True or report.get("real_model") is not False
                or report.get("reopen_success") != "blocked_provider_identity"):
            raise OnlineStageError("scenario", "online_execution_scope_invalid", "Graph execution scope/engine differs")
        crash = report.get("running_crash")
        if (not isinstance(crash, dict) or crash.get("result") != "passed" or type(crash.get("attempt")) is not int
                or crash.get("attempt") != 1 or any(crash.get(key) is not True for key in
                    ("live_attempt_observed", "no_replay", "unresolved_hold", "remote_return_terminal_unchanged"))
                or type(crash.get("graph_id")) is not int or crash["graph_id"] <= 0
                or type(report.get("graph_id")) is not int or report["graph_id"] <= 0
                or crash["graph_id"] == report["graph_id"]
                or type(crash.get("task_id")) is not int or crash["task_id"] <= 0):
            raise OnlineStageError("scenario", "online_execution_crash_invalid", "running Graph crash evidence is incomplete")
        for key in ("execution_id",):
            value = crash.get(key)
            try:
                valid = isinstance(value, str) and str(uuid.UUID(value)) == value and uuid.UUID(value).int != 0
            except ValueError:
                valid = False
            if not valid:
                raise OnlineStageError("scenario", "online_execution_crash_invalid", "running Graph crash identity is invalid")
    else:
        if report.get("uncertain_revocation") is not False or report.get("physical_effect_acceptance") != "not_run":
            raise OnlineStageError("scenario", "online_execution_scope_invalid", "Cleanup execution scope differs")
        rows = report.get("owner_results")
        expected = {(owner, action) for owner in EXECUTION_OWNERS for action in ("scan", "execute")}
        seen, identifiers = set(), set()
        if not isinstance(rows, list) or len(rows) != len(expected):
            raise OnlineStageError("scenario", "online_execution_owner_results_invalid", "Cleanup requires twenty Owner results")
        for row in rows:
            if not isinstance(row, dict):
                raise OnlineStageError("scenario", "online_execution_owner_results_invalid", "invalid Owner result")
            pair, execution = (row.get("module"), row.get("action")), row.get("execution_id")
            try:
                valid_id = isinstance(execution, str) and str(uuid.UUID(execution)) == execution and uuid.UUID(execution).int != 0
            except ValueError:
                valid_id = False
            if (pair not in expected or pair in seen or not valid_id or execution in identifiers
                    or any(row.get(key) is not True for key in ("receipt_result_equal", "other_user_denied", "get_no_write"))):
                raise OnlineStageError("scenario", "online_execution_owner_results_invalid", "Owner result identity or assertions differ")
            seen.add(pair)
            identifiers.add(execution)
        browser = report.get("browser")
        identity_keys = ("run_id", "tenant_id", "principal_id", "request_id", "execution_id")
        if (not isinstance(browser, dict) or any(not isinstance(browser.get(key), str) or not browser[key] for key in identity_keys)
                or browser["run_id"] != environment.get("ADDP_ONLINE_TEST_RUN_ID")
                or browser["tenant_id"] != environment.get("ADDP_ONLINE_TEST_TENANT_ID")
                or browser["principal_id"] == report.get("principal_id")):
            raise OnlineStageError("scenario", "online_execution_browser_invalid", "Cleanup browser identity differs")
        expected_browser = dict(schema_version="addp.cleanup-execution-browser/v1", result="passed",
            **{key: browser[key] for key in identity_keys}, phases=[dict(phase=phase, real_pagination=True,
                result_read_before_revoke=True, real_denial=True, previous_view_cleared=True, late_result_rejected=True)
                for phase in ("refresh", "pagination")])
        if json.dumps(browser, sort_keys=True) != json.dumps(expected_browser, sort_keys=True):
            raise OnlineStageError("scenario", "online_execution_browser_invalid", "Cleanup browser report is incomplete or unsafe")
        for key in ("request_id", "execution_id"):
            try:
                valid = str(uuid.UUID(browser[key])) == browser[key] and uuid.UUID(browser[key]).int != 0
            except ValueError:
                valid = False
            if not valid:
                raise OnlineStageError("scenario", "online_execution_browser_invalid", "Cleanup browser UUID is invalid")


def validate_execution_archive(profile: str, report: dict[str, object], directory: Path,
                               environment: Mapping[str, str]) -> None:
    path = directory / f"{profile}-execution.json"
    try:
        if path.is_symlink() or not path.is_file() or path.stat().st_size > 1024 * 1024:
            raise ValueError("missing or unsafe report")
        archived = json.loads(path.read_text())
    except (OSError, ValueError) as error:
        raise OnlineStageError("scenario", "online_execution_archive_invalid", "execution report archive is missing or invalid") from error
    if archived != report:
        raise OnlineStageError("scenario", "online_execution_archive_invalid", "execution stdout and archived report disagree")
    validate_execution_report(profile, report, environment)
    if profile == "cleanup":
        browser_path = directory / "cleanup-browser.json"
        try:
            if browser_path.is_symlink() or not browser_path.is_file() or browser_path.stat().st_size >= 16384:
                raise ValueError("unsafe browser report")
            browser = json.loads(browser_path.read_text())
        except (OSError, ValueError) as error:
            raise OnlineStageError("scenario", "online_execution_browser_invalid", "Cleanup browser archive is missing") from error
        if json.dumps(browser, sort_keys=True) != json.dumps(report["browser"], sort_keys=True):
            raise OnlineStageError("scenario", "online_execution_browser_invalid", "Cleanup browser archives disagree")


def run_online(
    suite_name: str,
    repository: Path,
    environment: dict[str, str],
    suites: Mapping[str, Suite] = SUITES,
    lock_root: Path | None = None,
) -> dict[str, object]:
    if environment.get("ADDP_ONLINE_TEST") != "1":
        raise OnlineGateError("ADDP_ONLINE_TEST must be exactly 1")
    if not SUITE_PATTERN.fullmatch(suite_name):
        raise OnlineGateError("ONLINE_SUITE must use lowercase kebab-case")
    suite = suites.get(suite_name)
    if suite is None:
        available = ", ".join(sorted(suites)) or "none"
        raise OnlineGateError(f"unknown ONLINE_SUITE {suite_name!r}; registered suites: {available}")

    profile = execution_profile(suite_name, environment)
    if profile == "graph":
        suite = replace(suite, command=(sys.executable, "-m", "scripts.test.graph-execution-online"),
                        services=(*suite.services, ("graph-peer", "ADDP_ONLINE_GRAPH_PEER_URL")))

    if suite_name == 'manager-internal-artifact-lineage' and environment.get('ADDP_ONLINE_MANAGER_CASE') in {'kml-export', 'native-table-orc'}:
        suite = replace(suite, services=(*suite.services, ('transfer', 'TRANSFER_URL')))

    timeout = parse_positive_timeout(
        environment.get("ADDP_ONLINE_TEST_TIMEOUT_SECONDS", "900")
    )
    started_at = time.monotonic()
    run_id = resolve_run_id(environment, execution=bool(profile))
    report_path = _report_directory(repository, environment, run_id) / "online-report.json"
    preflight = [
        sys.executable,
        str(repository / "scripts/test/online-preflight.py"),
        "--repository",
        str(repository),
    ]
    if profile:
        preflight.extend(("--execution-profile", profile))
    for module, variable in suite.services:
        url = environment.get(variable)
        if not url:
            raise OnlineGateError(f"{suite_name} requires {variable}")
        preflight.extend(("--service", f"{module}={url}"))

    print(f"Online suite: {suite_name}")
    print(f"Online run ID: {run_id}")
    report: dict[str, object] = {
        "schema_version": "addp.online-gate/v1",
        "suite": suite_name,
        "scenario": suite_name,
        "run_id": run_id,
        "result": "failed",
        "failure_stage": "gate",
        "error_code": "online_gate_failed",
        "database": {
            "category": "dedicated-online",
            "name": environment.get("POSTGRES_DB", ""),
        },
        "service_addresses": {},
        "stages": {},
    }
    try:
        with online_run_lock(suite_name, run_id, lock_root):
            preflight_report, preflight_ms = run_stage(
                "preflight",
                preflight,
                repository=repository,
                environment=environment,
                timeout=timeout,
            )
            report["preflight"] = preflight_report
            report["service_addresses"] = _service_addresses(suite, environment)
            report["stages"] = {"preflight_duration_ms": preflight_ms}
            remaining = timeout - (time.monotonic() - started_at)
            if remaining <= 0:
                raise OnlineStageError(
                    "scenario",
                    "online_scenario_timeout",
                    "scenario exceeded the Online total timeout",
                )
            suite_report, scenario_ms = run_stage(
                "scenario",
                suite.command,
                repository=repository,
                environment=environment,
                timeout=remaining,
            )
            report["suite_report"] = suite_report
            if profile:
                validate_execution_archive(profile, suite_report, report_path.parent, environment)
            report["stages"] = {
                "preflight_duration_ms": preflight_ms,
                "scenario_duration_ms": scenario_ms,
            }
            report["result"] = "passed"
            report["failure_stage"] = None
            report["error_code"] = None
    except OnlineStageError as error:
        report["failure_stage"] = error.stage
        report["error_code"] = error.error_code
        raise
    finally:
        report["duration_ms"] = round((time.monotonic() - started_at) * 1000)
        _write_report(report_path, report)
        print(f"Online report: {report_path}")
    return report


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, required=True)
    parser.add_argument("--suite", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        run_online(args.suite, args.repository.resolve(), dict(os.environ))
    except OnlineGateError as error:
        print(f"Online gate failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
