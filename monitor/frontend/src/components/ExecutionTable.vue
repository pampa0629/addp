<template>
  <el-table
    :data="executions"
    stripe
    style="width: 100%"
    @row-click="handleRowClick"
  >
    <el-table-column prop="id" label="ID" width="80" />
    <el-table-column prop="module" label="模块" width="120">
      <template #default="{ row }">
        <el-tag size="small">{{ row.module }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="execution_type" label="执行类型" width="120" />
    <el-table-column prop="status" label="状态" width="100">
      <template #default="{ row }">
        <el-tag :type="getStatusType(row.status)" size="small">
          {{ getStatusText(row.status) }}
        </el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="trigger_type" label="触发方式" width="100" />
    <el-table-column prop="progress" label="进度" width="120">
      <template #default="{ row }">
        <el-progress :percentage="row.progress || 0" :status="getProgressStatus(row.status)" />
      </template>
    </el-table-column>
    <el-table-column prop="created_at" label="创建时间" width="180">
      <template #default="{ row }">
        {{ formatDate(row.created_at) }}
      </template>
    </el-table-column>
    <el-table-column prop="execution_time_ms" label="执行时长" width="120">
      <template #default="{ row }">
        {{ formatDuration(row.execution_time_ms) }}
      </template>
    </el-table-column>
    <el-table-column label="操作" width="100" fixed="right">
      <template #default="{ row }">
        <el-button text size="small" @click.stop="handleView(row)">
          查看详情
        </el-button>
      </template>
    </el-table-column>
  </el-table>
</template>

<script setup>
import { ElMessage } from 'element-plus'

defineProps({
  executions: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['view'])

function getStatusType(status) {
  const typeMap = {
    pending: 'info',
    running: 'warning',
    success: 'success',
    failed: 'danger',
    timeout: 'danger',
    cancelled: 'info'
  }
  return typeMap[status] || 'info'
}

function getStatusText(status) {
  const textMap = {
    pending: '等待中',
    running: '运行中',
    success: '成功',
    failed: '失败',
    timeout: '超时',
    cancelled: '已取消'
  }
  return textMap[status] || status
}

function getProgressStatus(status) {
  if (status === 'success') return 'success'
  if (status === 'failed' || status === 'timeout') return 'exception'
  return undefined
}

function formatDate(date) {
  if (!date) return '-'
  return new Date(date).toLocaleString('zh-CN')
}

function formatDuration(ms) {
  if (!ms) return '-'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`
  return `${(ms / 60000).toFixed(2)}min`
}

function handleRowClick(row) {
  // 可选：点击行也触发查看详情
}

function handleView(row) {
  emit('view', row)
}
</script>

<style scoped>
.el-table {
  cursor: pointer;
}
</style>
