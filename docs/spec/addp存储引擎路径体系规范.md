# 存储引擎路径体系规范

本文定义 ADDP 中所有已支持存储引擎的路径体系，
作为元数据扫描、资源定位、数据预览等模块的设计规范。

当前已支持的存储引擎（按插件类型分类）：

| 引擎类型标识 | 显示名称 | 主要 Provider |
|------------|---------|---------|
| `postgresql` | PostgreSQL | CatalogProvider + CatalogFactsProvider + SQLQueryRuntimeProvider |
| `mysql` | MySQL | CatalogProvider + CatalogFactsProvider + SQLQueryRuntimeProvider |
| `doris` | Apache Doris | CatalogProvider + CatalogFactsProvider + SQLQueryRuntimeProvider |
| `clickhouse` | ClickHouse | CatalogProvider + CatalogFactsProvider + SQLQueryRuntimeProvider |
| `mongodb` | MongoDB | CatalogProvider + CatalogFactsProvider + QueryRuntimeProvider |
| `neo4j` | Neo4j | CatalogProvider + QueryRuntimeProvider + GraphQueryProvider |
| `minio` | MinIO | CatalogProvider + ContentReadableProvider |
| `s3` | Amazon S3 | CatalogProvider + ContentReadableProvider |
| `nfs` | NFS 文件系统 | CatalogProvider + ContentReadableProvider |

---

## 一、核心概念

### 引擎配置 vs 数据路径

**关键原则**：存储引擎的连接参数（如 NFS 的 `export_path`、MinIO 的 `endpoint`）是**配置**，
不是数据路径的一部分，不得出现在 full_name、ResourceLocator 或任何对用户可见的路径中。

| 引擎 | 连接参数（配置层，不进入路径） | 引擎目录根 | 第一层业务 branch |
|------|--------------------------|-------------|-------------|
| MinIO/S3 | endpoint、access_key | `service`，标题使用引擎实例名称，`full_name=""` | bucket |
| NFS | server、export_path | `root`，标题使用引擎实例名称，`full_name=""` | directory；根目录下 file 可直接挂到 root |
| PostgreSQL | host、port、user | `server`，标题使用引擎实例名称，`full_name=""` | schema |
| MySQL/Doris/ClickHouse | host、port、user | `server`，标题使用引擎实例名称，`full_name=""` | database |
| MongoDB/Neo4j | host、port | `server`，标题使用引擎实例名称，`full_name=""` | database |

### 引擎目录根与业务路径

所有存储引擎都有显性引擎目录根。引擎目录根是资源树和 Meta 树的结构入口，不是业务路径段：

- root `meta_node.parent_node_id` 为 `NULL`。
- root `meta_node.name` 使用引擎实例名称，展示层直接显示为存储引擎。
- root `meta_node.full_name` 固定为空字符串 `""`。
- root 的 ResourceLocator path 为空，但必须和普通 node 一样携带 `type` 与 `node_id`，例如 `addp://engine/8/path/?type=server&node_id=99`。
- root 不进入 `CatalogPath.StringPath()`、`full_name`、`storage_ref`、ResourceLocator 业务 path 或指纹输入。
- root 原生名称如有实际价值，写入 `meta_node.attributes.catalog.native_name`；例如 NFS 挂载根为 `/`。
- Provider 枚举第一层业务 branch 时必须使用 `ListChildren(rootPath)`，不得用 empty path 表达业务第一层。

bucket、schema、database、directory 是 root 下第一层业务 branch。它们可以对用户可见，也可以作为扫描目标，但不再被称为引擎根。

### item_type 与 data_type 分工

`meta_item.item_type` 表达 data item 在所属引擎 catalog / 路径模型中的原生叶子术语，用于路由、树展示和路径解析；`attributes.item.data_type` 表达平台对内容语义的理解，用于预览、读取、检索和传输能力选择。二者不得混用。

| 引擎类型 | 路径模型叶子 | `meta_item.item_type` | 内容语义示例 |
|---|---|---|---|
| MinIO / S3 | object | `object` | `data_type=table/document/media/container`，`format=csv/wps/png/excel` |
| NFS / 本地文件系统 | file | `file` | `data_type=table/document/media/container`，`format=csv/wps/png/excel` |
| PostgreSQL / MySQL / Doris / ClickHouse | table / view | `table` / `view` | 通常 `data_type=table` |
| MongoDB | collection | `collection` | 原生 JSON/BSON document 组成的动态 schema 记录集合，固定为 `data_type=table` |
| Neo4j | graph | `graph` | `data_type=graph` |

