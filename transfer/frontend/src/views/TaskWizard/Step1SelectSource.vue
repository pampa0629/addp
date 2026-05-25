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
const supportedEncodedSourceFormats = ref(new Set(['csv', 'tsv', 'json', 'jsonl', 'geojson', 'parquet', 'shapefile']))

const selectedEngine = computed(() => {
  return engines.value.find(engine => engine.id === formData.engineID) || null
})

const selectedDataType = computed(() => nodeDataType(selectedNode.value))
const selectedFormat = computed(() => nodeFormat(selectedNode.value))
const selectedRepresentation = computed(() => representationForSelection(selectedNode.value))
const selectedSourceLabel = computed(() => catalogPathForNode(selectedNode.value))
const selectedSourceSummary = computed(() => buildSelectedSourceSummary(selectedNode.value))
const selectedSourceSupported = computed(() => {
  if (selectedDataType.value !== 'table') return false
  if (selectedRepresentation.value === 'native') return true
  if (selectedRepresentation.value === 'encoded') return supportedEncodedSourceFormats.value.has(selectedFormat.value)
  return false
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
    if (formats.length > 0) {
      supportedEncodedSourceFormats.value = new Set(formats)
    }
  } catch (error) {
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
  normalized.metadata.catalogNode = treeNodeToCatalogNode(normalized)
  normalized.metadata.selectable = isSelectableSourceItem(normalized.metadata.catalogNode)
  normalized.metadata.dataType = nodeDataType(normalized.metadata.catalogNode)
  normalized.metadata.format = nodeFormat(normalized.metadata.catalogNode)
  return normalized
}

function treeNodeToCatalogNode(node) {
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
    is_item: isItemTreeNode(node),
    is_container: !isItemTreeNode(node) && Boolean(node.hasChildren),
    path: {
      segments
    },
    attributes,
    meta_id: metadata.meta_id,
    full_name: metadata.full_name
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
    scope: 'system',
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
        name: node.name,
        kind: node.kind,
        term: node.term,
        path: node.path,
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
  return String(attrs.item?.[key] || node?.[key] || '').trim()
}

function buildSelectedSourceSummary(node) {
  if (!node) return null

  const attrs = node.attributes || {}
  const fields = tableFieldsFromAttributes(attrs)
  const loadedFields = props.wizardState.sourceFields?.value || []
  const fieldCount = firstPresent(
    numericValue(attrs.field_count),
    numericValue(attrs.type_info?.table?.field_count),
    numericValue(attrs.type_info?.table?.column_count),
    fields.length || null,
    loadedFields.length || null
  )
  const rowCount = firstPresent(
    numericValue(node.row_count),
    numericValue(attrs.type_info?.table?.row_count)
  )
  const size = firstPresent(
    numericValue(node.size),
    numericValue(attrs.storage?.total_size),
    numericValue(attrs.storage?.size_bytes)
  )
  const modified = firstPresent(
    attrs.storage?.last_modified_at,
    attrs.storage?.modified_at
  )
  const format = selectedFormat.value
  const spatial = spatialSummaryFromAttributes(attrs, fields.length > 0 ? fields : loadedFields)

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

function tableFieldsFromAttributes(attrs) {
  return Array.isArray(attrs?.type_info?.table?.fields) ? attrs.type_info.table.fields : []
}

function spatialSummaryFromAttributes(attrs, fields) {
  const spatial = attrs?.capabilities?.spatial || {}
  const geometryColumns = Array.isArray(spatial.geometry_columns)
    ? spatial.geometry_columns
    : Array.isArray(spatial.geometryColumns)
      ? spatial.geometryColumns
      : []
  const geometryField = geometryColumns[0] || geometryFieldFromFields(fields)
  if (!geometryField) return null

  return {
    geometry: geometryField.name || spatial.primary_geometry_column || spatial.primaryGeometryColumn || t('transfer.taskWizard.sourceItemUnknown'),
    geometryType: geometryField.geometry_type || geometryField.geometryType || spatial.geometry_type || spatial.geometryType || '',
    srid: firstPresent(geometryField.srid, spatial.srid)
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

function isSelectableSourceItem(node) {
  if (!node?.is_item) return false
  return Boolean(nodeDataType(node))
}

function inferDataTypeFromKind(node) {
  const kind = String(node?.kind || node?.term || '').toLowerCase()
  if (['table', 'relation', 'collection'].includes(kind)) return 'table'
  if (['file', 'object'].includes(kind) && node?.is_item && tableFormatFromName(node?.name)) return 'table'
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
      is_item: savedItem.is_item !== false,
      is_container: false,
      attributes: restoreSourceAttributes(state, savedItem.attributes)
    }
  }

  const endpointResource = state.sourceEndpointResource.value || config.resource
  if (!endpointResource?.kind) return null

  if (endpointResource.kind === 'native_table') {
    const schema = endpointResource.path?.schema || state.sourceSchema.value || ''
    const table = endpointResource.path?.table || endpointResource.path?.name || state.sourceTable.value || ''
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
      attributes: restoreSourceAttributes(state, config.attributes)
    }
  }

  if (endpointResource.kind === 'object') {
    const bucket = endpointResource.path?.bucket || ''
    const objectPath = endpointResource.path?.path || ''
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
      attributes: restoreSourceAttributes(state, config.attributes)
    }
  }

  if (endpointResource.kind === 'file') {
    const parts = String(endpointResource.path?.path || '').split('/').filter(Boolean)
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
      attributes: restoreSourceAttributes(state, config.attributes)
    }
  }

  return null
}

function restoreSourceAttributes(state, savedAttributes = {}) {
  const attrs = { ...(savedAttributes || {}) }
  const item = {
    ...(attrs.item || {}),
    data_type: state.sourceDataType.value || attrs.item?.data_type || attrs.data_type || 'table',
    representation: state.sourceRepresentation.value || attrs.item?.representation || attrs.representation || 'native',
    format: state.sourceFormat.value || attrs.item?.format || attrs.format || ''
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
  return kind || (node?.is_container ? 'directory' : 'object')
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
