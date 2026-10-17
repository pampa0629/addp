# ADDP GitHub 外部治理待办

仓库内测试分层、单一入口、编排、身份隔离和报告已归入[测试与验收规范](../spec/addp测试与验收规范.md)。本专题仅保留无法由仓库代码证明的外部设置核查；不保留旧 workflow 数量、排队 Job、登录态和管理员状态快照。

## 待核查事项

- 具备仓库管理员只读权限后，核对 Actions 默认权限、fork PR 审批、Action 来源限制及 Secret 隔离。
- 核对 main 当前 Ruleset、required check 与实际 Job 名称、bypass 主体；不能把历史 PR 设置覆盖当前直接在 main 开发的已确认工作规则。
- 核对 Artifact/日志保留、Actions 用量、Runner Group/标签/并发、Environment 保护及 Secret 元数据。
- 核对失败通知接收者及策略；没有明确价值不增加外部通知或历史统计系统。
- 仅对现行 workflow 仍分派给专用宿主的 suite 准备隔离 Runner；GitHub Hosted disposable suite 按其现有 profile 执行，不套用旧的“全部 T4 依赖 macOS”说法。

## 关闭条件

在明确权限与范围内完成外部审计，偏差已修正或形成用户接受的决定；专用 Runner 未闭合场景由[Online 待办](ADDP统一测试与Online验收体系方案.md)唯一跟踪。稳定规则更新正式规范，可用入口更新 scripts 文档，本文无独有待办后删除。

本次只整理文档，没有访问或修改 GitHub 设置、注册 Runner、调度 workflow 或发送通知；历史快照不作为当前外部状态证据。
