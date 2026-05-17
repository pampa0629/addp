<template>
  <div class="step1-select-source">
    <h3>{{ t('transfer.taskWizard.selectSourcePage') }}</h3>
    <p class="step-description">{{ t('transfer.taskWizard.selectSourcePageDesc') }}</p>

    <el-form :model="formData" label-width="120px">
      <el-form-item :label="t('transfer.taskWizard.sourceEngineLabel')">
        <el-select
          v-model="formData.engineID"
          :placeholder="t('transfer.taskWizard.selectSourceEngine')"
          filterable
          :loading="loadingEngines"
          @change="handleEngineChange"
        >
          <el-option
            v-for="engine in engines"
            :key="engine.id"
            :label="engineOptionLabel(engine)"
            :value="engine.id"
          />
        </el-select>
      </el-form-item>

      <el-form-item v-if="formData.engineID" :label="t('transfer.taskWizard.sourceItemLabel')">
        <ResourceTree
          :tree-data="sourceTreeData"
          :loading="loadingNodes"
          :show-refresh-button="false"
          :show-count="false"
          :default-expand-root="true"
          :expand-on-click-node="true"
          :expanded-keys="expandedNodeKeys"
          :current-node-key="currentNodeKey"
          title=""
          height="360px"
          class="source-resource-tree"
          @node-click="handleTreeNodeClick"
          @node-expand="handleTreeNodeExpand"
          @update:expanded-keys="expandedNodeKeys = $event"
          @update:current-node-key="currentNodeKey = $event"
        >
          <template #node-label="{ data }">
            <span class="tree-node-label">
              <span>{{ data.label }}</span>
              <el-tag v-if="data.metadata?.selectable" size="small" type="success">
                {{ dataTypeLabel(data.metadata.dataType) }}
              </el-tag>
              <el-tag v-if="data.metadata?.format" size="small" type="info">
                {{ formatLabel(data.metadata.format) }}
              </el-tag>
            </span>
          </template>
        </ResourceTree>
      </el-form-item>

      <el-alert
        v-if="selectedNode && !selectedSourceSupported"
        type="warning"
        :closable="false"
        :title="t('transfer.taskWizard.unsupportedSourceDataTypeTitle')"
        :description="t('transfer.taskWizard.unsupportedSourceShapeDesc', {
          dataType: dataTypeLabel(selectedDataType),
          representation: representationLabel(selectedRepresentation),
          format: formatLabel(selectedFormat)
        })"
      />
    </el-form>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ResourceTree } from '@addp/common-frontend'
import { getItemFieldsByCatalogPath, getTableFields, listCatalogChildren } from '@/api/meta'
import { systemEnginesAPI } from '@/api/systemEngines'
import {
  dataTypeLabel,
  engineOptionLabel,
  formatLabel,
  hasStorageCapability,
  isContentEngine,
  representationLabel
} from '@/utils/transferDisplay'

const { t } = useI18n()

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

const formData = reactive({
  engineID: null
})

const engines = ref([])
const sourceTreeData = ref([])
const expandedNodeKeys = ref([])
const currentNodeKey = ref('')
const selectedNode = ref(null)

const loadingEngines = ref(false)
const loadingNodes = ref(false)

const selectedEngine = computed(() => {
  return engines.value.find(engine => engine.id === formData.engineID) || null
})

const selectedDataType = computed(() => nodeDataType(selectedNode.value))
const selectedFormat = computed(() => nodeFormat(selectedNode.value))
const selectedRepresentation = computed(() => representationForSelection(selectedNode.value))
const selectedSourceLabel = computed(() => catalogPathForNode(selectedNode.value))
const selectedSourceSupported = computed(() => {
  if (selectedDataType.value !== 'table') return false
  if (selectedRepresentation.value === 'native') return true
  if (selectedRepresentation.value === 'encoded') return supportedEncodedSourceFormats.includes(selectedFormat.value)
  return false
})

const supportedEncodedSourceFormats = ['csv', 'tsv', 'json', 'jsonl', 'geojson', 'parquet']

