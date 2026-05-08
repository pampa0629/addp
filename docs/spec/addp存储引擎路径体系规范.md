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

| | 对象存储（MinIO/S3） | 文件系统（NFS） | 关系型数据库 | NoSQL（MongoDB/Neo4j） |
|---|---------|--------------|------------|----------------|
| 根的名称 | bucket 名（有业务含义） | 无（挂载点透明） | PostgreSQL 为 schema 名；MySQL/Doris/ClickHouse 为 database 名 | database 名 |
| 根的数量 | 多个（一个引擎多个 bucket） | 一个（一个引擎一个挂载点） | 多个（一个引擎多个 schema/database） | 多个（一个引擎多个 database） |
| 根是否对用户可见 | 是 | 否 | 是 | 是 |

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
| lake_table | `addp/data/orders` |

### 文件系统（NFS）

full_name 是相对于挂载点的路径，不包含挂载点本身：

| 节点/数据项类型 | full_name 示例 | 说明 |
|--------------|--------------|------|
| root 节点 | `""` | 挂载根，空字符串 |
| dir 节点 | `gis-data` | 挂载点内的一级目录 |
| dir 节点（嵌套） | `gis-data/shp` | 多级目录 |
| file 数据项 | `gis-data/sample.csv` | 目录内的文件 |
| file 数据项（根目录） | `README.md` | 挂载根下的文件 |
| lake_table | `gis-data/orders` | 目录识别为湖表 |

**NFS 物理路径转换公式**：`物理路径 = "/" + full_name`

| full_name | 物理路径 |
|-----------|---------|
| `""` | `/` |
| `gis-data` | `/gis-data` |
| `gis-data/sample.csv` | `/gis-data/sample.csv` |
| `README.md` | `/README.md` |

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

### NoSQL（MongoDB / Neo4j）

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

root 节点在数据库中存在（作为顶层子节点的父节点），但在展示层透明化——
其子节点直接挂到引擎节点下，用户不感知 root 这一层。

NFS 必须创建 root meta_node，且 root 的 `name` 必须为 `.`。这是文件系统路径模型的结构性要求：

- NFS 的 `export_path` 属于连接配置，不得暴露为数据路径，也不得进入 `full_name`。
- 挂载根目录下直接存在的文件必须有父 node 容纳；该父 node 就是 root meta_node。
- root meta_node 是元数据树结构根，不是用户真实数据路径的一部分。
- 当前 NFS 实现中以 `.` 作为唯一 root 的效果是正确的，后续重构不得省略或改错。

root 节点字段规范：

| 字段 | 值 |
|------|-----|
| `name` | `.` |
| `full_name` | `""` |
| `node_type` | `root` |
| `attributes.storage.path` | `/` |

`name = "."` 源自 Unix 惯例的"当前目录"含义，同时避免暴露 `export_path`。

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

### NoSQL（MongoDB / Neo4j）

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
注意：数据库与 NoSQL 的 `full_name` 使用 `.` 分隔（如 `public.users`、`mydb.orders`），
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

数据库/NoSQL/Graph 的解析语义：`schema_or_db = path[0]`，`item = join(path[1:])`，具体数据项类型由 locator 的 `type` 决定。
Neo4j 的节点标签必须使用 `type=label`，关系类型必须使用 `type=relationship`，不得折叠为 `type=collection`。
NFS 物理路径重建公式为 `"/" + join(path, "/")`。

---

## 五、扫描行为规范

### 扫描触发粒度

| 引擎类型 | 扫描单元 | 说明 |
|---------|---------|------|
| 对象存储（MinIO/S3） | bucket / path | 可按 bucket 或指定路径触发扫描 |
| NFS | 挂载根 `/` | 只有一个扫描入口，扫描整个挂载点 |
| 关系型数据库（PostgreSQL/MySQL/Doris/ClickHouse） | schema 或 database | 用户按引擎术语选择（PostgreSQL 选 schema；MySQL/Doris/ClickHouse 选 database） |
| NoSQL（MongoDB/Neo4j） | database | 用户选择一个或多个 database 触发扫描 |

### NFS 扫描流程

1. 通过 `CatalogProvider.ListChildren(root)` 返回唯一根节点，`Name = "."`，`Path = "/"`
2. 创建 root `meta_node`，`full_name = ""`
3. 递归扫描 `/` 下的所有目录和文件
4. 子目录创建 dir `meta_node`，`full_name = 目录相对路径`
5. 文件识别为 lake_table 或 file，创建 `meta_item`，`full_name = 文件相对路径`

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

- 传入 `nil`：由系统自动计算，规则为 `parent.full_name + "." + name`（用于关系型数据库 schema/database 节点、NoSQL database 节点）
- 传入具体值（含空字符串）：直接使用，不覆盖（用于对象存储和文件系统，full_name 由扫描服务显式计算）

关系型数据库表和 NoSQL collection/label 的 `full_name` 由扫描服务显式拼接：

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
- 对象存储和文件系统不得共享 CatalogModel 或 catalog 拼装实现。
- 二者可以共享内容流读写接口、MIME 推断、格式解析、preview composer 等底层能力。
- Linux / macOS 本地文件系统后续也必须有结构性 root meta_node，用于容纳根目录下文件；展示名可另行确认，但不得省略 root。
