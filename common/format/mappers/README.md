# Format Type Mappers

此目录包含 `format` 系统的类型映射器实现。

## 目录说明

每个子目录实现了特定数据源的类型映射逻辑，将原生数据类型映射到 `format.FieldType` 通用类型系统。

- **mysql/** - MySQL 类型映射器
- **postgresql/** - PostgreSQL 类型映射器

## 类型映射器职责

类型映射器实现 `format.TypeMapper` 接口，提供：

1. **ToCommon()** - 将数据源原生类型转换为通用类型（如 `varchar` → `FieldTypeString`）
2. **FromCommon()** - 将通用类型转换回数据源原生类型（如 `FieldTypeString` → `TEXT`）

## 使用场景

这些类型映射器主要用于：

- **元数据管理** - Meta 模块扫描数据库 schema 时推断字段类型
- **Schema 推断** - Manager 模块分析数据源结构
- **数据转换** - Transfer 模块在不同数据源间转换数据时映射类型

## 与数据库插件系统的区别

⚠️ **重要**：`format` 类型映射器与 `common/database/plugin` 插件系统是**两套独立的系统**：

| 特性 | format 类型映射器 | database plugin 系统 |
|------|-------------------|----------------------|
| **位置** | `common/format/mappers/` | `common/database/plugins/` |
| **目的** | Schema 推断、类型转换 | 数据库连接管理、查询执行 |
| **类型系统** | `format.FieldType` (17种类型，包含空间类型) | `plugin.StandardType` (7种基础类型) |
| **注册方式** | `format.RegisterTypeMapper()` | `plugin.Register()` |
| **使用场景** | 元数据管理、文件格式处理 | 连接池、SQL 查询 |

## 自动注册

所有类型映射器通过 `common/format/builtin/init.go` 自动注册：

```go
import _ "github.com/addp/common/format/builtin"  // 自动注册所有内置映射器
```

## 添加新的类型映射器

1. 在此目录下创建新的子目录（如 `clickhouse/`）
2. 实现 `format.TypeMapper` 接口
3. 在 `init()` 函数中调用 `format.RegisterTypeMapper()`
4. 在 `common/format/builtin/init.go` 中添加 blank import

## 参考文档

- [common/format/README.md](../README.md) - Format 系统总体介绍
- [docs/数据库插件系统.md](../../../docs/数据库插件系统.md) - 数据库插件系统架构
