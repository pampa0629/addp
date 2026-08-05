"""Copilot-owned Inference Scenario Binding."""

from sqlalchemy import BigInteger, CheckConstraint, DateTime, Index, String, Text, func, text
from sqlalchemy.orm import Mapped, mapped_column
from sqlalchemy.dialects.postgresql import UUID

from database import Base


class InferenceScenarioBinding(Base):
    __tablename__ = "inference_scenario_bindings"
    __table_args__ = (
        CheckConstraint("scope_type IN ('platform', 'tenant')", name="ck_copilot_inference_binding_scope"),
        CheckConstraint(
            "(scope_type = 'platform' AND tenant_id IS NULL) OR "
            "(scope_type = 'tenant' AND tenant_id IS NOT NULL)",
            name="ck_copilot_inference_binding_tenant",
        ),
        Index(
            "idx_copilot_inference_binding_platform",
            "scenario_code",
            unique=True,
            postgresql_where=text("scope_type = 'platform' AND tenant_id IS NULL"),
        ),
        Index(
            "idx_copilot_inference_binding_tenant",
            "scenario_code",
            "tenant_id",
            unique=True,
            postgresql_where=text("scope_type = 'tenant' AND tenant_id IS NOT NULL"),
        ),
        {"schema": "copilot"},
    )

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True, autoincrement=True)
    scenario_code: Mapped[str] = mapped_column(String(80), nullable=False)
    scope_type: Mapped[str] = mapped_column(String(16), nullable=False)
    tenant_id: Mapped[int | None] = mapped_column(BigInteger, nullable=True)
    model_profile_id: Mapped[str] = mapped_column(UUID(as_uuid=False), nullable=False)
    version: Mapped[int] = mapped_column(BigInteger, nullable=False)
    updated_by: Mapped[int] = mapped_column(BigInteger, nullable=False)
    created_at: Mapped[object] = mapped_column(DateTime(timezone=True), nullable=False, server_default=func.now())
    updated_at: Mapped[object] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.now(), onupdate=func.now()
    )
