<template>
  <section data-testid="container-resources">
    <h3>{{ t('monitor.container.title') }}</h3>
    <p class="hint">{{ t('monitor.container.scope') }}</p>
    <el-alert v-if="errorKey" :title="t(errorKey)" type="warning" :closable="false" />
    <template v-else-if="value">
      <p class="hint" data-testid="container-proof-state">{{ t(`monitor.container.${value.proof_state}`) }}</p>
      <p v-if="value.proof_observed_at" class="hint">{{ t('monitor.container.proofTime', { value: date(value.proof_observed_at) }) }}</p>
      <p v-if="value.observation.collection.state === 'not_connected'" class="hint" data-testid="container-not-connected">{{ t('monitor.container.notConnected') }}</p>
      <div v-else class="metrics">
        <article v-for="metric in containerMetrics" :key="metric.key" :data-testid="`container-${metric.name}`">
          <h4>{{ t(`monitor.container.metrics.${metric.name}`) }}</h4>
          <strong>{{ display(current(metric), metric.unit) }}</strong>
          <p class="hint">{{ t(`monitor.container.hints.${metric.name}`) }}</p>
          <p v-if="metric.name.endsWith('Ratio')" class="hint">{{ t('monitor.container.bound', { value: display(metric.name === 'cpuRatio' ? value.cpu_upper_cores : value.memory_upper_bytes, metric.name === 'cpuRatio' ? 'cores' : 'bytes') }) }}</p>
          <p v-if="current(metric) === null" class="hint" data-testid="container-missing">{{ missing(metric) }}</p>
        </article>
      </div>
    </template>
  </section>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { formatBytes } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { containerMetrics } from '../utils/containerResources'
import { currentResourceValue } from '../utils/nodeResources'
const props = defineProps({ value: { type: Object, default: null }, errorKey: { type: String, default: '' } })
const { t, locale } = useI18n()
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
const current = metric => currentResourceValue(props.value?.observation.series.find(row => row.metric_key === metric.key)?.points[0])
const display = (value, unit) => value === null ? '—' : unit === 'bytes' ? formatBytes(value, 2, locale.value) : formatFieldPresentationValue(value, { precision: 2, unit: unit === 'percent' ? '%' : t('monitor.resources.units.cores') }, locale.value)
function missing(metric) {
  if (props.value.proof_state !== 'verified' && metric.name.endsWith('Ratio')) return t('monitor.container.noProof')
  if (metric.name === 'cpuRatio' && props.value.cpu_upper_cores === null || metric.name === 'memoryRatio' && props.value.memory_upper_bytes === null) return t('monitor.container.noLimit')
  if (metric.name === 'cpuRatio' && !props.value.cpu_window_verified) return t('monitor.container.window')
  return t('monitor.container.noSample')
}
</script>

<style scoped>
.metrics { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; }
article { border: 1px solid var(--addp-border-color); border-radius: 8px; padding: 12px; }
h4 { margin: 0 0 8px; } strong { font-size: 20px; }
.hint { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
</style>