因此，同一个 `sales.csv` 在 MinIO 中是 `item_type=object`，在 NFS 中是 `item_type=file`；它们都可以同时拥有 `attributes.item.data_type=table`、`attributes.item.format=csv`。不得因为对象内容可按表格读取，就把对象存储中的 `item_type` 写成 `table`。

MongoDB collection 由 JSON/BSON document 记录组成，不是关系型数据库表，也不是 DOCX / PDF 这类阅读型 `document`。在当前 ADDP 能力中，collection 作为动态 schema 记录集合消费：`meta_item.item_type=collection` 保留 MongoDB 原生 catalog 术语，`attributes.item.data_type=table` 用于预览、查询、字段画像和传输等平台能力选择。不得为 MongoDB collection 写入 `type_info.document` 或新增 `type_info.collection`。

### storage_ref

`storage_ref` 是后端可打开的存储叶子内容引用，用于 Manager 预览流、原始下载和其他需要直接读取存储内容的链路。它必须遵守所属引擎的 catalog 路径模型：

| 引擎路径模型 | `storage_ref` 示例 | 说明 |
|---|---|---|
| 对象存储（MinIO / S3） | `addp/image/photo.jpg` | 以 bucket 开头，后接 object key |
| 文件系统（NFS） | `gis-data/sample.csv` | 挂载根内相对路径，不包含 NFS `export_path` |

`storage_ref` 指向的是存储叶子，不是任意可预览对象。ZIP entry、Excel sheet、SQLite table 等容器内部 child 不是独立存储叶子，不能伪造 `storage_ref`。数据库 table、SQL 查询结果和计算结果也不是存储叶子，它们的完整下载属于导出语义。

对于 `layout=multi` 的 data item，primary content 可以作为入口 `storage_ref`，完整读取或下载必须再消费 Meta 已确认的 `attributes.item.refs`。`storage_ref` 本身不替代 multi refs 集合。

---

## 二、full_name 规则

`full_name` 是数据项在引擎内的**唯一逻辑标识**，贯穿元数据存储、资源定位和数据访问全流程。

### 对象存储（MinIO / S3）

full_name 以 bucket 开头，包含完整的层级路径：

| 节点/数据项类型 | full_name 示例 |
|--------------|--------------|
| bucket 节点 | `addp` |
| prefix 节点 | `addp/image` |
| object 数据项 | `addp/image/photo.jpg` |

对象存储的 item 叶子类型固定为 `object`。CSV、Shapefile、Parquet、WPS、PDF、图片等格式只改变 `attributes.item.data_type`、`attributes.item.format` 和 `attributes.item.layout`，不改变 `meta_item.item_type=object`。

### 文件系统（NFS）

full_name 是相对于挂载点的路径，不包含挂载点本身：

| 节点/数据项类型 | full_name 示例 | 说明 |
|--------------|--------------|------|
| root 节点 | `""` | 挂载根，空字符串 |
| dir 节点 | `gis-data` | 挂载点内的一级目录 |
| dir 节点（嵌套） | `gis-data/shp` | 多级目录 |
| file 数据项 | `gis-data/sample.csv` | 目录内的文件 |
| file 数据项（根目录） | `README.md` | 挂载根下的文件 |

文件系统的 item 叶子类型固定为 `file`。文件内容可以是表格、文档、媒体或容器；这些语义进入 `attributes.item.data_type` 和 `type_info.<data_type>`，不把文件系统中的 `item_type` 改写为 `table`。

**NFS 物理路径转换公式**：`物理路径 = "/" + full_name`

| full_name | 物理路径 |
|-----------|---------|
| `""` | `/` |
| `gis-data` | `/gis-data` |
| `gis-data/sample.csv` | `/gis-data/sample.csv` |
| `README.md` | `/README.md` |

文件系统的 `CatalogPath` 必须继续遵守 `root -> directory* -> file` 模型；内容读取不能因为底层 NFS 需要 `/a/b.csv` 这样的物理路径，就额外发明单段 `path` 语义。对象存储同理，内容读取仍使用 `bucket -> prefix* -> object` 的 item path。物理路径只作为 storage attribute 或插件内部解析结果存在，不成为第二套上层路径模型。

