# ADDP 垃圾数据清理设计文档

## 1. 背景和目标

### 1.1 问题描述

ADDP平台在运行过程中会产生无效的元数据和资源，这些"垃圾数据"会占用存储空间并影响系统性能：

1. **Engine被废弃**：Engine被删除或禁用后，关联的元数据仍然存在
2. **Node不存在**：Schema/Bucket被删除后，其下的items都变成孤儿数据
3. **租户被删除**：租户级联删除时需要清理所有关联数据
4. **扫描任务被删除**：对应的运行记录成为孤儿数据
5. **长期未扫描的过期数据**：元数据可能已不准确
6. **重复的fingerprint**：因bug产生的重复数据
7. **孤儿MetaItem**：node_id指向的MetaNode已被删除
8. **扫描失败遗留数据**：ScanTaskRun失败但已写入部分元数据
9. **Manager模块的垃圾**：无效的向量记录、过期的MVT瓦片
10. **Transfer模块的垃圾**：失败的任务记录、过期的传输历史

### 1.2 设计目标

1. **解耦设计**：各模块独立处理自己的垃圾数据，System不直接调用其他模块API
2. **两步清理**：先扫描统计（软删除标记），再物理删除
3. **权限隔离**：租户管理员只能清理本租户数据，SuperAdmin可清理全局
4. **可追踪**：记录清理历史，支持审计
5. **自动化**：Engine删除时自动触发软删除
6. **异步健壮**：支持超时、部分失败、重试

---

## 2. 整体架构设计

### 2.1 核心思路

**事件驱动 + Redis聚合**

- System模块负责**任务编排**和**结果聚合**
- 各模块订阅事件，**自治处理**垃圾数据
- 通过**Redis**作为统计数据的汇总点
- 前端通过**轮询**获取任务状态

### 2.2 架构图

```
┌─────────────────────────────────────────────────────────┐
│                    前端（Portal/System）                  │
│  1. 发起扫描  2. 轮询状态  3. 确认执行  4. 查询结果      │
└────────────────┬────────────────────────────────────────┘
                 │ HTTP API
┌────────────────▼────────────────────────────────────────┐
│                   System 模块                            │
│  CleanupOrchestratorService                             │
│  - CreateScanTask() → 创建扫描任务                       │
│  - CreateExecuteTask() → 创建清理任务                    │
│  - GetTaskStatus() → 查询任务状态                        │
│  - 发布事件到 Redis Stream                               │
│  - 聚合各模块结果                                         │
└────────────────┬────────────────────────────────────────┘
                 │ Redis Stream Events
                 │ cleanup:requests
        ┌────────┴────────┬────────────────┐
        │                 │                │
┌───────▼──────┐  ┌──────▼──────┐  ┌─────▼──────┐
│ Meta 模块    │  │Manager 模块 │  │Transfer模块│
│ CleanupSvc   │  │ CleanupSvc  │  │ CleanupSvc │
│              │  │             │  │            │
│ 订阅事件     │  │ 订阅事件    │  │ 订阅事件   │
│ ↓            │  │ ↓           │  │ ↓          │
│ ScanGarbage  │  │ ScanGarbage │  │ ScanGarbage│
│ Execute      │  │ Execute     │  │ Execute    │
│ ↓            │  │ ↓           │  │ ↓          │
│ 写入Redis    │  │ 写入Redis   │  │ 写入Redis  │
└──────────────┘  └─────────────┘  └────────────┘
        │                 │                │
        └────────┬────────┴────────────────┘
                 │ Redis Hash
                 │ cleanup:results:{task_id}
                 ▼
        ┌────────────────┐
        │  Redis 存储    │
        │  - 任务元数据  │
        │  - 各模块结果  │
        │  - 历史记录    │
        └────────────────┘
```

### 2.3 模块职责

| 模块 | 职责 | 输入 | 输出 |
|------|------|------|------|
| **System** | 任务编排、结果聚合 | 前端请求 | 聚合的统计数据 |
| **Meta** | 清理元数据（node/item） | cleanup事件 | 统计结果写入Redis |
| **Manager** | 清理向量、MVT瓦片 | cleanup事件 | 统计结果写入Redis |
| **Transfer** | 清理任务历史 | cleanup事件 | 统计结果写入Redis |
| **Redis** | 事件传递、结果存储 | - | - |

---

## 3. 数据结构定义

### 3.1 Common层事件定义

```go
// common/events/cleanup_events.go

package events

import "time"

// 事件类型常量
const (
    // 引擎删除事件
    EventEngineDeleted = "engine.deleted"
    // 租户删除事件
    EventTenantDeleted = "tenant.deleted"
    // 清理请求事件
    EventCleanupRequest = "cleanup.request"
)

// 清理动作类型
const (
    CleanupActionScan    = "scan"    // 扫描垃圾数据
    CleanupActionExecute = "execute" // 执行清理
)

// 删除类型
const (
    DeleteTypeSoft = "soft_delete" // 软删除（标记deleted_at）
    DeleteTypeHard = "hard_delete" // 物理删除
)

// 模块名称常量
const (
    ModuleMeta     = "meta"
    ModuleManager  = "manager"
    ModuleTransfer = "transfer"
    ModuleDevelop  = "develop"
    ModuleService  = "service"
)

// EngineDeletedEvent - Engine删除事件
type EngineDeletedEvent struct {
    EngineID  uint      `json:"engine_id"`
    TenantID  uint      `json:"tenant_id"`
    DeletedAt time.Time `json:"deleted_at"`
    DeletedBy uint      `json:"deleted_by"`
}

// TenantDeletedEvent - 租户删除事件
type TenantDeletedEvent struct {
    TenantID  uint      `json:"tenant_id"`
    DeletedAt time.Time `json:"deleted_at"`
    DeletedBy uint      `json:"deleted_by"`
}

// CleanupRequestEvent - 清理请求事件
type CleanupRequestEvent struct {
    TaskID          string   `json:"task_id"`           // 任务ID
    Action          string   `json:"action"`            // scan/execute
    TenantID        uint     `json:"tenant_id"`         // 租户ID（0=全局）
    DeleteType      string   `json:"delete_type"`       // soft_delete/hard_delete
    ExpectedModules []string `json:"expected_modules"`  // 期望响应的模块列表
    BasedOnScan     string   `json:"based_on_scan"`     // 基于哪次扫描（execute时使用）
    RequestedBy     uint     `json:"requested_by"`      // 请求用户ID
    RequestedAt     time.Time `json:"requested_at"`     // 请求时间
}

// CleanupResultData - 清理结果数据（各模块写入Redis）
type CleanupResultData struct {
    Module      string                 `json:"module"`      // 模块名称
    Status      string                 `json:"status"`      // success/failed/timeout
    Timestamp   time.Time              `json:"timestamp"`   // 完成时间
    Statistics  map[string]interface{} `json:"statistics"`  // 统计数据
    Details     interface{}            `json:"details"`     // 详细信息
    Error       string                 `json:"error,omitempty"` // 错误信息
}
```

### 3.2 Meta模块统计数据结构

