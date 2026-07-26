# llm_configs 表结构和 API 说明

## 一、表结构概览

`copilot.llm_configs` 表是 Copilot 模块的 LLM 配置表，负责存储和管理 AI 大语言模型的配置信息。支持多种 LLM 提供商（OpenAI、Claude、Ollama、DashScope 等），实现租户级配置和全局配置。

### 核心功能

- **多提供商支持**：支持 OpenAI、Claude、Ollama、DashScope 等主流 LLM 提供商
- **租户级配置**：每个租户可配置自己的 LLM，也可使用全局配置
- **API Key 加密**：使用 AES 加密存储 API Key
- **默认配置**：支持设置默认 LLM 配置
- **配置扩展**：使用 JSONB 存储额外配置（temperature、max_tokens 等）

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | INTEGER | PRIMARY KEY, AUTO_INCREMENT | 配置唯一标识 |
| `tenant_id` | INTEGER | NULLABLE, INDEXED | 租户 ID，NULL 表示全局配置 |
| `provider` | VARCHAR(50) | NOT NULL | LLM 提供商：'openai'、'claude'、'ollama'、'dashscope' |
| `model` | VARCHAR(100) | NOT NULL | 模型名称（如 'gpt-4'、'claude-3-5-sonnet' 等） |
| `api_key` | TEXT | | API 密钥（AES 加密存储） |
| `base_url` | VARCHAR(512) | | API 基础 URL（私有部署或代理） |
| `config` | JSONB | | 额外配置（temperature、max_tokens、top_p 等） |
| `is_default` | BOOLEAN | DEFAULT false | 是否为默认配置 |
| `is_active` | BOOLEAN | DEFAULT true | 是否激活 |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `idx_llm_configs_tenant` | `tenant_id` | 普通索引 | 租户隔离查询优化 |

### 2.3 外键关系

| 字段 | 引用表 | 说明 |
|------|--------|------|
| `tenant_id` | `system.tenants.id` | 配置所属租户（逻辑外键，非数据库约束） |

---

## 三、LLM 提供商说明

### 3.1 Provider 枚举

| 值 | 含义 | 典型模型 | 说明 |
|---|------|---------|------|
| `openai` | OpenAI | gpt-4、gpt-4-turbo、gpt-3.5-turbo | 官方 API 或兼容接口 |
| `claude` | Anthropic Claude | claude-3-5-sonnet、claude-3-opus | Claude 系列模型 |
| `ollama` | Ollama 本地部署 | llama2、mistral、qwen | 本地或私有部署 |
| `dashscope` | 阿里云通义千问 | qwen-turbo、qwen-plus | 国内 LLM 服务 |

### 3.2 配置优先级

1. **租户特定配置**（`tenant_id` 有值且 `is_default=true`）
2. **租户非默认配置**（`tenant_id` 有值且 `is_default=false`）
3. **全局默认配置**（`tenant_id=NULL` 且 `is_default=true`）
4. **系统回退配置**（代码内硬编码）

---

## 四、Python 模型定义

### 4.1 SQLAlchemy 模型

```python
from sqlalchemy import Column, Integer, String, Text, Boolean, DateTime
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.sql import func
from database import Base

class LLMConfig(Base):
    """LLM 配置表（支持租户级配置）"""
    __tablename__ = 'llm_configs'
    __table_args__ = {'schema': 'copilot'}

    id = Column(Integer, primary_key=True, autoincrement=True)
    tenant_id = Column(Integer, index=True)  # NULL 表示全局配置
    provider = Column(String(50), nullable=False)  # 'openai', 'claude', 'ollama', 'dashscope'
    model = Column(String(100), nullable=False)
    api_key = Column(Text)  # AES 加密存储
    base_url = Column(String(512))
    config = Column(JSONB)  # 额外配置（temperature, max_tokens 等）
    is_default = Column(Boolean, default=False)
    is_active = Column(Boolean, default=True)
    created_at = Column(DateTime(timezone=True), server_default=func.now())
```

### 4.2 Pydantic 模型