### 扫描请求路径字段

当 Meta、Manager 或其他模块按路径触发扫描时，请求字段统一使用 `catalog_paths`。该字段承载的是本章定义的引擎 catalog path：

| 引擎 | `catalog_paths` 示例 | 含义 |
|---|---|---|
| MinIO / S3 | `addp/image/photo.jpg` | bucket `addp` 下的 object |
| NFS | `/gis-data/sample.csv` 或 `gis-data/sample.csv` | 挂载点内的文件 catalog path |

路径型扫描目标统一使用 `catalog_paths`，不得在新增接口、前端状态名或跨模块客户端中引入存储族专属字段名。

`catalog_paths` 只承载路径型 selector，不承载 multi content 的 sibling refs 边界。Shapefile 等由多个 file/object 共同组成的 data item，如果调用方已经掌握本次实际生成或变更的 refs，应通过 Meta 扫描请求的 `ref_groups` 提交；不得把父目录或父 prefix 放入 `catalog_paths` 来间接要求 Meta 猜测本次 refs。

`ref_groups` 中的 path 仍必须遵守所属引擎的内容路径语义：对象存储以 bucket 开头，文件系统使用挂载根内相对路径或可被规范化为相对路径的输入。它不是 ResourceLocator，也不携带 `node_id` / `item_id`；进入 Meta 后由 ScanScope resolver 转换为引擎对应的 content ref 或 catalog path。

### Meta scan 内部路径语义

Meta scan 内部必须把“跨模块输入路径”和“扫描期规范化资源路径”分开处理。外部请求字段可以使用所属引擎的完整 content path；进入对象存储 catalog/resource 规划层后，bucket 必须成为独立 root 事实，不得继续混入 bucket 内 object key。

| 路径语义 | 对象存储示例 | 文件系统 / NFS 示例 | 说明 |
|---|---|---|---|
| 外部 `catalog_paths` | `addp/image/photo.jpg` | `gis-data/sample.csv` | 跨模块请求输入，按所属引擎 catalog path 表达。 |
| 外部 `ref_groups.path` | `addp/shp/roads.shp` | `shp/roads.shp` | 跨模块提交的一组 content refs；对象存储必须含 bucket，NFS 不含 `export_path`。 |
| scan root / root name | `addp` | `""` | 对象存储为 bucket；NFS 为挂载根结构 root。 |
| 扫描期资源相对路径 | `shp/roads.shp` | `shp/roads.shp` | 对象存储为 bucket 内 object key；NFS 为挂载根内相对路径。 |
| 完整 content path / `full_name` | `addp/shp/roads.shp` | `shp/roads.shp` | data item 身份、primary content 和 ref 对外表达使用的完整路径。 |
| `attributes.storage.path` | `shp/` | `shp/` | 目录路径，不含 bucket / root，不含文件名。 |
| `attributes.storage.physical_path` | `addp/shp/roads.shp` | `shp/roads.shp` | 可还原 primary content 的完整 content path；不得作为第二套 catalog path 模型。 |

实现约束：

1. 对象存储 `ref_groups.path` 进入 Meta 后应先拆为 `bucket` 与 `object_key`；scan resource 的 `Path` 类字段只允许保存 `object_key`，完整路径另行保存为 `bucket/object_key`。
2. 对象存储 `CatalogPathForBucket(bucket)` 这类 mapper 只允许消费 bucket 内 `object_key`；需要消费 `bucket/object_key` 时必须使用命名明确的 mapper，不得混用。
3. 对象存储普通 `catalog_paths` scan 与 `ref_groups` scan 对同一个 object 必须生成一致的 scan resource 语义、`meta_item.full_name`、`attributes.storage.*` 和指纹输入。
4. 文件系统 / NFS 没有 bucket 层，扫描期资源相对路径、完整 content path 与 `full_name` 在字符串上通常相同；实现不得为了对齐对象存储而给 NFS 额外引入 root 前缀或 bucket-like 段。
5. `physical_path` 只表达已裁决 item 的 primary content 或 whole scope 根范围；扫描实现不得把它当作可自由拼接的 catalog selector，也不得把对象存储的 `bucket/object_key` 再交给只接受 `object_key` 的 mapper。

