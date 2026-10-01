# Catalog 模块开发说明

Catalog 是企业资源目录的唯一事实源，负责稳定目录身份、来源绑定、业务语义关联、字段/组件标准映射、责任关系、目录可见性和治理状态。

## 必读文档

- `docs/concepts/addp企业资源目录体系图.md`
- `docs/spec/addp企业资源目录实现规范.md`
- `docs/concepts/addp术语表.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp新模块开发指南.md`

## 边界

- 不复制 Meta、Model、Standard、Service、Develop、System 的完整专业事实；只保存目录身份、失效解释、列表和搜索所需的最小已观察投影，完整专业详情动态读取 owner。
- 不向 Meta 或 Standard 回写 Catalog ID 或关联投影。
- 责任关系不自动授予 IAM Role、数据访问或授权办理资格。已确认的目标规则以 `docs/spec/addp授权上下文规范.md` 5.5 为准：Catalog 提供当前责任资格及业务入口，System 引擎访问控制领域唯一维护源数据规则；不在 Catalog 新建 Grant 副本，不以 Catalog 可达性阻塞已有合法数据访问或其他模块 Ready。普通只读共享的同人确认／办理及责任移交边界已确认，表级贯通尚未实现。
- 跨模块只走公开 API 和 Tenant Service Access Token，不跨 Schema 查询。
- `/entries` 的业务域上下文用 `catalog.entry.read` 动态发现 Standard Domain 的名称、编码、定义和层级，仅投影本次响应；独立前端业务域页已删除，Standard / Model 专业详情仍由 owner 对当前 User Token 判权，不能以 Catalog 运行身份代查。
- 除 System 注册和本模块必需基础设施外，任何业务模块不可达都不能阻止进程启动；Meta / Model / Standard / Service / Develop 同步失败只产生滞后并后台重试，各 owner 使用独立 checkpoint。
- `CatalogEntry` UUID 是企业稳定身份；Meta fingerprint 只是来源身份。
- 首次保存有效 `business_owner` 即建立过业务责任，不等待完成编目。内部 nullable `business_responsibility_established` 只表达历史依据：新建为 false，责任写入同事务置 true，旧历史不足为 NULL；不得因撤销编目、移交、失效或来源变化重置，也不接受请求方编辑。不把它单独当作精确源资源的授权依据。
- `StandardMapping` 是 CatalogComponent 到确定 `Standard.ElementRevision` 的可审核、可追溯关系事实，拥有独立 UUID、并发版本、来源、置信度和审核状态。企业落标关系只归 Catalog；Quality 方案的物理目标与冻结标准来源仅表达检查意图，不构成第二套企业映射，也不以 Catalog 为前置。
- 旧 `component_element_associations` 只在数据库迁移事务中读取：保留历史观察证据并转为未固定修订的 `legacy/proposed` 候选，随后删除旧表。业务 API 和 CatalogEntry 聚合更新只使用独立 StandardMapping；历史候选经人工固定发布修订并审核前，不得视为已落标。
- 不提供 DataItem CatalogEntry 的手工创建和删除 API。
- Model Entity / LogicalTable 的专业内生 Domain、Element、Metric 和建模关系归 Model，Catalog 不建立可编辑副本。
- Standard Metric 的定义、公式、状态、Domain、分类、单位、数据元映射和依赖关系归 Standard，Catalog 不建立可编辑副本。
- Service QueryService 的 SQL、发布快照、协议、输出契约、Consumer Descriptor、运行状态和端点归 Service，Catalog 不建立可编辑副本；QueryService 没有 owner Domain，primary Domain 仍归 Catalog。
- Develop 只为已持久化的 `query|workflow` DevTask 建立 `development_artifact`；`script` / Notebook、即时查询、execution、运行结果和 ToolApproval 不进入企业目录。DevTask 内容、DAG、参数、Engine 绑定与执行契约归 Develop，Catalog 只保存最小可重建观察摘要并动态解析当前详情。
- Quality 评分、Issue 和 execution 历史不是 CatalogEntry，不进入 Catalog 存储或搜索投影。只对 Meta PostgreSQL table DataItem 使用 owner 提供的 `engine_id + schema_name + table_name` 按需动态解析，不拆分 `full_name` 猜测定位。
- Meta 数据血缘只在 Catalog Frontend 中以当前 User Access Token 动态查询 Meta 唯一图接口，并复用 `common-frontend/graph`；Catalog Backend 不代理、不复制血缘边，Meta 不可达不影响 Catalog 详情和 Ready。
- 数据字典是 Catalog 联邦读模型：只对 active Meta DataItem 组合 Meta 当前物理字段、Catalog 权威且已审核的 StandardMapping 与其中冻结的 Standard ElementRevision，不落表、不使用已观察摘要或动态“当前版本”伪造标准事实。
- 数据字典导出是上述联邦读模型的一次同步 JSON 捕获：服务端重新解析并返回带生成时点和 SHA-256 ETag 的附件，不保存导出任务、文件或第二份事实；批量发布与长期托管不属于该接口。
- Catalog 不提供泛化 `CatalogRelation` 或可配置关系类型；当前唯一自有跨条目关系是弃用条目的可选推荐继任项，只通过治理子资源 `PUT /entries/:id/governance` 维护，使用 CatalogEntry 聚合版本；普通编目更新不得读写它。推荐继任保持两个独立企业身份，不等同 `merged`。
