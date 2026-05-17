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
        <div class="catalog-browser">
          <el-breadcrumb separator="/" class="catalog-breadcrumb">
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

          <div class="current-path">
            {{ t('transfer.taskWizard.sourceCatalogCurrentPath', { path: displayPath(currentSegments) }) }}
          </div>

          <el-skeleton v-if="loadingNodes" :rows="5" animated />

          <el-empty
            v-else-if="catalogNodes.length === 0"
            :description="t('transfer.taskWizard.sourceNoCatalogItems')"
          />

          <el-table
            v-else
            :data="catalogNodes"
            border
            row-key="nodeKey"
            class="catalog-table"
            @row-dblclick="handleRowDoubleClick"
          >
            <el-table-column :label="t('transfer.taskWizard.catalogNameColumn')" min-width="260">
              <template #default="{ row }">
                <span class="catalog-name">
                  <el-icon :size="16">
                    <component :is="nodeIcon(row)" />
                  </el-icon>
                  <span>{{ row.name }}</span>
                </span>
              </template>
            </el-table-column>
            <el-table-column :label="t('transfer.taskWizard.catalogKindColumn')" width="120">
              <template #default="{ row }">
                <el-tag size="small" :type="row.is_item ? 'success' : 'info'">
                  {{ nodeKindLabel(row) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('transfer.taskWizard.catalogDataTypeColumn')" width="130">
              <template #default="{ row }">
                {{ dataTypeLabel(nodeDataType(row)) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('transfer.taskWizard.catalogFormatColumn')" width="120">
              <template #default="{ row }">
                {{ formatLabel(nodeFormat(row)) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('transfer.taskWizard.catalogActionColumn')" width="180" align="right">
              <template #default="{ row }">
                <el-button
                  v-if="row.is_container"
                  type="primary"
                  link
                  size="small"
                  @click.stop="enterNode(row)"
                >
                  {{ t('transfer.taskWizard.openCatalogNode') }}
                </el-button>
                <el-button
                  v-if="row.is_item"
                  link
                  size="small"
                  @click.stop="selectNode(row)"
                >
                  {{ t('transfer.taskWizard.selectCatalogItem') }}
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-form-item>

      <el-form-item v-if="selectedNode" :label="t('transfer.taskWizard.tableInfoLabel')">
        <div class="source-info">
          <p><strong>{{ t('transfer.taskWizard.selectedSourceItemLabel') }}：</strong>{{ selectedSourceLabel }}</p>
          <p><strong>{{ t('transfer.taskWizard.dataType') }}：</strong>{{ dataTypeLabel(selectedDataType) }}</p>
          <p><strong>{{ t('transfer.taskWizard.representation') }}：</strong>{{ representationLabel(selectedRepresentation) }}</p>
          <p v-if="selectedFormat"><strong>{{ t('transfer.taskWizard.format') }}：</strong>{{ formatLabel(selectedFormat) }}</p>
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
import { Document, Folder } from '@element-plus/icons-vue'
import { getItemFieldsByCatalogPath, getTableFields, listCatalogChildren } from '@/api/meta'
import { systemEnginesAPI } from '@/api/systemEngines'
import {
  catalogKindLabel,
  dataTypeLabel,
  engineOptionLabel,
  formatLabel,
  hasStorageCapability,
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
const currentSegments = ref([])
const catalogNodes = ref([])
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
  currentSegments.value = []
  catalogNodes.value = []
  selectedNode.value = null
  props.wizardState.loadSourceFields([])
  if (formData.engineID) {
    await openPath([])
  }
}

async function openPath(segments) {
  currentSegments.value = Array.isArray(segments) ? segments : []
  loadingNodes.value = true
  try {
    const nodes = await listCatalogChildren(formData.engineID, { segments: currentSegments.value })
    catalogNodes.value = nodes.map(normalizeNode)
  } catch (error) {
    catalogNodes.value = []
    ElMessage.error(t('transfer.taskWizard.loadCatalogFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    loadingNodes.value = false
  }
}

function normalizeNode(node) {
  const segments = Array.isArray(node.path?.segments) && node.path.segments.length > 0
    ? node.path.segments
    : [...currentSegments.value, segmentForNode(node)]
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

function enterNode(node) {
  if (!node?.path?.segments) return
  openPath(node.path.segments)
}

function handleRowDoubleClick(row) {
  if (row?.is_container) {
    enterNode(row)
  } else if (row?.is_item) {
    selectNode(row)
  }
}

async function selectNode(node) {
  selectedNode.value = node
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

function nodeIcon(node) {
  return node?.is_container ? Folder : Document
}

function nodeKindLabel(node) {
  return catalogKindLabel(node?.kind || node?.term)
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
  const savedItem = state.sourceConfig.value?.sourceItem
  if (savedItem) {
    selectedNode.value = normalizeNode(savedItem)
  }
  await openPath([])
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

.catalog-browser {
  width: 100%;
}

.catalog-breadcrumb {
  margin-bottom: 8px;
}

.current-path {
  margin-bottom: 12px;
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.catalog-table {
  width: 100%;
}

.catalog-name {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.source-info {
  padding: 12px;
  background: var(--addp-bg-secondary);
  border-radius: 4px;
}

.source-info p {
  margin: 8px 0;
  color: var(--addp-text-secondary);
}
</style>
