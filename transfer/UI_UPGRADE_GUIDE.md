# Transfer 模块 UI 升级指南

## 📌 升级概述

Transfer 模块前端已从**手写 JSON 配置**升级为**可视化向导式界面**，大幅提升用户体验。

### 主要改进

| 功能 | 旧版本 | 新版本 |
|------|-------|--------|
| **任务创建** | 单页表单 + 手写 JSON | 7 步向导，分步引导 |
| **数据源选择** | 手动输入连接信息 | 从 System 模块选择已配置数据源 |
| **字段映射** | 无界面，只能写 API | 可视化编辑器，支持自动匹配 |
| **定时调度** | 手写 Cron 表达式 | 预设 + 可视化生成器 |
| **配置表单** | JSON 文本框 | 根据数据源类型动态表单 |

---

## 🎯 新版 UI 结构

### 1. 任务创建向导（7 步流程）

```
步骤 1: 基本信息
  └─ 任务名称、描述、类型（导入/导出/同步）
  └─ 执行模式（批处理/流式/微批）
  └─ 批量大小、并行度
  └─ 定时调度（带 Cron 生成器）

步骤 2: 选择源数据源
  └─ 选择连接器类型（PostgreSQL/MySQL/CSV/JSON/S3）
  └─ 从 System 模块已配置的资源中选择
  └─ 显示数据源连接信息预览

步骤 3: 配置源参数
  └─ 数据库：SQL 查询或选择表、增量字段
  └─ 文件：文件路径、编码、分隔符
  └─ 对象存储：前缀、过滤规则

步骤 4: 选择目标数据源
  └─ 与步骤 2 类似

步骤 5: 配置目标参数
  └─ 数据库：目标表、写入模式（INSERT/UPSERT/REPLACE）、冲突策略
  └─ 文件：输出路径、压缩方式
  └─ 对象存储：目标前缀、覆盖策略

步骤 6: 字段映射
  └─ 自动同名匹配
  └─ 可视化编辑器（拖拽式）
  └─ 配置字段类型、转换函数、默认值

步骤 7: 确认并创建
  └─ 预览所有配置
  └─ 选择是否立即执行
```

---

## 🚀 快速开始

### 创建第一个任务（示例：PostgreSQL → CSV）

#### 步骤 1：访问 Transfer 模块

```bash
# 方式一：通过 Portal
http://localhost:5170 → 点击"数据传输"卡片

# 方式二：直接访问
http://localhost:5176/tasks
```

#### 步骤 2：点击"创建任务"

点击页面右上角的 **"+ 新建任务"** 按钮，进入向导界面。

#### 步骤 3：填写基本信息

| 字段 | 值 | 说明 |
|------|-----|------|
| 任务名称 | 用户数据导出 | 描述性名称 |
| 任务描述 | 每日导出活跃用户到 CSV | 可选 |
| 任务类型 | **数据导出** | 选择 3 种类型之一 |
| 执行模式 | **批处理** | 推荐使用批处理 |
| 批量大小 | 1000 | 保持默认即可 |
| 最大并行度 | 4 | 根据硬件调整 |
| 定时调度 | `0 2 * * *` | 点击"生成"按钮选择预设 |

点击 **"下一步"**。

#### 步骤 4：选择源数据源

1. 选择 **数据源类型**：PostgreSQL
2. 在下拉列表中选择已配置的数据源
   - 如果没有，点击提示链接到 System 模块创建
3. 查看数据源信息预览（自动显示）

点击 **"下一步"**。

#### 步骤 5：配置源参数

1. **查询方式**：选择 "自定义 SQL"
2. **SQL 查询**：
   ```sql
   SELECT id, username, email, created_at
   FROM users
   WHERE status = 'active'
   ```
3. **增量字段**（可选）：留空（全量导出）

点击 **"下一步"**。

#### 步骤 6：选择目标数据源

1. **目标类型**：CSV 文件
2. **选择数据源**：选择 MinIO 存储

点击 **"下一步"**。

#### 步骤 7：配置目标参数

1. **输出路径**：`exports/users.csv`
2. **CSV 选项**：勾选 "包含表头"
3. **压缩**：选择 "不压缩"

