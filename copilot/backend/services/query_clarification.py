"""查询生成中的结构化语义缺口，不绑定具体查询语言。"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class ClarificationOption:
    value: str
    label: str
    description: str | None = None


@dataclass(frozen=True)
class QueryClarification:
    key: str
    category: str
    prompt: str
    control: str
    options: tuple[ClarificationOption, ...] = ()
    required: bool = True


class QueryClarificationRequired(ValueError):
    def __init__(self, clarification: QueryClarification):
        super().__init__(clarification.prompt)
        self.clarification = clarification