### 关系型数据库（PostgreSQL / MySQL / Doris / ClickHouse）

full_name 使用引擎原生术语：

- PostgreSQL：`<schema>.<table>`
- MySQL / Doris / ClickHouse：`<database>.<table>`

| 节点/数据项类型 | full_name 示例 | 说明 |
|--------------|--------------|------|
| PostgreSQL schema 节点 | `public` | PostgreSQL schema 名 |
| MySQL/Doris/ClickHouse database 节点 | `analytics` | database 名 |
| table 数据项 | `public.users` / `analytics.users` | `<schema|database> + 表名` |
| view 数据项 | `public.v_active_users` | `<schema|database> + 视图名（若引擎支持）` |

**full_name 自动计算**：schema 或 database 节点传入 `nil`，由 `UpsertNode` 自动计算为 `parent.full_name + "." + name`。

### Branch/Leaf 型引擎（MongoDB / Neo4j）

full_name 由 `database.item` 两段组成，使用 `.` 分隔：

| 引擎 | 节点/数据项类型 | full_name 示例 | 说明 |
|------|--------------|--------------|------|
| MongoDB | database 节点 | `mydb` | MongoDB database 名 |
| MongoDB | collection 数据项 | `mydb.orders` | database + collection 名 |
| Neo4j | database 节点 | `neo4j` | Neo4j database 名 |
| Neo4j | graph 数据项 | `neo4j.graph` | database + graph item 名 |

---

## 三、元数据树结构

### 对象存储（MinIO / S3）

```
Engine (MinIO)
  └── service: Business MinIO ← full_name=""
        └── bucket: addp      ← full_name="addp"
              ├── prefix: image   ← full_name="addp/image"
              │     └── item: photo.jpg
              └── item: data.csv
```

service root 作为结构入口展示，引擎实例名称作为标题。bucket 作为独立业务节点展示，用户可以按 bucket 触发扫描。

### 文件系统（NFS）

```
Engine (Business NFS)
  └── root: Business NFS      ← full_name=""
        ├── dir: gis-data
        │     └── ...
        └── file: README.md
```

root 节点在数据库和 Manager 资源树中都显式存在，标题使用引擎实例名称。目录选择器等需要表达挂载根原生语义时，可以读取 `meta_node.attributes.catalog.native_name="/"`。

#### NFS root、name 与 full_name 的语义定位

NFS 的 root 容易混淆，是因为它不像 MinIO bucket 或 PostgreSQL schema 那样是用户明确创建、可见且有业务名称的根对象。

MinIO/S3 的 bucket 同时是可见节点和业务路径起点：

```text
bucket.name = "addp"
bucket.full_name = "addp"
```

PostgreSQL 的 schema 也同时是可见节点和业务路径起点：

```text
schema.name = "public"
schema.full_name = "public"
```

NFS 不同。NFS root 是“挂载点内的结构性根”，不是业务路径本身：

```text
root.name = "Business NFS"
root.full_name = ""
root 的 `meta_node.attributes.catalog.native_name = "/"`
```

因此，NFS 在 ADDP 中要按四层语义分别处理：

| 层次 | 字段 / 概念 | NFS root 取值 | 用途 |
|---|---|---|---|
| 展示名 | `meta_node.name` | 引擎实例名称 | 资源树结构 root 的标题 |
| 语义路径 | `meta_node.full_name` | `""` | 资源定位、指纹、扫描、预览、Transfer meta 扫描 |
| Meta 树结构 | `meta_node.path` | node id 链 | 表达父子节点关系，不是存储路径 |
| 底层物理路径 | NFS plugin 内部路径 | `/` | 传给 NFS client 的真实文件系统路径 |

这一点是 NFS 与对象存储、数据库 catalog 最大的差异：

```text
Engine 实例
  └── root: Business NFS   ← 结构性 root，不进入 full_name
        ├── file: README.md
        ├── dir: shp
        │     └── file: shp/a3.shp
        └── dir: exports
              └── file: exports/a.csv
```

root 不透明化。根目录文件的 `node_id` 指向 root node；`shp/a3.shp` 的 `node_id` 必须指向 `shp` dir node，不能因为 `full_name` 已包含 `shp/` 就挂到 root node 下。