watch(selectedNode, (node) => {
  if (node) {
    syncSource(node)
  }
})

async function loadEngines() {
  loadingEngines.value = true
  try {
    const data = await systemEnginesAPI.list()
    engines.value = (data || []).filter(engine =>
      engine?.id !== undefined &&
      engine?.id !== null &&
      hasStorageCapability(engine)
    )
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.loadEnginesFailedMsg'))
  } finally {
    loadingEngines.value = false
  }
}

async function handleEngineChange() {
  sourceTreeData.value = []
  expandedNodeKeys.value = []
  currentNodeKey.value = ''
  selectedNode.value = null
  props.wizardState.loadSourceFields([])
  if (formData.engineID) {
    await loadSourceTreeRoot()
  }
}

async function loadSourceTreeRoot() {
  if (!formData.engineID || !selectedEngine.value) return
  loadingNodes.value = true
  try {
    const root = {
      id: engineRootNodeKey(),
      label: selectedEngine.value.name || selectedEngine.value.display_name || `#${formData.engineID}`,
      type: 'engine',
      hasChildren: true,
      children: [],
      metadata: {
        engineId: formData.engineID,
        engineType: selectedEngine.value.engine_type,
        catalogNode: null,
        childrenLoaded: true
      }
    }
    root.children = (await fetchCatalogChildren([])).map(catalogNodeToTreeNode)
    sourceTreeData.value = [root]
    expandedNodeKeys.value = [root.id]
  } catch (error) {
    sourceTreeData.value = []
    ElMessage.error(t('transfer.taskWizard.loadCatalogFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    loadingNodes.value = false
  }
}

async function fetchCatalogChildren(segments) {
  const normalizedSegments = Array.isArray(segments) ? segments : []
  const nodes = await listCatalogChildren(formData.engineID, { segments: normalizedSegments })
  return nodes.map(node => normalizeNode(node, normalizedSegments))
}

function normalizeNode(node, parentSegments = []) {
  const segments = Array.isArray(node.path?.segments) && node.path.segments.length > 0
    ? node.path.segments
    : [...parentSegments, segmentForNode(node)]
  return {
    ...node,
    path: {
      ...(node.path || {}),
      segments
    },
    nodeKey: displayPath(segments)
  }
}

function segmentForNode(node) {
  return {
    name: node.name,
    term: node.term || node.kind || 'item',
    kind: node.kind || node.term || 'item'
  }
}

async function selectNode(node) {
  selectedNode.value = node
  currentNodeKey.value = catalogTreeNodeKey(node)
  await loadFieldsForNode(node)
}

async function loadFieldsForNode(node) {
  if (nodeDataType(node) !== 'table') {
    props.wizardState.loadSourceFields([])
    return
  }

  const resource = buildSourceResource(node)
  try {
    const response = resource.kind === 'native_table'
      ? await getTableFields(formData.engineID, resource.path.schema, resource.path.table)
      : await getItemFieldsByCatalogPath(formData.engineID, catalogPathForNode(node))
    const fieldList = Array.isArray(response?.data) ? response.data : (response || [])
    props.wizardState.loadSourceFields(fieldList)
  } catch (error) {
    props.wizardState.loadSourceFields([])
    ElMessage.warning(t('transfer.taskWizard.loadSourceFieldsWarning', { error: error.response?.data?.error || error.message }))
  }
}

function syncSource(node) {
  const engine = selectedEngine.value
  if (!engine || !node) return

  const resource = buildSourceResource(node)
  props.wizardState.updateSource({
    engineID: formData.engineID,
    engineType: engine.engine_type,
    scope: 'system',
    schema: resource.path?.schema || '',
    table: resource.path?.table || '',
    sourceType: normalizeEngineType(engine.engine_type),
    dataType: nodeDataType(node),
    representation: representationForSelection(node),
    format: nodeFormat(node),
    resource,
    extra: {
      sourceLabel: catalogPathForNode(node),
      catalogPath: catalogPathForNode(node),
      dataType: nodeDataType(node),
      representation: representationForSelection(node),
      format: nodeFormat(node),
      resource,
      sourceItem: {
        name: node.name,
        kind: node.kind,
        term: node.term,
        path: node.path,
        attributes: node.attributes || {}
      }
    }
  })
}

function buildSourceResource(node) {
  const representation = representationForSelection(node)
  if (representation === 'encoded') {
    return contentResourceFromNode(node)
  }
  return nativeTableResourceFromNode(node)
}

function nativeTableResourceFromNode(node) {
  const names = pathNames(node)
  const table = names[names.length - 1] || node?.name || ''
  const schema = names.length > 1 ? names[names.length - 2] : ''
  return {
    kind: 'native_table',
    path: {
      schema,
      table
    }
  }
}

function contentResourceFromNode(node) {
  const names = pathNames(node)
  if (isObjectStorageEngine(selectedEngine.value?.engine_type)) {
    return {
      kind: 'object',
      path: {
        bucket: names[0] || '',
        path: names.slice(1).join('/')
      }
    }
  }
  return {
    kind: 'file',
    path: {
      path: names.join('/')
    }
  }
}

function representationForSelection(node) {
  if (!node) return ''
  if (nodeAttribute(node, 'representation')) return nodeAttribute(node, 'representation')
  return isContentEngine(selectedEngine.value?.engine_type) ? 'encoded' : 'native'
}

function nodeDataType(node) {
  return nodeAttribute(node, 'data_type') || inferDataTypeFromKind(node)
}

function nodeFormat(node) {
  return nodeAttribute(node, 'format') || inferFormatFromName(node?.name)
}

function nodeAttribute(node, key) {
  if (!node?.attributes) return ''
  const attrs = node.attributes
  return String(attrs[key] || attrs.item?.[key] || attrs.type_info?.[key] || '').trim()
}

function inferDataTypeFromKind(node) {
  const kind = String(node?.kind || node?.term || '').toLowerCase()
  if (['table', 'relation', 'collection'].includes(kind)) return 'table'
  if (['file', 'object'].includes(kind) && tableFormatFromName(node?.name)) return 'table'
  return ''
}

function inferFormatFromName(name) {
  return tableFormatFromName(name)
}

function tableFormatFromName(name) {
  const extension = String(name || '').split('.').pop()?.toLowerCase()
  const formats = {
    csv: 'csv',
    tsv: 'tsv',
    json: 'json',
    jsonl: 'jsonl',
    ndjson: 'jsonl',
    parquet: 'parquet',
    shp: 'shapefile',
    geojson: 'geojson'
  }
  return formats[extension] || ''
}

function pathNames(node) {
  return (node?.path?.segments || [])
    .map(segment => String(segment.name || '').trim())
    .filter(name => name && name !== '.' && name !== '/')
}

function catalogPathForNode(node) {
  if (!node) return ''
  const resource = buildPathOnlyResource(node)
  if (resource.kind === 'native_table') {
    return [resource.path.schema, resource.path.table].filter(Boolean).join('.')
  }
  if (resource.kind === 'object') {
    return [resource.path.bucket, resource.path.path].filter(Boolean).join('/')
  }
  return resource.path.path || pathNames(node).join('/')
}

function buildPathOnlyResource(node) {
  return representationForSelection(node) === 'encoded'
    ? contentResourceFromNode(node)
    : nativeTableResourceFromNode(node)
}

function displayPath(segments) {
  const names = (segments || []).map(segment => segment.name).filter(Boolean)
  return names.length > 0 ? names.join('/') : '/'
}

function isObjectStorageEngine(engineType) {
  const type = String(engineType || '').toLowerCase()
  return type.includes('s3') || type.includes('minio') || type.includes('oss')
}

function normalizeEngineType(engineType) {
  const type = String(engineType || '').toLowerCase()
  if (type.includes('postgres')) return 'postgresql'
  return type
}

async function restoreState() {
  const state = props.wizardState
  if (!state.sourceEngineID.value) return

  formData.engineID = state.sourceEngineID.value
  await loadSourceTreeRoot()
  const restoredNode = restoreSourceNodeFromState(state)
  if (restoredNode) {
    selectedNode.value = normalizeNode(restoredNode)
    await expandAndSelectTreePath(selectedNode.value.path?.segments || [])
    await loadFieldsForNode(selectedNode.value)
  }
}

function restoreSourceNodeFromState(state) {
  const config = state.sourceConfig.value || {}
  const savedItem = config.sourceItem
  if (savedItem) {
    return {
      ...savedItem,
      is_item: savedItem.is_item !== false,
      is_container: false,
      attributes: restoreSourceAttributes(state, savedItem.attributes)
    }
  }

  const resource = state.sourceResource.value || config.resource
  if (!resource?.kind) return null

  if (resource.kind === 'native_table') {
    const schema = resource.path?.schema || state.sourceSchema.value || ''
    const table = resource.path?.table || resource.path?.name || state.sourceTable.value || ''
    return {
      name: table,
      kind: 'table',
      term: 'table',
      is_item: true,
      is_container: false,
      path: {
        segments: [
          schema ? { name: schema, kind: 'schema', term: 'schema' } : null,
          table ? { name: table, kind: 'table', term: 'table' } : null
        ].filter(Boolean)
      },
      attributes: restoreSourceAttributes(state)
    }
  }

  if (resource.kind === 'object') {
    const bucket = resource.path?.bucket || ''
    const objectPath = resource.path?.path || ''
    const objectParts = objectPath.split('/').filter(Boolean)
    const name = objectParts[objectParts.length - 1] || bucket
    return {
      name,
      kind: 'object',
      term: 'object',
      is_item: true,
      is_container: false,
      path: {
        segments: [
          bucket ? { name: bucket, kind: 'bucket', term: 'bucket' } : null,
          ...objectParts.map((part, index) => ({
            name: part,
            kind: index === objectParts.length - 1 ? 'object' : 'prefix',
            term: index === objectParts.length - 1 ? 'object' : 'prefix'
          }))
        ].filter(Boolean)
      },
      attributes: restoreSourceAttributes(state)
    }
  }

  if (resource.kind === 'file') {
    const parts = String(resource.path?.path || '').split('/').filter(Boolean)
    const name = parts[parts.length - 1] || config.sourceLabel || ''
    return {
      name,
      kind: 'file',
      term: 'file',
      is_item: true,
      is_container: false,
      path: {
        segments: parts.map((part, index) => ({
          name: part,
          kind: index === parts.length - 1 ? 'file' : 'directory',
          term: index === parts.length - 1 ? 'file' : 'directory'
        }))
      },
      attributes: restoreSourceAttributes(state)
    }
  }

  return null
}

function restoreSourceAttributes(state, savedAttributes = {}) {
  return {
    ...(savedAttributes || {}),
    data_type: state.sourceDataType.value || savedAttributes?.data_type || 'table',
    representation: state.sourceRepresentation.value || savedAttributes?.representation || 'native',
    format: state.sourceFormat.value || savedAttributes?.format || ''
  }
}

function engineRootNodeKey() {
  return `engine:${formData.engineID}`
}

function catalogNodeToTreeNode(node) {
  const dataType = nodeDataType(node)
  const nodeFormatValue = nodeFormat(node)
  return {
    id: catalogTreeNodeKey(node),
    label: node.name || displayPath(node.path?.segments),
    type: treeNodeType(node),
    hasChildren: Boolean(node.is_container),
    children: node.is_container ? [] : undefined,
    metadata: {
      catalogNode: node,
      selectable: Boolean(node.is_item),
      dataType,
      format: nodeFormatValue,
      childrenLoaded: false
    }
  }
}

function treeNodeType(node) {
  const kind = String(node?.kind || node?.term || '').toLowerCase()
  if (kind === 'namespace') return 'schema'
  if (kind === 'root') return 'directory'
  return kind || (node?.is_container ? 'directory' : 'object')
}

function catalogTreeNodeKey(node) {
  return catalogTreeNodeKeyFromSegments(node?.path?.segments || [])
}

function catalogTreeNodeKeyFromSegments(segments) {
  const path = (segments || [])
    .map(segment => `${segment.kind || segment.term || 'node'}:${segment.name}`)
    .join('/')
  return `catalog:${formData.engineID}:${path}`
}

function findTreeNodeById(id, nodes = sourceTreeData.value) {
  for (const node of nodes || []) {
    if (node.id === id) return node
    const found = findTreeNodeById(id, node.children || [])
    if (found) return found
  }
  return null
}

async function handleTreeNodeClick(treeNode) {
  const catalogNode = treeNode?.metadata?.catalogNode
  if (!catalogNode) return
  if (catalogNode.is_item) {
    await selectNode(catalogNode)
    return
  }
  if (catalogNode.is_container) {
    await loadTreeNodeChildren(treeNode)
  }
}

async function handleTreeNodeExpand(treeNode) {
  await loadTreeNodeChildren(treeNode)
}

async function loadTreeNodeChildren(treeNode) {
  const target = findTreeNodeById(treeNode?.id)
  if (!target || target.metadata?.childrenLoaded) return

  const catalogNode = target.metadata?.catalogNode
  if (!catalogNode?.is_container) return

  loadingNodes.value = true
  try {
    target.children = (await fetchCatalogChildren(catalogNode.path?.segments || [])).map(catalogNodeToTreeNode)
    target.metadata.childrenLoaded = true
    sourceTreeData.value = [...sourceTreeData.value]
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.loadCatalogFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    loadingNodes.value = false
  }
}

async function expandAndSelectTreePath(segments) {
  if (!segments.length) return
  const root = sourceTreeData.value[0]
  if (!root) return

  const keys = new Set([root.id, ...expandedNodeKeys.value])
  let parent = root
  let current = null
  for (let index = 0; index < segments.length; index += 1) {
    const segment = segments[index]
    current = findChildTreeNodeBySegment(parent, segment)
    if (!current) break
    if (index < segments.length - 1) {
      keys.add(current.id)
      await loadTreeNodeChildren(current)
      parent = findTreeNodeById(current.id) || current
    }
  }

  expandedNodeKeys.value = Array.from(keys)
  if (current) {
    currentNodeKey.value = current.id
    if (current.metadata?.catalogNode?.is_item) {
      selectedNode.value = current.metadata.catalogNode
    }
  } else {
    const restored = selectedNode.value
    currentNodeKey.value = restored ? catalogTreeNodeKey(restored) : ''
  }
}

function findChildTreeNodeBySegment(parent, segment) {
  const wantedName = String(segment?.name || '').trim()
  if (!wantedName) return null
  const wantedKind = String(segment?.kind || segment?.term || '').toLowerCase()
  const children = parent?.children || []
  return children.find(child => {
    const catalogNode = child.metadata?.catalogNode
    if (String(catalogNode?.name || child.label || '').trim() !== wantedName) return false
    if (!wantedKind) return true
    const actualKind = String(catalogNode?.kind || catalogNode?.term || child.type || '').toLowerCase()
    return actualKind === wantedKind || compatibleCatalogKinds(actualKind, wantedKind)
  }) || children.find(child => String(child.metadata?.catalogNode?.name || child.label || '').trim() === wantedName) || null
}

function compatibleCatalogKinds(actual, wanted) {
  const groups = [
    ['bucket', 'root', 'schema', 'namespace'],
    ['prefix', 'directory', 'folder'],
    ['object', 'file'],
    ['table', 'relation']
  ]
  return groups.some(group => group.includes(actual) && group.includes(wanted))
}

onMounted(async () => {
  await loadEngines()
  await restoreState()
})
</script>

<style scoped>
.step1-select-source {
  max-width: 1000px;
  margin: 0 auto;
}

.step-description {
  color: var(--addp-text-secondary);
  margin-bottom: 30px;
}

.source-resource-tree {
  width: 100%;
}

.tree-node-label {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
</style>
