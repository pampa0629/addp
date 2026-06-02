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
        <div v-if="selectedSourceSummary" class="selected-source-summary">
          <div class="summary-main">
            <span class="summary-title">{{ t('transfer.taskWizard.selectedSourceSummaryTitle') }}</span>
            <span class="summary-path" :title="selectedSourceSummary.path">{{ selectedSourceSummary.path }}</span>
            <el-tag size="small" type="success">{{ selectedSourceSummary.dataType }}</el-tag>
            <el-tag v-if="selectedSourceSummary.format" size="small" type="info">
              {{ selectedSourceSummary.format }}
            </el-tag>
            <el-tag v-if="selectedSourceSummary.spatial" size="small" type="warning">
              {{ t('transfer.taskWizard.spatialDataLabel') }}
            </el-tag>
          </div>
          <div class="summary-items">
            <span
              v-for="item in selectedSourceSummary.items"
              :key="item.label"
              class="summary-item"
              :title="`${item.label}: ${item.value}`"
            >
              <span class="summary-label">{{ item.label }}</span>
              <span class="summary-value" :title="item.value">{{ item.value }}</span>
            </span>
          </div>
          <div v-if="selectedSourceSummary.spatialInfo" class="summary-spatial">
            <span class="summary-label">{{ t('transfer.taskWizard.spatialInfoLabel') }}</span>
            <span class="summary-spatial-value" :title="selectedSourceSummary.spatialInfo">
              {{ selectedSourceSummary.spatialInfo }}
            </span>
          </div>
        </div>
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
import { capabilitiesAPI } from '@/api/capabilities'
import { getItemFieldsByCatalogPath, getTableFields, getTransferEngineTree, getTransferNodeChildren } from '@/api/meta'
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
const supportedEncodedSourceFormats = ref(new Set())
const supportedRawCopyFormats = ref(new Map())

const selectedEngine = computed(() => {
  return engines.value.find(engine => engine.id === formData.engineID) || null
})

const selectedDataType = computed(() => nodeDataType(selectedNode.value))
const selectedFormat = computed(() => nodeFormat(selectedNode.value))
const selectedRepresentation = computed(() => representationForSelection(selectedNode.value))
const selectedSourceLabel = computed(() => catalogPathForNode(selectedNode.value))
const selectedSourceSummary = computed(() => buildSelectedSourceSummary(selectedNode.value))
const selectedSourceSupported = computed(() => {
  return isSupportedSourceShape(selectedDataType.value, selectedRepresentation.value, selectedFormat.value)
})

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

