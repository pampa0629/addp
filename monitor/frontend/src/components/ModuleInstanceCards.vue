<template>
  <div class="module-instance-grid" :class="{ overview: $slots.overview && !detailModule, detail: detailModule }">
    <component :is="$slots.overview ? 'section' : ElCard" shadow="never" v-for="[name, rows] in groups" :key="name" class="module-group" :data-module="name" :data-testid="`${testPrefix}-module-card`">
      <template v-if="!$slots.overview" #header><h3>{{ name }}</h3></template>
      <button v-if="$slots.overview && !detailModule" type="button" class="module-summary" :aria-label="t('monitor.process.moduleDetail', { module: name })" @click="$emit('select', name)">
        <h3>{{ name }}<span class="chevron" aria-hidden="true">›</span></h3>
        <slot name="overview" :module-name="name" :rows="rows" />
      </button>
      <div v-show="!$slots.overview || detailModule" class="module-details">
        <template v-for="(row, index) in rows" :key="row.id">
          <slot v-if="row.role === 'worker' && rows[index - 1]?.role !== 'worker'" name="frontend" :module-name="name" />
          <article :data-instance-id="row.id" class="module-instance" :aria-label="instanceName(row)" :data-testid="`${testPrefix}-instance`">
            <header class="module-instance-header">
              <div>
                <strong :title="row.instance_id">{{ instanceName(row) }}</strong>
                <p class="role" :title="row.instance_id" :data-testid="`${testPrefix}-role`">{{ t(processRoleLabelKey(row)) }}</p>
              </div>
              <el-tag :data-testid="`${testPrefix}-online`" size="small" :type="row.status === 'up' ? 'success' : 'danger'">{{ row.status.toUpperCase() }} · {{ t(`monitor.process.status.${row.status}`) }}</el-tag>
            </header>
            <slot :row="row" />
          </article>
        </template>
        <slot v-if="!rows.some(row => row.role === 'worker')" name="frontend" :module-name="name" />
      </div>
    </component>
  </div>
</template>

<script setup>
import { ElCard } from 'element-plus'
import { computed, useSlots } from 'vue'
import { useI18n } from 'vue-i18n'
import { processRoleLabelKey } from '../utils/processResources'

defineEmits(['select'])
const props = defineProps({
  detailModule: { type: String, default: '' },
  instances: { type: Array, required: true },
  frontendModules: { type: Array, default: () => [] },
  testPrefix: { type: String, required: true }
})
const slots = useSlots()
const { t } = useI18n()
const instanceName = row => row.runtime_hostname || row.registered_host || row.instance_id
const groups = computed(() => {
  const byModule = new Map()
  for (const row of props.instances) {
    if (!byModule.has(row.module_name)) byModule.set(row.module_name, [])
    byModule.get(row.module_name).push(row)
  }
  for (const name of props.frontendModules) {
    if (!byModule.has(name)) byModule.set(name, [])
  }
  if (!slots.overview) return byModule
  for (const rows of byModule.values()) rows.sort((a, b) => Number(a.role === 'worker') - Number(b.role === 'worker'))
  if (props.detailModule) return byModule.has(props.detailModule) ? [[props.detailModule, byModule.get(props.detailModule)]] : []
  return [...byModule].sort(([a], [b]) => a.localeCompare(b))
})
</script>

<style scoped>
.module-instance-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 320px), 1fr)); align-items: start; gap: 16px; }
.module-group { min-width: 0; }
.overview .module-group { border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-secondary); }
.detail { display: block; }
.detail .module-details > * { padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-secondary); }
.detail .module-details { display: grid; gap: 12px; }
.detail .module-instance + .module-instance { margin-top: 0; }
.overview { grid-template-columns: repeat(auto-fill, minmax(min(100%, 290px), 1fr)); }
.module-summary { box-sizing: border-box; width: 100%; height: 184px; padding: 16px; cursor: pointer; border: 0; background: transparent; color: inherit; font: inherit; text-align: start; }
.module-summary:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: -2px; }
.module-summary h3 { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 10px; }
.chevron { color: var(--addp-text-secondary); }
h3 { margin: 0; overflow-wrap: anywhere; }
.module-instance { min-width: 0; overflow-wrap: anywhere; }
.module-instance + .module-instance { margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--addp-border-color); }
.module-instance-header { display: flex; justify-content: space-between; align-items: flex-start; flex-wrap: wrap; gap: 8px; }
.role { margin: 4px 0; font-size: 12px; line-height: 1.5; color: var(--addp-text-secondary); }
.module-details :deep(.instance-metrics) { display: grid; grid-template-columns: repeat(var(--instance-metric-columns, 2), minmax(0, 1fr)); gap: 8px; margin: 12px 0; }
.module-details :deep(.instance-metrics dt) { font-size: 12px; color: var(--addp-text-secondary); }
.module-details :deep(.instance-metrics dd) { margin: 4px 0 0; }
.module-instance :deep(summary) { cursor: pointer; color: var(--addp-text-secondary); }
</style>
