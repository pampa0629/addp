<template>
  <section data-testid="frontend-resources">
    <div class="frontend-toolbar">
      <HostNodeSelect :model-value="nodeId" :allowed="true" :identity="identity" :api="hosts"
        :label="t('monitor.targets.node')" :placeholder="t('monitor.targets.nodeSearch')" :more-label="t('monitor.targets.moreNodes')"
        :error-message="t('monitor.targets.nodeLoadingFailed')" @update:model-value="emit('update:nodeId', $event)" />
      <el-button :disabled="!nodeId" :loading="loading" @click="load">{{ t('common.refresh') }}</el-button>
    </div>
    <el-alert v-if="errorKey" :title="t(errorKey)" type="warning" :closable="false" />
    <el-empty v-else-if="!observed && !loading" :description="t('monitor.frontend.selectNode')" :image-size="64" />
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
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import HostNodeSelect from '../../../../common-frontend/basic/src/components/HostNodeSelect.vue'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { formatBytes } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue as formatNumber } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { nodeResourcesAPI as hosts } from '../api/nodeResources'
import { processResourcesAPI as api } from '../api/processResources'
import { resourceErrorKey } from '../utils/nodeResources'

const props = defineProps({ nodeId: { type: String, required: true }, identity: { type: String, required: true } })
const emit = defineEmits(['update:nodeId'])
const { t, locale } = useI18n()
const requests = createLatestRequestScope()
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
onBeforeUnmount(() => { refresh.dispose(); requests.dispose() })
</script>

<style scoped>
.frontend-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.frontend-toolbar :deep(.host-node-select) { width: min(360px, 100%); }
</style>
