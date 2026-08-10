---
name: data-discovery
description: 发现、校验并确认 ADDP 任务需要读取的输入数据资源。用户按业务名称查找数据、存在多个候选、需要跨语言检索、需要确认 locator、字段、几何列或 CRS，或者其他领域 Skill 需要先取得可信 ResourceFact 时使用。
---

# 数据发现与确认

把自然语言中的输入数据需求收敛为 owner Tool 已验证、用户已确认的资源事实。只处理已有输入资源；创建目标使用 `parent_locator + name`，不生成虚拟 locator。

## 核心流程

1. 把需求拆成独立输入角色。角色描述业务用途，不使用表名、字段名或固定数据集充当角色。
2. 根据当前宿主范围选择候选来源：普通场景使用 `data.search`；宿主已经限定 Session Catalog 时只使用宿主提供的候选，不扩大到租户搜索。
3. 使用用户原始业务词和常见跨语言技术名称检索。零召回时只为缺失角色补充尚未尝试的直接同义词，再检索一次。
4. 对 Tool 搜索候选调用 `resource.ancestors.get` 确认 locator，再调用 `data.preview` 收敛字段、几何列、几何类型和 CRS。Session Catalog 候选由宿主 owner 按同等要求重新校验。
5. 可以基于已验证事实排序和标记推荐项，但不得删除仍合理的候选，也不得让模型生成资源身份。
6. 每个角色只有一个候选且领域 Skill 允许自动确认时可以继续；存在多个候选或领域策略要求显式确认时，创建 clarification 等待用户选择。
7. 在进入领域生成或执行前重新校验用户确认的资源，输出受限 `ResourceFact`，不输出连接信息、Token 或完整样本。

## 场景约束

- Query：候选必须属于当前 Query Engine，并兼容当前查询语言。
- Workflow：允许在 Tenant 可访问范围内发现多个 Source Engine 的输入资源。
- Notebook：只使用当前 Notebook Session Catalog，禁止调用租户级搜索扩大范围。
- Transfer：只解析一个源资源；目标引擎、目标父节点和目标名称不属于输入资源发现。

领域 Skill 的约束优先于本 Skill 的通用流程；约束冲突时停止并澄清，不切换到更宽的候选范围。

## 验证

- 每个确认资源都有 owner 返回的 locator 或 Session owner 的稳定候选身份。
- 字段、几何列、几何类型和 CRS 来自最新 owner 事实。
- 多候选选择由用户完成，没有按分数自动代替用户决定。
- 没有把目标位置、算子、指标、距离或输出名称误识别为输入资源。

## 反模式

- 不自行拼接 ResourceLocator，不把 Engine ID 当成数据项身份。
- 不假定 schema、表名、字段名、`geom`、`geometry` 或 CRS。
- 不绕过 ToolExecutor、SDK 或 owner API 直接请求模块私有 URL。
- 不因当前场景零召回而改用全平台或其他 Tenant 范围搜索。
- 不把资源排序结果视为用户确认结果。
