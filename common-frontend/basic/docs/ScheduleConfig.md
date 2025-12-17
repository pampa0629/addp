# ScheduleConfig 定时调度组件使用文档

## 概述

`ScheduleConfig` 是 ADDP 平台统一的定时调度配置组件，用于配置 Cron 表达式和调度规则。所有模块的定时任务配置必须使用此组件，以保证用户体验和功能一致性。

**组件位置**：`common-frontend/basic/src/components/ScheduleConfig.vue`

**工具函数**：`common-frontend/basic/src/utils/schedule.js`

---

## 核心特性

✅ **11种快捷预设**：每分钟、每5分钟、每15分钟、每30分钟、每小时、每2小时、每天多个时段、每周、每月

✅ **4种调度模式**：
- `daily`（每天）：指定具体时刻（如 09:00）
- `weekly`（每周）：指定星期几 + 时刻
- `monthly`（每月）：指定日期 + 时刻
- `cron`（自定义）：手动输入标准 Cron 表达式

✅ **实时预览**：显示中文调度描述（如"每周一 09:00 执行"）

✅ **在线工具**：集成 crontab.guru 链接，帮助用户理解和验证 Cron 表达式

✅ **格式验证**：自动校验 Cron 表达式合法性

---

## 字段命名规范（重要！）

### 统一字段名：`schedule`

所有模块必须统一使用 `schedule` 作为定时调度字段名，禁止使用其他命名：

| ✅ 正确 | ❌ 错误 |
|--------|--------|
| `schedule` | `cron_expr` |
| | `cronExpression` |
| | `scheduleCron` |
| | `cron_expression` |

### 前端字段定义

```javascript
// 表单数据结构
const form = ref({
  name: '',
  description: '',
  schedule: '',  // ✅ 统一使用 schedule
  // ... 其他字段
})
```

### 后端字段定义

```go
// Go 结构体
type Task struct {
    ID          uint      `json:"id"`
    Name        string    `json:"name"`
    Schedule    string    `json:"schedule" gorm:"column:schedule"`  // ✅ 统一使用 schedule
    // ... 其他字段
}
```

```sql
-- 数据库表结构
CREATE TABLE tasks (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255),
    schedule VARCHAR(255),  -- ✅ 统一使用 schedule
    -- ... 其他字段
);
```

---

## 数据格式

### Cron 表达式格式

使用标准 5 字段 Cron 格式（分钟级精度）：

```
分 时 日 月 周
* * * * *
```

**字段说明**：
- **分**：0-59
- **时**：0-23
- **日**：1-31
- **月**：1-12
- **周**：0-7（0 和 7 都表示周日）

**示例**：
```javascript
'0 9 * * 1'        // 每周一 09:00
'*/5 * * * *'      // 每 5 分钟
'0 0 1 * *'        // 每月 1 号零点
'30 14 * * 1-5'    // 每个工作日 14:30
```

---

## 基础用法

### 1. 安装依赖（各模块 package.json）

确保 `package.json` 中包含依赖：

```json
{
  "dependencies": {
    "element-plus": "^2.4.2",
    "cronstrue": "^2.31.0"
  }
}
```

### 2. 配置别名（vite.config.js）

```javascript
import { defineConfig } from 'vite'
import { resolve } from 'path'

export default defineConfig({
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
    }
  }
})
```

### 3. 导入组件

```javascript
import { ScheduleConfig } from '@common-ui'
import { describeCron } from '@common-ui'
```

### 4. 基础示例

```vue
<template>
  <el-form :model="form">
    <el-form-item label="定时调度">
      <ScheduleConfig v-model="form.schedule" />
    </el-form-item>
  </el-form>
</template>

<script setup>
import { ref } from 'vue'
import { ScheduleConfig } from '@common-ui'

const form = ref({
  schedule: ''  // Cron 表达式字符串
})
</script>
```

---

## 使用模式

### 模式 1：表单嵌入模式（推荐）

**适用场景**：任务创建/编辑表单

**示例**：Transfer、Orchestrator、Develop 模块

