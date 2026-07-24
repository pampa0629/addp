# conversations 表结构和 API 说明

## 一、表结构概览

`copilot.conversations` 表是 Copilot 模块的对话会话表,负责存储用户与 AI 助手的对话会话信息。支持两种上下文类型(SQL 生成和工作流生成),实现多租户隔离和会话管理。

### 核心功能

- **会话管理**:存储对话会话的基本信息和上下文类型
- **多租户隔离**:按 tenant_id 隔离不同租户的对话
- **上下文类型**:支持 SQL 和 Workflow 两种对话场景
- **会话状态**:支持 active 和 archived 两种状态
- **消息关联**:一对多关联 messages 表

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | INTEGER | PRIMARY KEY, AUTO_INCREMENT | 对话会话唯一标识 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID,用于多租户隔离 |
| `user_id` | INTEGER | NOT NULL, INDEXED | 用户 ID,关联 system.users.id |
| `context_type` | VARCHAR(32) | NOT NULL | 上下文类型:'sql' 或 'workflow' |
| `status` | VARCHAR(32) | DEFAULT 'active' | 会话状态:'active'(活跃) 或 'archived'(归档) |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW(), ON UPDATE NOW() | 更新时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `idx_conversations_tenant` | `tenant_id` | 普通索引 | 租户隔离查询优化 |
| `idx_conversations_user` | `user_id` | 普通索引 | 按用户查询优化 |

### 2.3 外键关系

| 字段 | 引用表 | 说明 |
|------|--------|------|
| `tenant_id` | `system.tenants.id` | 对话所属租户(逻辑外键,非数据库约束) |
| `user_id` | `system.users.id` | 对话所属用户(逻辑外键,非数据库约束) |

**关联表**:
- `copilot.messages.conversation_id` ← `conversations.id`(1:N,级联删除)

---

## 三、上下文类型说明

### 3.1 ContextType 枚举

| 值 | 含义 | 用途 |
|---|------|------|
| `sql` | SQL 生成对话 | 用户请求生成 SQL 查询语句 |
| `workflow` | 工作流生成对话 | 用户请求生成 GIS 工作流 DAG |

### 3.2 状态类型

| 值 | 含义 | 说明 |
|---|------|------|
| `active` | 活跃会话 | 正在进行的对话,可继续添加消息 |
| `archived` | 归档会话 | 已结束的对话,仅用于历史记录查看 |

---

## 四、Python 模型定义

### 4.1 SQLAlchemy 模型

```python
from sqlalchemy import Column, Integer, String, DateTime
from sqlalchemy.orm import relationship
from sqlalchemy.sql import func
from database import Base

class Conversation(Base):
    """对话会话表"""
    __tablename__ = 'conversations'
    __table_args__ = {'schema': 'copilot'}

    id = Column(Integer, primary_key=True, autoincrement=True)
    tenant_id = Column(Integer, nullable=False, index=True)
    user_id = Column(Integer, nullable=False, index=True)
    context_type = Column(String(32), nullable=False)  # 'sql' 或 'workflow'
    status = Column(String(32), default='active')  # 'active', 'archived'
    created_at = Column(DateTime(timezone=True), server_default=func.now())
    updated_at = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now())

    # 关系：一对多 Message
    messages = relationship("Message", back_populates="conversation", cascade="all, delete-orphan")
```

### 4.2 Pydantic 模型(API DTO)

```python
from pydantic import BaseModel
from datetime import datetime
from typing import List, Optional

class ConversationBase(BaseModel):
    """对话会话基础模型"""
    context_type: str  # 'sql' 或 'workflow'
    status: Optional[str] = 'active'

class ConversationCreate(ConversationBase):
    """创建对话请求"""
    tenant_id: int
    user_id: int

class ConversationResponse(ConversationBase):
    """对话响应模型"""
    id: int
    tenant_id: int
    user_id: int
    created_at: datetime
    updated_at: datetime

    class Config:
        from_attributes = True
```

---

## 五、API 端点说明

