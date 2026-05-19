# 存储引擎路径体系规范

本文定义 ADDP 中所有已支持存储引擎的路径体系，
作为元数据扫描、资源定位、数据预览等模块的设计规范。

当前已支持的存储引擎（按插件类型分类）：

| 引擎类型标识 | 显示名称 | 主要 Provider |
|------------|---------|---------|
| `postgresql` | PostgreSQL | CatalogProvider + ItemMetadataProvider + SQLQueryRuntimeProvider |
| `mysql` | MySQL | CatalogProvider + ItemMetadataProvider + SQLQueryRuntimeProvider |
| `doris` | Apache Doris | CatalogProvider + ItemMetadataProvider + SQLQueryRuntimeProvider |
| `clickhouse` | ClickHouse | CatalogProvider + ItemMetadataProvider + SQLQueryRuntimeProvider |
| `mongodb` | MongoDB | CatalogProvider + ItemMetadataProvider + DocumentQueryRuntimeProvider |
| `neo4j` | Neo4j | CatalogProvider + QueryRuntimeProvider + GraphQueryProvider |
| `minio` | MinIO | CatalogProvider + ContentReadableProvider |
| `s3` | Amazon S3 | CatalogProvider + ContentReadableProvider |
| `nfs` | NFS 文件系统 | CatalogProvider + ContentReadableProvider |

---

## 一、核心概念

### 引擎配置 vs 数据路径

**关键原则**：存储引擎的连接参数（如 NFS 的 `export_path`、MinIO 的 `endpoint`）是**配置**，
不是数据路径的一部分，不得出现在 full_name、ResourceLocator 或任何对用户可见的路径中。

| 引擎 | 连接参数（配置层，不进入路径） | 数据路径的起点 |
|------|--------------------------|-------------|
| MinIO/S3 | endpoint、access_key | bucket |
| NFS | server、export_path | 挂载点内的 `/` |
| PostgreSQL | host、port、user | schema |
| MySQL/Doris/ClickHouse | host、port、user | database |
| MongoDB/Neo4j | host、port | database |

### 根节点的性质差异

| | 对象存储（MinIO/S3） | 文件系统（NFS） | 关系型数据库 | Namespace/Item 型引擎（MongoDB/Neo4j） |
|---|---------|--------------|------------|----------------|
| 根的名称 | bucket 名（有业务含义） | 无（挂载点透明） | PostgreSQL 为 schema 名；MySQL/Doris/ClickHouse 为 database 名 | database 名 |
| 根的数量 | 多个（一个引擎多个 bucket） | 一个（一个引擎一个挂载点） | 多个（一个引擎多个 schema/database） | 多个（一个引擎多个 database） |
| 根是否对用户可见 | 是 | 否 | 是 | 是 |

### item_type 与 data_type 分工

`meta_item.item_type` 表达 data item 在所属引擎 catalog / 路径模型中的原生叶子术语，用于路由、树展示和路径解析；`attributes.item.data_type` 表达平台对内容语义的理解，用于预览、读取、检索和传输能力选择。二者不得混用。

| 引擎类型 | 路径模型叶子 | `meta_item.item_type` | 内容语义示例 |
|---|---|---|---|
| MinIO / S3 | object | `object` | `data_type=table/document/media/container`，`format=csv/wps/png/excel` |
| NFS / 本地文件系统 | file | `file` | `data_type=table/document/media/container`，`format=csv/wps/png/excel` |
| PostgreSQL / MySQL / Doris / ClickHouse | table / view | `table` / `view` | 通常 `data_type=table` |
| MongoDB | collection | `collection` | 当前按表格型文档集合消费时可为 `data_type=table` |
| Neo4j | label / relationship | `label` / `relationship` | `data_type=graph` |

因此，同一个 `sales.csv` 在 MinIO 中是 `item_type=object`，在 NFS 中是 `item_type=file`；它们都可以同时拥有 `attributes.item.data_type=table`、`attributes.item.format=csv`。不得因为对象内容可按表格读取，就把对象存储中的 `item_type` 写成 `table`。

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

