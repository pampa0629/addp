<template>
  <div class="orchestration-list">
    <div class="header">
      <h2>{{ t('orchestrator.orchestrationList.title') }}</h2>
      <div class="header-actions">
        <MonitorExecutionsButton module="orchestrator" task-type="orchestration" />
        <el-button type="primary" @click="handleCreate">{{ t('orchestrator.orchestrationList.createBtn') }}</el-button>
      </div>
    </div>

    <el-alert v-if="filterState.error" :title="t('orchestrator.orchestrationList.invalidTaskFilter')" type="error" :closable="false" show-icon>
      <el-button link type="primary" @click="clearTaskFilter">{{ t('orchestrator.orchestrationList.showAll') }}</el-button>
    </el-alert>
    <el-alert v-else-if="filterState.filter" :title="t('orchestrator.orchestrationList.relatedTaskFilter')" type="info" :closable="false" show-icon>
      <el-button link type="primary" @click="clearTaskFilter">{{ t('orchestrator.orchestrationList.showAll') }}</el-button>
    </el-alert>
    <el-alert v-if="loadError" :title="t('orchestrator.orchestrationList.loadFailed')" type="error" :closable="false" show-icon>
      <el-button link type="primary" @click="loadOrchestrations">{{ t('orchestrator.orchestrationList.retry') }}</el-button>
    </el-alert>
    <el-table v-else :data="visibleOrchestrations" :empty-text="t(filterState.filter ? 'orchestrator.orchestrationList.noRelatedOrchestrations' : 'common.noData')" style="width: 100%" v-loading="loading">
      <el-table-column prop="id" :label="t('orchestrator.orchestrationList.colId')" width="80"></el-table-column>
      <el-table-column prop="name" :label="t('orchestrator.orchestrationList.colName')" width="200"></el-table-column>
      <el-table-column prop="description" :label="t('orchestrator.orchestrationList.colDescription')"></el-table-column>
      <el-table-column :label="t('orchestrator.orchestrationList.colStatus')" width="100">
        <template #default="scope">
          <el-tag :type="scope.row.enabled ? 'success' : 'info'">
            {{ scope.row.enabled ? t('orchestrator.orchestrationList.enabled') : t('orchestrator.orchestrationList.disabled') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('orchestrator.orchestrationList.colSchedule')" width="200">
        <template #default="scope">
          <el-tag v-if="scope.row.schedule" type="success" size="small">
            {{ describeCron(scope.row.schedule) }}
          </el-tag>
          <el-tag v-else type="info" size="small">{{ t('orchestrator.orchestrationList.manualTrigger') }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('orchestrator.orchestrationList.colSteps')" width="100">
        <template #default="scope">
          {{ scope.row.steps?.length || 0 }}
        </template>
      </el-table-column>
      <el-table-column :label="t('orchestrator.orchestrationList.colActions')" width="300">
        <template #default="scope">
          <el-button size="small" @click="handleEdit(scope.row)">{{ t('orchestrator.orchestrationList.editBtn') }}</el-button>
          <OrchestrationExecuteButton v-if="authStore.hasPermission('orchestrator.workflow.execute')" size="small" :orchestration="scope.row" :execute="orchestrationAPI.execute" :disabled="executingId !== null" @busy="executingId = $event ? scope.row.id : null" />
          <el-button size="small" type="info" @click="handleViewExecutions(scope.row)">{{ t('orchestrator.orchestrationList.recordsBtn') }}</el-button>
          <el-button size="small" type="danger" @click="handleDelete(scope.row)">{{ t('orchestrator.orchestrationList.deleteBtn') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAuthStore } from '../store/auth'
import orchestrationAPI from '../api/orchestration'
import { describeCron, MonitorExecutionsButton, resolveOrchestrationTaskFilter, matchesOrchestrationTask, OrchestrationExecuteButton } from '@common-ui'
import { navigateOrchestratorRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const route = useRoute()
const loadError = ref(false)
const filterState = computed(() => {
  try { return { filter: resolveOrchestrationTaskFilter(route.query), error: false } }
  catch { return { filter: null, error: true } }
})
const visibleOrchestrations = computed(() => {
  if (filterState.value.error) return []
  const filter = filterState.value.filter
  if (!filter) return orchestrations.value
  return orchestrations.value.filter(orchestration => matchesOrchestrationTask(orchestration, filter))
})
const clearTaskFilter = () => navigateOrchestratorRoute(router, '/orchestrations', { history: 'replace' })
const detailLocation = path => ({ path, query: filterState.value.filter || {} })
const orchestrations = ref([])
const loading = ref(false)
const executingId = ref(null)

onMounted(() => {
  loadOrchestrations()
})

async function loadOrchestrations() {
  loading.value = true
  loadError.value = false
  try {
    orchestrations.value = await orchestrationAPI.list()
  } catch (error) {
    loadError.value = true
    orchestrations.value = []
  } finally {
    loading.value = false
  }
}

function handleCreate() {
  navigateOrchestratorRoute(router, detailLocation('/orchestrations/new'))
}

function handleEdit(row) {
  navigateOrchestratorRoute(router, detailLocation(`/orchestrations/${row.id}/edit`))
}

function handleViewExecutions(row) {
  navigateOrchestratorRoute(router, detailLocation(`/orchestrations/${row.id}/executions`))
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(
      t('orchestrator.orchestrationList.deleteConfirm'),
      t('orchestrator.orchestrationList.deleteWarning'),
      {
        type: 'warning',
        customClass: 'addp-message-box',
        confirmButtonText: t('orchestrator.orchestrationList.deleteConfirmAction'),
        cancelButtonText: t('orchestrator.orchestrationList.deleteConfirmCancel'),
        confirmButtonClass: 'el-button--danger'
      }
    )

    await orchestrationAPI.delete(row.id)
    ElMessage.success(t('orchestrator.orchestrationList.deleteSuccess'))
    loadOrchestrations()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(t('orchestrator.orchestrationList.deleteFailed'))
    }
  }
}
</script>

<style scoped>
.orchestration-list {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.header-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}

h2 {
  margin: 0;
}
</style>
