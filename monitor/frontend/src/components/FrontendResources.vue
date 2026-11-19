<template>
  <section data-testid="frontend-resources">
    <div class="frontend-toolbar">
      <el-select v-if="nodes.length || nodeId" :model-value="nodeId" filterable
        :aria-label="t('monitor.frontend.selectNode')" :placeholder="t('monitor.frontend.selectNode')"
        @update:model-value="emit('update:nodeId', $event)">
        <el-option v-for="node in options" :key="node.node_id" :value="node.node_id" :label="node.display_name" />
      </el-select>
      <el-button :loading="loading || nodesLoading" @click="refreshView">{{ t('common.refresh') }}</el-button>
      <el-button v-if="canConfigure" @click="emit('configure')">{{ t('monitor.frontend.configure') }}</el-button>
    </div>
    <el-alert v-if="errorKey" :title="t(errorKey)" type="warning" :closable="false" />
    <el-alert v-else-if="!nodeId && nodesError" :title="t(nodesError)" type="warning" :closable="false" />
    <el-empty v-else-if="!nodeId && !nodesLoading && !nodes.length" :description="t('monitor.frontend.noConfiguredNodes')" :image-size="64" />
    <template v-else-if="observed">
      <el-alert v-if="observed.source_state !== 'collecting'" :title="t(`monitor.frontend.states.${observed.source_state}`)" type="info" :closable="false" />
      <el-table v-else :data="observed.data" border>
        <el-table-column prop="module" :label="t('monitor.process.module')" min-width="130" />
        <el-table-column :label="t('monitor.frontend.runtime')" min-width="140">
          <template #default="{ row }">{{ row.mode ? t(`monitor.frontend.modes.${row.mode}`) : '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.frontend.entry')" min-width="110">
          <template #default="{ row }"><el-tag :type="tagType(row.entry_up)">{{ reachability(row.entry_up) }}</el-tag></template>
        </el-table-column>
        <el-table-column :label="t('monitor.frontend.asset')" min-width="120">
          <template #default="{ row }"><el-tag :type="tagType(row.asset_up)">{{ reachability(row.asset_up) }}</el-tag></template>
        </el-table-column>
        <el-table-column :label="t('monitor.frontend.cpu')" min-width="100">
          <template #default="{ row }">{{ row.cpu_cores === null ? '—' : formatNumber(row.cpu_cores, { precision: 2, unit: t('monitor.processTotals.cores') }, locale) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.frontend.memory')" min-width="130">
          <template #default="{ row }">{{ row.memory_bytes === null ? '—' : formatBytes(row.memory_bytes, 2, locale) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.frontend.restarts')" min-width="100">
          <template #default="{ row }">{{ row.restart_count ?? '—' }}</template>
        </el-table-column>
      </el-table>
    </template>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { formatBytes } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue as formatNumber } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { processResourcesAPI as api } from '../api/processResources'
import { resourceErrorKey } from '../utils/nodeResources'
import { isTargetUUID } from '../utils/monitoringTargets.js'

const props = defineProps({ nodeId: { type: String, required: true }, canConfigure: { type: Boolean, default: false } })
const emit = defineEmits(['update:nodeId', 'configure'])
const { t, locale } = useI18n()
const requests = createLatestRequestScope(), inventory = createLatestRequestScope()
const nodes = ref([]), nodesLoading = ref(true), nodesError = ref('')
const options = computed(() => !props.nodeId || nodes.value.some(node => node.node_id === props.nodeId)
  ? nodes.value : [{ node_id: props.nodeId, display_name: observed.value?.node?.display_name || props.nodeId }, ...nodes.value])
const observed = ref(null), loading = ref(false), blocked = ref(false), errorKey = ref('')
const tagType = value => value === true ? 'success' : value === false ? 'danger' : 'info'
const reachability = value => t(`monitor.frontend.states.${value === true ? 'up' : value === false ? 'down' : 'unknown'}`)
const refresh = createAutoRefresh({
  interval: () => '15',
  enabled: () => Boolean(props.nodeId) && !loading.value && !blocked.value,
  refresh: () => load(),
  pause: () => { requests.invalidate(); loading.value = false; observed.value = null },
  visibilityChanged: () => {}
})

async function loadNodes() {
  const ticket = inventory.invalidate()
  nodesLoading.value = true
  nodesError.value = ''
  try {
    const result = await inventory.request(api.frontendNodes)
    if (!inventory.current(ticket)) return
    const data = result.data
    if (!Array.isArray(data) || data.length > 100 || data.some(node => !isTargetUUID(node?.node_id) || typeof node.display_name !== 'string' || !node.display_name.trim() || typeof node.enabled !== 'boolean') ||
      new Set(data.map(node => node.node_id)).size !== data.length) throw new Error('invalid_resource_response')
    nodes.value = data
    if (!props.nodeId && data.length) emit('update:nodeId', data[0].node_id)
  } catch (error) {
    if (!inventory.current(ticket)) return
    nodes.value = []
    nodesError.value = resourceErrorKey(error)
  } finally {
    if (inventory.current(ticket)) nodesLoading.value = false
  }
}
function refreshView() {
  void loadNodes()
  if (props.nodeId) void load()
}

async function load() {
  refresh.stop()
  const ticket = requests.invalidate()
  if (!props.nodeId) return
  loading.value = true; blocked.value = false
  try {
    const result = await requests.request(config => api.frontend(props.nodeId, config))
    if (!requests.current(ticket)) return
    if (result.node?.node_id !== props.nodeId || !['collecting','not_connected','disabled','no_sample'].includes(result.source_state) || !Array.isArray(result.data)) throw new Error('invalid_resource_response')
    observed.value = result; errorKey.value = ''
  } catch (error) {
    if (!requests.current(ticket)) return
    observed.value = null
    errorKey.value = resourceErrorKey(error)
    blocked.value = [401, 403, 404].includes(error.response?.status)
  } finally {
    if (requests.current(ticket)) { loading.value = false; refresh.schedule() }
  }
}
watch(() => props.nodeId, () => {
  observed.value = null; errorKey.value = ''; loading.value = false
  void load()
}, { immediate: true })
onMounted(loadNodes)
onBeforeUnmount(() => { refresh.dispose(); requests.dispose(); inventory.dispose() })
</script>

<style scoped>
.frontend-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.frontend-toolbar .el-select { width: min(360px, 100%); }
</style>
