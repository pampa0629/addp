# ADDP 前端风格设计规范

## 概述

本文档定义了 ADDP 平台所有前端模块的统一视觉风格和开发规范。所有前端开发人员必须遵循本规范，以确保平台界面的一致性和可维护性。

## 核心原则

### 1. 统一颜色系统

**禁止使用硬编码颜色值**。所有颜色必须通过 CSS 变量引用，以支持主题切换（浅色/深色/蓝色/紫色模式）。

### 2. DRY（Don't Repeat Yourself）原则

- 前端组件唯一实现规则以 [ADDP 开发原则](../../docs/spec/addp开发原则.md) 为规范真源
- 所有共享的 UI 组件必须放在 `common-frontend/` 目录
- 相同语义职责、输入输出和交互契约的能力不得在各模块中重复实现
- 业务模块只保留领域组合和协议适配，不复制共享组件内部的基础渲染、状态管理、格式化或通用交互
- 优先使用 Element Plus 官方组件

### 3. 响应式设计

- 所有界面必须支持主流分辨率（1280px 及以上）
- 使用 Flexbox 或 Grid 布局
- 避免固定宽度/高度，使用相对单位（%、vh、vw）

---

## 颜色系统

### CSS 变量命名规范

所有自定义 CSS 变量使用 `--addp-*` 前缀，定义在 `common-frontend/basic/src/styles/theme.css` 中。

#### 背景色变量

```css
--addp-bg-primary      /* 主背景色：页面、卡片、对话框等的主要背景 */
--addp-bg-secondary    /* 次要背景色：灰色区域、禁用状态、分隔区域 */
--addp-bg-sidebar      /* 侧边栏背景色 */
--addp-bg-header       /* 顶部导航栏背景色 */
```

**不同主题的典型值**：

| 变量 | 浅色模式 | 深色模式 | 蓝色模式 | 紫色模式 |
|------|---------|---------|---------|---------|
| `--addp-bg-primary` | `#FFFFFF` | `#1D1E1F` | `#0f1629` | `#1a1625` |
| `--addp-bg-secondary` | `#F5F7FA` | `#141414` | `#0a0f1e` | `#0f0a1a` |

#### 文本色变量

```css
--addp-text-primary    /* 主文本：标题、正文等重要文本 */
--addp-text-secondary  /* 次要文本：说明文字、辅助信息 */
--addp-text-tertiary   /* 三级文本：占位符、禁用文本、提示信息 */
```

**不同主题的典型值**：

| 变量 | 浅色模式 | 深色模式 | 蓝色模式 | 紫色模式 |
|------|---------|---------|---------|---------|
| `--addp-text-primary` | `#303133` | `#E5EAF3` | `#e0e7ff` | `#e8e3f0` |
| `--addp-text-secondary` | `#606266` | `#CFD3DC` | `#c7d2fe` | `#c4b8d9` |
| `--addp-text-tertiary` | `#909399` | `#A3A6AD` | `#a5b4fc` | `#9d8eb8` |

#### 边框色变量

```css
--addp-border-color       /* 主边框色：分隔线、卡片边框、表格边框 */
--addp-border-color-light /* 浅边框色：次要分隔线 */
```

**不同主题的典型值**：

| 变量 | 浅色模式 | 深色模式 | 蓝色模式 | 紫色模式 |
|------|---------|---------|---------|---------|
| `--addp-border-color` | `#E4E7ED` | `#4C4D4F` | `#1e3a8a` | `#3a2f52` |
| `--addp-border-color-light` | `#EBEEF5` | `#414243` | `#1e40af` | `#2d243f` |

#### 阴影变量

```css
--addp-shadow-card       /* 卡片阴影 */
--addp-shadow-hover      /* 悬浮阴影 */
```

#### 品牌色变量

```css
--addp-primary-gradient  /* 品牌色渐变（紫色渐变） */
```

#### 模块色变量

每个模块有独立的主题色，用于模块卡片、图标等：

```css
--addp-module-system        /* System 模块：蓝色 #409EFF */
--addp-module-transfer      /* Transfer 模块：红色 #F56C6C */
--addp-module-manager       /* Manager 模块：绿色 #67C23A */
--addp-module-develop       /* Develop 模块：紫色 #9370DB */
--addp-module-service       /* Service 模块：天蓝色 #1890FF */
--addp-module-orchestrator  /* Orchestrator 模块：橙红色 #FF7875 */
--addp-module-meta          /* Meta 模块：橙色 #E6A23C */
```

#### 交互状态色变量

**重要**：导航菜单的选中/激活状态、链接颜色等交互状态，推荐直接使用 Element Plus 的状态色变量，以确保与组件库保持一致：

```css
var(--el-color-primary)     /* 主色/激活色：菜单选中、链接 #409EFF */
var(--el-color-success)     /* 成功状态：绿色 */
var(--el-color-warning)     /* 警告状态：橙色 */
var(--el-color-danger)      /* 错误/危险：红色 */
var(--el-color-info)        /* 信息状态：灰色 */
```

