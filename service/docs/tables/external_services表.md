# external_services 表结构和 API 说明

## 一、表结构概览

`service.external_services` 表是 Service 模块的外部服务注册表，存储第三方数据服务的配置信息。支持多种服务类型（OGC WMS/WFS、REST API 等），提供统一的服务管理和健康检查功能。

### 核心功能

- **服务注册管理**：注册和管理外部数据服务（GIS服务、REST API 等）
- **元数据存储**：自动解析和存储服务能力文档（GetCapabilities）
- **认证配置**：支持多种认证方式（Basic、Bearer、API Key）
- **健康检查**：定期检查服务可用性，自动更新服务状态
- **服务目录**：提供统一的服务目录查询和搜索功能

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 服务唯一标识 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID |
| `name` | VARCHAR(255) | NOT NULL | 服务名称 |
| `description` | TEXT | | 服务描述 |
| `service_type` | VARCHAR(50) | NOT NULL, INDEXED | 服务类型 |
| `url` | TEXT | NOT NULL | 服务 URL |
| `metadata` | JSONB | | 服务元数据（GetCapabilities 解析结果） |
| `auth_type` | VARCHAR(50) | | 认证类型：'none'、'basic'、'bearer'、'api_key' |
| `auth_config` | JSONB | | 认证配置（AES 加密存储） |
| `status` | VARCHAR(20) | DEFAULT 'active' | 服务状态：'active'、'inactive'、'error' |
| `health_check_url` | TEXT | | 健康检查端点 |
| `last_checked_at` | TIMESTAMP | | 上次健康检查时间 |
| `created_by` | INTEGER | NOT NULL | 创建者 ID |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |
| `deleted_at` | TIMESTAMP | INDEXED | 软删除时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_external_service_tenant` | tenant_id | 按租户查询 |
| `idx_external_service_type` | service_type | 按服务类型过滤 |
| `idx_external_services_deleted` | deleted_at | 软删除查询 |

---

## 三、ServiceType 说明

| 值 | 含义 | 说明 |
|---|------|------|
| `wms` | Web Map Service | OGC WMS 地图图片服务 |
| `wfs` | Web Feature Service | OGC WFS 矢量要素服务 |
| `wmts` | Web Map Tile Service | OGC WMTS 瓦片地图服务 |
| `ogc_api` | OGC API - Features | 新一代 OGC RESTful API |
| `data_api` | 数据 API | 通用 RESTful 数据 API |
| `rest` | REST API | 其他 REST 风格 API |

---

## 四、JSON 字段详细结构

### 4.1 Metadata 字段（服务元数据）

**WMS 服务**：
```json
{
  "version": "1.3.0",
  "title": "某市地理信息服务",
  "abstract": "提供城市地理信息数据",
  "layers": [
    {
      "name": "cities",
      "title": "城市点图层",
      "crs": ["EPSG:4326", "EPSG:3857"],
      "bbox": [-180, -90, 180, 90]
    }
  ],
  "formats": ["image/png", "image/jpeg"],
  "capabilities_url": "https://example.com/wms?SERVICE=WMS&REQUEST=GetCapabilities"
}
```

**REST API 服务**：
```json
{
  "version": "1.0",
  "endpoints": [
    {
      "path": "/api/data/cities",
      "method": "GET",
      "description": "获取城市数据"
    }
  ],
  "base_url": "https://api.example.com"
}
```

### 4.2 AuthConfig 字段（认证配置）

**Basic 认证**：
```json
{
  "username": "admin",
  "password": "<AES加密后的密码>"
}
```

**Bearer Token**：
```json
{
  "token": "<AES加密后的token>"
}
```

**API Key**：
```json
{
  "key_name": "X-API-Key",
  "key_value": "<AES加密后的key>"
}
```

**注意**：auth_config 中的敏感信息（密码、token、key）使用 AES-256-GCM 加密存储。

---

## 五、API 端点说明

### 5.1 POST /api/service/registry/services - 创建外部服务

**请求体**：

```json
{
  "name": "某市 WMS 服务",
  "description": "提供城市地理信息数据",
  "service_type": "wms",
  "url": "https://gis.example.com/wms",
  "auth_type": "basic",
  "auth_config": {
    "username": "admin",
    "password": "password123"
  },
  "health_check_url": "https://gis.example.com/health"
}
```

**响应**（201 Created）：

```json
{
  "id": 1,
  "tenant_id": 1,
  "name": "某市 WMS 服务",
  "service_type": "wms",
  "url": "https://gis.example.com/wms",
  "auth_type": "basic",
  "status": "active",
  "metadata": {
    "version": "1.3.0",
    "layers": [...]
  },
  "created_at": "2025-01-01T10:00:00Z"
}
```

**注意**：创建服务时会自动调用 GetCapabilities 解析元数据。

---

### 5.2 GET /api/service/registry/services - 查询服务列表

**查询参数**：
- `service_type`：按服务类型过滤
- `status`：按状态过滤
- `keyword`：搜索名称或描述
- `page`、`page_size`：分页参数

**响应**：

```json
{
  "items": [
    {
      "id": 1,
      "name": "某市 WMS 服务",
      "service_type": "wms",
      "status": "active",
      "url": "https://gis.example.com/wms",
      "last_checked_at": "2025-01-01T12:00:00Z"
    }
  ],
  "total": 10,
  "page": 1,
  "page_size": 20
}
```

---

### 5.3 GET /api/service/registry/services/:id - 获取服务详情

**响应**：返回完整 ExternalService 对象（包含 metadata、layers）

---

### 5.4 PUT /api/service/registry/services/:id - 更新服务

**请求体**（部分更新）：

```json
{
  "name": "某市 WMS 服务（更新）",
  "status": "inactive",
  "auth_config": {
    "username": "admin",
    "password": "new_password"
  }
}
```

---

### 5.5 DELETE /api/service/registry/services/:id - 删除服务

**响应**（204 No Content）

**注意**：软删除，关联的 service_layers 记录会级联删除（ON DELETE CASCADE）。

---

### 5.6 POST /api/service/registry/services/:id/refresh - 刷新服务元数据

**功能**：重新调用 GetCapabilities，更新 metadata 和 layers。

**响应**：

```json
{
  "message": "服务元数据已刷新",
  "metadata": {...},
  "layers_updated": 5
}
```

---

### 5.7 POST /api/service/registry/services/:id/health - 健康检查

**功能**：手动触发健康检查，更新 status 和 last_checked_at。

**响应**：

```json
{
  "service_id": 1,
  "status": "active",
  "response_time_ms": 123,
  "last_checked_at": "2025-01-01T14:00:00Z"
}
```

---

### 5.8 GET /api/service/registry/search - 搜索服务

**查询参数**：
- `q`：搜索关键词（名称、描述、URL）
- `service_type`：服务类型过滤
- `limit`：结果数量限制

**响应**：返回匹配的服务列表

---

### 5.9 GET /api/service/catalog - 获取服务目录

**功能**：返回所有活跃服务的目录信息（公开访问）

**响应**：

```json
{
  "services": [
    {
      "id": 1,
      "name": "某市 WMS 服务",
      "type": "wms",
      "url": "https://gis.example.com/wms",
      "layers_count": 5
    }
  ],
  "total_services": 10
}
```

---

### 5.10 GET /api/service/proxy/:id/*path - 服务代理

**功能**：代理请求到注册的外部服务，自动添加认证信息。

**示例**：
```
GET /api/service/proxy/1/GetMap?SERVICE=WMS&...
  ↓ 代理到
