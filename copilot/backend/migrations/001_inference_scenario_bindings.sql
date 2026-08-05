CREATE SCHEMA IF NOT EXISTS copilot;

-- Clean break: Copilot no longer owns Provider endpoints, model names or credentials.
DROP TABLE IF EXISTS copilot.llm_configs;

CREATE TABLE IF NOT EXISTS copilot.inference_scenario_bindings (
    id                  BIGSERIAL PRIMARY KEY,
    scenario_code       VARCHAR(80) NOT NULL,
    scope_type          VARCHAR(16) NOT NULL CHECK (scope_type IN ('platform', 'tenant')),
    tenant_id           BIGINT,
    model_profile_id    UUID NOT NULL,
    version             BIGINT NOT NULL,
    updated_by          BIGINT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((scope_type = 'platform' AND tenant_id IS NULL) OR
           (scope_type = 'tenant' AND tenant_id IS NOT NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_copilot_inference_binding_platform
ON copilot.inference_scenario_bindings (scenario_code)
WHERE scope_type = 'platform' AND tenant_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_copilot_inference_binding_tenant
ON copilot.inference_scenario_bindings (scenario_code, tenant_id)
WHERE scope_type = 'tenant' AND tenant_id IS NOT NULL;
