<template>
  <el-tooltip :content="content" :trigger="['hover', 'focus']" placement="top" :show-after="150" :popper-style="{ maxWidth: 'min(320px, calc(100vw - 32px))', overflowWrap: 'anywhere', whiteSpace: 'normal' }">
    <button type="button" class="field-help" :aria-label="content"><el-icon><QuestionFilled /></el-icon></button>
  </el-tooltip>
</template>
<script setup>
import { ElIcon, ElTooltip } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'
defineProps({ content: { type: String, required: true } })
</script>
<style scoped>
.field-help { display: inline-flex; align-items: center; justify-content: center; flex: 0 0 auto; width: 24px; height: 24px; border: 0; background: transparent; color: var(--addp-text-secondary); padding: 3px; border-radius: 4px; cursor: help; font-size: 15px; }
.field-help:hover, .field-help:focus-visible { color: var(--el-color-primary); }
.field-help:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 1px; }
</style>
