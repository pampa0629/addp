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
      <span>{{ backendSummary }}</span>
      <span>{{ taskDiscoverySummary }}</span>
      <span v-if="capabilitiesMessage">
        {{ capabilitiesMessage }}
      </span>
      <span v-if="!module.backends?.length && module.message" class="message">
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
  const latencies = (props.module.backends || [])
    .map(item => item.module_health?.latency || 0)
    .filter(value => value > 0)
  return latencies.length > 0 ? Math.max(...latencies) : 0
})

const backendSummary = computed(() => {
  const backends = props.module.backends || []
  if (!backends.some(item => item.module_health)) {
    return t('monitor.module_status.backends_unchecked')
  }
  const upCount = backends.filter(item => item.module_health?.status === 'up').length
  return t('monitor.module_status.backends', { up: upCount, total: backends.length })
})

const taskDiscoverySummary = computed(() => {
  const checks = (props.module.backends || []).flatMap(item => item.task_discovery || [])
  if (checks.length === 0) {
    return t('monitor.module_status.task_discovery_unchecked')
  }
  const upCount = checks.filter(item => item.status === 'up').length
  const deniedCount = checks.filter(item => item.status_code === 403).length
  if (deniedCount > 0) {
    return t('monitor.module_status.task_discovery_denied', { up: upCount, total: checks.length, denied: deniedCount })
  }
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
