# inference_scenario_bindings 表

`copilot.inference_scenario_bindings` 是 Copilot owner-local 场景绑定表。它只把稳定业务场景绑定到 Inference `model_profile_id`，不保存 Provider、endpoint、上游模型、生成参数或凭据。

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | BIGSERIAL | PK | 绑定 ID。 |
| `scenario_code` | VARCHAR(80) | NOT NULL | `resource_resolution`、`query_generation`、`workflow_generation`、`notebook_generation`、`transfer_generation`、`navigation_guide`、`knowledge_graph_extraction` 或 `standard_document_extraction`。 |
| `scope_type` | VARCHAR(16) | NOT NULL | `platform` 或 `tenant`。 |
| `tenant_id` | BIGINT | NULL | Tenant 绑定的租户；平台绑定必须为空。 |
| `model_profile_id` | UUID | NOT NULL | Inference Model Profile ID。 |
| `version` | BIGINT | NOT NULL | 乐观锁版本。 |
| `updated_by` | BIGINT | NOT NULL | 最后更新 Principal ID。 |
| `created_at` | TIMESTAMPTZ | NOT NULL | 创建时间。 |
| `updated_at` | TIMESTAMPTZ | NOT NULL | 更新时间。 |

唯一性由两个部分索引保证：平台范围按 `scenario_code` 唯一，Tenant 范围按 `(scenario_code, tenant_id)` 唯一。范围 CHECK 保证 `scope_type` 与 `tenant_id` 一致。

管理 API 为 `GET/PUT /api/v1/copilot/settings/inference-bindings/{scenario_code}`。请求体只包含 `version` 和 `model_profile_id`；范围和 Tenant 完全来自 System AuthContext。