一句话原则：

```text
NFS root 是结构上必须存在、语义路径上为空、展示标题使用引擎实例名称的节点。
```

NFS 必须创建 root meta_node，且 root 的 `name` 必须使用引擎实例名称。这是统一显性 catalog root 的结构性要求：

- NFS 的 `export_path` 属于连接配置，不得暴露为数据路径，也不得进入 `full_name`。
- 挂载根目录下直接存在的文件必须有父 node 容纳；该父 node 就是 root meta_node。
- root meta_node 是元数据树结构根，不是用户真实数据路径的一部分。
- `.` 只是底层文件系统 API 可接受的当前目录写法，不得进入 CatalogPath、`full_name`、ResourceLocator 或 Transfer 任务 JSON。

root 节点字段规范：

| 字段 | 值 |
|------|-----|
| `name` | 引擎实例名称 |
| `full_name` | `""` |
| `node_type` | `root` |
| `meta_node.attributes.catalog.native_name` | `/` |

这里三个字段含义不同，不能互相替代：

- `name` 是节点名 / 展示名；统一使用引擎实例名称。
- `full_name` 是引擎内资源语义路径；NFS 根路径为空字符串。
- `path` 是 Meta 内部节点层级路径，由 node id 组成，不表达存储路径。

### 关系型数据库（PostgreSQL / MySQL / Doris / ClickHouse）

```
Engine (PostgreSQL)
  └── server: Business PG    ← full_name=""
        ├── schema: public         ← full_name="public"
        │     ├── item: users      ← full_name="public.users"
        │     └── item: orders     ← full_name="public.orders"
        └── schema: gis            ← full_name="gis"
              └── item: regions    ← full_name="gis.regions"
```

```
Engine (MySQL)
  └── server: Business MySQL ← full_name=""
        └── database: analytics    ← full_name="analytics"
              ├── item: users      ← full_name="analytics.users"
              └── item: orders     ← full_name="analytics.orders"
```

server root 是结构入口，标题使用引擎实例名称。schema/database 节点对用户可见；术语按引擎原生语义展示。
系统 schema/database（如 `pg_catalog`、`information_schema`、`mysql`）由插件过滤，不进入元数据树。

### Branch/Leaf 型引擎（MongoDB / Neo4j）

```
Engine (MongoDB)
  └── server: Business Mongo ← full_name=""
        └── database: mydb         ← full_name="mydb"
              ├── item: orders     ← full_name="mydb.orders"      (collection)
              └── item: users      ← full_name="mydb.users"       (collection)
```

```
Engine (Neo4j)
  └── server: Business Neo4j ← full_name=""
        └── database: neo4j        ← full_name="neo4j"
              └── item: graph      ← full_name="neo4j.graph"      (graph)
```

server root 是结构入口，标题使用引擎实例名称。database 作为独立节点展示，用户可以按 database 触发扫描。

---

## 四、ResourceLocator 路径规则

Locator URI 格式：
```
addp://engine/{engine_id}/path/{segments}?type={type}&node_id={node_id}&item_id={item_id}
```

`node_id` 与 `item_id` 互斥，分别表示 MetaNode 与 MetaItem 的真实 ID。不得使用 `meta_id` 混合表达两类 ID，也不得把前端虚拟 ID 编码进 locator。

`type` 表达 catalog / 路径模型中的稳定术语，用于路径语义、路由提示和展示，不表示内容数据类型，也不负责区分 node / item。ID 对应的 Meta 事实优先于 `type`。

`path segments` 由 `full_name` 按 `/` 分割得到。
注意：数据库与 branch/leaf 型引擎的 `full_name` 使用 `.` 分隔（如 `public.users`、`mydb.orders`），
在 Locator 中应先按业务语义转换为路径段（如 `public/users`、`mydb/orders`）。

