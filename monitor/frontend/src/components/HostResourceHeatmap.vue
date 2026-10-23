<template>
  <section class="host-resource-heatmap" data-testid="resource-heatmap" :aria-label="t('monitor.resources.heatmap.title')" :aria-busy="loading">
    <header>
      <h3>{{ t('monitor.resources.heatmap.title') }}</h3>
      <div class="heatmap-legend" :aria-label="t('monitor.resources.heatmap.scale')">
        <span>0%</span><span class="heatmap-scale" aria-hidden="true" /><span>100%</span>
        <span>{{ t('monitor.resources.heatmap.scale') }}</span>
      </div>
    </header>
    <div class="heatmap-grid">
      <article v-for="entry in entries" :key="entry.id" class="heatmap-tile" :data-testid="`heatmap-${entry.name}`" :data-state="entry.point?.data_state || 'no_data'" :style="entry.value === null ? undefined : { borderTopColor: entry.color, background: `color-mix(in srgb, ${entry.color} 8%, var(--addp-bg-primary))` }">
        <h4>{{ title(entry) }}</h4>
        <p v-if="entry.dimensions" class="heatmap-mount">{{ entry.dimensions.device }} · {{ entry.dimensions.fstype }}</p>
        <strong>{{ entry.value === null ? '—' : formatFieldPresentationValue(entry.value, { unit: '%', precision: 2 }, locale) }}</strong>
        <el-progress v-if="entry.value !== null" :percentage="entry.value" :show-text="false" :stroke-width="12" :color="entry.color" :aria-label="title(entry)" />
        <div v-else class="heatmap-missing" aria-hidden="true" />
        <p>{{ t(`monitor.resources.states.${entry.point?.data_state || 'no_data'}`) }}</p>
        <p>{{ t('monitor.resources.sampledAt') }}: {{ formatFieldPresentationValue(entry.point?.sampled_at, { temporal_format: 'datetime' }, locale) }}</p>
        <el-button text type="primary" :aria-label="t('monitor.resources.heatmap.viewTrend', { resource: title(entry) })" @click="$emit('select', entry)">{{ t('monitor.resources.filesystem.viewTrend') }}</el-button>
      </article>
    </div>
    <p v-if="!partitions.length" class="heatmap-unavailable" role="status" data-testid="heatmap-filesystem-unavailable">{{ t(filesystemErrorKey || filesystemMessage || 'monitor.resources.heatmap.noPartitions') }}</p>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { pressureMetrics, currentResourceValue, resourceDimensionRows } from '../utils/nodeResources'

const props = defineProps({ instant: { type: Object, required: true }, filesystems: { type: Object, default: null }, filesystemErrorKey: { type: String, default: '' }, filesystemMessage: { type: String, default: '' }, loading: Boolean })
defineEmits(['select'])
const { t, locale } = useI18n()
const partitions = computed(() => resourceDimensionRows(props.filesystems))
const entries = computed(() => [
  ...pressureMetrics.map(metric => ({ id: metric.key, name: metric.name, metric: metric.key, point: props.instant.series.find(series => series.metric_key === metric.key)?.points[0] })),
  ...partitions.value.map(row => ({ id: row.key, name: 'partition', metric: 'node.filesystem.used_percent', dimensions: row.dimensions, point: row.metrics['node.filesystem.used_percent'] }))
].map(entry => {
  const value = currentResourceValue(entry.point)
  return { ...entry, value, color: value === null ? undefined : `color-mix(in srgb, var(--el-color-danger) ${value}%, var(--el-color-success))` }
}))
const title = entry => entry.dimensions ? entry.dimensions.mountpoint : t(`monitor.resources.metrics.${entry.name}`)
</script>

<style scoped>
.host-resource-heatmap { margin: 20px 0; min-width: 0; }
header { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
header h3 { flex: 1; margin: 0; }
.heatmap-legend { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; color: var(--addp-text-secondary); font-size: 13px; }
.heatmap-scale { display: inline-block; width: 90px; height: 8px; border-radius: 4px; background: linear-gradient(to right, var(--el-color-success), var(--el-color-danger)); }
.heatmap-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(220px, 100%), 1fr)); gap: 12px; margin-top: 12px; }
.heatmap-tile { min-width: 0; padding: 16px; border: 1px solid var(--addp-border-color); border-top: 4px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-primary); overflow-wrap: anywhere; }
h4 { margin: 0 0 8px; }
strong { display: block; font-size: 22px; margin: 12px 0; }
p { color: var(--addp-text-secondary); font-size: 13px; margin: 8px 0; }
.heatmap-mount { overflow-wrap: anywhere; }
.heatmap-missing { height: 12px; border-radius: 6px; background: var(--el-fill-color-light); }
.heatmap-unavailable { margin-bottom: 0; }
</style>
