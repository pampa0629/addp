# Common Engine 引擎插件概念边界讨论稿

更新时间：2026-05-08

本文整理 `common/engine` 引擎插件体系的概念边界共识。本文仍是 next 阶段讨论稿，不是正式规范；确认后应同步修订：

- `docs/spec/addp引擎插件接口规范.md`
- `docs/spec/addp引擎能力声明规范.md`
- `docs/spec/addp存储引擎路径体系规范.md`
- `docs/spec/addp路径统一和指纹计算.md`

## 一、总原则

- 就简不就繁；没有明确模块消费价值的字段先删除，以后有真实需求再加。
- capabilities 是模块消费能力的事实来源；Provider 是能力实现承诺。
- 声明了能力，就必须有明确 Provider 或明确不作为可调用能力。
- Catalog、Metadata、Store、QueryRuntime、Transfer、Preview 是不同能力面，不能混用。
- `connection_info` 是所有引擎连接信息的统一事实源；不要把 connection string / DSN 当作所有引擎的统一抽象。

## 二、能力面边界

| 能力面 | 回答的问题 | 典型消费者 | 典型接口 |
| --- | --- | --- | --- |
| Catalog | 引擎里有什么，目录层级是什么 | System、Meta、Manager | `CatalogProvider` |
| Metadata | 叶子 item 是什么样，字段 / 统计 / 索引是什么 | Meta、Manager | `ItemMetadataProvider` |
| Store | 已定位 item 的真实内容如何读写 | Manager、Transfer、Meta 格式识别 | `ContentReadableProvider`、`BatchReadableProvider` |
| QueryRuntime | 如何执行查询语言 | Develop、Service，Manager 退路 | `QueryRuntimeProvider` |
| Workflow / Script Runtime | 如何执行工作流 / 脚本 / Notebook | Develop、Orchestrator | `WorkflowRuntimeProvider`、`ScriptRuntimeProvider` |
| Transfer | 如何构造导入导出 reader / writer 配置 | Transfer | `TransferAdapter` |
| Preview | 如何直接返回预览结果 | Manager | `PreviewProvider` |

## 三、Store 能力与 Provider

### 3.1 删除无独立价值的总开关

`StoreCapability.read` 和 `StoreCapability.write` 作为总开关没有独立价值：

- 调用方真正需要知道的是 stream read、range read、batch read、stream write、range write、batch write。
- `read=true` 仍无法决定调用哪个接口。
- `write=true` 也无法表达写文件、写对象、写表批次还是其他能力。

建议删除 `read` / `write`，由细分字段派生展示结论。

### 3.2 保留的最小 StoreCapability

建议核心 Store 能力先收敛为：

```go
type StoreCapability struct {
    StreamRead  bool `json:"stream_read,omitempty"`
    StreamWrite bool `json:"stream_write,omitempty"`
    RangeRead   bool `json:"range_read,omitempty"`
    RangeWrite  bool `json:"range_write,omitempty"`
    BatchRead   bool `json:"batch_read,omitempty"`
    BatchWrite  bool `json:"batch_write,omitempty"`
}
```

建议映射：

| 字段 | 含义 | 必须对应的 Provider |
| --- | --- | --- |
| `stream_read` | 顺序流式读取单个对象 / 文件 | `ContentReadableProvider.OpenContent()` |
| `stream_write` | 顺序流式创建或覆盖单个对象 / 文件 | `ContentWritableProvider.CreateContent()` |
| `range_read` | 从指定 byte range 读取内容 | `RangeReadableProvider.OpenRange()` 或 `OpenContent()` 明确支持 offset / length |
| `range_write` | 向指定 byte range / offset 写入内容 | `RangeWritableProvider.WriteRange()` |
| `batch_read` | 按批次读取结构化 item | `BatchReadableProvider.ReadBatch()` |
| `batch_write` | 按批次写入结构化 item | `BatchWritableProvider.WriteBatch()` |

### 3.3 Range 与 Random 的术语选择

主流技术语境中有两套叫法：

- HTTP / S3 使用 `Range` 表达 byte-range 读取，例如 S3 `GetObject` 支持 HTTP `Range` 请求头。
- POSIX / Linux 文件 API 使用 `pread` / `pwrite` 表达“在给定 offset 读写”；Java 使用 `RandomAccessFile` 表达“可随机访问文件位置”。

