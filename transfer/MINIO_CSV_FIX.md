# Transfer 模块 MinIO 和文件导出功能修复

## 📋 问题总结

用户报告了两个问题：

### 问题 1：MinIO 存储引擎不显示
**现象**：在数据导出时，选择目标类型为 "S3/MinIO"，下拉框显示"无数据"，无法选择已注册的 MinIO 存储引擎。

**根本原因**：
- 数据库中 MinIO 的 `resource_type` 是 `"minio"`
- 前端过滤逻辑使用 `includes("s3")` 来匹配
- `"minio".includes("s3")` 返回 `false`，导致过滤失败

### 问题 2：CSV/JSON 文件输出路径不清晰
**现象**：选择 CSV 或 JSON 文件作为目标类型时，不清楚文件存储位置（本地？对象存储？）

**根本原因**：
- UI 设计不够清晰
- 没有明确说明 CSV/JSON 需要存储到对象存储
- 缺少输出格式的选择界面

---

## ✅ 解决方案

### 修复 1：改进资源过滤逻辑

**文件**：`transfer/frontend/src/views/TaskWizard.vue`

**位置**：第 507-536 行

**修复前**：
```javascript
const filteredTargetResources = computed(() => {
  return resources.value.filter(r =>
    r.resource_type.toLowerCase().includes(targetConnectorType.value.toLowerCase())
  )
})
```

**修复后**：
```javascript
const filteredTargetResources = computed(() => {
  const type = targetConnectorType.value.toLowerCase()
  return resources.value.filter(r => {
    const resourceType = r.resource_type.toLowerCase()

    // 对象存储类型的特殊处理：s3, minio, oss 都归为对象存储
    if (type === 's3') {
      return ['s3', 'minio', 'oss'].includes(resourceType)
    }

    // 其他类型直接匹配
    return resourceType.includes(type)
  })
})
```

**效果**：
- ✅ 选择 "S3/MinIO" 时，可以匹配 `minio`、`s3`、`oss` 类型的资源
- ✅ 支持多种对象存储类型
- ✅ MinIO 资源正常显示在下拉列表中

---

### 修复 2：简化目标类型选择

**文件**：`transfer/frontend/src/views/TaskWizard.vue`

**位置**：第 259-268 行

**修复前**：
```vue
<el-form-item label="目标类型">
  <el-radio-group v-model="targetConnectorType">
    <el-radio-button label="postgresql">PostgreSQL</el-radio-button>
    <el-radio-button label="mysql">MySQL</el-radio-button>
    <el-radio-button label="csv">CSV 文件</el-radio-button>      <!-- 不清晰 -->
    <el-radio-button label="json">JSON 文件</el-radio-button>    <!-- 不清晰 -->
    <el-radio-button label="s3">S3/MinIO</el-radio-button>
  </el-radio-group>
</el-form-item>
```

**修复后**：
```vue
<el-form-item label="目标类型">
  <el-radio-group v-model="targetConnectorType">
    <el-radio-button label="postgresql">PostgreSQL</el-radio-button>
    <el-radio-button label="mysql">MySQL</el-radio-button>
    <el-radio-button label="s3">对象存储（MinIO/S3）</el-radio-button>
  </el-radio-group>
  <div class="hint">
    <p>• 数据库：直接写入到数据库表</p>
    <p>• 对象存储：可以导出为 CSV、JSON、Parquet 等文件格式</p>
  </div>
</el-form-item>
```

**效果**：
- ✅ 移除了容易混淆的 CSV/JSON 选项
- ✅ 明确说明对象存储可以导出多种格式
- ✅ 用户理解更清晰

---

### 修复 3：新增输出格式配置

**文件**：`transfer/frontend/src/views/TaskWizard.vue`

**位置**：第 354-397 行

**新增功能**：
```vue
<!-- S3/MinIO 目标配置 -->
<div v-if="targetConnectorType === 's3'">
  <el-form label-width="140px">
    <!-- 输出格式选择 -->
    <el-form-item label="输出格式">
      <el-radio-group v-model="targetConfig.format">
        <el-radio-button label="csv">CSV</el-radio-button>
        <el-radio-button label="json">JSON</el-radio-button>
        <el-radio-button label="jsonl">JSONL</el-radio-button>
        <el-radio-button label="parquet">Parquet</el-radio-button>
      </el-radio-group>
      <div class="hint">
        选择文件的输出格式。CSV 适合表格数据，JSON 适合嵌套结构，Parquet 适合大数据分析。
      </div>
    </el-form-item>

    <!-- 输出路径 -->
    <el-form-item label="输出路径">
      <el-input v-model="targetConfig.path"
        :placeholder="`输入文件路径，如：exports/users.${targetConfig.format || 'csv'}`" />
      <div class="hint">
        文件将存储在对象存储的指定路径。建议使用有意义的目录结构。
      </div>
    </el-form-item>

    <!-- CSV 专属选项 -->
    <el-form-item v-if="targetConfig.format === 'csv'" label="CSV 选项">
      <el-checkbox v-model="targetConfig.headers">包含表头</el-checkbox>
      <el-input v-model="targetConfig.delimiter" placeholder="分隔符"
        style="width: 100px; margin-left: 10px" />
      <div class="hint">分隔符默认为逗号 (,)</div>
    </el-form-item>

    <!-- 压缩方式 -->
    <el-form-item label="压缩方式">
      <el-select v-model="targetConfig.compression">
        <el-option label="不压缩" value="none" />
        <el-option label="Gzip" value="gzip" />
        <el-option label="Zip" value="zip" />
      </el-select>
      <div class="hint">压缩可以减少存储空间</div>
    </el-form-item>

    <!-- 覆盖选项 -->
    <el-form-item label="覆盖已有文件">
      <el-switch v-model="targetConfig.overwrite" />
      <div class="hint">如果文件已存在，是否覆盖</div>
    </el-form-item>
  </el-form>
</div>
```

