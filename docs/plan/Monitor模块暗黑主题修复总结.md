# ADDP 暗黑主题适配 - Monitor 模块修复总结

**修复时间**: 2026-02-16
**模块**: Monitor（执行监控）
**完成度**: 100% ✅

---

## 📝 修复内容概览

### 问题描述
Monitor 模块在暗黑主题下存在以下问题：
1. 页面顶部出现重复的"ADDP 监控中心"标题
2. 卡片和文字在暗黑模式下颜色不清晰
3. 图标、图表、菜单等使用硬编码颜色
4. 未适配平台统一主题系统

### 修复成果
✅ 完全消除硬编码颜色（6处）
✅ 实现浅色/暗黑/蓝色三种主题完美适配
✅ 统一使用平台 CSS 变量系统
✅ 主题切换平滑过渡

---

## 🔧 具体修改

### 1. App.vue
**问题**: 独立 header 导致重复标题
**修复**:
- 移除独立的 header 和菜单
- 简化为只保留 `<router-view />`
- 背景色使用 `var(--addp-bg-secondary)`

```vue
<!-- 修改后 -->
<template>
  <router-view />
</template>

<style>
#app {
  min-height: 100vh;
  background: var(--addp-bg-secondary);
}
</style>
```

### 2. StatisticsCard.vue
**问题**: 图标颜色硬编码 `#409eff`
**修复**:
- 移除 `iconColor` prop
- 新增 `type` prop (primary/success/warning/danger/info)
- 使用 Element Plus 颜色变量

```vue
<!-- 修改前 -->
<statistics-card iconColor="#409eff" />

<!-- 修改后 -->
<statistics-card type="primary" />
```

```javascript
const computedIconColor = computed(() => {
  const typeColorMap = {
    primary: 'var(--el-color-primary)',
    success: 'var(--el-color-success)',
    warning: 'var(--el-color-warning)',
    danger: 'var(--el-color-danger)',
    info: 'var(--el-color-info)'
  }
  return typeColorMap[props.type] || 'var(--el-color-primary)'
})
```

### 3. ModuleStatusBadge.vue
**问题**: 卡片背景和文字颜色硬编码
**修复**:
- `background: #fff` → `var(--addp-bg-primary)`
- `border: 1px solid #e4e7ed` → `var(--addp-border-color)`
- `color: #303133` → `var(--addp-text-primary)`
- `color: #606266` → `var(--addp-text-secondary)`
- `color: #909399` → `var(--addp-text-tertiary)`

### 4. Dashboard.vue

#### 4.1 统计卡片颜色
```vue
<!-- 修改前 -->
<statistics-card iconColor="#409eff" />
<statistics-card iconColor="#67c23a" />
<statistics-card iconColor="#e6a23c" />
<statistics-card iconColor="#f56c6c" />

<!-- 修改后 -->
<statistics-card type="primary" />
<statistics-card type="success" />
<statistics-card type="warning" />
<statistics-card type="danger" />
```

#### 4.2 Echarts 图表颜色
**问题**: 图表颜色硬编码，文字在暗黑模式下不可见
**修复**:
- 动态从 CSS 变量获取颜色
- 配置图例、坐标轴、分割线颜色
- 主题切换时自动重新渲染

```javascript
// 动态获取 CSS 变量
const getCssVar = (varName) => {
  const value = getComputedStyle(document.documentElement).getPropertyValue(varName).trim()
  if (!value) {
    console.warn(`CSS variable ${varName} not found`)
  }
  return value
}

const successColor = getCssVar('--el-color-success')
const dangerColor = getCssVar('--el-color-danger')
const primaryColor = getCssVar('--el-color-primary')
const textColor = getCssVar('--addp-text-primary')
const textSecondaryColor = getCssVar('--addp-text-secondary')
const borderColor = getCssVar('--addp-border-color')

// 图表配置
const option = {
  textStyle: { color: textColor },
  legend: { textStyle: { color: textColor } },
  xAxis: {
    axisLabel: { color: textSecondaryColor },
    axisLine: { lineStyle: { color: textSecondaryColor } }
  },
  yAxis: {
    axisLabel: { color: textSecondaryColor },
    splitLine: { lineStyle: { color: borderColor } }
  },
  series: [
    { itemStyle: { color: successColor } },
    { itemStyle: { color: dangerColor } },
    { itemStyle: { color: primaryColor } }
  ]
}
```

#### 4.3 主题切换响应
```javascript
import { useTheme } from '@common-ui'

const { mode } = useTheme()

// 计算 echarts 主题
const echartsTheme = computed(() => {
  return mode.value === 'dark' || mode.value === 'blue' ? 'dark' : null
})

// 监听主题变化
watch(echartsTheme, () => {
  if (trendData.value.length > 0) {
    renderTrendChart()  // 重新渲染图表
  }
})
```

### 5. ExecutionList.vue
**问题**: 卡片 header 文字颜色不清晰
**修复**:
```css
.card-header span {
  color: var(--addp-text-primary);
  font-weight: 500;
  font-size: 16px;
}
```

### 6. Login.vue
**问题**: 渐变背景硬编码
**修复**:
```css
/* 修改前 */
.login-container {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

/* 修改后 */
.login-container {
  background: var(--addp-primary-gradient);
}
```

---

## 🎨 使用的 CSS 变量

