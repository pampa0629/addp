<template>
  <section data-testid="engine-collection-settings">
    <el-tabs :model-value="scope" @update:model-value="emit('update:scope', $event)">
      <el-tab-pane name="infra" :label="t('monitor.engines.scopes.infra')" />
      <el-tab-pane name="business" :label="t('monitor.engines.scopes.business')" />
    </el-tabs>
    <div class="engine-collection-toolbar">
      <el-button :loading="loading" :disabled="saving" @click="load">{{ t('common.refresh') }}</el-button>
      <el-button v-if="canRegister" @click="openConsoleRoute(registrationRoute)">{{ t('monitor.targets.engine.register') }}</el-button>
    </div>
    <el-alert v-if="error" :title="t(error)" type="warning" :closable="false" />
    <el-table :data="rows" v-loading="loading" border stripe>
      <el-table-column prop="display_name" :label="t('monitor.targets.automatic.scope')" min-width="175" show-overflow-tooltip />
      <el-table-column prop="engine_type" :label="t('monitor.targets.kind')" min-width="115" />
      <el-table-column :label="t('monitor.targets.configState')" min-width="110">
        <template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'">{{ t(row.enabled ? 'monitor.targets.enabled' : 'monitor.targets.disabled') }}</el-tag></template>
      </el-table-column>
      <el-table-column v-if="canObserve" :label="t('monitor.targets.automatic.observation')" min-width="150">
        <template #default="{ row }">
          {{ t(`monitor.targets.engine.states.${collections[row.deployment_id]?.state || 'unknown'}`) }}
          <div v-if="collections[row.deployment_id]?.sampled_at" class="collection-time">{{ formatDateTime(collections[row.deployment_id].sampled_at) }}</div>
        </template>
      </el-table-column>
      <el-table-column v-if="canWrite" :label="t('monitor.targets.automatic.operation')" min-width="115">
        <template #default="{ row }">
          <el-popconfirm :title="t(row.enabled ? 'monitor.targets.automatic.confirmPause' : 'monitor.targets.engine.confirmResume')"
            :confirm-button-text="t('common.confirm')" :cancel-button-text="t('common.cancel')" @confirm="save(row)">
            <template #reference>
              <el-button text type="primary" :disabled="blocked || Boolean(saving) || loading">
                {{ t(row.enabled ? 'monitor.targets.automatic.pause' : 'monitor.targets.automatic.resume') }}
              </el-button>
            </template>
          </el-popconfirm>
        </template>
      </el-table-column>
      <template #empty><el-empty :description="t('monitor.targets.engine.empty')" :image-size="64" /></template>
    </el-table>
    <el-pagination v-if="total" :current-page="page" :page-size="pageSize" :page-sizes="[10,20,50]"
      :total="total" :disabled="Boolean(saving)" layout="total, sizes, prev, pager, next"
      @current-change="emit('update:page', $event)" @size-change="emit('update:pageSize', $event)" />
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { openConsoleRoute } from '@common-ui'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { validateEngineCollection } from '../../../../common-frontend/basic/src/utils/engineCollection.mjs'
import { formatDateTime } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { engineResourcesAPI as api } from '../api/engineResources'
import { useAuthStore } from '../store/auth'
import { validateEngineDeployment, validateEnginePage } from '../utils/engineResources'
import { isTargetUUID } from '../utils/monitoringTargets.js'

const props = defineProps({ scope: String, page: Number, pageSize: Number })
const emit = defineEmits(['update:scope', 'update:page', 'update:pageSize'])
const auth = useAuthStore(), { t } = useI18n()
const canWrite = computed(() => auth.hasPermission('platform.host_node.read') && auth.hasPermission('platform.engine_deployment.update'))
const canRegister = computed(() => auth.hasPermission('platform.host_node.read') && auth.hasPermission('platform.engine_deployment.create'))
const canObserve = computed(() => auth.hasPermission('monitor.resource_observation.read'))
const registrationRoute = computed(() => props.scope === 'business' ? '/system/engine-deployments?scope=business' : '/system/engine-deployments')
const rows = ref([]), total = ref(0), collections = ref({}), loading = ref(false), saving = ref(false), blocked = ref(false), error = ref('')
const requests = createLatestRequestScope(), writes = createLatestRequestScope()

async function load() {
  const ticket = requests.invalidate()
  const { scope, page, pageSize } = props
  rows.value = []; total.value = 0; collections.value = {}; loading.value = true; error.value = ''
  try {
    const result = validateEnginePage(await requests.request(config => api.deployments({ scope, page, page_size: pageSize }, config)), page, pageSize)
    if (!requests.current(ticket)) return
    if (result.data.some(row => row.scope !== scope || !isTargetUUID(row.declared_node_id))) throw new Error('invalid_deployment_scope')
    rows.value = result.data
    total.value = result.total
    blocked.value = false
    collections.value = Object.fromEntries(result.data.filter(row => !row.enabled || row.host_binding_state !== 'bound').map(row => [row.deployment_id, { state: 'not_active' }]))
    const owners = result.data.filter(row => row.enabled && row.host_binding_state === 'bound')
    if (!canObserve.value || !owners.length) return
    try {
      const summary = await requests.request(config => api.summaries(owners.map(row => row.deployment_id), config))
      if (!requests.current(ticket) || !canObserve.value) return
      if (!Array.isArray(summary.data) || summary.data.length !== owners.length) return
      const byID = new Map(summary.data.map(row => [row.subject?.deployment_id, row]))
      if (byID.size !== owners.length) return
      const queriedAt = Date.parse(summary.queried_at)
      Object.assign(collections.value, Object.fromEntries(owners.map(owner => [
        owner.deployment_id, validateEngineCollection(byID.get(owner.deployment_id), owner, queriedAt)
      ])))
    } catch {
      // Configuration remains usable when the optional collection query fails.
    }
  } catch {
    if (requests.current(ticket)) { blocked.value = true; error.value = 'monitor.targets.loadFailed' }
  } finally {
    if (requests.current(ticket)) loading.value = false
  }
}

async function save(row) {
  if (!canWrite.value || saving.value || blocked.value || loading.value || !rows.value.includes(row)) return
  const body = { display_name: row.display_name, enabled: !row.enabled, declared_node_id: row.declared_node_id, version: row.version }
  const ticket = writes.invalidate()
  saving.value = true
  try {
    const saved = validateEngineDeployment(await writes.request(config => api.updateDeployment(row.deployment_id, body, config)))
    if (!writes.current(ticket)) return
    if (saved.deployment_id !== row.deployment_id || saved.enabled !== body.enabled || saved.version !== body.version + 1) throw new Error('invalid_deployment_update')
    await load()
  } catch (failure) {
    if (writes.current(ticket)) {
      blocked.value = true
      collections.value = {}
      error.value = failure.response?.status === 409 ? 'monitor.targets.conflict' : 'monitor.targets.engine.saveFailed'
    }
  } finally {
    if (writes.current(ticket)) saving.value = false
  }
}

watch(() => [props.scope, props.page, props.pageSize], load, { immediate: true })
onBeforeUnmount(() => { requests.dispose(); writes.dispose() })
</script>

<style scoped>
.engine-collection-toolbar { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 12px; }
.engine-collection-toolbar :deep(.el-button + .el-button) { margin-left: 0; }
.el-alert { margin-bottom: 12px; }
.collection-time { color: var(--addp-text-secondary); font-size: 12px; }
.el-pagination { margin-top: 12px; overflow-x: auto; }
</style>