也就是说：

- `range read` 是对象存储 / HTTP 场景中的常见读术语。
- `random access write` / `positional write` / `pwrite` 是文件系统场景中的常见写术语。
- `range write` 不是 POSIX 文件 API 的主流叫法，但从 ADDP 能力建模角度，它和 `range_read` 对称，且语义清楚：对一个指定字节范围写入。

建议 ADDP 统一使用：

- `range_read`
- `range_write`

理由：

- ADDP capability 是平台抽象，不必完全采用某个底层 API 的命名。
- `range_read/range_write` 比 `range_read/random_write` 更对称，容易被上层模块理解。
- 对文件系统，`range_write` 的底层实现可以是 `pwrite` / seek + write。
- 对对象存储，大多数情况下只声明 `range_read`，不声明 `range_write`。

建议 Provider：

```go
type RangeReadableProvider interface {
    StoreProvider
    OpenRange(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, offset int64, length int64, opts ReadOptions) (io.ReadCloser, error)
}

type RangeWritableProvider interface {
    StoreProvider
    WriteRange(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, offset int64, r io.Reader, opts WriteOptions) (int64, error)
}
```

如果后续希望贴近 POSIX，也可以在实现层使用 `WriteAt` 命名，但 capability 层仍建议叫 `range_write`。

### 3.4 暂不保留的字段

| 字段 | 建议 | 原因 |
| --- | --- | --- |
| `atomic_rename` | 暂不进核心 Store | 当前没有明确调用方；等出现临时文件提交需求时，再和 `RenameProvider` 一起设计 |
| `transactions` | 不放 Store 顶层 | SQL 事务、Transfer checkpoint、文件 rename 不是同一语义 |
| `formats` | 不放 Store 顶层 | 容易混淆文件格式和数据类型；格式支持应放到具体 provider capability 或 Transfer connector |

## 四、CatalogModel

建议概念上只保留一个 CatalogModel：`StorageCapabilities.CatalogModel` 是对外事实源。

`CatalogModelProvider.CatalogModel()` 可以短期保留为插件内部 helper 和测试校验入口，但必须满足：

- 返回值必须与 `Capabilities().Storage.CatalogModel` 完全一致。
- 插件实现应由同一个 builder / helper 生成两处结果。
- System 刷新 capabilities 后，上层模块优先消费 System 中持久化的 catalog model。

不建议当前立即删除 `CatalogModelProvider`，但规范要明确它不是第二事实源。

## 五、NFS / 文件系统 root

NFS 必须有 root meta node，且 root 的 `name` 必然是 `"."`。

原因：

- MinIO / S3 的第一层是 bucket，bucket 是有业务含义的 meta node。
- NFS 的挂载点来自连接配置 `export_path`，不能暴露为数据路径，也不能进入 full_name。
- 如果没有 root meta node，挂载根目录下直接存储的文件没有父 node 可以容纳。
- root meta node 是元数据树结构根，不是用户真实数据路径的一部分。

当前实现效果是正确的：NFS 以 `"."` 作为唯一 root，挂载根目录下的文件可归属到该 root meta node 下。后续重构 object / file catalog adapter 时，必须保留这个行为。

NFS 元数据树应为：

```text
Engine
  root meta_node: name=".", full_name="", node_type="root"
    dir meta_node: full_name="gis-data"
      file meta_item: full_name="gis-data/sample.csv"
    file meta_item: full_name="README.md"
```

root 节点字段固定为：

| 字段 | 值 |
| --- | --- |
| `name` | `.` |
| `full_name` | `""` |
| `node_type` | `root` |
| `attributes.storage.path` | `/` |

Linux / macOS 本地文件系统后续也必须挂到根 meta node 下。其展示名可再讨论，但必须有结构性 root，用于容纳根目录下文件。

## 六、连接测试

`TestConnection()` 只验证只读连接可用性，不验证写能力。

连接测试应验证：

- 连接参数完整。
- 凭据有效。
- 引擎可达。
- 当前账号具有最小只读 / 控制面访问能力。

连接测试不得验证：

- 是否可写。
- 是否可创建表 / bucket / 文件。
- 是否可执行管理操作。

建议测试动作：