```vue
<template>
  <el-dialog title="创建任务" v-model="dialogVisible" width="600px">
    <el-form :model="form" label-width="120px">
      <el-form-item label="任务名称" required>
        <el-input v-model="form.name" />
      </el-form-item>

      <el-form-item label="任务描述">
        <el-input type="textarea" v-model="form.description" />
      </el-form-item>

      <el-form-item label="定时调度">
        <ScheduleConfig
          v-model="form.schedule"
          :allow-custom-cron="true"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="dialogVisible = false">取消</el-button>
      <el-button type="primary" @click="handleSubmit">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import { ScheduleConfig } from '@common-ui'

const dialogVisible = ref(false)
const form = ref({
  name: '',
  description: '',
  schedule: ''  // ✅ 统一字段名
})

const handleSubmit = () => {
  // 提交时 form.schedule 包含 Cron 表达式
  console.log('提交数据：', form.value)
}
</script>
```

---

### 模式 2：独立对话框模式

**适用场景**：为已存在的资源配置定时规则

**示例**：Meta 模块（资源级别的定时扫描配置）

```vue
<template>
  <!-- 主界面：资源列表 -->
  <el-table :data="resources">
    <el-table-column label="资源名称" prop="name" />
    <el-table-column label="定时计划">
      <template #default="{ row }">
        <div v-if="row.schedule">
          <el-tag type="success">{{ describeCron(row.schedule) }}</el-tag>
        </div>
        <el-tag v-else type="info">未设置</el-tag>
      </template>
    </el-table-column>
    <el-table-column label="操作">
      <template #default="{ row }">
        <el-button size="small" @click="openScheduleDialog(row)">
          设置定时
        </el-button>
      </template>
    </el-table-column>
  </el-table>

  <!-- 定时调度配置对话框 -->
  <el-dialog
    title="定时扫描设置"
    v-model="scheduleDialogVisible"
    width="600px"
  >
    <el-form label-width="120px">
      <el-form-item label="是否启用">
        <el-switch v-model="scheduleEnabled" />
      </el-form-item>

      <el-form-item label="调度配置" v-if="scheduleEnabled">
        <ScheduleConfig
          v-model="currentSchedule"
          :allow-custom-cron="true"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="scheduleDialogVisible = false">取消</el-button>
      <el-button type="primary" @click="saveSchedule">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import { ScheduleConfig, describeCron } from '@common-ui'

const resources = ref([])
const scheduleDialogVisible = ref(false)
const scheduleEnabled = ref(false)
const currentSchedule = ref('')
const currentResource = ref(null)

const openScheduleDialog = (resource) => {
  currentResource.value = resource
  currentSchedule.value = resource.schedule || ''
  scheduleEnabled.value = !!resource.schedule
  scheduleDialogVisible.value = true
}

const saveSchedule = () => {
  // 保存调度配置到资源
  const updateData = {
    schedule: scheduleEnabled.value ? currentSchedule.value : null
  }
  // 调用 API 更新资源配置
  scheduleDialogVisible.value = false
}
</script>
```

---

## 组件属性（Props）

### 基础属性

| 属性名 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `modelValue` | `String` | `''` | Cron 表达式（v-model 绑定） |
| `allowCustomCron` | `Boolean` | `false` | 是否允许自定义 Cron 表达式 |
| `showPresets` | `Boolean` | `true` | 是否显示快捷预设按钮 |
| `presetList` | `Array` | 默认11个 | 自定义预设列表 |
| `compactMode` | `Boolean` | `false` | 紧凑模式（减少间距） |
| `disabled` | `Boolean` | `false` | 是否禁用 |
| `readonly` | `Boolean` | `false` | 是否只读 |

### 高级属性示例

```vue
<!-- 自定义预设列表 -->
<ScheduleConfig
  v-model="schedule"
  :preset-list="[
    { label: '每小时', value: '0 * * * *' },
    { label: '每天9点', value: '0 9 * * *' }
  ]"
/>

<!-- 紧凑模式 -->
<ScheduleConfig
  v-model="schedule"
  :compact-mode="true"
  :show-presets="false"
/>

<!-- 只读模式（用于详情展示） -->
<ScheduleConfig
  v-model="schedule"
  :readonly="true"
/>
```

