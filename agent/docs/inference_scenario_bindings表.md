# inference_scenario_bindings 表

`agent.inference_scenario_bindings` 只保存 Agent 业务场景到 Inference Model Profile 的绑定。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | BIGSERIAL | 主键。 |
| `scenario_code` | VARCHAR(80) | `reasoning` 或 `general-chat`。 |
| `scope_type` | VARCHAR(16) | `platform` 或 `tenant`。 |
| `tenant_id` | BIGINT | Tenant 绑定的租户；平台绑定为空。 |
| `model_profile_id` | UUID | Inference Model Profile ID。 |
| `version` | BIGINT | 乐观锁版本。 |
| `updated_by` | BIGINT | 最后更新 Principal ID。 |
| `created_at/updated_at` | TIMESTAMPTZ | 审计时间。 |

平台范围按 `scenario_code` 唯一，Tenant 范围按 `(scenario_code, tenant_id)` 唯一。绑定不保存 Provider、endpoint、adapter、上游模型、生成参数或凭据。

管理 API 为 `GET/PUT /api/v1/agent/settings/inference-bindings/{scenario_code}`。范围和 Tenant 来自 System AuthContext；请求体不得自报 Tenant。
