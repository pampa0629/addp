# Common Module

ADDP 平台的共享代码模块，提供各个微服务模块通用的工具和类型定义。

## 包说明

### client
提供与其他服务交互的客户端：
- `SystemClient`: 与 System 模块交互的客户端，用于获取资源配置、用户认证等

### models
共享的数据模型：
- `Resource`: 资源信息结构体
- `BuildConnectionString()`: 根据资源信息构建数据库连接字符串

## 使用方法

在其他模块的 `go.mod` 中引用：

```go
require (
    github.com/yourusername/addp/common v0.0.0
)

replace github.com/yourusername/addp/common => ../common
```

在代码中导入：

```go
import (
    "github.com/yourusername/addp/common/client"
    "github.com/yourusername/addp/common/models"
)

// 使用 SystemClient
sysClient := client.NewSystemClient("http://localhost:8080", token)
resource, err := sysClient.GetResource(1)

// 构建连接字符串
connStr, err := models.BuildConnectionString(resource)
```

## 设计原则

1. **单一职责**: 只包含真正通用的代码
2. **零依赖**: 尽量减少外部依赖，只使用 Go 标准库
3. **向后兼容**: 修改时保持 API 兼容性
4. **文档完善**: 所有公开函数和类型都有文档注释
