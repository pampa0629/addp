# Transfer 前端快速开始

## 🎯 新版 UI 核心功能

### ✅ 已实现的改进

| 功能 | 描述 | 文件 |
|------|------|------|
| **任务创建向导** | 7步可视化流程，无需手写JSON | [TaskWizard.vue](src/views/TaskWizard.vue) |
| **数据源选择器** | 从System模块选择已配置资源 | TaskWizard.vue (内置) |
| **字段映射编辑器** | 自动同名匹配 + 手动调整 | [FieldMappingEditor.vue](src/components/FieldMappingEditor.vue) |
| **Cron表达式生成器** | 13个预设 + 自定义配置 | [CronBuilderDialog.vue](src/components/CronBuilderDialog.vue) |
| **类型化配置表单** | PostgreSQL/MySQL/CSV/S3 专属表单 | TaskWizard.vue (动态表单) |

---

## 🚀 3 分钟创建第一个任务

### 步骤 1: 启动服务

```bash
cd /Users/pampa/code/addp
./scripts/dev/start.sh
```

访问：http://localhost:5176/tasks

### 步骤 2: 点击"新建任务"

### 步骤 3: 按向导操作

```
① 基本信息
  └─ 填写名称、选择类型、设置调度

② 选择源
  └─ 选择PostgreSQL → 从下拉框选择已配置的数据库

③ 配置源
  └─ 输入SQL查询：SELECT * FROM users

④ 选择目标
  └─ 选择CSV → 从下拉框选择MinIO存储

⑤ 配置目标
  └─ 输入路径：exports/users.csv

⑥ 字段映射
  └─ 点击"自动匹配" → 检查结果 → 调整（如需要）

⑦ 确认
  └─ 勾选"创建后立即执行" → 点击"创建任务"
```

---

## 📦 核心组件

### TaskWizard.vue - 向导式创建

**用法**：
- 创建：访问 `/tasks/create`
- 编辑：访问 `/tasks/:id/edit`

**特点**：
- ✅ 7步分步引导
- ✅ 每步验证，减少错误
- ✅ 从System模块加载数据源
- ✅ 动态表单（根据连接器类型）

---

### FieldMappingEditor.vue - 字段映射

**用法**：
```vue
<FieldMappingEditor
  :source-fields="['id', 'name', 'email']"
  :target-fields="['user_id', 'username', 'email_address']"
  v-model:mappings="fieldMappings"
/>
```

**功能**：
- ✅ 自动同名匹配（含模糊匹配）
- ✅ 9种转换函数（upper/lower/trim/format_date/...）
- ✅ 字段类型、默认值、可空配置

**自动匹配示例**：
```
源字段        目标字段
id        →  id           (完全匹配)
user_id   →  userId       (模糊匹配，去除_和大小写)
email     →  email_addr   (需手动调整)
```

---

### CronBuilderDialog.vue - 定时调度

**用法**：
```vue
<CronBuilderDialog
  v-model="showDialog"
  @select="schedule = $event"
/>
```

**预设列表**：
```
• 每天零点        0 0 * * *
• 每天凌晨2点     0 2 * * *       ← 推荐
• 每小时          0 * * * *
• 每15分钟        */15 * * * *
• 工作日上午9点   0 9 * * 1-5
```

---

## 🔧 开发

### 安装依赖

```bash
npm install
```

### 运行开发服务器

```bash
npm run dev
```

### 构建生产版本

```bash
npm run build
```

---

## 📁 文件结构

```
src/
├── views/
│   ├── TaskWizard.vue         # 新：向导式创建
│   ├── TaskForm.vue           # 旧：简单表单（保留）
│   ├── TaskList.vue
│   ├── TaskDetail.vue
│   ├── ExecutionList.vue
│   └── ExecutionDetail.vue
├── components/
│   ├── FieldMappingEditor.vue # 新：字段映射
│   └── CronBuilderDialog.vue  # 新：Cron生成器
├── api/
│   └── tasks.js
├── router/
│   └── index.js               # 更新：/tasks/create → TaskWizard
└── store/
    └── auth.js
```

---

## 🎨 UI对比

### 旧版（TaskForm.vue）
```
[配置JSON] ← 15行文本框，需手写JSON
```

### 新版（TaskWizard.vue）
```
[选择数据源] ← 下拉框，从System模块加载
[查询方式]   ← 单选按钮（表/SQL）
[SQL查询]    ← 语法高亮文本框
[增量字段]   ← 输入框 + 提示
```

---

## 📖 文档

- **UI升级指南**: [UI_UPGRADE_GUIDE.md](../UI_UPGRADE_GUIDE.md)
- **使用指南**: [USAGE_GUIDE.md](../USAGE_GUIDE.md)
- **模块文档**: [README.md](../README.md)

---

## ✅ 下一步

1. ✅ 启动服务：`./scripts/dev/start.sh`
2. ✅ 访问界面：http://localhost:5176
3. ✅ 创建任务：点击"新建任务"
4. ✅ 查看文档：阅读 [UI_UPGRADE_GUIDE.md](../UI_UPGRADE_GUIDE.md)

**快速测试**：
```bash
# 1. 确保System模块有数据源
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/resources

# 2. 访问Transfer前端
open http://localhost:5176/tasks

# 3. 点击"新建任务"，体验向导流程
```

---

**版本**: v2.0.0 | **更新**: 2025-01-15
