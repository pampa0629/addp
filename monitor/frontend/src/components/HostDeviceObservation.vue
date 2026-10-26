<template>
  <div class="host-device-observation" data-testid="host-device-observation" v-loading="loading">
    <header>
      <span v-if="observation" class="resource-hint" data-testid="resource-device-queried-at">{{ t('monitor.resources.queriedAt') }}: {{ formatFieldPresentationValue(observation.queried_at, { temporal_format: 'datetime' }, locale) }}</span>
      <el-button :loading="loading" data-testid="resource-device-refresh" @click="reload()">{{ t('monitor.resources.devices.refresh') }}</el-button>
    </header>
    <HostDeviceResources :family-name="familyName" :observation="observation" :error-key="errorKey" :loading="loading" :show-all-disks="showAllDisks" :format-value="formatValue" @update:show-all-disks="$emit('update:showAllDisks', $event)" @select="(row, metric) => $emit('select', row, metric)" />
  </div>
</template>

<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import HostDeviceResources from './HostDeviceResources.vue'
import { nodeResourcesAPI as api } from '../api/nodeResources'
import { deviceMetricSets, resourceErrorKey, validateCollection, validateResourceResponse } from '../utils/nodeResources'

const props = defineProps({
  node: { type: Object, required: true }, familyName: { type: String, required: true }, identity: { type: String, required: true },
  refresh: { type: String, required: true }, showAllDisks: Boolean, formatValue: { type: Function, required: true }
})
const emit = defineEmits(['update:showAllDisks', 'select', 'denied'])
const { t, locale } = useI18n()
const observation = ref(null), loading = ref(false), errorKey = ref('')
const scope = createLatestRequestScope()
let disposed = false, blocked = false
const refreshCycle = createAutoRefresh({
  interval: () => props.refresh,
  enabled: () => !disposed && !blocked && !loading.value,
  refresh: () => reload(),
  pause: () => { scope.invalidate(); loading.value = false }
})
async function reload() {
  refreshCycle.stop()
  const ticket = scope.invalidate()
  observation.value = null; errorKey.value = ''; loading.value = false; blocked = false
  if (disposed) return
  const { node_id: nodeID, version } = props.node
  const metrics = deviceMetricSets[props.familyName]
  if (!metrics) { errorKey.value = 'monitor.resources.errors.invalidResponse'; return }
  loading.value = true
  try {
    const keys = metrics.map(metric => metric.key)
    const value = await scope.request(config => api.instant({ node_id: nodeID, metrics: keys.join(',') }, config))
    if (!scope.current(ticket)) return
    validateCollection(value.collection, value.queried_at)
    validateResourceResponse(value, nodeID, keys)
    if (value.node_version !== version) throw new Error('invalid_resource_response')
    observation.value = value
  } catch (error) {
    if (!scope.current(ticket)) return
    errorKey.value = resourceErrorKey(error)
    if ([401, 403, 404].includes(error.response?.status)) { blocked = true; emit('denied', error) }
  } finally {
    if (scope.current(ticket)) { loading.value = false; refreshCycle.schedule() }
  }
}
watch(() => JSON.stringify([props.node.node_id, props.node.version, props.node.enabled, props.familyName, props.identity]), reload, { immediate: true, flush: 'sync' })
watch(() => props.refresh, () => refreshCycle.schedule())
onBeforeUnmount(() => { disposed = true; refreshCycle.dispose(); scope.dispose() })
</script>

<style scoped>
.host-device-observation { min-width: 0; }
header { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 12px; }
.resource-hint { color: var(--addp-text-secondary); font-size: 13px; }
</style>
