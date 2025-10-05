# Common 模块 - 代码重构说明

## 概述

为了消除代码重复，我们创建了 `common` 模块，将 Manager、Meta、Transfer 模块中重复的代码提取到一个共享库中。

## 问题背景

在重构前，以下文件在多个模块中重复存在：
- `manager/backend/pkg/utils/system_client.go`
- `meta/backend/pkg/utils/system_client.go`
- `transfer/backend/pkg/utils/system_client.go`

这些文件内容几乎完全相同，维护起来容易产生不一致。

## 重构方案

### 目录结构

```
common/
├── go.mod                    # 独立的 Go 模块
├── README.md                 # 使用文档
├── client/
│   └── system.go            # SystemClient 实现
└── models/
    └── resource.go          # Resource 模型和工具函数
```

### 模块内容

#### 1. `client/system.go`
提供与 System 模块交互的客户端：
- `SystemClient` 结构体
- `NewSystemClient()` 构造函数
- `GetResource()` 获取单个资源
- `ListResources()` 获取资源列表

#### 2. `models/resource.go`
共享的数据模型和工具：
- `Resource` 结构体（包含完整字段：ID, TenantID, ResourceName, ResourceType, ConnectionInfo, Status, Description, IsActive）
- `BuildConnectionString()` 根据资源信息构建数据库连接字符串

## 使用方法

### 在各模块的 go.mod 中引用

```go
module github.com/addp/manager

require (
    github.com/addp/common v0.0.0
    // ... 其他依赖
)

replace github.com/addp/common => ../../common
```

### 在代码中导入

```go
import (
    "github.com/addp/common/client"
    "github.com/addp/common/models"
)

// 使用 SystemClient
sysClient := client.NewSystemClient("http://localhost:8080", token)
resource, err := sysClient.GetResource(1)

// 构建连接字符串
connStr, err := models.BuildConnectionString(resource)
```

### 别名导入（避免命名冲突）

如果模块内部也有 `models` 包，可以使用别名：

```go
import (
    commonModels "github.com/addp/common/models"
    "github.com/addp/meta/internal/models"
)

// 使用时
connStr, err := commonModels.BuildConnectionString(resource)
```

## 重构影响的文件

### Manager 模块
- `backend/go.mod` - 添加 common 依赖
- 删除 `backend/pkg/utils/system_client.go`

### Meta 模块
- `backend/go.mod` - 添加 common 依赖
- `internal/service/sync_service.go` - 更新 import
- `internal/service/scan_service.go` - 更新 import
- `internal/api/sync_handler.go` - 更新 import
- `internal/api/router.go` - 更新 import
- 删除 `backend/pkg/utils/system_client.go`

### Transfer 模块
- `backend/go.mod` - 添加 common 依赖（新建）
- 删除 `backend/pkg/utils/system_client.go`

## 优势

1. ✅ **单一数据源**：只需维护一份代码，修改一处即可影响所有模块
2. ✅ **统一版本**：所有模块使用相同的实现，避免不一致
3. ✅ **易于扩展**：新增功能只需修改 common 模块
4. ✅ **类型安全**：Resource 结构体定义统一，编译时检查类型
5. ✅ **降低维护成本**：减少代码重复，降低 bug 风险

## 注意事项

1. **依赖管理**：使用 `replace` 指令指向本地路径，开发时无需发布到远程仓库
2. **版本控制**：common 模块的修改会影响所有依赖模块，需要谨慎测试
3. **向后兼容**：修改 common 模块时要考虑向后兼容性
4. **最小依赖**：common 模块应保持最少的外部依赖，只使用 Go 标准库

## 未来扩展

可以考虑将以下内容也提取到 common 模块：
- 统一的错误处理和错误类型
- 统一的日志格式和工具
- 通用的中间件（如租户隔离中间件）
- 共享的配置结构和验证逻辑
- 通用的工具函数（时间处理、字符串处理等）

## 测试

所有模块编译测试通过：
```bash
# Manager 模块
cd manager/backend && go build ./...

# Meta 模块
cd meta/backend && go build ./...

# Transfer 模块
cd transfer/backend && go build ./...
```

## 参考

- 项目根目录 CLAUDE.md
- common/README.md
