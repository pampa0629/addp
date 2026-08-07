<template>
  <section class="configuration-management">
    <header class="page-header">
      <div>
        <div class="title-line">
          <el-button v-if="selectedOwner" :icon="ArrowLeft" text circle :title="t('console.configuration.back')" @click="goBack" />
          <h1>{{ t('console.configuration.title') }}</h1>
        </div>
        <p>{{ contextLabel }}</p>
      </div>
      <el-button :icon="Refresh" circle :loading="loading" @click="loadEntries" />
    </header>

    <ModuleConfiguration v-if="selectedOwner" :key="selectedOwner" :owner="selectedOwner" />

    <el-table v-else v-loading="loading" :data="moduleEntries" class="entries-table" :row-class-name="entryRowClass" @row-click="openEntry">
      <el-table-column :label="t('console.configuration.owner')" min-width="150">
        <template #default="{ row }"><div class="entry-owner"><span>{{ t(`console.configuration.modules.${row.owner_module}.name`) }}</span><code>{{ row.owner_module }}</code></div></template>
      </el-table-column>
      <el-table-column :label="t('console.configuration.entry')" min-width="240">
        <template #default="{ row }">
          <div class="entry-name">{{ entryLabel(row) }}</div>
          <code>{{ row.id }}</code>
        </template>
      </el-table-column>
      <el-table-column :label="t('console.configuration.scope')" min-width="220">
        <template #default="{ row }">
          <el-tag v-for="scope in row.scope_types" :key="scope" effect="plain">
            {{ scopeLabel(scope) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('console.configuration.status')" width="120">
        <template #default="{ row }">
          <el-tag :type="row.available ? 'success' : 'info'" effect="plain">
            {{ row.available ? t('console.configuration.available') : t('console.configuration.unavailable') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column width="72" align="right">
        <template #default="{ row }">
          <el-button :icon="ArrowRight" text circle :disabled="!row.available" @click.stop="openEntry(row)" />
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :description="t('console.configuration.empty')" />
      </template>
    </el-table>
  </section>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../store/auth'
import { listConfigurationManagementEntries } from '../api/configurationManagement'
import ModuleConfiguration from '../components/configuration/ModuleConfiguration.vue'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const loading = ref(false)
const entries = ref([])
const CONSOLE_MODULE_ROUTES = {
  agent: '/configuration/agent',
  copilot: '/configuration/copilot',
  develop: '/configuration/develop',
  transfer: '/configuration/transfer',
  monitor: '/configuration/monitor',
  service: '/configuration/service'
}
const ENTRY_LABEL_KEYS = {
  'agent.configuration': 'console.configuration.entries.agentConfiguration',
  'copilot.configuration': 'console.configuration.entries.copilotConfiguration',
  'develop.configuration': 'console.configuration.entries.developConfiguration',
  'inference.configuration': 'console.configuration.entries.inferenceConfiguration',
  'manager.configuration': 'console.configuration.entries.managerConfiguration',
  'transfer.configuration': 'console.configuration.entries.transferConfiguration',
  'monitor.configuration': 'console.configuration.entries.monitorConfiguration',
  'service.configuration': 'console.configuration.entries.serviceConfiguration'
}
const moduleEntries = computed(() => {
  const modules = new Map()
  for (const entry of entries.value) {
    const existing = modules.get(entry.owner_module)
    if (!existing) {
      modules.set(entry.owner_module, {
        ...entry,
        id: `${entry.owner_module}.configuration`,
        frontend_route: CONSOLE_MODULE_ROUTES[entry.owner_module] || entry.frontend_route,
        scope_types: [...entry.scope_types]
      })
      continue
    }
    existing.scope_types = [...new Set([...existing.scope_types, ...entry.scope_types])]
    existing.available = existing.available || entry.available
  }
  return [...modules.values()]
})
const selectedOwner = computed(() => {
  const parts = route.path.split('/').filter(Boolean)
  return parts[0] === 'configuration' && ['agent', 'copilot', 'develop', 'manager', 'transfer', 'monitor', 'service'].includes(parts[1]) ? parts[1] : ''
})

const contextLabel = computed(() => authStore.authContext?.context?.type === 'tenant'
  ? t('console.configuration.tenantContext')
  : t('console.configuration.platformContext'))

function scopeLabel(scope) {
  return t(`console.configuration.scopes.${scope}`)
}

function entryLabel(entry) {
  const key = ENTRY_LABEL_KEYS[entry.id]
  return key ? t(key) : entry.id
}

async function loadEntries() {
  loading.value = true
  try {
    entries.value = await listConfigurationManagementEntries()
  } catch (error) {
    entries.value = []
    ElMessage.error(t('console.configuration.loadFailed'))
  } finally {
    loading.value = false
  }
}

function openEntry(entry) {
  if (entry?.available && entry?.frontend_route) router.push(entry.frontend_route)
}

function goBack() {
  router.push('/configuration')
}

function entryRowClass({ row }) {
  return row.available ? 'entry-row--available' : 'entry-row--unavailable'
}

onMounted(() => {
  if (!selectedOwner.value) loadEntries()
})
</script>

<style scoped>
.configuration-management {
  width: min(1120px, 100%);
  margin: 0 auto;
  padding: 28px 32px;
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 24px;
}

.page-header h1 {
  margin: 0 0 6px;
  color: var(--addp-text-primary);
  font-size: 24px;
  font-weight: 600;
  letter-spacing: 0;
}
.title-line { display: flex; align-items: center; gap: 6px; }
.title-line h1 { margin-bottom: 0; }

.page-header p {
  margin: 0;
  color: var(--addp-text-secondary);
  font-size: 14px;
}

.entries-table { width: 100%; }
.entry-name { color: var(--addp-text-primary); font-weight: 500; }
.entry-owner { display: flex; flex-direction: column; gap: 2px; }
.entry-owner code { color: var(--addp-text-tertiary); font-size: 12px; }
.entries-table code { color: var(--addp-text-tertiary); font-size: 12px; }
.entries-table :deep(.entry-row--available) { cursor: pointer; }
.entries-table :deep(.entry-row--unavailable) { color: var(--addp-text-secondary); }

.entries-table .el-tag + .el-tag {
  margin-left: 8px;
}

@media (max-width: 760px) {
  .configuration-management { padding: 20px 16px; }
}
</style>
