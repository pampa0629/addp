<template>
  <el-button v-if="canViewExecutions" :disabled="scope === 'execution' && !executionRoute" @click="handleClick">
    <slot>{{ label }}</slot>
  </el-button>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { openMonitorExecutions, openConsoleRoute, resolveMonitorExecutionReturnRoute } from '../utils/taskOwnerUrl'
import { getBoundAuthStore } from '../composables/useAuth'

const props = defineProps({
  module: {
    type: String,
    required: true
  },
  taskType: {
    type: String,
    default: ''
  },
  sourceTaskId: {
    type: [String, Number],
    default: ''
  },
  executionId: { type: String, default: '' },
  returnTo: { type: [String, Array], default: undefined },
  scope: {
    type: String,
    default: 'module',
    validator: value => ['module', 'task', 'execution'].includes(value)
  }
})

const { t } = useI18n()
const canViewExecutions = computed(() => {
  try {
    return getBoundAuthStore().hasPermission('monitor.execution.read')
  } catch {
    return false
  }
})

const executionRoute = computed(() => resolveMonitorExecutionReturnRoute(props.executionId, props.returnTo))
const label = computed(() => t(
  props.scope === 'execution' ? 'common.executionMonitor.returnToExecution' :
  props.scope === 'task'
    ? 'common.executionMonitor.viewTaskExecutions'
    : 'common.executionMonitor.viewExecutions'
))

const handleClick = () => {
  if (!canViewExecutions.value) return
  if (props.scope === 'execution') {
    if (executionRoute.value) openConsoleRoute(executionRoute.value)
    return
  }
  openMonitorExecutions({
  module: props.module,
  task_type: props.taskType,
  source_task_id: props.sourceTaskId
  })
}
</script>
