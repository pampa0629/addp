---
name: workflow-analysis
description: 设计、解释、校验或执行 ADDP 数据分析工作流。用户需要组合多个算子、根据数据事实生成 DAG、修复无效 workflow definition、执行工作流或跟踪执行结果时使用。
---

# 工作流分析

通过 ADDP Tool 完成可复用的数据分析工作流。只把 Skill 作为方法和约束；数据、算子、执行和结果事实始终来自 Tool。

## 核心流程

1. 明确任务是只设计、需要校验，还是需要实际执行。不要把“看看方案”理解成执行授权。
2. 调用 `engine.list` 确认可用的工作流运行时。用户指定实例时验证其存在且可用；未指定且候选不唯一时要求澄清。
3. 调用 `data.search` 查找每个输入数据项。若原始业务词未召回满足目标数据类型或能力的数据项，或结果只有文档内容命中，则把业务资源名转换为可能的资源命名语言或常用技术名后补充搜索，按 locator 合并去重；转换后的词只用于检索，不能当作资源事实。候选不唯一时展示稳定 locator 并要求用户选择，不按名称相似度擅自决定。
4. 对选中的输入调用 `resource.ancestors.get` 校验 locator，再调用 `data.preview` 获取受限字段、类型、CRS 和样本事实。
5. 调用 `workflow.operators.list` 读取目标运行时的 Public Operator Spec。只使用存在且声明支持 workflow 模式的算子和公开参数。
6. 调用 `workflow.draft.generate` 生成候选 definition，并通过 `resources[]` 传入前述步骤已确认的全部 locator、用途、字段、几何列和 CRS 事实；Copilot 不负责重新搜索数据。也可以根据已确认事实构造候选 definition。
7. 调用 `workflow.validate`。存在错误时根据错误和 Public Operator Spec 修正后重新校验；未通过正式校验不得执行。
8. 展示 DAG、关键参数、输入 locator、目标位置和仍需用户决定的事项。
9. 仅当用户明确要求执行且 owner 策略允许时调用 `workflow.run`。保存返回的 `execution_id`，不要同步等待长任务。
10. 调用 `execution.get` 查询状态；返回简短摘要、execution id、ResultRef、locator 或 owner 页面链接。

构造或修复 definition 时读取 [workflow-contract.md](references/workflow-contract.md)。

## 必须澄清

以下信息缺失且会改变结果时暂停并澄清：

- 输入数据候选不唯一；
- 工作流运行时不唯一；
- 距离、单位、坐标系或统计口径未确定；
- 输出父位置、名称或写入模式未确定；
- 执行会产生持久化写入但用户只要求设计或预览。

## 验证要求

- 所有数据身份都是 owner API 返回并经校验的 locator。
- 字段、几何列、CRS、单位和算子参数来自预览或 Public Operator Spec。
- `workflow.validate` 返回 `valid=true` 后才能执行。
- `workflow.run` 只返回 execution，不在 Tool 内轮询到完成。
- 最终输出控制大小，不嵌入完整表格、地图要素、运行日志或 A2UI Surface。

## 反模式

- 不假定空间字段名为 `geom`。
- 不把铁路、耕地或任何固定数据集写成 Skill 前提。
- 不把存储引擎 ID 当成工作流运行时 ID。
- 不在 workflow task params 中放连接密码、connection info 或私有运行时参数。
- 不绕过 Tool 调用 owner 模块 URL，也不通过 shell 拼接 HTTP 请求。
- 不用一次实际执行代替 `workflow.validate`。
- 不在客户端把高风险操作标记为已审批。
