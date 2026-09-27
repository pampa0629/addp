<template>
  <el-button v-if="canViewExecutions" @click="handleClick">
    <slot>{{ label }}</slot>
  </el-button>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { openMonitorExecutions } from '../utils/taskOwnerUrl'
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
  scope: {
    type: String,
    default: 'module',
    validator: value => ['module', 'task'].includes(value)
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

const label = computed(() => t(
  props.scope === 'task'
    ? 'common.executionMonitor.viewTaskExecutions'
    : 'common.executionMonitor.viewExecutions'
))

const handleClick = () => {
  if (!canViewExecutions.value) return
  openMonitorExecutions({
  module: props.module,
  task_type: props.taskType,
  source_task_id: props.sourceTaskId
  })
}
</script>
