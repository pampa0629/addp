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
      <span class="latency" v-if="module.latency">
        {{ t('monitor.module_status.latency', { ms: module.latency }) }}
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
    case 'down':
      return t('monitor.module_status.down')
    default:
      return t('monitor.module_status.unknown')
  }
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

.latency {
  margin-bottom: 4px;
}

.message {
  color: var(--addp-text-tertiary);
  font-size: 12px;
}
</style>
