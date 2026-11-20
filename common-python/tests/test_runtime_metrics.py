import os
import unittest
from datetime import datetime, timezone
from unittest.mock import patch

from addp_common.httpmetrics.runtime import start_workflow_metrics
from addp_common.workflow_runtime.execution import ExecutionRegistry, ExecutionSnapshot


class RuntimeMetricsTest(unittest.TestCase):
    def test_registered_workflows_render_their_own_active_executions(self):
        registry = ExecutionRegistry()
        for status in ['pending', 'running', 'completed']:
            registry.record(ExecutionSnapshot(status, status))
        started = datetime.now(timezone.utc)
        for kind in ('document', 'model3d', 'pointcloud', 'spark'):
            prefix = 'SPARK_WORKFLOW' if kind == 'spark' else kind.upper()
            with self.subTest(kind=kind), patch.dict(os.environ, {
                f'ADDP_{prefix}_METRICS_ENDPOINT': 'https://127.0.0.1:19881/metrics',
            }), patch('addp_common.httpmetrics.runtime.start_runtime_metrics') as source:
                start_workflow_metrics(kind, 'https://system', 'test', '{"engine_id":17}', started, registry)
                self.assertEqual(source.call_args.args[:5], (kind, 'https://system', 'test', 17, started.isoformat()))
                body = source.call_args.args[5]().decode().splitlines()
                self.assertIn(f'addp_{kind}_executions_pending 1', body)
                self.assertIn(f'addp_{kind}_executions_running 1', body)
                self.assertEqual(len(body), 4)
