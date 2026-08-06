# Inference 模块

Inference 是 ADDP 统一 AI 推理 owner。正式契约见 `docs/spec/addp AI推理接口规范.md`。

- System 只登记 `inference_runtime` Engine Instance。
- 本模块拥有 Provider Connection、Model Deployment、Model Profile 和加密凭据。
- 本模块拥有只读 Provider Template 目录、服务端连接检测和模型发现；模板只用于创建正式资源，不形成第二套配置模型。
- Agent、Copilot、Manager 拥有各自 Scenario Binding，调用 `addp.inference/v1`。
- 不新增厂商环境变量、调用方直连或隐藏 fallback。
- API 变更必须同步 Swagger、双语注解和正式规范。
