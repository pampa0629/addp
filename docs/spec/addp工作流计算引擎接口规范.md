# ADDP 工作流计算引擎接口规范

**适用引擎**: Math Workflow、GeoPython Workflow、Spark Workflow、Model3D Workflow、PointCloud Workflow、Document Workflow、SuperMap Workflow

Spark 的已确认扩展方案是在现有内置 Runtime 中同时提供本规范工作流入口和 `addp.query-runtime/v1` 查询入口，不新增独立查询服务。工作流仍逐任务绑定一个 Business Spark 集群，不同任务可以选择不同集群；查询工作台则直接选择集群，由平台解析 Runtime。两种协议、集群身份及数据源身份不得混用，迁移状态和统一边界见[引擎体系图](../concepts/addp引擎体系图.md#spark-集群与内置运行时边界)。

本文档定义 ADDP 工作流运行时的 `addp.workflow/v1` HTTP 协议。该协议由 Common Engine 的通用 HTTP `WorkflowRuntimeProvider` 消费；工作流运行时通过 System Engine Instance 和 `engine.capabilities/v1` 纳入统一引擎体系，不要求把每个运行时的 `engine_type` 编译成 Common Engine Plugin。

---

## 按主题阅读

| 阅读目的 | 专题 |
| --- | --- |
| 理解 Provider / HTTP Runtime 架构、当前引擎与目录结构 | [总体架构](workflow/总体架构.md) |
| 实现五个标准接口、请求响应、异步状态和错误码 | [HTTP 协议](workflow/HTTP协议.md) |
| 声明算子字段、参数、端口、连线和分类 | [算子元数据与实现](workflow/算子元数据与实现.md) |
| 处理三层算子契约、资源适配、访问计划、任务输入输出与 Spark 对象成果 | [Develop 资源与执行](workflow/Develop资源与执行.md) |
| 注册 Runtime、校验能力与 DAG、扩展新引擎 | [Runtime 注册与扩展](workflow/Runtime注册与扩展.md) |
| 使用坐标转换、构面、几何检查修复、吸附与面叠检查 | [矢量算子契约](workflow/矢量算子契约.md) |
| 处理栅格资源、切片成果、网格、统计、分类及资源准入 | [栅格算子契约](workflow/栅格算子契约.md) |

协议字段、算子约束和接入步骤分别在对应专题维护；本页保留总入口。实际端口以标准生命周期输出为准，验证分层与入口见[测试与验收规范](addp测试与验收规范.md)。

## 相关文档

- [ADDP 开发原则](addp开发原则.md)
- [ADDP API 设计规范](addp-API设计规范.md)
- [Develop 模块文档](../../develop/CLAUDE.md)
- [System 引擎管理](../../system/CLAUDE.md)

## 技术支持

如有问题，请参考各引擎的 `README.md` 或提交 Issue。

---

**文档维护**: 本文档应随引擎接口变更及时更新。
