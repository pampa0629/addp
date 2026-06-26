"""
Math Workflow Engine - 工作流执行测试

测试 DAG 工作流引擎的功能：拓扑排序、参数引用、循环依赖检测
"""

import pytest
from workflow_engine import MathWorkflowEngine, WorkflowInvalidError


class TestWorkflowEngine:
    """工作流引擎测试"""

    def test_simple_workflow(self):
        """测试简单工作流：(5 + 3) × 2 = 16"""
        workflow_def = {
            "tasks": [
                {
                    "id": "t1",
                    "operator": "add",
                    "params": {"a": 5, "b": 3},
                    "depends_on": []
                },
                {
                    "id": "t2",
                    "operator": "multiply",
                    "params": {"a": {"$ref": "t1"}, "b": 2},
                    "depends_on": ["t1"]
                }
            ]
        }

        engine = MathWorkflowEngine()
        engine.load_workflow(workflow_def)
        results = engine.execute()

        assert results['t1'] == 8
        assert results['t2'] == 16

    def test_complex_workflow(self):
        """测试复杂工作流：((10 + 20) × 2 - 5) / 5 = 11"""
        workflow_def = {
            "tasks": [
                {
                    "id": "t1",
                    "operator": "add",
                    "params": {"a": 10, "b": 20},
                    "depends_on": []
                },
                {
                    "id": "t2",
                    "operator": "multiply",
                    "params": {"a": {"$ref": "t1"}, "b": 2},
                    "depends_on": ["t1"]
                },
                {
                    "id": "t3",
                    "operator": "subtract",
                    "params": {"a": {"$ref": "t2"}, "b": 5},
                    "depends_on": ["t2"]
                },
                {
                    "id": "t4",
                    "operator": "divide",
                    "params": {"a": {"$ref": "t3"}, "b": 5},
                    "depends_on": ["t3"]
                }
            ]
        }

        engine = MathWorkflowEngine()
        engine.load_workflow(workflow_def)
        results = engine.execute()

        assert results['t1'] == 30
        assert results['t2'] == 60
        assert results['t3'] == 55
        assert results['t4'] == 11

    def test_parallel_tasks(self):
        """测试并行任务：两个独立任务可以同时执行（拓扑排序）"""
        workflow_def = {
            "tasks": [
                {
                    "id": "t1",
                    "operator": "add",
                    "params": {"a": 10, "b": 20},
                    "depends_on": []
                },
                {
                    "id": "t2",
                    "operator": "multiply",
                    "params": {"a": 5, "b": 6},
                    "depends_on": []
                },
                {
                    "id": "t3",
                    "operator": "add",
                    "params": {"a": {"$ref": "t1"}, "b": {"$ref": "t2"}},
                    "depends_on": ["t1", "t2"]
                }
            ]
        }

        engine = MathWorkflowEngine()
        engine.load_workflow(workflow_def)
        task_order = engine.topological_sort()

        # t1 和 t2 可以按任意顺序执行，但必须在 t3 之前
        assert len(task_order) == 3
        assert task_order[2] == 't3'  # t3 必须最后执行
        assert set(task_order[:2]) == {'t1', 't2'}  # t1 和 t2 顺序不限

        results = engine.execute()
        assert results['t1'] == 30
        assert results['t2'] == 30
        assert results['t3'] == 60

    def test_average_in_workflow(self):
        """测试 average 算子在工作流中的使用"""
        workflow_def = {
            "tasks": [
                {
                    "id": "t1",
                    "operator": "add",
                    "params": {"a": 10, "b": 20},
                    "depends_on": []
                },
                {
                    "id": "t2",
                    "operator": "multiply",
                    "params": {"a": 5, "b": 4},
                    "depends_on": []
                },
                {
                    "id": "t3",
                    "operator": "subtract",
                    "params": {"a": 15, "b": 5},
                    "depends_on": []
                },
                {
                    "id": "t4",
                    "operator": "average",
                    "params": {"values": [30, 20, 10]},  # 手动传入数组
                    "depends_on": []
                }
            ]
        }

        engine = MathWorkflowEngine()
        engine.load_workflow(workflow_def)
        results = engine.execute()

        assert results['t1'] == 30
        assert results['t2'] == 20
        assert results['t3'] == 10
        assert results['t4'] == 20  # (30 + 20 + 10) / 3


