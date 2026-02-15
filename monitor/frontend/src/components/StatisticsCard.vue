<template>
  <el-card class="statistics-card" shadow="hover">
    <div class="card-header">
      <el-icon :size="32" :color="computedIconColor">
        <component :is="icon" />
      </el-icon>
      <span class="title">{{ title }}</span>
    </div>
    <div class="card-body">
      <span class="value">{{ value }}</span>
      <span v-if="subtitle" class="subtitle">{{ subtitle }}</span>
    </div>
  </el-card>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  title: {
    type: String,
    required: true
  },
  value: {
    type: [String, Number],
    required: true
  },
  subtitle: {
    type: String,
    default: ''
  },
  icon: {
    type: String,
    default: 'DataAnalysis'
  },
  type: {
    type: String,
    default: '',
    validator: (value) => ['', 'primary', 'success', 'warning', 'danger', 'info'].includes(value)
  }
})

// 计算图标颜色：优先使用 type 对应的 Element Plus 颜色变量
const computedIconColor = computed(() => {
  const typeColorMap = {
    primary: 'var(--el-color-primary)',      // 蓝色
    success: 'var(--el-color-success)',      // 绿色
    warning: 'var(--el-color-warning)',      // 橙色
    danger: 'var(--el-color-danger)',        // 红色
    info: 'var(--el-color-info)'             // 灰色
  }

  return typeColorMap[props.type] || 'var(--el-color-primary)'
})
</script>

<style scoped>
.statistics-card {
  cursor: pointer;
  transition: all 0.3s;
}

.statistics-card:hover {
  transform: translateY(-5px);
}

.card-header {
  display: flex;
  align-items: center;
  margin-bottom: 20px;
}

.card-header .title {
  margin-left: 10px;
  font-size: 16px;
  color: var(--addp-text-secondary);
}

.card-body {
  display: flex;
  flex-direction: column;
}

.card-body .value {
  font-size: 32px;
  font-weight: bold;
  color: var(--addp-text-primary);
  margin-bottom: 8px;
}

.card-body .subtitle {
  font-size: 14px;
  color: var(--addp-text-tertiary);
}
</style>
