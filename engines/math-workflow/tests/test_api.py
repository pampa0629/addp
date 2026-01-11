"""
Math Workflow Engine - API 集成测试

测试所有 5 个标准 HTTP 端点的功能
"""

import pytest
import json


@pytest.fixture
def client():
    """创建 Flask 测试客户端"""
    from api_server import app
    app.config['TESTING'] = True

    with app.test_client() as client:
        yield client


class TestHealthEndpoint:
    """健康检查端点测试"""

    def test_health_check(self, client):
        """测试 GET /health"""
        response = client.get('/health')

        assert response.status_code == 200

        data = response.get_json()
        assert data['status'] == 'healthy'
        assert data['service'] == 'math-workflow-engine'
        assert data['version'] == '1.0.0'
        assert 'uptime' in data
        assert data['uptime'] >= 0
        assert data['operators_count'] == 5


class TestOperatorsEndpoint:
    """算子列表端点测试"""

    def test_get_operators(self, client):
        """测试 GET /api/operators"""
        response = client.get('/api/operators')

        assert response.status_code == 200

        data = response.get_json()
        assert data['status'] == 'success'
        assert 'operators' in data
        assert data['count'] == 5

        operator_names = [op['name'] for op in data['operators']]
        assert 'add' in operator_names
        assert 'subtract' in operator_names
        assert 'multiply' in operator_names
        assert 'divide' in operator_names
        assert 'average' in operator_names

    def test_get_operators_by_category(self, client):
        """测试按分类过滤算子"""
        response = client.get('/api/operators?category=数学运算')

        assert response.status_code == 200

        data = response.get_json()
        assert data['status'] == 'success'
        assert data['count'] == 4  # add, subtract, multiply, divide

    def test_operator_metadata_structure(self, client):
        """测试算子元数据结构"""
        response = client.get('/api/operators')
        data = response.get_json()

        add_operator = next(op for op in data['operators'] if op['name'] == 'add')

        assert add_operator['id'] == 'add'
        assert add_operator['name'] == 'add'
        assert add_operator['display_name'] == '加法'
        assert add_operator['category'] == '数学运算'
        assert add_operator['module'] == 'math-workflow'
        assert len(add_operator['parameters']) == 2
        assert len(add_operator['output_ports']) == 1


class TestSingleOperatorExecution:
    """单算子执行端点测试"""

    def test_execute_add(self, client):
        """测试 POST /api/operators/add/execute"""
        response = client.post(
            '/api/operators/add/execute',
            json={'params': {'a': 5, 'b': 3}},
            content_type='application/json'
        )

        assert response.status_code == 200

        data = response.get_json()
        assert data['status'] == 'success'
        assert 'execution_id' in data
        assert data['result'] == 8
        assert 'execution_time_ms' in data

    def test_execute_divide(self, client):
        """测试除法算子"""
        response = client.post(
            '/api/operators/divide/execute',
            json={'params': {'a': 10, 'b': 2}},
            content_type='application/json'
        )

        assert response.status_code == 200
        data = response.get_json()
        assert data['result'] == 5

    def test_execute_average(self, client):
        """测试平均值算子"""
        response = client.post(
            '/api/operators/average/execute',
            json={'params': {'values': [1, 2, 3, 4, 5]}},
            content_type='application/json'
        )

        assert response.status_code == 200
        data = response.get_json()
        assert data['result'] == 3.0

    def test_operator_not_found(self, client):
        """测试算子不存在"""
        response = client.post(
            '/api/operators/unknown/execute',
            json={'params': {}},
            content_type='application/json'
        )

        assert response.status_code == 404

        data = response.get_json()
        assert data['status'] == 'failed'
        assert data['error_code'] == 'OPERATOR_NOT_FOUND'

    def test_missing_params(self, client):
        """测试缺少参数"""
        response = client.post(
            '/api/operators/add/execute',
            json={},  # 缺少 params
            content_type='application/json'
        )

        assert response.status_code == 400

        data = response.get_json()
        assert data['status'] == 'failed'
        assert data['error_code'] == 'INVALID_PARAMS'

    def test_invalid_params(self, client):
        """测试无效参数"""
        response = client.post(
            '/api/operators/add/execute',
            json={'params': {'a': 5}},  # 缺少 b 参数
            content_type='application/json'
        )

        assert response.status_code == 400

        data = response.get_json()
        assert data['status'] == 'failed'
        assert data['error_code'] == 'INVALID_PARAMS'

    def test_divide_by_zero(self, client):
        """测试除零错误"""
        response = client.post(
            '/api/operators/divide/execute',
            json={'params': {'a': 10, 'b': 0}},
            content_type='application/json'
        )

        assert response.status_code == 500

        data = response.get_json()
        assert data['status'] == 'failed'
        assert data['error_code'] == 'EXECUTION_FAILED'