class TestWorkflowValidation:
    """工作流验证测试"""

    def test_missing_tasks_field(self):
        """测试缺少 tasks 字段"""
        workflow_def = {}

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="缺少 'tasks' 字段"):
            engine.load_workflow(workflow_def)

    def test_empty_tasks(self):
        """测试 tasks 不能为空"""
        workflow_def = {"tasks": []}

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="必须是非空数组"):
            engine.load_workflow(workflow_def)

    def test_missing_task_id(self):
        """测试任务缺少 id 字段"""
        workflow_def = {
            "tasks": [
                {"operator": "add", "params": {"a": 5, "b": 3}}
            ]
        }

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="缺少 'id' 字段"):
            engine.load_workflow(workflow_def)

    def test_missing_operator(self):
        """测试任务缺少 operator 字段"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "params": {"a": 5, "b": 3}}
            ]
        }

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="缺少 'operator' 字段"):
            engine.load_workflow(workflow_def)

    def test_missing_params(self):
        """测试任务缺少 params 字段"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "depends_on": []}
            ]
        }

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="缺少 'params' 字段"):
            engine.load_workflow(workflow_def)

    def test_params_must_be_object(self):
        """测试 params 必须是对象"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "params": [], "depends_on": []}
            ]
        }

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="'params' 必须是对象"):
            engine.load_workflow(workflow_def)

    def test_duplicate_task_id_rejected(self):
        """测试重复任务 id 会被拒绝"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "params": {"a": 1, "b": 2}, "depends_on": []},
                {"id": "t1", "operator": "add", "params": {"a": 3, "b": 4}, "depends_on": []}
            ]
        }

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="任务 id 重复"):
            engine.load_workflow(workflow_def)

    def test_ref_dependency_must_be_declared(self):
        """测试 $ref 引用必须同步声明 depends_on"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "params": {"a": 1, "b": 2}, "depends_on": []},
                {"id": "t2", "operator": "multiply", "params": {"a": {"$ref": "t1"}, "b": 2}, "depends_on": []}
            ]
        }

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="未在 depends_on 中声明"):
            engine.load_workflow(workflow_def)

    def test_missing_depends_on(self):
        """测试任务缺少 depends_on 字段"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "params": {"a": 5, "b": 3}}
            ]
        }

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="缺少 'depends_on' 字段"):
            engine.load_workflow(workflow_def)

    def test_depends_on_must_be_array(self):
        """测试 depends_on 必须是数组"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "params": {"a": 5, "b": 3}, "depends_on": "t0"}
            ]
        }

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="'depends_on' 必须是数组"):
            engine.load_workflow(workflow_def)

    def test_depends_on_must_be_string_array(self):
        """测试 depends_on 元素必须是字符串"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "params": {"a": 5, "b": 3}, "depends_on": [1]}
            ]
        }

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="'depends_on' 必须是字符串数组"):
            engine.load_workflow(workflow_def)

    def test_circular_dependency(self):
        """测试循环依赖检测"""
        workflow_def = {
            "tasks": [
                {
                    "id": "t1",
                    "operator": "add",
                    "params": {"a": {"$ref": "t2"}, "b": 3},
                    "depends_on": ["t2"]
                },
                {
                    "id": "t2",
                    "operator": "multiply",
                    "params": {"a": {"$ref": "t1"}, "b": 2},
                    "depends_on": ["t1"]
                }
            ]
        }

        engine = MathWorkflowEngine()
        engine.load_workflow(workflow_def)

        with pytest.raises(WorkflowInvalidError, match="循环依赖"):
            engine.topological_sort()

    def test_missing_dependency(self):
        """测试依赖的任务不存在"""
        workflow_def = {
            "tasks": [
                {
                    "id": "t1",
                    "operator": "add",
                    "params": {"a": {"$ref": "t2"}, "b": 3},
                    "depends_on": ["t2"]  # t2 不存在
                }
            ]
        }

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="引用了不存在的任务 't2'"):
            engine.load_workflow(workflow_def)

    def test_invalid_ref(self):
        """测试无效的参数引用"""
        workflow_def = {
            "tasks": [
                {
                    "id": "t1",
                    "operator": "add",
                    "params": {"a": {"$ref": "t2"}, "b": 3},
                    "depends_on": []
                }
            ]
        }

        engine = MathWorkflowEngine()
        with pytest.raises(WorkflowInvalidError, match="引用了不存在的任务 't2'"):
            engine.load_workflow(workflow_def)


class TestTopologicalSort:
    """拓扑排序测试"""

    def test_linear_dependencies(self):
        """测试线性依赖：t1 → t2 → t3"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "params": {"a": 1, "b": 2}, "depends_on": []},
                {"id": "t2", "operator": "multiply", "params": {"a": {"$ref": "t1"}, "b": 2}, "depends_on": ["t1"]},
                {"id": "t3", "operator": "add", "params": {"a": {"$ref": "t2"}, "b": 5}, "depends_on": ["t2"]}
            ]
        }

        engine = MathWorkflowEngine()
        engine.load_workflow(workflow_def)
        task_order = engine.topological_sort()

        assert task_order == ['t1', 't2', 't3']

    def test_diamond_dependencies(self):
        """测试菱形依赖：t1 → {t2, t3} → t4"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "params": {"a": 10, "b": 5}, "depends_on": []},
                {"id": "t2", "operator": "multiply", "params": {"a": {"$ref": "t1"}, "b": 2}, "depends_on": ["t1"]},
                {"id": "t3", "operator": "subtract", "params": {"a": {"$ref": "t1"}, "b": 5}, "depends_on": ["t1"]},
                {"id": "t4", "operator": "add", "params": {"a": {"$ref": "t2"}, "b": {"$ref": "t3"}}, "depends_on": ["t2", "t3"]}
            ]
        }

        engine = MathWorkflowEngine()
        engine.load_workflow(workflow_def)
        task_order = engine.topological_sort()

        assert len(task_order) == 4
        assert task_order[0] == 't1'
        assert task_order[3] == 't4'
        assert set(task_order[1:3]) == {'t2', 't3'}


if __name__ == '__main__':
    pytest.main([__file__, '-v'])
