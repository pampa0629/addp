---
name: transfer-generation
description: 为 ADDP Transfer 生成待人工确认且无副作用的任务草稿。用户要求导入、导出、同步或复制一个已有源资源，并需要发现源数据、确认目标父节点、生成任务名称、描述或字段映射时使用。
---

# Transfer 草稿生成

复用数据发现方法确认唯一源资源，在 Transfer owner 已确定的运行边界、装载模式和目标策略内生成任务草稿。当前 Skill 不创建、不启动任务。

## 核心流程

1. 按 `data-discovery` 的方法只解析 Transfer 的源侧输入；忽略“传到哪里”等目标描述中的资源词。
2. 源资源必须唯一。多个候选时等待用户确认，不按推荐分数自动选择。
3. 通过 `resource.ancestors.get` 和 `data.preview` 重新校验源 locator、字段、类型、格式和空间事实。
4. 要求用户或 Transfer 向导先确认目标引擎、目标 `parent_locator + name`、运行边界、装载模式和目标策略。
5. 调用 `transfer.draft.generate`，只允许模型补充任务名称、描述和基于已验证源字段的映射意图。
6. 展示完整草稿和警告，等待用户在 Transfer owner 页面复核后提交。

## 必须澄清

- 未识别出唯一源资源；
- 目标父节点、目标名称或目标引擎未确认；
- bounded/continuous、snapshot/incremental 或目标写入策略未确定；
- 字段映射引用了不存在的源字段；
- 用户要求立即运行，但当前 Tool 集合没有正式的 Transfer 创建或运行 Tool。

## 验证

- source locator 来自已确认并重新校验的 `ResourceFact`。
- target 使用 owner 确认的 `parent_locator + name`，没有虚拟 locator。
- 草稿没有 credential、connection info、内部 URL 或已删除的旧配置字段。
- 输出只代表候选草稿，不宣称任务已经创建或执行。

## 反模式

- 不把目标资源当成已有输入资源搜索或确认。
- 不让模型改变运行边界、装载模式、目标策略或引擎选择。
- 不根据常见字段名猜测映射，不保留模型生成的未知源字段。
- 不把 `transfer.task.create` 权限名当成 ADDP Tool。
- 不绕过 Transfer owner API 创建或运行任务。
