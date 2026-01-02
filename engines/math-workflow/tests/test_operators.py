"""
Math Workflow Engine - 算子单元测试

测试 5 个数学算子的功能正确性
"""

import pytest
from operators import add, subtract, multiply, divide, average


class TestBasicOperators:
    """基础算子测试"""

    def test_add(self):
        """测试加法算子"""
        assert add(5, 3) == 8
        assert add(0, 0) == 0
        assert add(-5, 3) == -2
        assert add(1.5, 2.5) == 4.0

    def test_subtract(self):
        """测试减法算子"""
        assert subtract(5, 3) == 2
        assert subtract(0, 0) == 0
        assert subtract(3, 5) == -2
        assert subtract(10.5, 5.5) == 5.0

    def test_multiply(self):
        """测试乘法算子"""
        assert multiply(5, 3) == 15
        assert multiply(0, 100) == 0
        assert multiply(-5, 3) == -15
        assert multiply(2.5, 4) == 10.0

    def test_divide(self):
        """测试除法算子"""
        assert divide(10, 2) == 5
        assert divide(7, 2) == 3.5
        assert divide(-10, 2) == -5

    def test_divide_by_zero(self):
        """测试除零错误"""
        with pytest.raises(ValueError, match="除数不能为零"):
            divide(10, 0)

    def test_average(self):
        """测试平均值算子"""
        assert average([1, 2, 3, 4, 5]) == 3.0
        assert average([10]) == 10.0
        assert average([1.5, 2.5, 3.0]) == pytest.approx(2.333, rel=0.01)

    def test_average_empty_list(self):
        """测试空列表错误"""
        with pytest.raises(ValueError, match="values 不能为空"):
            average([])


class TestOperatorMetadata:
    """算子元数据测试"""

    def test_operator_list(self):
        """测试算子列表"""
        from operators import list_operators

        operators = list_operators()
        assert len(operators) == 5

        operator_names = [op['name'] for op in operators]
        assert 'add' in operator_names
        assert 'subtract' in operator_names
        assert 'multiply' in operator_names
        assert 'divide' in operator_names
        assert 'average' in operator_names

    def test_operator_metadata_structure(self):
        """测试算子元数据结构"""
        from operators import get_operator_metadata

        metadata = get_operator_metadata('add')
        assert metadata.id == 'add'
        assert metadata.name == 'add'
        assert metadata.display_name == '加法'
        assert metadata.category == '数学运算'
        assert metadata.module == 'math-workflow'
        assert len(metadata.parameters) == 2
        assert len(metadata.output_ports) == 1

    def test_get_operator_function(self):
        """测试获取算子函数"""
        from operators import get_operator_function

        add_func = get_operator_function('add')
        assert callable(add_func)
        assert add_func(5, 3) == 8

        with pytest.raises(ValueError, match="算子 'unknown' 不存在"):
            get_operator_function('unknown')


if __name__ == '__main__':
    pytest.main([__file__, '-v'])