| 引擎类型 | full_name | path segments | 示例 URI |
|---------|-----------|---------------|---------|
| 引擎目录根 | `""` | `[]` | `.../path/?type=server&node_id=12` / `.../path/?type=service&node_id=12` / `.../path/?type=root&node_id=12` |
| 对象存储 | `addp/image/data.jpg` | `["addp","image","data.jpg"]` | `.../path/addp/image/data.jpg?type=object&item_id=456` |
| NFS | `gis-data/sample.csv` | `["gis-data","sample.csv"]` | `.../path/gis-data/sample.csv?type=file&item_id=789` |
| 关系型数据库 | `public.users` | `["public","users"]` | `.../path/public/users?type=table&item_id=123` |
| MongoDB collection | `mydb.orders` | `["mydb","orders"]` | `.../path/mydb/orders?type=collection&item_id=234` |
| Neo4j graph | `neo4j.graph` | `["neo4j","graph"]` | `.../path/neo4j/graph?type=graph&item_id=578` |

数据库与 branch/leaf 型引擎的解析语义：`branch = path[0]`，`leaf = join(path[1:])`，具体数据项类型由 locator 的 `type` 决定。关系型数据库的 schema/database、MongoDB/Neo4j 的 database 都是 server root 下的第一层 branch。
Neo4j 的 catalog leaf 必须使用 `type=graph`；节点 label、relationship type 和连接模式属于 `type_info.graph`，不得作为独立 catalog leaf。
NFS 物理路径重建公式为 `"/" + join(path, "/")`。

### ADDP infra locator 与业务 ResourceLocator 的边界

`addp://engine/...` 只定位用户可访问的数据引擎资源。ADDP 系统基础设施资源使用内部 `addp-infra://` locator，不属于本章定义的业务 ResourceLocator。

```text
addp-infra://{infra_kind}/{namespace}/{path...}?type={resource_type}
```

第一阶段已使用的 infra locator 形态：

```text
addp-infra://minio/manager/tenant_7/import/20260622/upload-uuid/roads.shp?type=object
addp-infra://minio/manager/tenant_7/export/20260622/execution-id?type=prefix
```

边界规则：

1. `addp-infra://` 只在后端模块间契约中使用，例如 Manager 调用 Transfer sync 时传递导入 source 或导出 target。
2. infra locator 不携带 `engine_id`、`node_id` 或 `item_id`，不得进入前端资源树定位、Meta 扫描定位或用户可见资源选择。
3. infra MinIO 使用系统基础设施 MinIO 配置，不通过 System engines 解析，不产生业务 engine 记录。
4. Transfer endpoint 解析层可以把 `addp-infra://minio/...` 绑定到 infra MinIO 的 engine binding 和 catalog path，后续执行链路仍复用通用 content reader / writer 和 format reader / writer，不在 planner / executor 为 Manager 导入导出建立专用格式分支。
5. 导出到 infra 暂存时 `auto_scan_metadata=false`；导入到业务库后是否扫描目标由 Transfer / Meta 正常任务链路处理。

---

## 五、扫描行为规范

### 扫描触发粒度

| 引擎类型 | 扫描单元 | 说明 |
|---------|---------|------|
| 对象存储（MinIO/S3） | bucket / path | 可按 bucket 或指定路径触发扫描 |
| NFS | 挂载根 `/` 或任意目录路径 | 可扫描整个挂载点，也可按目录路径扫描；扫描非根路径时必须先确保 root -> directory 节点链存在 |
| 关系型数据库（PostgreSQL/MySQL/Doris/ClickHouse） | schema 或 database | 用户按引擎术语选择（PostgreSQL 选 schema；MySQL/Doris/ClickHouse 选 database） |
| Branch/Leaf 型引擎（MongoDB/Neo4j） | database branch | 用户选择一个或多个 database 触发扫描 |

### NFS 扫描流程

1. 通过 `CatalogRootEntry(model, engineID, engineName)` 获取结构 root，CatalogPath 包含 root segment，但其 `StringPath()` 为空。
2. 创建 root `meta_node`，`name = engine.name`，`full_name = ""`，`meta_node.attributes.catalog.native_name="/"`。
3. 扫描根目录时，递归扫描 `/` 下的所有目录和文件。
4. 扫描非根目录时，先按 `catalog_paths` 确保从 root 到目标目录的 `dir meta_node` 链存在，再把扫描上下文切换到该目录 node。
5. 子目录创建 dir `meta_node`，`full_name = 目录相对路径`。
6. 文件创建 `meta_item`（`item_type = file`），`node_id` 指向所在目录 node，`full_name = 文件相对路径`。
7. 文件格式和内容语义写入 `attributes.item.data_type`、`attributes.item.format`、`attributes.item.layout` 等 attributes 分区。

