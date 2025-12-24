"""
元数据匹配服务：智能匹配数据源
"""
from typing import List, Optional, Dict
from dataclasses import dataclass
import httpx
from config import settings


@dataclass
class MatchResult:
    """匹配结果"""
    auto_selected: bool  # 是否自动选择
    selected: Optional[Dict]  # 选中的数据源
    candidates: List[Dict]  # 候选数据源列表


class MetadataMatcherService:
    """元数据匹配服务"""

    def __init__(self):
        self.meta_api_url = settings.meta_service_url
        self.system_api_url = settings.system_service_url
        # 禁用系统代理，直接访问本地服务
        self.client = httpx.AsyncClient(timeout=30.0, trust_env=False)
        self.score_threshold = settings.copilot_score_threshold  # 0.15
        self.max_candidates = settings.copilot_max_candidates  # 10

    async def match_datasources(
        self,
        query: str,
        tenant_id: int,
        resource_id: Optional[int] = None
    ) -> MatchResult:
        """
        匹配数据源

        Args:
            query: 用户查询
            tenant_id: 租户 ID
            resource_id: 指定的资源 ID（可选）

        Returns:
            MatchResult 匹配结果
        """
        # 1. 查询元数据
        if resource_id:
            candidates = await self._get_resource_metadata(resource_id, tenant_id)
        else:
            candidates = await self._search_metadata(query, tenant_id)

        if not candidates:
            return MatchResult(
                auto_selected=False,
                selected=None,
                candidates=[]
            )

        # 2. 计算相似度得分
        scored_candidates = self._score_candidates(candidates, query)

        # 3. 混合模式判断（阈值 0.15）
        if len(scored_candidates) < 2:
            return MatchResult(
                auto_selected=True,
                selected=scored_candidates[0] if scored_candidates else None,
                candidates=scored_candidates
            )

        score_diff = scored_candidates[0]["score"] - scored_candidates[1]["score"]

        if score_diff >= self.score_threshold:
            # 得分差距大，自动选择
            return MatchResult(
                auto_selected=True,
                selected=scored_candidates[0],
                candidates=scored_candidates[:self.max_candidates]
            )
        else:
            # 得分差距小，展示候选列表
            return MatchResult(
                auto_selected=False,
                selected=None,
                candidates=scored_candidates[:self.max_candidates]
            )

    async def _search_metadata(self, query: str, tenant_id: int) -> List[Dict]:
        """
        通过 Meta API 搜索元数据

        Args:
            query: 搜索查询
            tenant_id: 租户 ID

        Returns:
            元数据列表
        """
        try:
            response = await self.client.post(
                f"{self.meta_api_url}/api/metadata/search",
                json={"query": query, "tenant_id": tenant_id}
            )
            response.raise_for_status()
            data = response.json()
            return data.get("items", [])
        except Exception as e:
            print(f"Error searching metadata: {e}")
            return []

    async def _get_resource_metadata(self, resource_id: int, tenant_id: int) -> List[Dict]:
        """
        获取指定资源的元数据

        Args:
            resource_id: 资源 ID
            tenant_id: 租户 ID

        Returns:
            元数据列表
        """
        try:
            response = await self.client.get(
                f"{self.meta_api_url}/api/metadata/resources/{resource_id}",
                params={"tenant_id": tenant_id}
            )
            response.raise_for_status()
            data = response.json()
            return data.get("items", [])
        except Exception as e:
            print(f"Error getting resource metadata: {e}")
            return []

    def _score_candidates(self, candidates: List[Dict], query: str) -> List[Dict]:
        """
        计算相似度得分

        Args:
            candidates: 候选数据源列表
            query: 查询字符串

        Returns:
            排序后的候选列表
        """
        scored = []

        for candidate in candidates:
            score = 0.0

            # 表名匹配（简单字符串相似度）
            table_name = candidate.get("table_name", "").lower()
            if query.lower() in table_name:
                score += 0.4

            # 空间表优先
            if candidate.get("spatial_metadata"):
                score += 0.2

            # 数据新鲜度
            if candidate.get("last_modified_at"):
                score += 0.2

            # 数据量
            if candidate.get("row_count", 0) > 0:
                score += 0.2

            candidate["score"] = score
            candidate["reason"] = self._generate_reason(candidate, score)
            scored.append(candidate)

        # 按得分排序
        scored.sort(key=lambda x: x["score"], reverse=True)
        return scored

    def _generate_reason(self, candidate: Dict, score: float) -> str:
        """
        生成推荐理由

        Args:
            candidate: 候选数据源
            score: 得分

        Returns:
            推荐理由字符串
        """
        reasons = []
        if score >= 0.8:
            reasons.append("高度匹配")
        elif score >= 0.5:
            reasons.append("可能相关")

        if candidate.get("spatial_metadata"):
            reasons.append("包含空间字段")

        if candidate.get("row_count", 0) > 1000:
            reasons.append("数据量充足")

        return ", ".join(reasons) if reasons else "一般匹配"

    async def close(self):
        """关闭 HTTP 客户端"""
        await self.client.aclose()


# 全局单例
metadata_matcher = MetadataMatcherService()