点击 **"下一步"**。

#### 步骤 8：字段映射

1. 点击 **"自动匹配同名字段"** 按钮
   - 系统自动匹配 `id → id`、`username → username` 等
2. 手动调整（如需要）：
   - 修改字段类型：将 `id` 改为 `integer`
   - 添加转换函数：将 `email` 转为小写（选择 `lower`）
   - 设置默认值：为 `created_at` 设置格式 `2006-01-02`

点击 **"下一步"**。

#### 步骤 9：确认并创建

1. 检查配置摘要
2. 勾选 **"创建后立即执行"**（如果想立即测试）
3. 点击 **"创建任务"**

**完成！** 系统会跳转到任务详情页，可以查看执行进度。

---

## 📚 核心组件详解

### 1. TaskWizard.vue - 任务创建向导

**位置**：`/Users/pampa/code/addp/transfer/frontend/src/views/TaskWizard.vue`

**功能**：
- 7 步向导流程
- 步骤验证（每步完成才能下一步）
- 数据源集成（从 System 模块加载）
- 动态表单（根据连接器类型显示不同配置项）

**路由**：
- 创建任务：`/tasks/create`
- 编辑任务：`/tasks/:id/edit`

**快速创建（旧版）**：
- 如果需要快速创建简单任务，仍可使用：`/tasks/create-simple`

---

### 2. FieldMappingEditor.vue - 字段映射编辑器

**位置**：`/Users/pampa/code/addp/transfer/frontend/src/components/FieldMappingEditor.vue`

**功能**：
- ✅ **自动匹配**：一键匹配同名字段（支持模糊匹配，如 `user_id` ↔ `userId`）
- ✅ **可视化编辑**：表格式编辑，直观清晰
- ✅ **字段类型**：下拉选择（string/integer/float/boolean/date/timestamp/json）
- ✅ **转换函数**：9 种内置函数（upper/lower/trim/format_date/to_timestamp/to_int/to_float/parse_json/stringify_json）
- ✅ **默认值**：支持为空值设置默认值
- ✅ **可空设置**：开关控制是否允许 NULL

**使用示例**：

```vue
<FieldMappingEditor
  :source-fields="['id', 'name', 'email']"
  :target-fields="['user_id', 'username', 'email_address']"
  v-model:mappings="fieldMappings"
  @fetch-fields="handleFetchFields"
/>
```

**自动匹配逻辑**：
1. **完全匹配**：`email` → `email`
2. **模糊匹配**：`user_id` → `userId`（去除下划线和大小写后比较）
3. **智能提示**：选择源字段后，自动推荐目标字段

---

### 3. CronBuilderDialog.vue - Cron 表达式生成器

**位置**：`/Users/pampa/code/addp/transfer/frontend/src/components/CronBuilderDialog.vue`

**功能**：
- ✅ **13 个常用预设**（每分钟/每小时/每天/工作日/每月等）
- ✅ **自定义配置**（分/时/日/月/周独立配置）
- ✅ **手动输入**（支持直接输入 Cron 表达式）
- ✅ **实时预览**（生成表达式并显示描述）
- ✅ **一键复制**（复制到剪贴板）

**使用示例**：

```vue
<CronBuilderDialog
  v-model="showCronBuilder"
  @select="taskForm.schedule = $event"
/>
```

**预设列表**：
```
• 每分钟            * * * * *
• 每 5 分钟         */5 * * * *
• 每 15 分钟        */15 * * * *
• 每小时            0 * * * *
• 每天零点          0 0 * * *
• 每天凌晨 2 点     0 2 * * *       ← 推荐
• 工作日上午 9 点   0 9 * * 1-5
• 每周一零点        0 0 * * 1
• 每月 1 号零点     0 0 1 * *
```

---

## 🔧 技术实现细节

### 数据源集成（System 模块）

向导会在 `onMounted` 时从 System 模块加载数据源列表：

```javascript
const loadResources = async () => {
  const token = localStorage.getItem('token')
  const response = await axios.get('http://localhost:8080/api/resources', {
    headers: { Authorization: `Bearer ${token}` }
  })
  resources.value = response.data || []
}
```

