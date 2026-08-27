<template>
  <div class="step1-select-source">
    <h3>{{ t('transfer.taskWizard.selectSourcePage') }}</h3>
    <p class="step-description">{{ t('transfer.taskWizard.selectSourcePageDesc') }}</p>

    <el-form :model="formData" label-width="120px">
      <el-form-item class="source-picker-form-item" :label="t('transfer.taskWizard.sourceItemLabel')">
        <ResourceTreePicker
          v-model="pickerSelection"
          api-base-url="/api/v1/meta"
          :initial-locator="initialSourceLocator"
          :engine-filter="hasStorageCapability"
          :selectable-filter="isSelectablePickerNode"
          mode="item"
          tree-height="360px"
          :engine-label="t('transfer.taskWizard.sourceEngineLabel')"
          :engine-placeholder="t('transfer.taskWizard.selectSourceEngine')"
          :search-placeholder="t('transfer.taskWizard.searchCurrentEngineSource')"
          :search-all-engines-placeholder="t('transfer.taskWizard.searchAllEnginesSource')"
          :search-empty-text="t('transfer.taskWizard.noSourceSearchResults')"
          :show-disabled-label="false"
          :search-selectable-only="true"
          :engine-multiple="true"
          :select-all-engines-by-default="true"
          :show-selection-summary="false"
          :show-count="false"
          @engine-change="handlePickerEngineChange"
          @select="handlePickerSelect"
        />
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

      <el-form-item
        v-if="isContainerSource"
        :label="t('transfer.taskWizard.containerChildLabel')"
      >
        <el-select
          v-model="containerChildName"
          :placeholder="t('transfer.taskWizard.containerChildPlaceholder')"
        >
          <el-option
            v-for="child in selectedContainerChildren"
            :key="child.name"
            :label="child.name"
            :value="child.name"
          />
        </el-select>
      </el-form-item>

      <el-form-item
        v-if="querySourceAvailable"
        :label="t('transfer.taskWizard.querySourceLabel')"
      >
        <div class="query-source-config">
          <el-switch
            v-model="querySourceEnabled"
            :active-text="t('transfer.taskWizard.querySourceEnabled')"
            @change="syncQuerySource"
          />
          <el-alert
            v-if="querySourceEnabled"
            type="info"
            :closable="false"
            :title="t('transfer.taskWizard.querySourceHint')"
          />
          <template v-if="querySourceEnabled">
            <el-select v-model="queryLanguage" @change="syncQuerySource">
              <el-option label="MQL" value="mql" />
              <el-option label="SQL" value="sql" />
            </el-select>
            <el-input
              v-model="queryStatement"
              type="textarea"
              :rows="10"
              :placeholder="t('transfer.taskWizard.queryStatementPlaceholder')"
              @input="syncQuerySource"
            />
            <div v-if="queryStatementError" class="query-error">{{ queryStatementError }}</div>
            <el-input
              v-model="queryParametersText"
              type="textarea"
              :rows="3"
              :placeholder="t('transfer.taskWizard.queryParametersPlaceholder')"
              @input="syncQuerySource"
            />
            <div v-if="queryParametersError" class="query-error">{{ queryParametersError }}</div>
          </template>
        </div>
      </el-form-item>

    </el-form>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ResourceTreePicker, buildLocator as buildResourceLocator, parseLocatorSafe } from '@addp/common-frontend'
import { parseTransferLocator } from '@/utils/resourceLocator'
import { capabilitiesAPI } from '@/api/capabilities'
import { getItemFieldsByID } from '@/api/meta'
import {
  dataTypeLabel,
  formatLabel,
  hasStorageCapability,
  isContentEngine,
  representationLabel
} from '@/utils/transferDisplay'
import {
  containerTableChildren,
  isTransferableTableContainer,
  resolveContainerTableChild
} from './containerSource.mjs'
import { queryStatementValid } from './runtimeTarget.mjs'

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

const selectedNode = ref(null)
const pickerSelection = ref(null)
const containerChildName = ref('')
const querySourceEnabled = ref(props.wizardState.sourceQueryEnabled.value)
const queryLanguage = ref(props.wizardState.sourceQueryLanguage.value || 'mql')
const queryStatement = ref(props.wizardState.sourceQueryStatement.value || '')
const queryParametersText = ref(JSON.stringify(props.wizardState.sourceQueryParameters.value || {}, null, 2))
const queryStatementError = ref('')
const queryParametersError = ref('')

