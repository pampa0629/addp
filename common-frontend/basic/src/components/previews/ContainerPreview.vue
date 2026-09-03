<template>
  <div class="container-preview">
    <div v-if="summaryItems.length" class="container-summary">
      <div v-for="item in summaryItems" :key="item.label" class="summary-item">
        <span class="label">{{ item.label }}</span>
        <span class="value">{{ item.value }}</span>
      </div>
    </div>

    <div class="container-alerts">
      <el-alert
        v-if="truncated"
        type="info"
        show-icon
        :closable="false"
        :title="t('containerPreview.childrenTruncatedTitle')"
      >
        <template #default>
          {{ t('containerPreview.childrenTruncatedBody', { limit: loadedChildrenCount }) }}
        </template>
      </el-alert>
    </div>

    <div v-if="children.length" class="container-body">
      <div class="child-toolbar">
        <span class="child-toolbar-label">{{ selectorLabel }}</span>
        <el-select
          v-model="activeChildKey"
          size="small"
          class="child-select"
          :disabled="children.length <= 1"
          @change="handleChildSelect"
        >
          <el-option
            v-for="child in children"
            :key="child.key"
            :label="childLabel(child)"
            :value="child.key"
          />
        </el-select>
        <template v-if="activeRefOptions.length">
          <span class="child-toolbar-label">{{ t('containerPreview.refs') }}</span>
          <el-select
            v-model="activeRefPath"
            size="small"
            class="ref-select"
            @change="handleRefSelect"
          >
            <el-option
              v-for="ref in activeRefOptions"
              :key="ref.key"
              :label="ref.label"
              :value="ref.path"
            />
          </el-select>
        </template>
      </div>

      <template v-if="activeChild">
        <div class="child-meta">
          <el-descriptions border size="small" :column="3">
            <el-descriptions-item :label="t('containerPreview.childName')">{{ activeChild.name || activeChild.label || activeChild.key }}</el-descriptions-item>
            <el-descriptions-item :label="t('containerPreview.currentPageRows')">{{ formatNumber(activeRows.length || 0) }}</el-descriptions-item>
            <el-descriptions-item :label="t('containerPreview.totalRows')">{{ activeTotal ? formatNumber(activeTotal) : '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('containerPreview.columns')">{{ formatNumber(activeColumnCount) }}</el-descriptions-item>
            <el-descriptions-item v-if="activeChild.hasHeader !== undefined" :label="t('containerPreview.hasHeader')">
              {{ activeChild.hasHeader ? t('common.yes') : t('common.no') }}
            </el-descriptions-item>
          </el-descriptions>
        </div>

        <div class="column-tags" v-if="activeColumnPairs.length">
          <el-tag
            v-for="[header, type] in activeColumnPairs"
            :key="`${activeChild.key}-${header}`"
            size="small"
            effect="plain"
          >
            {{ header }}: {{ type }}
          </el-tag>
        </div>

        <div class="child-preview">
          <component
            v-if="activePreviewComponent"
            :is="activePreviewComponent"
            :data="activeChildPreviewData"
            :loading="activeChildLoading"
            @page-change="handlePageChange"
            @child-change="handleNestedChildChange"
          />
          <TabularResultRenderer
            v-else-if="activeColumns.length"
            :loading="activeChildLoading"
            :rows="activeRows"
            :columns="activeColumns"
            :height="420"
            border
            stripe
            class="child-table"
          />
          <el-empty
            v-else-if="!activeChildLoading"
            :description="activeChildNoPreviewText"
          />

          <div v-if="showTablePagination" class="child-pagination">
            <DataPagination
              :total="activeTotal"
              :page-size="activePageSize"
              :current-page="activePage"
              layout="prev, pager, next"
              @change="handlePageChange"
            />
          </div>
        </div>
      </template>
    </div>

    <el-empty v-else :description="emptyText || t('containerPreview.empty')" />
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import DataPagination from '../DataPagination.vue'
import TabularResultRenderer from '../TabularResultRenderer.vue'

const { t } = useI18n()

const props = defineProps({
  summaryItems: {
    type: Array,
    default: () => []
  },
  children: {
    type: Array,
    default: () => []
  },
  defaultChildKey: {
    type: String,
    default: ''
  },
  selectedChildKey: {
    type: String,
    default: ''
  },
  selectedRefPath: {
    type: String,
    default: ''
  },
  selectorLabel: {
    type: String,
    default: ''
  },
  activeChildPreview: {
    type: Object,
    default: null
  },
  activeChildLoading: {
    type: Boolean,
    default: false
  },
  activeChildPreviewComponent: {
    type: [Object, Function],
    default: null
  },
  truncated: {
    type: Boolean,
    default: false
  },
  emptyText: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['child-change', 'page-change'])

const activeChildKey = ref('')
const activeRefPath = ref('')

const normalizedChildren = computed(() => {
  return props.children.filter(child => child && child.key)
})

const children = computed(() => normalizedChildren.value)

const loadedChildrenCount = computed(() => children.value.length)

const ensureActiveChild = () => {
  const list = children.value
  if (!list.length) {
    activeChildKey.value = ''
    return
  }
  const preferredKey = props.selectedChildKey || props.defaultChildKey
  const target = list.find(child => child.key === preferredKey || child.name === preferredKey || child.table === preferredKey) || list[0]
  activeChildKey.value = target.key
}

watch([children, () => props.defaultChildKey, () => props.selectedChildKey], ensureActiveChild, { immediate: true })

watch(activeChildKey, () => {
  activeRefPath.value = ''
})

watch(
  () => props.selectedRefPath,
  (path) => {
    activeRefPath.value = path || ''
  },
  { immediate: true }
)

const activeChild = computed(() => {
  return children.value.find(child => child.key === activeChildKey.value) || children.value[0] || null
})

const refDisplayName = (path) => {
  if (!path) return ''
  const parts = String(path).split(/[\\/]/).filter(Boolean)
  return parts.pop() || String(path)
}

const activeRefOptions = computed(() => {
  const refs = Array.isArray(activeChild.value?.refs) ? activeChild.value.refs : []
  const options = refs
    .filter(ref => ref && ref.path)
    .map((ref, index) => {
      const path = String(ref.path || '')
      const label = ref.label || ref.role || ref.key || refDisplayName(path) || String(index)
      const fileName = refDisplayName(path)
      return {
        key: ref.key || ref.role || path || String(index),
        path,
        label: fileName && !String(label).includes(fileName) ? `${label} · ${fileName}` : String(label)
      }
    })
  if (!options.length) return []
  return [
    {
      key: '__combined__',
      path: '',
      label: t('containerPreview.combinedPreview')
    },
    ...options
  ]
})

const activeChildPreviewData = computed(() => props.activeChildPreview || null)
const activePreviewComponent = computed(() => props.activeChildPreviewComponent || null)

const activePreviewRows = computed(() => {
  const rows = props.activeChildPreview?.rows
  return Array.isArray(rows) ? rows : []
})

const activePreviewColumns = computed(() => {
  const columns = props.activeChildPreview?.columns
  return Array.isArray(columns) ? columns : []
})

const activeRows = computed(() => {
  if (activePreviewRows.value.length) return activePreviewRows.value
  const rows = activeChild.value?.rows
  return Array.isArray(rows) ? rows : []
})

const activeColumns = computed(() => {
  if (activePreviewColumns.value.length) return activePreviewColumns.value
  const columns = activeChild.value?.columns
  return Array.isArray(columns) ? columns : []
})

const activeTotal = computed(() => Number(props.activeChildPreview?.total || activeChild.value?.rowCount || 0))
const activePage = computed(() => Number(props.activeChildPreview?.page || 1))
const activePageSize = computed(() => Number(props.activeChildPreview?.page_size || 20))
const activeColumnCount = computed(() => Number(activeChild.value?.columnCount || activeColumns.value.length || 0))
const showTablePagination = computed(() => !activePreviewComponent.value && activeTotal.value > 0)

const activeColumnPairs = computed(() => {
  const child = activeChild.value
  if (!child || !Array.isArray(child.columns)) return []
  const types = Array.isArray(child.columnTypes) ? child.columnTypes : []
  return child.columns.map((header, index) => [header, types[index] || 'string'])
})

const activeChildNoPreviewText = computed(() => {
  if (!activeChild.value) return t('containerPreview.noPreview')
  const dataType = activeChild.value.dataType || activeChild.value.data_type || ''
  if (dataType) {
    return t('containerPreview.noPreviewForType', { type: dataType })
  }
  return t('containerPreview.noPreview')
})

const formatNumber = (value) => {
  if (typeof value !== 'number' || Number.isNaN(value)) return '—'
  return new Intl.NumberFormat().format(value)
}

const displayedChildRowCount = (child) => {
  if (!child) return undefined
  if (activeChild.value?.key === child.key && Number(props.activeChildPreview?.total) > 0) {
    return Number(props.activeChildPreview.total)
  }
  return typeof child.rowCount === 'number' && child.rowCount > 0 ? child.rowCount : undefined
}

const childLabel = (child) => {
  const label = child.label || child.name || child.key
  const rowCount = displayedChildRowCount(child)
  if (typeof rowCount === 'number') {
    return `${label} (${t('containerPreview.rowCount', { count: formatNumber(rowCount) })})`
  }
  return label
}

const handleChildSelect = (key) => {
  const child = children.value.find(item => item.key === key)
  if (!child) return
  emit('child-change', { ...child, nestedChildPath: '' })
}

const handleRefSelect = (path) => {
  activeRefPath.value = path || ''
  if (!activeChild.value) return
  emit('child-change', {
    ...activeChild.value,
    refPath: path || '',
    nestedChildPath: '',
    refSwitch: true
  })
}

const handleNestedChildChange = (payload) => {
  if (!activeChild.value) return
  const childName = typeof payload === 'string' ? payload : payload?.childName || payload?.name || payload?.key
  const childPath = typeof payload === 'object' ? payload?.nestedChildPath || '' : ''
  const nestedPath = [childName, childPath]
    .map(value => String(value || '').replace(/^\/+|\/+$/g, ''))
    .filter(Boolean)
    .join('/')
  if (!nestedPath) return
  emit('child-change', {
    ...activeChild.value,
    nestedChildPath: nestedPath
  })
}

const handlePageChange = (page) => {
  emit('page-change', page)
}
</script>

<style scoped>
.container-preview {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 16px;
  min-height: 0;
  overflow: hidden;
}

.container-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.summary-item {
  background: var(--el-fill-color);
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

.container-alerts :deep(.el-alert) + :deep(.el-alert) {
  margin-top: 8px;
}

.container-body {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
  background: var(--addp-bg-primary);
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  padding: 8px;
  overflow: hidden;
}

.child-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 8px;
}

.child-toolbar-label {
  color: var(--addp-text-secondary);
  font-size: 13px;
  white-space: nowrap;
}

.child-select {
  width: min(360px, 100%);
}

.ref-select {
  width: min(320px, 100%);
}

.child-meta {
  margin-bottom: 12px;
}

.column-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 12px;
}

.child-preview {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
  overflow: hidden;
}

.child-pagination {
  display: flex;
  justify-content: flex-end;
}
</style>
