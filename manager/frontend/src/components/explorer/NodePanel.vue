<template>
  <el-card shadow="never" class="node-panel">
    <template #header>
      <div class="panel-header">
        <span class="header-title">{{ selectedNode?.label || t('manager.explorer.dataPreview') }}</span>
        <div class="header-actions">
          <el-button
            v-if="showUploadButton"
            size="small"
            type="primary"
            @click="uploadDialogVisible = true"
          >
            <el-icon><Upload /></el-icon>
            {{ t('manager.explorer.uploadData') }}
          </el-button>
          <el-button
            v-if="showImportButton"
            size="small"
            type="warning"
            @click="importDialogVisible = true"
          >
            <el-icon><Upload /></el-icon>
            {{ t('manager.explorer.importData') }}
          </el-button>
          <el-button
            v-if="showVectorizeButton"
            size="small"
            type="success"
            :loading="vectorizing"
            @click="handleVectorizeNode"
          >
            <el-icon><MagicStick /></el-icon>
            {{ t('manager.explorer.batchVectorize') }}
          </el-button>
        </div>
      </div>
    </template>

    <div v-if="!selectedNode" class="empty-state">
      <el-empty :description="t('manager.explorer.selectDataToPreview')" />
    </div>

    <div v-else class="panel-content">
      <el-descriptions :column="1" border class="meta-block">
        <el-descriptions-item :label="t('meta.itemType')">{{ typeLabel }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.fullName')">{{ fullName }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.itemCount')">{{ metadataItemCount }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.scanStatus')">{{ scanStatusLabel }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.scannedAt')">{{ scannedAt }}</el-descriptions-item>
      </el-descriptions>

      <el-divider>{{ t('meta.nodeChildren') }}</el-divider>
      <el-table :data="pagedChildNodes" size="small" class="node-table" empty-text="-">
        <el-table-column prop="label" :label="t('manager.explorer.name')" min-width="180">
          <template #default="scope">
            <el-button link type="primary" @click="openNode(scope.row)">{{ scope.row.label }}</el-button>
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.explorer.type')" width="140">
          <template #default="scope">
            {{ resolveTypeLabel(scope.row) }}
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-if="childNodeTotal > pageSize"
        small
        layout="prev, pager, next"
        :total="childNodeTotal"
        :page-size="pageSize"
        :current-page="childNodePage"
        @current-change="childNodePage = $event"
      />

      <el-divider>{{ t('meta.nodeItems') }}</el-divider>
      <el-table :data="pagedItems" size="small" class="node-table" empty-text="-">
        <el-table-column prop="label" :label="t('manager.explorer.name')" min-width="180">
          <template #default="scope">
            <el-button link type="primary" @click="openNode(scope.row)">{{ scope.row.label }}</el-button>
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.explorer.type')" width="160">
          <template #default="scope">
            {{ resolveTypeLabel(scope.row) }}
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-if="itemTotal > pageSize"
        small
        layout="prev, pager, next"
        :total="itemTotal"
        :page-size="pageSize"
        :current-page="itemPage"
        @current-change="itemPage = $event"
      />
    </div>

    <ImportDialog
      v-model="importDialogVisible"
      :engine-id="selectedEngineId"
      :engine-name="selectedEngineName"
      :schema-name="selectedNode?.label || ''"
      :target-node-locator="selectedNode?.locator || ''"
      @success="handleImportSuccess"
    />
    <UploadDialog
      v-model="uploadDialogVisible"
      :target-node-locator="selectedNode?.locator || ''"
      :target-label="selectedNode?.label || ''"
      @success="handleUploadSuccess"
    />
  </el-card>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { MagicStick, Upload } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { parseLocator } from '@addp/common-frontend'
import client from '@/api/client'
import { dataExplorerAPI } from '@/api/dataExplorer'
import ImportDialog from '@/components/explorer/ImportDialog.vue'
import UploadDialog from '@/components/explorer/UploadDialog.vue'
import { useExplorerStore } from '@/stores/explorer'
import { isVectorizableRangeNode } from '@/utils/vectorization'

const { t } = useI18n()
const store = useExplorerStore()

const props = defineProps({
  selectedNode: {
    type: Object,
    default: null
  },
  children: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['open-node'])

const pageSize = 50
const childNodePage = ref(1)
const itemPage = ref(1)
const vectorizing = ref(false)
const importDialogVisible = ref(false)
const uploadDialogVisible = ref(false)
const resourceActions = ref(null)

watch(() => props.selectedNode?.locator, () => {
  childNodePage.value = 1
  itemPage.value = 1
  importDialogVisible.value = false
  uploadDialogVisible.value = false
})

const itemTypes = new Set(['table', 'view', 'collection', 'graph', 'file', 'object'])

const nodeChildren = computed(() => (props.children || []).filter(n => !itemTypes.has(n.type)))
const itemChildren = computed(() => (props.children || []).filter(n => itemTypes.has(n.type)))

const childNodeTotal = computed(() => nodeChildren.value.length)
const itemTotal = computed(() => itemChildren.value.length)

const pagedChildNodes = computed(() => {
  const start = (childNodePage.value - 1) * pageSize
  return nodeChildren.value.slice(start, start + pageSize)
})

const pagedItems = computed(() => {
  const start = (itemPage.value - 1) * pageSize
  return itemChildren.value.slice(start, start + pageSize)
})

const typeLabel = computed(() => {
  const nodeType = props.selectedNode?.type
  const key = props.selectedNode?.typeLabel || (nodeType ? `engine.term.${nodeType}` : '')
  if (!key) return '-'
  const result = t(key)
  return result === key ? (nodeType || '-') : result
})

const fullName = computed(() => {
  return props.selectedNode?.metadata?.full_name || props.selectedNode?.metadata?.path || props.selectedNode?.label || '-'
})

const metadataItemCount = computed(() => {
  return props.selectedNode?.metadata?.item_count ?? 0
})

const scanStatusLabel = computed(() => {
  const rawStatus = props.selectedNode?.metadata?.scan_status
  if (!rawStatus) return '-'

  const statusMap = {
    '未扫描': 'pending',
    '扫描中': 'running',
    '已扫描': 'completed',
    '扫描失败': 'failed'
  }
  const status = statusMap[rawStatus] || rawStatus

  const key = `meta.status.${status}`
  const translated = t(key)
  return translated === key ? status : translated
})

const scannedAt = computed(() => props.selectedNode?.metadata?.scanned_at || '-')
const selectedEngineId = computed(() => {
  const direct = Number(props.selectedNode?.engineId || props.selectedNode?.engine_id || 0)
  if (direct > 0) {
    return direct
  }
  const locator = props.selectedNode?.locator || props.selectedNode?.id || ''
  if (!locator) {
    return 0
  }
  try {
    return Number(parseLocator(locator)?.engineId || 0)
  } catch {
    return 0
  }
})
const selectedEngineName = computed(() => {
  const directName = String(props.selectedNode?.engineName || props.selectedNode?.engine_name || '').trim()
  if (directName) {
    return directName
  }
  const engine = store.engines.find(item => Number(item.id) === selectedEngineId.value)
  return engine?.name || ''
})

const showVectorizeButton = computed(() => isVectorizableRangeNode(props.selectedNode))
const showUploadButton = computed(() => resourceActions.value?.actions?.upload?.supported === true)
const showImportButton = computed(() => resourceActions.value?.actions?.import?.supported === true)

watch(
  () => props.selectedNode?.locator || '',
  async (locator) => {
    resourceActions.value = null
    if (!locator) return
    try {
      const response = await dataExplorerAPI.getResourceActions(locator)
      resourceActions.value = response?.data || response
    } catch (error) {
      resourceActions.value = null
      console.warn('加载资源动作能力失败:', error)
    }
  },
  { immediate: true }
)

const handleVectorizeNode = async () => {
  const node = props.selectedNode
  if (!node || vectorizing.value) return

  const locator = node.locator || node.id
  try {
    const loc = parseLocator(locator)
    if (!loc.nodeId) {
      ElMessage.warning(t('manager.explorer.batchVectorizeDirOnly'))
      return
    }

    vectorizing.value = true
    const response = await client.post('/manager/embedding_executions', {
      scope: 'node',
      target: {
        engine_id: loc.engineId,
        node_id: loc.nodeId,
        locator,
        recursive: true
      }
    })
    const payload = response?.data || response
    ElMessage.success(t('manager.explorer.vectorizeSubmitted', { key: payload?.execution_id || node.label }))
  } catch (error) {
    console.error('节点向量化失败:', error)
    ElMessage.error(t('manager.explorer.vectorizeFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    vectorizing.value = false
  }
}

const resolveTypeLabel = (row) => {
  const rowType = row?.type
  const key = row?.typeLabel || (rowType ? `engine.term.${rowType}` : '')
  if (!key) return '-'
  const translated = t(key)
  return translated === key ? (rowType || '-') : translated
}

const openNode = (row) => {
  emit('open-node', row?.locator || row?.id)
}

const handleImportSuccess = async () => {
  importDialogVisible.value = false
  if (!props.selectedNode?.locator) return
  try {
    await store.loadNodeChildren(props.selectedNode.locator, true)
    ElMessage.success(t('manager.explorer.importSuccessRefreshed'))
  } catch (error) {
    console.error('刷新节点失败:', error)
  }
}

const handleUploadSuccess = async () => {
  uploadDialogVisible.value = false
  if (!props.selectedNode?.locator) return
  try {
    await store.loadNodeChildren(props.selectedNode.locator, true)
    ElMessage.success(t('manager.explorer.uploadSuccessRefreshed'))
  } catch (error) {
    console.error('刷新节点失败:', error)
  }
}
</script>

<style scoped>
.node-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  border: none;
}

:deep(.el-card__body) {
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.panel-content {
  min-height: 0;
}

.meta-block {
  margin-bottom: 12px;
}

.node-table {
  margin-bottom: 8px;
}

.header-title {
  font-weight: 600;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.header-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  margin-left: auto;
}

.empty-state {
  padding-top: 40px;
}
</style>