```go
// meta/backend/internal/models/cleanup.go

package models

// MetaCleanupStatistics - Meta模块垃圾数据统计
type MetaCleanupStatistics struct {
    // 无效引擎的数据
    InvalidEngines struct {
        Count   int                        `json:"count"`
        Details []InvalidEngineDetail      `json:"details"`
    } `json:"invalid_engines"`

    // 孤儿数据项（node_id不存在）
    OrphanItems struct {
        Count  int               `json:"count"`
        Sample []OrphanItemDetail `json:"sample"` // 最多返回10条样本
    } `json:"orphan_items"`

    // 过期数据（长期未扫描）
    ExpiredData struct {
        Count         int    `json:"count"`
        ThresholdDays int    `json:"threshold_days"` // 过期阈值（天）
    } `json:"expired_data"`

    // 软删除数据
    SoftDeleted struct {
        Nodes        int  `json:"nodes"`
        Items        int  `json:"items"`
        CanRecover   bool `json:"can_recover"`
    } `json:"soft_deleted"`

    // 重复fingerprint
    DuplicateFingerprints struct {
        Count int `json:"count"`
    } `json:"duplicate_fingerprints"`
}

// InvalidEngineDetail - 无效引擎详情
type InvalidEngineDetail struct {
    EngineID      uint   `json:"engine_id"`
    EngineName    string `json:"engine_name"`
    AffectedNodes int    `json:"affected_nodes"`
    AffectedItems int    `json:"affected_items"`
    Reason        string `json:"reason"` // "engine已删除"/"engine已禁用"
}

// OrphanItemDetail - 孤儿数据详情
type OrphanItemDetail struct {
    ItemID   uint   `json:"item_id"`
    ItemName string `json:"item_name"`
    NodeID   uint   `json:"node_id"`
    Reason   string `json:"reason"`
}

// MetaCleanupExecuteResult - Meta清理执行结果
type MetaCleanupExecuteResult struct {
    DeletedNodes          int      `json:"deleted_nodes"`
    DeletedItems          int      `json:"deleted_items"`
    DeletedFingerprints   int      `json:"deleted_fingerprints"`
    Errors                []string `json:"errors"`
}
```

### 3.3 Manager模块统计数据结构

```go
// manager/backend/internal/models/cleanup.go

package models

// ManagerCleanupStatistics - Manager模块垃圾数据统计
type ManagerCleanupStatistics struct {
    // 孤儿向量记录（meta_item已被删除）
    OrphanVectors struct {
        Count  int    `json:"count"`
        Reason string `json:"reason"`
    } `json:"orphan_vectors"`

    // 孤儿MVT瓦片（meta_item已被删除）
    OrphanMVTTiles struct {
        Count  int     `json:"count"`
        SizeMB float64 `json:"size_mb"`
        Reason string  `json:"reason"`
    } `json:"orphan_mvt_tiles"`

    // 过期缓存
    ExpiredCache struct {
        Count      int `json:"count"`
        AgeDays    int `json:"age_days"`
    } `json:"expired_cache"`
}

// ManagerCleanupExecuteResult - Manager清理执行结果
type ManagerCleanupExecuteResult struct {
    DeletedVectors   int      `json:"deleted_vectors"`
    DeletedMVTTiles  int      `json:"deleted_mvt_tiles"`
    DeletedCaches    int      `json:"deleted_caches"`
    FreedSpaceMB     float64  `json:"freed_space_mb"`
    Errors           []string `json:"errors"`
}
```

### 3.4 System任务数据结构

```go
// system/backend/internal/models/cleanup.go

package models

import "time"

// CleanupTask - 清理任务元数据
type CleanupTask struct {
    TaskID          string    `json:"task_id"`
    Action          string    `json:"action"`            // scan/execute
    TenantID        uint      `json:"tenant_id"`
    DeleteType      string    `json:"delete_type"`       // soft_delete/hard_delete
    Status          string    `json:"status"`            // pending/running/completed/timeout/failed
    ExpectedModules []string  `json:"expected_modules"`
    RequestedBy     uint      `json:"requested_by"`
    StartedAt       time.Time `json:"started_at"`
    CompletedAt     *time.Time `json:"completed_at,omitempty"`
    TimeoutAt       time.Time `json:"timeout_at"`
    BasedOnScan     string    `json:"based_on_scan,omitempty"`
}

// TaskStatusResponse - 任务状态响应
type TaskStatusResponse struct {
    TaskID   string                    `json:"task_id"`
    Action   string                    `json:"action"`
    Status   string                    `json:"status"`
    Progress TaskProgress              `json:"progress"`
    Results  map[string]interface{}    `json:"results"`  // key=module, value=CleanupResultData
    Summary  TaskSummary               `json:"summary"`
    Task     CleanupTask               `json:"task"`
}

// TaskProgress - 任务进度
type TaskProgress struct {
    Total     int                `json:"total"`     // 期望模块数
    Completed int                `json:"completed"` // 已完成模块数
    Modules   map[string]string  `json:"modules"`   // key=module, value=status
}

// TaskSummary - 任务汇总（仅scan任务）
type TaskSummary struct {
    TotalItemsToClean int     `json:"total_items_to_clean"`
    TotalSizeMB       float64 `json:"total_size_mb"`
    RiskLevel         string  `json:"risk_level"` // low/medium/high
}

// ExecuteSummary - 执行汇总（execute任务）
type ExecuteSummary struct {
    TotalDeleted int     `json:"total_deleted"`
    FreedSpaceMB float64 `json:"freed_space_mb"`
    HasErrors    bool    `json:"has_errors"`
}
```

---

## 4. Redis存储设计

### 4.1 Key命名规范

| Key Pattern | 类型 | 用途 | TTL |
|-------------|------|------|-----|
| `cleanup:tasks:{task_id}` | Hash | 任务元数据 | 1小时 |
| `cleanup:results:{task_id}` | Hash | 各模块结果（field=module） | 1小时 |
| `cleanup:history:{tenant_id}` | List | 历史任务ID列表（最多100条） | 永久 |
| `cleanup:requests` | Stream | 清理请求事件流 | 自动裁剪 |

### 4.2 数据结构示例

#### cleanup:tasks:{task_id}

```json
{
  "task_id": "cleanup-scan-1736496000-a1b2c3d4",
  "action": "scan",
  "tenant_id": "1",
  "status": "running",
  "expected_modules": "[\"meta\",\"manager\",\"transfer\"]",
  "requested_by": "2",
  "started_at": "2026-01-10T10:00:00Z",
  "timeout_at": "2026-01-10T10:00:30Z"
}
```

#### cleanup:results:{task_id}

```json
{
  "meta": "{\"module\":\"meta\",\"status\":\"success\",\"timestamp\":\"2026-01-10T10:00:05Z\",\"statistics\":{\"invalid_engines\":{\"count\":2}}}",
  "manager": "{\"module\":\"manager\",\"status\":\"success\",\"timestamp\":\"2026-01-10T10:00:06Z\",\"statistics\":{\"orphan_vectors\":{\"count\":45}}}",
  "transfer": "{\"module\":\"transfer\",\"status\":\"timeout\",\"timestamp\":\"2026-01-10T10:00:30Z\"}"
}
```

#### cleanup:history:{tenant_id}