const supportedEncodedSourceFormats = ref(new Set())
const supportedRawCopyFormats = ref(new Map())

const selectedEngine = computed(() => {
  return pickerSelection.value?.raw?.engine || engineFromSelectedNode(selectedNode.value)
})

const selectedDataType = computed(() => nodeDataType(selectedNode.value))
const selectedFormat = computed(() => nodeFormat(selectedNode.value))
const selectedSourceLabel = computed(() => catalogPathForNode(selectedNode.value))
const selectedSourceSummary = computed(() => buildSelectedSourceSummary(selectedNode.value))
const initialSourceLocator = computed(() => props.wizardState.sourceLocator.value || '')
const selectedContainerChildren = computed(() => containerTableChildren(selectedNode.value?.attributes || {}))
const isContainerSource = computed(() => isContainerSourceNode(selectedNode.value, selectedEngine.value))
const selectedContainerChild = computed(() => {
  if (!isContainerSource.value) return null
  return resolveContainerTableChild(selectedNode.value?.attributes || {}, containerChildName.value)
})

const selectedTransferDataType = computed(() => {
  return selectedContainerChild.value ? 'table' : selectedDataType.value
})
const querySourceAvailable = computed(() => {
  return !!selectedNode.value &&
    props.wizardState.runtimeBoundary.value === 'bounded' &&
    selectedTransferDataType.value === 'table' &&
    representationForSelection(selectedNode.value, selectedEngine.value) === 'native'
})

watch(
  () => props.wizardState.sourceQueryStatement.value,
  (statement) => {
    if (statement === queryStatement.value) return
    querySourceEnabled.value = props.wizardState.sourceQueryEnabled.value
    queryLanguage.value = props.wizardState.sourceQueryLanguage.value || 'mql'
    queryStatement.value = statement || ''
    queryParametersText.value = JSON.stringify(props.wizardState.sourceQueryParameters.value || {}, null, 2)
  },
  { flush: 'post' }
)

watch(selectedNode, async (node) => {
  if (!node) {
    containerChildName.value = ''
    return
  }
  if (isContainerSourceNode(node, selectedEngine.value)) {
    const previousChild = props.wizardState.sourceConfig.value?.options?.child_name || ''
    containerChildName.value = resolveContainerTableChild(node.attributes || {}, previousChild)?.name || ''
    syncSource(node)
    props.wizardState.loadSourceFields([])
    return
  }
  containerChildName.value = ''
  syncSource(node)
  await loadFieldsForNode(node)
})

function syncQuerySource() {
  let parameters = {}
  queryStatementError.value = ''
  queryParametersError.value = ''
  if (querySourceEnabled.value && !queryStatementValid(queryLanguage.value, queryStatement.value)) {
    queryStatementError.value = queryLanguage.value === 'mql'
      ? t('transfer.taskWizard.queryMqlCommandObjectRequired')
      : t('transfer.taskWizard.queryStatementRequired')
  }
  try {
    parameters = JSON.parse(queryParametersText.value.trim() || '{}')
    if (!parameters || Array.isArray(parameters) || typeof parameters !== 'object') {
      throw new Error(t('transfer.taskWizard.queryParametersObjectRequired'))
    }
  } catch (error) {
    queryParametersError.value = error.message || t('transfer.taskWizard.queryParametersInvalid')
    parameters = {}
  }
  props.wizardState.updateSourceQuery({
    enabled: querySourceEnabled.value,
    language: queryLanguage.value,
    statement: queryStatement.value,
    parameters,
    valid: !queryStatementError.value && !queryParametersError.value
  })
}

watch(containerChildName, () => {
  if (!selectedNode.value || !isContainerSource.value) return
  syncSource(selectedNode.value)
  props.wizardState.loadSourceFields([])
})

watch(
  () => [
    props.wizardState.sourceEngineID.value,
    props.wizardState.sourceLocator.value
  ],
  ([engineID, locator]) => {
    if (!engineID || !locator) return
    if (formData.engineID === engineID && selectedNode.value) return
    formData.engineID = engineID
  },
  { flush: 'post' }
)

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
		rawCopyFormats: nextRawCopyFormats,
		databaseCDC: data?.continuous?.database_cdc || data?.continuous?.databaseCDC || null
    })
  } catch (error) {
    supportedEncodedSourceFormats.value = new Set()
    supportedRawCopyFormats.value = new Map()
    props.wizardState.updateFormatCapabilities()
    ElMessage.warning(t('transfer.taskWizard.loadCapabilitiesFailedMsg'))
  }
}

