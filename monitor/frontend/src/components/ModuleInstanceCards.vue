<template>
  <div class="module-instance-grid">
    <el-card v-for="[name, rows] in groups" :key="name" shadow="never" :data-testid="`${testPrefix}-module-card`">
      <template #header><h3>{{ name }}</h3></template>
      <article v-for="row in rows" :key="row.id" class="module-instance" :aria-label="instanceName(row)" :data-testid="`${testPrefix}-instance`">
        <header class="module-instance-header">
          <div>
            <strong :title="row.instance_id">{{ instanceName(row) }}</strong>
            <p class="role" :title="row.instance_id" :data-testid="`${testPrefix}-role`">{{ t(processRoleLabelKey(row)) }}</p>
          </div>
          <el-tag :data-testid="`${testPrefix}-online`" size="small" :type="row.status === 'up' ? 'success' : 'danger'">{{ row.status.toUpperCase() }} · {{ t(`monitor.process.status.${row.status}`) }}</el-tag>
        </header>
        <slot :row="row" />
      </article>
    </el-card>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { processRoleLabelKey } from '../utils/processResources'

const props = defineProps({
  instances: { type: Array, required: true },
  testPrefix: { type: String, required: true }
})
const { t } = useI18n()
const instanceName = row => row.runtime_hostname || row.registered_host || row.instance_id
const groups = computed(() => {
  const byModule = new Map()
  for (const row of props.instances) {
    if (!byModule.has(row.module_name)) byModule.set(row.module_name, [])
    byModule.get(row.module_name).push(row)
  }
  return byModule
})
</script>

<style scoped>
.module-instance-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 320px), 1fr)); align-items: start; gap: 16px; }
h3 { margin: 0; overflow-wrap: anywhere; }
.module-instance { min-width: 0; overflow-wrap: anywhere; }
.module-instance + .module-instance { margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--addp-border-color); }
.module-instance-header { display: flex; justify-content: space-between; align-items: flex-start; flex-wrap: wrap; gap: 8px; }
.role { margin: 4px 0; font-size: 12px; line-height: 1.5; color: var(--addp-text-secondary); }
.module-instance :deep(.instance-metrics) { display: grid; grid-template-columns: repeat(var(--instance-metric-columns, 2), minmax(0, 1fr)); gap: 8px; margin: 12px 0; }
.module-instance :deep(.instance-metrics dt) { font-size: 12px; color: var(--addp-text-secondary); }
.module-instance :deep(.instance-metrics dd) { margin: 4px 0 0; }
.module-instance :deep(summary) { cursor: pointer; color: var(--addp-text-secondary); }
</style>