| 引擎 | 建议动作 |
| --- | --- |
| SQL | `SELECT 1` 或 `SELECT version()` |
| MongoDB | `listDatabases` 或读取指定 database 元信息 |
| Neo4j | `RETURN 1` |
| MinIO / S3 | `ListBuckets` 或指定 bucket 的只读 stat/list |
| NFS | 挂载后 `ReadDir("/")` 或 stat root |
| Workflow / Script | 只读 health / metadata 接口 |

是否可写由 capabilities 和写 Provider 表达，不属于连接测试职责。

## 七、对象存储与文件系统共享边界

对象存储和文件系统在“树形浏览”和“内容流读取”上有相似性，但不能共享领域模型。

可以共享：

- `ContentReadableProvider` / `ContentWritableProvider` 接口。
- MIME / extension 推断 helper。
- `ReadOptions` / `WriteOptions` 数据结构。
- 格式 parser、preview composer、metadata extractor。
- 纯字符串路径工具，但必须区分 object path 和 filesystem path。

不应共享：

- CatalogModel。
- CatalogAdapter。
- 节点 term/kind。
- root / bucket 语义。
- range / rename / 写入原子性等能力声明。
- object key 与 filesystem physical path 的解析函数。

建议：

- 对象存储使用 `ObjectCatalogAdapter`：`bucket -> prefix -> object`。
- 文件系统使用 `FileCatalogAdapter`：`root -> directory -> file`。
- 内容流接口可以共用，但名称不能暗示共享目录模型。

## 八、QueryRuntime、BatchRead 与 Manager 预览

建议边界：

- QueryRuntime 主要给 Develop 使用。
- BatchRead / BatchWrite 主要给 Transfer 提速。
- Manager 结构化预览也可以优先使用 BatchRead，因为预览需要的是“按 item 读取有限样本”，不是任意查询能力。

推荐策略：

结构化数据：

1. `PreviewProvider`
2. `BatchReadableProvider.ReadBatch()`
3. QueryRuntime 生成只读 sample query 作为退路

对象 / 文件：

1. `PreviewProvider`
2. `ContentReadableProvider.OpenContent()` + parser / composer

## 九、EngineFamily 与 StorageCapabilities

当前 `storage.families` 暂无独立价值，建议删除。

理由：

- 有 `storage` 能力时，storage family 基本等同顶层 `engine_family`。
- 没有 `storage` 能力时，自然也不需要 storage family。
- 上层真正要判断的是具体 capability，例如 catalog、metadata、store、query，而不是再读一层 family。
- 多 family 混合引擎目前没有明确真实需求，先不要为未来假设增加字段。

建议保留：

- 顶层 `engine_family`：主分类，如 `tabular`、`document`、`graph`、`object`、`file`、`workflow`、`script`。
- `storage` 是否存在：表达是否具备存储能力。
- `storage.catalog`、`storage.metadata`、`storage.store`：表达具体存储能力。

如果未来出现真实混合存储引擎，再新增字段讨论。

## 十、EngineOrigin

`EngineCategory()` 建议改为 `EngineOrigin()`。

取值选：

| 值 | 含义 |
| --- | --- |
| `general` | 用户熟悉的通用现成技术 / 主流引擎，如 PostgreSQL、MySQL、MinIO、Neo4j |
| `extension` | 按 ADDP 扩展规范实现的引擎 / 运行时，如 Python Workflow、Math Workflow |

选择 `general` 而不是 `standard`：

- `standard` 容易和旧 `standard` category 混淆。
- `standard` 容易被理解为规范标准、质量等级或强约束。
- `general` 更接近“通用现成技术”，站在用户认知角度，而不是 ADDP 内外部视角。

`EngineOrigin` 不是能力判断字段。上层功能判断必须基于 capabilities。

## 十一、连接信息与 DSN

### 11.1 connection_info 是统一事实源

每个引擎都必须使用 `connection_info` 承载连接信息：

```go
type ConnectionInfo map[string]interface{}
```

现有接口已经基本足够：

- `RequiredFields()`
- `SensitiveFields()`
- `ValidateConnectionInfo()`
- `TestConnection()`

暂不新增复杂结构：

- 不新增 `ConnectionSpec`。
- 不新增 `ConnectionSummary`。
- 不强行定义通用 `Endpoint`。
- 不把 i18n 放进 connection info。

理由：