**过滤逻辑**：根据选择的连接器类型过滤资源
```javascript
const filteredSourceResources = computed(() => {
  return resources.value.filter(r =>
    r.resource_type.toLowerCase().includes(sourceConnectorType.value)
  )
})
```

### 动态配置表单

根据连接器类型显示不同的配置项：

```vue
<!-- PostgreSQL/MySQL 源配置 -->
<div v-if="['postgresql', 'mysql'].includes(sourceConnectorType)">
  <el-form-item label="查询方式">
    <el-radio-group v-model="sourceConfig.queryType">
      <el-radio-button label="table">选择表</el-radio-button>
      <el-radio-button label="sql">自定义 SQL</el-radio-button>
    </el-radio-group>
  </el-form-item>
  ...
</div>

<!-- CSV/JSON 文件源配置 -->
<div v-if="['csv', 'json'].includes(sourceConnectorType)">
  ...
</div>

<!-- S3/MinIO 源配置 -->
<div v-if="sourceConnectorType === 's3'">
  ...
</div>
```

### 配置对象构建

在提交时，将表单数据转换为后端需要的 `config` JSON：

```javascript
const handleSubmit = async () => {
  const config = {
    source: sourceConfig.value,
    target: targetConfig.value
  }

  // 处理 JSON 数组字符串
  if (sourceConfig.value.parameters) {
    config.source.parameters = JSON.parse(sourceConfig.value.parameters)
  }

  const data = {
    ...taskForm.value,
    config,
    mappings: fieldMappings.value
  }

  await taskAPI.create(data)
}
```

---

## 🎨 UI 截图说明（模拟）

### 步骤 1：基本信息
```
┌──────────────────────────────────────────────┐
│  ● 基本信息  ○ 选择源  ○ 配置源  ...        │
├──────────────────────────────────────────────┤
│  任务名称：[用户数据导出                  ]  │
│  任务描述：[每日导出活跃用户...           ]  │
│                                              │
│  任务类型：◉ 数据导入  ○ 数据导出  ○ 同步   │
│                                              │
│  执行模式：◉ 批处理  ○ 流式  ○ 微批         │
│                                              │
│  批量大小：[1000        ] 记录/批            │
│  并行度：  [4           ] Worker             │
│                                              │
│  定时调度：[0 2 * * *          ] [生成]      │
│            每天凌晨 2 点执行                  │
│                                              │
│           [取消]              [下一步 →]     │
└──────────────────────────────────────────────┘
```

### 步骤 6：字段映射
```
┌──────────────────────────────────────────────┐
│  [🪄 自动匹配] [+ 添加] [🗑 清空] [🔄 刷新]  │
├──────────────────────────────────────────────┤
│ # │ 源字段   │ → │ 目标字段     │ 类型 │ 转换 │
├───┼──────────┼───┼──────────────┼──────┼──────┤
│ 1 │ id       │ → │ user_id      │ int  │ -    │
│ 2 │ username │ → │ name         │ str  │ -    │
│ 3 │ email    │ → │ email_addr   │ str  │ lower│
│ 4 │ created  │ → │ reg_date     │ date │ fmt  │
└──────────────────────────────────────────────┘
  源字段数: 4  │  目标字段数: 5  │  已映射: 4
```

---

## 📋 对比：旧版 vs 新版

### 旧版界面（TaskForm.vue）

```vue
<!-- 旧版：手写 JSON -->
<el-form-item label="配置JSON">
  <el-input
    v-model="configJson"
    type="textarea"
    :rows="15"
    placeholder='{"source": {...}, "target": {...}}'
  />
  <div class="hint">请参考文档配置数据源连接信息</div>
</el-form-item>
```

**问题**：
- ❌ 需要记住 JSON 结构
- ❌ 容易出现语法错误
- ❌ 无法预览数据源
- ❌ 无字段映射界面
- ❌ 配置门槛高

---

### 新版界面（TaskWizard.vue）

