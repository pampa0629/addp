# 类型映射器重构迁移指南

## 概述

我们已经将类型映射从 `common/format/schema.go` 中的硬编码实现重构为可扩展的注册表机制。

## 架构变更

### 之前（硬编码）
```
common/format/schema.go
├── TypeMapping.PostgreSQLToCommon()  (142 行代码)
├── TypeMapping.MySQLToCommon()       (51 行代码)
├── TypeMapping.ShapefileDBFToCommon() (17 行代码)
└── ...
```

### 之后（可扩展注册表）
```
common/
├── format/
│   ├── schema.go             # Schema 定义 + TypeMapping 兼容层
│   ├── type_mapper.go        # TypeMapper 接口 + 注册表
│   └── builtin/init.go       # 自动导入所有内置映射器
├── database/
│   ├── postgresql/
│   │   └── type_mapper.go    # PostgreSQL 类型映射实现
│   └── mysql/
│       └── type_mapper.go    # MySQL 类型映射实现
└── geo/
    └── shapefile/
        └── type_mapper.go    # Shapefile 类型映射实现
```

## 使用方式

### 方式一：使用旧的 TypeMapping（兼容层，推荐）

**不需要修改现有代码**，旧的 API 继续可用：

```go
import "github.com/addp/common/format"

mapper := &format.TypeMapping{}
commonType := mapper.PostgreSQLToCommon("varchar") // 仍然有效
```

**注意**：必须确保导入了内置类型映射器（通过 Shapefile 等包的导入会自动触发）。

### 方式二：使用新的注册表 API（推荐用于新代码）

```go
import (
    "github.com/addp/common/format"
    _ "github.com/addp/common/format/builtin" // 导入所有内置映射器
)

// 获取特定数据库的类型映射器
mapper := format.GetTypeMapper("postgresql")
if mapper != nil {
    commonType := mapper.ToCommon("varchar")
}

// 列出所有已注册的类型映射器
mappers := format.ListTypeMappers() // ["postgresql", "mysql", "shapefile"]
```

## 扩展新的数据库类型

### 步骤 1：实现 TypeMapper 接口

创建 `common/database/oracle/type_mapper.go`：

```go
package oracle

import (
    "strings"
    "github.com/addp/common/format"
)

type TypeMapper struct{}

func (m *TypeMapper) Name() string {
    return "oracle"
}

func (m *TypeMapper) ToCommon(oracleType string) format.FieldType {
    oracleType = strings.ToLower(strings.TrimSpace(oracleType))

    switch oracleType {
    case "varchar2", "nvarchar2", "clob":
        return format.FieldTypeString
    case "number":
        return format.FieldTypeDecimal
    case "date", "timestamp":
        return format.FieldTypeTimestamp
    // ... 更多映射
    default:
        return format.FieldTypeUnknown
    }
}

func (m *TypeMapper) FromCommon(commonType format.FieldType) (string, int, int) {
    switch commonType {
    case format.FieldTypeString:
        return "VARCHAR2", 4000, 0
    case format.FieldTypeInt:
        return "NUMBER", 10, 0
    // ... 更多映射
    default:
        return "VARCHAR2", 4000, 0
    }
}

// init 自动注册
func init() {
    format.RegisterTypeMapper(&TypeMapper{})
}
```

### 步骤 2：在需要的地方导入

```go
import (
    _ "github.com/addp/common/database/oracle" // 触发 init() 注册
)

// 现在可以使用 Oracle 类型映射器了
mapper := format.GetTypeMapper("oracle")
```

### 步骤 3：（可选）添加到 builtin

如果希望 Oracle 成为内置支持，编辑 `common/format/builtin/init.go`：

```go
import (
    _ "github.com/addp/common/database/postgresql"
    _ "github.com/addp/common/database/mysql"
    _ "github.com/addp/common/database/oracle"  // 新增
    _ "github.com/addp/common/geo/shapefile"
)
```

## 测试

### 单元测试（现有包内）

```go
package oracle

import (
    "testing"
    "github.com/addp/common/format"
)

func TestOracleTypeMapper(t *testing.T) {
    mapper := &TypeMapper{}

    if mapper.ToCommon("varchar2") != format.FieldTypeString {
        t.Error("Oracle VARCHAR2 should map to FieldTypeString")
    }
}
```

### 集成测试（跨包）

在 `common/format/integration_test/` 中添加测试，会自动导入所有内置映射器。

## 迁移检查清单

- [x] 创建 `format/type_mapper.go` 注册表
- [x] 创建 `database/postgresql/type_mapper.go`
- [x] 创建 `database/mysql/type_mapper.go`
- [x] 创建 `geo/shapefile/type_mapper.go`
- [x] 更新 `format/schema.go` 为兼容层
- [x] 创建 `format/builtin/init.go` 统一导入
- [x] 创建集成测试
- [x] 验证 Meta 模块编译通过
- [ ] 验证 Manager 模块编译通过
- [ ] 验证 Transfer 模块编译通过
- [ ] 更新文档

## 优势

1. **解耦**：类型映射逻辑与具体数据库隔离
2. **可扩展**：用户可以添加新数据库支持，无需修改核心代码
3. **可测试**：每个映射器独立测试
4. **向后兼容**：旧的 API 继续可用

## 注意事项

1. **循环依赖**：测试文件不要在 `format` 包内导入 `format/builtin`，使用 `format/integration_test` 包
2. **自动注册**：所有映射器通过 `init()` 自动注册，无需手动调用
3. **线程安全**：注册表使用 `sync.RWMutex` 保护，支持并发访问

## 相关文件

- `common/format/type_mapper.go` - 核心注册表实现
- `common/format/builtin/init.go` - 内置映射器统一导入
- `common/format/integration_test/type_mapping_test.go` - 集成测试
- `common/database/*/type_mapper.go` - 各数据库实现
- `common/geo/shapefile/type_mapper.go` - Shapefile 实现
