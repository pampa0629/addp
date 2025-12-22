"""
算子选择 Agent：从所有算子中选择相关算子（两阶段工作流生成的第一阶段）
"""
import time
import httpx
import json
import re
from typing import List, Dict
from langchain_core.messages import SystemMessage

from services.llm_service import llm_service
from config import settings


class OperatorSelectionAgent:
    """
    算子选择 Agent（第一阶段）

    功能：
    - 从 Develop API 获取所有算子的简要信息
    - 根据用户需求，调用 LLM 选择 3-8 个最相关的算子
    - 返回选中的算子名称列表

    缓存策略：
    - 缓存 TTL: 5 分钟
    - 自动刷新：缓存过期时自动从 API 获取最新数据
    """

    def __init__(self):
        self.llm_service = llm_service
        self.develop_api_url = settings.develop_service_url

        # 缓存配置（5 分钟 TTL）
        self.operators_cache = None
        self.cache_time = None
        self.cache_ttl = 300  # 5 分钟

    async def select_operators(
        self,
        query: str,
        tenant_id: int = 1
    ) -> List[str]:
        """
        选择相关算子

        Args:
            query: 用户查询
            tenant_id: 租户ID

        Returns:
            算子名称列表，例如: ["buffer", "intersection", "save"]
        """
        print(f"\n{'='*60}")
        print(f"[OperatorSelection] 开始选择算子")
        print(f"[OperatorSelection] 用户查询: {query}")
        print(f"{'='*60}\n")

        # 1. 获取所有算子的简要信息（带缓存）
        brief_operators = await self._fetch_brief_operators()
        print(f"[OperatorSelection] 获取到 {len(brief_operators)} 个算子")

        # 2. 构建选择 Prompt
        prompt = self._build_selection_prompt(query, brief_operators)
        print(f"[OperatorSelection] Prompt 长度: {len(prompt)} 字符")

        # 3. 调用 LLM（低温度保证稳定性）
        try:
            llm = self.llm_service.get_llm(tenant_id=tenant_id)
            print(f"[OperatorSelection] 正在调用 LLM...")

            response = await llm.ainvoke(
                [SystemMessage(content=prompt)],
                temperature=0.3,  # 低温度保证选择稳定性
                max_tokens=500
            )
            print(f"[OperatorSelection] LLM 响应: {response[:200]}...")
        except Exception as e:
            print(f"[OperatorSelection] ❌ LLM 调用失败: {e}")
            raise

        # 4. 解析算子列表
        selected = self._parse_operator_list(response)
        print(f"[OperatorSelection] ✅ 选中 {len(selected)} 个算子: {selected}")

        return selected

    async def _fetch_brief_operators(self) -> List[Dict]:
        """
        获取算子简要信息（带缓存）

        缓存策略：
        - 缓存命中: 直接返回缓存数据
        - 缓存过期: 自动调用 API 刷新

        Returns:
            算子简要信息列表: [{"name": "buffer", "brief": "...", "category": "..."}, ...]
        """
        # 检查缓存有效性
        if self.operators_cache and self.cache_time:
            age = time.time() - self.cache_time
            if age < self.cache_ttl:
                print(f"[OperatorSelection] 使用缓存的算子信息（年龄: {age:.1f}秒）")
                return self.operators_cache

        # 从 Develop API 获取最新数据
        print(f"[OperatorSelection] 缓存过期或不存在，从 Develop API 获取...")
        try:
            # 构建认证头
            headers = {}
            if settings.internal_api_key:
                headers["X-Internal-API-Key"] = settings.internal_api_key

            async with httpx.AsyncClient(timeout=10.0) as client:
                resp = await client.get(
                    f"{self.develop_api_url}/api/develop/operators",
                    headers=headers
                )
                resp.raise_for_status()
                data = resp.json()
                print(f"[OperatorSelection] ✅ 从 API 获取到 {len(data['operators'])} 个算子")
        except Exception as e:
            print(f"[OperatorSelection] ❌ API 调用失败: {e}")
            # 如果有缓存，即使过期也返回
            if self.operators_cache:
                print(f"[OperatorSelection] 使用过期缓存作为降级")
                return self.operators_cache
            raise

        # 提取简要信息
        brief = []
        for op in data['operators']:
            brief.append({
                "name": op["name"],
                "brief": op.get("brief_description", op.get("description", "")),
                "category": op.get("category", "其他")
            })

        # 更新缓存
        self.operators_cache = brief
        self.cache_time = time.time()

        return brief

    def _build_selection_prompt(self, query: str, operators: List[Dict]) -> str:
        """
        构建算子选择 Prompt

        Args:
            query: 用户查询
            operators: 算子简要信息列表

        Returns:
            完整的 Prompt 文本
        """
        # 按类别分组算子
        by_category = {}
        for op in operators:
            cat = op["category"]
            if cat not in by_category:
                by_category[cat] = []
            by_category[cat].append(op)

        # 构建算子列表文本
        operator_text = ""
        for cat, ops in sorted(by_category.items()):
            operator_text += f"\n### {cat}\n"
            for op in ops:
                operator_text += f"- **{op['name']}**: {op['brief']}\n"

        prompt = f"""你是一个 GIS 算子选择专家。根据用户需求，从以下算子中选择最相关的算子。

## 可用算子（共 {len(operators)} 个）
{operator_text}

## 用户需求
{query}

## 任务要求

请选择 **3-8 个最相关的算子**，返回 JSON 数组格式：
```json
["operator1", "operator2", "operator3"]
```

## 选择原则

1. **必须包含数据输入算子**：如 `load`（从数据库/文件加载）
2. **必须包含数据输出算子**：如 `save`（保存到数据库/文件）
3. **选择核心处理算子**：根据需求选择，如缓冲、相交、融合等
4. **避免功能重复**：不要选择功能相似的算子
5. **优先简单直接**：能用一个算子完成就不用多个

## 重要提示

- **只返回 JSON 数组**，不要有任何其他内容
- 算子名称必须与上述列表完全一致
- 数量控制在 3-8 个之间
"""
        return prompt

    def _parse_operator_list(self, response: str) -> List[str]:
        """
        解析 LLM 返回的算子列表

        Args:
            response: LLM 响应文本

        Returns:
            算子名称列表
        """
        # 尝试提取 JSON 数组
        json_match = re.search(r'\[(.*?)\]', response, re.DOTALL)
        if json_match:
            json_str = '[' + json_match.group(1) + ']'
            try:
                operators = json.loads(json_str)
                # 验证是否为字符串列表
                if isinstance(operators, list) and all(isinstance(op, str) for op in operators):
                    return operators[:8]  # 最多返回 8 个
            except json.JSONDecodeError:
                pass

        # 回退方案：逐行提取算子名
        print(f"[OperatorSelection] JSON 解析失败，使用回退方案")
        operators = []
        for line in response.split('\n'):
            line = line.strip(' -"\'[]')
            # 过滤掉注释和空行
            if line and not line.startswith('#') and not line.startswith('//'):
                # 提取可能的算子名（字母、数字、下划线）
                match = re.match(r'^([a-z_][a-z0-9_]*)', line, re.IGNORECASE)
                if match:
                    operators.append(match.group(1))

        return operators[:8]  # 最多返回 8 个


# 全局单例
operator_selector = OperatorSelectionAgent()