### 5.1 POST /api/v1/copilot/sql/generate - 生成 SQL(创建 SQL 对话)

请求必须携带 `Authorization: Bearer <token>`。Copilot 通过 System AuthContext 取得用户与租户身份，请求体禁止提交 `tenant_id` 或 `user_id`。

**请求体**:

```json
{
  "query": "查询所有人口大于 100 万的城市",
  "conversation_id": null,
  "engine_id": 1
}
```

**响应**(200 OK):

```json
{
  "sql": "SELECT * FROM cities WHERE population > 1000000",
  "explanation": "该查询查找所有人口超过 100 万的城市",
  "auto_selected": true,
  "candidates": [
    {
      "engine_id": 1,
      "engine_name": "PostgreSQL-生产库",
      "schema_name": "public",
      "table_name": "cities",
      "score": 0.95,
      "reason": "表名和字段高度匹配查询意图"
    }
  ],
  "conversation_id": 1
}
```

**说明**:
- 首次请求时 `conversation_id` 为 null,系统自动创建新对话并返回 `conversation_id`
- 后续请求可携带 `conversation_id` 继续同一对话
- 自动创建 `context_type='sql'` 的对话记录

---

### 5.2 POST /api/v1/copilot/workflow/generate - 生成工作流

请求必须携带 `Authorization: Bearer <token>`。Copilot 通过 System 校验 JWT 并取得用户与租户身份，请求体不得提交 `tenant_id` 或 `user_id`。

**请求体**:

```json
{
  "query": "加载数据,计算 100 米缓冲区,然后保存结果",
  "conversation_id": null,
  "workflow_engine_id": 1,
  "resources": [
    {"role": "input", "locator": "addp://engine/1/path/public/cities?type=table&item_id=103", "geometry_column": "geom", "crs": "EPSG:32650"}
  ]
}
```

**响应**(200 OK):

```json
{
  "status": "success",
  "workflow": {
    "name": "缓冲区分析工作流",
    "tasks": [
      {
        "id": "task1",
        "operator": "load",
        "params": {
          "locator": "addp://engine/1/path/public/cities?type=table&item_id=103"
        },
        "depends_on": []
      },
      {
        "id": "task2",
        "operator": "buffer",
        "params": {
          "input_gdf": {"$ref": "task1"},
          "distance": 100
        },
        "depends_on": ["task1"]
      },
      {
        "id": "task3",
        "operator": "save",
        "params": {
          "input_df": {"$ref": "task2"},
          "target_parent_locator": "addp://engine/1/path/public?type=schema&node_id=11",
          "target_name": "cities_buffer"
        },
        "depends_on": ["task2"]
      }
    ]
  },
  "explanation": "该工作流加载数据,计算 100 米缓冲区,最后保存结果",
  "conversation_id": 2
}
```

**说明**:
- 首次请求时自动创建 `context_type='workflow'` 的对话
- `use_two_stage=true` 启用两阶段生成模式(规划 + 生成)

---

### 5.3 GET /copilot/conversations - 查询对话列表(待实现)

**查询参数**:
- `user_id`(可选):按用户过滤
- `context_type`(可选):按上下文类型过滤
- `status`(可选):按状态过滤
- `page`(可选):页码,默认 1
- `page_size`(可选):每页条数,默认 20

**响应**(200 OK):

