<template>
  <div class="runtime-instance-time-filter">
    <div class="runtime-instance-time-controls">
      <el-select v-model="basisModel" :aria-label="t('common.runtimeInstanceTime.timeBasis')" @change="emit('change')">
        <el-option v-for="basis in ['registered', 'offline']" :key="basis" :value="basis" :label="t(`common.runtimeInstanceTime.${basis}Time`)" />
      </el-select>
      <FieldHelp :content="t(`common.runtimeInstanceTime.${timeBasis}Hint`)" />
      <el-select v-model="periodModel" :aria-label="t('common.runtimeInstanceTime.timeRange')" @change="emit('change')">
        <el-option value="all" :label="t('common.runtimeInstanceTime.allTime')" />
        <el-option v-for="item in runtimeInstancePeriods" :key="item.value" :value="item.value" :label="t(item.label)" />
        <el-option value="custom" :label="t('common.runtimeInstanceTime.custom')" />
      </el-select>
      <el-date-picker v-if="period === 'custom'" v-model="rangeModel" type="datetimerange"
        :start-placeholder="t('common.runtimeInstanceTime.from')" :end-placeholder="t('common.runtimeInstanceTime.to')" @change="emit('change')" />
    </div>
    <p v-if="period === 'custom' && !validRuntimeInstanceRange(customRange)" class="runtime-instance-time-hint" role="status">{{ t('common.runtimeInstanceTime.completeRange') }}</p>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import FieldHelp from './FieldHelp.vue'
import { useI18n } from 'vue-i18n'
import { runtimeInstancePeriods, validRuntimeInstanceRange } from '../utils/runtimeInstanceTime.mjs'
const props = defineProps({ timeBasis: { type: String, default: 'registered' }, period: { type: String, default: 'all' }, customRange: { type: Array, default: null } })
const emit = defineEmits(['update:timeBasis', 'update:period', 'update:customRange', 'change'])
const { t } = useI18n()
const basisModel = computed({ get: () => props.timeBasis, set: value => emit('update:timeBasis', value) })
const periodModel = computed({ get: () => props.period, set: value => emit('update:period', value) })
const rangeModel = computed({ get: () => props.customRange, set: value => emit('update:customRange', value) })
</script>

<style scoped>
.runtime-instance-time-filter { min-width: 0; max-width: 100%; }
.runtime-instance-time-controls { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.runtime-instance-time-controls .el-select { width: 200px; max-width: 100%; }
.runtime-instance-time-controls :deep(.el-date-editor) { width: 380px; max-width: 100%; box-sizing: border-box; }
.runtime-instance-time-hint { margin: 4px 0 0; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
@media (max-width: 600px) { .runtime-instance-time-controls .el-select { width: calc((100% - 44px) / 2); min-width: 100px; } }
</style>
