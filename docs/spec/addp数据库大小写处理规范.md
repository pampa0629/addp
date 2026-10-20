# ADDP 数据库大小写处理规范

本文规定动态 SQL 中对象名的大小写保留、标识符引用和值参数绑定。适用于平台按真实目录事实生成的 SQL；用户 SQL 的解析、授权和执行仍遵循[引擎插件接口规范](addp引擎插件接口规范.md)，不通过字符串替换统一改写大小写。

## 对象名与方言

外部数据库的 schema / database、表名和列名以引擎目录返回的真实名称为准，保留原始大小写；不能为方便拼接统一转成小写或大写，也不能把混合大小写故障归为数据为空。

PostgreSQL 会把未引用的标识符折叠为小写：`SmGeometry` 会解析成 `smgeometry`，而 `"SmGeometry"` 保留原名。其他引擎的引用符与名称比较规则由其方言决定，不能把“双引号适用于所有引擎”作为跨引擎规范。

职责分工沿用现有共享边界：

| 场景 | 规范入口 |
| --- | --- |
| 跨 SQL 引擎的标识符引用、表名拼接、基础 SELECT / COUNT / 分页与占位符 | [common/query](../../common/query/dialect.go) |
| PostGIS 空间表达式、MVT、GeoJSON 分页、范围、物化视图与 GiST 索引 | [common/spatial](../../common/spatial/README.md) |
| 引擎方言声明与原生能力 | 对应 Engine Plugin 的 `SQLDialectProvider` 及 Provider 实现 |

调用方从已注册插件取得 `SQLDialectProvider.SQLDialect()`，再使用 `common/query.ForDialect()`。不能根据 `engine_type` 猜测方言，或用空值代替插件声明；`ForDialect()` 是格式化工具，默认引用行为不代表它已经校验方言支持或引擎能力。

PostGIS 专用入口使用 `QuotePostGISIdentifier()`、`QualifiedPostGISTable()` 等既有工具。底层引用函数可使用对应驱动实现；通用业务代码不以直接调用 `pq.QuoteIdentifier()` 代替插件方言，也不复制另一套引用规则。共享归属见[共享模块介绍](../concepts/addp共享模块介绍.md)。

## 标识符、值与 SQL 结构

| 输入 | 处理要求 |
| --- | --- |
| schema / database、表名、列名、动态别名 | 对每个名称分别调用当前方言的 `QuoteIdentifier()`；表名拼接使用 `QualifiedTable()` |
| 运行时比较值、筛选参数、动态 JSONB 键 | 使用绑定参数；位置占位符由 `Dialect.Placeholder()` 生成 |
| SELECT 表达式、WHERE 条件、排序方向、函数调用 | 由受控代码或既有编译器构造；不能把任意用户文本交给 SQL 拼装函数 |

受控 SQL 常量沿用既有编译器的类型化编码，运行时参数保持原值并单独绑定。

引用函数负责引用符及其转义，不验证对象是否存在、调用者是否有权访问或 SQL 是否只读。对象解析、名称合法性、授权与只读检查继续由既有入口负责。

- schema / database 与 table 分别引用，不把完整的 `schema.table` 当成一个标识符。若真实列名本身含点号，应作为一个列名整体引用，不能按点号自行拆分。
- 标识符中的引用符必须转义。例如 PostgreSQL 的 `city"name` 应编码为 `"city""name"`；仅在字符串两端手工加双引号不能完成这项工作。
- 引用后的片段只在本次 SQL 构造中使用，不能覆盖原始目录事实、字段名或对外元数据，也不重复引用同一个片段。
- 标识符不能通过值占位符绑定；值也不能用 `QuoteIdentifier()` 编码。不要在 `fmt.Sprintf()` 中插入未经编码的对象名、运行时数据值或 JSONB 键。
- 已有参数时，`Placeholder()` 的序号从 `len(args)+1` 继续，且 SQL 与 Args 保持同一顺序；不能由 Provider 在执行前扫描 SQL 补救。

`SelectTableSQL()` 接收的投影、WHERE 和 ORDER BY 是 SQL 片段，只负责组合，不会解析或自动转义这些表达式。调用方须先引用其中的标识符，并绑定其中的值。

## 通用查询示例

下例中的 `dialect` 已按插件声明取得；名称来自当前目标的已校验目录事实，`limit` 由调用方校验。函数只构造 SQL 与参数，调用方将两者原样交给既有查询和授权执行入口。

