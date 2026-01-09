# search_history 表结构和 API 说明

## 一、表结构概览

`manager.search_history` 表是 Manager 模块的搜索历史表，记录用户的数据检索历史（全文检索 + 向量检索）。支持按用户隔离，提供快速访问历史搜索的功能。

### 核心功能

- **搜索历史记录**：记录用户的所有数据检索查询（包括全文检索和向量检索）
- **按用户隔离**：每个用户只能查看自己的搜索历史
- **去重机制**：同一用户的相同查询只保留一条记录（更新时间）
- **快速访问**：提供历史搜索列表，支持快速重复搜索

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 历史记录唯一标识 |
| `user_id` | INTEGER | NOT NULL, INDEXED, UK | 用户 ID |
| `tenant_id` | INTEGER | INDEXED | 租户 ID |
| `query` | VARCHAR(512) | NOT NULL, UK | 搜索查询字符串 |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 首次搜索时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 最后搜索时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_search_history_user_query` | user_id, query | 唯一索引，确保用户+查询唯一 |
| `idx_search_history_tenant` | tenant_id | 按租户查询 |

### 2.3 约束说明

**唯一索引**：
- `(user_id, query)` 组合唯一
- 同一用户搜索相同内容时，更新 `updated_at`，不创建新记录

---

## 三、API 端点说明

### 3.1 GET /api/search/history - 获取搜索历史列表

**功能**：获取当前用户的搜索历史，按最后搜索时间倒序

**查询参数**：
- `limit`：返回数量（默认 20，最大 100）
- `offset`：偏移量（分页）

**响应**：

```json
{
  "items": [
    {
      "id": 1,
      "query": "城市数据",
      "created_at": "2025-01-01T10:00:00Z",
      "updated_at": "2025-01-03T15:30:00Z"
    },
    {
      "id": 2,
      "query": "shapefile",
      "created_at": "2024-12-28T08:00:00Z",
      "updated_at": "2025-01-02T14:20:00Z"
    }
  ],
  "total": 2
}
```

---

### 3.2 DELETE /api/search/history/:id - 删除单条历史记录

**功能**：删除指定的搜索历史记录

**响应**（204 No Content）

---

### 3.3 DELETE /api/search/history - 清空搜索历史

**功能**：清空当前用户的所有搜索历史

**响应**：

```json
{
  "message": "搜索历史已清空",
  "deleted_count": 15
}
```

---

### 3.4 GET /api/search - 混合检索（自动记录历史）

**功能**：执行混合检索（全文检索 + 向量检索），自动记录到搜索历史

**查询参数**：
- `q`：搜索关键词（必填）
- `page`：页码，默认 1
- `page_size`：每页数量，默认 10

**响应**：

```json
{
  "data": {
    "total": 15,
    "page": 1,
    "page_size": 10,
    "results": [
      {
        "document_id": "abc123",
        "file_name": "城市数据.csv",
        "engine_name": "业务数据库",
        "score": 0.95,
        "highlights": {
          "content": ["包含<mark>城市</mark>相关信息"]
        }
      }
    ],
    "vector_hits": [...]
  }
}
```

**副作用**：自动创建或更新搜索历史记录

---

## 四、搜索历史管理

### 4.1 自动记录机制

**触发时机**：
- 用户执行混合检索（/api/search）
- 查询字符串非空
- 查询成功（有结果或无结果都记录）

**去重逻辑**：
```sql
INSERT INTO manager.search_history (user_id, tenant_id, query)
VALUES (1, 1, '城市数据')
ON CONFLICT (user_id, query)
DO UPDATE SET updated_at = NOW();
```

### 4.2 历史记录排序

**默认排序**：按 `updated_at DESC`（最近搜索的在前）

**用途**：
- 快速访问常用搜索
- 提供搜索建议（基于历史）

---

## 五、使用示例

### 示例 1：执行搜索并自动记录历史

```bash
# 搜索"城市数据"
curl -X GET "http://localhost:8081/api/search?q=城市数据&page=1&page_size=10" \
  -H "Authorization: Bearer $TOKEN"

# 再次搜索相同内容，只会更新 updated_at
curl -X GET "http://localhost:8081/api/search?q=城市数据&page=1&page_size=10" \
  -H "Authorization: Bearer $TOKEN"
```

### 示例 2：查看搜索历史

```bash
# 获取最近 10 条搜索历史
curl -X GET "http://localhost:8081/api/search/history?limit=10" \
  -H "Authorization: Bearer $TOKEN"
```

### 示例 3：清空搜索历史

```bash
# 清空所有搜索历史
curl -X DELETE http://localhost:8081/api/search/history \
  -H "Authorization: Bearer $TOKEN"
```

---

## 六、重要说明

### 6.1 隐私保护

**用户隔离**：
- 每个用户只能查看/删除自己的搜索历史
- 通过 JWT token 中的 user_id 自动过滤

**租户隔离**：
- `tenant_id` 字段用于跨租户查询（SuperAdmin）
- 普通用户只能访问自己租户的数据

### 6.2 存储限制

**历史记录数量**：
- 建议单个用户最多保留 100 条历史
- 超过限制时自动删除最旧的记录

**实现**：
```go
// 创建历史记录后，检查数量并清理
func CreateSearchHistory(userID uint, query string) {
    // 插入记录
    db.Create(&SearchHistory{UserID: userID, Query: query})
    
    // 保留最新 100 条
    db.Exec(`
        DELETE FROM manager.search_history
        WHERE user_id = ? AND id NOT IN (
            SELECT id FROM manager.search_history
            WHERE user_id = ?
            ORDER BY updated_at DESC
            LIMIT 100
        )
    `, userID, userID)
}
```

### 6.3 搜索建议功能

**基于历史的搜索建议**：
- 前端输入时，从搜索历史中模糊匹配
- 提供快速补全功能

**实现**：
```sql
-- 模糊匹配历史查询
SELECT DISTINCT query 
FROM manager.search_history
WHERE user_id = 1 
  AND query LIKE '%城市%'
ORDER BY updated_at DESC
LIMIT 5;
```

---

## 七、性能优化

### 7.1 索引优化

**唯一索引**：`(user_id, query)` 支持快速去重

**查询优化**：
```sql
-- 高效查询用户历史（使用索引）
SELECT * FROM manager.search_history
WHERE user_id = 1
ORDER BY updated_at DESC
LIMIT 20;
```

### 7.2 定期清理

**清理策略**：
- 自动删除 6 个月前的历史记录
- 定时任务每月执行一次

**实现**：
```sql
-- 清理旧记录
DELETE FROM manager.search_history
WHERE updated_at < NOW() - INTERVAL '6 months';
```

---

## 八、相关文档

- [directories表](./directories表.md) - 目录结构表
- [quick_view表](./quick_view表.md) - 快显缓存表
- [数据库架构](../数据库架构.md) - Manager 模块架构