class TestWorkflowExecution:
    """工作流执行端点测试"""

    def test_simple_workflow(self, client):
        """测试简单工作流：(10 + 20) × 2 = 60"""
        workflow_def = {
            "tasks": [
                {
                    "id": "task1",
                    "operator": "add",
                    "params": {"a": 10, "b": 20},
                    "depends_on": []
                },
                {
                    "id": "task2",
                    "operator": "multiply",
                    "params": {"a": {"$ref": "task1"}, "b": 2},
                    "depends_on": ["task1"]
                }
            ]
        }

        response = client.post(
            '/api/workflow',
            json={'workflow_def': workflow_def},
            content_type='application/json'
        )

        assert response.status_code == 200

        data = response.get_json()
        assert data['status'] == 'success'
        assert 'execution_id' in data
        assert data['final_result'] == 60
        assert data['all_results']['task1'] == 30
        assert data['all_results']['task2'] == 60
        assert 'execution_time_ms' in data

    def test_complex_workflow(self, client):
        """测试复杂工作流：((10 + 20) × 2 - 5) / 5 = 11"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "params": {"a": 10, "b": 20}, "depends_on": []},
                {"id": "t2", "operator": "multiply", "params": {"a": {"$ref": "t1"}, "b": 2}, "depends_on": ["t1"]},
                {"id": "t3", "operator": "subtract", "params": {"a": {"$ref": "t2"}, "b": 5}, "depends_on": ["t2"]},
                {"id": "t4", "operator": "divide", "params": {"a": {"$ref": "t3"}, "b": 5}, "depends_on": ["t3"]}
            ]
        }

        response = client.post(
            '/api/workflow',
            json={'workflow_def': workflow_def},
            content_type='application/json'
        )

        assert response.status_code == 200

        data = response.get_json()
        assert data['final_result'] == 11
        assert len(data['all_results']) == 4

    def test_parallel_tasks_workflow(self, client):
        """测试并行任务工作流"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "params": {"a": 10, "b": 20}, "depends_on": []},
                {"id": "t2", "operator": "multiply", "params": {"a": 5, "b": 6}, "depends_on": []},
                {"id": "t3", "operator": "add", "params": {"a": {"$ref": "t1"}, "b": {"$ref": "t2"}}, "depends_on": ["t1", "t2"]}
            ]
        }

        response = client.post(
            '/api/workflow',
            json={'workflow_def': workflow_def},
            content_type='application/json'
        )

        assert response.status_code == 200

        data = response.get_json()
        assert data['final_result'] == 60  # (10+20) + (5*6) = 30 + 30 = 60

    def test_missing_workflow_def(self, client):
        """测试缺少 workflow_def"""
        response = client.post(
            '/api/workflow',
            json={},
            content_type='application/json'
        )

        assert response.status_code == 400

        data = response.get_json()
        assert data['status'] == 'failed'
        assert data['error_code'] == 'INVALID_PARAMS'

    def test_invalid_workflow(self, client):
        """测试无效工作流"""
        workflow_def = {
            "tasks": []  # 空任务列表
        }

        response = client.post(
            '/api/workflow',
            json={'workflow_def': workflow_def},
            content_type='application/json'
        )

        # 空任务列表会导致索引错误，应该返回 500
        assert response.status_code == 500

    def test_circular_dependency(self, client):
        """测试循环依赖检测"""
        workflow_def = {
            "tasks": [
                {"id": "t1", "operator": "add", "params": {"a": {"$ref": "t2"}, "b": 3}, "depends_on": ["t2"]},
                {"id": "t2", "operator": "multiply", "params": {"a": {"$ref": "t1"}, "b": 2}, "depends_on": ["t1"]}
            ]
        }

        response = client.post(
            '/api/workflow',
            json={'workflow_def': workflow_def},
            content_type='application/json'
        )

        assert response.status_code == 400

        data = response.get_json()
        assert data['status'] == 'failed'
        assert data['error_code'] == 'WORKFLOW_INVALID'


class TestExecutionStatusEndpoint:
    """执行状态查询端点测试"""

    def test_get_execution_status(self, client):
        """测试 GET /api/executions/{execution_id}"""
        response = client.get('/api/executions/test-uuid-1234')

        assert response.status_code == 200

        data = response.get_json()
        assert data['status'] == 'success'
        assert data['execution_id'] == 'test-uuid-1234'
        assert data['task_status'] == 'completed'


if __name__ == '__main__':
    pytest.main([__file__, '-v'])