---

## 工具函数

### 从 `@common-ui` 导入

```javascript
import {
  describeCron,           // 生成中文描述
  validateCron,           // 验证 Cron 格式
  buildScheduleFromForm,  // 从表单构建 Cron
  decodeScheduleToForm,   // 解析 Cron 到表单
} from '@common-ui'
```

### 1. describeCron(cronExpr)

生成 Cron 表达式的中文描述。

```javascript
import { describeCron } from '@common-ui'

describeCron('0 9 * * 1')        // "每周一 09:00 执行"
describeCron('*/5 * * * *')      // "每 5 分钟执行"
describeCron('0 0 1 * *')        // "每月 1 号 00:00 执行"
describeCron('invalid')          // "无效的 Cron 表达式"
```

**表格中显示调度描述**：

```vue
<el-table :data="tasks">
  <el-table-column label="调度配置">
    <template #default="{ row }">
      <el-tag v-if="row.schedule" type="success">
        {{ describeCron(row.schedule) }}
      </el-tag>
      <el-tag v-else type="info">手动触发</el-tag>
    </template>
  </el-table-column>
</el-table>
```

### 2. validateCron(cronExpr)

验证 Cron 表达式是否合法。

```javascript
import { validateCron } from '@common-ui'

validateCron('0 9 * * 1')    // true
validateCron('invalid')      // false
```

### 3. buildScheduleFromForm(formData)

从表单数据构建 Cron 表达式（内部使用，一般无需手动调用）。

```javascript
buildScheduleFromForm({
  mode: 'daily',
  time: '09:00'
})
// 返回: '0 9 * * *'
```

### 4. decodeScheduleToForm(cronExpr)

解析 Cron 表达式到表单数据（内部使用）。

```javascript
decodeScheduleToForm('0 9 * * 1')
// 返回: { mode: 'weekly', time: '09:00', weekDays: [1] }
```

---

## 完整示例

### Transfer 模块：数据传输任务

**文件**：`transfer/frontend/src/views/TaskForm.vue`

```vue
<template>
  <el-dialog title="创建传输任务" v-model="dialogVisible" width="800px">
    <el-form :model="form" label-width="120px">
      <el-form-item label="任务名称" required>
        <el-input v-model="form.name" placeholder="请输入任务名称" />
      </el-form-item>

      <el-form-item label="源数据库">
        <el-select v-model="form.source_resource_id" placeholder="选择源">
          <el-option
            v-for="res in resources"
            :key="res.id"
            :label="res.name"
            :value="res.id"
          />
        </el-select>
      </el-form-item>

      <el-form-item label="目标数据库">
        <el-select v-model="form.target_resource_id" placeholder="选择目标">
          <el-option
            v-for="res in resources"
            :key="res.id"
            :label="res.name"
            :value="res.id"
          />
        </el-select>
      </el-form-item>

      <el-form-item label="定时调度">
        <ScheduleConfig
          v-model="form.schedule"
          :allow-custom-cron="true"
        />
        <div style="color: #909399; font-size: 12px; margin-top: 5px">
          留空表示手动触发，设置后将按计划自动执行
        </div>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="dialogVisible = false">取消</el-button>
      <el-button type="primary" @click="handleSubmit">创建任务</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { ScheduleConfig } from '@common-ui'
import { createTask, listResources } from '@/api/transfer'

const dialogVisible = ref(false)
const resources = ref([])

const form = ref({
  name: '',
  source_resource_id: null,
  target_resource_id: null,
  schedule: ''  // ✅ 统一字段名
})

onMounted(async () => {
  const res = await listResources()
  resources.value = res.data
})

const handleSubmit = async () => {
  try {
    await createTask(form.value)
    ElMessage.success('任务创建成功')
    dialogVisible.value = false
  } catch (error) {
    ElMessage.error('创建失败：' + error.message)
  }
}
</script>
```

---

### Orchestrator 模块：编排任务

**文件**：`orchestrator/frontend/src/views/OrchestrationForm.vue`

