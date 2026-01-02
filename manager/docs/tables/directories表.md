# directories 表结构和 API 说明

## 一、表结构概览

`manager.directories` 表是 Manager 模块的目录结构表，存储文件和文件夹的层级关系。支持类似文件系统的树形结构，用于组织和管理用户上传的数据资产。

### 核心功能

- **树形目录结构**：支持多级嵌套的文件夹和文件
- **路径管理**：自动维护完整路径，支持快速查找
- **存储关联**：关联到对象存储引擎（MinIO/S3）
- **元数据存储**：记录文件大小、MIME类型等元数据

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 目录项唯一标识 |
| `name` | VARCHAR(255) | NOT NULL | 文件/文件夹名称 |
| `parent_id` | INTEGER | INDEXED | 父目录 ID（根目录为 NULL） |
| `path` | TEXT | INDEXED | 完整路径（如：/folder1/folder2/file.txt） |
| `type` | VARCHAR(50) | NOT NULL | 类型：'folder'、'file' |
| `size` | BIGINT | | 文件大小（字节，文件夹为 0） |
| `mime_type` | VARCHAR(255) | | MIME 类型（如：image/png） |
| `storage_id` | INTEGER | | 关联的存储引擎 ID |
| `created_by` | INTEGER | NOT NULL | 创建者 ID |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_directories_path` | path | 路径查找优化 |
| `idx_directories_parent` | parent_id | 父子关系查询 |

### 2.3 外键关系

| 外键字段 | 引用表 | 引用字段 | 约束 |
|---------|--------|---------|------|
| `parent_id` | `directories` | `id` | ON DELETE CASCADE |
| `storage_id` | `system.engines` | `id` | ON DELETE SET NULL |

---

## 三、Type 说明

| 值 | 含义 | 说明 |
|---|------|------|
| `folder` | 文件夹 | 可包含子文件夹和文件 |
| `file` | 文件 | 叶子节点，不能有子节点 |

---

## 四、API 端点说明

### 4.1 GET /api/data-explorer/tree - 获取目录树

**功能**：获取完整的目录树结构（包含引擎、schema、表等）

**查询参数**：
- `engine_id`：按引擎过滤（可选）
- `expand`：是否展开所有节点（默认 false）

**响应**：

```json
{
  "engines": [
    {
      "id": 1,
      "name": "业务数据库",
      "engine_type": "postgresql",
      "schemas": [
        {
          "name": "public",
          "tables": [
            {
              "name": "cities",
              "full_name": "public.cities",
              "type": "table"
            }
          ]
        }
      ]
    },
    {
      "id": 2,
      "name": "文件存储",
      "engine_type": "minio",
      "schemas": [
        {
          "name": "gis-data",
          "tables": [
            {
              "id": 1,
              "name": "shapefiles",
              "type": "folder",
              "size_bytes": 0,
              "children": [
                {
                  "id": 2,
                  "name": "cities.shp",
                  "type": "file",
                  "size_bytes": 1024000,
                  "content_type": "application/x-shp"
                }
              ]
            }
          ]
        }
      ]
    }
  ]
}
```

---

### 4.2 GET /api/data-explorer/engines/:id/tree - 获取引擎资源树

**功能**：获取指定引擎的详细资源树（schema、表、文件等）

**响应**：类似 /tree，但只包含指定引擎

---

### 4.3 POST /api/data-explorer/engines/:id/refresh - 刷新节点

**功能**：刷新目录节点，从存储引擎同步最新文件列表

**请求体**：

```json
{
  "path": "/folder1/folder2"
}
```

**响应**：

```json
{
  "message": "刷新成功",
  "added": 5,
  "updated": 2,
  "removed": 1
}
```

---

## 五、目录树管理

### 5.1 树形结构维护

**父子关系**：
- 根目录：`parent_id = NULL`
- 子目录：`parent_id` 指向父目录的 `id`

**路径自动生成**：
```go
// 创建文件/文件夹时自动生成 path
func GeneratePath(parentID *uint, name string) string {
    if parentID == nil {
        return "/" + name
    }
    parent := GetDirectoryByID(*parentID)
    return parent.Path + "/" + name
}
```

### 5.2 级联删除

**删除文件夹**：
- 自动删除所有子文件夹和文件（ON DELETE CASCADE）
- 同时删除对象存储中的实际文件

**删除文件**：
- 仅删除数据库记录
- 同时删除对象存储中的文件

---

## 六、使用示例

### 示例 1：获取目录树（前端文件浏览器）

```bash
# 获取完整目录树
curl -X GET http://localhost:8081/api/data-explorer/tree \
  -H "Authorization: Bearer $TOKEN"
```

### 示例 2：上传文件并创建目录记录

```bash
# 1. 上传文件到 MinIO（通过 System 模块的上传 API）
# ...

# 2. 创建目录记录（通常由 Manager 服务自动创建）
# 无独立 API，由文件上传流程自动处理
```

### 示例 3：刷新目录节点

```bash
# 刷新某个文件夹（从 MinIO 同步）
curl -X POST http://localhost:8081/api/data-explorer/engines/2/refresh \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "path": "/shapefiles"
  }'
```

---

## 七、重要说明

### 7.1 与对象存储的同步

**设计理念**：
- `directories` 表是**元数据索引**，不是真实存储
- 实际文件存储在 MinIO/S3
- Manager 模块负责元数据和实际存储的同步

**同步时机**：
- 文件上传时：创建 directory 记录
- 目录扫描时：批量创建/更新记录
- 手动刷新时：调用 /refresh API

### 7.2 路径唯一性

**问题**：同一路径不能有两个文件/文件夹

**解决**：
- 路径字段添加唯一索引（按 storage_id + path）
- 上传前检查路径冲突
- 支持自动重命名（如 file(1).txt）

### 7.3 大文件处理

**文件大小限制**：
- 前端上传：默认 100MB
- 直接对象存储上传：支持 GB 级文件

**大文件元数据**：
- 文件大小存储在 `size` 字段
- 支持查询大文件列表（ORDER BY size DESC）

### 7.4 MIME 类型识别

**自动识别**：
- 根据文件扩展名自动识别
- 支持的常见类型：image/*, application/pdf, text/*, etc

**用途**：
- 前端预览图标
- 文件类型过滤
- 预览功能路由（图片、PDF、文本等）

---

## 八、性能优化

### 8.1 路径索引优化

**场景**：按路径前缀查询（如查找 /folder1/ 下所有文件）

**优化**：
```sql
-- 使用 LIKE 查询 + 索引
SELECT * FROM manager.directories 
WHERE path LIKE '/folder1/%';
```

**索引**：`idx_directories_path` 支持前缀匹配

### 8.2 树形查询优化

**递归查询**（PostgreSQL CTE）：
```sql
-- 查询某个文件夹的所有子节点
WITH RECURSIVE tree AS (
  SELECT * FROM manager.directories WHERE id = 1
  UNION ALL
  SELECT d.* FROM manager.directories d
  INNER JOIN tree t ON d.parent_id = t.id
)
SELECT * FROM tree;
```

---

## 九、相关文档

- [search_history表](./search_history表.md) - 搜索历史表
- [quick_view表](./quick_view表.md) - 快显缓存表
- [数据库架构](../数据库架构.md) - Manager 模块架构