```go
package example

import commonquery "github.com/addp/common/query"

func BuildLookupSQL(
	dialect commonquery.Dialect,
	namespace, table, outputColumn, filterColumn string,
	filterValue any,
	limit int,
) (string, []any) {
	projection := dialect.QuoteIdentifier(outputColumn)
	predicate := dialect.QuoteIdentifier(filterColumn) + " = " + dialect.Placeholder(1)
	statement := dialect.SelectTableSQL(
		projection, namespace, table, predicate, "", limit, 0,
	)
	return statement, []any{filterValue}
}
```

该示例通过 `SelectTableSQL()` 分别引用 namespace 与 table；投影和筛选列在进入片段前引用，筛选值留在 Args 中。公共参数契约见[引擎插件接口规范](addp引擎插件接口规范.md)的 `QueryOptions.Args` 与 `QueryOptions.Parameters`。

## PostGIS 与 JSONB

几何列从真实字段及空间元数据中解析，不假定总叫 `geom`。WKT / GeoJSON 表达式、MVT 和派生对象 SQL 复用 [PostGIS 工具](../../common/spatial/postgis.go)及 [MVT 工具](../../common/spatial/mvt.go)，不在 Manager、Service 等 owner 中复制完整模板。

JSONB 中的对象键是文本值，列引用则是标识符。例如从行 JSON 中排除 `SmGeometry` 时，可把键作为 PostgreSQL 绑定参数：

```sql
SELECT to_jsonb(src.*) - $1::text
FROM "PublicData"."TestTable" AS src
```

此时 Args 的首项为字符串 `SmGeometry`。直接读取该列时才使用 `src."SmGeometry"`；固定键的 SQL 文本字面量使用单引号，动态键不手工拼入单引号。现有 `BuildPostGISGeoJSONPageQuery()` 已集中处理列引用与 JSONB 键的编码，调用方复用它，不复制其内部 SQL。

## 排查与验证

出现以下错误时，应比对引擎目录的真实名称、当前 namespace 与生成 SQL；错误本身不能证明一定是大小写问题：

| 错误 | 核对重点 |
| --- | --- |
| `42703` / column does not exist | 实际列名、混合大小写、引用和别名作用域 |
| `42P01` / relation does not exist | 表 / 视图名称、namespace、对象可见性及实际存在性 |
| `3F000` / schema does not exist | schema 名称与当前连接目标 |

修复后的验证按[测试与验收规范](addp测试与验收规范.md)选择标准入口。实现变更运行对应 owner 门禁，共享代码变更按实际消费者扩散。引号测试通过不等于真实数据库或业务出口已验收。

| 验证维度 | 必须保护的结果 |
| --- | --- |
| 混合大小写 | 保留 `PublicData` / `UserSchema`、`TestTable` / `MyData_V2`、`SmGeometry` / `UserName` / `CreateTime` 等真实名称 |
| 关键字、空格与嵌入引用符 | 单个标识符按当前方言完整引用并转义，不被拆成 SQL 结构 |
| 标识符与值混合 | 名称正确引用，值保留原始类型并通过 Args 绑定；已有参数序号不冲突 |
| JSONB 与空间列 | 文本键与列引用分别编码，几何列由元数据解析 |
| 受影响业务出口 | 按改动覆盖预览、要素详情、MVT / 物化视图、导入导出、扫描、SQL 执行或服务查询（含 OGC），不凭历史修复清单宣称全部通过 |

基础实现与既有回归见 [dialect.go](../../common/query/dialect.go)、[dialect_test.go](../../common/query/dialect_test.go)、[queryparams_test.go](../../common/query/queryparams_test.go)和 [postgis_test.go](../../common/spatial/postgis_test.go)。具体故障处理见[空间数据与快显排查](../guide/troubleshooting/空间数据与快显.md#postgresql-混合大小写字段报-column-does-not-exist)。

## 参考资料

- [PostgreSQL 标识符与常量规则](https://www.postgresql.org/docs/current/sql-syntax-lexical.html#SQL-SYNTAX-IDENTIFIERS)
- [pq.QuoteIdentifier](https://pkg.go.dev/github.com/lib/pq#QuoteIdentifier)：PostgreSQL 专用引用函数，通用调用方仍按本文的方言边界使用。