```
["cleanup-scan-1736496000-a1b2c3d4", "cleanup-exec-1736496100-e5f6g7h8", ...]
```

### 4.3 Redis Stream处理

```go
// System发布事件
XADD cleanup:requests *
  task_id cleanup-scan-1736496000-a1b2c3d4
  action scan
  tenant_id 1
  expected_modules meta,manager,transfer
  requested_at 2026-01-10T10:00:00Z

// 各模块订阅（Consumer Group模式）
XGROUP CREATE cleanup:requests meta-consumer $ MKSTREAM
XREADGROUP GROUP meta-consumer meta-worker COUNT 1 BLOCK 5000 STREAMS cleanup:requests >

// 确认处理
XACK cleanup:requests meta-consumer {message_id}
```

---

## 5. API接口规范

### 5.1 System模块API

#### 5.1.1 创建扫描任务

```http
POST /api/system/admin/cleanup/scan
Authorization: Bearer {token}
Content-Type: application/json

Request:
{
  "tenant_id": 1,  // 0=全局（SuperAdmin），非0=租户ID
  "scope": ["meta", "manager", "transfer"]  // 可选，默认全部模块
}

Response (200):
{
  "task_id": "cleanup-scan-1736496000-a1b2c3d4",
  "status": "pending",
  "message": "扫描任务已创建，请轮询查询状态"
}

Error (403):
{
  "error": "权限不足：只能扫描本租户数据"
}
```

#### 5.1.2 查询任务状态

```http
GET /api/system/admin/cleanup/tasks/{task_id}
Authorization: Bearer {token}

Response (200) - 扫描任务:
{
  "task_id": "cleanup-scan-1736496000-a1b2c3d4",
  "action": "scan",
  "status": "completed",  // pending/running/completed/timeout/failed
  "progress": {
    "total": 3,
    "completed": 3,
    "modules": {
      "meta": "success",
      "manager": "success",
      "transfer": "timeout"
    }
  },
  "results": {
    "meta": {
      "module": "meta",
      "status": "success",
      "timestamp": "2026-01-10T10:00:05Z",
      "statistics": {
        "invalid_engines": {
          "count": 2,
          "details": [...]
        },
        "orphan_items": {
          "count": 12
        }
      }
    },
    "manager": {
      "module": "manager",
      "status": "success",
      "statistics": {
        "orphan_vectors": {"count": 45},
        "orphan_mvt_tiles": {"count": 128, "size_mb": 23.5}
      }
    },
    "transfer": {
      "module": "transfer",
      "status": "timeout",
      "error": "响应超时"
    }
  },
  "summary": {
    "total_items_to_clean": 187,
    "total_size_mb": 23.5,
    "risk_level": "low"
  },
  "task": {
    "started_at": "2026-01-10T10:00:00Z",
    "completed_at": "2026-01-10T10:00:30Z"
  }
}

Response (200) - 执行任务:
{
  "task_id": "cleanup-exec-1736496100-e5f6g7h8",
  "action": "execute",
  "status": "completed",
  "results": {
    "meta": {
      "status": "success",
      "statistics": {
        "deleted_nodes": 17,
        "deleted_items": 234
      }
    },
    "manager": {
      "status": "success",
      "statistics": {
        "deleted_vectors": 45,
        "deleted_mvt_tiles": 128,
        "freed_space_mb": 23.5
      }
    }
  },
  "summary": {
    "total_deleted": 424,
    "freed_space_mb": 23.5,
    "has_errors": false
  }
}
```

#### 5.1.3 执行清理

```http
POST /api/system/admin/cleanup/execute
Authorization: Bearer {token}
Content-Type: application/json

Request:
{
  "based_on_scan": "cleanup-scan-1736496000-a1b2c3d4",  // 基于哪次扫描
  "delete_type": "soft_delete",  // soft_delete/hard_delete
  "confirm": true,  // 二次确认
  "dry_run": false  // true=模拟运行，不真删除
}

Response (200):
{
  "task_id": "cleanup-exec-1736496100-e5f6g7h8",
  "status": "pending",
  "message": "清理任务已创建，请轮询查询状态"
}

Error (400):
{
  "error": "基础扫描任务不存在或已过期"
}

Error (403):
{
  "error": "只有SuperAdmin可以执行硬删除"
}
```

#### 5.1.4 查询历史任务

```http
GET /api/system/admin/cleanup/history?tenant_id={tenant_id}&limit={limit}
Authorization: Bearer {token}

Response (200):
{
  "tasks": [
    {
      "task_id": "cleanup-scan-1736496000-a1b2c3d4",
      "action": "scan",
      "status": "completed",
      "started_at": "2026-01-10T10:00:00Z",
      "completed_at": "2026-01-10T10:00:30Z"
    },
    ...
  ],
  "total": 25
}
```

### 5.2 各模块内部实现（不暴露HTTP API）

各模块只需实现清理服务，订阅事件即可，**不需要暴露HTTP API**。

---

## 6. 各模块实现要点

### 6.1 System模块实现

#### 6.1.1 CleanupOrchestratorService

