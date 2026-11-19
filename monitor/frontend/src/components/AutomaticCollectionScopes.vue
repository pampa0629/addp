<template>
  <section data-testid="automatic-collection-scopes">
    <div class="collection-toolbar">
      <el-button :loading="loading" :disabled="Boolean(saving)" @click="load()">{{ t('common.refresh') }}</el-button>
      <el-button v-if="canConfigureEngines" @click="openConsoleRoute('/system/engine-deployments')">{{ t('monitor.engines.configure') }}</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="warning" :closable="false" show-icon />
    <el-alert v-else-if="catalogUnavailable" :title="t('monitor.targets.automatic.catalogUnavailable')" type="warning" :closable="false" show-icon />
    <el-table :data="rows" :aria-label="t('monitor.targets.automatic.tab')" border stripe>
      <el-table-column :label="t('monitor.targets.automatic.scope')" min-width="210">
        <template #default="{ row }">
          <template v-if="row.kind === 'service'">
            {{ row.module_name }} · {{ t(`monitor.process.roles.${row.role}`, row.role) }}<template v-if="row.runtime_name"> · {{ t(`monitor.availability.purposes.${row.runtime_name}`, row.runtime_name) }}</template>
          </template>
          <template v-else>{{ t(`monitor.targets.automatic.${row.kind}`) }}</template>
        </template>
      </el-table-column>
      <el-table-column :label="t('monitor.targets.configState')" min-width="125">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ t(row.enabled ? 'monitor.targets.enabled' : 'monitor.targets.automatic.paused') }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('monitor.targets.automatic.observation')" min-width="210">
        <template #default="{ row }">
          <el-tag :type="row.observation === 'collecting' ? 'success' : row.observation === 'stopping' ? 'warning' : 'info'">
            {{ t(`monitor.targets.automatic.states.${row.observation}`) }}
          </el-tag>
          <span v-if="Number.isInteger(row.active_targets)" class="active-count">{{ t('monitor.targets.automatic.activeCount', { count: row.active_targets }) }}</span>
        </template>
      </el-table-column>
      <el-table-column v-if="canWrite" :label="t('monitor.targets.automatic.operation')" min-width="115">
        <template #default="{ row }">
          <el-popconfirm v-if="row.enabled" :title="t('monitor.targets.automatic.confirmPause')" :confirm-button-text="t('common.confirm')" :cancel-button-text="t('common.cancel')" @confirm="save(row, false)">
            <template #reference><el-button text type="primary" :disabled="blocked || Boolean(saving)" :loading="saving === row.id">{{ t('monitor.targets.automatic.pause') }}</el-button></template>
          </el-popconfirm>
          <el-button v-else text type="primary" :disabled="blocked || Boolean(saving)" :loading="saving === row.id" @click="save(row, true)">{{ t('monitor.targets.automatic.resume') }}</el-button>
        </template>
      </el-table-column>
      <template #empty><el-empty :description="t(loading ? 'monitor.targets.automatic.loading' : 'monitor.targets.automatic.empty')" :image-size="64" /></template>
    </el-table>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { openConsoleRoute } from '@common-ui'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { automaticCollectionAPI as api } from '../api/monitoringTargets'
import { useAuthStore } from '../store/auth'

const { t } = useI18n(), auth = useAuthStore()
const canWrite = computed(() => auth.hasPermission('monitor.monitoring_target.update'))
const canConfigureEngines = computed(() => auth.hasPermission('platform.engine_deployment.read') &&
  (auth.hasPermission('platform.engine_deployment.create') || auth.hasPermission('platform.engine_deployment.update')))
const rows = ref([]), error = ref(''), loading = ref(false), saving = ref(''), blocked = ref(false), catalogUnavailable = ref(false)
const requests = createLatestRequestScope(), writes = createLatestRequestScope()
const refresh = createAutoRefresh({
  interval: () => '15',
  enabled: () => !loading.value && !saving.value && !blocked.value,
  refresh: () => load(),
  pause: () => { requests.invalidate(); loading.value = false; clearObservation() },
  visibilityChanged: () => {}
})

function clearObservation() {
  rows.value = rows.value.map(row => ({ ...row, observation: 'unknown', active_targets: undefined }))
}

async function load() {
  refresh.stop()
  const ticket = requests.invalidate()
  loading.value = true
  try {
    const data = await requests.request(api.list)
    if (!requests.current(ticket)) return
    rows.value = data.data
    catalogUnavailable.value = !data.catalog_available
    error.value = ''
    blocked.value = false
  } catch (failure) {
    if (!requests.current(ticket)) return
    if ([401, 403, 404].includes(failure.response?.status)) {
      rows.value = []
      blocked.value = true
    } else {
      clearObservation()
    }
    error.value = t('monitor.targets.loadFailed')
  } finally {
    if (requests.current(ticket)) { loading.value = false; refresh.schedule() }
  }
}

async function save(row, enabled) {
  if (!canWrite.value || saving.value || blocked.value) return
  refresh.stop()
  requests.invalidate()
  loading.value = false
  const ticket = writes.invalidate()
  saving.value = row.id
  error.value = ''
  try {
    const saved = await writes.request(config => api.update(row.id, { enabled, version: row.version }, config))
    if (!writes.current(ticket)) return
    rows.value = rows.value.map(item => item.id === saved.id ? { ...saved, observation: 'unknown' } : item)
    saving.value = ''
    await load()
  } catch (failure) {
    if (!writes.current(ticket)) return
    blocked.value = true
    clearObservation()
    error.value = t(failure.response?.data?.error_code === 'resource_version_conflict' ? 'monitor.targets.automatic.conflict' : 'monitor.targets.automatic.saveFailed')
  } finally {
    if (writes.current(ticket)) saving.value = ''
  }
}

onMounted(load)
onBeforeUnmount(() => { refresh.dispose(); requests.dispose(); writes.dispose() })
</script>

<style scoped>
.collection-toolbar { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 12px; }
.collection-toolbar :deep(.el-button + .el-button) { margin-left: 0; }
.active-count { display: inline-block; margin-inline-start: 8px; color: var(--addp-text-secondary); }
.el-alert { margin-bottom: 12px; }
</style>