async function loadCapabilities() {
  try {
    const data = await capabilitiesAPI.get()
    const formats = (data?.table_formats || data?.tableFormats || [])
      .filter(format => format?.read)
      .map(format => String(format.value || '').toLowerCase())
      .filter(Boolean)
    supportedEncodedSourceFormats.value = new Set(formats)
    const rawCopyFormats = data?.raw_copy_formats || data?.rawCopyFormats || []
    const nextRawCopyFormats = new Map()
    rawCopyFormats.forEach(format => {
      const value = String(format?.value || '').toLowerCase()
      const dataType = String(format?.data_type || format?.dataType || '').toLowerCase()
      if (value && dataType) {
        nextRawCopyFormats.set(value, dataType)
      }
    })
    supportedRawCopyFormats.value = nextRawCopyFormats
    props.wizardState.updateFormatCapabilities({
      readableEncodedFormats: formats,
      rawCopyFormats: nextRawCopyFormats
    })
  } catch (error) {
    supportedEncodedSourceFormats.value = new Set()
    supportedRawCopyFormats.value = new Map()
    props.wizardState.updateFormatCapabilities()
    ElMessage.warning(t('transfer.taskWizard.loadCapabilitiesFailedMsg'))
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
    const root = normalizeTreeNode(await getTransferEngineTree(formData.engineID, 1))
    sourceTreeData.value = [root]
    expandedNodeKeys.value = [root.id]
  } catch (error) {
    sourceTreeData.value = []
    ElMessage.error(t('transfer.taskWizard.loadCatalogFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    loadingNodes.value = false
  }
}

function normalizeTreeNode(node) {
  const normalized = {
    ...node,
    id: node.id || node.locator,
    label: node.label || node.name || displayPath(pathSegmentsFromTreeNode(node)),
    type: treeNodeType(node),
    hasChildren: Boolean(node.hasChildren || node.has_children),
    children: Array.isArray(node.children) ? node.children.map(normalizeTreeNode).filter(visibleSourceTreeNode) : [],
    metadata: {
      ...(node.metadata || {}),
      childrenLoaded: Array.isArray(node.children) && node.children.length > 0
    }
  }
  normalized.metadata.catalogNode = treeNodeToCatalogEntry(normalized)
  normalized.metadata.selectable = isSelectableSourceItem(normalized.metadata.catalogNode)
  normalized.metadata.dataType = nodeDataType(normalized.metadata.catalogNode)
  normalized.metadata.format = nodeFormat(normalized.metadata.catalogNode)
  return normalized
}

function treeNodeToCatalogEntry(node) {
  const metadata = node.metadata || {}
  const attributes = {
    ...(metadata.attributes || {}),
    ...standardAttributeSections(metadata)
  }
  const segments = pathSegmentsFromTreeNode(node)
  return {
    name: node.label,
    kind: node.type,
    term: node.type,
    role: catalogRoleFromTreeNode(node),
    path: {
      segments
    },
    attributes,
    meta_id: metadata.meta_id,
    full_name: metadata.full_name,
    data_type: metadata.data_type,
    representation: metadata.representation,
    format: metadata.format,
    layout: metadata.layout,
    physical_path: metadata.physical_path,
    item_id: metadata.item_id,
    meta_id: metadata.item_id || metadata.meta_id,
    size_bytes: metadata.size_bytes,
    last_modified_at: metadata.last_modified_at,
    row_count: metadata.row_count,
    field_count: metadata.field_count,
    spatial: metadata.spatial
  }
}

function standardAttributeSections(metadata) {
  const sections = {}
  for (const key of ['item', 'storage', 'type_info', 'format_info', 'content_index', 'capabilities']) {
    if (metadata[key] && typeof metadata[key] === 'object') {
      sections[key] = metadata[key]
    }
  }
  return sections
}

function pathSegmentsFromTreeNode(node) {
  const locatorPath = parseLocatorPath(node.locator || node.id || '')
  if (locatorPath.length > 0) {
    return locatorPath.map((name, index) => ({
      name,
      kind: index === locatorPath.length - 1 ? treeNodeType(node) : containerKindForPath(index),
      term: index === locatorPath.length - 1 ? treeNodeType(node) : containerKindForPath(index)
    }))
  }
  return String(node.metadata?.full_name || node.label || '')
    .split(pathSeparatorForNode(node))
    .map(part => part.trim())
    .filter(Boolean)
    .map((name, index, parts) => ({
      name,
      kind: index === parts.length - 1 ? treeNodeType(node) : containerKindForPath(index),
      term: index === parts.length - 1 ? treeNodeType(node) : containerKindForPath(index)
    }))
}

function parseLocatorPath(locator) {
  const match = String(locator || '').match(/^addp:\/\/engine\/[^/]+\/path\/([^?]*)/)
  if (!match) return []
  return match[1]
    .split('/')
    .map(part => decodeURIComponent(part).trim())
    .filter(Boolean)
}

function pathSeparatorForNode(node) {
  return isContentEngine(selectedEngine.value) || ['object', 'file', 'directory', 'dir', 'prefix', 'bucket'].includes(treeNodeType(node)) ? '/' : '.'
}

function containerKindForPath(index) {
  if (isObjectStorageEngine(selectedEngine.value?.engine_type) && index === 0) return 'bucket'
  return isContentEngine(selectedEngine.value) ? 'directory' : 'schema'
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

  const endpointResource = buildSourceEndpointResource(node)
  try {
    const response = endpointResource.kind === 'native_table'
      ? await getTableFields(formData.engineID, endpointResource.path.schema, endpointResource.path.table)
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

  const endpointResource = buildSourceEndpointResource(node)
  props.wizardState.updateSource({
    engineID: formData.engineID,
    engineType: engine.engine_type,
    schema: endpointResource.path?.schema || '',
    table: endpointResource.path?.table || '',
    sourceType: normalizeEngineType(engine.engine_type),
    dataType: nodeDataType(node),
    representation: representationForSelection(node),
    format: nodeFormat(node),
    resource: endpointResource,
    extra: {
      sourceLabel: catalogPathForNode(node),
      catalogPath: catalogPathForNode(node),
      dataType: nodeDataType(node),
      representation: representationForSelection(node),
      format: nodeFormat(node),
      resource: endpointResource,
      sourceItem: {
        item_id: node.item_id,
        meta_id: node.meta_id || node.item_id,
        full_name: node.full_name,
        name: node.name,
        kind: node.kind,
        term: node.term,
        path: node.path,
        data_type: nodeDataType(node),
        representation: representationForSelection(node),
        format: nodeFormat(node),
        layout: node.layout,
        physical_path: node.physical_path,
        attributes: node.attributes || {}
      }
    }
  })
}

function buildSourceEndpointResource(node) {
  const representation = representationForSelection(node)
  if (representation === 'encoded') {
    return contentEndpointResourceFromNode(node)
  }
  return nativeTableEndpointResourceFromNode(node)
}

function nativeTableEndpointResourceFromNode(node) {
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

function contentEndpointResourceFromNode(node) {
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
  if (nodeItemAttribute(node, 'representation')) return nodeItemAttribute(node, 'representation')
  return isContentEngine(selectedEngine.value?.engine_type) ? 'encoded' : 'native'
}

function nodeDataType(node) {
  return nodeAttribute(node, 'data_type') || nodeItemAttribute(node, 'data_type')
}

function nodeFormat(node) {
  return nodeAttribute(node, 'format') || nodeItemAttribute(node, 'format')
}

function nodeAttribute(node, key) {
  return String(node?.[key] || '').trim()
}

function nodeItemAttribute(node, key) {
  return String(node?.attributes?.item?.[key] || '').trim()
}

function buildSelectedSourceSummary(node) {
  if (!node) return null

  const loadedFields = props.wizardState.sourceFields?.value || []
  const fieldCount = firstPresent(
    numericValue(node.field_count),
    loadedFields.length || null
  )
  const rowCount = firstPresent(
    numericValue(node.row_count)
  )
  const size = firstPresent(
    numericValue(node.size_bytes)
  )
  const modified = firstPresent(
    node.last_modified_at
  )
  const format = selectedFormat.value
  const spatial = node.spatial || spatialSummaryFromFields(loadedFields)

  const items = []

  if (rowCount !== null && rowCount !== undefined) {
    items.push({
      label: t('transfer.taskWizard.rowCount'),
      value: `${formatInteger(rowCount)} ${t('transfer.taskWizard.rowsUnit')}`
    })
  }
  if (fieldCount !== null && fieldCount !== undefined) {
    items.push({
      label: t('transfer.taskWizard.sourceItemFields'),
      value: formatInteger(fieldCount)
    })
  }
  if (spatial) {
    // 空间信息通常更长，摘要条里单独换行展示，避免挤占行数/字段数等基础信息。
  }
  if (size !== null && size !== undefined) {
    items.push({
      label: t('transfer.taskWizard.fileSize'),
      value: formatBytes(size)
    })
  }
  if (modified) {
    items.push({
      label: t('transfer.taskWizard.lastModified'),
      value: formatDateTime(modified)
    })
  }

  return {
    path: selectedSourceLabel.value || node.name || '-',
    dataType: dataTypeLabel(selectedDataType.value),
    format: format ? formatLabel(format) : '',
    spatial,
    spatialInfo: spatial
      ? [spatial.geometry, spatial.geometryType, spatial.srid ? `SRID ${spatial.srid}` : ''].filter(Boolean).join(' · ')
      : '',
    items
  }
}

function spatialSummaryFromFields(fields) {
  const geometryField = geometryFieldFromFields(fields)
  if (!geometryField) return null

  return {
    geometry: geometryField.name || t('transfer.taskWizard.sourceItemUnknown'),
    geometryType: geometryField.geometry_type || geometryField.geometryType || '',
    srid: geometryField.srid
  }
}

function geometryFieldFromFields(fields) {
  return (fields || []).find(field => {
    const values = [
      field?.type,
      field?.geometry_type,
      field?.geometryType
    ].map(value => String(value || '').toLowerCase())
    return values.some(value => value.includes('geometry') || value.includes('geography'))
  }) || null
}

function numericValue(value) {
  if (value === null || value === undefined || value === '') return null
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : null
}

function firstPresent(...values) {
  return values.find(value => value !== null && value !== undefined && value !== '')
}

function formatInteger(value) {
  return new Intl.NumberFormat().format(Number(value))
}

function formatBytes(value) {
  const bytes = Number(value)
  if (!Number.isFinite(bytes)) return String(value)
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let size = bytes / 1024
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex += 1
  }
  return `${size.toFixed(size >= 10 ? 1 : 2)} ${units[unitIndex]}`
}

function formatDateTime(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return String(value)
  return date.toLocaleString()
}

function visibleSourceTreeNode(node) {
  return node?.type === 'engine' || node?.hasChildren || isSelectableSourceItem(node.metadata?.catalogNode)
}

function catalogRoleFromTreeNode(node) {
  return isItemTreeNode(node) ? 'leaf' : 'branch'
}

function isSelectableSourceItem(node) {
  if (node?.role !== 'leaf') return false
  return isSupportedSourceShape(nodeDataType(node), representationForSelection(node), nodeFormat(node))
}

function isSupportedSourceShape(dataType, representation, sourceFormat) {
  const normalizedDataType = String(dataType || '').toLowerCase()
  const normalizedRepresentation = String(representation || '').toLowerCase()
  const normalizedFormat = String(sourceFormat || '').toLowerCase()
  if (normalizedDataType === 'table') {
    return normalizedRepresentation === 'native' ||
      (normalizedRepresentation === 'encoded' && supportedEncodedSourceFormats.value.has(normalizedFormat))
  }
  return ['document', 'media', 'unknown'].includes(normalizedDataType) &&
    normalizedRepresentation === 'encoded' &&
    supportedRawCopyFormats.value.get(normalizedFormat) === normalizedDataType
}

function pathNames(node) {
  return (node?.path?.segments || [])
    .map(segment => String(segment.name || '').trim())
    .filter(name => name && name !== '.' && name !== '/')
}

function catalogPathForNode(node) {
  if (!node) return ''
  const endpointResource = buildPathOnlyEndpointResource(node)
  if (endpointResource.kind === 'native_table') {
    return [endpointResource.path.schema, endpointResource.path.table].filter(Boolean).join('.')
  }
  if (endpointResource.kind === 'object') {
    return [endpointResource.path.bucket, endpointResource.path.path].filter(Boolean).join('/')
  }
  return endpointResource.path.path || pathNames(node).join('/')
}

function buildPathOnlyEndpointResource(node) {
  return representationForSelection(node) === 'encoded'
    ? contentEndpointResourceFromNode(node)
    : nativeTableEndpointResourceFromNode(node)
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
    selectedNode.value = restoredNode
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
      role: savedItem.role || 'leaf',
      data_type: state.sourceDataType.value || savedItem.data_type || 'table',
      representation: state.sourceRepresentation.value || savedItem.representation || 'native',
      format: state.sourceFormat.value || savedItem.format || '',
      attributes: restoreSourceAttributes(state, savedItem.attributes)
    }
  }
  return null
}

function restoreSourceAttributes(state, savedAttributes = {}) {
  const attrs = { ...(savedAttributes || {}) }
  const item = {
    data_type: state.sourceDataType.value || 'table',
    representation: state.sourceRepresentation.value || 'native',
    format: state.sourceFormat.value || ''
  }
  delete attrs.data_type
  delete attrs.representation
  delete attrs.format
  return {
    ...attrs,
    item
  }
}

function treeNodeType(node) {
  const kind = String(node?.type || node?.kind || node?.term || '').toLowerCase()
  if (kind === 'namespace') return 'schema'
  if (kind === 'root') return 'directory'
  return kind || (node?.role === 'branch' ? 'directory' : 'object')
}

function catalogTreeNodeKey(node) {
  return node?.locator || node?.id || `catalog:${formData.engineID}:${displayPath(node?.path?.segments || [])}`
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
  if (catalogNode.role === 'leaf') {
    await selectNode(catalogNode)
    return
  }
  if (catalogNode.role === 'branch') {
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
  if (catalogNode?.role !== 'branch') return

  loadingNodes.value = true
  try {
    const metaID = target.metadata?.meta_id
    target.children = metaID ? (await getTransferNodeChildren(metaID)).map(normalizeTreeNode).filter(visibleSourceTreeNode) : []
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
    if (current.metadata?.catalogNode?.role === 'leaf') {
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

function isItemTreeNode(node) {
  return ['table', 'collection', 'label', 'relationship', 'object', 'file'].includes(treeNodeType(node)) && !node.hasChildren
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
  await loadCapabilities()
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

.selected-source-summary {
  box-sizing: border-box;
  width: 100%;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 16px;
  margin-top: 12px;
  padding: 9px 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
  min-height: 44px;
}

.summary-main {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}

.summary-title {
  flex: 0 0 auto;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.summary-path {
  min-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--addp-text-primary);
  font-size: 13px;
}

.summary-items {
  min-width: 0;
  display: flex;
  align-items: center;
  flex-wrap: nowrap;
  justify-content: flex-end;
  gap: 8px 14px;
  overflow: hidden;
}

.summary-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
}

.summary-label {
  flex: 0 0 auto;
  color: var(--addp-text-secondary);
  font-size: 12px;
  line-height: 20px;
}

.summary-value {
  max-width: 220px;
  color: var(--addp-text-primary);
  font-size: 13px;
  line-height: 20px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.summary-spatial {
  grid-column: 1 / -1;
  min-width: 0;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding-top: 4px;
  border-top: 1px solid var(--addp-border-color-light);
}

.summary-spatial-value {
  min-width: 0;
  color: var(--addp-text-primary);
  font-size: 13px;
  line-height: 20px;
  white-space: normal;
  word-break: break-word;
}

@media (max-width: 900px) {
  .selected-source-summary {
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
  }

  .summary-items {
    justify-content: flex-start;
    flex-wrap: wrap;
  }
}
</style>
