from sqlalchemy import BigInteger, CheckConstraint, DateTime, Index, Integer, Numeric, String, func, text
from sqlalchemy.orm import Mapped, mapped_column

from database import Base


class MatchingPolicy(Base):
    __tablename__ = "matching_policies"
    __table_args__ = (
        CheckConstraint("scope_type IN ('platform', 'tenant')", name="ck_copilot_matching_policy_scope"),
        CheckConstraint("(scope_type = 'platform' AND tenant_id IS NULL) OR (scope_type = 'tenant' AND tenant_id IS NOT NULL)", name="ck_copilot_matching_policy_tenant"),
        Index("idx_copilot_matching_policy_platform", "scope_type", unique=True, postgresql_where=text("scope_type = 'platform' AND tenant_id IS NULL")),
        Index("idx_copilot_matching_policy_tenant", "scope_type", "tenant_id", unique=True, postgresql_where=text("scope_type = 'tenant' AND tenant_id IS NOT NULL")),
        {"schema": "copilot"},
    )

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True, autoincrement=True)
    scope_type: Mapped[str] = mapped_column(String(16), nullable=False)
    tenant_id: Mapped[int | None] = mapped_column(BigInteger, nullable=True)
    score_threshold: Mapped[float] = mapped_column(Numeric(5, 4), nullable=False)
    max_candidates: Mapped[int] = mapped_column(Integer, nullable=False)
    version: Mapped[int] = mapped_column(BigInteger, nullable=False, default=0)
    updated_by: Mapped[int] = mapped_column(BigInteger, nullable=False)
    created_at: Mapped[object] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())
    updated_at: Mapped[object] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now(), onupdate=func.now())
