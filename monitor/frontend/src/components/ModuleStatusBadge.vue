<template>
  <div class="module-status-badge">
    <div class="module-header">
      <span class="module-name">{{ module.module }}</span>
      <el-tag
        :type="statusType"
        size="small"
        effect="dark"
      >
        {{ statusText }}
      </el-tag>
    </div>
    <div class="module-info">
      <span v-if="displayLatency">
        {{ t('monitor.module_status.latency', { ms: displayLatency }) }}
      </span>
      <span v-if="taskDiscoverySummary">
        {{ taskDiscoverySummary }}
      </span>
      <span v-if="capabilitiesMessage">
        {{ capabilitiesMessage }}
      </span>
      <span class="message" v-if="module.message">
        {{ module.message }}
      </span>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  module: {
    type: Object,
    required: true
  }
})

const statusType = computed(() => {
  switch (props.module.status) {
    case 'up':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'down':
      return 'danger'
    default:
      return 'info'
  }
})

const statusText = computed(() => {
  switch (props.module.status) {
    case 'up':
      return t('monitor.module_status.up')
    case 'degraded':
      return t('monitor.module_status.degraded')
    case 'down':
      return t('monitor.module_status.down')
    default:
      return t('monitor.module_status.unknown')
  }
})

const displayLatency = computed(() => {
  return props.module.latency || props.module.module_health?.latency || 0
})

const taskDiscoverySummary = computed(() => {
  const checks = props.module.task_discovery || []
  if (checks.length === 0) {
    return ''
  }
  const upCount = checks.filter(item => item.status === 'up').length
  return t('monitor.module_status.task_discovery', { up: upCount, total: checks.length })
})

const capabilitiesMessage = computed(() => {
  const capabilities = props.module.capabilities
  if (!capabilities || capabilities.status === 'up') {
    return ''
  }
  return capabilities.message || t('monitor.module_status.capabilities_invalid')
})
</script>

<style scoped>
.module-status-badge {
  padding: 16px;
  background: var(--addp-bg-primary);
  border-radius: 8px;
  border: 1px solid var(--addp-border-color);
  margin-bottom: 12px;
}

.module-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.module-name {
  font-size: 16px;
  font-weight: bold;
  color: var(--addp-text-primary);
}

.module-info {
  display: flex;
  flex-direction: column;
  font-size: 14px;
  color: var(--addp-text-secondary);
}

.message {
  color: var(--addp-text-tertiary);
  font-size: 12px;
}
</style>