```python
from pydantic import BaseModel, Field
from typing import Optional, Dict, Any

class LLMConfigBase(BaseModel):
    """LLM 配置基础模型"""
    provider: str = Field(..., description="LLM 提供商")
    model: str = Field(..., description="模型名称")
    api_key: Optional[str] = Field(None, description="API 密钥")
    base_url: Optional[str] = Field(None, description="API 基础 URL")
    config: Optional[Dict[str, Any]] = Field(default_factory=dict, description="额外配置")
    is_default: bool = Field(False, description="是否为默认配置")
    is_active: bool = Field(True, description="是否激活")

class LLMConfigCreate(LLMConfigBase):
    """创建 LLM 配置请求"""
    tenant_id: Optional[int] = None

class LLMConfigResponse(LLMConfigBase):
    """LLM 配置响应模型"""
    id: int
    tenant_id: Optional[int]
    created_at: datetime

    class Config:
        from_attributes = True
```

---

## 五、Config 字段结构

### 5.1 OpenAI 配置示例

```json
{
  "temperature": 0.7,
  "max_tokens": 4000,
  "top_p": 0.9,
  "frequency_penalty": 0,
  "presence_penalty": 0,
  "timeout": 60
}
```

### 5.2 Claude 配置示例

```json
{
  "temperature": 0.7,
  "max_tokens": 4096,
  "top_p": 0.9,
  "timeout": 60
}
```

### 5.3 Ollama 配置示例

```json
{
  "temperature": 0.7,
  "num_predict": 2000,
  "top_k": 40,
  "top_p": 0.9,
  "repeat_penalty": 1.1
}
```

---

## 六、API 端点说明（待实现）

### 6.1 POST /copilot/llm-configs - 创建 LLM 配置

**权限**：待实现；不得在没有稳定 Permission 和路由 Guard 时开放。

**请求体**：

```json
{
  "tenant_id": 1,
  "provider": "openai",
  "model": "gpt-4-turbo",
  "api_key": "sk-xxxxx",
  "base_url": "https://api.openai.com/v1",
  "config": {
    "temperature": 0.7,
    "max_tokens": 4000
  },
  "is_default": true,
  "is_active": true
}
```

**响应**（201 Created）：

```json
{
  "id": 1,
  "tenant_id": 1,
  "provider": "openai",
  "model": "gpt-4-turbo",
  "api_key": "******",
  "base_url": "https://api.openai.com/v1",
  "config": {
    "temperature": 0.7,
    "max_tokens": 4000
  },
  "is_default": true,
  "is_active": true,
  "created_at": "2026-01-01T10:00:00Z"
}
```

**说明**：
- API Key 自动加密存储
- 响应时 API Key 脱敏显示为 `******`

---

### 6.2 GET /copilot/llm-configs - 查询 LLM 配置列表

**权限**：当前未纳入首批公开 IAM 管理 API；开放前必须定义稳定 Permission 和 Tenant 资源策略。

**查询参数**：
- `provider`（可选）：按提供商过滤
- `is_active`（可选）：按激活状态过滤
- `is_default`（可选）：按是否默认过滤

**响应**（200 OK）：

```json
{
  "configs": [
    {
      "id": 1,
      "tenant_id": 1,
      "provider": "openai",
      "model": "gpt-4-turbo",
      "api_key": "******",
      "is_default": true,
      "is_active": true,
      "created_at": "2026-01-01T10:00:00Z"
    },
    {
      "id": 2,
      "tenant_id": null,
      "provider": "claude",
      "model": "claude-3-5-sonnet",
      "api_key": "******",
      "is_default": true,
      "is_active": true,
      "created_at": "2026-01-01T09:00:00Z"
    }
  ]
}
```

---

### 6.3 GET /copilot/llm-configs/:id - 获取指定配置

**权限**：当前未开放公开管理 API。

**响应**（200 OK）：返回 LLMConfig 对象（API Key 脱敏）

---

### 6.4 PUT /copilot/llm-configs/:id - 更新配置

**权限**：当前未开放公开管理 API。

**请求体**：

```json
{
  "config": {
    "temperature": 0.8,
    "max_tokens": 5000
  },
  "is_default": false,
  "is_active": true
}
```

**响应**（200 OK）：返回更新后的 LLMConfig 对象

**说明**：
- 如果 API Key 传入 `******` 或 `****`，保留原始加密值
- 传入真实新值时重新加密

---

### 6.5 DELETE /copilot/llm-configs/:id - 删除配置

**权限**：当前未开放公开管理 API。

**响应**（200 OK）：

```json
{
  "message": "LLM 配置删除成功"
}
```

**限制**：
- 不能删除全局默认配置（`tenant_id=NULL` 且 `is_default=true`）

---

### 6.6 GET /copilot/llm-configs/default - 获取默认配置

