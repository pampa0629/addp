"""
Spark 工作流算子元数据质量检查脚本

功能:
1. 检查元数据必填字段完整性
2. 检查 brief_description 和 detailed_description 质量
3. 检查 use_cases 是否包含具体场景和数字
4. 检查 workflow_example 格式是否符合 DAG 规范
5. 生成质量检查报告
"""

import sys
import os

# 添加当前目录到 Python 路径
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from typing import List, Dict, Any
import re


SOURCE_OPERATOR_IDS = {"load"}
VIEW_DEPENDENCY_OPERATOR_IDS = {"sql"}


class MetadataValidator:
    """元数据验证器"""

    def __init__(self):
        self.errors = []
        self.warnings = []
        self.stats = {
            "total": 0,
            "has_brief_description": 0,
            "has_detailed_description": 0,
            "has_quality_use_cases": 0,
            "has_workflow_example": 0
        }

    def validate_all(self, metadata_list: List[Dict[str, Any]]) -> Dict[str, Any]:
        """验证所有算子元数据"""
        self.stats["total"] = len(metadata_list)

        for operator in metadata_list:
            self._validate_operator(operator)

        return self._generate_report()

    def _validate_operator(self, operator: Dict[str, Any]):
        """验证单个算子元数据"""
        operator_id = operator.get("id", "unknown")

        # 1. 检查必填字段
        required_fields = ["id", "name", "display_name", "engine_type", "category", "category_path", "description", "parameters", "output_ports", "execution_modes", "effects"]
        for field in required_fields:
            if field not in operator:
                self.errors.append(f"[{operator_id}] 缺少必填字段: {field}")

        # 2. 检查 brief_description
        if "brief_description" not in operator:
            self.warnings.append(f"[{operator_id}] 缺少 brief_description (简要描述)")
        else:
            brief_desc = operator["brief_description"]
            if len(brief_desc) < 15:
                self.warnings.append(f"[{operator_id}] brief_description 过短 (< 15字): '{brief_desc}'")
            elif len(brief_desc) > 50:
                self.warnings.append(f"[{operator_id}] brief_description 过长 (> 50字): '{brief_desc}'")
            else:
                self.stats["has_brief_description"] += 1

        # 3. 检查 detailed_description
        if "detailed_description" not in operator:
            self.warnings.append(f"[{operator_id}] 缺少 detailed_description (详细描述)")
        else:
            self._validate_detailed_description(operator_id, operator["detailed_description"])

        # 4. 检查 parameters
        if "parameters" in operator:
            self._validate_parameters(operator_id, operator["parameters"])

        # 5. 检查 output_ports
        if "output_ports" in operator:
            self._validate_output_ports(operator_id, operator["output_ports"])

    def _validate_detailed_description(self, operator_id: str, detailed_desc: Dict[str, Any]):
        """验证详细描述"""
        required_fields = ["overview", "parameters", "use_cases", "notes", "input", "output", "workflow_example"]

        for field in required_fields:
            if field not in detailed_desc:
                self.warnings.append(f"[{operator_id}] detailed_description 缺少字段: {field}")

        # 检查 overview
        if "overview" in detailed_desc:
            overview = detailed_desc["overview"]
            if len(overview) < 30:
                self.warnings.append(f"[{operator_id}] overview 过短 (< 30字)")

        # 检查 use_cases 质量
        if "use_cases" in detailed_desc:
            use_cases = detailed_desc["use_cases"]
            if not isinstance(use_cases, list):
                self.errors.append(f"[{operator_id}] use_cases 必须是列表")
            elif len(use_cases) < 3:
                self.warnings.append(f"[{operator_id}] use_cases 数量过少 (< 3个), 建议 4-5 个")
            else:
                has_quality_use_cases = self._check_use_cases_quality(operator_id, use_cases)
                if has_quality_use_cases:
                    self.stats["has_quality_use_cases"] += 1
        else:
            self.warnings.append(f"[{operator_id}] 缺少 use_cases (使用场景)")

        # 检查 notes
        if "notes" in detailed_desc:
            notes = detailed_desc["notes"]
            if not isinstance(notes, list):
                self.errors.append(f"[{operator_id}] notes 必须是列表")
            elif len(notes) < 3:
                self.warnings.append(f"[{operator_id}] notes 数量过少 (< 3条), 建议 4-5 条")

        # 检查 workflow_example
        if "workflow_example" in detailed_desc:
            workflow_example = detailed_desc["workflow_example"]
            if self._validate_workflow_example(operator_id, workflow_example):
                self.stats["has_workflow_example"] += 1
        else:
            self.warnings.append(f"[{operator_id}] 缺少 workflow_example (工作流示例)")

        # 如果所有关键字段都存在,标记为完整
        if all(field in detailed_desc for field in required_fields):
            self.stats["has_detailed_description"] += 1

    def _check_use_cases_quality(self, operator_id: str, use_cases: List[str]) -> bool:
        """检查 use_cases 质量"""
        quality_count = 0

        for i, use_case in enumerate(use_cases):
            has_number = bool(re.search(r'\d+', use_case))  # 是否包含数字
            has_colon = ':' in use_case or '：' in use_case  # 是否包含冒号(场景:操作)
            has_detail = len(use_case) > 20  # 是否有足够详细的描述

            if not has_number:
                self.warnings.append(f"[{operator_id}] use_cases[{i}] 缺少具体数字: '{use_case[:50]}...'")
            if not has_colon:
                self.warnings.append(f"[{operator_id}] use_cases[{i}] 缺少场景分隔符(:): '{use_case[:50]}...'")
            if not has_detail:
                self.warnings.append(f"[{operator_id}] use_cases[{i}] 描述过短 (< 20字): '{use_case}'")

            if has_number and has_colon and has_detail:
                quality_count += 1

        # 至少一半的 use_cases 是高质量的
        return quality_count >= len(use_cases) / 2

    def _validate_workflow_example(self, operator_id: str, workflow_example: Dict[str, Any]) -> bool:
        """验证工作流示例格式"""
        required_fields = ["id", "operator", "params", "depends_on"]

        for field in required_fields:
            if field not in workflow_example:
                self.errors.append(f"[{operator_id}] workflow_example 缺少字段: {field}")
                return False

        # 检查 operator 字段是否匹配算子 id
        if workflow_example.get("operator") != operator_id:
            self.warnings.append(f"[{operator_id}] workflow_example.operator ('{workflow_example.get('operator')}') 与算子 id 不一致")

        # 检查 params 中是否有引用
        params = workflow_example.get("params", {})
        depends_on = workflow_example.get("depends_on", [])
        has_ref = False
        for value in params.values():
            if isinstance(value, dict) and "$ref" in value:
                has_ref = True
                break

        is_source_example = operator_id in SOURCE_OPERATOR_IDS and len(depends_on) == 0
        is_view_dependency_example = (
            operator_id in VIEW_DEPENDENCY_OPERATOR_IDS and len(depends_on) > 0
        )
        if (
            not has_ref
            and "input_df" not in params
            and "left_df" not in params
            and not is_source_example
            and not is_view_dependency_example
        ):
            self.warnings.append(f"[{operator_id}] workflow_example.params 缺少上游引用 ($ref)")

        return True

    def _validate_parameters(self, operator_id: str, parameters: List[Dict[str, Any]]):
        """验证参数定义"""
        for i, param in enumerate(parameters):
            if "name" not in param:
                self.errors.append(f"[{operator_id}] parameters[{i}] 缺少 name 字段")
            if "type" not in param:
                self.errors.append(f"[{operator_id}] parameters[{i}] 缺少 type 字段")
            if "description" not in param:
                self.warnings.append(f"[{operator_id}] parameters[{i}] ({param.get('name')}) 缺少 description")

    def _validate_output_ports(self, operator_id: str, output_ports: List[Dict[str, Any]]):
        """验证输出端口定义"""
        if not output_ports:
            self.errors.append(f"[{operator_id}] output_ports 为空")
            return

        has_default = False
        for i, port in enumerate(output_ports):
            if "name" not in port:
                self.errors.append(f"[{operator_id}] output_ports[{i}] 缺少 name 字段")
            if "type" not in port:
                self.errors.append(f"[{operator_id}] output_ports[{i}] 缺少 type 字段")
            if port.get("is_default"):
                has_default = True

        if not has_default:
            self.warnings.append(f"[{operator_id}] output_ports 中没有默认端口 (is_default=True)")

    def _generate_report(self) -> Dict[str, Any]:
        """生成检查报告"""
        return {
            "stats": self.stats,
            "errors": self.errors,
            "warnings": self.warnings,
            "completion": {
                "brief_description": f"{self.stats['has_brief_description']}/{self.stats['total']} ({self.stats['has_brief_description']*100//self.stats['total'] if self.stats['total'] > 0 else 0}%)",
                "detailed_description": f"{self.stats['has_detailed_description']}/{self.stats['total']} ({self.stats['has_detailed_description']*100//self.stats['total'] if self.stats['total'] > 0 else 0}%)",
                "quality_use_cases": f"{self.stats['has_quality_use_cases']}/{self.stats['total']} ({self.stats['has_quality_use_cases']*100//self.stats['total'] if self.stats['total'] > 0 else 0}%)",
                "workflow_example": f"{self.stats['has_workflow_example']}/{self.stats['total']} ({self.stats['has_workflow_example']*100//self.stats['total'] if self.stats['total'] > 0 else 0}%)"
            }
        }