GET https://gis.example.com/wms/GetMap?SERVICE=WMS&...
  （自动添加 Authorization 头）
```

---

## 六、服务健康检查

### 6.1 自动健康检查（定时任务）

**频率**：每小时执行一次

**逻辑**：
1. 查询所有 `status='active'` 的服务
2. 并发请求 `health_check_url`（或默认端点）
3. 根据响应更新 `status` 和 `last_checked_at`

**实现**：
```go
// 定时任务（Cron）
func HealthCheckScheduler() {
    // 每小时执行
    c := cron.New()
    c.AddFunc("0 * * * *", func() {
        services := GetActiveServices()
        for _, svc := range services {
            CheckServiceHealth(svc)
        }
    })
    c.Start()
}
```

### 6.2 健康检查结果

| HTTP 状态码 | 结果状态 | 说明 |
|------------|---------|------|
| 200-299 | active | 服务正常 |
| 400-499 | error | 客户端错误（配置问题） |
| 500-599 | error | 服务端错误 |
| 超时/无响应 | inactive | 服务不可用 |

---

## 七、权限控制

### 7.1 租户隔离

- 所有查询自动过滤 `tenant_id`
- 用户只能管理自己租户的服务
- SuperAdmin 可查看所有租户的服务

### 7.2 创建者追踪

- `created_by` 字段记录创建者用户 ID
- 用于审计和权限验证

---

## 八、数据安全

### 8.1 敏感信息加密

**AES-256-GCM 加密**：
- `auth_config` 中的密码、token、key
- 加密密钥从环境变量 `ENCRYPTION_KEY` 读取
- 解密时需要相同的密钥

**实现**：
```go
// 保存时加密
encryptedPassword, _ := crypto.AESEncrypt(password, encryptionKey)
authConfig := map[string]string{
    "password": encryptedPassword,
}

