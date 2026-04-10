<template>
  <div class="schedule-display">
    <el-icon v-if="cron" class="schedule-icon"><Clock /></el-icon>
    <span v-if="cron" class="schedule-text">
      {{ describeCron(cron, t) }}
    </span>
    <el-tag v-else type="info" size="small">{{ emptyText || t('schedule.manualTrigger') }}</el-tag>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { Clock } from '@element-plus/icons-vue'
import { describeCron } from '../utils/schedule'

const { t } = useI18n()

defineProps({
  cron: {
    type: String,
    default: ''
  },
  emptyText: {
    type: String,
    default: ''
  }
})
</script>

<style scoped>
.schedule-display {
  display: flex;
  align-items: center;
  gap: 6px;
}

.schedule-icon {
  color: #409eff;
  font-size: 14px;
}

.schedule-text {
  font-size: 13px;
  color: var(--addp-text-primary);
}
</style>
