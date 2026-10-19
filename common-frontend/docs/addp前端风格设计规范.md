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

## 阅读导航

| 阅读目的 | 专题 |
| --- | --- |
| 颜色变量、页面布局、字体、图标和响应式适配 | [视觉与布局](frontend-style/视觉与布局.md) |
| 业务域选择、AI 助手入口、表单、表格、按钮、对话框、消息和无障碍交互 | [组件交互](frontend-style/组件交互.md) |
| 主题模式、共享初始化、Element Plus 覆盖、iframe 同步和排障验收 | [主题接入](frontend-style/主题接入.md) |
| 共享组件唯一所有权、样式隔离、性能、国际化、代码、测试和版本控制 | [工程约束](frontend-style/工程约束.md) |

按任务阅读对应专题；共享能力与组件清单见[共享前端说明](../README.md)。

## 参考资料

- [Element Plus 官方文档](https://element-plus.org/)
- [Vue 3 官方文档](https://vuejs.org/)
- [CSS 变量（MDN）](https://developer.mozilla.org/zh-CN/docs/Web/CSS/Using_CSS_custom_properties)
- [WCAG 无障碍指南](https://www.w3.org/WAI/WCAG21/quickref/)
