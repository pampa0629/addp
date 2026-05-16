# Common Module

ADDP 平台的共享代码模块，提供各个微服务模块通用的工具和类型定义。

## 包说明

### client
提供与其他服务交互的客户端：
- `SystemClient`: 与 System 模块交互的客户端，用于获取资源配置、用户认证等

### jsonmap
decoded JSON map 的通用读取工具，用于读取嵌套 section、字符串、数字、时间等基础值。

`jsonmap` 不承载 `meta_item.attributes` 业务规范；attributes 标准分区、normalizer 和落库构造属于 Meta 模块。

### resource
平台级资源定位、资源树、单资源读取和多组件资源读取抽象。

对象存储读取通过 `resource/objectstore.Reader` 适配到 `ResourceReader`，上层应优先依赖 `ResourceReader` / `ComponentReader`，不要绕过引擎和资源抽象直接引入具体存储客户端。

### sqldialect
跨 SQL 引擎的轻量方言工具，用于标识符引用、命名空间表名拼接、基础 SELECT / COUNT 和 LIMIT / OFFSET 分页 SQL 生成。

`sqldialect` 只承载通用 SQL 方言差异，不放入 PostGIS 等特定引擎扩展函数；PostGIS 空间表达式属于 `spatial`。

### spatial
空间数据通用能力，包括 CRS、MVT、WKB、坐标转换和 PostGIS 空间 SQL 表达式。

### format
文件格式、类型信息、格式信息、字段类型映射、parser / extractor / analyzer 等通用能力。

`format` 不直接决定 meta item 如何归并，也不绕过 Meta normalizer 写最终 attributes。

### models
共享的数据模型：
- `Resource`: 资源信息结构体
- `Engine`: 引擎配置模型，`ConnectionInfo` 是连接信息事实源

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
)

// 使用 SystemClient
sysClient := client.NewSystemClient("http://localhost:8180", token)
engine, err := sysClient.GetEngine(1)
```

## 设计原则

1. **单一职责**: 只包含真正通用的代码
2. **边界清晰**: 通用概念和工具可进入 common，Meta item 识别、claims / exclusive、`meta_item.full_name` 决策和 attributes 落库构造属于 Meta
3. **零依赖**: 尽量减少外部依赖，只使用 Go 标准库
4. **无需旧兼容**: 开发阶段不为旧包名、旧数据或旧逻辑保留兼容层
5. **文档完善**: 所有公开函数和类型都有文档注释
