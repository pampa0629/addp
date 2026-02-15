<template>
  <div class="sql-result-container">
    <!-- 结果信息栏 -->
    <div class="result-info" v-if="result">
      <el-tag v-if="result.success" type="success">
        <el-icon><SuccessFilled /></el-icon>
        执行成功
      </el-tag>
      <el-tag v-else type="danger">
        <el-icon><CircleCloseFilled /></el-icon>
        执行失败
      </el-tag>

      <span class="info-item" v-if="result.rows_count !== undefined">
        返回行数: <strong>{{ result.rows_count }}</strong>
      </span>
      <span class="info-item" v-if="result.rows_affected !== undefined">
        影响行数: <strong>{{ result.rows_affected }}</strong>
      </span>
      <span class="info-item" v-if="result.execution_time_ms">
        执行时间: <strong>{{ result.execution_time_ms }}ms</strong>
      </span>

      <el-button
        v-if="result.rows && result.rows.length > 0"
        type="primary"
        size="small"
        @click="exportCSV"
        style="margin-left: auto;"
      >
        <el-icon><Download /></el-icon>
        导出 CSV
      </el-button>
    </div>

    <!-- 错误信息 -->
    <el-alert
      v-if="result && !result.success && result.error"
      type="error"
      :title="result.error"
      :closable="false"
      show-icon
    />

    <!-- 结果表格 -->
    <el-table
      v-if="result && result.rows && result.rows.length > 0"
      :data="result.rows"
      stripe
      border
      height="100%"
      style="width: 100%"
      :default-sort="{ prop: result.columns[0], order: 'ascending' }"
    >
      <el-table-column
        v-for="col in result.columns"
        :key="col"
        :prop="col"
        :label="col"
        :sortable="true"
        :show-overflow-tooltip="true"
        min-width="120"
      >
        <template #default="{ row }">
          <span :class="getValueClass(row[col])">
            {{ formatValue(row[col]) }}
          </span>
        </template>
      </el-table-column>
    </el-table>

    <!-- 空状态 -->
    <el-empty
      v-if="!result"
      description="执行 查询后结果将显示在这里"
      :image-size="120"
    />

    <el-empty
      v-else-if="result.success && (!result.rows || result.rows.length === 0)"
      description="查询成功，但没有返回数据"
      :image-size="100"
    />
  </div>
</template>

<script setup>
import { defineProps } from 'vue'
import { ElMessage } from 'element-plus'
import { SuccessFilled, CircleCloseFilled, Download } from '@element-plus/icons-vue'

const props = defineProps({
  result: {
    type: Object,
    default: null
  }
})

// 格式化值显示
const formatValue = (value) => {
  if (value === null) return 'NULL'
  if (value === undefined) return ''
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

// 获取值的样式类
const getValueClass = (value) => {
  if (value === null) return 'null-value'
  if (typeof value === 'number') return 'number-value'
  if (typeof value === 'boolean') return 'boolean-value'
  return ''
}

// 导出为 CSV
const exportCSV = () => {
  if (!props.result?.rows || !props.result?.columns) {
    ElMessage.warning('没有可导出的数据')
    return
  }

  try {
    // 创建 CSV 内容
    const headers = props.result.columns.join(',')
    const rows = props.result.rows.map(row => {
      return props.result.columns.map(col => {
        const value = row[col]
        if (value === null) return 'NULL'
        // 转义包含逗号的值
        const strValue = String(value)
        return strValue.includes(',') ? `"${strValue}"` : strValue
      }).join(',')
    })

    const csv = [headers, ...rows].join('\n')

    // 下载文件
    const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8;' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = `query_result_${Date.now()}.csv`
    link.click()
    URL.revokeObjectURL(link.href)

    ElMessage.success('导出成功')
  } catch (error) {
    ElMessage.error('导出失败: ' + error.message)
  }
}
</script>

<style scoped>
.sql-result-container {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--addp-bg-primary);
  border-radius: 4px;
}

.result-info {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 16px;
  background: var(--addp-bg-secondary);
  border-bottom: 1px solid var(--addp-border-color);
}

.info-item {
  font-size: 14px;
  color: var(--addp-text-secondary);
}

.info-item strong {
  color: var(--addp-text-primary);
  margin-left: 4px;
}

.null-value {
  color: var(--addp-text-tertiary);
  font-style: italic;
}

.number-value {
  color: var(--el-color-primary);
  font-weight: 500;
}

.boolean-value {
  color: var(--el-color-success);
  font-weight: 500;
}

.el-table {
  flex: 1;
  overflow: auto;
}

.el-empty {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}
</style>