```go
// system/backend/internal/service/cleanup_orchestrator_service.go

package service

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "addp/common/events"
    "addp/system/backend/internal/models"
    "github.com/go-redis/redis/v8"
    "github.com/google/uuid"
)

type CleanupOrchestratorService struct {
    redis     *redis.Client
    eventBus  EventBus
    jwtSecret string
}

// CreateScanTask 创建扫描任务
func (s *CleanupOrchestratorService) CreateScanTask(ctx context.Context, tenantID uint, scope []string, userID uint) (string, error) {
    // 生成任务ID
    taskID := fmt.Sprintf("cleanup-scan-%d-%s", time.Now().Unix(), uuid.New().String()[:8])

    // 默认扫描所有模块
    if len(scope) == 0 {
        scope = []string{events.ModuleMeta, events.ModuleManager, events.ModuleTransfer}
    }

    // 创建任务
    task := models.CleanupTask{
        TaskID:          taskID,
        Action:          events.CleanupActionScan,
        TenantID:        tenantID,
        Status:          "pending",
        ExpectedModules: scope,
        RequestedBy:     userID,
        StartedAt:       time.Now(),
        TimeoutAt:       time.Now().Add(30 * time.Second),
    }

    // 写入Redis
    taskData, _ := json.Marshal(task)
    err := s.redis.HSet(ctx, fmt.Sprintf("cleanup:tasks:%s", taskID), map[string]interface{}{
        "data": string(taskData),
    }).Err()
    if err != nil {
        return "", fmt.Errorf("failed to save task: %w", err)
    }

    // 设置过期时间
    s.redis.Expire(ctx, fmt.Sprintf("cleanup:tasks:%s", taskID), 1*time.Hour)

    // 发布事件
    event := events.CleanupRequestEvent{
        TaskID:          taskID,
        Action:          events.CleanupActionScan,
        TenantID:        tenantID,
        ExpectedModules: scope,
        RequestedBy:     userID,
        RequestedAt:     time.Now(),
    }

    err = s.eventBus.Publish(ctx, events.EventCleanupRequest, event)
    if err != nil {
        return "", fmt.Errorf("failed to publish event: %w", err)
    }

    // 记录历史
    s.redis.LPush(ctx, fmt.Sprintf("cleanup:history:%d", tenantID), taskID)
    s.redis.LTrim(ctx, fmt.Sprintf("cleanup:history:%d", tenantID), 0, 99) // 只保留最近100条

    return taskID, nil
}

// GetTaskStatus 查询任务状态
func (s *CleanupOrchestratorService) GetTaskStatus(ctx context.Context, taskID string) (*models.TaskStatusResponse, error) {
    // 读取任务信息
    taskDataStr, err := s.redis.HGet(ctx, fmt.Sprintf("cleanup:tasks:%s", taskID), "data").Result()
    if err == redis.Nil {
        return nil, fmt.Errorf("task not found")
    }
    if err != nil {
        return nil, err
    }

    var task models.CleanupTask
    if err := json.Unmarshal([]byte(taskDataStr), &task); err != nil {
        return nil, err
    }

    // 读取各模块结果
    resultsMap, err := s.redis.HGetAll(ctx, fmt.Sprintf("cleanup:results:%s", taskID)).Result()
    if err != nil {
        return nil, err
    }

    // 解析结果
    results := make(map[string]interface{})
    progress := models.TaskProgress{
        Total:   len(task.ExpectedModules),
        Modules: make(map[string]string),
    }

    for _, module := range task.ExpectedModules {
        if resultStr, ok := resultsMap[module]; ok {
            var result events.CleanupResultData
            if err := json.Unmarshal([]byte(resultStr), &result); err == nil {
                results[module] = result
                progress.Modules[module] = result.Status
                if result.Status == "success" || result.Status == "failed" {
                    progress.Completed++
                }
            }
        } else {
            // 检查超时
            if time.Now().After(task.TimeoutAt) {
                progress.Modules[module] = "timeout"
            } else {
                progress.Modules[module] = "pending"
            }
        }
    }

    // 计算整体状态
    overallStatus := s.calculateOverallStatus(&task, &progress)

    // 汇总统计（仅scan任务）
    var summary interface{}
    if task.Action == events.CleanupActionScan {
        summary = s.aggregateScanSummary(results)
    } else {
        summary = s.aggregateExecuteSummary(results)
    }

    return &models.TaskStatusResponse{
        TaskID:   taskID,
        Action:   task.Action,
        Status:   overallStatus,
        Progress: progress,
        Results:  results,
        Summary:  summary,
        Task:     task,
    }, nil
}

// calculateOverallStatus 计算整体状态
func (s *CleanupOrchestratorService) calculateOverallStatus(task *models.CleanupTask, progress *models.TaskProgress) string {
    if progress.Completed == progress.Total {
        // 全部完成
        hasFailure := false
        for _, status := range progress.Modules {
            if status == "failed" || status == "timeout" {
                hasFailure = true
                break
            }
        }
        if hasFailure {
            return "completed_with_errors"
        }
        return "completed"
    }

    if time.Now().After(task.TimeoutAt) {
        return "timeout"
    }

    if progress.Completed > 0 {
        return "running"
    }

    return "pending"
}

// aggregateScanSummary 汇总扫描统计
func (s *CleanupOrchestratorService) aggregateScanSummary(results map[string]interface{}) models.TaskSummary {
    summary := models.TaskSummary{}

    // 遍历各模块结果，累加统计数据
    for _, result := range results {
        if resultData, ok := result.(events.CleanupResultData); ok {
            if stats, ok := resultData.Statistics.(map[string]interface{}); ok {
                // 根据不同模块累加数据
                // TODO: 实现具体的累加逻辑
            }
        }
    }

    // 评估风险等级
    if summary.TotalItemsToClean > 1000 {
        summary.RiskLevel = "high"
    } else if summary.TotalItemsToClean > 100 {
        summary.RiskLevel = "medium"
    } else {
        summary.RiskLevel = "low"
    }

    return summary
}

// CreateExecuteTask 创建清理执行任务
func (s *CleanupOrchestratorService) CreateExecuteTask(ctx context.Context, basedOnScan string, deleteType string, dryRun bool, userID uint) (string, error) {
    // 验证基础扫描任务
    scanTask, err := s.GetTaskStatus(ctx, basedOnScan)
    if err != nil {
        return "", fmt.Errorf("scan task not found: %w", err)
    }

    // 生成任务ID
    taskID := fmt.Sprintf("cleanup-exec-%d-%s", time.Now().Unix(), uuid.New().String()[:8])

    // 创建任务
    task := models.CleanupTask{
        TaskID:          taskID,
        Action:          events.CleanupActionExecute,
        TenantID:        scanTask.Task.TenantID,
        DeleteType:      deleteType,
        Status:          "pending",
        ExpectedModules: scanTask.Task.ExpectedModules,
        RequestedBy:     userID,
        StartedAt:       time.Now(),
        TimeoutAt:       time.Now().Add(5 * time.Minute), // 执行任务超时5分钟
        BasedOnScan:     basedOnScan,
    }

    // 保存任务并发布事件（逻辑同CreateScanTask）
    // ...

    return taskID, nil
}
```

#### 6.1.2 API Handler

```go
// system/backend/internal/api/cleanup_handler.go

package api

import (
    "net/http"
    "strconv"

    "addp/system/backend/internal/middleware"
    "addp/system/backend/internal/service"
    "github.com/gin-gonic/gin"
)

type CleanupHandler struct {
    cleanupService *service.CleanupOrchestratorService
}

// CreateScanTask 创建扫描任务
func (h *CleanupHandler) CreateScanTask(c *gin.Context) {
    var req struct {
        TenantID uint     `json:"tenant_id"`
        Scope    []string `json:"scope"`
    }

    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    // 权限检查
    user := c.MustGet("user").(*middleware.UserClaims)
    if user.Role != "SuperAdmin" && req.TenantID != user.TenantID {
        c.JSON(http.StatusForbidden, gin.H{"error": "权限不足：只能扫描本租户数据"})
        return
    }

    taskID, err := h.cleanupService.CreateScanTask(c.Request.Context(), req.TenantID, req.Scope, user.UserID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "task_id": taskID,
        "status":  "pending",
        "message": "扫描任务已创建，请轮询查询状态",
    })
}

// GetTaskStatus 查询任务状态
func (h *CleanupHandler) GetTaskStatus(c *gin.Context) {
    taskID := c.Param("task_id")

    status, err := h.cleanupService.GetTaskStatus(c.Request.Context(), taskID)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, status)
}

// CreateExecuteTask 创建执行任务
func (h *CleanupHandler) CreateExecuteTask(c *gin.Context) {
    var req struct {
        BasedOnScan string `json:"based_on_scan" binding:"required"`
        DeleteType  string `json:"delete_type" binding:"required"`
        Confirm     bool   `json:"confirm"`
        DryRun      bool   `json:"dry_run"`
    }

    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    if !req.Confirm {
        c.JSON(http.StatusBadRequest, gin.H{"error": "需要确认操作"})
        return
    }

    // 权限检查：只有SuperAdmin可以硬删除
    user := c.MustGet("user").(*middleware.UserClaims)
    if req.DeleteType == "hard_delete" && user.Role != "SuperAdmin" {
        c.JSON(http.StatusForbidden, gin.H{"error": "只有SuperAdmin可以执行硬删除"})
        return
    }

    taskID, err := h.cleanupService.CreateExecuteTask(c.Request.Context(), req.BasedOnScan, req.DeleteType, req.DryRun, user.UserID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "task_id": taskID,
        "status":  "pending",
        "message": "清理任务已创建，请轮询查询状态",
    })
}
```