- 各引擎字段差异大，key-value map 正好适合承载差异。
- System 已经围绕 map 做敏感字段加密、脱敏、合并和覆盖。
- 字段 key 应稳定使用英文机器名，如 `host`、`port`、`database`、`endpoint`、`access_key`。
- 前端展示标签如需中文，由前端或 registry 展示层处理，不进入核心 connection info。

### 11.2 DSN 当前确实有使用

DSN / connection string 当前在代码中有实际使用，主要包括：

- 数据库插件内部：PostgreSQL、MySQL、Doris、ClickHouse、MongoDB、Neo4j 的测试连接、连接池或 driver 创建。
- `common/engine/plugin/dsn_builder.go`：提供数据库 DSN helper。
- `common/models/engine.go` 的旧 `BuildConnectionString()`：Manager、Spatial、MVT 等旧路径仍有调用。
- Transfer JDBC reader/writer、部分 Notebook 相关代码也存在 `connection_string`。

因此不能说 DSN 没用。

但 DSN 是数据库 driver / 连接池的实现细节，不是 System 统一管理所有引擎的必选概念。MinIO、S3、NFS、Workflow、Script 不需要统一 DSN。

### 11.3 BuildConnectionString 改为可选 DSNProvider

建议从 `EnginePlugin` 基础接口中移除 `BuildConnectionString()`，改为可选 Provider：

```go
type DSNProvider interface {
    EnginePlugin
    BuildDSN(connInfo ConnectionInfo) (string, error)
}
```

规则：

- SQL / MongoDB / Neo4j 等需要 driver DSN 的插件可以实现 `DSNProvider`。
- MinIO / S3 / NFS / Workflow / Script 不需要实现 `DSNProvider`。
- System 不依赖 `DSNProvider`。
- `BuildDSN()` 返回值不得持久化到 System，不得作为跨模块能力判断依据。
- 非数据库引擎不再返回 JSON 字符串冒充 connection string。

短期迁移：

- 保留现有 DSN helper，但重命名为 `BuildDSN` 方向。
- 逐步清理 `common/models.BuildConnectionString()` 和上层旧调用路径。
- 数据库类内部仍可使用 DSN helper 创建连接池和测试连接。

## 十二、规范修订清单

- [ ] `addp引擎能力声明规范.md`：删除 `StoreCapability.read/write`。
- [ ] `addp引擎能力声明规范.md`：新增或确认 `range_read` / `range_write`，并删除 `random_write` 命名。
- [ ] `addp引擎能力声明规范.md`：暂不保留 `atomic_rename`、`transactions`、`formats` 作为 Store 顶层字段。
- [ ] `addp引擎能力声明规范.md`：删除 `storage.families`。
- [ ] `addp引擎能力声明规范.md`：明确 CatalogModel 对外事实源为 `storage.catalog_model`。
- [ ] `addp引擎插件接口规范.md`：补充 StoreCapability 与 Provider 映射。
- [ ] `addp引擎插件接口规范.md`：新增或确认 `RangeReadableProvider` / `RangeWritableProvider`。
- [ ] `addp引擎插件接口规范.md`：明确 Catalog / Metadata Provider 默认只读。
- [ ] `addp引擎插件接口规范.md`：将 `EngineCategory` 调整为 `EngineOrigin`，取值 `general` / `extension`。
- [ ] `addp引擎插件接口规范.md`：保留 `connection_info` map 作为统一连接信息事实源。
- [ ] `addp引擎插件接口规范.md`：将 `BuildConnectionString()` 从基础接口移除，改为可选 `DSNProvider.BuildDSN()`。
- [ ] `addp存储引擎路径体系规范.md`：强化 NFS root meta node 必须存在，`name="."`。
- [ ] `addp存储引擎路径体系规范.md`：明确对象存储和文件系统不共享 CatalogModel / CatalogAdapter。

## 参考来源

- AWS S3 `GetObject` 官方文档使用 HTTP `Range` 请求头表达对象 byte-range 读取：https://docs.aws.amazon.com/AmazonS3/latest/API/API_GetObject.html
- Linux `pread(2)` / `pwrite(2)` 文档使用 “given offset” 描述指定偏移读写：https://www.man7.org/linux/man-pages/man2/pwrite.2.html
- Java `RandomAccessFile` 官方文档使用 random access 表达可在文件任意位置读写：https://docs.oracle.com/en/java/javase/25/docs/api/java.base/java/io/RandomAccessFile.html