```json
{
  "conversations": [
    {
      "id": 1,
      "tenant_id": 1,
      "user_id": 2,
      "context_type": "sql",
      "status": "active",
      "message_count": 5,
      "created_at": "2026-01-01T10:00:00Z",
      "updated_at": "2026-01-01T10:30:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

---

### 5.4 GET /copilot/conversations/:id - 获取对话详情(待实现)

**响应**(200 OK):

```json
{
  "id": 1,
  "tenant_id": 1,
  "user_id": 2,
  "context_type": "sql",
  "status": "active",
  "messages": [
    {
      "id": 1,
      "role": "user",
      "content": "查询所有城市",
      "created_at": "2026-01-01T10:00:00Z"
    },
    {
      "id": 2,
      "role": "assistant",
      "content": "SELECT * FROM cities",
      "created_at": "2026-01-01T10:00:05Z"
    }
  ],
  "created_at": "2026-01-01T10:00:00Z",
  "updated_at": "2026-01-01T10:00:05Z"
}
```

---

### 5.5 PUT /copilot/conversations/:id - 更新对话状态(待实现)

**请求体**:

```json
{
  "status": "archived"
}
```

**响应**(200 OK):

```json
{
  "id": 1,
  "status": "archived",
  "updated_at": "2026-01-01T11:00:00Z"
}
```

---

### 5.6 DELETE /copilot/conversations/:id - 删除对话(待实现)

**响应**(200 OK):

```json
{
  "message": "对话删除成功"
}
```

**说明**:
- 级联删除所有关联的 messages 记录
- 仅对话所属用户或管理员可删除

---

## 六、权限控制

### 6.1 访问权限

| 操作 | TenantAdmin | User | 说明 |
|------|------------|------|------|
| 创建对话 | ✅ | ✅ | 自动关联到当前用户 |
| 查看对话列表 | ✅(本租户所有) | ✅(仅自己) | 按 tenant_id 和 user_id 过滤 |
| 查看对话详情 | ✅(本租户所有) | ✅(仅自己) | 验证所有权 |
| 更新对话状态 | ✅(本租户所有) | ✅(仅自己) | 仅能修改 status 字段 |
| 删除对话 | ✅(本租户所有) | ✅(仅自己) | 级联删除消息 |

### 6.2 租户隔离

**自动隔离**:
- 所有查询自动添加 `WHERE tenant_id = <当前租户>`
- User 查询额外添加 `WHERE user_id = <当前用户>`
- SuperAdmin 查询不受租户限制(可跨租户管理)

---

## 七、使用示例

### 7.1 创建 SQL 对话(首次请求)

```bash
curl -X POST http://localhost:8087/api/v1/copilot/sql/generate \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "查询所有人口大于 100 万的城市",
    "conversation_id": null,
    "engine_id": 1
  }'
```

**响应**:

```json
{
  "sql": "SELECT * FROM cities WHERE population > 1000000",
  "explanation": "该查询查找所有人口超过 100 万的城市",
  "auto_selected": true,
  "candidates": [...],
  "conversation_id": 1
}
```

---

### 7.2 继续 SQL 对话

```bash
curl -X POST http://localhost:8087/api/v1/copilot/sql/generate \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "按人口降序排列",
    "conversation_id": 1
  }'
```

**说明**:Agent 会基于上下文理解,继续修改之前的 SQL 查询

---

### 7.3 创建工作流对话

```bash
curl -X POST http://localhost:8087/api/v1/copilot/workflow/generate \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "加载数据,计算 100 米缓冲区,保存结果",
    "conversation_id": null,
    "workflow_engine_id": 1
  }'
```

---

## 八、重要说明

### 8.1 会话生命周期

**创建**:
- 首次调用 SQL/Workflow 生成 API 时自动创建
- 不需要手动调用创建接口

**更新**:
- 每次添加新消息时自动更新 `updated_at`
- 可手动将状态改为 `archived`

**删除**:
- 删除对话会级联删除所有消息
- 建议使用归档而非删除

### 8.2 上下文类型限制

- 一个对话只能有一个 `context_type`
- SQL 对话和 Workflow 对话不能混合
- 如需切换类型,应创建新对话

### 8.3 租户隔离策略

- 对话严格按租户隔离
- 用户只能访问自己创建的对话
- TenantAdmin 可查看本租户所有对话

---

## 九、相关文档

- [messages 表](./messages表.md) - 对话消息表,存储具体对话内容
- [llm_configs 表](./llm_configs表.md) - LLM 配置表,管理 AI 模型配置
- [数据库架构](../数据库架构.md) - Copilot 模块整体架构
- [Copilot 模块说明](../CLAUDE.md) - 模块整体架构和设计理念
