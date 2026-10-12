<template>
  <div class="auto-refresh-control">
    <el-select :model-value="modelValue" :aria-label="t('autoRefresh.interval')" :data-testid="testId" @update:model-value="value => emit('update:modelValue', value)">
      <el-option v-for="value in autoRefreshOptions" :key="value" :value="value" :label="value === 'off' ? t('autoRefresh.off') : t('autoRefresh.seconds', { seconds: value })" />
    </el-select>
    <span :data-testid="`${testId}-status`">{{ t(`autoRefresh.${status}`) }}</span>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { autoRefreshOptions } from '../utils/autoRefresh.mjs'
defineProps({ modelValue: { type: String, required: true }, status: { type: String, required: true }, testId: { type: String, default: 'auto-refresh' } })
const emit = defineEmits(['update:modelValue']), { t } = useI18n()
</script>

<style scoped>
.auto-refresh-control { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; min-width: 0; }
.el-select { width: 200px; max-width: 100%; }
span { font-size: 12px; color: var(--addp-text-secondary); }
</style>
