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
          <el-button :icon="Refresh" :loading="loading" @click="refreshNow">
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
          <span class="summary-value">{{ workerInstanceCount }}</span>
          <span class="summary-label">{{ t('system.module.summary.workers') }}</span>
        </div>
      </div>

      <el-table v-loading="loading" :data="modules" row-key="id" stripe>
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="instance-panel">
              <div class="instance-panel-title">
                {{ t('system.module.instances.title', { count: row.instances.length }) }}
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
                    <span class="status-cell">
                      <span class="status-dot" :class="isInstanceOnline(instance) ? 'is-online' : 'is-offline'"></span>
                      {{ isInstanceOnline(instance) ? t('system.module.status.up') : t('system.module.status.down') }}
                    </span>
                  </template>
                </el-table-column>
                <el-table-column prop="module_url" :label="t('system.module.instances.url')" min-width="210" show-overflow-tooltip>
                  <template #default="{ row: instance }">{{ instance.module_url || '—' }}</template>
                </el-table-column>
                <el-table-column :label="t('system.module.instances.lastHeartbeat')" width="180">
                  <template #default="{ row: instance }">{{ formatDate(instance.last_heartbeat) }}</template>
                </el-table-column>
                <el-table-column :label="t('system.module.instances.leaseExpiresAt')" width="180">
                  <template #default="{ row: instance }">{{ formatDate(instance.lease_expires_at) }}</template>
                </el-table-column>
              </el-table>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="id" label="ID" width="72" />
        <el-table-column prop="module_name" :label="t('system.module.columns.name')" min-width="150" />
        <el-table-column prop="route_prefix" :label="t('system.module.columns.routePrefix')" min-width="150" show-overflow-tooltip />
        <el-table-column :label="t('system.module.columns.availability')" width="130">
          <template #default="{ row }">
            <el-tag :type="isRoutable(row) ? 'success' : 'info'">
              {{ isRoutable(row) ? t('system.module.availability.routable') : t('system.module.availability.unavailable') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('system.module.columns.instances')" width="160">
          <template #default="{ row }">
            {{ instanceSummary(row) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('system.module.columns.enabled')" width="130" align="center">
          <template #default="{ row }">
            <el-switch
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
    </el-card>
  </section>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { StatusAnnouncer } from '@common-ui'
import { modulesAPI } from '../api/modules'
import { useAuthStore } from '../store/auth'
import { isModuleRoutable, isRuntimeInstanceOnline } from '../utils/moduleRegistry'

const { t } = useI18n()
const authStore = useAuthStore()
const modules = ref([])
const loading = ref(false)
const loadError = ref('')
const conflictMessage = ref('')
const announcement = ref('')
const refreshPaused = ref(false)
const updatingModules = ref(new Set())
let refreshTimer = null

const canUpdate = computed(() => authStore.hasPermission('platform.module.update'))
const allInstances = computed(() => modules.value.flatMap(module => module.instances || []))
const onlineInstanceCount = computed(() => allInstances.value.filter(isInstanceOnline).length)
const workerInstanceCount = computed(() => allInstances.value.filter(instance => instance.role === 'worker').length)
const routableModuleCount = computed(() => modules.value.filter(isRoutable).length)

function isInstanceOnline(instance) {
  return isRuntimeInstanceOnline(instance)
}

function isRoutable(module) {
  return isModuleRoutable(module)
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
  return t('system.module.instanceSummary', { online, total: instances.length })
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

async function loadModules({ announce = false, force = false } = {}) {
  if (loading.value || (!force && (refreshPaused.value || updatingModules.value.size > 0))) return
  loading.value = true
  loadError.value = ''
  try {
    const response = await modulesAPI.list()
    modules.value = response.modules || []
    if (announce) announcement.value = t('system.module.refreshed')
  } catch (error) {
    loadError.value = error.response?.data?.error || error.message || t('system.module.loadFailed')
    if (announce) announcement.value = loadError.value
  } finally {
    loading.value = false
  }
}

async function refreshNow() {
  refreshPaused.value = false
  conflictMessage.value = ''
  await loadModules({ announce: true, force: true })
}

async function updateEnabled(module, enabled) {
  if (!canUpdate.value || isUpdating(module.module_name)) return
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
  refreshTimer = window.setInterval(() => loadModules(), 10000)
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
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

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

.instance-panel-title {
  margin-bottom: 10px;
  color: var(--addp-text-primary);
  font-weight: 600;
}

.status-cell {
  display: inline-flex;
  align-items: center;
  gap: 7px;
}

.status-dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
}

.status-dot.is-online {
  background: var(--el-color-success);
}

.status-dot.is-offline {
  background: var(--el-color-danger);
}

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
