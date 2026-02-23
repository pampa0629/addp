<template>
  <div class="excel-preview">
    <div v-if="summaryItems.length" class="excel-summary">
      <div v-for="item in summaryItems" :key="item.label" class="summary-item">
        <span class="label">{{ item.label }}</span>
        <span class="value">{{ item.value }}</span>
      </div>
    </div>

    <div class="excel-alerts">
      <el-alert
        v-if="summary?.sheets_truncated"
        type="info"
        show-icon
        :closable="false"
        title="仅展示部分工作表"
      >
        <template #default>
          当前仅展示前 {{ summary.sheet_limit ?? sheets.length }} 个工作表，可下载 Excel 查看全部内容。
        </template>
      </el-alert>
      <el-alert
        v-if="summary?.rows_truncated"
        type="info"
        show-icon
        :closable="false"
        title="示例数据有限"
      >
        <template #default>
          每个工作表仅展示前 {{ summary.row_limit ?? excelSampleRows }} 行样例数据。
        </template>
      </el-alert>
    </div>

    <div v-if="sheets.length" class="excel-body">
      <el-tabs v-model="activeTab" class="sheet-tabs">
        <el-tab-pane
          v-for="sheet in sheets"
          :key="sheet.index"
          :name="String(sheet.index)"
          :label="sheetLabel(sheet)"
        >
          <div class="sheet-meta">
            <el-descriptions border size="small" :column="3">
              <el-descriptions-item label="工作表">{{ sheet.name || `Sheet ${sheet.index + 1}` }}</el-descriptions-item>
              <el-descriptions-item label="示例行数">{{ formatNumber(sheet.rows?.length || 0) }}</el-descriptions-item>
              <el-descriptions-item label="总行数">
                {{ sheet.row_count != null ? formatNumber(sheet.row_count) : '—' }}
              </el-descriptions-item>
              <el-descriptions-item label="列数">{{ formatNumber(sheet.column_count || 0) }}</el-descriptions-item>
              <el-descriptions-item label="包含表头">{{ sheet.has_header ? '是' : '否' }}</el-descriptions-item>
              <el-descriptions-item label="示例截断">{{ sheet.rows_truncated ? '是' : '否' }}</el-descriptions-item>
            </el-descriptions>
          </div>

          <div class="column-tags" v-if="getColumnPairs(sheet).length">
            <el-tag
              v-for="[header, type] in getColumnPairs(sheet)"
              :key="`${sheet.index}-${header}`"
              size="small"
              effect="plain"
            >
              {{ header }}: {{ type }}
            </el-tag>
          </div>

          <el-table
            v-if="sheet.headers && sheet.headers.length"
            :data="sheet.rows || []"
            height="420"
            border
            stripe
            class="sheet-table"
          >
            <el-table-column
              v-for="header in sheet.headers"
              :key="`${sheet.index}-${header}`"
              :prop="header"
              :label="header"
              show-overflow-tooltip
              min-width="140"
            />
          </el-table>
          <el-empty v-else description="该工作表暂无可展示的列" />

          <el-alert
            v-if="sheet.rows_truncated"
            type="info"
            :closable="false"
            show-icon
            title="仅展示部分数据"
            class="sheet-alert"
          >
            <template #default>
              当前仅展示前 {{ excelSampleRows }} 行示例，实际数据可能更多。
            </template>
          </el-alert>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-empty v-else description="未能解析 Excel 工作表" />
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'

const props = defineProps({
  data: {
    type: Object,
    default: () => ({})
  }
})

const excelSampleRows = 20

const content = computed(() => props.data?.object?.content ?? {})
const json = computed(() => content.value?.json ?? {})
const summary = computed(() => json.value?.summary ?? {})
const sheets = computed(() => {
  const list = json.value?.sheets
  return Array.isArray(list) ? list : []
})

const defaultSheetName = computed(() => json.value?.active_sheet || json.value?.default_sheet || '')

const activeTab = ref('')

const ensureActiveTab = () => {
  const list = sheets.value
  if (!Array.isArray(list) || list.length === 0) {
    activeTab.value = ''
    return
  }
  const target = list.find((sheet) => sheet.name === defaultSheetName.value) || list[0]
  activeTab.value = String(target.index ?? 0)
}

watch([sheets, defaultSheetName], ensureActiveTab, { immediate: true })

const activeSheet = computed(() => {
  const list = sheets.value
  if (!Array.isArray(list) || !list.length || !activeTab.value) {
    return null
  }
  const idx = Number.parseInt(activeTab.value, 10)
  if (Number.isNaN(idx)) {
    return list[0]
  }
  return list.find((sheet) => sheet.index === idx) || list[0]
})

const formatNumber = (value) => {
  if (typeof value !== 'number' || Number.isNaN(value)) return '—'
  return new Intl.NumberFormat().format(value)
}

const formatBytes = (bytes) => {
  if (typeof bytes !== 'number' || Number.isNaN(bytes) || bytes < 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = bytes
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index++
  }
  const digits = index === 0 ? 0 : 2
  return `${size.toFixed(digits)} ${units[index]}`
}

const summaryItems = computed(() => {
  const meta = summary.value || {}
  const items = [
    { label: '工作表总数', value: formatNumber(meta.sheet_count ?? sheets.value.length) },
    { label: '已加载工作表', value: formatNumber(meta.sampled_sheets ?? sheets.value.length) },
    { label: '示例行数上限', value: formatNumber(meta.row_limit ?? excelSampleRows) },
    { label: '列数上限', value: formatNumber(meta.column_limit ?? 0) }
  ]
  if (meta.size_bytes != null) {
    items.push({ label: '文件大小', value: formatBytes(meta.size_bytes) })
  }
  return items
})

const sheetLabel = (sheet) => {
  if (!sheet) return 'Sheet'
  const name = sheet.name || `Sheet ${Number(sheet.index ?? 0) + 1}`
  if (typeof sheet.row_count === 'number') {
    return `${name} (${formatNumber(sheet.row_count)} 行)`
  }
  return name
}

const getColumnPairs = (sheet) => {
  if (!sheet || !Array.isArray(sheet.headers)) return []
  const types = Array.isArray(sheet.column_types) ? sheet.column_types : []
  return sheet.headers.map((header, idx) => [header, types[idx] || 'string'])
}
</script>

<style scoped>
.excel-preview {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.excel-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.summary-item {
  background: #f4f7ff;
  border-radius: 6px;
  padding: 10px 14px;
  display: flex;
  flex-direction: column;
  min-width: 140px;
}

.summary-item .label {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-bottom: 4px;
}

.summary-item .value {
  font-size: 16px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.excel-alerts :deep(.el-alert) + :deep(.el-alert) {
  margin-top: 8px;
}

.excel-body {
  background: var(--addp-bg-primary);
  border: 1px solid #ebeef5;
  border-radius: 6px;
  padding: 8px;
}

.sheet-tabs :deep(.el-tabs__header) {
  margin-bottom: 12px;
}

.sheet-meta {
  margin-bottom: 12px;
}

.column-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 12px;
}

.sheet-alert {
  margin-top: 12px;
}
</style>