```vue
<template>
  <el-form :model="form" label-width="120px">
    <el-form-item label="编排名称" required>
      <el-input v-model="form.name" />
    </el-form-item>

    <el-form-item label="定时调度">
      <ScheduleConfig
        v-model="form.schedule"
        :allow-custom-cron="true"
      />
    </el-form-item>

    <el-form-item label="DAG 配置">
      <!-- DAG 编辑器 -->
    </el-form-item>
  </el-form>
</template>

<script setup>
import { ref } from 'vue'
import { ScheduleConfig } from '@common-ui'

const form = ref({
  name: '',
  schedule: '',  // ✅ 统一字段名
  dag_config: {}
})
</script>
```

---

### Meta 模块：资源定时扫描

**文件**：`meta/frontend/src/views/MetadataScan.vue`

```vue
<template>
  <el-table :data="resources">
    <el-table-column label="资源名称" prop="name" />

    <el-table-column label="定时计划" width="200">
      <template #default="{ row }">
        <div v-if="row.schedule">
          <el-icon><Clock /></el-icon>
          <span style="margin-left: 5px">{{ describeCron(row.schedule) }}</span>
        </div>
        <el-tag v-else type="info">未设置</el-tag>
      </template>
    </el-table-column>

    <el-table-column label="操作" width="150">
      <template #default="{ row }">
        <el-button
          size="small"
          type="primary"
          @click="openScheduleDialog(row)"
        >
          设置定时
        </el-button>
      </template>
    </el-table-column>
  </el-table>

  <!-- 定时扫描配置对话框 -->
  <el-dialog
    title="定时扫描设置"
    v-model="scheduleDialogVisible"
    width="600px"
  >
    <el-form label-width="120px">
      <el-form-item label="资源">
        <el-input :value="currentResource?.name" disabled />
      </el-form-item>

      <el-form-item label="启用定时扫描">
        <el-switch v-model="scheduleEnabled" />
      </el-form-item>

      <el-form-item label="调度配置" v-if="scheduleEnabled">
        <ScheduleConfig
          v-model="currentSchedule"
          :allow-custom-cron="true"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="scheduleDialogVisible = false">取消</el-button>
      <el-button type="primary" @click="saveSchedule">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { Clock } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { ScheduleConfig, describeCron } from '@common-ui'
import { listResources, updateResourceSchedule } from '@/api/meta'

const resources = ref([])
const scheduleDialogVisible = ref(false)
const scheduleEnabled = ref(false)
const currentSchedule = ref('')
const currentResource = ref(null)

onMounted(async () => {
  const res = await listResources()
  resources.value = res.data
})

const openScheduleDialog = (resource) => {
  currentResource.value = resource
  currentSchedule.value = resource.schedule || ''
  scheduleEnabled.value = !!resource.schedule
  scheduleDialogVisible.value = true
}

const saveSchedule = async () => {
  try {
    await updateResourceSchedule(currentResource.value.id, {
      schedule: scheduleEnabled.value ? currentSchedule.value : null
    })

    // 更新本地数据
    currentResource.value.schedule = scheduleEnabled.value ? currentSchedule.value : null

    ElMessage.success('定时扫描配置已保存')
    scheduleDialogVisible.value = false
  } catch (error) {
    ElMessage.error('保存失败：' + error.message)
  }
}
</script>
```

---

## 后端集成

### Go 后端示例

```go
package models

import "time"

type Task struct {
    ID          uint      `json:"id" gorm:"primaryKey"`
    Name        string    `json:"name" gorm:"not null"`
    Description string    `json:"description"`
    Schedule    string    `json:"schedule" gorm:"column:schedule"`  // ✅ 统一字段名
    Enabled     bool      `json:"enabled" gorm:"default:true"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}

// 表名
func (Task) TableName() string {
    return "tasks"
}
```

### API 接口示例

```go
// POST /api/tasks - 创建任务
type CreateTaskRequest struct {
    Name        string `json:"name" binding:"required"`
    Description string `json:"description"`
    Schedule    string `json:"schedule"`  // ✅ 统一字段名，可选
}

