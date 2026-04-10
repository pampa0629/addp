<template>
  <div class="sql-result-container">
    <!-- 结果信息栏 -->
    <div class="result-info" v-if="result">
      <el-tag v-if="result.success" type="success">
        <el-icon><SuccessFilled /></el-icon>
        {{ t('develop.queryResult.success') }}
      </el-tag>
      <el-tag v-else type="danger">
        <el-icon><CircleCloseFilled /></el-icon>
        {{ t('develop.queryResult.failed') }}
      </el-tag>

      <span class="info-item" v-if="result.rows_count !== undefined">
        {{ t('develop.queryResult.rowsCount') }}: <strong>{{ result.rows_count }}</strong>
      </span>
      <span class="info-item" v-if="result.rows_affected !== undefined">
        {{ t('develop.queryResult.rowsAffected') }}: <strong>{{ result.rows_affected }}</strong>
      </span>
      <span class="info-item" v-if="result.execution_time_ms">
        {{ t('develop.queryResult.executionTime') }}: <strong>{{ result.execution_time_ms }}ms</strong>
      </span>

      <el-button
        v-if="result.rows && result.rows.length > 0"
        type="primary"
        size="small"
        @click="exportCSV"
        style="margin-left: auto;"
      >
        <el-icon><Download /></el-icon>
        {{ t('develop.queryResult.exportCsv') }}
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
      :description="t('develop.queryResult.emptyHint')"
      :image-size="120"
    />

    <el-empty
      v-else-if="result.success && (!result.rows || result.rows.length === 0)"
      :description="t('develop.queryResult.noData')"
      :image-size="100"
    />
  </div>
</template>

<script setup>
import { defineProps } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { SuccessFilled, CircleCloseFilled, Download } from '@element-plus/icons-vue'

const { t } = useI18n()

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
    ElMessage.warning(t('develop.queryResult.noExportData'))
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

    ElMessage.success(t('develop.queryResult.exportSuccess'))
  } catch (error) {
    ElMessage.error(t('develop.queryResult.exportFailed') + error.message)
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
