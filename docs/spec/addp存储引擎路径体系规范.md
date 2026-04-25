# 存储引擎路径体系规范

本文定义 ADDP 中所有已支持存储引擎的路径体系，
作为元数据扫描、资源定位、数据预览等模块的设计规范。

当前已支持的存储引擎（按插件类型分类）：

| 引擎类型标识 | 显示名称 | 插件接口 |
|------------|---------|---------|
| `postgresql` | PostgreSQL | RelationalDBPlugin |
| `mysql` | MySQL | RelationalDBPlugin |
| `doris` | Apache Doris | RelationalDBPlugin |
| `clickhouse` | ClickHouse | RelationalDBPlugin |
| `mongodb` | MongoDB | NoSQLPlugin |
| `neo4j` | Neo4j | NoSQLPlugin（GraphDBPlugin） |
| `minio` | MinIO | ObjectStoragePlugin（继承 FileSystemPlugin） |
| `s3` | Amazon S3 | ObjectStoragePlugin（继承 FileSystemPlugin） |
| `nfs` | NFS 文件系统 | FileSystemPlugin |

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

root 节点字段规范：

| 字段 | 值 |
|------|-----|
| `name` | `.` |
| `full_name` | `""` |
| `node_type` | `root` |
| `attributes.path` | `/` |

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
| 对象存储 | `addp/image/data.jpg` | `["addp","image","data.jpg"]` | `.../path/addp/image/data.jpg` |
| NFS | `gis-data/sample.csv` | `["gis-data","sample.csv"]` | `.../path/gis-data/sample.csv` |
| NFS 根 | `""` | `[]` | `.../path/` |
| 关系型数据库 | `public.users` | `["public","users"]` | `.../path/public/users?type=table` |
| NoSQL | `mydb.orders` / `neo4j.Person` / `neo4j.WORKS_FOR` | `["mydb","orders"]` / `["neo4j","Person"]` / `["neo4j","WORKS_FOR"]` | `.../path/mydb/orders?type=collection` / `.../path/neo4j/Person?type=collection` / `.../path/neo4j/WORKS_FOR?type=collection` |

数据库/NoSQL 的解析语义：`schema_or_db = path[0]`，`table_or_collection = join(path[1:])`。
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

1. `ListRoots()` 返回唯一根节点，`Name = "."`，`Path = "/"`
2. 创建 root `meta_node`，`full_name = ""`
3. 递归扫描 `/` 下的所有目录和文件
4. 子目录创建 dir `meta_node`，`full_name = 目录相对路径`
5. 文件识别为 lake_table 或 file，创建 `meta_item`，`full_name = 文件相对路径`

### 关系型数据库扫描流程

1. 通过 `RelationalDBPlugin.ListSchemas()` 获取列表（PostgreSQL 为 schema；MySQL/Doris/ClickHouse 为 database）
2. 过滤系统 schema/database（`IsSystemSchema()`）
3. 为每个 schema 或 database 创建 `meta_node`
4. 通过 `ListTables()` 获取表/视图，创建 `meta_item`（`item_type = table/view`）
5. `meta_item.full_name` 使用 `<schema|database>.<table>`

### MongoDB / Neo4j 扫描流程

1. 通过 `NoSQLPlugin.ListDatabases()` 获取 database 列表
2. 为每个 database 创建 `meta_node`（`node_type = database`，`full_name = database`）
3. 通过 `ListCollections()` 获取集合/label，创建 `meta_item`（MongoDB: `item_type = collection`；Neo4j label: `item_type = label`）
4. `meta_item.full_name` 使用 `database.collection`（Neo4j 为 `database.label` / `database.relationship`）
5. Neo4j 额外通过 `GraphDBPlugin` 扫描关系类型（relationship），以 `item_type = relationship` 落库

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