func (h *TaskHandler) Create(c *gin.Context) {
    var req CreateTaskRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    // 如果提供了 schedule，验证 Cron 表达式
    if req.Schedule != "" {
        if !validateCronExpr(req.Schedule) {
            c.JSON(400, gin.H{"error": "Invalid cron expression"})
            return
        }
    }

    task := &models.Task{
        Name:        req.Name,
        Description: req.Description,
        Schedule:    req.Schedule,  // ✅ 统一字段名
    }

    if err := h.service.Create(task); err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    // 如果有调度配置，注册到调度器
    if task.Schedule != "" {
        h.scheduler.Schedule(task.ID, task.Schedule, func() {
            // 执行任务
        })
    }

    c.JSON(201, task)
}
```

### 调度器集成（使用 common/scheduler）

```go
import (
    commonScheduler "github.com/addp/common/scheduler"
)

type TaskService struct {
    repo      *TaskRepository
    scheduler *commonScheduler.Scheduler
}

func (s *TaskService) Create(task *models.Task) error {
    // 保存到数据库
    if err := s.repo.Create(task); err != nil {
        return err
    }

    // 如果有调度配置，注册定时任务
    if task.Schedule != "" && task.Enabled {
        taskID := fmt.Sprintf("task_%d", task.ID)
        s.scheduler.Schedule(taskID, task.Schedule, func() {
            s.ExecuteTask(task.ID)
        })
    }

    return nil
}

func (s *TaskService) Update(id uint, updates map[string]interface{}) error {
    task, err := s.repo.GetByID(id)
    if err != nil {
        return err
    }

    // 更新数据库
    if err := s.repo.Update(id, updates); err != nil {
        return err
    }

    // 重新调度
    taskID := fmt.Sprintf("task_%d", id)
    s.scheduler.Unschedule(taskID)

    if schedule, ok := updates["schedule"].(string); ok && schedule != "" {
        s.scheduler.Schedule(taskID, schedule, func() {
            s.ExecuteTask(id)
        })
    }

    return nil
}
```

---

## 表格展示最佳实践

### 调度信息列

```vue
<el-table-column label="调度配置" width="200">
  <template #default="{ row }">
    <div v-if="row.schedule">
      <el-icon><Clock /></el-icon>
      <span style="margin-left: 5px">{{ describeCron(row.schedule) }}</span>
    </div>
    <el-tag v-else type="info">手动触发</el-tag>
  </template>
</el-table-column>
```

### 下次执行时间列

```vue
<el-table-column label="下次执行" width="180">
  <template #default="{ row }">
    <span v-if="row.next_run_time">
      {{ formatDateTime(row.next_run_time) }}
    </span>
    <span v-else style="color: #909399">-</span>
  </template>
</el-table-column>
```

### 启用状态列

```vue
<el-table-column label="状态" width="100">
  <template #default="{ row }">
    <el-tag v-if="row.enabled && row.schedule" type="success">运行中</el-tag>
    <el-tag v-else-if="row.schedule" type="warning">已暂停</el-tag>
    <el-tag v-else type="info">手动</el-tag>
  </template>
</el-table-column>
```

---

## 样式定制

### 自定义样式示例

```vue
<style scoped>
/* 调整快捷按钮间距 */
:deep(.schedule-presets) {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
}

/* 紧凑模式 */
:deep(.schedule-config.compact) {
  .el-form-item {
    margin-bottom: 12px;
  }
}

/* 只读模式 */
:deep(.schedule-config.readonly) {
  .el-input__inner {
    background-color: #f5f7fa;
    cursor: not-allowed;
  }
}
</style>
```

---

## 常见问题

### 1. 组件不显示或报错？

**检查清单**：
- ✅ 是否配置了 `@common-ui` 别名？
- ✅ 是否安装了 `cronstrue` 依赖？
- ✅ 是否正确导入组件？

```javascript
// ❌ 错误
import ScheduleConfig from '@common-ui/components/ScheduleConfig.vue'

// ✅ 正确
import { ScheduleConfig } from '@common-ui'
```

### 2. 中文描述显示为英文？

确保 `cronstrue` 使用了中文语言包：

```javascript
import cronstrue from 'cronstrue/i18n'