```vue
<!-- 新版：可视化表单 -->
<el-form-item label="源数据源">
  <el-select v-model="taskForm.source_id" filterable>
    <el-option
      v-for="res in resources"
      :label="res.name"
      :value="res.id"
    />
  </el-select>
</el-form-item>

<el-form-item label="查询方式">
  <el-radio-group v-model="sourceConfig.queryType">
    <el-radio-button label="table">选择表</el-radio-button>
    <el-radio-button label="sql">自定义 SQL</el-radio-button>
  </el-radio-group>
</el-form-item>
```

**优势**：
- ✅ 分步引导，逻辑清晰
- ✅ 下拉选择，无需手写
- ✅ 即时验证，减少错误
- ✅ 自动匹配，省时省力
- ✅ 零门槛使用

---

## 🔗 相关文件

### 新增文件
- `/Users/pampa/code/addp/transfer/frontend/src/views/TaskWizard.vue` - 任务创建向导
- `/Users/pampa/code/addp/transfer/frontend/src/components/FieldMappingEditor.vue` - 字段映射编辑器
- `/Users/pampa/code/addp/transfer/frontend/src/components/CronBuilderDialog.vue` - Cron 生成器

### 修改文件
- `/Users/pampa/code/addp/transfer/frontend/src/router/index.js` - 路由配置（新增 `/tasks/create` 指向 TaskWizard）

### 保留文件
- `/Users/pampa/code/addp/transfer/frontend/src/views/TaskForm.vue` - 旧版表单（作为快速创建入口保留）

---

## 🚀 部署和使用

### 1. 安装依赖

```bash
cd /Users/pampa/code/addp/transfer/frontend
npm install
```

### 2. 启动开发服务器

```bash
npm run dev
```

访问：http://localhost:5176

### 3. 构建生产版本

```bash
npm run build
```

### 4. Docker 部署

```bash
cd /Users/pampa/code/addp
docker-compose up -d transfer-frontend
```

---

## 📝 待办改进（未来版本）

### Priority 1 - 高优先级
- [ ] **字段列表获取**：从后端 API 获取真实的表字段（目前是模拟数据）
- [ ] **连接测试**：在步骤 2/4 添加"测试连接"按钮
- [ ] **进度保存**：支持保存草稿，下次继续编辑
- [ ] **预览数据**：在字段映射前预览源数据样本

### Priority 2 - 中优先级
- [ ] **模板功能**：保存常用任务配置为模板
- [ ] **批量创建**：从 CSV 批量导入任务配置
- [ ] **可视化查询**：SQL 查询构建器（拖拽式）
- [ ] **字段推荐**：基于数据类型智能推荐转换函数

### Priority 3 - 低优先级
- [ ] **国际化**：支持中英文切换
- [ ] **暗色主题**：支持暗色模式
- [ ] **快捷键**：键盘快捷键操作

---

## 💬 常见问题

### Q1: 为什么看不到数据源选项？

**A**: 需要先在 System 模块创建数据源配置。步骤：
1. 访问 http://localhost:8080（System 模块）
2. 进入 "资源管理"
3. 创建 PostgreSQL/MySQL/MinIO 等资源
4. 回到 Transfer 模块，刷新页面

### Q2: 自动匹配字段不准确怎么办？

**A**:
- 先点击"自动匹配"获得初始映射
- 手动调整不正确的映射（下拉选择正确的目标字段）
- 删除不需要的映射（点击删除按钮）
- 添加新映射（点击"添加映射"按钮）

### Q3: 如何编辑已创建的任务？

**A**:
1. 进入任务列表：http://localhost:5176/tasks
2. 点击任务行的"编辑"按钮
3. 进入相同的向导界面，修改任何步骤
4. 保存更新

### Q4: 旧版简单表单还能用吗？

**A**: 可以，访问 `/tasks/create-simple` 即可使用旧版表单（适合高级用户快速创建）

---

## 📖 更多资源

- **Transfer 模块使用指南**：[USAGE_GUIDE.md](./USAGE_GUIDE.md)
- **Transfer 模块架构文档**：[README.md](./README.md)
- **ADDP 平台总览**：[../CLAUDE.md](../CLAUDE.md)
- **System 模块文档**：[../system/CLAUDE.md](../system/CLAUDE.md)

---

**文档版本**: v1.0.0
**最后更新**: 2025-01-15
**适用版本**: Transfer Frontend v2.0.0+
