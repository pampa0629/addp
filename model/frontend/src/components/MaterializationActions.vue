<template>
  <div class="materialization-actions">
    <el-button v-if="can('orchestrator.workflow.read') && available" :disabled="disabled" @click="visible = true">{{ t('model.materialization.workflows') }}</el-button>
    <el-popover trigger="click" width="280">
      <template #reference><el-button>{{ t('model.materialization.execution_records') }}</el-button></template>
      <div class="record-actions">
        <template v-if="tableId">
          <MonitorExecutionsButton module="model" task-type="materialization_prepare" :source-task-id="tableId" scope="task">{{ t('model.materialization.prepare_records') }}</MonitorExecutionsButton>
          <MonitorExecutionsButton module="model" task-type="materialization_seal" :source-task-id="tableId" scope="task">{{ t('model.materialization.seal_records') }}</MonitorExecutionsButton>
          <MonitorExecutionsButton module="model" task-type="materialization_publish" :source-task-id="tableId" scope="task">{{ t('model.materialization.publish_records') }}</MonitorExecutionsButton>
        </template>
        <MonitorExecutionsButton v-for="group in groups" :key="group.id" module="model" task-type="materialization_group_publish" :source-task-id="group.id" scope="task">{{ t('model.materialization.group_records', { name: group.name }) }}</MonitorExecutionsButton>
      </div>
    </el-popover>
    <RelatedOrchestrationsDialog v-model="visible" :title="t('model.materialization.workflow_title', { name })" :tasks="tasks" :api="api"
      :can-read="can('orchestrator.workflow.read')" :can-create="can('orchestrator.workflow.create')" :can-update="can('orchestrator.workflow.update')" :can-execute="can('orchestrator.workflow.execute')" />
  </div>
</template>
<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { createOrchestrationAPI, MonitorExecutionsButton, RelatedOrchestrationsDialog } from '@common-ui'
import client from '../api/client'
import { useAuthStore } from '../store/auth'
const props = defineProps({ tableId: Number, groups: { type: Array, default: () => [] }, name: String, available: { type: Boolean, default: true }, disabled: Boolean })
const { t } = useI18n()
const authStore = useAuthStore()
const can = permission => authStore.hasPermission(permission)
const api = createOrchestrationAPI(client)
const visible = ref(false)
const tasks = computed(() => props.groups.length
  ? props.groups.map(group => ({ module: 'model', task_type: 'materialization_group_publish', task_id: group.id }))
  : [{ module: 'model', task_type: 'materialization_publish', task_id: props.tableId }])
</script>
<style scoped>
.materialization-actions { display:flex; flex-wrap:wrap; gap:8px; }
.record-actions { display:flex; flex-direction:column; gap:8px; }
.record-actions :deep(.el-button) { margin:0; white-space:normal; height:auto; min-height:32px; }
</style>