### 6.2 Meta模块实现

#### 6.2.1 CleanupService

```go
// meta/backend/internal/service/cleanup_service.go

package service

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "addp/common/events"
    "addp/meta/backend/internal/models"
    "addp/meta/backend/internal/repository"
    "github.com/go-redis/redis/v8"
    "gorm.io/gorm"
)

type CleanupService struct {
    db       *gorm.DB
    redis    *redis.Client
    eventBus EventBus
}

// SubscribeCleanupEvents 订阅清理事件
func (s *CleanupService) SubscribeCleanupEvents(ctx context.Context) error {
    // 订阅清理请求事件
    return s.eventBus.Subscribe(ctx, events.EventCleanupRequest, func(event events.CleanupRequestEvent) {
        s.handleCleanupRequest(ctx, event)
    })
}

// handleCleanupRequest 处理清理请求
func (s *CleanupService) handleCleanupRequest(ctx context.Context, event events.CleanupRequestEvent) {
    result := events.CleanupResultData{
        Module:    events.ModuleMeta,
        Timestamp: time.Now(),
    }

    // 无论成功失败都写入响应
    defer func() {
        s.writeResult(ctx, event.TaskID, result)
    }()

    // 根据动作类型处理
    switch event.Action {
    case events.CleanupActionScan:
        stats, err := s.ScanGarbage(ctx, event.TenantID)
        if err != nil {
            result.Status = "failed"
            result.Error = err.Error()
            return
        }
        result.Status = "success"
        result.Statistics = stats

    case events.CleanupActionExecute:
        execResult, err := s.ExecuteCleanup(ctx, event.TenantID, event.DeleteType)
        if err != nil {
            result.Status = "failed"
            result.Error = err.Error()
            return
        }
        result.Status = "success"
        result.Statistics = execResult

    default:
        result.Status = "failed"
        result.Error = "unknown action"
    }
}

// ScanGarbage 扫描垃圾数据
func (s *CleanupService) ScanGarbage(ctx context.Context, tenantID uint) (*models.MetaCleanupStatistics, error) {
    stats := &models.MetaCleanupStatistics{}

    // 1. 扫描无效引擎的数据
    invalidEngines, err := s.scanInvalidEngines(ctx, tenantID)
    if err != nil {
        return nil, err
    }
    stats.InvalidEngines.Count = len(invalidEngines)
    stats.InvalidEngines.Details = invalidEngines

    // 2. 扫描孤儿数据项
    orphanItems, err := s.scanOrphanItems(ctx, tenantID)
    if err != nil {
        return nil, err
    }
    stats.OrphanItems.Count = len(orphanItems)
    if len(orphanItems) > 10 {
        stats.OrphanItems.Sample = orphanItems[:10]
    } else {
        stats.OrphanItems.Sample = orphanItems
    }

    // 3. 扫描过期数据（90天未扫描）
    expiredCount, err := s.scanExpiredData(ctx, tenantID, 90)
    if err != nil {
        return nil, err
    }
    stats.ExpiredData.Count = expiredCount
    stats.ExpiredData.ThresholdDays = 90

    // 4. 扫描软删除数据
    softDeletedNodes, softDeletedItems, err := s.scanSoftDeleted(ctx, tenantID)
    if err != nil {
        return nil, err
    }
    stats.SoftDeleted.Nodes = softDeletedNodes
    stats.SoftDeleted.Items = softDeletedItems
    stats.SoftDeleted.CanRecover = true

    // 5. 扫描重复fingerprint
    duplicateCount, err := s.scanDuplicateFingerprints(ctx, tenantID)
    if err != nil {
        return nil, err
    }
    stats.DuplicateFingerprints.Count = duplicateCount

    return stats, nil
}

// scanInvalidEngines 扫描无效引擎的数据
func (s *CleanupService) scanInvalidEngines(ctx context.Context, tenantID uint) ([]models.InvalidEngineDetail, error) {
    var details []models.InvalidEngineDetail

    // 查询所有在meta中存在但在system中不存在或已禁用的engine_id
    query := `
        SELECT
            mn.engine_id,
            COUNT(DISTINCT mn.id) as affected_nodes,
            COUNT(DISTINCT mi.id) as affected_items
        FROM metadata.meta_node mn
        LEFT JOIN metadata.meta_item mi ON mn.id = mi.node_id
        LEFT JOIN system.engines e ON mn.engine_id = e.id
        WHERE (e.id IS NULL OR e.is_active = false OR e.deleted_at IS NOT NULL)
    `

    if tenantID > 0 {
        query += fmt.Sprintf(" AND mn.tenant_id = %d", tenantID)
    }

    query += " GROUP BY mn.engine_id"

    rows, err := s.db.Raw(query).Rows()
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    for rows.Next() {
        var detail models.InvalidEngineDetail
        if err := rows.Scan(&detail.EngineID, &detail.AffectedNodes, &detail.AffectedItems); err != nil {
            return nil, err
        }
        detail.Reason = "引擎已删除或禁用"
        details = append(details, detail)
    }

    return details, nil
}

// scanOrphanItems 扫描孤儿数据项
func (s *CleanupService) scanOrphanItems(ctx context.Context, tenantID uint) ([]models.OrphanItemDetail, error) {
    var items []models.OrphanItemDetail

    query := `
        SELECT mi.id, mi.name, mi.node_id
        FROM metadata.meta_item mi
        LEFT JOIN metadata.meta_node mn ON mi.node_id = mn.id
        WHERE mn.id IS NULL
    `

    if tenantID > 0 {
        query += fmt.Sprintf(" AND mi.tenant_id = %d", tenantID)
    }

    query += " LIMIT 100"  // 最多返回100条

    rows, err := s.db.Raw(query).Rows()
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    for rows.Next() {
        var item models.OrphanItemDetail
        if err := rows.Scan(&item.ItemID, &item.ItemName, &item.NodeID); err != nil {
            return nil, err
        }
        item.Reason = "node_id不存在"
        items = append(items, item)
    }

    return items, nil
}

// scanExpiredData 扫描过期数据
func (s *CleanupService) scanExpiredData(ctx context.Context, tenantID uint, thresholdDays int) (int, error) {
    var count int64

    query := s.db.Model(&models.MetaItem{}).
        Where("scanned_at < ?", time.Now().AddDate(0, 0, -thresholdDays))

    if tenantID > 0 {
        query = query.Where("tenant_id = ?", tenantID)
    }

    if err := query.Count(&count).Error; err != nil {
        return 0, err
    }

    return int(count), nil
}

// scanSoftDeleted 扫描软删除数据
func (s *CleanupService) scanSoftDeleted(ctx context.Context, tenantID uint) (int, int, error) {
    var nodeCount, itemCount int64

    // 统计软删除的节点
    nodeQuery := s.db.Model(&models.MetaNode{}).Unscoped().Where("deleted_at IS NOT NULL")
    if tenantID > 0 {
        nodeQuery = nodeQuery.Where("tenant_id = ?", tenantID)
    }
    if err := nodeQuery.Count(&nodeCount).Error; err != nil {
        return 0, 0, err
    }

    // 统计软删除的数据项
    itemQuery := s.db.Model(&models.MetaItem{}).Unscoped().Where("deleted_at IS NOT NULL")
    if tenantID > 0 {
        itemQuery = itemQuery.Where("tenant_id = ?", tenantID)
    }
    if err := itemQuery.Count(&itemCount).Error; err != nil {
        return 0, 0, err
    }

    return int(nodeCount), int(itemCount), nil
}

// scanDuplicateFingerprints 扫描重复fingerprint
func (s *CleanupService) scanDuplicateFingerprints(ctx context.Context, tenantID uint) (int, error) {
    var count int64

    query := `
        SELECT COUNT(*) FROM (
            SELECT fingerprint
            FROM metadata.meta_item
            WHERE deleted_at IS NULL
    `

    if tenantID > 0 {
        query += fmt.Sprintf(" AND tenant_id = %d", tenantID)
    }

    query += `
            GROUP BY fingerprint
            HAVING COUNT(*) > 1
        ) AS duplicates
    `

    if err := s.db.Raw(query).Scan(&count).Error; err != nil {
        return 0, err
    }

    return int(count), nil
}

// ExecuteCleanup 执行清理
func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, deleteType string) (*models.MetaCleanupExecuteResult, error) {
    result := &models.MetaCleanupExecuteResult{}

    // 根据删除类型执行
    switch deleteType {
    case events.DeleteTypeSoft:
        return s.executeSoftDelete(ctx, tenantID)
    case events.DeleteTypeHard:
        return s.executeHardDelete(ctx, tenantID)
    default:
        return nil, fmt.Errorf("unknown delete type: %s", deleteType)
    }
}

// executeSoftDelete 执行软删除
func (s *CleanupService) executeSoftDelete(ctx context.Context, tenantID uint) (*models.MetaCleanupExecuteResult, error) {
    result := &models.MetaCleanupExecuteResult{}

    // 1. 软删除无效引擎的节点
    invalidEngineIDs := s.getInvalidEngineIDs(ctx, tenantID)
    if len(invalidEngineIDs) > 0 {
        nodeResult := s.db.Model(&models.MetaNode{}).
            Where("engine_id IN ?", invalidEngineIDs)

        if tenantID > 0 {
            nodeResult = nodeResult.Where("tenant_id = ?", tenantID)
        }

        if err := nodeResult.Delete(&models.MetaNode{}).Error; err != nil {
            result.Errors = append(result.Errors, fmt.Sprintf("软删除节点失败: %v", err))
        } else {
            result.DeletedNodes = int(nodeResult.RowsAffected)
        }

        // 软删除关联的items
        itemResult := s.db.Model(&models.MetaItem{}).
            Where("engine_id IN ?", invalidEngineIDs)

        if tenantID > 0 {
            itemResult = itemResult.Where("tenant_id = ?", tenantID)
        }

        if err := itemResult.Delete(&models.MetaItem{}).Error; err != nil {
            result.Errors = append(result.Errors, fmt.Sprintf("软删除项失败: %v", err))
        } else {
            result.DeletedItems = int(itemResult.RowsAffected)
        }
    }

    // 2. 软删除孤儿items
    orphanResult := s.db.Exec(`
        DELETE FROM metadata.meta_item
        WHERE id IN (
            SELECT mi.id
            FROM metadata.meta_item mi
            LEFT JOIN metadata.meta_node mn ON mi.node_id = mn.id
            WHERE mn.id IS NULL
        )
    `)

    if orphanResult.Error != nil {
        result.Errors = append(result.Errors, fmt.Sprintf("软删除孤儿项失败: %v", orphanResult.Error))
    } else {
        result.DeletedItems += int(orphanResult.RowsAffected)
    }

    return result, nil
}

// executeHardDelete 执行硬删除（物理删除软删除的数据）
func (s *CleanupService) executeHardDelete(ctx context.Context, tenantID uint) (*models.MetaCleanupExecuteResult, error) {
    result := &models.MetaCleanupExecuteResult{}

    // 物理删除软删除的节点
    nodeQuery := s.db.Unscoped().Where("deleted_at IS NOT NULL")
    if tenantID > 0 {
        nodeQuery = nodeQuery.Where("tenant_id = ?", tenantID)
    }

    if err := nodeQuery.Delete(&models.MetaNode{}).Error; err != nil {
        result.Errors = append(result.Errors, fmt.Sprintf("物理删除节点失败: %v", err))
    } else {
        result.DeletedNodes = int(nodeQuery.RowsAffected)
    }

    // 物理删除软删除的items
    itemQuery := s.db.Unscoped().Where("deleted_at IS NOT NULL")
    if tenantID > 0 {
        itemQuery = itemQuery.Where("tenant_id = ?", tenantID)
    }

    if err := itemQuery.Delete(&models.MetaItem{}).Error; err != nil {
        result.Errors = append(result.Errors, fmt.Sprintf("物理删除项失败: %v", err))
    } else {
        result.DeletedItems = int(itemQuery.RowsAffected)
    }

    return result, nil
}

// getInvalidEngineIDs 获取无效的engine_id列表
func (s *CleanupService) getInvalidEngineIDs(ctx context.Context, tenantID uint) []uint {
    var ids []uint

    query := `
        SELECT DISTINCT mn.engine_id
        FROM metadata.meta_node mn
        LEFT JOIN system.engines e ON mn.engine_id = e.id
        WHERE e.id IS NULL OR e.is_active = false OR e.deleted_at IS NOT NULL
    `

    if tenantID > 0 {
        query += fmt.Sprintf(" AND mn.tenant_id = %d", tenantID)
    }

    s.db.Raw(query).Scan(&ids)
    return ids
}

// writeResult 写入结果到Redis
func (s *CleanupService) writeResult(ctx context.Context, taskID string, result events.CleanupResultData) {
    resultJSON, _ := json.Marshal(result)
    s.redis.HSet(ctx, fmt.Sprintf("cleanup:results:%s", taskID), events.ModuleMeta, string(resultJSON))
}
```

