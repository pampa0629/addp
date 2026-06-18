<template>
  <el-card shadow="never" class="node-panel">
    <template #header>
      <div class="panel-header">
        <span class="header-title">{{ selectedNode?.label || t('manager.explorer.dataPreview') }}</span>
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
  </el-card>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { MagicStick } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { parseLocator } from '@addp/common-frontend'
import client from '@/api/client'
import { isVectorizableRangeNode } from '@/utils/vectorization'

const { t } = useI18n()

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

watch(() => props.selectedNode?.locator, () => {
  childNodePage.value = 1
  itemPage.value = 1
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

const showVectorizeButton = computed(() => isVectorizableRangeNode(props.selectedNode))

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
</script>

<style scoped>
.node-panel {
  display: flex;
  flex-direction: column;
  border: none;
}

:deep(.el-card__body) {
  overflow: visible;
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

.empty-state {
  padding-top: 40px;
}
</style>