function handlePickerEngineChange(engine) {
  const nextEngines = Array.isArray(engine) ? engine : [engine].filter(Boolean)
  const currentEngineID = Number(
    pickerSelection.value?.identity?.engine_id ||
    parseTransferLocator(selectedNode.value?.locator || '').engineID ||
    0
  )
  if (Array.isArray(engine) && currentEngineID && nextEngines.some(item => Number(item?.id) === currentEngineID)) {
    formData.engineID = currentEngineID
    return
  }
  const nextEngine = Array.isArray(engine) ? null : engine
  formData.engineID = nextEngine?.id || null
  selectedNode.value = null
  pickerSelection.value = null
  props.wizardState.loadSourceFields([])
}

async function handlePickerSelect(selection) {
  pickerSelection.value = selection
  formData.engineID = selection?.identity?.engine_id || null
  const catalogNode = treeNodeToCatalogEntry(selection?.raw?.node || {}, selection)
  if (!catalogNode.locator) return
  await selectNode(catalogNode)
}

function isSelectablePickerNode(node, context = {}) {
  const engine = context.engine || selectedEngine.value
  return isSelectableSourceItem(treeNodeToCatalogEntry(node, engine), engine)
}

function treeNodeToCatalogEntry(node, selectionOrEngine = selectedEngine.value) {
  const selection = selectionOrEngine && typeof selectionOrEngine === 'object' && selectionOrEngine.identity
    ? selectionOrEngine
    : null
  const engine = selection?.raw?.engine || selectionOrEngine || selectedEngine.value
  const metadata = node.metadata || {}
  const itemID = resolveNodeItemID(node, selection)
  const attributes = {
    ...(metadata.attributes || {}),
    ...standardAttributeSections(metadata)
  }
  const segments = pathSegmentsFromTreeNode(node, engine)
  return {
    name: node.label,
    kind: node.type,
    term: node.type,
    role: catalogRoleFromTreeNode(node),
    path: {
      segments
    },
    attributes,
    full_name: metadata.full_name,
    data_type: metadata.data_type,
    representation: metadata.representation || representationForSelection(node, engine),
    format: metadata.format,
    layout: metadata.layout,
    physical_path: metadata.physical_path,
    item_id: itemID > 0 ? itemID : undefined,
    meta_id: itemID > 0 ? itemID : metadata.meta_id,
    locator: node.locator || node.id,
    size_bytes: metadata.size_bytes,
    last_modified_at: metadata.last_modified_at,
    row_count: metadata.row_count,
    field_count: metadata.field_count,
    spatial: metadata.spatial
  }
}