**使用说明**：
- 侧边栏菜单的选中状态使用 `var(--el-color-primary)` (蓝色 #409EFF)
- 悬浮状态背景色使用 `var(--addp-bg-secondary)`
- 链接颜色使用 `var(--el-color-primary)`
- Element Plus 会自动在暗黑模式下调整这些颜色的亮度，无需手动覆盖

**注意**：不要在 ADDP 的 CSS 变量中重新定义这些状态色，直接使用 Element Plus 提供的变量即可。

### 颜色使用规范

#### ✅ 正确示例

```vue
<style scoped>
.page-container {
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
  border: 1px solid var(--addp-border-color);
}

.secondary-section {
  background: var(--addp-bg-secondary);
  color: var(--addp-text-secondary);
}

.help-text {
  color: var(--addp-text-tertiary);
}

/* 空状态区域、预览面板等需要明确设置背景色 */
.empty-state,
.preview-content {
  background: var(--addp-bg-primary);
}

/* 链接和激活状态使用 Element Plus 变量 */
.nav-link.active {
  color: var(--el-color-primary);
}
</style>
```

#### ❌ 错误示例

```vue
<style scoped>
/* ❌ 禁止：硬编码颜色 */
.page-container {
  background: #ffffff;
  color: #303133;
  border: 1px solid #e4e7ed;
}

/* ❌ 禁止：使用 Element Plus 内部变量 */
.page-container {
  background: var(--el-bg-color);
}
</style>
```

### 状态色使用

对于成功、警告、错误、信息等状态色，**可以使用** Element Plus 的官方变量：

```css
var(--el-color-success)   /* 成功：绿色 */
var(--el-color-warning)   /* 警告：橙色 */
var(--el-color-danger)    /* 错误：红色 */
var(--el-color-info)      /* 信息：灰色 */
var(--el-color-primary)   /* 主色：蓝色 */
```

**注意**：这些状态色 Element Plus 已经自动适配了暗黑模式，无需手动处理。

---

## 主题系统架构

### 主题类型

ADDP 平台支持 **5 种主题模式**，定义在 `common-frontend/basic/src/config/themes.js` 中：

| 主题值 | 主题名称 | 图标 | 是否深色 | 说明 |
|--------|---------|------|---------|------|
| `system` | 跟随系统 | Monitor | 跟随系统 | 根据操作系统设置自动切换浅色/深色 |
| `light` | 浅色模式 | Sunny | `false` | 传统浅色主题，白色背景 |
| `dark` | 深色模式 | Moon | `true` | 传统深色主题，深灰色背景 |
| `blue` | 蓝色模式 | Cloudy | `true` | 深色系，蓝色调背景和边框 |
| `purple` | 紫色模式 | MagicStick | `true` | 深色系，紫色调背景和边框 |

### 主题配置文件

所有主题配置集中在 `common-frontend/basic/src/config/themes.js` 中：

```javascript
export const THEME_CONFIGS = [
  {
    value: 'system',
    label: '跟随系统',
    icon: 'Monitor',
    isDarkTheme: null // null 表示跟随系统
  },
  {
    value: 'light',
    label: '浅色模式',
    icon: 'Sunny',
    isDarkTheme: false
  },
  {
    value: 'dark',
    label: '深色模式',
    icon: 'Moon',
    isDarkTheme: true
  },
  {
    value: 'blue',
    label: '蓝色模式',
    icon: 'Cloudy',
    isDarkTheme: true // 蓝色主题视为深色（用于 Element Plus 深色 CSS）
  },
  {
    value: 'purple',
    label: '紫色模式',
    icon: 'MagicStick',
    isDarkTheme: true // 紫色主题视为深色（用于 Element Plus 深色 CSS）
  }
]
```

**新增主题步骤**：
1. 在 `themes.js` 中添加主题配置
2. 在 `theme.css` 中添加对应的 CSS 变量定义（`html.{theme-name}` 选择器）
3. 在 `ThemeSwitcher.vue` 中导入对应的图标组件

### 主题 CSS 变量定义

所有主题的 CSS 变量定义在 `common-frontend/basic/src/styles/theme.css` 中。

#### 浅色模式（`:root`）

```css
:root {
  --addp-bg-primary: #FFFFFF;
  --addp-bg-secondary: #F5F7FA;
  --addp-bg-sidebar: #FFFFFF;
  --addp-bg-header: #FFFFFF;

  --addp-text-primary: #303133;
  --addp-text-secondary: #606266;
  --addp-text-tertiary: #909399;

  --addp-border-color: #E4E7ED;
  --addp-border-color-light: #EBEEF5;

  --addp-primary-gradient: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}
```

#### 深色模式（`html.dark`）

```css
html.dark {
  --addp-bg-primary: #1D1E1F;
  --addp-bg-secondary: #141414;
  --addp-bg-sidebar: #1D1E1F;
  --addp-bg-header: #1D1E1F;

  --addp-text-primary: #E5EAF3;
  --addp-text-secondary: #CFD3DC;
  --addp-text-tertiary: #A3A6AD;

  --addp-border-color: #4C4D4F;
  --addp-border-color-light: #414243;

  --addp-primary-gradient: linear-gradient(135deg, #7c8fe6 0%, #9068bc 100%);
}
```

#### 蓝色模式（`html.blue`）

```css
html.blue {
  --addp-bg-primary: #0f1629;
  --addp-bg-secondary: #0a0f1e;
  --addp-bg-sidebar: #0f1629;
  --addp-bg-header: #0f1629;

  --addp-text-primary: #e0e7ff;
  --addp-text-secondary: #c7d2fe;
  --addp-text-tertiary: #a5b4fc;

  --addp-border-color: #1e3a8a;
  --addp-border-color-light: #1e40af;

  --addp-primary-gradient: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
}
```

#### 紫色模式（`html.purple`）

```css
html.purple {
  --addp-bg-primary: #1a1625;
  --addp-bg-secondary: #0f0a1a;
  --addp-bg-sidebar: #1a1625;
  --addp-bg-header: #1a1625;

  --addp-text-primary: #e8e3f0;
  --addp-text-secondary: #c4b8d9;
  --addp-text-tertiary: #9d8eb8;

  --addp-border-color: #3a2f52;
  --addp-border-color-light: #2d243f;

  --addp-primary-gradient: linear-gradient(135deg, #a78bfa 0%, #c084fc 100%);
}
```

### 深色系主题的双重 class 机制

**核心机制**：深色系主题（`dark`、`blue`、`purple`）会同时应用两个 class：

```html
<!-- 深色模式 -->
<html class="dark">

<!-- 蓝色模式 -->
<html class="dark blue">

<!-- 紫色模式 -->
<html class="dark purple">
```

**为什么需要同时应用 `dark` class？**

1. **Element Plus 深色 CSS 依赖**：`element-plus/theme-chalk/dark/css-vars.css` 只在 `html.dark` 时生效
2. **自定义主题变量覆盖**：`html.blue` 和 `html.purple` 在 `dark` 的基础上覆盖特定颜色变量
3. **避免重复定义**：不需要为每个深色系主题重新定义所有 Element Plus 组件样式

**实现逻辑**（在 `useTheme.js` 中）：

```javascript
function applyTheme(mode, systemPrefersDark = false) {
  const html = document.documentElement
  const config = getThemeConfig(mode)

  // 清除所有主题 class
  html.classList.remove('dark', 'light', 'blue', 'purple')

  if (mode === 'light') {
    // 浅色模式：不添加任何 class（使用 :root 变量）
    console.log('[Theme] 应用浅色模式')
  } else if (mode === 'system') {
    // 跟随系统：根据系统偏好决定是否添加 dark class
    if (systemPrefersDark) {
      html.classList.add('dark')
      console.log('[Theme] 跟随系统（深色）')
    } else {
      console.log('[Theme] 跟随系统（浅色）')
    }
  } else {
    // 其他主题：添加对应的 class
    html.classList.add(mode)

    // 如果是深色主题，同时添加 dark class 以触发 Element Plus 深色 CSS
    if (config.isDarkTheme) {
      html.classList.add('dark')
      console.log(`[Theme] 应用 ${config.label}（深色系，同时启用 Element Plus 深色模式）`)
    } else {
      console.log(`[Theme] 应用 ${config.label}`)
    }
  }
}
```

### Element Plus 深色模式集成

**所有模块的 `main.js` 必须静态导入 Element Plus 深色 CSS**：

```javascript
// 导入 Element Plus 深色模式 CSS
import 'element-plus/theme-chalk/dark/css-vars.css'
// 导入统一主题 CSS
import '@common-ui/styles/theme.css'
```

**重要说明**：
- 这个 CSS 文件只在 `html.dark` 时生效，不会强制应用深色模式
- 通过动态添加/移除 `dark` class 来控制 Element Plus 是否使用深色样式
- 静态导入是正确的做法，不需要动态加载

### 自动过渡动画

主题系统已在 `theme.css` 中配置了全局过渡动画：

```css
*,
*::before,
*::after {
  transition: background-color 0.3s ease,
              border-color 0.3s ease,
              color 0.3s ease,
              box-shadow 0.3s ease;
}
```

**无需在组件中额外配置过渡动画**，切换主题时所有颜色变化会自动平滑过渡。

### Console 与模块通信

#### Console（发送方）

Console 通过 postMessage 向所有 iframe 模块广播主题变化：

```javascript
// console/frontend/src/store/theme.js
watch(isDark, (newIsDark) => {
  const iframes = document.querySelectorAll('iframe.module-iframe')
  iframes.forEach((iframe) => {
    iframe.contentWindow?.postMessage({
      type: 'theme-change',
      source: 'addp-console',
      mode: mode.value,          // 'system' | 'light' | 'dark' | 'blue' | 'purple'
      isDark: newIsDark,         // boolean
      timestamp: Date.now()
    }, window.location.origin)
  })
})
```

#### 模块（接收方）

各模块在 `main.js` 中初始化主题系统，自动监听 Console 消息：

```javascript
// {module}/frontend/src/main.js
import { useTheme } from '@common-ui'
import '@common-ui/styles/theme.css'
import 'element-plus/theme-chalk/dark/css-vars.css'

const { init: initTheme } = useTheme({
  listenToConsole: true,        // 监听 Console 的 postMessage
  storageKey: 'theme-mode'     // localStorage 键名
})

initTheme()
app.mount('#app')
```

**开发者无需手动处理 postMessage 通信**，`useTheme` 已封装所有逻辑。

---

## 组件开发规范

### 1. 新建共享组件

当需要创建可复用的组件时，遵循以下规则：

#### 1.1 创建前检查与唯一所有权

1. 使用文件搜索和代码搜索检查 `common-frontend/` 及各业务模块中是否已有相同职责的组件、composable 或工具函数。
2. 已有规范实现满足契约时必须直接复用；契约缺少稳定的通用能力时扩展原实现，不新建旁路组件。
3. 多个模块已经需要相同稳定能力时，将基础能力提炼到 `common-frontend/`。模块可以保留只负责数据适配、领域编排或依赖装配的薄适配器。
4. 新实现替代旧实现时，同一次变更删除旧组件、旧导出和旧调用路径，不保留兼容分支。
5. 跨模块共享组件必须增加最小充分测试；容易再次被复制的关键能力应增加所有权测试，约束消费端组合规范实现而非直接重写。

判断是否重复以组件的语义职责和契约为准，而不是以是否都使用 `el-table`、`el-form` 等底层标签为准。不同业务职责可以有不同组件；同一查询结果、资源选择或预览能力不能因模块名称不同而维护多份实现。

#### 1.2 组件分类

- **无地图依赖**的组件：放在 `common-frontend/basic/src/components/`
- **需要地图库**的组件：放在 `common-frontend/map/src/components/`

#### 1.3 组件导出

在对应的 `index.js` 中导出：

```javascript
// common-frontend/basic/src/index.js
export { default as MyComponent } from './components/MyComponent.vue'
```

#### 1.4 组件使用

在模块中使用共享组件：

```vue
<script setup>
import { MyComponent } from '@common-ui'
</script>

<template>
  <MyComponent />
</template>
```

### 2. 组件样式规范

#### 2.1 Scoped 样式

所有组件必须使用 `<style scoped>`，避免样式污染：

```vue
<style scoped>
.my-component {
  background: var(--addp-bg-primary);
  padding: 16px;
}
</style>
```

#### 2.2 深度选择器

需要修改子组件样式时，使用 `:deep()` 选择器：

```vue
<style scoped>
.my-component :deep(.el-input__wrapper) {
  background: var(--addp-bg-secondary);
}
</style>
```

#### 2.3 禁止使用内联样式

**禁止**在模板中使用 `style` 属性：

```vue
<!-- ❌ 错误 -->
<div style="background: #ffffff; color: #303133;">内容</div>

<!-- ✅ 正确 -->
<div class="content-box">内容</div>

<style scoped>
.content-box {
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
}
</style>
```

**例外情况**：动态样式可以使用 `:style` 绑定，但值应来自计算属性或响应式变量。

---

## Element Plus 组件覆盖

### 全局覆盖规则

`theme.css` 已经为 Element Plus 组件提供了全局深色系主题覆盖样式，**无需在各模块中重复覆盖**。

**深色系主题选择器**（`html.dark, html.blue, html.purple`）下已覆盖的组件：

- `el-empty`：空状态
- `el-pagination`：分页器
- `el-dialog`：对话框
- `el-drawer`：抽屉
- `el-input`：输入框
- `el-textarea`：文本域
- `el-select`：下拉选择器
- `el-table`：表格
- `el-card`：卡片
- `el-dropdown`：下拉菜单
- `el-tree`：树形控件
- `el-tabs`：标签页
- `el-breadcrumb`：面包屑
- `el-descriptions`：描述列表
- `el-menu`：菜单组件

**覆盖样式示例**（在 `theme.css` 中）：

```css
/* 深色系主题和紫色主题、蓝色主题共享这些样式 */
html.dark,
html.blue,
html.purple {
  .el-card {
    background-color: var(--addp-bg-primary) !important;
    border-color: var(--addp-border-color) !important;
  }

  .el-empty {
    background-color: transparent !important;
  }

  .el-pagination button,
  .el-pagination .el-pager li {
    background-color: var(--addp-bg-primary) !important;
    color: var(--addp-text-primary) !important;
  }
}
```

### 组件级覆盖

如果需要在特定组件中覆盖 Element Plus 样式，使用 `:deep()` 选择器：

```vue
<style scoped>
.my-table :deep(.el-table__header) {
  background: var(--addp-bg-secondary);
}
</style>
```

---

## 布局规范

### 1. 页面布局

推荐使用 Flexbox 布局：

```vue
<template>
  <div class="page-container">
    <div class="page-header">页头</div>
    <div class="page-content">内容</div>
    <div class="page-footer">页脚</div>
  </div>
</template>

<style scoped>
.page-container {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--addp-bg-secondary);
}

.page-header {
  flex-shrink: 0;
  padding: 16px;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
}

.page-content {
  flex: 1;
  overflow: auto;
  padding: 16px;
}

.page-footer {
  flex-shrink: 0;
  padding: 16px;
  background: var(--addp-bg-primary);
  border-top: 1px solid var(--addp-border-color);
}
</style>
```

### 2. 间距规范

使用 8px 基础倍数进行间距设计：

```css
padding: 8px;     /* 小间距 */
padding: 16px;    /* 中间距（推荐） */
padding: 24px;    /* 大间距 */
padding: 32px;    /* 超大间距 */

margin: 8px;      /* 小间距 */
margin: 16px;     /* 中间距（推荐） */
margin: 24px;     /* 大间距 */
```

### 3. 圆角规范

```css
border-radius: 4px;   /* 小圆角：按钮、输入框 */
border-radius: 8px;   /* 中圆角：卡片、对话框 */
border-radius: 12px;  /* 大圆角：模块卡片 */
```

---

## 字体规范

### 1. 字体大小

```css
font-size: 12px;   /* 辅助文字 */
font-size: 14px;   /* 正文（默认） */
font-size: 16px;   /* 小标题 */
font-size: 18px;   /* 中标题 */
font-size: 20px;   /* 大标题 */
font-size: 24px;   /* 主标题 */
```

### 2. 字体粗细

```css
font-weight: 400;  /* 正常 */
font-weight: 500;  /* 中等（推荐） */
font-weight: 600;  /* 加粗（标题） */
font-weight: 700;  /* 特粗 */
```

### 3. 行高

```css
line-height: 1.5;   /* 正文（推荐） */
line-height: 1.2;   /* 标题 */
line-height: 2;     /* 疏松排版 */
```

---

## 图标规范

### 1. 图标来源

优先使用 Element Plus Icons：

```javascript
import { Edit, Delete, Plus, Refresh } from '@element-plus/icons-vue'
```

### 2. 图标注册

在 `main.js` 中按需注册：

```javascript
const icons = { Edit, Delete, Plus, Refresh }
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}
```

### 3. 图标使用

```vue
<template>
  <el-button>
    <el-icon><Plus /></el-icon>
    新增
  </el-button>
</template>
```

## AI 助手入口规范

ADDP 各模块的 AI 助手属于同一类平台能力，入口和基础交互必须保持一致：

- 默认使用 `MagicStick`（魔法棒）作为入口图标，并使用统一的按钮尺寸、悬浮反馈和打开动效；不得在不同模块使用含义不一致的图标或自定义一套入口样式。
- 默认将入口固定在页面右下角，使用浮动按钮呈现。入口必须位于当前模块内容层级之上，但不得遮挡主要操作、编辑器内容、结果区域或对话框；窄屏和 iframe 模式下应自动避让边缘和底部操作栏。
- 入口按钮必须提供国际化的 `aria-label` 和 Tooltip（例如“打开 AI 助手”）；不能只依赖魔法棒图形表达含义。加载、禁用和错误状态需要有明确的视觉状态，并通过共享状态播报能力提供可访问反馈。
- 打开的助手面板应复用统一的尺寸、层级、标题栏、关闭行为、加载状态、错误提示和输入/提交区域风格。关闭面板后应保留已生成内容和会话状态，除非用户明确结束当前会话或页面生命周期结束。
- 入口和面板优先复用 `common-frontend` 的共享组件、主题变量和交互能力；不得在各业务模块重复实现同一套视觉规则。若尚无合适的共享能力，应先评估抽取到 `common-frontend`，再由模块提供业务内容。
- 只有在业务上下文确实要求其他入口位置或交互方式时才允许偏离，并须在对应模块文档中说明原因和适用范围；偏离不得改变魔法棒的语义和助手面板的基础交互约定。

---

## 表单规范

### 1. 表单布局

使用 Element Plus 的 `el-form` 组件：

```vue
<template>
  <el-form :model="form" label-width="120px">
    <el-form-item label="用户名" required>
      <el-input v-model="form.username" placeholder="请输入用户名" />
    </el-form-item>
    <el-form-item label="邮箱">
      <el-input v-model="form.email" placeholder="请输入邮箱" />
    </el-form-item>
    <el-form-item>
      <el-button type="primary" @click="handleSubmit">提交</el-button>
      <el-button @click="handleCancel">取消</el-button>
    </el-form-item>
  </el-form>
</template>
```

### 2. 表单验证

使用 Element Plus 的内置验证：

```javascript
const rules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { min: 3, max: 20, message: '长度在 3 到 20 个字符', trigger: 'blur' }
  ],
  email: [
    { required: true, message: '请输入邮箱', trigger: 'blur' },
    { type: 'email', message: '请输入正确的邮箱地址', trigger: 'blur' }
  ]
}
```

---

## 表格规范

### 1. 基础表格

```vue
<template>
  <el-table :data="tableData" stripe>
    <el-table-column prop="name" label="姓名" width="180" />
    <el-table-column prop="email" label="邮箱" />
    <el-table-column label="操作" width="180">
      <template #default="{ row }">
        <el-button size="small" @click="handleEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="handleDelete(row)">删除</el-button>
      </template>
    </el-table-column>
  </el-table>
</template>
```

### 2. 分页表格

```vue
<template>
  <div class="table-container">
    <el-table :data="tableData" stripe>
      <!-- 表格列 -->
    </el-table>

    <el-pagination
      v-model:current-page="currentPage"
      v-model:page-size="pageSize"
      :total="total"
      :page-sizes="[10, 20, 50, 100]"
      layout="total, sizes, prev, pager, next, jumper"
    />
  </div>
</template>

<style scoped>
.table-container {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
</style>
```

---

## 按钮规范

### 1. 按钮类型

```vue
<template>
  <!-- 主要操作 -->
  <el-button type="primary">主要按钮</el-button>

  <!-- 次要操作 -->
  <el-button>次要按钮</el-button>

  <!-- 成功操作 -->
  <el-button type="success">成功按钮</el-button>

  <!-- 警告操作 -->
  <el-button type="warning">警告按钮</el-button>

  <!-- 危险操作 -->
  <el-button type="danger">危险按钮</el-button>

  <!-- 信息操作 -->
  <el-button type="info">信息按钮</el-button>

  <!-- 文本按钮 -->
  <el-button type="text">文本按钮</el-button>
</template>
```

### 2. 按钮尺寸

```vue
<template>
  <el-button size="large">大按钮</el-button>
  <el-button>默认按钮</el-button>
  <el-button size="small">小按钮</el-button>
</template>
```

### 3. 按钮组

```vue
<template>
  <el-button-group>
    <el-button type="primary"><el-icon><ArrowLeft /></el-icon> 上一页</el-button>
    <el-button type="primary">下一页 <el-icon><ArrowRight /></el-icon></el-button>
  </el-button-group>
</template>
```

---

## 对话框规范

### 1. 基础对话框

```vue
<template>
  <el-dialog
    v-model="dialogVisible"
    class="addp-dialog"
    title="对话框标题"
    width="min(500px, calc(100vw - 24px))"
  >
    <div>对话框内容</div>
    <template #footer>
      <el-button @click="dialogVisible = false">取消</el-button>
      <el-button type="primary" @click="handleConfirm">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'

const dialogVisible = ref(false)

const handleConfirm = () => {
  // 处理确认逻辑
  dialogVisible.value = false
}
</script>
```

### 2. 表单对话框

```vue
<template>
  <el-dialog
    v-model="dialogVisible"
    class="addp-dialog"
    title="编辑用户"
    width="min(500px, calc(100vw - 24px))"
  >
    <el-form :model="form" label-position="top">
      <el-form-item label="用户名">
        <el-input v-model="form.username" />
      </el-form-item>
      <el-form-item label="邮箱">
        <el-input v-model="form.email" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="dialogVisible = false">取消</el-button>
      <el-button type="primary" @click="handleSubmit">保存</el-button>
    </template>
  </el-dialog>
</template>
```

所有业务对话框使用共享 `addp-dialog` 类。该类统一标题、正文、页脚间距，并限制正文最大高度；内容超过可用高度时只滚动正文，标题和操作按钮保持可见。宽度必须同时声明桌面目标宽度和 `100vw - 24px` 的窄窗口上限，不能只写固定像素宽度。

页脚操作按钮默认作为一组整体右对齐，不得把取消按钮和主操作分散到页脚两端。组内从左到右依次为取消、关闭或返回等次要操作，危险但非推荐的操作，以及唯一推荐的主操作；主操作始终位于最右侧。只有一个关闭按钮时，该按钮也保持右对齐。页脚允许按钮在窄窗口下换行，不能让按钮或文字溢出对话框。字段较少的业务表单优先使用顶部标签，避免长标签在窄窗口中挤压输入区域。

`ElMessageBox.confirm` 类确认框使用共享 `addp-message-box` 类，并显式传入已国际化的确定、取消按钮文案，不得依赖 Element Plus 的默认语言。按钮组整体右对齐，取消在确认左侧、确认位于最右侧；删除、清空等不可逆操作的确认按钮必须使用危险样式。

弹窗打开后必须有明确且安全的初始焦点：表单弹窗优先聚焦首个主要输入字段，只读弹窗优先聚焦关闭按钮，包含不可逆操作的弹窗优先聚焦取消按钮。关闭弹窗后应把焦点恢复到触发该弹窗的控件；下拉菜单项触发或连续弹窗等无法依赖组件默认恢复的场景，调用方必须显式恢复到稳定的触发按钮。弹窗继续使用 Element Plus 的焦点陷阱和 `Esc` 关闭能力，不在业务模块重复实现焦点循环。

---

## 消息提示规范

### 1. Message 消息

```javascript
import { ElMessage } from 'element-plus'

// 成功
ElMessage.success('操作成功')

// 警告
ElMessage.warning('请注意')

// 错误
ElMessage.error('操作失败')

// 信息
ElMessage.info('提示信息')
```

### 2. MessageBox 确认框

```javascript
import { ElMessageBox } from 'element-plus'

// 确认对话框
ElMessageBox.confirm('确定要删除吗？', '警告', {
  confirmButtonText: '确定',
  cancelButtonText: '取消',
  type: 'warning'
}).then(() => {
  // 确认操作
}).catch(() => {
  // 取消操作
})
```

### 3. Notification 通知

```javascript
import { ElNotification } from 'element-plus'

ElNotification({
  title: '成功',
  message: '操作已完成',
  type: 'success'
})
```

---

## 响应式设计规范

### 1. 断点定义

```css
/* 小屏幕（平板） */
@media (max-width: 768px) {
  .sidebar {
    width: 100%;
  }
}

/* 中等屏幕（笔记本） */
@media (min-width: 769px) and (max-width: 1440px) {
  .sidebar {
    width: 300px;
  }
}

/* 大屏幕（桌面） */
@media (min-width: 1441px) {
  .sidebar {
    width: 350px;
  }
}
```

### 2. 移动端适配

虽然 ADDP 主要面向桌面端，但仍需考虑小屏幕设备：

- 使用 `vh`、`vw` 单位代替固定像素
- 使用 Flexbox 布局自动换行
- 隐藏次要信息，保留核心功能

---

## 无障碍规范（Accessibility）

### 1. 语义化 HTML

使用正确的 HTML 标签：

```vue
<template>
  <!-- ✅ 正确 -->
  <header>页头</header>
  <nav>导航</nav>
  <main>主内容</main>
  <footer>页脚</footer>

  <!-- ❌ 错误 -->
  <div class="header">页头</div>
  <div class="nav">导航</div>
</template>
```

### 2. ARIA 属性

为交互元素添加 ARIA 属性：

```vue
<template>
  <button
    aria-label="关闭对话框"
    @click="handleClose"
  >
    <el-icon><Close /></el-icon>
  </button>
</template>
```

### 3. 键盘导航

确保所有交互元素可通过键盘访问：

- Tab：焦点移动
- Enter / Space：确认操作
- Esc：关闭对话框/弹窗

### 4. 动态状态播报

校验、保存、执行等异步状态不能只依赖颜色、加载图标或短暂提示。业务页面应使用共享 `StatusAnnouncer` 播报进行中和校验结果；普通进度使用 `role="status"` 和 `aria-live="polite"`，需要用户立即处理的错误继续使用具备 alert 语义的错误提示组件，避免同一消息重复播报。

纯图标按钮必须提供国际化的 `aria-label`。Tooltip 只用于视觉说明，不能代替按钮的可访问名称。

可拖拽分隔条必须同时支持键盘操作：使用 `role="separator"`、正确的 `aria-orientation`、`aria-valuemin/max/now` 和受控区域 `aria-controls`；方向键逐步调整，`Home` / `End` 跳到最小/最大尺寸，并提供清晰的 `:focus-visible` 状态。

Canvas DAG 编辑器必须提供不依赖鼠标的节点导航。画布获得焦点后，方向键按共享的画布位置顺序循环选择节点，Enter 打开节点配置，`Esc` 清除选择；当前节点名称和校验问题数量通过带名称的共享 `StatusAnnouncer` 播报。

DAG 连线不得要求进入全局“连线模式”。鼠标通过端口直接拖拽连线；键盘通过节点参数面板或配置抽屉维护输入连接或前置步骤。画布边是依赖关系的唯一编辑事实源，表单操作必须复用同一套环路、重复连接校验和历史记录。

---

## 性能优化规范

### 1. 组件懒加载

对于大型组件，使用懒加载：

```javascript
const MyComponent = defineAsyncComponent(() =>
  import('./components/MyComponent.vue')
)
```

### 2. 列表虚拟滚动

对于超长列表，使用虚拟滚动：

```vue
<template>
  <el-table-v2
    :columns="columns"
    :data="data"
    :width="700"
    :height="400"
  />
</template>
```

### 3. 图片懒加载

```vue
<template>
  <el-image
    :src="imageUrl"
    lazy
  />
</template>
```

---

## 国际化规范

### 1. Element Plus 国际化

在 `main.js` 中配置语言：

```javascript
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import ElementPlus from 'element-plus'

app.use(ElementPlus, { locale: zhCn })
```

### 2. 自定义国际化

使用 Vue I18n：

```javascript
import { createI18n } from 'vue-i18n'

const i18n = createI18n({
  locale: 'zh-CN',
  messages: {
    'zh-CN': {
      welcome: '欢迎'
    },
    'en-US': {
      welcome: 'Welcome'
    }
  }
})

app.use(i18n)
```

---

## 代码规范

### 1. 命名规范

- **组件文件名**：PascalCase（大驼峰），如 `UserList.vue`
- **组件名称**：与文件名一致
- **变量/函数**：camelCase（小驼峰），如 `userData`、`fetchUserList()`
- **常量**：UPPER_SNAKE_CASE（大写下划线），如 `API_BASE_URL`
- **CSS 类名**：kebab-case（短横线），如 `.user-list-container`

### 2. 文件组织

```
module/frontend/
├── src/
│   ├── api/              # API 接口
│   ├── assets/           # 静态资源
│   ├── components/       # 组件
│   ├── router/           # 路由
│   ├── store/            # 状态管理
│   ├── views/            # 页面
│   ├── utils/            # 工具函数
│   ├── App.vue
│   └── main.js
└── package.json
```

### 3. 代码注释

为复杂逻辑添加注释：

```javascript
/**
 * 获取用户列表
 * @param {Object} params - 查询参数
 * @param {number} params.page - 页码
 * @param {number} params.pageSize - 每页数量
 * @returns {Promise<Array>} 用户列表
 */
async function fetchUserList(params) {
  // 实现代码
}
```

---

## 测试规范

### 1. 单元测试

使用 Vitest 进行单元测试：

```javascript
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MyComponent from './MyComponent.vue'

describe('MyComponent', () => {
  it('renders properly', () => {
    const wrapper = mount(MyComponent, {
      props: { msg: 'Hello' }
    })
    expect(wrapper.text()).toContain('Hello')
  })
})
```

### 2. E2E 测试

使用 Playwright 进行端到端测试：

```javascript
import { test, expect } from '@playwright/test'

test('login flow', async ({ page }) => {
  await page.goto('http://localhost:5170')
  await page.fill('input[name="username"]', 'admin')
  await page.fill('input[name="password"]', '123456')
  await page.click('button[type="submit"]')
  await expect(page).toHaveURL(/console/)
})
```

---

## 版本控制规范

### 1. Git 提交消息

使用语义化提交消息：

```bash
# 新功能
feat(manager): 添加数据预览功能

# Bug 修复
fix(develop): 修复工作流编辑器保存失败问题

# 样式调整
style: 统一使用 CSS 变量替代硬编码颜色

# 重构
refactor(common-ui): 提取共享主题系统

# 文档
docs: 更新前端设计规范

# 性能优化
perf(meta): 优化元数据扫描性能
```

### 2. 分支策略

- `main`：主分支，生产环境代码
- `develop`：开发分支，集成最新功能
- `feature/*`：功能分支
- `fix/*`：修复分支

---

## 常见问题（FAQ）

### Q1: 为什么不能使用硬编码颜色？

**A**: 硬编码颜色会导致主题切换无法生效。所有颜色必须通过 CSS 变量引用，以支持主题切换（浅色/深色/蓝色/紫色模式）。

### Q2: 如何在模板中动态设置颜色？

**A**: 使用计算属性返回 CSS 变量名：

```vue
<template>
  <div :style="{ background: bgColor }">内容</div>
</template>

<script setup>
import { computed } from 'vue'

const bgColor = computed(() => 'var(--addp-bg-primary)')
</script>
```

### Q3: 如何覆盖 Element Plus 组件的样式？

**A**: 使用 `:deep()` 选择器：

```vue
<style scoped>
.my-table :deep(.el-table__header) {
  background: var(--addp-bg-secondary);
}
</style>
```

### Q4: 新增的 CSS 变量是否需要同步到所有主题？

**A**: 是的。在 `theme.css` 中新增变量时，必须同时为所有主题（`:root`、`html.dark`、`html.blue`、`html.purple`）定义值。

### Q5: 如何测试不同主题的效果？

**A**: 在浏览器中切换 Console 右上角的主题开关，选择不同主题查看效果。也可以直接在开发者工具中为 `<html>` 元素添加/移除主题 class（`dark`、`blue`、`purple`）。

### Q6: 为什么蓝色/紫色主题需要同时添加 `dark` class？

**A**: 蓝色和紫色主题是深色系主题，需要 Element Plus 的深色模式 CSS 支持。通过同时添加 `dark` class，可以复用 Element Plus 的深色模式样式，只需覆盖特定颜色变量即可。

### Q7: 如何新增自定义主题？

**A**: 按以下步骤新增：
1. 在 `common-frontend/basic/src/config/themes.js` 中添加主题配置
2. 在 `common-frontend/basic/src/styles/theme.css` 中添加对应的 CSS 变量定义（`html.{theme-name}` 选择器）
3. 在 `ThemeSwitcher.vue` 中导入对应的图标组件
4. 如果是深色系主题，设置 `isDarkTheme: true`

---

## 主题系统实施指南

### 关键实施步骤

#### 1. CSS 变量定义位置

每个模块的 `main.js` 必须导入统一的主题 CSS：

```javascript
// 导入 Element Plus 深色模式 CSS（静态导入，仅在 html.dark 时生效）
import 'element-plus/theme-chalk/dark/css-vars.css'
// 导入统一主题 CSS（包含所有主题的 CSS 变量定义）
import '@common-ui/styles/theme.css'
```

**重要说明**：
- `theme.css` 包含所有主题（`:root`、`html.dark`、`html.blue`、`html.purple`）的 CSS 变量定义
- `element-plus/theme-chalk/dark/css-vars.css` 仅在 `html.dark` 时生效
- 所有主题变量由 `common-frontend/basic/src/styles/theme.css` 统一管理

可选：如需在模块的 `App.vue` 中添加模块特定的覆盖样式：

```vue
<style>
/* 模块特定的样式覆盖（可选） */
html.dark .module-specific-component {
  background-color: var(--addp-bg-primary) !important;
}
</style>
```

#### 2. 布局组件中禁止硬编码背景色

**❌ 错误示例** - Layout.vue 中的硬编码背景：

```vue
<style scoped>
/* 错误：硬编码的浅色背景 */
.content-only {
  background: #f0f2f5;  /* 在切换主题时不会变化！*/
}
</style>
```

**✅ 正确示例**：

```vue
<style scoped>
.content-only {
  background: var(--addp-bg-secondary) !important;
}

.header {
  background: var(--addp-bg-primary) !important;
}

.sidebar {
  background: var(--addp-bg-primary) !important;
}

.main-content {
  background: var(--addp-bg-secondary) !important;
}
</style>
```

#### 3. 组件样式必须使用 !important

在组件级别设置背景色时，必须添加 `!important` 来覆盖 Element Plus 的默认样式：

```vue
<style scoped>
.preview-panel {
  background: var(--addp-bg-primary) !important;
}

.preview-panel :deep(.el-card) {
  background: var(--addp-bg-primary) !important;
  border-color: var(--addp-border-color) !important;
}

.data-explorer {
  background: var(--addp-bg-secondary) !important;
}
</style>
```

#### 4. iframe 模式特殊处理

在 `Layout.vue` 中，iframe 模式（`isInIframe` 为 true）的容器样式需要特别注意：

```vue
<template>
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>
</template>

<style scoped>
/* iframe 模式样式 */
.content-only {
  width: 100%;
  height: auto;
  min-height: 100vh;
  padding: 20px;
  margin: 0;
  background: var(--addp-bg-secondary) !important;  /* 必须使用 CSS 变量 */
  overflow: visible;
  box-sizing: border-box;
}
</style>
```

### 常见问题和解决方案

#### 问题 1：切换主题后某些区域颜色不变

**症状**：切换主题后，某些区域仍然显示原来的颜色。

**排查步骤**：
1. 打开浏览器开发者工具
2. 检查 HTML 元素是否有正确的主题 class：
   ```javascript
   // 深色模式
   document.documentElement.classList.contains('dark')  // 应该返回 true

   // 蓝色模式
   document.documentElement.className  // 应该是 "dark blue"

   // 紫色模式
   document.documentElement.className  // 应该是 "dark purple"
   ```
3. 检查 CSS 变量是否正确：
   ```javascript
   getComputedStyle(document.documentElement).getPropertyValue('--addp-bg-primary')
   // 深色模式应该返回 '#1D1E1F'
   // 蓝色模式应该返回 '#0f1629'
   // 紫色模式应该返回 '#1a1625'
   ```
4. 检查元素的实际背景色：
   ```javascript
   getComputedStyle(document.querySelector('.problem-element')).backgroundColor
   ```

**常见原因**：
- Layout 组件中有硬编码的背景色（如 `background: #f0f2f5`）
- Element Plus 组件的默认样式优先级更高，需要添加 `!important`
- CSS 变量未在 `theme.css` 中正确定义

**解决方案**：
1. 查找所有 `.vue` 文件中的硬编码颜色并替换为 CSS 变量
2. 在组件样式中添加 `!important`
3. 确保 `main.js` 中导入了 `@common-ui/styles/theme.css`

#### 问题 2：Console 与 iframe 模块主题不同步

**症状**：Console 切换主题后，嵌入的 iframe 模块仍保持原主题。

**排查步骤**：
1. 检查浏览器控制台是否有 postMessage 日志：
   ```
   [Console Theme] 广播主题变化到 X 个 iframe
   [Theme] 收到 Console 主题切换消息
   ```
2. 如果没有收到消息，检查模块的 main.js 是否正确初始化了 useTheme：
   ```javascript
   import { useTheme } from '@common-ui'

   const { init: initTheme } = useTheme({
     listenToConsole: true,
     storageKey: 'theme-mode'
   })

   initTheme()
   ```

**解决方案**：
- 确保每个模块的 main.js 都调用了 `useTheme().init()`
- 检查 useTheme 的 origin 验证逻辑是否正确（只验证 hostname，忽略端口差异）

#### 问题 3：CSS 变量值正确但界面仍不变化

**症状**：
- `getComputedStyle` 显示 CSS 变量值正确（如 `#1D1E1F`）
- 但元素的实际背景色是白色（`rgb(255, 255, 255)`）

**原因**：Element Plus 组件有更高优先级的内联样式或默认样式。

**解决方案**：在 `theme.css` 中添加全局覆盖（已在 `common-frontend/basic/src/styles/theme.css` 中定义）：

```css
html.dark,
html.blue,
html.purple {
  .el-card {
    background-color: var(--addp-bg-primary) !important;
  }

  .el-empty {
    background-color: transparent !important;
  }

  .el-pagination {
    background-color: transparent !important;
  }
}
```

### 验证清单

新增或修改模块时，使用以下清单验证主题系统是否正确实施：

- [ ] `main.js` 中导入了 `element-plus/theme-chalk/dark/css-vars.css`
- [ ] `main.js` 中导入了 `@common-ui/styles/theme.css`
- [ ] `main.js` 中正确初始化了 `useTheme`（包括 `listenToConsole: true`）
- [ ] Layout.vue 中所有容器使用 CSS 变量，无硬编码颜色
- [ ] 所有组件的背景色、文本色、边框色使用 CSS 变量
- [ ] 关键容器样式使用 `!important`
- [ ] 浏览器中切换所有主题（浅色/深色/蓝色/紫色），所有区域都正确变化
- [ ] 在 Console 中嵌入时，主题能正确同步

---

## 修订历史

| 版本 | 日期 | 修订内容 | 作者 |
|------|------|----------|------|
| 1.2.0 | 2026-08-10 | 新增 AI 助手入口一致性规范 | Codex |
| 1.1.0 | 2026-02-11 | 新增多主题系统支持（蓝色/紫色模式），完善主题系统架构说明 | Claude Code |
| 1.0.0 | 2026-02-10 | 初始版本 | Claude Code |

---

## 参考资料

- [Element Plus 官方文档](https://element-plus.org/)
- [Vue 3 官方文档](https://vuejs.org/)
- [CSS 变量（MDN）](https://developer.mozilla.org/zh-CN/docs/Web/CSS/Using_CSS_custom_properties)
- [WCAG 无障碍指南](https://www.w3.org/WAI/WCAG21/quickref/)
