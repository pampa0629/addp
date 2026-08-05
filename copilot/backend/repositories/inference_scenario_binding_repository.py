"""Persistence for Copilot-owned Inference Scenario Bindings."""

from sqlalchemy import select
from sqlalchemy.orm import Session

from models.inference_scenario_binding import InferenceScenarioBinding


class InferenceScenarioBindingVersionConflict(RuntimeError):
    pass


class InferenceScenarioBindingRepository:
    def __init__(self, db: Session) -> None:
        self._db = db

    def resolve(self, tenant_id: int, scenario_code: str) -> InferenceScenarioBinding | None:
        tenant = self._db.scalar(
            select(InferenceScenarioBinding).where(
                InferenceScenarioBinding.scenario_code == scenario_code,
                InferenceScenarioBinding.scope_type == "tenant",
                InferenceScenarioBinding.tenant_id == tenant_id,
            )
        )
        if tenant is not None:
            return tenant
        return self._db.scalar(
            select(InferenceScenarioBinding).where(
                InferenceScenarioBinding.scenario_code == scenario_code,
                InferenceScenarioBinding.scope_type == "platform",
                InferenceScenarioBinding.tenant_id.is_(None),
            )
        )

    def get(self, scope_type: str, tenant_id: int | None, scenario_code: str) -> InferenceScenarioBinding | None:
        statement = select(InferenceScenarioBinding).where(
            InferenceScenarioBinding.scenario_code == scenario_code,
            InferenceScenarioBinding.scope_type == scope_type,
        )
        statement = statement.where(
            InferenceScenarioBinding.tenant_id.is_(None)
            if tenant_id is None
            else InferenceScenarioBinding.tenant_id == tenant_id
        )
        return self._db.scalar(statement)

    def save(
        self,
        *,
        scope_type: str,
        tenant_id: int | None,
        scenario_code: str,
        model_profile_id: str,
        expected_version: int,
        updated_by: int,
    ) -> InferenceScenarioBinding:
        statement = select(InferenceScenarioBinding).where(
            InferenceScenarioBinding.scenario_code == scenario_code,
            InferenceScenarioBinding.scope_type == scope_type,
        )
        statement = statement.where(
            InferenceScenarioBinding.tenant_id.is_(None)
            if tenant_id is None
            else InferenceScenarioBinding.tenant_id == tenant_id
        ).with_for_update()
        current = self._db.scalar(statement)
        if current is None:
            if expected_version != 0:
                raise InferenceScenarioBindingVersionConflict()
            current = InferenceScenarioBinding(
                scenario_code=scenario_code,
                scope_type=scope_type,
                tenant_id=tenant_id,
                model_profile_id=model_profile_id,
                version=1,
                updated_by=updated_by,
            )
            self._db.add(current)
        else:
            if current.version != expected_version:
                raise InferenceScenarioBindingVersionConflict()
            current.model_profile_id = model_profile_id
            current.version += 1
            current.updated_by = updated_by
        self._db.commit()
        self._db.refresh(current)
        return current
