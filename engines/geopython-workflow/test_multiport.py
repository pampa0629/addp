#!/usr/bin/env python3
"""
多输出端口功能测试

测试 split_by_area 算子和端口引用机制。
"""

import json
import unittest

from workflow_engine import GeoPythonWorkflowRunner, execute_workflow


def build_multiport_workflow():
    return {
        "tasks": [
            {
                "id": "project_data",
                "operator": "vector_reproject",
                "params": {
                    "input_gdf": {
                        "type": "FeatureCollection",
                        "features": [
                            {
                                "type": "Feature",
                                "geometry": {
                                    "type": "Point",
                                    "coordinates": [116.404, 39.915],
                                },
                                "properties": {"name": "Point1"},
                            },
                            {
                                "type": "Feature",
                                "geometry": {
                                    "type": "Point",
                                    "coordinates": [116.414, 39.925],
                                },
                                "properties": {"name": "Point2"},
                            },
                        ],
                    },
                    "target_crs": "EPSG:3857",
                },
                "depends_on": [],
            },
            {
                "id": "load_data",
                "operator": "buffer",
                "params": {
                    "input_gdf": {"$ref": "project_data"},
                    "distance": 0.01,
                },
                "depends_on": ["project_data"],
            },
            {
                "id": "split_task",
                "operator": "split_by_area",
                "params": {
                    "input_gdf": {"$ref": "load_data"},
                    "threshold": 0.0003,
                },
                "depends_on": ["load_data"],
            },
            {
                "id": "process_large",
                "operator": "get_area",
                "params": {
                    "input_gdf": {"$ref": "split_task", "port": "large"},
                },
                "depends_on": ["split_task"],
            },
            {
                "id": "process_small",
                "operator": "get_area",
                "params": {
                    "input_gdf": {"$ref": "split_task", "port": "small"},
                },
                "depends_on": ["split_task"],
            },
        ],
    }


class MultiPortWorkflowTest(unittest.TestCase):
    def test_execute_workflow_resolves_named_output_ports(self):
        result = execute_workflow(build_multiport_workflow())

        self.assertEqual(result["status"], "success", result.get("error"))
        final_result = json.loads(result["final_result"])
        self.assertIn("features", final_result)

    def test_engine_keeps_split_outputs_by_port_name(self):
        from pyproj import CRS

        result = GeoPythonWorkflowRunner().execute(build_multiport_workflow())
        assert result.task_order[:3] == ['project_data', 'load_data', 'split_task']
        projected = json.loads(result.all_results['project_data'])
        assert CRS.from_user_input(projected['crs']['properties']['name']).to_epsg() == 3857
        assert set(json.loads(result.all_results['split_task'])) == {'large','small'}
        assert 'process_large' in result.all_results and 'process_small' in result.all_results



if __name__ == "__main__":
    unittest.main()
