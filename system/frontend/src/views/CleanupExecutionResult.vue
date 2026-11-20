<template>
  <ExecutionResultPage
    :key="parentId"
    module="system"
    :title="t('system.cleanup.detail.title')"
    :execution-id="childId || parentId"
    :return-to="route.query.return_to"
    :read="readResult"
    v-slot="{ execution }"
  >
    <el-descriptions border :column="2">
      <el-descriptions-item :label="t('system.cleanup.detail.taskId')">{{ execution.task_id }}</el-descriptions-item>
      <el-descriptions-item :label="t('system.cleanup.detail.action')">
        {{ t(execution.action === 'scan' ? 'system.cleanup.history.actionScan' : 'system.cleanup.history.actionCleanup') }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('system.cleanup.detail.status')">{{ t(`common.cleanupObservation.status.${execution.status}`) }}</el-descriptions-item>
    </el-descriptions>
    <CleanupModuleResults :executions="execution.rows" :request-id="execution.task_id" :identity-generation="0"
      :can-read-result="canReadResult" :read-result="cleanupApi.readProfessionalResult" />
  </ExecutionResultPage>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { CleanupModuleResults, ExecutionResultPage, parseCleanupExecutions } from '@common-ui'
import { cleanupApi } from '../api/cleanup'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()
const parentId = computed(() => route.params.parentId)
const childId = computed(() => route.params.childId)
const canReadResult = owner => auth.hasPermission('system.cleanup.read') && auth.hasPermission(`${owner}.cleanup_result.read`)

async function readResult(id, options) {
  const parent = parentId.value
  const child = childId.value
  const history = await cleanupApi.getTaskHistory({ parent_execution_id: parent, page: 1, page_size: 1 }, options)
  const task = history.tasks?.[0]
  if (history.total !== 1 || history.tasks?.length !== 1 || task?.execution_id !== parent || !task.task_id) throw new Error('cleanup_parent_mismatch')
  const detail = await cleanupApi.getTaskStatus(task.task_id, options)
  if (detail.task_id !== task.task_id || detail.task?.execution_id !== parent) throw new Error('cleanup_parent_mismatch')
  const rows = parseCleanupExecutions(detail)
  if (rows?.some(row => row.parent_execution_id !== parent)) throw new Error('cleanup_parent_mismatch')
  if (child) {
    const row = rows?.find(item => item.execution_id === id)
    if (!row) throw new Error('cleanup_child_unavailable')
    return { task_id: task.task_id, action: detail.action, execution_id: row.execution_id, status: row.status, rows: [row] }
  }
  return { task_id: task.task_id, action: detail.action, execution_id: parent, status: detail.status, rows }
}
</script>