#### 6.2.2 订阅Engine删除事件

```go
// meta/backend/internal/service/engine_cleanup_service.go

package service

import (
    "context"

    "addp/common/events"
    "addp/meta/backend/internal/models"
    "gorm.io/gorm"
)

type EngineCleanupService struct {
    db       *gorm.DB
    eventBus EventBus
}

// SubscribeEngineDeletedEvent 订阅Engine删除事件
func (s *EngineCleanupService) SubscribeEngineDeletedEvent(ctx context.Context) error {
    return s.eventBus.Subscribe(ctx, events.EventEngineDeleted, func(event events.EngineDeletedEvent) {
        s.handleEngineDeleted(ctx, event)
    })
}

// handleEngineDeleted 处理Engine删除事件
func (s *EngineCleanupService) handleEngineDeleted(ctx context.Context, event events.EngineDeletedEvent) {
    // 软删除关联的MetaNode
    s.db.Where("engine_id = ?", event.EngineID).Delete(&models.MetaNode{})

    // 软删除关联的MetaItem
    s.db.Where("engine_id = ?", event.EngineID).Delete(&models.MetaItem{})

    // TODO: 记录清理日志
}
```

#### 6.2.3 启动订阅

```go
// meta/backend/cmd/server/main.go

func main() {
    // ... 初始化代码 ...

    // 初始化清理服务
    cleanupService := service.NewCleanupService(db, redisClient, eventBus)
    engineCleanupService := service.NewEngineCleanupService(db, eventBus)

    // 启动事件订阅（后台goroutine）
    go func() {
        ctx := context.Background()
        if err := cleanupService.SubscribeCleanupEvents(ctx); err != nil {
            log.Fatalf("Failed to subscribe cleanup events: %v", err)
        }
    }()

    go func() {
        ctx := context.Background()
        if err := engineCleanupService.SubscribeEngineDeletedEvent(ctx); err != nil {
            log.Fatalf("Failed to subscribe engine deleted event: %v", err)
        }
    }()

    // ... 启动HTTP服务 ...
}
```

