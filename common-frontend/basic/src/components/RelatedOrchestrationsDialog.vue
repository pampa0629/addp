<template>
  <el-dialog :model-value="modelValue" :title="title" width="min(860px, calc(100vw - 32px))" class="addp-dialog"
    :close-on-click-modal="!executing" :close-on-press-escape="!executing" :show-close="!executing"
    @update:model-value="$emit('update:modelValue', $event)">
    <p>{{ t('common.orchestration.scopeHelp') }}</p>
    <el-alert v-if="!canRead" :title="t('common.orchestration.noPermission')" type="warning" :closable="false" />
    <el-alert v-else-if="loadError" :title="t('common.orchestration.loadFailed')" type="error" :closable="false">
      <el-button @click="load">{{ t('common.orchestration.retry') }}</el-button>
    </el-alert>
    <div v-else v-loading="loading">
      <el-empty v-if="!loading && !workflows.length" :description="t('common.orchestration.empty')">
        <el-button v-if="canCreate" @click="openConsoleRoute('/orchestrator/orchestrations/new')">{{ t('common.orchestration.configure') }}</el-button>
      </el-empty>
      <el-card v-for="workflow in workflows" :key="workflow.id" shadow="never" class="workflow-card">
        <template #header>
          <div class="workflow-header">
            <strong>{{ workflow.name }}</strong>
            <div class="workflow-actions">
              <el-button v-if="canUpdate" :disabled="executing" @click="openConsoleRoute(`/orchestrator/orchestrations/${workflow.id}/edit`)">{{ t('common.orchestration.edit') }}</el-button>
              <OrchestrationExecuteButton v-if="canExecute" :orchestration="workflow" :execute="api.execute" :disabled="executing || Boolean(executionID)" @busy="executing = $event" @executed="onExecuted" />
            </div>
          </div>
        </template>
        <p v-if="workflow.description">{{ workflow.description }}</p>
        <p>{{ t('common.orchestration.steps', { count: workflow.steps?.length || 0 }) }}</p>
        <ul><li v-for="step in workflow.steps" :key="step.id">{{ step.name }}</li></ul>
      </el-card>
    </div>
    <el-alert v-if="executionID" :title="t('common.orchestration.started')" type="success" :closable="false">
      <el-button @click="openMonitorExecution(executionID)">{{ t('common.orchestration.viewExecution') }}</el-button>
    </el-alert>
  </el-dialog>
</template>
<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import OrchestrationExecuteButton from './OrchestrationExecuteButton.vue'
import { matchesOrchestrationTask } from '../utils/orchestrationRoute'
import { openConsoleRoute, openMonitorExecution } from '../utils/taskOwnerUrl'
const props = defineProps({ modelValue: Boolean, title: String, tasks: { type: Array, required: true }, api: { type: Object, required: true }, canRead: Boolean, canCreate: Boolean, canUpdate: Boolean, canExecute: Boolean })
defineEmits(['update:modelValue'])
const { t } = useI18n()
const workflows = ref([])
const loading = ref(false)
const loadError = ref(false)
const executing = ref(false)
const executionID = ref('')
let sequence = 0
async function load() {
  const current = ++sequence
  workflows.value = []
  loadError.value = false
  if (!props.canRead) return
  loading.value = true
  try {
    const rows = await props.api.list()
    if (current !== sequence) return
    workflows.value = rows.filter(workflow => props.tasks.some(task => matchesOrchestrationTask(workflow, task)))
  } catch {
    if (current === sequence) loadError.value = true
  } finally {
    if (current === sequence) loading.value = false
  }
}
const onExecuted = result => { executionID.value = result.execution_id }
watch(() => [props.modelValue, props.tasks, props.canRead], () => {
  ++sequence
  if (props.modelValue) { executionID.value = ''; load() }
}, { immediate: true, deep: true })
</script>
<style scoped>
.workflow-card { margin: 12px 0; }
.workflow-header, .workflow-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.workflow-header { justify-content: space-between; }
</style>