def print_report(report: Dict[str, Any]):
    """打印检查报告"""
    print("=" * 80)
    print("Spark 工作流算子元数据质量检查报告")
    print("=" * 80)
    print()

    # 统计信息
    print("📊 统计信息:")
    print(f"  总算子数量: {report['stats']['total']}")
    print()

    # 完整性统计
    print("✅ 完整性统计:")
    completion = report['completion']
    print(f"  brief_description:     {completion['brief_description']}")
    print(f"  detailed_description:  {completion['detailed_description']}")
    print(f"  quality_use_cases:     {completion['quality_use_cases']}")
    print(f"  workflow_example:      {completion['workflow_example']}")
    print()

    # 错误信息
    if report['errors']:
        print(f"❌ 错误 ({len(report['errors'])}条):")
        for error in report['errors']:
            print(f"  - {error}")
        print()
    else:
        print("❌ 错误: 无")
        print()

    # 警告信息
    if report['warnings']:
        print(f"⚠️  警告 ({len(report['warnings'])}条):")
        for warning in report['warnings'][:20]:  # 只显示前20条
            print(f"  - {warning}")
        if len(report['warnings']) > 20:
            print(f"  ... 还有 {len(report['warnings']) - 20} 条警告")
        print()
    else:
        print("⚠️  警告: 无")
        print()

    # 总体评估
    print("=" * 80)
    if not report['errors']:
        total_completion = (
            report['stats']['has_brief_description'] +
            report['stats']['has_detailed_description'] +
            report['stats']['has_quality_use_cases'] +
            report['stats']['has_workflow_example']
        ) / (4 * report['stats']['total']) * 100 if report['stats']['total'] > 0 else 0

        if total_completion >= 90:
            print("🎉 总体评估: 优秀 (完整度 >= 90%)")
        elif total_completion >= 70:
            print("👍 总体评估: 良好 (完整度 >= 70%)")
        elif total_completion >= 50:
            print("⚠️  总体评估: 及格 (完整度 >= 50%)")
        else:
            print("❌ 总体评估: 需改进 (完整度 < 50%)")
    else:
        print("❌ 总体评估: 存在严重错误,需要修复")
    print("=" * 80)


if __name__ == "__main__":
    # 导入元数据
    try:
        from operator_metadata import get_operator_metadata
    except ImportError:
        print("错误: 无法导入 operator_metadata 模块")
        print("请确保在 engines/spark-workflow/ 目录下运行此脚本")
        sys.exit(1)

    # 获取元数据
    metadata_list = get_operator_metadata()

    # 验证
    validator = MetadataValidator()
    report = validator.validate_all(metadata_list)

    # 打印报告
    print_report(report)

    # 如果有错误,退出码为 1
    if report['errors']:
        sys.exit(1)
