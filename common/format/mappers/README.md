# common/format/mappers

本目录存放 format 类型映射器。类型映射器只负责在数据源原生字段类型和 `datatype.FieldType` 之间转换，不负责连接数据库、执行 SQL、读取文件或推断 item。

## 已有映射器

| 目录 | 映射器名称 | 说明 |
| --- | --- | --- |
| `mysql/` | `mysql` | MySQL 原生类型映射 |
| `oracle/` | `oracle` | Oracle 原生类型映射；`MDSYS.SDO_GEOMETRY` 映射为 geometry，其他未实现的 UDT、XML 和 INTERVAL 映射为 `unknown` |
| `postgresql/` | `postgresql` | PostgreSQL 原生类型映射 |
| `spatialite/` | `spatialite` | SQLite / SpatiaLite 原生类型映射 |

## 接口

```go
type TypeMapper interface {
    Name() string
    ToCommon(nativeType string) FieldType
    FromCommon(commonType FieldType) (nativeType string, size int, precision int)
}
```

职责说明：

- `Name()`：返回注册名称，例如 `postgresql`、`mysql`、`spatialite`。
- `ToCommon()`：把原生类型转换为 ADDP 通用字段类型。
- `FromCommon()`：把 ADDP 通用字段类型转换为目标原生类型，并返回长度/精度提示。

## 使用方式

内置映射器通过 `common/format/builtin` 统一注册：

```go
import _ "github.com/addp/common/format/builtin"

mapper := format.GetTypeMapper("postgresql")
fieldType := mapper.ToCommon("varchar(255)")
```

也可以在确实需要精确控制依赖时只 blank import 单个 mapper 包。

## 与 engine plugin 的边界

`TypeMapper` 和 `common/engine/plugin` 是不同层次：

| 层次 | 职责 |
| --- | --- |
| `common/format/mappers` | 字段类型转换 |
| `common/engine/plugin` | engine capability、连接、目录、内容读写和原生查询 |

新增映射器不应引入 engine 连接参数，也不应直接依赖具体 engine provider。需要连接、鉴权、列举、读取或写入时，应由上层通过 engine capability 和 contentio 适配能力完成。

## 新增映射器

1. 在本目录下创建子目录，例如 `clickhouse/`。
2. 实现 `format.TypeMapper`。
3. 在包内 `init()` 中调用 `format.RegisterTypeMapper()`。
4. 如需默认注册，在 `common/format/builtin/init.go` 添加 blank import。
5. 增加类型映射测试，覆盖常见原生类型、空间类型和未知类型。
