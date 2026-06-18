<template>
  <div class="nfs-file-picker">
    <!-- 引擎选择 -->
    <div class="picker-row">
      <span class="picker-label">{{ t('develop.nfsFilePicker.engine') }}</span>
      <el-select
        v-model="selectedEngineId"
        :placeholder="t('develop.nfsFilePicker.selectEngine')"
        filterable
        :loading="loadingEngines"
        style="width: 100%"
        @change="onEngineChange"
      >
        <el-option
          v-for="engine in nfsEngines"
          :key="engine.id"
          :label="engine.name || engine.display_name"
          :value="engine.id"
        >
          <div class="engine-option">
            <span>{{ engine.name || engine.display_name }}</span>
            <el-tag size="small" type="info">nfs</el-tag>
          </div>
        </el-option>
      </el-select>
    </div>

    <!-- 文件路径输入框（选中后显示，点击可重新展开树） -->
    <div v-if="selectedEngineId" class="picker-row">
      <span class="picker-label">{{ t('develop.nfsFilePicker.path') }}</span>
      <el-input
        v-model="manualPath"
        :placeholder="t('develop.nfsFilePicker.pathPlaceholder')"
        size="small"
        clearable
        @focus="showTree = true"
        @clear="onClear"
        @change="onManualPathChange"
      >
        <template #suffix>
          <el-icon v-if="selectedPath" style="color: var(--el-color-success)"><Check /></el-icon>
        </template>
      </el-input>
    </div>

    <!-- 文件树（未选中文件时显示，或点击路径框时展开） -->
    <div v-if="selectedEngineId && showTree" class="file-tree-container">
      <div v-loading="loadingTree" class="tree-wrapper">
        <el-empty v-if="!loadingTree && treeData.length === 0" :description="t('develop.nfsFilePicker.emptyTree')" :image-size="60" />
        <el-tree
          v-else
          ref="treeRef"
          :data="treeData"
          :props="treeProps"
          node-key="id"
          :highlight-current="true"
          :expand-on-click-node="false"
          :lazy="true"
          :load="loadChildren"
          @node-click="onNodeClick"
        >
          <template #default="{ data }">
            <span class="tree-node">
              <el-icon v-if="data.type === 'directory' || data.type === 'folder'"><Folder /></el-icon>
              <el-icon v-else><Document /></el-icon>
              <span class="node-label" :class="{ 'is-file': data.type !== 'directory' && data.type !== 'folder' }">
                {{ data.label }}
              </span>
            </span>
          </template>
        </el-tree>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Folder, Document, Check } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { getResourceTree, getResourceTreeNode, listResourceTreeEngines, parseLocatorSafe } from '@addp/common-frontend'

const { t } = useI18n()

const props = defineProps({
  engineId: { type: [Number, String], default: null },
  path: { type: String, default: '' }
})

const emit = defineEmits(['update:engineId', 'update:path', 'change'])

const selectedEngineId = ref(props.engineId)
const selectedPath = ref(props.path)
const manualPath = ref(props.path)
const showTree = ref(!props.path) // 有初始路径时默认折叠

const engines = ref([])
const loadingEngines = ref(false)
const treeData = ref([])
const loadingTree = ref(false)
const treeRef = ref(null)

const treeProps = {
  label: 'label',
  children: 'children',
  isLeaf: (data) => data.type !== 'directory' && data.type !== 'folder' && !data.hasChildren
}

const nfsEngines = computed(() => engines.value)

// 从文件路径推断格式
const EXT_FORMAT_MAP = {
  csv: 'csv', tsv: 'csv',
  parquet: 'parquet',
  xlsx: 'xlsx', xls: 'xlsx',
  json: 'json',
  feather: 'feather',
  shp: 'shp',
  geojson: 'geojson',
  gpkg: 'gpkg',
  kml: 'kml',
  gml: 'gml',
  fgb: 'fgb'
}

function inferFormat(path) {
  if (!path) return null
  const ext = path.split('.').pop()?.toLowerCase()
  return EXT_FORMAT_MAP[ext] || null
}

const META_API_BASE_URL = '/api/v1/meta'

// 从 locator 中提取绝对路径
function extractPathFromLocator(locator) {
  const parsed = parseLocatorSafe(locator)
  if (!parsed.path?.length) {
    return ''
  }
  return `/${parsed.path.join('/')}`
}

async function loadEngines() {
  loadingEngines.value = true
  try {
    engines.value = await listResourceTreeEngines(META_API_BASE_URL, { engineTypes: ['nfs'] })
  } catch (e) {
    ElMessage.error(t('develop.nfsFilePicker.loadEnginesFailed'))
  } finally {
    loadingEngines.value = false
  }
}

async function loadRootTree(engineId) {
  loadingTree.value = true
  treeData.value = []
  try {
    const root = await getResourceTree(META_API_BASE_URL, engineId, { expandDepth: 1 })
    treeData.value = root?.children || []
  } catch (e) {
    ElMessage.error(t('develop.nfsFilePicker.loadTreeFailed'))
  } finally {
    loadingTree.value = false
  }
}

async function loadChildren(node, resolve) {
  const data = node.data
  if (!data?.locator) return resolve([])
  try {
    const result = await getResourceTreeNode(META_API_BASE_URL, selectedEngineId.value, data.locator)
    resolve(result?.children || [])
  } catch {
    resolve([])
  }
}

function onEngineChange(engineId) {
  selectedPath.value = ''
  manualPath.value = ''
  showTree.value = true
  emit('update:engineId', engineId)
  emit('update:path', '')
  emit('change', { engineId, path: '', format: null })
  loadRootTree(engineId)
}

function onNodeClick(data) {
  // 只允许选择文件节点（非目录）
  if (data.type === 'directory' || data.type === 'folder') return
  const path = extractPathFromLocator(data.locator) || ('/' + data.label)
  selectedPath.value = path
  manualPath.value = path
  showTree.value = false // 选中文件后折叠树
  const format = inferFormat(path)
  emit('update:path', path)
  emit('change', { engineId: selectedEngineId.value, path, format })
}

function onClear() {
  selectedPath.value = ''
  showTree.value = true
  // 重新加载树（lazy tree 销毁重建后需要重新加载）
  loadRootTree(selectedEngineId.value)
  emit('change', { engineId: selectedEngineId.value, path: '', format: null })
}

function onManualPathChange(val) {
  selectedPath.value = val
  const format = inferFormat(val)
  emit('update:path', val)
  emit('change', { engineId: selectedEngineId.value, path: val, format })
}

onMounted(async () => {
  await loadEngines()
  if (selectedEngineId.value) {
    loadRootTree(selectedEngineId.value)
  }
})
</script>

<style scoped>
.nfs-file-picker {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.picker-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.picker-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.file-tree-container {
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  overflow: hidden;
}

.tree-wrapper {
  max-height: 240px;
  overflow-y: auto;
  padding: 4px 0;
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 13px;
}

.node-label.is-file {
  cursor: pointer;
  color: var(--el-color-primary);
}

.node-label.is-file:hover {
  text-decoration: underline;
}

.selected-path {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--el-color-success);
  padding: 4px 6px;
  background: var(--el-color-success-light-9);
  border-radius: 4px;
}

.engine-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}
</style>
