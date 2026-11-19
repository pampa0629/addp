<template>
  <section data-testid="automatic-collection-scopes">
    <el-alert v-if="error" :title="error" type="warning" :closable="false" show-icon />
    <el-alert v-else-if="catalogUnavailable" :title="t('monitor.targets.automatic.catalogUnavailable')" type="warning" :closable="false" show-icon />
    <CollectionSettingsTable :can-toggle="canWrite" :busy="blocked || Boolean(saving) || loading" @toggle="row => save(row, !row.enabled)"
      :rows="rows" :empty="t(loading ? 'monitor.targets.automatic.loading' : 'monitor.targets.automatic.empty')">
      <template #object="{ row }">
        {{ row.kind === 'service' ? row.module_name : t(`monitor.targets.automatic.${row.kind}`) }}
      </template>
      <template #kind="{ row }">
        {{ row.kind === 'service' ? t(`monitor.process.roles.${row.role}`, row.role) : t(`monitor.targets.categories.${row.kind}`) }}
        <template v-if="row.runtime_name"> · {{ t(`monitor.availability.purposes.${row.runtime_name}`, row.runtime_name) }}</template>
      </template>
      <template #observation="{ row }">
        <el-tag :type="row.observation === 'collecting' ? 'success' : row.observation === 'stopping' ? 'warning' : 'info'">
          {{ t(`monitor.targets.automatic.states.${row.observation}`) }}
        </el-tag>
        <span v-if="Number.isInteger(row.active_targets)" class="active-count">{{ t('monitor.targets.automatic.activeCount', { count: row.active_targets }) }}</span>
      </template>
    </CollectionSettingsTable>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { automaticCollectionAPI as api } from '../api/monitoringTargets'
import CollectionSettingsTable from './CollectionSettingsTable.vue'
import { useAuthStore } from '../store/auth'

const props = defineProps({ kind: { type: String, required: true } })
const { t } = useI18n(), auth = useAuthStore()
const canWrite = computed(() => auth.hasPermission('monitor.monitoring_target.update'))
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
    rows.value = data.data.filter(row => row.kind === props.kind)
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

defineExpose({ refresh: () => !saving.value && load() })
onMounted(load)
onBeforeUnmount(() => { refresh.dispose(); requests.dispose(); writes.dispose() })
</script>

<style scoped>
.active-count { display: inline-block; margin-inline-start: 8px; color: var(--addp-text-secondary); }
.el-alert { margin-bottom: 12px; }
</style>