cronstrue.toString('0 9 * * 1', { locale: 'zh_CN' })
```

组件内部已处理，无需手动配置。

### 3. 自定义 Cron 不生效？

确保启用了 `allow-custom-cron` 属性：

```vue
<ScheduleConfig
  v-model="form.schedule"
  :allow-custom-cron="true"  <!-- 必须显式启用 -->
/>
```

### 4. 后端收到的 Cron 表达式格式不对？

检查前后端字段名是否统一为 `schedule`：

```javascript
// ❌ 错误
const form = {
  cron_expr: '0 9 * * 1'
}

// ✅ 正确
const form = {
  schedule: '0 9 * * 1'
}
```

### 5. 如何禁用某些预设选项？

使用 `preset-list` 属性自定义预设：

```vue
<ScheduleConfig
  v-model="schedule"
  :preset-list="[
    { label: '每小时', value: '0 * * * *' },
    { label: '每天9点', value: '0 9 * * *' }
  ]"
/>
```

---

## 迁移指南

### 从旧字段名迁移

如果你的模块使用了其他字段名，需要全局替换：

**步骤 1**：全局搜索替换

```bash
# Manager 模块
cd manager/frontend
grep -r "cronExpression" src/
# 替换为 schedule

# Orchestrator 模块
cd orchestrator/frontend
grep -r "cron_expr" src/
# 替换为 schedule

# Meta 模块
cd meta/frontend
grep -r "scheduleCron" src/
# 替换为 schedule
```

**步骤 2**：更新前端代码

```javascript
// ❌ 旧代码
const form = ref({
  cron_expr: ''
})

// ✅ 新代码
const form = ref({
  schedule: ''
})
```

**步骤 3**：更新后端代码

```go
// ❌ 旧代码
type Task struct {
    CronExpr string `json:"cron_expr" gorm:"column:cron_expr"`
}

// ✅ 新代码
type Task struct {
    Schedule string `json:"schedule" gorm:"column:schedule"`
}
```

**步骤 4**：更新数据库字段（可选）

```sql
-- 如果数据库字段名不同，可以通过 GORM 标签映射，无需修改数据库
-- 但建议统一修改数据库字段名

ALTER TABLE tasks RENAME COLUMN cron_expr TO schedule;
```

---

## 开发规范

### 必须遵守的规范

1. ✅ **字段名统一**：所有模块必须使用 `schedule` 字段名
2. ✅ **格式统一**：使用标准 5 字段 Cron 格式
3. ✅ **导入统一**：从 `@common-ui` 导入组件和工具函数
4. ✅ **描述统一**：使用 `describeCron()` 显示中文描述

### 推荐的实践

1. 📌 表单验证：在提交前验证 `schedule` 非空（如果是必填）
2. 📌 空值处理：`schedule` 为空字符串或 `null` 表示手动触发
3. 📌 预览功能：在表单中实时显示调度描述
4. 📌 表格展示：使用 `describeCron()` 和图标增强可读性
5. 📌 错误提示：后端验证失败时返回友好的错误信息

---

## 更新日志

### v1.0.0 (2025-12-16)

- ✨ 初始版本发布
- ✨ 统一字段名为 `schedule`
- ✨ 支持 11 种快捷预设
- ✨ 支持 4 种调度模式（daily/weekly/monthly/cron）
- ✨ 提供完整的工具函数库
- ✨ 增强组件功能（预设定制、紧凑模式、只读模式）
- 📚 完善使用文档和示例代码

---

## 相关资源

- **组件源码**：`common-frontend/basic/src/components/ScheduleConfig.vue`
- **工具函数**：`common-frontend/basic/src/utils/schedule.js`
- **Cron 语法参考**：[crontab.guru](https://crontab.guru/)
- **Cronstrue 文档**：[GitHub - bradymholt/cRonstrue](https://github.com/bradymholt/cRonstrue)
- **后端调度器**：`common/scheduler/scheduler.go`

---

## 技术支持

如有问题或建议，请联系：
- 📧 开发团队
- 💬 项目 Issues
- 📖 查阅 `common-frontend/README.md`

---

**最后更新**：2025-12-16

**维护者**：ADDP 平台开发团队
