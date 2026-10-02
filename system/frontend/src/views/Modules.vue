<template>
  <section class="module-page">
    <StatusAnnouncer :message="announcement" />

    <el-card>
      <template #header>
        <div class="page-header">
          <div>
            <h2>{{ t('system.module.title') }}</h2>
            <p>{{ t('system.module.description') }}</p>
          </div>
          <el-button v-if="activeTab === 'overview'" :icon="Refresh" :loading="loading" @click="refreshNow">
            {{ t('system.module.refresh') }}
          </el-button>
        </div>
      </template>

      <el-alert
        v-if="conflictMessage"
        class="module-alert"
        type="warning"
        :title="conflictMessage"
        :description="t('system.module.conflictHint')"
        show-icon
        :closable="false"
      >
        <template #default>
          <el-button size="small" type="warning" plain @click="refreshNow">
            {{ t('system.module.reload') }}
          </el-button>
        </template>
      </el-alert>
      <el-alert
        v-else-if="loadError"
        class="module-alert"
        type="error"
        :title="loadError"
        show-icon
        :closable="false"
      />

      <el-alert v-if="activeTab === 'log-pipeline' && !canReadPipeline" :title="t('system.module.pipeline.permissionDenied')" type="error" :closable="false" show-icon />
      <el-tabs v-model="activeTab" class="module-tabs">
        <el-tab-pane :label="t('system.module.tabs.overview')" name="overview">
          <div class="summary-grid">
            <div class="summary-item">
              <span class="summary-value">{{ modules.length }}</span>
              <span class="summary-label">{{ t('system.module.summary.definitions') }}</span>
            </div>
            <div class="summary-item">
              <span class="summary-value">{{ routableModuleCount }}</span>
              <span class="summary-label">{{ t('system.module.summary.routable') }}</span>
            </div>
            <div class="summary-item">
              <span class="summary-value">{{ onlineInstanceCount }}</span>
              <span class="summary-label">{{ t('system.module.summary.onlineInstances') }}</span>
            </div>
            <div class="summary-item">
              <span class="summary-value">{{ onlineWorkerInstanceCount }}</span>
              <span class="summary-label">{{ t('system.module.summary.workers') }}</span>
            </div>
            <div class="summary-item">
              <span class="summary-value">{{ attentionCount }}</span>
              <span class="summary-label">{{ t('system.module.summary.attention') }}</span>
            </div>
          </div>

          <div class="module-toolbar">
            <el-radio-group v-model="attentionOnly" size="small">
              <el-radio-button :value="false">{{ t('system.module.filters.all') }}</el-radio-button>
              <el-radio-button :value="true">{{ t('system.module.filters.attention') }}</el-radio-button>
            </el-radio-group>
          </div>

          <el-table v-loading="loading" :data="visibleModules" row-key="id" stripe>
            <el-table-column type="expand">
              <template #default="{ row }">
                <div class="instance-panel">
                  <div class="definition-observation">
                    <span class="definition-observation-label">{{ t('system.module.declarations.title') }}</span>
                    <el-tag v-if="configurationEntryCount(row) > 0" size="small" effect="plain">
                      {{ t('system.module.declarations.configuration', { count: configurationEntryCount(row) }) }}
                    </el-tag>
                    <el-tag v-if="row.task_provider" size="small" type="success" effect="plain">
                      {{ t('system.module.declarations.taskProvider') }}
                    </el-tag>
                    <span v-if="configurationEntryCount(row) === 0 && !row.task_provider" class="definition-observation-empty">
                      {{ t('system.module.declarations.empty') }}
                    </span>
                  </div>
                  <div class="instance-panel-heading">
                    <div class="instance-panel-title">
                      {{ t('system.module.instances.title', { count: row.instances.length }) }}
                    </div>
                    <el-button type="primary" link @click.stop="openInstanceHistory(row)">
                      {{ t('system.module.instances.queryAction') }}
                    </el-button>
                  </div>
                  <el-empty
                    v-if="row.instances.length === 0"
                    :description="t('system.module.instances.empty')"
                    :image-size="72"
                  />
                  <el-table v-else :data="row.instances" size="small" border>
                    <el-table-column prop="instance_id" :label="t('system.module.instances.id')" min-width="190" show-overflow-tooltip />
                    <el-table-column :label="t('system.module.instances.role')" width="120">
                      <template #default="{ row: instance }">
                        <el-tag size="small" effect="plain" :type="roleTagType(instance.role)">
                          {{ roleLabel(instance.role) }}
                        </el-tag>
                      </template>
                    </el-table-column>
                    <el-table-column :label="t('system.module.instances.status')" width="110">
                      <template #default="{ row: instance }">
                        <el-tag size="small" :type="instanceStatusTagType(instance)">{{ instanceStatusLabel(instance) }}</el-tag>
                      </template>
                    </el-table-column>
                    <el-table-column :label="t('system.module.instances.stopReason')" width="145">
                      <template #default="{ row: instance }">{{ stopReasonLabel(instance) }}</template>
                    </el-table-column>
                    <el-table-column :label="t('system.module.instances.node')" min-width="220">
                      <template #default="{ row: instance }"><ModuleInstanceNode :instance="instance" /></template>
                    </el-table-column>
                    <el-table-column :label="t('system.module.instances.endpoint')" min-width="180" show-overflow-tooltip>
                      <template #default="{ row: instance }">{{ endpointLabel(instance) }}</template>
                    </el-table-column>
                    <el-table-column prop="module_url" :label="t('system.module.instances.url')" min-width="210" show-overflow-tooltip>
                      <template #default="{ row: instance }">{{ instance.module_url || '—' }}</template>
                    </el-table-column>
                    <el-table-column prop="health_check_url" :label="t('system.module.instances.healthCheckUrl')" min-width="210" show-overflow-tooltip>
                      <template #default="{ row: instance }">{{ instance.health_check_url || '—' }}</template>
                    </el-table-column>
                    <el-table-column :label="t('system.module.instances.lastHeartbeat')" width="180">
                      <template #default="{ row: instance }">{{ formatDate(instance.last_heartbeat) }}</template>
                    </el-table-column>
                    <el-table-column :label="t('system.module.instances.leaseExpiresAt')" width="180">
                      <template #default="{ row: instance }">{{ formatDate(instance.lease_expires_at) }}</template>
                    </el-table-column>
                    <el-table-column :label="t('system.module.instances.registeredAt')" width="180">
                      <template #default="{ row: instance }">{{ formatDate(instance.registered_at) }}</template>
                    </el-table-column>
                    <el-table-column :label="t('system.module.instances.processStartedAt')" width="180">
                      <template #default="{ row: instance }">{{ formatDate(instance.process_started_at) }}</template>
                    </el-table-column>
                    <el-table-column :label="t('system.module.instances.uptime')" width="150">
                      <template #default="{ row: instance }">{{ formatUptime(instance) }}</template>
                    </el-table-column>
                  </el-table>
                </div>
              </template>
            </el-table-column>
            <el-table-column prop="id" :label="t('system.module.columns.id')" width="72" />
            <el-table-column :label="t('system.module.columns.name')" min-width="190">
              <template #default="{ row }">
                <div class="module-name">{{ resolveIAMModuleName(row.module_name, t, te) }}</div>
                <span class="module-technical-name">{{ row.module_name }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('system.module.columns.routePrefix')" min-width="150" show-overflow-tooltip>
              <template #default="{ row }">{{ row.route_prefix || '—' }}</template>
            </el-table-column>
            <el-table-column :label="t('system.module.columns.availability')" width="130">
              <template #default="{ row }">
                <el-tag :type="availabilityTagType(row)">
                  {{ availabilityLabel(row) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('system.module.columns.instances')" min-width="360">
              <template #default="{ row }">
                <div class="instance-summary">{{ instanceSummary(row) }}</div>
                <div v-for="instance in previewInstances(row)" :key="instance.instance_id" class="instance-observation">
                  <el-tag size="small" :type="instanceStatusTagType(instance)">{{ instanceStatusLabel(instance) }}</el-tag>
                  <span class="instance-observation-endpoint" :title="instance.module_url || instance.health_check_url || ''">
                    {{ roleLabel(instance.role) }} · {{ endpointLabel(instance) }}
                  </span>
                  <span class="instance-observation-uptime">{{ formatUptime(instance) }}</span>
                </div>
                <span v-if="(row.instances || []).length > 2" class="instance-observation-more">
                  {{ t('system.module.instances.more', { count: row.instances.length - 2 }) }}
                </span>
              </template>
            </el-table-column>
            <el-table-column :label="t('system.module.columns.enabled')" width="130" align="center">
              <template #default="{ row }">
                <el-tag v-if="row.module_name === 'system' || row.module_name === 'gateway'" type="info" :title="t('system.module.systemAlwaysEnabled')">
                  {{ t('system.module.systemAlwaysEnabled') }}
                </el-tag>
                <el-switch
                  v-else
                  :model-value="row.enabled"
                  :loading="isUpdating(row.module_name)"
                  :disabled="!canUpdate || isUpdating(row.module_name)"
                  :aria-label="t('system.module.enabledAria', { name: row.module_name })"
                  @change="value => updateEnabled(row, value)"
                />
              </template>
            </el-table-column>
            <el-table-column prop="version" :label="t('system.module.columns.version')" width="90" align="center" />
            <el-table-column :label="t('system.module.columns.updatedAt')" width="180">
              <template #default="{ row }">{{ formatDate(row.updated_at) }}</template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane :label="t('system.module.tabs.instances')" name="instances">
          <ModuleInstances v-if="activeTab === 'instances'" :modules="modules" />
        </el-tab-pane>
        <el-tab-pane v-if="canReadPipeline" :label="t('system.module.pipeline.title')" name="log-pipeline">
          <ModuleLogPipeline v-if="activeTab === 'log-pipeline'" />
        </el-tab-pane>
      </el-tabs>
    </el-card>

  </section>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { StatusAnnouncer } from '@common-ui'
import ModuleLogPipeline from '../components/ModuleLogPipeline.vue'
import { modulesAPI } from '../api/modules'
import { useAuthStore } from '../store/auth'
import { resolveIAMModuleName } from '../utils/iamPresentation'
import { getModuleAvailability, isModuleRoutable, isRuntimeInstanceOnline, moduleNeedsAttention } from '../utils/moduleRegistry'
import { formatRuntimeUptime, getRegisteredEndpoint } from '../utils/moduleRuntimePresentation'
import ModuleInstanceNode from '../components/ModuleInstanceNode.vue'
import ModuleInstances from '../components/ModuleInstances.vue'
import { resolveModulesRouteState } from '../utils/routeState'
import { navigateSystemRoute } from '../utils/moduleNavigation'

const { t, te } = useI18n()
const authStore = useAuthStore()
const canReadPipeline = computed(() => authStore.hasPermission('monitor.log_pipeline.read'))
const modules = ref([])
const attentionOnly = ref(false)
const loading = ref(false)
const loadError = ref('')
const conflictMessage = ref('')
const announcement = ref('')
const refreshPaused = ref(false)
const updatingModules = ref(new Set())
const route = useRoute()
const router = useRouter()
const routeState = computed(() => resolveModulesRouteState(route.query))
const activeTab = computed({
  get: () => routeState.value.tab,
  set: tab => navigateSystemRoute(router, { name: 'Modules', query: tab !== 'overview' ? { tab } : {} }, { history: 'replace' })
})
watch(() => route.fullPath, () => {
  if (routeState.value.changed) {
    navigateSystemRoute(router, { name: 'Modules', query: routeState.value.query }, { history: 'replace' })
  }
}, { immediate: true })
let refreshTimer = null
let moduleListRequestInFlight = false

const canUpdate = computed(() => authStore.hasPermission('platform.module.update'))
const allInstances = computed(() => modules.value.flatMap(module => module.instances || []))
const onlineInstanceCount = computed(() => allInstances.value.filter(isInstanceOnline).length)
const onlineWorkerInstanceCount = computed(() => allInstances.value.filter(instance => (
  instance.role === 'worker' && isInstanceOnline(instance)
)).length)
const routableModuleCount = computed(() => modules.value.filter(isRoutable).length)
const attentionCount = computed(() => modules.value.filter(moduleNeedsAttention).length)
const visibleModules = computed(() => modules.value
  .filter(module => !attentionOnly.value || moduleNeedsAttention(module))
  .sort((left, right) => Number(moduleNeedsAttention(right)) - Number(moduleNeedsAttention(left)) || left.module_name.localeCompare(right.module_name)))

function stopReasonLabel(instance) {
  if (isInstanceOnline(instance)) return '—'
  const key = `system.module.stopReasons.${instance.stop_reason}`
  return instance.stop_reason && te(key) ? t(key) : '—'
}

function formatUptime(instance) {
  return formatRuntimeUptime(instance, t)
}

function endpointLabel(instance) {
  return getRegisteredEndpoint(instance) || t('system.module.instances.noEndpoint')
}

function instanceStatusLabel(instance) {
  return t(`system.module.status.${isInstanceOnline(instance) ? 'up' : 'down'}`)
}

function instanceStatusTagType(instance) {
  return isInstanceOnline(instance) ? 'success' : 'danger'
}

function isInstanceOnline(instance) {
  return isRuntimeInstanceOnline(instance)
}

function isRoutable(module) {
  return isModuleRoutable(module)
}

function availabilityLabel(module) {
  return t(`system.module.availability.${getModuleAvailability(module)}`)
}

function availabilityTagType(module) {
  return {
    routable: 'success',
    disabled: 'warning',
    no_backend: 'danger',
    backend_offline: 'danger',
    ingress_online: 'success',
    ingress_offline: 'danger'
  }[getModuleAvailability(module)] || 'info'
}

function configurationEntryCount(module) {
  return Array.isArray(module.configuration_management?.entries)
    ? module.configuration_management.entries.length
    : 0
}

function roleLabel(role) {
  const key = `system.module.roles.${role}`
  const label = t(key)
  return label === key ? role : label
}

function roleTagType(role) {
  return { backend: 'primary', worker: 'warning', scheduler: 'info' }[role] || 'info'
}

function instanceSummary(module) {
  const instances = module.instances || []
  const online = instances.filter(isInstanceOnline).length
  return t('system.module.instanceSummary', { online, offline: instances.length - online })
}

function previewInstances(module) {
  return [...(module.instances || [])]
    .sort((left, right) => Number(isInstanceOnline(left)) - Number(isInstanceOnline(right)))
    .slice(0, 2)
}

function formatDate(value) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString()
}

function isUpdating(moduleName) {
  return updatingModules.value.has(moduleName)
}

function setUpdating(moduleName, active) {
  const next = new Set(updatingModules.value)
  if (active) next.add(moduleName)
  else next.delete(moduleName)
  updatingModules.value = next
}

async function loadModules({ announce = false, force = false, silent = false } = {}) {
  if (moduleListRequestInFlight || (!force && (refreshPaused.value || updatingModules.value.size > 0))) return false
  moduleListRequestInFlight = true
  if (!silent) loading.value = true
  loadError.value = ''
  try {
    const response = await modulesAPI.list()
    modules.value = response.modules || []
    if (announce) announcement.value = t('system.module.refreshed')
    return true
  } catch (error) {
    loadError.value = error.response?.data?.error || error.message || t('system.module.loadFailed')
    if (announce) announcement.value = loadError.value
    return false
  } finally {
    if (!silent) loading.value = false
    moduleListRequestInFlight = false
  }
}

async function refreshNow() {
  refreshPaused.value = false
  conflictMessage.value = ''
  await loadModules({ announce: true, force: true })
}

function openInstanceHistory(module) {
  navigateSystemRoute(router, {
    name: 'Modules', query: { tab: 'instances', module_name: module.module_name }
  }, { history: 'replace' })
}

async function updateEnabled(module, enabled) {
  if (module.module_name === 'system' || module.module_name === 'gateway' || !canUpdate.value || isUpdating(module.module_name)) return
  setUpdating(module.module_name, true)
  conflictMessage.value = ''
  try {
    const updated = await modulesAPI.update(module.module_name, { enabled, version: module.version })
    const index = modules.value.findIndex(item => item.module_name === module.module_name)
    if (index >= 0) modules.value.splice(index, 1, updated)
    const message = enabled ? t('system.module.enabledSuccess') : t('system.module.disabledSuccess')
    ElMessage.success(message)
    announcement.value = message
  } catch (error) {
    if (error.response?.status === 409 && error.response?.data?.error_code === 'resource_version_conflict') {
      conflictMessage.value = error.response?.data?.error || t('system.module.conflict')
      refreshPaused.value = true
      announcement.value = conflictMessage.value
    } else {
      const message = error.response?.data?.error || error.message || t('system.module.updateFailed')
      ElMessage.error(message)
      announcement.value = message
    }
  } finally {
    setUpdating(module.module_name, false)
  }
}

onMounted(() => {
  loadModules()
  refreshTimer = window.setInterval(() => loadModules({ silent: true }), 10000)
})

onUnmounted(() => {
  if (refreshTimer) window.clearInterval(refreshTimer)
})
</script>

<style scoped>
.module-page {
  width: 100%;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.page-header h2 {
  margin: 0;
  color: var(--addp-text-primary);
  font-size: 20px;
}

.page-header p {
  margin: 6px 0 0;
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.module-alert {
  margin-bottom: 16px;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.module-toolbar { margin-bottom: 12px; }
.module-name { color: var(--addp-text-primary); font-weight: 600; }
.module-technical-name { color: var(--addp-text-tertiary); font-size: 12px; }

.summary-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 14px 16px;
  border: 1px solid var(--addp-border-color-light);
  border-radius: 8px;
  background: var(--addp-bg-secondary);
}

.summary-value {
  color: var(--addp-text-primary);
  font-size: 22px;
  font-weight: 600;
}

.summary-label {
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.instance-panel {
  padding: 12px 20px 20px;
  background: var(--addp-bg-secondary);
}

.definition-observation {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 14px;
}

.definition-observation-label {
  color: var(--addp-text-primary);
  font-weight: 600;
}

.definition-observation-empty {
  color: var(--addp-text-tertiary);
}

.instance-panel-title {
  color: var(--addp-text-primary);
  font-weight: 600;
}

.instance-panel-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.instance-summary { color: var(--addp-text-secondary); font-size: 12px; }
.instance-observation { display: flex; align-items: center; gap: 8px; margin-top: 5px; min-width: 0; }
.instance-observation-endpoint { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.instance-observation-uptime { flex: none; color: var(--addp-text-secondary); font-size: 12px; }
.instance-observation-more { display: block; margin-top: 5px; color: var(--addp-text-tertiary); font-size: 12px; }

@media (max-width: 900px) {
  .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 600px) {
  .page-header {
    flex-direction: column;
  }

  .summary-grid {
    grid-template-columns: 1fr;
  }
}
</style>