### 6.3 Manager模块实现

Manager模块的实现与Meta类似，只需实现：

1. `CleanupService` - 扫描和清理向量、MVT瓦片
2. `SubscribeCleanupEvents` - 订阅清理请求事件
3. `SubscribeEngineDeletedEvent` - 订阅Engine删除事件

关键方法：

```go
// ScanGarbage - 扫描垃圾数据
func (s *CleanupService) ScanGarbage(tenantID uint) (*models.ManagerCleanupStatistics, error) {
    stats := &models.ManagerCleanupStatistics{}

    // 1. 扫描孤儿向量记录
    // SELECT COUNT(*) FROM manager.embeddings e
    // LEFT JOIN metadata.meta_item mi ON e.meta_item_id = mi.id
    // WHERE mi.id IS NULL

    // 2. 扫描孤儿MVT瓦片
    // 检查MVT缓存目录中的文件，查询对应的meta_item是否存在

    // 3. 扫描过期缓存
    // 统计超过30天未访问的MVT瓦片

    return stats, nil
}

// ExecuteCleanup - 执行清理
func (s *CleanupService) ExecuteCleanup(tenantID uint, deleteType string) (*models.ManagerCleanupExecuteResult, error) {
    result := &models.ManagerCleanupExecuteResult{}

    // 1. 删除孤儿向量记录
    // DELETE FROM manager.embeddings WHERE meta_item_id NOT IN (SELECT id FROM metadata.meta_item)

    // 2. 删除孤儿MVT瓦片文件
    // 遍历缓存目录，删除对应meta_item不存在的瓦片

    // 3. 删除过期缓存
    // 删除超过30天未访问的MVT瓦片

    return result, nil
}
```

### 6.4 Transfer模块实现

Transfer模块主要清理任务历史记录：

```go
// ScanGarbage - 扫描垃圾数据
func (s *CleanupService) ScanGarbage(tenantID uint) (*models.TransferCleanupStatistics, error) {
    stats := &models.TransferCleanupStatistics{}

    // 1. 统计失败的任务记录
    // SELECT COUNT(*) FROM transfer.tasks WHERE status = 'failed'

    // 2. 统计超过90天的历史记录
    // SELECT COUNT(*) FROM transfer.tasks WHERE completed_at < NOW() - INTERVAL '90 days'

    // 3. 统计孤儿任务（engine_id不存在）
    // SELECT COUNT(*) FROM transfer.tasks t
    // LEFT JOIN system.engines e ON t.engine_id = e.id
    // WHERE e.id IS NULL

    return stats, nil
}
```

---

## 7. 流程图

### 7.1 扫描流程

```
┌─────────┐
│ 前端    │
└────┬────┘
     │ POST /api/system/admin/cleanup/scan
     ▼
┌─────────────────────────────┐
│ System: CreateScanTask      │
│ 1. 生成 task_id             │
│ 2. 写入 Redis task元数据    │
│ 3. 发布 cleanup.request事件 │
│ 4. 返回 task_id             │
└────┬────────────────────────┘
     │
     │ 发布事件到 Redis Stream
     │
     ├───────────┬───────────┬───────────┐
     ▼           ▼           ▼           ▼
┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐
│  Meta   │ │ Manager │ │Transfer │ │ Develop │
│  订阅   │ │  订阅   │ │  订阅   │ │  订阅   │
└────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘
     │           │           │           │
     │ ScanGarbage()        │           │
     ▼           ▼           ▼           ▼
┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐
│ 统计数据│ │ 统计数据│ │ 统计数据│ │ 统计数据│
└────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘
     │           │           │           │
     │ 写入Redis results:{task_id}      │
     └───────────┴───────────┴───────────┘
                     │
                     ▼
            ┌────────────────┐
            │ Redis: results │
            │ {task_id}      │
            └────────────────┘
                     ▲
                     │
          ┌──────────┴──────────┐
          │ 前端轮询             │
          │ GET /tasks/{task_id}│
          └─────────────────────┘
                     │
                     ▼
          ┌─────────────────────┐
          │ System: 聚合结果     │
          │ 返回统计数据         │
          └─────────────────────┘
```

### 7.2 清理执行流程

```
┌─────────┐
│ 前端    │
└────┬────┘
     │ POST /api/system/admin/cleanup/execute
     │ {based_on_scan: "task-001", delete_type: "soft_delete"}
     ▼
┌─────────────────────────────┐
│ System: CreateExecuteTask   │
│ 1. 验证扫描任务存在          │
│ 2. 生成 exec_task_id        │
│ 3. 发布 cleanup.request事件 │
│    (action=execute)         │
└────┬────────────────────────┘
     │
     │ 发布事件
     │
     ├───────────┬───────────┐
     ▼           ▼           ▼
┌─────────┐ ┌─────────┐ ┌─────────┐
│  Meta   │ │ Manager │ │Transfer │
│Execute  │ │ Execute │ │ Execute │
└────┬────┘ └────┬────┘ └────┬────┘
     │           │           │
     │ 软删除/硬删除          │
     ▼           ▼           ▼
┌─────────┐ ┌─────────┐ ┌─────────┐
│ 执行结果│ │ 执行结果│ │ 执行结果│
└────┬────┘ └────┬────┘ └────┬────┘
     │           │           │
     │ 写入Redis results     │
     └───────────┴───────────┘
                     │
                     ▼
          ┌─────────────────────┐
          │ 前端轮询执行结果     │
          │ 显示删除统计         │
          └─────────────────────┘
```

### 7.3 Engine删除自动清理流程

```
┌─────────────────┐
│ System: 删除Engine│
└────┬────────────┘
     │
     │ 发布 engine.deleted 事件
     │
     ├───────────┬───────────┬───────────┐
     ▼           ▼           ▼           ▼
┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐
│  Meta   │ │ Manager │ │Transfer │ │ Service │
│  订阅   │ │  订阅   │ │  订阅   │ │  订阅   │
└────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘
     │           │           │           │
     │ 软删除meta_node/item  │           │
     │           │ 删除向量和MVT          │
     │           │           │ 取消传输任务
     │           │           │           │
     ▼           ▼           ▼           ▼
   完成        完成        完成        完成
```

