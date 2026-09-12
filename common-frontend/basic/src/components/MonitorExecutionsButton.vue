<template>
  <el-button @click="handleClick">
    <slot>{{ label }}</slot>
  </el-button>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { openMonitorExecutions } from '../utils/taskOwnerUrl'

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

const label = computed(() => t(
  props.scope === 'task'
    ? 'common.executionMonitor.viewTaskExecutions'
    : 'common.executionMonitor.viewExecutions'
))

const handleClick = () => openMonitorExecutions({
  module: props.module,
  task_type: props.taskType,
  source_task_id: props.sourceTaskId
})
</script>
