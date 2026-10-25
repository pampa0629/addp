<template>
  <div class="host-node-filters">
    <el-select class="node-filter" :model-value="nodeKind" :empty-values="[null, undefined]" :aria-label="t('common.hostNodeFilters.kind')" @update:model-value="value => emit('change', 'node_kind', value)">
      <el-option value="" :label="t('common.hostNodeFilters.allKinds')" />
      <el-option v-for="kind in hostNodeKinds" :key="kind" :value="kind" :label="t(`common.hostNodeFilters.kinds.${kind}`)" />
    </el-select>
    <el-select class="node-filter" :model-value="enabled" :empty-values="[null, undefined]" :aria-label="t('common.hostNodeFilters.state')" @update:model-value="value => emit('change', 'enabled', value)">
      <el-option value="" :label="t('common.hostNodeFilters.allStates')" />
      <el-option value="true" :label="t('common.hostNodeFilters.enabled')" />
      <el-option value="false" :label="t('common.hostNodeFilters.disabled')" />
    </el-select>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { hostNodeKinds } from '../utils/hostNodeFilters.mjs'
defineProps({ nodeKind: { type: String, default: '' }, enabled: { type: String, default: '' } })
const emit = defineEmits(['change'])
const { t } = useI18n()
</script>

<style scoped>
.host-node-filters { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; min-width: 0; max-width: 100%; }
.host-node-filters .el-select { width: 180px; max-width: 100%; }
@media (max-width: 600px) { .host-node-filters { width: 100%; } .host-node-filters .el-select { flex: 1; min-width: 0; } }
</style>