### 对象存储扫描流程

1. upsert service root `meta_node`，再通过 `CatalogProvider.ListChildren(root)` 获取 bucket 列表，创建 bucket `meta_node`
2. 通过 `CatalogProvider.ListChildren(bucket/prefix)` 获取 prefix 和 object
3. prefix 创建 `meta_node`（`node_type = prefix`），`full_name = bucket + "/" + prefix`
4. object 创建 `meta_item`（`item_type = object`），`full_name = bucket + "/" + object_key`
5. object 的格式和内容语义写入 `attributes.item.data_type`、`attributes.item.format`、`attributes.item.layout` 等 attributes 分区

### 关系型数据库扫描流程

1. 通过 `CatalogProvider.ListChildren(root)` 获取 namespace 列表（PostgreSQL 为 schema；MySQL/Doris/ClickHouse 为 database）
2. 插件负责过滤系统 schema/database，或通过 `CatalogCapability.system_filtering` 声明过滤能力
3. upsert server root `meta_node`，为每个 schema 或 database 创建子 `meta_node`
4. 通过 `CatalogProvider.ListChildren(namespace)` 获取表/视图，创建 `meta_item`（`item_type = table/view`）
5. `meta_item.full_name` 使用 `<schema|database>.<table>`

### MongoDB / Neo4j 扫描流程

1. 通过 `CatalogProvider.ListChildren(root)` 获取第一层业务 branch（MongoDB / Neo4j 为 database）
2. upsert server root `meta_node`，为每个 database branch 创建子 `meta_node`（`node_type = database`，`full_name = database`）
3. 通过 `CatalogProvider.ListChildren(database branch)` 获取 collection/graph leaf，创建 `meta_item`
4. `meta_item.full_name` 使用 `database.collection`（Neo4j 为 `database.graph`）
5. Neo4j label、relationship type 和 endpoint pattern 写入 `attributes.type_info.graph`，不作为独立 `meta_item`

---

## 六、full_name 自动计算规则

`UpsertNode` 写入节点时，`fullName` 参数：

- 传入 `nil`：由系统自动计算，规则为 `parent.full_name + "." + name`（用于关系型数据库 schema/database 节点、MongoDB/Neo4j database 节点）
- 传入具体值（含空字符串）：直接使用，不覆盖（用于对象存储和文件系统，full_name 由扫描服务显式计算）

关系型数据库表和 branch/leaf 型 collection/graph 的 `full_name` 由扫描服务显式拼接：

- 关系型数据库：`schema + "." + table`（PostgreSQL）或 `database + "." + table`（MySQL/Doris/ClickHouse）
- MongoDB：`database + "." + collection`
- Neo4j：`database + ".graph"`

文件系统各节点 full_name 的计算由 `filesystem_scan_service` 负责，遵循本文第二节的规则。

---

## 七、对象存储与文件系统模型边界

对象存储和文件系统在树形浏览和内容流读取上有相似性，但路径模型和 catalog 模型不能共享：

| 维度 | 对象存储（MinIO/S3） | 文件系统（NFS、本地 FS） |
| --- | --- | --- |
| 根节点 | bucket，有业务含义，可见，可有多个 | root，结构性根，通常一个 |
| 中间层 | prefix，通常是 key 前缀，不一定真实存在 | directory，真实目录实体 |
| 叶子 | object | file |
| 路径起点 | bucket | 挂载点内 `/` 或本地 `/` |
| root 是否进入 full_name | bucket 进入 full_name | root 不进入 NFS full_name |

规范要求：

- 对象存储使用 `bucket -> prefix -> object`。
- 文件系统使用 `root -> directory -> file`。
- 对象存储中的 `meta_item.item_type` 必须使用 `object`，文件系统中的 `meta_item.item_type` 必须使用 `file`；表格、文档、媒体、容器等内容语义进入 `attributes.item.data_type`。
- 对象存储和文件系统不得共享 CatalogModel 或 catalog 拼装实现。
- 二者可以共享内容流读写接口、MIME 推断、格式解析、preview composer 等底层能力。
- Linux / macOS 本地文件系统后续也必须有结构性 root meta_node，用于容纳根目录下文件；展示名可另行确认，但不得省略 root。
