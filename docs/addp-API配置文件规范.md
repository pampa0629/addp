# API 配置文件规范 (api-manifest.json)

## 概述

每个模块需要在 `docs/api-manifest.json` 文件中维护自己的 API 文档配置，System 前端会自动读取并聚合展示。

## 文件位置

```
system/docs/api-manifest.json
manager/docs/api-manifest.json
meta/docs/api-manifest.json
transfer/docs/api-manifest.json
develop/docs/api-manifest.json
service/docs/api-manifest.json
orchestrator/docs/api-manifest.json
```

## JSON Schema

### 根对象

```json
{
  "module": "string",      // 模块名称（system、manager、meta 等）
  "title": "string",       // 模块中文标题
  "version": "string",     // 模块版本号
  "categories": [          // API 分类数组
    {
      "name": "string",           // 分类名称
      "description": "string",    // 分类描述（可选）
      "apis": [                   // API 接口数组
        {
          "name": "string",              // API 名称
          "method": "string",            // HTTP 方法：GET、POST、PUT、DELETE
          "path": "string",              // API 路径
          "description": "string",       // 功能描述
          "auth": boolean,               // 是否需要认证
          "params": "string",            // 路径参数说明（可选）
          "query": "string",             // 查询参数说明（可选）
          "request": "string",           // 请求体示例（可选）
          "response": "string"           // 响应示例
        }
      ]
    }
  ]
}
```

## 字段说明

### 模块级字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| module | string | 是 | 模块标识（小写，如 system、manager） |
| title | string | 是 | 模块中文名称（如 "系统模块"、"数据管理模块"） |
| version | string | 是 | 模块版本号（如 "0.0.20"） |
| categories | array | 是 | API 分类数组 |

### 分类级字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| name | string | 是 | 分类名称（如 "认证接口"、"用户管理"） |
| description | string | 否 | 分类详细说明 |
| apis | array | 是 | 该分类下的 API 接口数组 |

### API 级字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| name | string | 是 | API 接口名称（如 "用户登录"） |
| method | string | 是 | HTTP 方法（GET/POST/PUT/DELETE） |
| path | string | 是 | API 路径（如 `/api/auth/login`） |
| description | string | 是 | 功能说明 |
| auth | boolean | 是 | 是否需要 JWT 认证 |
| params | string | 否 | 路径参数说明（如 `:id - 用户 ID`） |
| query | string | 否 | 查询参数说明（支持多行） |
| request | string | 否 | 请求体 JSON 示例 |
| response | string | 是 | 响应体 JSON 示例 |

## 示例 1: System 模块

```json
{
  "module": "system",
  "title": "系统模块",
  "version": "0.0.20",
  "categories": [
    {
      "name": "认证接口",
      "description": "用户注册和登录相关接口",
      "apis": [
        {
          "name": "用户登录",
          "method": "POST",
          "path": "/api/auth/login",
          "description": "用户登录获取访问令牌",
          "auth": false,
          "request": "{\n  \"username\": \"admin\",\n  \"password\": \"admin123\"\n}",
          "response": "{\n  \"access_token\": \"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...\",\n  \"token_type\": \"Bearer\"\n}"
        }
      ]
    },
    {
      "name": "用户管理",
      "apis": [
        {
          "name": "获取用户列表",
          "method": "GET",
          "path": "/api/users",
          "description": "获取用户列表，支持分页",
          "auth": true,
          "query": "page: 页码，默认 1\npage_size: 每页数量，默认 10",
          "response": "[\n  {\n    \"id\": 1,\n    \"username\": \"admin\",\n    \"email\": \"admin@test.com\"\n  }\n]"
        }
      ]
    }
  ]
}
```

## 示例 2: Manager 模块

```json
{
  "module": "manager",
  "title": "数据管理模块",
  "version": "0.0.20",
  "categories": [
    {
      "name": "数据探查",
      "description": "数据预览和资源树相关接口",
      "apis": [
        {
          "name": "获取引擎列表",
          "method": "GET",
          "path": "/api/explorer/engines",
          "description": "获取数据探查可用的引擎列表",
          "auth": true,
          "response": "[\n  {\n    \"id\": 1,\n    \"name\": \"业务数据库\",\n    \"type\": \"postgresql\"\n  }\n]"
        },
        {
          "name": "数据预览",
          "method": "GET",
          "path": "/api/explorer/preview",
          "description": "预览各类数据资源（表数据、图片、视频、GeoJSON 等）",
          "auth": true,
          "query": "locator: 资源定位符 URI (必填)\n例如: db://72/public/users\n      minio://67/images/photo.jpg",
          "response": "{\n  \"type\": \"table\",\n  \"columns\": [\"id\", \"name\"],\n  \"rows\": [[1, \"张三\"]]\n}"
        }
      ]
    }
  ]
}
```

## 编写规范

### 1. 路径规范
- 所有路径必须以 `/api/` 开头
- 使用 RESTful 风格路径
- 路径参数使用 `:param` 表示（如 `/api/users/:id`）

### 2. 示例规范
- JSON 示例必须使用 `\n` 表示换行，确保展示时格式化正确
- 示例数据要贴近真实场景，避免使用 `xxx`、`...` 等占位符
- 响应示例要完整，包含主要字段

### 3. 描述规范
- description 要简洁明了，一句话说清楚功能
- query 参数说明每行一个参数，格式：`参数名: 说明`
- params 参数说明格式：`:参数名 - 说明`

### 4. 维护规范
- API 变更时，务必同步更新配置文件
- 新增 API 时，选择合适的 category 或创建新 category
- 废弃 API 直接删除，不要保留注释

## 加载机制

System 前端会在构建时读取所有模块的 `api-manifest.json` 文件，聚合后展示：

```javascript
// Developer.vue 中的加载逻辑（示例）
import systemManifest from '../../../system/docs/api-manifest.json'
import managerManifest from '../../../manager/docs/api-manifest.json'
import metaManifest from '../../../meta/docs/api-manifest.json'
// ... 其他模块

const allManifests = [
  systemManifest,
  managerManifest,
  metaManifest,
  // ...
]
```

## 验证工具

可以使用 JSON Schema 验证工具验证配置文件：

```bash
# 使用 ajv-cli 验证
npm install -g ajv-cli
ajv validate -s api-manifest.schema.json -d system/docs/api-manifest.json
```

## 常见问题

### Q: 如何组织多个相关的 API？
A: 使用 category 分组。一个 category 下可以包含多个相关的 API。

### Q: Gateway 的路由信息如何配置？
A: Gateway 路由不属于标准 API，可以使用特殊的字段：

```json
{
  "module": "gateway",
  "title": "API 网关",
  "version": "0.0.20",
  "categories": [
    {
      "name": "路由规则",
      "routes": [
        {
          "name": "System 系统管理",
          "path": "/api/system/*",
          "target": "System Backend",
          "port": "8180",
          "description": "用户认证、日志审计、引擎配置"
        }
      ]
    }
  ]
}
```

### Q: 是否支持版本控制？
A: 配置文件跟随模块代码一起进入 Git 版本控制，可追溯历史变更。