function resolveNodeItemID(node, selection = null) {
  return Number(
    node?.item_id ||
    node?.meta_id ||
    selection?.identity?.item_id ||
    parseTransferLocator(node?.locator || node?.id || '').itemID ||
    0
  )
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

function pathSegmentsFromTreeNode(node, engine = selectedEngine.value) {
  const locatorPath = parseLocatorPath(node.locator || node.id || '')
  if (locatorPath.length > 0) {
    return locatorPath.map((name, index) => ({
      name,
      kind: index === locatorPath.length - 1 ? treeNodeType(node) : containerKindForPath(index, engine),
      term: index === locatorPath.length - 1 ? treeNodeType(node) : containerKindForPath(index, engine)
    }))
  }
  return String(node.metadata?.full_name || node.label || '')
    .split(pathSeparatorForNode(node, engine))
    .map(part => part.trim())
    .filter(Boolean)
    .map((name, index, parts) => ({
      name,
      kind: index === parts.length - 1 ? treeNodeType(node) : containerKindForPath(index, engine),
      term: index === parts.length - 1 ? treeNodeType(node) : containerKindForPath(index, engine)
    }))
}

function parseLocatorPath(locator) {
  return parseTransferLocator(locator).path
}

function pathSeparatorForNode(node, engine = selectedEngine.value) {
  return isContentEngine(engine) || ['object', 'file', 'directory', 'dir', 'prefix', 'bucket'].includes(treeNodeType(node)) ? '/' : '.'
}

function containerKindForPath(index, engine = selectedEngine.value) {
  if (isObjectStorageEngine(engine?.engine_type) && index === 0) return 'bucket'
  return isContentEngine(engine) ? 'directory' : 'schema'
}

async function selectNode(node) {
  selectedNode.value = node
}

async function loadFieldsForNode(node) {
  if (isContainerSourceNode(node, selectedEngine.value)) {
    props.wizardState.loadSourceFields([])
    return
  }
  if (treeNodeType(node) === 'topic') {
    props.wizardState.loadSourceFields([])
    return
  }
  if (nodeDataType(node) !== 'table') {
    props.wizardState.loadSourceFields([])
    return
  }

  try {
    const itemID = resolveNodeItemID(node)
    if (!itemID) {
      props.wizardState.loadSourceFields([])
      return
    }
    const response = await getItemFieldsByID(itemID)
    const fieldList = Array.isArray(response?.data) ? response.data : (response || [])
    props.wizardState.loadSourceFields(fieldList, node.attributes || {})
  } catch (error) {
    props.wizardState.loadSourceFields([])
    ElMessage.warning(t('transfer.taskWizard.loadSourceFieldsWarning', { error: error.response?.data?.error || error.message }))
  }
}

function syncSource(node) {
  const engine = selectedEngine.value
  if (!engine || !node) return

  const containerChild = selectedContainerChild.value
  const endpointResource = buildSourceEndpointResource(node)
  const locator = sourceLocatorForNode(node)
  const dataType = treeNodeType(node) === 'topic'
    ? 'unknown'
    : (containerChild ? 'table' : nodeDataType(node))
  props.wizardState.updateSource({
    engineID: formData.engineID,
    engineType: engine.engine_type,
    capabilities: engine.capabilities,
    schema: endpointResource.path?.schema || '',
    table: endpointResource.path?.table || '',
    sourceType: normalizeEngineType(engine.engine_type),
    dataType,
    representation: representationForSelection(node),
    format: nodeFormat(node),
    locator,
    extra: {
      sourceLabel: catalogPathForNode(node),
      catalogPath: catalogPathForNode(node),
      dataType,
      representation: representationForSelection(node),
      format: nodeFormat(node),
      locator,
      options: containerChild ? { child_name: containerChild.name } : {},
      sourceItem: {
        item_id: resolveNodeItemID(node) || undefined,
        meta_id: resolveNodeItemID(node) || undefined,
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
        locator,
        attributes: node.attributes || {}
      }
    }
  })
}

function sourceLocatorForNode(node) {
  const existing = String(node?.locator || node?.id || '').trim()
  if (existing.startsWith('addp://')) {
    return ensureLocatorItemID(existing, resolveNodeItemID(node))
  }
  const cleanEngineID = Number(formData.engineID)
  const cleanType = String(locatorTypeForNode(node) || '').trim()
  const path = pathNames(node)
  const cleanItemID = resolveNodeItemID(node)
  if (!cleanEngineID || !cleanType || path.length === 0) return ''
  return buildResourceLocator({
    engineId: cleanEngineID,
    type: cleanType,
    path,
    itemId: cleanItemID > 0 ? cleanItemID : undefined
  })
}

function locatorTypeForNode(node) {
  if (treeNodeType(node) === 'topic') return 'topic'
  if (representationForSelection(node) === 'native') return 'table'
  return isObjectStorageEngine(selectedEngine.value?.engine_type) ? 'object' : 'file'
}

function ensureLocatorItemID(locator, itemID = 0) {
  const cleanItemID = Number(itemID || 0)
  if (!cleanItemID) return locator
  const loc = parseLocatorSafe(locator)
  if (loc.itemId) return locator
  if (!loc.engineId || !loc.type || !loc.path?.length) {
    return locator
  }
  return buildResourceLocator({
    engineId: loc.engineId,
    type: loc.type,
    path: loc.path || [],
    itemId: cleanItemID
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

function representationForSelection(node, engine = selectedEngine.value) {
  if (!node) return ''
  if (nodeAttribute(node, 'representation')) return nodeAttribute(node, 'representation')
  if (nodeItemAttribute(node, 'representation')) return nodeItemAttribute(node, 'representation')
  return isContentEngine(engine) ? 'encoded' : 'native'
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

  const containerChild = selectedContainerChild.value
  const loadedFields = props.wizardState.sourceFields?.value || []
  const fieldCount = firstPresent(
    numericValue(containerChild?.column_count),
    numericValue(node.field_count),
    loadedFields.length || null
  )
  const rowCount = firstPresent(
    numericValue(containerChild?.row_count),
    numericValue(node.row_count)
  )
  const size = firstPresent(
    numericValue(node.size_bytes)
  )
  const modified = firstPresent(
    node.last_modified_at
  )
  const format = selectedFormat.value
  const spatial = spatialSummaryFromContainerChild(containerChild) || node.spatial || spatialSummaryFromFields(loadedFields)

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
    dataType: treeNodeType(node) === 'topic'
      ? t('transfer.taskWizard.kafkaTopicLabel')
      : dataTypeLabel(selectedTransferDataType.value),
    format: format ? formatLabel(format) : '',
    spatial,
    spatialInfo: spatial
      ? [spatial.geometry, spatial.geometryType, spatial.srid ? `SRID ${spatial.srid}` : ''].filter(Boolean).join(' · ')
      : '',
    items
  }
}

function spatialSummaryFromContainerChild(child) {
  const native = child?.native || {}
  const geometry = String(native.geometry_column || '').trim()
  if (!geometry) return null
  return {
    geometry,
    geometryType: native.geometry_type || '',
    srid: native.srid
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

function catalogRoleFromTreeNode(node) {
  return isItemTreeNode(node) ? 'leaf' : 'branch'
}

function isSelectableSourceItem(node, engine = selectedEngine.value) {
  if (node?.role !== 'leaf') return false
  if (isContainerSourceNode(node, engine)) return true
  if (treeNodeType(node) === 'topic') {
    return normalizeEngineType(engine?.engine_type) === 'kafka'
  }
  return isSupportedSourceShape(nodeDataType(node), representationForSelection(node, engine), nodeFormat(node))
}

function isContainerSourceNode(node, engine = selectedEngine.value) {
  return isTransferableTableContainer({
    dataType: nodeDataType(node),
    representation: representationForSelection(node, engine),
    format: nodeFormat(node),
    attributes: node?.attributes || {},
    readableFormats: supportedEncodedSourceFormats.value
  })
}

function isSupportedSourceShape(dataType, representation, sourceFormat) {
  const normalizedDataType = String(dataType || '').toLowerCase()
  const normalizedRepresentation = String(representation || '').toLowerCase()
  const normalizedFormat = String(sourceFormat || '').toLowerCase()
  if (normalizedDataType === 'table') {
    return normalizedRepresentation === 'native' ||
      (normalizedRepresentation === 'encoded' && supportedEncodedSourceFormats.value.has(normalizedFormat))
  }
  return ['document', 'media', 'cad', 'unknown'].includes(normalizedDataType) &&
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

function isObjectStorageEngine(engineType) {
  const type = String(engineType || '').toLowerCase()
  return type.includes('s3') || type.includes('minio') || type.includes('oss')
}

function normalizeEngineType(engineType) {
  const type = String(engineType || '').toLowerCase()
  if (type.includes('postgres')) return 'postgresql'
  return type
}

function engineFromSelectedNode(node) {
  const engineID = Number(formData.engineID || parseTransferLocator(node?.locator || '').engineID || 0)
  if (!engineID) return null
  return {
    id: engineID,
    engine_type: props.wizardState.sourceEngineType.value || ''
  }
}

function treeNodeType(node) {
  const kind = String(node?.type || node?.kind || node?.term || '').toLowerCase()
  if (kind === 'namespace') return 'schema'
  if (kind === 'root') return 'directory'
  return kind || (node?.role === 'branch' ? 'directory' : 'object')
}

function isItemTreeNode(node) {
  return ['table', 'collection', 'label', 'relationship', 'object', 'file', 'topic'].includes(treeNodeType(node)) && !node.hasChildren
}

onMounted(async () => {
  await loadCapabilities()
  if (props.wizardState.sourceEngineID.value) {
    formData.engineID = props.wizardState.sourceEngineID.value
  }
})
</script>

<style scoped>
.step1-select-source {
  max-width: 1120px;
  margin: 0 auto;
}

.step-description {
  color: var(--addp-text-secondary);
  margin-bottom: 30px;
}

.query-source-config {
  width: min(960px, 100%);
  display: grid;
  gap: 12px;
}

.query-error {
  color: var(--addp-color-danger);
  font-size: 12px;
}

.source-resource-tree {
  width: 100%;
}

.source-picker-form-item :deep(.el-form-item__content) {
  width: min(960px, 100%);
  max-width: 100%;
}

.source-picker-form-item :deep(.resource-tree-picker) {
  width: 100%;
  min-width: 0;
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