### Namespace/Item 型引擎（MongoDB / Neo4j）

full_name 由 `database.collection` 两段组成，使用 `.` 分隔：

| 引擎 | 节点/数据项类型 | full_name 示例 | 说明 |
|------|--------------|--------------|------|
| MongoDB | database 节点 | `mydb` | MongoDB database 名 |
| MongoDB | collection 数据项 | `mydb.orders` | database + collection 名 |
| Neo4j | database 节点 | `neo4j` | Neo4j database 名 |
| Neo4j | label 数据项 | `neo4j.Person` | database + label 名 |
| Neo4j | relationship 数据项 | `neo4j.WORKS_FOR` | database + relationship 名 |

---

## 三、元数据树结构

### 对象存储（MinIO / S3）

```
Engine (MinIO)
  └── bucket: addp          ← full_name="addp"
        ├── prefix: image   ← full_name="addp/image"
        │     └── item: photo.jpg
        └── item: data.csv
```

bucket 作为独立节点展示，用户可以按 bucket 触发扫描。

### 文件系统（NFS）

```
meta_node 存储结构：        用户看到的树：
  root (full_name="")        Engine (Business NFS)
    ├── dir: gis-data          ├── gis-data/
    │     └── ...              │     └── ...
    └── file: README.md        └── README.md
```

root 节点在数据库中存在（作为顶层子节点的父节点），但在 Manager 等资源树展示层透明化——
其子节点直接挂到引擎节点下，用户不感知 root 这一层。目录选择器等需要让用户选择挂载根时，可以把该结构性根显示为 `/`。

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
root.name = "/"
root.full_name = ""
```

因此，NFS 在 ADDP 中要按四层语义分别处理：

| 层次 | 字段 / 概念 | NFS root 取值 | 用途 |
|---|---|---|---|
| 展示名 | `meta_node.name` | `/` | 节点标题、目录选择器中的根目录显示 |
| 语义路径 | `meta_node.full_name` | `""` | 资源定位、指纹、扫描、预览、Transfer meta 扫描 |
| Meta 树结构 | `meta_node.path` | node id 链 | 表达父子节点关系，不是存储路径 |
| 底层物理路径 | NFS plugin 内部路径 | `/` | 传给 NFS client 的真实文件系统路径 |

这一点是 NFS 与对象存储、数据库 catalog 最大的差异：

```text
Engine 实例
  └── root: /              ← 结构性 root，不进入 full_name
        ├── file: README.md
        ├── dir: shp
        │     └── file: shp/a3.shp
        └── dir: exports
              └── file: exports/a.csv
```

资源树展示可以透明化 root：

```text
NFS 引擎
  ├── README.md
  ├── shp
  │   └── a3.shp
  └── exports
      └── a.csv
```

但透明化只属于展示层，不得改变 Meta 中的父子关系。根目录文件的 `node_id` 指向 root node；`shp/a3.shp` 的 `node_id` 必须指向 `shp` dir node，不能因为 `full_name` 已包含 `shp/` 就挂到 root node 下。

一句话原则：

```text
NFS root 是结构上必须存在、语义路径上为空、展示上可透明的节点。
```

NFS 必须创建 root meta_node，且 root 的 `name` 必须为 `/`。这是文件系统路径模型的结构性要求：

- NFS 的 `export_path` 属于连接配置，不得暴露为数据路径，也不得进入 `full_name`。
- 挂载根目录下直接存在的文件必须有父 node 容纳；该父 node 就是 root meta_node。
- root meta_node 是元数据树结构根，不是用户真实数据路径的一部分。
- `.` 只是底层文件系统 API 可接受的当前目录写法，不得进入 CatalogPath、`full_name`、ResourceLocator 或 Transfer 任务 JSON。

root 节点字段规范：

| 字段 | 值 |
|------|-----|
| `name` | `/` |
| `full_name` | `""` |
| `node_type` | `root` |
| `attributes.path` | `""` |

这里三个字段含义不同，不能互相替代：

- `name` 是节点名 / 展示名；NFS 根显示为 `/`。
- `full_name` 是引擎内资源语义路径；NFS 根路径为空字符串。
- `path` 是 Meta 内部节点层级路径，由 node id 组成，不表达存储路径。

### 关系型数据库（PostgreSQL / MySQL / Doris / ClickHouse）

```
Engine (PostgreSQL)
  ├── schema: public         ← full_name="public"
  │     ├── item: users      ← full_name="public.users"
  │     └── item: orders     ← full_name="public.orders"
  └── schema: gis            ← full_name="gis"
        └── item: regions    ← full_name="gis.regions"