// 使用时解密
decryptedPassword, _ := crypto.AESDecrypt(authConfig["password"], encryptionKey)
```

---

## 九、使用示例

### 示例 1：注册 WMS 服务并查询图层

```bash
# 1. 注册 WMS 服务
SERVICE_ID=$(curl -X POST http://localhost:8085/api/service/registry/services \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "某市 WMS 服务",
    "service_type": "wms",
    "url": "https://gis.example.com/wms",
    "auth_type": "none"
  }' | jq -r '.id')

# 2. 查看服务详情（包含解析的图层列表）
curl -X GET http://localhost:8085/api/service/registry/services/$SERVICE_ID \
  -H "Authorization: Bearer $TOKEN" | jq '.layers'

# 3. 通过代理访问 WMS 服务
curl "http://localhost:8085/api/service/proxy/$SERVICE_ID/GetMap?SERVICE=WMS&REQUEST=GetMap&LAYERS=cities&WIDTH=800&HEIGHT=600" \
  -H "Authorization: Bearer $TOKEN" \
  --output map.png
```

### 示例 2：注册需要认证的 REST API

```bash
curl -X POST http://localhost:8085/api/service/registry/services \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "数据中心 API",
    "service_type": "rest",
    "url": "https://api.datacenter.com",
    "auth_type": "bearer",
    "auth_config": {
      "token": "your-bearer-token-here"
    },
    "health_check_url": "https://api.datacenter.com/health"
  }'
```

---

## 十、重要说明

### 10.1 GetCapabilities 解析

**支持的服务类型**：
- WMS 1.1.1、1.3.0
- WFS 1.0.0、1.1.0、2.0.0
- WMTS 1.0.0

**解析失败处理**：
- 如果 GetCapabilities 请求失败，服务仍会创建，但 metadata 为空
- 用户可以手动调用 `/refresh` API 重新解析

### 10.2 服务代理限制

**安全限制**：
- 只能代理已注册的服务
- 自动添加认证信息，前端无需处理
- 防止 SSRF 攻击（URL 白名单验证）

### 10.3 性能优化

**元数据缓存**：
- metadata 和 layers 存储在数据库，避免重复解析
- 定期刷新（手动或定时任务）

**健康检查优化**：
- 并发检查多个服务（goroutine pool）
- 超时时间 5 秒

---

## 十一、相关文档

- [service_layers表](./service_layers表.md) - 服务图层表
- [数据库架构](../数据库架构.md) - Service 模块架构
