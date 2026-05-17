<template>
  <el-dialog
    :model-value="visible"
    width="640px"
    :title="t('transfer.catalogDirectory.dialogTitle')"
    @close="handleClose"
  >
    <div class="picker-container">
      <el-breadcrumb class="picker-breadcrumb" separator="/">
        <el-breadcrumb-item>
          <el-link type="primary" @click="openPath([])">
            {{ t('transfer.catalogDirectory.root') }}
          </el-link>
        </el-breadcrumb-item>
        <el-breadcrumb-item
          v-for="(segment, index) in currentSegments"
          :key="`${segment.kind}:${segment.name}:${index}`"
        >
          <el-link type="primary" @click="openPath(currentSegments.slice(0, index + 1))">
            {{ segment.name }}
          </el-link>
        </el-breadcrumb-item>
      </el-breadcrumb>

      <div class="current-prefix">
        {{ t('transfer.catalogDirectory.currentDir', { path: displayPath(currentSegments) }) }}
      </div>

      <el-skeleton v-if="loading" :rows="4" animated />

      <el-empty
        v-else-if="directories.length === 0"
        :description="t('transfer.catalogDirectory.noSubdirs')"
      />

      <el-table
        v-else
        :data="directories"
        height="320"
        border
        highlight-current-row
        row-key="nodeKey"
        @row-click="handleRowClick"
      >
        <el-table-column :label="t('transfer.catalogDirectory.dirColumn')" min-width="300">
          <template #default="{ row }">
            <span class="directory-cell">
              <el-icon :size="16" class="directory-icon"><Folder /></el-icon>
              <span class="directory-name">{{ row.name }}</span>
            </span>
          </template>
        </el-table-column>
        <el-table-column :label="t('transfer.catalogDirectory.actionsColumn')" width="180" align="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click.stop="enterDirectory(row)">
              {{ t('transfer.catalogDirectory.open') }}
            </el-button>
            <el-button link size="small" @click.stop="selectDirectory(row.segments)">
              {{ t('transfer.catalogDirectory.select') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <template #footer>
      <div class="picker-footer">
        <div class="selected-info">
          {{ t('transfer.catalogDirectory.selectedLabel', { path: displayPath(selectedSegments || currentSegments) }) }}
        </div>
        <div class="footer-actions">
          <el-button @click="handleClose">{{ t('transfer.catalogDirectory.cancel') }}</el-button>
          <el-button
            type="primary"
            :disabled="!engineId"
            @click="selectDirectory(selectedSegments || currentSegments)"
          >
            {{ t('transfer.catalogDirectory.useCurrentDir') }}
          </el-button>
        </div>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Folder } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { listCatalogChildren } from '@/api/meta'

const { t } = useI18n()

const props = defineProps({
  visible: { type: Boolean, default: false },
  engineId: { type: Number, default: null },
  initialPath: { type: String, default: '' },
  storageKind: { type: String, default: '' }
})

const emit = defineEmits(['update:visible', 'selected'])

const loading = ref(false)
const currentSegments = ref([])
const selectedSegments = ref(null)
const directories = ref([])

const pathForCatalog = computed(() => ({
  segments: currentSegments.value
}))

function segmentForNode(node) {
  return {
    name: node.name,
    term: node.term || termForKind(node.kind),
    kind: node.kind || termForKind(node.term)
  }
}

function termForKind(kind) {
  if (kind === 'bucket') return 'bucket'
  if (kind === 'prefix' || kind === 'directory') return 'prefix'
  return kind || 'prefix'
}

function isDirectoryNode(node) {
  const kind = String(node?.kind || node?.term || '').toLowerCase()
  return !!node?.is_container || ['root', 'bucket', 'prefix', 'directory'].includes(kind)
}

function displayPath(segments) {
  const names = (segments || []).map(segment => segment.name).filter(Boolean)
  return names.length > 0 ? names.join('/') : '/'
}

function normalizeInitialPath(path) {
  return String(path || '')
    .split('/')
    .map(part => part.trim())
    .filter(Boolean)
    .map((name, index) => ({
      name,
      term: props.storageKind === 's3' && index === 0 ? 'bucket' : 'prefix',
      kind: props.storageKind === 's3' && index === 0 ? 'bucket' : 'prefix'
    }))
}

async function loadDirectories() {
  if (!props.engineId) return
  loading.value = true
  try {
    const nodes = await listCatalogChildren(props.engineId, pathForCatalog.value)
    directories.value = nodes
      .filter(isDirectoryNode)
      .map(node => {
        const segments = [...currentSegments.value, segmentForNode(node)]
        return {
          ...node,
          nodeKey: displayPath(segments),
          segments
        }
      })
  } catch (error) {
    ElMessage.error(t('transfer.catalogDirectory.loadFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    loading.value = false
  }
}

function openPath(segments) {
  currentSegments.value = segments || []
  selectedSegments.value = null
  loadDirectories()
}

function enterDirectory(row) {
  if (!row?.segments) return
  openPath(row.segments)
}

function handleRowClick(row) {
  selectedSegments.value = row?.segments || null
}

function selectDirectory(segments) {
  emit('selected', displayPath(segments || []))
  emit('update:visible', false)
}

function handleClose() {
  emit('update:visible', false)
}

watch(
  () => props.visible,
  (visible) => {
    if (!visible) return
    currentSegments.value = normalizeInitialPath(props.initialPath)
    selectedSegments.value = null
    loadDirectories()
  }
)
</script>

<style scoped>
.picker-container {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.picker-breadcrumb {
  margin-bottom: 4px;
}

.current-prefix,
.selected-info {
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.directory-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.picker-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}
</style>