```

```
Engine (MySQL)
  └── database: analytics    ← full_name="analytics"
        ├── item: users      ← full_name="analytics.users"
        └── item: orders     ← full_name="analytics.orders"
```

schema/database 节点对用户可见；术语按引擎原生语义展示。
系统 schema/database（如 `pg_catalog`、`information_schema`、`mysql`）由插件过滤，不进入元数据树。

### Namespace/Item 型引擎（MongoDB / Neo4j）

```
Engine (MongoDB)
  └── database: mydb         ← full_name="mydb"
        ├── item: orders     ← full_name="mydb.orders"      (collection)
        └── item: users      ← full_name="mydb.users"       (collection)
```

```
Engine (Neo4j)
  └── database: neo4j        ← full_name="neo4j"
        ├── item: Person     ← full_name="neo4j.Person"     (label)
        └── item: Company    ← full_name="neo4j.Company"    (label)
```

database 作为独立节点展示，用户可以按 database 触发扫描。

---

## 四、ResourceLocator 路径规则

Locator URI 格式：
```
addp://engine/{engine_id}/path/{segments}?type={type}
```

`path segments` 由 `full_name` 按 `/` 分割得到。
注意：数据库与 namespace/item 型引擎的 `full_name` 使用 `.` 分隔（如 `public.users`、`mydb.orders`），
在 Locator 中应先按业务语义转换为路径段（如 `public/users`、`mydb/orders`）。

| 引擎类型 | full_name | path segments | 示例 URI |
|---------|-----------|---------------|---------|
| 对象存储 | `addp/image/data.jpg` | `["addp","image","data.jpg"]` | `.../path/addp/image/data.jpg?type=object` |
| NFS | `gis-data/sample.csv` | `["gis-data","sample.csv"]` | `.../path/gis-data/sample.csv?type=file` |
| NFS 根 | `""` | `[]` | `.../path/?type=root` |
| 关系型数据库 | `public.users` | `["public","users"]` | `.../path/public/users?type=table` |
| MongoDB collection | `mydb.orders` | `["mydb","orders"]` | `.../path/mydb/orders?type=collection` |
| Neo4j label | `neo4j.Person` | `["neo4j","Person"]` | `.../path/neo4j/Person?type=label` |
| Neo4j relationship | `neo4j.WORKS_FOR` | `["neo4j","WORKS_FOR"]` | `.../path/neo4j/WORKS_FOR?type=relationship` |

数据库与 namespace/item 型引擎的解析语义：`namespace = path[0]`，`item = join(path[1:])`，具体数据项类型由 locator 的 `type` 决定。
Neo4j 的节点标签必须使用 `type=label`，关系类型必须使用 `type=relationship`，不得折叠为 `type=collection`。
NFS 物理路径重建公式为 `"/" + join(path, "/")`。

---

## 五、扫描行为规范

### 扫描触发粒度

| 引擎类型 | 扫描单元 | 说明 |
|---------|---------|------|
| 对象存储（MinIO/S3） | bucket / path | 可按 bucket 或指定路径触发扫描 |
| NFS | 挂载根 `/` 或任意目录路径 | 可扫描整个挂载点，也可按目录路径扫描；扫描非根路径时必须先确保 root -> directory 节点链存在 |
| 关系型数据库（PostgreSQL/MySQL/Doris/ClickHouse） | schema 或 database | 用户按引擎术语选择（PostgreSQL 选 schema；MySQL/Doris/ClickHouse 选 database） |
| Namespace/Item 型引擎（MongoDB/Neo4j） | database | 用户选择一个或多个 database 触发扫描 |

### NFS 扫描流程

1. 通过 `CatalogProvider.ListChildren(root)` 返回唯一根节点，`Name = "/"`，CatalogPath 包含 root segment，但其 `StringPath()` 为空。
2. 创建 root `meta_node`，`name = "/"`，`full_name = ""`。
3. 扫描根目录时，递归扫描 `/` 下的所有目录和文件。
4. 扫描非根目录时，先按 `catalog_paths` 确保从 root 到目标目录的 `dir meta_node` 链存在，再把扫描上下文切换到该目录 node。
5. 子目录创建 dir `meta_node`，`full_name = 目录相对路径`。
6. 文件创建 `meta_item`（`item_type = file`），`node_id` 指向所在目录 node，`full_name = 文件相对路径`。
7. 文件格式和内容语义写入 `attributes.item.data_type`、`attributes.item.format`、`attributes.item.layout` 等 attributes 分区。

### 对象存储扫描流程

1. 通过 `CatalogProvider.ListChildren(root)` 获取 bucket 列表，创建 bucket `meta_node`
2. 通过 `CatalogProvider.ListChildren(bucket/prefix)` 获取 prefix 和 object
3. prefix 创建 `meta_node`（`node_type = prefix`），`full_name = bucket + "/" + prefix`
4. object 创建 `meta_item`（`item_type = object`），`full_name = bucket + "/" + object_key`
5. object 的格式和内容语义写入 `attributes.item.data_type`、`attributes.item.format`、`attributes.item.layout` 等 attributes 分区

### 关系型数据库扫描流程

1. 通过 `CatalogProvider.ListChildren(root)` 获取 namespace 列表（PostgreSQL 为 schema；MySQL/Doris/ClickHouse 为 database）
2. 插件负责过滤系统 schema/database，或通过 `CatalogCapability.system_filtering` 声明过滤能力
3. 为每个 schema 或 database 创建 `meta_node`
4. 通过 `CatalogProvider.ListChildren(namespace)` 获取表/视图，创建 `meta_item`（`item_type = table/view`）
5. `meta_item.full_name` 使用 `<schema|database>.<table>`

### MongoDB / Neo4j 扫描流程

1. 通过 `CatalogProvider.ListChildren(root)` 获取 database 列表
2. 为每个 database 创建 `meta_node`（`node_type = database`，`full_name = database`）
3. 通过 `CatalogProvider.ListChildren(database)` 获取 collection/label/relationship，创建 `meta_item`
4. `meta_item.full_name` 使用 `database.collection`（Neo4j 为 `database.label` / `database.relationship`）
5. Neo4j 节点标签使用 `item_type = label`，关系类型使用 `item_type = relationship`

---

## 六、full_name 自动计算规则

`UpsertNode` 写入节点时，`fullName` 参数：

- 传入 `nil`：由系统自动计算，规则为 `parent.full_name + "." + name`（用于关系型数据库 schema/database 节点、MongoDB/Neo4j database 节点）
- 传入具体值（含空字符串）：直接使用，不覆盖（用于对象存储和文件系统，full_name 由扫描服务显式计算）

关系型数据库表和 namespace/item 型 collection/label 的 `full_name` 由扫描服务显式拼接：

- 关系型数据库：`schema + "." + table`（PostgreSQL）或 `database + "." + table`（MySQL/Doris/ClickHouse）
- MongoDB：`database + "." + collection`
- Neo4j：`database + "." + label`

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