---

## 8. 错误处理和边界情况

### 8.1 超时处理

**场景**：某个模块响应慢或无响应

**处理**：
- System设置30秒超时（scan）或5分钟超时（execute）
- 超时后标记该模块为`timeout`状态
- 其他模块的结果正常返回
- 用户可选择重试或忽略

### 8.2 部分失败

**场景**：某个模块执行失败

**处理**：
- 模块写入`status: "failed"`和`error`信息
- 其他模块继续执行
- System返回`completed_with_errors`状态
- 前端显示失败详情，用户可选择重试失败的模块

### 8.3 任务过期

**场景**：用户查询1小时前的任务

**处理**：
- Redis Key设置1小时TTL
- 过期后返回`task not found`
- 用户可从历史记录查询（仅保留task_id和基本信息）

### 8.4 权限校验

**场景**：租户A尝试清理租户B的数据

**处理**：
- System在创建任务时检查权限
- 各模块执行时再次验证`tenant_id`
- 拒绝跨租户访问

### 8.5 并发清理

**场景**：多个用户同时发起清理

**处理**：
- 每个任务独立执行，不互相影响
- Redis Stream的Consumer Group保证消息不重复消费
- 各模块使用数据库事务保证原子性

### 8.6 硬删除保护

**场景**：误操作硬删除

**处理**：
- 只有SuperAdmin可以执行硬删除
- 需要二次确认（`confirm: true`）
- 支持dry_run模式预览
- 记录审计日志

---

## 9. 安全和权限控制

### 9.1 权限矩阵

| 操作 | 租户管理员 | SuperAdmin |
|------|-----------|------------|
| 扫描本租户数据 | ✅ | ✅ |
| 扫描全局数据 | ❌ | ✅ |
| 软删除本租户数据 | ✅ | ✅ |
| 软删除全局数据 | ❌ | ✅ |
| 硬删除本租户数据 | ❌ | ✅ |
| 硬删除全局数据 | ❌ | ✅ |

### 9.2 审计日志

**记录内容**：
- 谁（user_id）
- 何时（timestamp）
- 做了什么（action: scan/soft_delete/hard_delete）
- 影响范围（tenant_id, deleted_count）
- 结果（success/failed）

**存储位置**：
- `system.audit_logs` 表
- 或写入独立的日志文件

### 9.3 敏感操作保护

**硬删除保护**：
- 需要SuperAdmin权限
- 需要二次确认
- 支持dry_run预览
- 记录详细审计日志

**防误操作**：
- 扫描和执行分两步
- 执行前展示影响范围
- 提供撤销机制（软删除可恢复）

---

## 10. 测试要点

### 10.1 单元测试

**System模块**：
- `CreateScanTask` - 任务创建和事件发布
- `GetTaskStatus` - 状态聚合和超时处理
- `CreateExecuteTask` - 权限验证

**Meta模块**：
- `ScanGarbage` - 各类垃圾数据统计准确性
- `ExecuteCleanup` - 软删除和硬删除逻辑

**Manager模块**：
- `ScanGarbage` - 孤儿向量和MVT统计
- `ExecuteCleanup` - 文件删除和空间释放

### 10.2 集成测试

**端到端流程**：
1. 创建测试数据（无效Engine、孤儿Item）
2. 发起扫描任务
3. 验证各模块响应
4. 验证统计数据准确性
5. 执行清理
6. 验证数据已删除

**事件驱动测试**：
1. 删除Engine
2. 验证各模块收到事件
3. 验证关联数据已软删除

### 10.3 性能测试

**大数据量测试**：
- 10万条meta_item的扫描性能
- 1000个MVT瓦片的清理速度

**并发测试**：
- 多个用户同时发起清理
- 验证Redis Stream消息不丢失

### 10.4 异常测试

**模块宕机**：
- Meta模块宕机，验证超时机制
- 验证其他模块正常响应

**Redis故障**：
- Redis短暂不可用
- 验证错误处理和重试机制

---

## 11. 部署和运维

### 11.1 Redis配置

**Stream配置**：
```bash
# 最大长度限制（保留最近1000条）
XADD cleanup:requests MAXLEN ~ 1000 * ...

# Consumer Group创建
XGROUP CREATE cleanup:requests meta-consumer $ MKSTREAM
XGROUP CREATE cleanup:requests manager-consumer $ MKSTREAM
XGROUP CREATE cleanup:requests transfer-consumer $ MKSTREAM
```

**内存优化**：
- 任务结果设置1小时TTL
- Stream自动裁剪保留1000条
- 历史记录只保留task_id，不保存详细结果

### 11.2 监控指标

**关键指标**：
- 清理任务成功率
- 各模块响应时间
- 垃圾数据总量趋势
- 释放的存储空间

**告警规则**：
- 模块超时率 > 10%
- 清理任务失败率 > 5%
- 垃圾数据量 > 阈值

### 11.3 定时清理（可选）

**自动化策略**：
- 每周日凌晨3点自动扫描
- 垃圾数据 > 阈值时发送通知
- SuperAdmin审批后自动执行清理

**实现方式**：
```go
// System模块添加定时任务
func (s *CleanupScheduler) ScheduleWeeklyScan() {
    c := cron.New()
    c.AddFunc("0 3 * * 0", func() {
        taskID, _ := s.cleanupService.CreateScanTask(context.Background(), 0, nil, 0)
        log.Printf("Auto scan task created: %s", taskID)
    })
    c.Start()
}
```

---

## 12. 未来优化方向

### 12.1 增量清理

**当前**：全量扫描所有垃圾数据

**优化**：
- 只扫描最近变更的数据
- 基于时间戳的增量扫描
- 减少扫描时间

### 12.2 智能清理策略

**当前**：手动触发或固定阈值

**优化**：
- 根据存储空间使用率自动触发
- 根据数据访问频率智能清理
- 机器学习预测垃圾数据

### 12.3 清理预览优化

**当前**：只返回统计数据

**优化**：
- 提供详细的数据列表下载
- 支持按条件筛选（如只清理某个引擎）
- 可视化展示影响范围

### 12.4 多租户隔离增强

**当前**：通过tenant_id过滤

**优化**：
- 租户配额管理（限制最大存储）
- 租户级别的清理策略配置
- 租户清理历史独立追踪

---

## 13. 总结

本设计通过**事件驱动 + Redis聚合**的架构，实现了：

1. ✅ **完全解耦**：System不直接调用其他模块API
2. ✅ **两步清理**：扫描统计 → 确认执行
3. ✅ **异步健壮**：支持超时、部分失败、重试
4. ✅ **权限隔离**：租户管理员和SuperAdmin分级管理
5. ✅ **可追踪**：任务历史和审计日志
6. ✅ **自动化**：Engine删除自动触发软删除

**核心优势**：
- 各模块独立演进，不互相依赖
- 通过Redis作为通信和聚合中心
- 前端体验好（统一入口、实时进度）
- 运维友好（监控、告警、定时任务）

**实现路径**：
1. 先实现common层事件定义
2. 实现Meta模块作为示例
3. 实现System的任务编排
4. 扩展到Manager、Transfer等模块
5. 完善前端UI和监控
