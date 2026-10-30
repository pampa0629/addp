# Inference 模块

Inference 是 ADDP 统一 AI 推理 owner。正式契约见 [AI 推理接口规范](<../docs/spec/addp AI推理接口规范.md>)。

- System 只登记 `inference_runtime` Engine Instance。
- 本模块拥有 Provider Connection、Model Deployment、Model Profile 和加密凭据。
- 本模块拥有只读 Provider Template 目录、服务端连接检测和模型发现；模板只用于创建正式资源，不形成第二套配置模型。
- Agent、Copilot、Manager 拥有各自 Scenario Binding，调用 `addp.inference/v1`。
- 不新增厂商环境变量、调用方直连或隐藏 fallback。
- API 变更必须同步 Swagger、双语注解和正式规范。

## 专题导航

| 阅读目的 | 正式契约 |
| --- | --- |
| 唯一技术路线、Runtime 发现与多实例前提 | [目标与唯一技术路线](<../docs/spec/addp AI推理接口规范.md#一目标与唯一技术路线>) |
| Provider、Deployment、Profile 与场景绑定的归属 | [资源模型与 owner](<../docs/spec/addp AI推理接口规范.md#二资源模型与-owner>) |
| 适配器、Chat 输出上限、温度与思考模式 | [模型适配与调用参数](<../docs/spec/addp AI推理接口规范.md#模型适配与调用参数>) |
| 只读模板、服务端检测与模型发现 | [Provider Template 与快速接入](<../docs/spec/addp AI推理接口规范.md#21-provider-template-与快速接入>) |
| Platform／Tenant 范围、场景解析与数据面身份 | [范围和授权](<../docs/spec/addp AI推理接口规范.md#三范围和授权>) |
| 凭据设置、轮换、删除、加密与响应边界 | [凭据](<../docs/spec/addp AI推理接口规范.md#四凭据>) |
| 资源管理 API | [控制面 API](<../docs/spec/addp AI推理接口规范.md#五控制面-api>) |
| 统一推理协议、Profile 解析与请求校验 | [数据面 API](<../docs/spec/addp AI推理接口规范.md#六数据面-api>) |
| Agent、Copilot、Manager 的场景职责与配置边界 | [调用方边界](<../docs/spec/addp AI推理接口规范.md#七调用方边界>) |
| Console／System 配置管理入口与强类型资源 | [配置管理入口](<../docs/spec/addp AI推理接口规范.md#八配置管理入口>) |
| 授权、凭据、能力、场景解析及上游错误的验证 | [验证要求](<../docs/spec/addp AI推理接口规范.md#九验证要求>) |

验证要求是检查清单，实际结果按[测试与验收规范](../docs/spec/addp测试与验收规范.md)分层记录。受控上游测试不代替新代码部署后的真实厂商调用；未运行、失败和未覆盖项分别说明，不计为通过。
