# ScheduleConfig 定时调度组件使用文档

## 概述

`ScheduleConfig` 是共享 Cron 配置组件，`ScheduleDisplay` 负责只读展示。任务配置应复用这两个组件；组件不保存任务、不调用调度 API，也不决定任务是否具有调度能力。

实现入口：[ScheduleConfig.vue](../src/components/ScheduleConfig.vue)、[ScheduleDisplay.vue](../src/components/ScheduleDisplay.vue)、[schedule.js](../src/utils/schedule.js)。

## 字段与数据格式

任务定义和提交 DTO 的调度字段统一使用 `schedule`，字段与启用状态的归属遵循[调度规范](../../../docs/spec/tasks/编排与调度.md#调度规范)。组件可以绑定宿主的局部变量，再由宿主组装 owner DTO；不在组件文档中维护另一份数据库模型或字段迁移流程。

- `v-model` 为 Cron 字符串，清空操作发出空字符串；空计划的手动触发语义、`enabled` 和 `next_run_at` 由 owner 维护。
- 默认预设和 daily / weekly / monthly 表单生成 5 字段表达式：`分 时 日 月 周`；周值为 `0–7`，0 和 7 表示周日，表单使用字符串 `"0"` 表示周日。
- 自定义 Cron 输入由 `allowCustomCron` 开启。当前前端粗校验接受 5 或 6 个字段，不能据此推断后端支持秒级；实际精度由 owner 的校验与调度配置决定。Transfer 已有自己的 6 字段预设，见[实际消费示例](../../../transfer/frontend/src/views/TaskWizard/Step4Configure.vue)。
- 支持每天、每周、每月，以及启用后的自定义 Cron 模式。内置预设当前为 7 个，完整列表以 `presetOptions` 为准，不复制另一份预设清单。

## 基础用法

组件和工具函数从 `@common-ui` 导入，别名、依赖和语言初始化见[共享前端架构](../../docs/ARCHITECTURE.md)与[国际化规范](../../../docs/spec/addp国际化开发规范.md#前端规范)。下面示例假定宿主已初始化 Vue I18n 和 Element Plus：

```vue
<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ScheduleConfig, ScheduleDisplay } from '@common-ui'

const { t } = useI18n()
const form = ref({ schedule: '' })
</script>

<template>
  <el-form :model="form">
    <el-form-item :label="t('schedule.title')">
      <ScheduleConfig
        v-model="form.schedule"
        :allow-custom-cron="true"
      />
    </el-form-item>
  </el-form>
  <ScheduleDisplay :cron="form.schedule" />
</template>
```

组件内部已有自定义配置对话框；表单嵌入与表格只读展示共用上述入口。当前组件未通过 `defineExpose` 提供 `openDialog()`，不要通过组件 ref 调用内部方法。

## 组件属性与事件

### ScheduleConfig

| 属性 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `modelValue` | String | `''` | Cron 字符串，通过 `v-model` 更新。 |
| `allowCustomCron` | Boolean | `false` | 显示自定义 Cron 输入模式；每天、每周、每月仍可配置。 |
| `showPresets` | Boolean | `true` | 控制快捷按钮、配置入口及当前计划区域的显示；设为 false 不会自动打开对话框。 |
| `presetList` | Array | `null` | null 使用内置预设，传数组则使用该列表。 |
| `compactMode` | Boolean | `false` | 紧凑间距。 |
| `disabled` | Boolean | `false` | 禁用交互。 |
| `readonly` | Boolean | `false` | 只读；详情或表格优先使用 ScheduleDisplay。 |

唯一事件为 `update:modelValue`，确认、选择预设和清空操作返回字符串。提交任务、请求失败、启用/停用及保存后的刷新均由宿主处理。

### ScheduleDisplay

| 属性 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `cron` | String | `''` | 非空时显示调度描述。 |
| `emptyText` | String | `''` | 空计划的展示文案；未提供时使用当前语言的手动触发词条。 |

### 自定义预设

`presetList` 每项使用 `key`、`label`、`cron`、`description`，不能用 `value` 替代 `cron`。key 在列表内唯一；label 和 description 由宿主按当前语言提供，内置列表则由组件根据 `i18nKey` 翻译。例如在已取得 `t()` 的宿主中定义一项：

```javascript
const hourlyPreset = {
  key: 'hourly',
  label: t('schedule.presetLabel.hourly'),
  cron: '0 * * * *',
  description: t('schedule.presetDesc.hourly')
}
```

将列表绑定给 `:preset-list`；需要运行时切换语言时，由宿主使用计算属性生成当前语言的列表。定制预设不改变 owner 接受的 Cron 精度。

## 工具函数

以下函数由 `@common-ui` 导出，具体实现只在 `schedule.js` 维护：

| 函数 | 返回值与边界 |
| --- | --- |
| `describeCron(cron, t?)` | 返回本地化描述；不传 t 时回退中文。未知表达式显示自定义调度文案，不是合法性诊断。 |
| `validateCron(cron)` | 空值返回 false；当前仅检查是否为 5 或 6 个字段，不校验字段语法或数值范围，不能替代后端校验。 |
| `buildScheduleFromForm(form, t?)` | 仅处理 daily / weekly / monthly，返回 `{ cron, description }` 或 null；不会直接返回字符串，不处理 cron 模式，也不能替代严格输入校验。 |
| `decodeScheduleToForm(cron)` | 只还原可识别的 5 字段每日、每周、每月配置，返回表单对象或 null；`weekDays` 是字符串数组，不能保证任意 Cron 往返还原。 |
| `generateScheduleDescription(form, t?)` | 生成表单预览；不完整时返回当前语言的提示。 |

清空、预设与辅助函数的展示结果不能作为后端接受表达式或任务已成功保存的证明。

## 后端与宿主边界

任务 owner 必须按自己的 API 契约校验和保存 `schedule`，维护 `enabled` 与 `next_run_at`。用户任务采用 DB-driven due task claim 主路径；到期后创建 execution，由合法执行方领取，不能把保存任务时注册的内存业务回调作为正式调度主路径。完整闭环以[编排与调度规范](../../../docs/spec/tasks/编排与调度.md#调度规范)及[执行记录与租约](../../../docs/spec/tasks/执行记录与租约.md)为准。

`common/scheduler` 是后端 Cron 校验与下次执行时间计算的统一封装；接口与精度选项以[源码](../../../common/scheduler/scheduler.go)为准。本前端说明不定义通用 `/api/tasks`、错误响应或数据库建表模板。

## 接入检查与排查

- 快捷项或自定义输入不出现：检查 `showPresets`、`allowCustomCron` 和自定义列表字段；默认不启用手工 Cron 输入。
- 预览语言不一致：确认宿主已初始化共享国际化，并向直接调用的描述函数传入当前 t；自定义预设文案也由宿主更新。
- 前端接受、后端拒绝：核对 owner 要求的字段数与完整 Cron 语法，不能通过前端粗校验推断合法性。
- 需要详情或表格展示：使用 ScheduleDisplay；不要给每个模块复制标签、手动触发文案或内部对话框。
- 样式调整：沿用[主题与样式规范](../../docs/frontend-style/主题接入.md)，使用共享变量和受限 scoped / `:deep()` 覆盖，不复制固定背景色。

共享实现验证使用 `make test-common-frontend`；实际消费页面按[测试与验收规范](../../../docs/spec/addp测试与验收规范.md)选择 owner 门禁。静态检查、组件测试、浏览器交互与真实调度执行分别记录，不互相替代。