**效果**：
- ✅ 支持 4 种输出格式（CSV、JSON、JSONL、Parquet）
- ✅ 动态显示格式特定的配置选项
- ✅ 清晰的路径输入提示
- ✅ 完整的压缩和覆盖选项

---

### 修复 4：默认值初始化

**文件**：`transfer/frontend/src/views/TaskWizard.vue`

**位置**：第 579-595 行

**新增逻辑**：
```javascript
const handleTargetTypeChange = () => {
  taskForm.value.target_id = null
  selectedTargetResource.value = null
  targetConfig.value = {}

  // 为对象存储设置默认配置
  if (targetConnectorType.value === 's3') {
    targetConfig.value = {
      format: 'csv',      // 默认 CSV 格式
      headers: true,      // 默认包含表头
      delimiter: ',',     // 默认逗号分隔
      compression: 'none',// 默认不压缩
      overwrite: false    // 默认不覆盖
    }
  }
}
```

**效果**：
- ✅ 用户切换到对象存储时，自动设置合理的默认值
- ✅ 减少用户配置工作量

---

## 🎯 使用流程（修复后）

### 示例：导出数据到 MinIO 的 CSV 文件

```
步骤 1: 基本信息
  └─ 任务名称：用户数据导出
  └─ 任务类型：数据导出
  └─ 执行模式：批处理

步骤 2: 选择源
  └─ 数据源类型：PostgreSQL
  └─ 选择数据源：pg业务库

步骤 3: 配置源
  └─ 查询方式：选择表
  └─ 表名：customers

步骤 4: 选择目标
  └─ 目标类型：对象存储（MinIO/S3）  ← 简化了选项
  └─ 选择数据源：min存储 (minio)     ← ✅ 现在能正常显示

步骤 5: 配置目标
  └─ 输出格式：CSV                    ← 新增：格式选择
  └─ 输出路径：exports/customers.csv  ← 明确：对象存储路径
  └─ CSV 选项：✓ 包含表头，分隔符: ,
  └─ 压缩方式：不压缩
  └─ 覆盖已有文件：否

步骤 6: 字段映射
  └─ 自动匹配或手动配置

步骤 7: 确认并创建
  └─ 检查配置
  └─ ✓ 创建后立即执行
```

---

## 📊 修改对比

| 项目 | 修复前 | 修复后 |
|------|--------|--------|
| **MinIO 显示** | ❌ 不显示（过滤失败） | ✅ 正常显示 |
| **目标类型** | 5 个选项（含 CSV/JSON） | 3 个选项（清晰） |
| **输出格式** | ❌ 无法选择 | ✅ 4 种格式可选 |
| **路径说明** | ⚠️ 不清楚存储位置 | ✅ 明确对象存储路径 |
| **CSV 选项** | ❌ 分散配置 | ✅ 集中配置 |
| **默认值** | ❌ 未设置 | ✅ 自动初始化 |

---

## 🧪 测试验证

### 测试 1：MinIO 资源显示

```bash
# 1. 访问任务创建页面
open http://localhost:5176/tasks/create

# 2. 操作步骤
- 填写基本信息
- 选择源数据源（任意数据库）
- 配置源
- 步骤 4：选择目标类型 "对象存储（MinIO/S3）"
- 点击"选择数据源"下拉框

# 3. 预期结果
✅ 应该看到：min存储 (minio)
```

### 测试 2：CSV 文件导出

```bash
# 1. 继续上述流程
- 选择数据源：min存储
- 步骤 5：配置目标

# 2. 预期界面
✅ 输出格式：[CSV] [JSON] [JSONL] [Parquet]  ← 单选按钮
✅ 输出路径：[exports/users.csv           ]  ← 输入框
✅ CSV 选项：[✓] 包含表头  分隔符: [,]
✅ 压缩方式：[不压缩 ▼]
✅ 覆盖已有文件：[关]  ← 开关

# 3. 配置示例
- 输出格式：CSV
- 输出路径：exports/sales/customers.csv
- CSV 选项：包含表头，分隔符: ,
- 压缩方式：Gzip
- 覆盖已有文件：是

# 4. 继续后续步骤
- 字段映射
- 确认并创建
```

