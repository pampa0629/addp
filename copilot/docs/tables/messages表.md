# messages 表结构和 API 说明

## 一、表结构概览

`copilot.messages` 表是 Copilot 模块的对话消息表，负责存储对话会话中的每条消息。支持用户消息、助手消息和系统消息三种角色，使用 JSONB 存储额外的元数据信息。

### 核心功能

- **消息存储**：存储对话中的所有消息内容
- **角色区分**：支持 user、assistant、system 三种消息角色
- **元数据扩展**：使用 JSONB 存储额外信息（如工作流定义、数据源候选等）
- **Token 统计**：记录每条消息的 Token 数量
- **会话关联**：多对一关联 conversations 表

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | INTEGER | PRIMARY KEY, AUTO_INCREMENT | 消息唯一标识 |
| `conversation_id` | INTEGER | FK → conversations.id, NOT NULL, INDEXED | 所属对话会话 ID |
| `role` | VARCHAR(20) | NOT NULL | 消息角色：'user'、'assistant'、'system' |
| `content` | TEXT | NOT NULL | 消息内容 |
| `metadata` | JSONB | | 元数据（工作流定义、数据源信息等） |
| `token_count` | INTEGER | | Token 数量（用于成本统计） |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `idx_messages_conversation` | `conversation_id` | 普通索引 | 按对话查询消息优化 |

### 2.3 外键关系

| 字段 | 引用表 | 约束 | 说明 |
|------|--------|------|------|
| `conversation_id` | `copilot.conversations.id` | ON DELETE CASCADE | 级联删除，删除对话时自动删除所有消息 |

---

## 三、消息角色说明

### 3.1 Role 枚举

| 值 | 含义 | 用途 |
|---|------|------|
| `user` | 用户消息 | 用户输入的自然语言请求 |
| `assistant` | 助手消息 | AI 生成的 SQL 或工作流 |
| `system` | 系统消息 | 系统提示信息（如上下文、指令） |

### 3.2 消息流转

```
用户输入 → Message(role='user', content='查询所有城市')
    ↓
AI 处理
    ↓
AI 回复 → Message(role='assistant', content='SELECT * FROM cities')
```

---

## 四、Python 模型定义

### 4.1 SQLAlchemy 模型

```python
from sqlalchemy import Column, Integer, String, Text, DateTime, ForeignKey
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import relationship, mapped_column
from sqlalchemy.sql import func
from database import Base

class Message(Base):
    """对话消息表"""
    __tablename__ = 'messages'
    __table_args__ = {'schema': 'copilot'}

    id = Column(Integer, primary_key=True, autoincrement=True)
    conversation_id = Column(Integer, ForeignKey('copilot.conversations.id', ondelete='CASCADE'),
                             nullable=False, index=True)
    role = Column(String(20), nullable=False)  # 'user', 'assistant', 'system'
    content = Column(Text, nullable=False)
    extra_data = mapped_column('metadata', JSONB)  # 映射到数据库的 metadata 列
    token_count = Column(Integer)
    created_at = Column(DateTime(timezone=True), server_default=func.now())

    # 关系：多对一 Conversation
    conversation = relationship("Conversation", back_populates="messages")
```

### 4.2 Pydantic 模型

```python
from pydantic import BaseModel
from datetime import datetime
from typing import Optional, Dict, Any

class MessageBase(BaseModel):
    """消息基础模型"""
    role: str  # 'user', 'assistant', 'system'
    content: str
    metadata: Optional[Dict[str, Any]] = None
    token_count: Optional[int] = None

class MessageCreate(MessageBase):
    """创建消息请求"""
    conversation_id: int

class MessageResponse(MessageBase):
    """消息响应模型"""
    id: int
    conversation_id: int
    created_at: datetime

    class Config:
        from_attributes = True
```

---

## 五、Metadata 字段结构

### 5.1 SQL 对话元数据

```json
{
  "selected_datasource": {
    "engine_id": 1,
    "engine_name": "PostgreSQL-生产库",
    "schema_name": "public",
    "table_name": "cities"
  },
  "candidates": [
    {
      "engine_id": 1,
      "schema_name": "public",
      "table_name": "cities",
      "score": 0.95,
      "reason": "表名和字段高度匹配"
    }
  ]
}
```

### 5.2 Workflow 对话元数据

