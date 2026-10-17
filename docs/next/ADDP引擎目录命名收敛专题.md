# ADDP Engine Catalog 命名收敛验收边界

引擎目录与企业目录的稳定概念、类型命名及唯一 Provider 边界已进入[术语表](../concepts/addp术语表.md)、[引擎体系](../concepts/addp引擎体系图.md)和[插件接口规范](../spec/addp引擎插件接口规范.md)。本专题不再保存旧名→新名迁移表、影响文件数和分批实施历史。

## 已完成范围

原专题记录阶段 0–3 完成：跨模块 Engine Catalog 统一使用 EngineCatalog 词族，Common 契约与真实消费者原子切换，旧公共类型、别名、错误码及生成物重复路径删除。裸 Catalog / CatalogEntry 归企业资源目录，Meta node/item 与 Manager resource tree 各保留自身语义。该命名不再阻塞企业 Catalog。

## 验收边界

当时定向模块、Python 与 Swagger 取得分项通过；完整平台及工作区聚合门禁未取得该次通过记录。历史上其他工作造成的中断不作为当前交付阻碍，也不把后续别的版本结果倒填为当次通过。

后续若再次修改共享目录契约，按当前 `make test-changed` 与实际消费者扩散运行标准门禁，检查旧公共词族、别名、兼容字段及错误码没有回流。需要复核历史迁移时使用 Git；当前入口和 JSON 字段以正式规范、Swagger 与代码为准，不从旧映射表恢复旧路由。

历史命名迁移已经完成，不再追加实现待办；本文件只保留原验收口径，最终证据归并后可删除。