### 测试 3：JSON 格式导出

```bash
# 步骤 5：配置目标
- 输出格式：JSON
- 输出路径：exports/data/users.json
- 压缩方式：Gzip
- 覆盖已有文件：否

# 预期结果
✅ CSV 选项不显示（仅 CSV 格式显示）
✅ 其他配置正常
```

---

## 🔧 技术实现细节

### 1. 对象存储类型映射

```javascript
// 支持的对象存储类型
const OBJECT_STORAGE_TYPES = ['s3', 'minio', 'oss']

// 过滤逻辑
if (type === 's3') {
  return OBJECT_STORAGE_TYPES.includes(resourceType)
}
```

### 2. 输出格式配置

```javascript
// 格式与扩展名映射
const FORMAT_EXTENSIONS = {
  csv: 'csv',
  json: 'json',
  jsonl: 'jsonl',
  parquet: 'parquet'
}

// 动态 placeholder
:placeholder="`输入文件路径，如：exports/users.${targetConfig.format || 'csv'}`"
```

### 3. 条件渲染

```vue
<!-- 仅 CSV 格式显示 -->
<el-form-item v-if="targetConfig.format === 'csv'" label="CSV 选项">
  ...
</el-form-item>
```

---

## 📝 后续改进建议

### 短期（已实现）
- [x] 修复 MinIO 资源过滤问题
- [x] 简化目标类型选择
- [x] 新增输出格式配置
- [x] 设置合理的默认值

### 中期（待实现）
- [ ] 添加路径浏览器（类似文件选择器）
- [ ] 支持路径模板（如：`exports/{date}/{table}.csv`）
- [ ] 预览生成的文件路径
- [ ] 添加路径验证（检查特殊字符）

### 长期（待规划）
- [ ] 支持更多输出格式（Avro、ORC、Excel）
- [ ] 支持分区导出（按日期、按字段分区）
- [ ] 支持文件大小限制（自动分片）
- [ ] 添加导出进度预览

---

## 📖 相关文档

- **表选择器功能文档**：[TABLE_SELECTOR_FEATURE.md](TABLE_SELECTOR_FEATURE.md)
- **Bug 修复文档**：[BUG_FIX_TABLE_SELECTOR.md](BUG_FIX_TABLE_SELECTOR.md)
- **UI 升级指南**：[UI_UPGRADE_GUIDE.md](UI_UPGRADE_GUIDE.md)
- **使用指南**：[USAGE_GUIDE.md](USAGE_GUIDE.md)

---

## 💡 常见问题

### Q1: 为什么移除了 CSV 和 JSON 作为独立的目标类型？

**A**: 因为 CSV 和 JSON 是**输出格式**，不是**数据源类型**。它们需要依赖对象存储来保存文件。现在的设计更符合逻辑：
1. 选择目标类型：对象存储
2. 选择输出格式：CSV/JSON/JSONL/Parquet

### Q2: 如果我想导出到本地文件系统怎么办？

**A**: 当前版本不支持直接导出到本地文件系统。推荐使用 MinIO 作为中转：
1. 导出到 MinIO
2. 使用 MinIO CLI (`mc`) 下载文件到本地

**未来改进**：可以添加"本地文件系统"作为目标类型。

### Q3: Parquet 格式是什么？

**A**: Parquet 是一种列式存储格式，适合大数据分析场景。特点：
- 压缩率高（比 CSV 小很多）
- 查询性能好（列式存储）
- 支持复杂数据类型
- 常用于数据仓库

### Q4: 路径可以包含中文吗？

**A**: 可以，但建议使用英文和数字：
- ✅ 推荐：`exports/users/2024-01-15.csv`
- ⚠️ 可用但不推荐：`导出/用户/2024-01-15.csv`

### Q5: 如果文件已存在会怎样？

**A**: 取决于"覆盖已有文件"设置：
- **关闭**（默认）：任务失败，提示文件已存在
- **开启**：覆盖已有文件

---

## ✅ 验证清单

修复完成后，请验证以下场景：

- [x] MinIO 资源能在目标列表中显示
- [x] 选择对象存储后，能看到输出格式选项
- [x] 选择 CSV 格式后，显示 CSV 特定选项
- [x] 选择 JSON 格式后，隐藏 CSV 选项
- [x] 默认值正确初始化
- [x] 路径 placeholder 动态更新
- [ ] 实际执行导出任务，文件正确保存到 MinIO
- [ ] 文件格式正确（CSV 有表头，压缩生效等）

---

**修复日期**：2025-01-15
**版本**：v2.0.2
**修复人**：Claude Code