```json
{
  "workflow": {
    "name": "缓冲区分析工作流",
    "steps": [
      {
        "id": "step1",
        "type": "data_loader",
        "params": {
          "source_type": "postgresql",
          "table": "cities"
        }
      },
      {
        "id": "step2",
        "type": "buffer",
        "params": {
          "distance": 100,
          "unit": "meters"
        }
      }
    ]
  }
}
```

---

## 六、使用示例

### 6.1 保存对话消息（内部服务调用）

```python
from services.memory_service import memory_service

# 保存用户消息和助手回复
conversation_id = await memory_service.save_message(
    conversation_id=1,  # 已存在的对话 ID，或 None 创建新对话
    tenant_id=1,
    user_id=2,
    user_message="查询所有人口大于 100 万的城市",
    assistant_message="SELECT * FROM cities WHERE population > 1000000",
    metadata={
        "selected_datasource": {
            "engine_id": 1,
            "table_name": "cities"
        }
    },
    context_type='sql'
)
```

---

### 6.2 查询对话消息历史

```python
from services.memory_service import memory_service

# 获取对话的完整消息历史
memory = await memory_service.get_memory(conversation_id=1)

# memory 包含：
# - messages: List[Dict] - 消息列表
# - context_type: str - 对话类型
```

**返回格式**：

```python
{
    "messages": [
        {
            "role": "user",
            "content": "查询所有城市",
            "metadata": None
        },
        {
            "role": "assistant",
            "content": "SELECT * FROM cities",
            "metadata": {
                "selected_datasource": {...}
            }
        }
    ],
    "context_type": "sql"
}
```

---

### 6.3 统计对话消息数量（SQL）

```sql
-- 统计每个对话的消息数量
SELECT
    conversation_id,
    COUNT(*) AS message_count,
    SUM(token_count) AS total_tokens
FROM copilot.messages
GROUP BY conversation_id
ORDER BY message_count DESC;
```

---

### 6.4 查询最近的用户问题（SQL）

```sql
-- 查询租户下最近 10 个用户问题
SELECT
    m.id,
    m.conversation_id,
    m.content,
    m.created_at,
    c.context_type
FROM copilot.messages m
JOIN copilot.conversations c ON m.conversation_id = c.id
WHERE m.role = 'user'
  AND c.tenant_id = 1
ORDER BY m.created_at DESC
LIMIT 10;
```

---

## 七、权限控制

### 7.1 访问权限

| 操作 | TenantAdmin | User | 说明 |
|------|------------|------|------|
| 创建消息 | ✅ | ✅ | 通过对话 API 自动创建 |
| 查看消息 | ✅(本租户所有对话) | ✅(仅自己的对话) | 通过对话关联验证 |
| 删除消息 | ✅(本租户所有对话) | ✅(仅自己的对话) | 删除对话级联删除消息 |

### 7.2 租户隔离

**通过对话表间接隔离**：
- 消息表本身没有 `tenant_id` 字段
- 通过 `conversation_id` 关联到 conversations 表
- conversations 表有 `tenant_id`，实现间接租户隔离

**查询示例**：

```sql
-- 查询租户下的所有消息
SELECT m.*
FROM copilot.messages m
JOIN copilot.conversations c ON m.conversation_id = c.id
WHERE c.tenant_id = 1;
```

---

## 八、重要说明

### 8.1 级联删除

- 删除 conversation 会自动删除所有关联的 messages
- 设置了 `ON DELETE CASCADE` 外键约束
- 删除操作不可恢复

### 8.2 Token 统计

**用途**：
- 统计 LLM API 调用成本
- 监控对话长度
- 优化 Prompt 设计

**计算方式**：
- 使用 tiktoken 库计算（OpenAI 模型）
- 或使用模型提供的 usage 信息

### 8.3 消息顺序

- 消息按 `created_at` 升序排列
- 用于构建对话上下文
- Agent 会读取最近 N 条消息作为上下文

### 8.4 Metadata 扩展性

**当前用途**：
- SQL 对话：存储数据源候选和选中结果
- Workflow 对话：存储生成的工作流定义

**未来扩展**：
- 存储执行结果
- 存储用户反馈
- 存储调试信息

---

## 九、相关文档

- [conversations 表](./conversations表.md) - 对话会话表，消息所属对话
- [llm_configs 表](./llm_configs表.md) - LLM 配置表，管理 AI 模型
- [数据库架构](../数据库架构.md) - Copilot 模块整体架构
- [Copilot 模块说明](../CLAUDE.md) - 模块整体架构和设计理念
