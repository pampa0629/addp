<template>
  <div class="orchestration-list">
    <div class="header">
      <h2>{{ t('orchestrator.orchestrationList.title') }}</h2>
      <el-button type="primary" @click="handleCreate">{{ t('orchestrator.orchestrationList.createBtn') }}</el-button>
    </div>

    <el-table :data="orchestrations" style="width: 100%" v-loading="loading">
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
          <el-button
            size="small"
            type="success"
            :loading="executingId === scope.row.id"
            :disabled="executingId !== null"
            @click="handleExecute(scope.row)"
          >
            {{ t('orchestrator.orchestrationList.executeBtn') }}
          </el-button>
          <el-button size="small" type="info" @click="handleViewExecutions(scope.row)">{{ t('orchestrator.orchestrationList.recordsBtn') }}</el-button>
          <el-button size="small" type="danger" @click="handleDelete(scope.row)">{{ t('orchestrator.orchestrationList.deleteBtn') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import orchestrationAPI from '../api/orchestration'
import { describeCron } from '@common-ui'
import { navigateOrchestratorRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()
const router = useRouter()
const orchestrations = ref([])
const loading = ref(false)
const executingId = ref(null)

onMounted(() => {
  loadOrchestrations()
})

async function loadOrchestrations() {
  loading.value = true
  try {
    orchestrations.value = await orchestrationAPI.list()
  } catch (error) {
    ElMessage.error(t('orchestrator.orchestrationList.loadFailed'))
  } finally {
    loading.value = false
  }
}

function handleCreate() {
  navigateOrchestratorRoute(router, '/orchestrations/new')
}

function handleEdit(row) {
  navigateOrchestratorRoute(router, `/orchestrations/${row.id}/edit`)
}

async function handleExecute(row) {
  try {
    await ElMessageBox.confirm(
      t('orchestrator.orchestrationList.executeConfirmMessage', { name: row.name }),
      t('orchestrator.orchestrationList.executeConfirmTitle'),
      {
        type: 'warning',
        customClass: 'addp-message-box',
        confirmButtonText: t('orchestrator.orchestrationList.executeConfirmAction'),
        cancelButtonText: t('orchestrator.orchestrationList.executeConfirmCancel')
      }
    )

    executingId.value = row.id
    await orchestrationAPI.execute(row.id)
    ElMessage.success(t('orchestrator.orchestrationList.executeSuccess'))
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(t('orchestrator.orchestrationList.executeFailed'))
    }
  } finally {
    executingId.value = null
  }
}

function handleViewExecutions(row) {
  navigateOrchestratorRoute(router, `/orchestrations/${row.id}/executions`)
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

h2 {
  margin: 0;
}
</style>
