<template>
  <div class="schedule-display">
    <el-icon v-if="cron" class="schedule-icon"><Clock /></el-icon>
    <span v-if="cron" class="schedule-text">
      {{ describeCron(cron) }}
    </span>
    <el-tag v-else type="info" size="small">{{ emptyText }}</el-tag>
  </div>
</template>

<script setup>
import { Clock } from '@element-plus/icons-vue'
import { describeCron } from '../utils/schedule'

defineProps({
  cron: {
    type: String,
    default: ''
  },
  emptyText: {
    type: String,
    default: '手动触发'
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
  color: #303133;
}
</style>
