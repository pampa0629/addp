<template>
  <div class="filter-bar" data-testid="engine-filters">
    <div class="filter-group" role="group" :aria-label="t('system.engine.filters.capabilities')">
      <span class="filter-label">{{ t('system.engine.filters.capabilities') }}</span>
      <el-checkbox-group :model-value="modelValue.capabilityGroups" @update:model-value="update('capabilityGroups', $event)">
        <el-checkbox v-for="group in ENGINE_CAPABILITY_GROUPS" :key="group" :value="group">
          {{ t(group === 'storage' ? 'system.engine.filterStorage' : 'system.engine.filterCompute') }}
        </el-checkbox>
      </el-checkbox-group>
    </div>
    <div class="filter-group" role="group" :aria-label="t('system.engine.filters.origin')">
      <span class="filter-label">{{ t('system.engine.filters.origin') }}</span>
      <el-checkbox-group :model-value="modelValue.engineOrigins" @update:model-value="update('engineOrigins', $event)">
        <el-checkbox v-for="origin in ENGINE_ORIGINS" :key="origin" :value="origin">
          {{ t(`system.engine.category.${origin}`) }}
        </el-checkbox>
      </el-checkbox-group>
    </div>
    <div class="filter-group" role="group" :aria-label="t('system.engine.filters.registration')">
      <span class="filter-label">{{ t('system.engine.filters.registration') }}</span>
      <el-radio-group :model-value="modelValue.registration" @update:model-value="update('registration', $event)">
        <el-radio value="all">{{ t('common.all') }}</el-radio>
        <el-radio value="registered">{{ t('system.engine.builtin.registered') }}</el-radio>
        <el-radio value="builtin">{{ t('system.engine.builtin.builtin') }}</el-radio>
      </el-radio-group>
    </div>
    <div class="filter-group" role="group" :aria-label="t('system.engine.filters.lifecycle')">
      <span class="filter-label">{{ t('system.engine.filters.lifecycle') }}</span>
      <el-checkbox-group :model-value="modelValue.lifecycleStates" @update:model-value="update('lifecycleStates', $event)">
        <el-checkbox v-for="state in ENGINE_LIFECYCLE_STATES" :key="state" :value="state">
          {{ t(`system.engine.status.${state}`) }}
        </el-checkbox>
      </el-checkbox-group>
    </div>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { ENGINE_CAPABILITY_GROUPS, ENGINE_ORIGINS, ENGINE_LIFECYCLE_STATES } from '../../utils/engineList'

const props = defineProps({ modelValue: { type: Object, required: true } })
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const update = (key, value) => emit('update:modelValue', { ...props.modelValue, [key]: value })
</script>

<style scoped>
.filter-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 28px;
  padding: 16px 0;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--addp-border-color-light);
}

.filter-group {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px 12px;
  min-width: 0;
}

.filter-label {
  color: var(--addp-text-secondary);
  font-weight: 500;
}

.filter-group :deep(.el-checkbox-group),
.filter-group :deep(.el-radio-group) {
  display: flex;
  flex-wrap: wrap;
  gap: 0 16px;
}

.filter-group :deep(.el-checkbox),
.filter-group :deep(.el-radio) {
  margin-right: 0;
}
</style>