### Element Plus 颜色变量
- `--el-color-primary` - 主色/蓝色 (#409eff)
- `--el-color-success` - 成功色/绿色 (#67c23a)
- `--el-color-warning` - 警告色/橙色 (#e6a23c)
- `--el-color-danger` - 危险色/红色 (#f56c6c)
- `--el-color-info` - 信息色/灰色 (#909399)

### ADDP 平台变量（在 `common-frontend/basic/src/styles/theme.css` 定义）

#### 文本颜色
- `--addp-text-primary` - 主文本（浅色: #303133 / 暗黑: #E5EAF3）
- `--addp-text-secondary` - 次要文本（浅色: #606266 / 暗黑: #CFD3DC）
- `--addp-text-tertiary` - 三级文本（浅色: #909399 / 暗黑: #A3A6AD）

#### 背景颜色
- `--addp-bg-primary` - 主背景（浅色: #FFFFFF / 暗黑: #1D1E1F）
- `--addp-bg-secondary` - 次要背景（浅色: #F5F7FA / 暗黑: #141414）
- `--addp-bg-sidebar` - 侧边栏背景
- `--addp-bg-header` - 页头背景

#### 边框和其他
- `--addp-border-color` - 边框色（浅色: #E4E7ED / 暗黑: #4C4D4F）
- `--addp-primary-gradient` - 品牌渐变色

---

## 🌓 主题模式支持

### 浅色模式
- 白色背景 + 深色文字
- 清晰的对比度
- Element Plus 默认颜色

### 暗黑模式
- 深灰色背景 (#1D1E1F)
- 浅色文字 (#E5EAF3)
- 降低的对比度，护眼舒适

### 蓝色模式
- 深蓝色背景 (#0f1629)
- 蓝色调文字 (#e0e7ff)
- 科技感配色

---

## ✅ 测试验证

### 测试场景
- [x] 浅色模式 - 所有文字清晰可见
- [x] 暗黑模式 - 所有文字清晰可见，对比度合适
- [x] 蓝色模式 - 所有文字清晰可见，配色协调
- [x] 主题切换 - 平滑过渡，无闪烁
- [x] Echarts 图表 - 图例、坐标轴、线条颜色正确
- [x] 统计卡片 - 图标颜色正确，文字清晰
- [x] 模块健康状态卡片 - 背景和文字对比度正确

### 访问地址
- Portal 入口: http://localhost:5170 → 执行监控
- 直接访问: http://localhost:5179
- 生产环境: http://localhost:8000/monitor/

---

## 📚 关键经验

### 1. 主题系统架构
```
平台主题系统 (theme.css)
  ├─ Element Plus 变量覆盖 (--el-color-*)
  ├─ ADDP 平台变量 (--addp-*)
  └─ 组件响应式样式

组件使用
  ├─ 优先使用 Element Plus 组件 type 属性
  ├─ 样式中使用 CSS 变量
  └─ 动态场景使用 getComputedStyle 获取
```

### 2. 颜色选择原则
- **状态色**: 使用 Element Plus 变量 (`--el-color-*`)
- **文字色**: 使用 ADDP 文本变量 (`--addp-text-*`)
- **背景色**: 使用 ADDP 背景变量 (`--addp-bg-*`)
- **边框色**: 使用 `--addp-border-color`

### 3. 动态颜色获取
对于 Canvas、Echarts、第三方库等无法直接使用 CSS 变量的场景：
```javascript
const color = getComputedStyle(document.documentElement)
  .getPropertyValue('--el-color-primary').trim()
```

### 4. 主题切换响应
```javascript
import { useTheme } from '@common-ui'
const { mode } = useTheme()

watch(mode, () => {
  // 重新渲染需要动态颜色的组件
})
```

---

## 🚀 可复用模式

Monitor 模块的修复方案可作为模板应用到其他模块：

### 模式 1：统计卡片
```vue
<template>
  <el-card>
    <el-icon :color="computedIconColor">
      <component :is="icon" />
    </el-icon>
    <span class="value">{{ value }}</span>
  </el-card>
</template>

<script setup>
const computedIconColor = computed(() => {
  const colorMap = {
    primary: 'var(--el-color-primary)',
    success: 'var(--el-color-success)',
    // ...
  }
  return colorMap[props.type]
})
</script>

<style scoped>
.value {
  color: var(--addp-text-primary);
}
</style>
```

### 模式 2：Echarts 图表
```javascript
function renderChart() {
  const getCssVar = (name) =>
    getComputedStyle(document.documentElement).getPropertyValue(name).trim()

  const option = {
    textStyle: { color: getCssVar('--addp-text-primary') },
    // ...
  }

  chart.setOption(option)
}

watch(echartsTheme, renderChart)
```

### 模式 3：渐变背景
```css
.container {
  background: var(--addp-primary-gradient);
}
```

---

## 📖 参考文档

- [ADDP 硬编码颜色修复计划](./硬编码颜色修复计划.md)
- [ADDP 技术栈规约](../addp技术栈规约.md)
- [Element Plus 暗黑模式](https://element-plus.org/zh-CN/guide/dark-mode.html)

---

## 🎯 后续工作

Monitor 模块修复完成后，建议按以下优先级修复其他模块：

1. **Service 模块** (243 处硬编码) - P0
2. **Manager 模块** (171 处硬编码) - P1
3. **System 模块** (81 处硬编码) - P2
4. **Develop 模块** (73 处硬编码) - P2
5. 其他模块

详见 [硬编码颜色修复计划](./硬编码颜色修复计划.md)