**权限**：已认证用户

**响应**（200 OK）：

```json
{
  "id": 1,
  "provider": "openai",
  "model": "gpt-4-turbo",
  "config": {
    "temperature": 0.7,
    "max_tokens": 4000
  }
}
```

**逻辑**：
1. 查找当前租户的默认配置（`tenant_id=<当前租户>` 且 `is_default=true`）
2. 如果没有，查找全局默认配置（`tenant_id=NULL` 且 `is_default=true`）
3. 如果还没有，返回系统硬编码配置

---

## 七、权限控制

### 7.1 访问权限

当前没有公开的 LLM Config 管理 Operation，因此不发布对应 active Permission。未来开放时必须在 Copilot Permission Manifest、路由 Guard 和 Swagger 中同时声明，Tenant 配置只能在当前 Tenant Context 内管理。

### 7.2 租户隔离

**查询规则**：
- Tenant 查询只能读取本 Tenant 配置和明确发布的全局只读配置。
- Platform Context 不得通过普通 Copilot Repository 跨 Tenant 读取配置。

---

## 八、数据安全

### 8.1 API Key 加密

**加密算法**：AES-256-GCM（与 System 模块的引擎加密一致）

**加密流程**：

```python
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
import base64

def encrypt_api_key(api_key: str, encryption_key: bytes) -> str:
    """加密 API Key"""
    aesgcm = AESGCM(encryption_key)
    nonce = os.urandom(12)
    ciphertext = aesgcm.encrypt(nonce, api_key.encode(), None)
    encrypted = base64.b64encode(nonce + ciphertext).decode()
    return f"AES256GCM:{encrypted}"

def decrypt_api_key(encrypted_key: str, encryption_key: bytes) -> str:
    """解密 API Key"""
    if not encrypted_key.startswith("AES256GCM:"):
        return encrypted_key  # 未加密
    encrypted = base64.b64decode(encrypted_key.split(":", 1)[1])
    nonce = encrypted[:12]
    ciphertext = encrypted[12:]
    aesgcm = AESGCM(encryption_key)
    plaintext = aesgcm.decrypt(nonce, ciphertext, None)
    return plaintext.decode()
```

**密钥来源**：
- 环境变量 `ENCRYPTION_KEY`（Base64 编码，32 字节）
- 与 System 模块共享相同的加密密钥

### 8.2 响应脱敏

**外部 API**：
- API Key 显示为 `******`

**内部服务调用**：
- 自动解密 API Key

---

## 九、使用示例

### 9.1 创建租户级 OpenAI 配置

```bash
curl -X POST http://localhost:8087/copilot/llm-configs \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "tenant_id": 1,
    "provider": "openai",
    "model": "gpt-4-turbo",
    "api_key": "sk-xxxxxxxxxxxxx",
    "base_url": "https://api.openai.com/v1",
    "config": {
      "temperature": 0.7,
      "max_tokens": 4000
    },
    "is_default": true
  }'
```

---

### 9.2 全局配置

当前不提供通过公开 API 创建全局 LLM 配置的路径；部署级默认配置由受控配置流程维护。

### 9.3 查询默认配置

```bash
curl http://localhost:8087/copilot/llm-configs/default \
  -H "Authorization: Bearer $TOKEN"
```

---

## 十、重要说明

### 10.1 默认配置策略

**每个租户只能有一个默认配置**：
- 设置新配置为默认时，自动将旧的默认配置改为非默认
- 全局配置同理

### 10.2 模型命名规范

**OpenAI**：
- gpt-4、gpt-4-turbo、gpt-3.5-turbo

**Claude**：
- claude-3-5-sonnet、claude-3-opus、claude-3-sonnet

**Ollama**：
- llama2、mistral、qwen

### 10.3 Base URL 用途

**官方 API**：
- OpenAI：https://api.openai.com/v1
- Claude：https://api.anthropic.com

**私有部署**：
- Ollama：http://localhost:11434
- 代理服务：自定义 URL

**国内代理**：
- 第三方 API 中转服务

---

## 十一、相关文档

- [conversations 表](./conversations表.md) - 对话会话表，使用 LLM 配置生成回复
- [messages 表](./messages表.md) - 对话消息表，存储 LLM 生成的内容
- [数据库架构](../数据库架构.md) - Copilot 模块整体架构
- [Copilot 模块说明](../CLAUDE.md) - 模块整体架构和设计理念
