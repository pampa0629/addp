<template>
  <div class="step2-select-target">
    <h3>{{ t('transfer.taskWizard.selectTargetPage') }}</h3>
    <p class="step-description">{{ t('transfer.taskWizard.selectTargetPageDesc') }}</p>

    <el-alert
      v-if="!sourceTransferSupported"
      class="target-filter-alert"
      type="warning"
      :closable="false"
      :title="t('transfer.taskWizard.unsupportedSourceDataTypeTitle')"
      :description="t('transfer.taskWizard.unsupportedSourceShapeDesc', {
        dataType: dataTypeLabel(sourceDataType),
        representation: representationLabel(sourceRepresentation),
        format: formatLabel(sourceFormat)
      })"
    />

    <el-form :model="formData" label-width="120px">
      <el-form-item :label="t('transfer.taskWizard.targetEngineLabel')">
        <el-select
          v-model="formData.engineID"
          :placeholder="targetEnginePlaceholder"
          filterable
          :loading="loadingEngines"
          :disabled="!sourceTransferSupported"
          @change="handleTargetEngineChange"
        >
          <el-option
            v-for="engine in engines"
            :key="engine.id"
            :label="engineOptionLabel(engine)"
            :value="engine.id"
            :disabled="!isAllowedTargetEngine(engine)"
          />
        </el-select>
      </el-form-item>

      <el-form-item v-if="isNativeTableTarget" :label="t('transfer.taskWizard.schemaLabel')">
        <el-select
          v-model="targetSchema"
          :placeholder="t('transfer.taskWizard.schemaPlaceholder')"
          filterable
          :loading="loadingNamespaces"
          :disabled="!formData.engineID"
          @change="handleTargetSchemaChange"
        >
          <el-option
            v-for="schema in namespaces"
            :key="schema.name"
            :label="schema.name"
            :value="schema.name"
          />
        </el-select>
      </el-form-item>

      <el-form-item v-if="isNativeTableTarget" :label="t('transfer.taskWizard.targetTableLabel')">
        <el-select
          v-model="targetTable"
          :placeholder="t('transfer.taskWizard.targetTablePlaceholder')"
          filterable
          allow-create
          :loading="loadingTables"
          :disabled="!formData.engineID || !targetSchema"
          @change="syncTarget"
        >
          <el-option
            v-for="table in targetTables"
            :key="table.name"
            :label="table.name"
            :value="table.name"
          />
        </el-select>
      </el-form-item>

      <el-form-item v-if="isNativeTableTarget" :label="t('transfer.taskWizard.writeModeLabel')">
        <el-select v-model="tableWriteMode" @change="syncTarget">
          <el-option
            v-for="mode in tableWriteModeOptions"
            :key="mode"
            :label="writeModeLabel(mode)"
            :value="mode"
          >
            <div class="write-mode-option">
              <span>{{ writeModeLabel(mode) }}</span>
              <small>{{ writeModeDescription(mode) }}</small>
            </div>
          </el-option>
        </el-select>
        <div class="field-hint">{{ writeModeDescription(tableWriteMode) }}</div>
      </el-form-item>

      <el-form-item v-if="isContentTarget" :label="t('transfer.taskWizard.outputFormatLabel')">
        <el-select v-model="outputFormat" :placeholder="t('transfer.taskWizard.outputFormatLabel')" @change="handleOutputFormatChange">
          <el-option-group
            v-for="group in outputFormatGroups"
            :key="group.key"
            :label="t(group.labelKey)"
          >
            <el-option
              v-for="format in group.formats"
              :key="format.value"
              :label="format.labelKey ? t(format.labelKey) : format.label"
              :value="format.value"
            />
          </el-option-group>
        </el-select>
        <div v-if="selectedOutputFormat.hintKey" class="field-hint">{{ t(selectedOutputFormat.hintKey) }}</div>
      </el-form-item>

      <el-form-item v-if="isContentTarget" :label="t('transfer.taskWizard.outputPathLabel')">
        <el-input
          v-model="outputPath"
          :placeholder="t('transfer.taskWizard.storagePathPlaceholder')"
          readonly
          @click="showOutputPathPicker = true"
          @input="syncTarget"
        >
          <template #append>
            <el-button :disabled="!formData.engineID" @click="showOutputPathPicker = true">{{ t('transfer.taskWizard.browse') }}</el-button>
          </template>
        </el-input>
      </el-form-item>

      <el-form-item v-if="isContentTarget" :label="t('transfer.taskWizard.outputFileNameLabel')">
        <el-input
          v-model="outputFileName"
          :placeholder="outputFileNamePlaceholder"
          @input="syncTarget"
        />
      </el-form-item>

      <el-form-item v-if="isContentTarget && isDelimitedFormat" :label="t('transfer.taskWizard.delimitedOptionsLabel')">
        <div class="csv-options">
          <el-checkbox v-model="csvHeaders" @change="syncTarget">{{ t('transfer.taskWizard.csvHeadersLabel') }}</el-checkbox>
          <div v-if="outputFormat === 'csv'" class="delimiter-control">
            <span>{{ t('transfer.taskWizard.csvDelimiterLabel') }}：</span>
            <el-input v-model="csvDelimiter" placeholder="," @input="syncTarget" />
          </div>
        </div>
      </el-form-item>

      <el-alert
        v-if="isContentTarget && isSpatialFormat && spatialTargetHint"
        class="spatial-hint"
        type="info"
        :closable="false"
        :title="spatialTargetHint"
      />
    </el-form>

    <CatalogDirectoryPicker
      v-if="isContentTarget"
      v-model:visible="showOutputPathPicker"
      :engine-id="formData.engineID"
      :initial-path="outputPath"
      :storage-kind="targetStorageKind"
      @selected="handleOutputPathSelected"
    />
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { capabilitiesAPI } from '@/api/capabilities'
import { systemEnginesAPI } from '@/api/systemEngines'
import { getSchemas, getTables } from '@/api/meta'
import CatalogDirectoryPicker from '@/components/CatalogDirectoryPicker.vue'
import {
  dataTypeLabel,
  engineOptionLabel,
  formatLabel,
  hasStorageCapability,
  isContentEngine,
  isNativeTableEngine,
  representationLabel,
  writeModeDescription,
  writeModeLabel
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

const outputFormat = ref('csv')
const outputPath = ref('')
const outputFileName = ref('')
const csvHeaders = ref(true)
const csvDelimiter = ref(',')
const showOutputPathPicker = ref(false)
const targetSchema = ref('')
const targetTable = ref('')
const targetTables = ref([])
const namespaces = ref([])
const tableWriteMode = ref('overwrite')
const tableWriteModeOptions = ['overwrite', 'append']

const engines = ref([])
const loadingEngines = ref(false)
const loadingNamespaces = ref(false)
const loadingTables = ref(false)
const supportedEncodedSourceFormats = ref(new Set(['csv', 'tsv', 'json', 'jsonl', 'geojson', 'parquet', 'shapefile']))
const writableOutputFormats = ref(defaultOutputFormats())

const outputFormatGroups = computed(() => {
  const groups = [
    { key: 'table', labelKey: 'transfer.taskWizard.formatGroupTable', formats: [] },
    { key: 'spatial', labelKey: 'transfer.taskWizard.formatGroupSpatial', formats: [] }
  ]
  writableOutputFormats.value.forEach(format => {
    const group = format.spatial ? groups[1] : groups[0]
    group.formats.push(format)
  })
  return groups.filter(group => group.formats.length > 0)
})

const outputFormats = computed(() => outputFormatGroups.value.flatMap(group => group.formats))
const geometryTypes = ['Point', 'LineString', 'Polygon', 'MultiPoint', 'MultiLineString', 'MultiPolygon']

const selectedEngine = computed(() => {
  return engines.value.find(engine => engine.id === formData.engineID) || null
})

const sourceDataType = computed(() => props.wizardState.sourceDataType?.value || 'table')

const sourceRepresentation = computed(() => props.wizardState.sourceRepresentation?.value || 'native')

const sourceFormat = computed(() => props.wizardState.sourceFormat?.value || '')

const sourceTransferSupported = computed(() => {
  return sourceDataType.value === 'table' &&
    (
      sourceRepresentation.value === 'native' ||
      (sourceRepresentation.value === 'encoded' && supportedEncodedSourceFormat(sourceFormat.value))
    )
})

const targetEnginePlaceholder = computed(() => {
  if (!sourceTransferSupported.value) {
    return t('transfer.taskWizard.noSupportedTargetForSource')
  }
  return t('transfer.taskWizard.targetEnginePlaceholder')
})

const isNativeTableTarget = computed(() => {
  return sourceRepresentation.value === 'encoded' && isNativeTableEngine(selectedEngine.value)
})

const isContentTarget = computed(() => {
  return isContentEngine(selectedEngine.value)
})

const targetStorageKind = computed(() => {
  return isObjectStorageEngine(selectedEngine.value?.engine_type) ? 's3' : selectedEngine.value?.engine_type || ''
})

const canProceed = computed(() => {
  if (isNativeTableTarget.value) {
    return !!(formData.engineID && targetSchema.value.trim() && targetTable.value.trim())
  }
  if (isContentTarget.value) {
    const hasRequiredPath = targetStorageKind.value === 's3' ? !!outputPath.value.trim() : true
    return !!(formData.engineID && hasRequiredPath && outputFileName.value.trim())
  }
  return false
})

const selectedOutputFormat = computed(() => {
  return outputFormats.value.find(format => format.value === outputFormat.value) || outputFormats.value[0] || defaultOutputFormat('csv')
})

const outputFileNamePlaceholder = computed(() => {
  return `example.${selectedOutputFormat.value.extension}`
})

const isDelimitedFormat = computed(() => {
  return outputFormat.value === 'csv' || outputFormat.value === 'tsv'
})

const isSpatialFormat = computed(() => {
  return !!selectedOutputFormat.value.spatial
})

const primaryGeometryField = computed(() => {
  const fields = geometryFieldOptions.value
  return fields.length === 1 ? fields[0] : fields[0] || null
})

const primaryGeometryFieldName = computed(() => {
  return primaryGeometryField.value?.name || ''
})

const primaryGeometryType = computed(() => {
  return inferGeometryType(primaryGeometryField.value)
})

const spatialTargetHint = computed(() => {
  if (!isSpatialFormat.value) return ''
  if (!primaryGeometryFieldName.value) {
    return t('transfer.taskWizard.spatialFieldWillUseMapping')
  }
  if (outputFormat.value === 'shapefile' && !primaryGeometryType.value) {
    return t('transfer.taskWizard.spatialTypeWillUseMapping', { field: primaryGeometryFieldName.value })
  }
  return t('transfer.taskWizard.spatialAutoDetectedHint', {
    field: primaryGeometryFieldName.value,
    type: primaryGeometryType.value || t('transfer.taskWizard.geometryTypeUnknown')
  })
})

const geometryFieldOptions = computed(() => {
  const fields = props.wizardState.sourceFields?.value || []
  return fields.filter(isGeometryField)
})

watch(canProceed, (ready) => {
  if (ready) {
    syncTarget()
  }
})

function isObjectStorageEngine(engineType) {
  const type = (engineType || '').toLowerCase()
  return type.includes('s3') || type.includes('minio') || type.includes('oss') || type.includes('objectstore')
}

function syncTarget() {
  if (!canProceed.value || !selectedEngine.value) return

  const extra = isNativeTableTarget.value
    ? {
        schema: targetSchema.value,
        table: targetTable.value,
        writeMode: tableWriteMode.value
      }
    : {
        format: outputFormat.value,
        backendFormat: selectedOutputFormat.value.backendType,
        resourcePath: outputPath.value,
        resourceFile: outputFileName.value,
        includeHeader: csvHeaders.value,
        delimiter: outputFormat.value === 'tsv' ? '\t' : csvDelimiter.value,
        geometryField: primaryGeometryFieldName.value,
        geometryType: primaryGeometryType.value,
        backendOptions: selectedOutputFormat.value.options || {},
        writeMode: 'overwrite'
      }

  props.wizardState.updateTarget({
    engineID: formData.engineID,
    engineType: selectedEngine.value.engine_type,
    scope: 'system',
    targetType: targetStorageKind.value,
    schema: isNativeTableTarget.value ? targetSchema.value : '',
    table: isNativeTableTarget.value ? targetTable.value : '',
    representation: isNativeTableTarget.value ? 'native' : 'encoded',
    extra
  })
}

function handleOutputFormatChange() {
  applyOutputFileExtension()
  if (isSpatialFormat.value) {
    applyDefaultGeometryConfig()
  }
  syncTarget()
}

async function handleTargetEngineChange() {
  outputPath.value = ''
  outputFileName.value = ''
  targetSchema.value = ''
  targetTable.value = ''
  targetTables.value = []
  namespaces.value = []
  if (isNativeTableTarget.value) {
    await loadNamespaces()
  }
  syncTarget()
}

async function handleTargetSchemaChange() {
  targetTable.value = ''
  targetTables.value = []
  await loadTargetTables()
  syncTarget()
}

function handleOutputPathSelected(path) {
  outputPath.value = path
  syncTarget()
}

async function loadEngines() {
  loadingEngines.value = true
  try {
    const data = await systemEnginesAPI.list()
    engines.value = (data || []).filter(engine =>
      engine?.id !== undefined &&
      engine?.id !== null &&
      hasStorageCapability(engine)
    )
    if (formData.engineID && !engines.value.some(engine => engine.id === formData.engineID)) {
      formData.engineID = null
    }
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.loadTargetEngineFailedMsg'))
  } finally {
    loadingEngines.value = false
  }
}

function isAllowedTargetEngine(engine) {
  if (!sourceTransferSupported.value) return false
  if (sourceRepresentation.value === 'native') {
    return isContentEngine(engine)
  }
  if (sourceRepresentation.value === 'encoded') {
    return isNativeTableEngine(engine) || isContentEngine(engine)
  }
  return false
}

function supportedEncodedSourceFormat(format) {
  return supportedEncodedSourceFormats.value.has(String(format || '').toLowerCase())
}

async function loadCapabilities() {
  try {
    const data = await capabilitiesAPI.get()
    const formats = data?.table_formats || data?.tableFormats || []
    const readable = formats
      .filter(format => format?.read)
      .map(format => String(format.value || '').toLowerCase())
      .filter(Boolean)
    const writable = formats
      .filter(format => format?.write)
      .map(normalizeOutputFormatCapability)
      .filter(Boolean)
    if (readable.length > 0) {
      supportedEncodedSourceFormats.value = new Set(readable)
    }
    if (writable.length > 0) {
      writableOutputFormats.value = writable
    }
    if (!writableOutputFormats.value.some(format => format.value === outputFormat.value)) {
      outputFormat.value = writableOutputFormats.value[0]?.value || 'csv'
    }
  } catch (error) {
    writableOutputFormats.value = defaultOutputFormats()
    ElMessage.warning(t('transfer.taskWizard.loadCapabilitiesFailedMsg'))
  }
}

async function loadNamespaces() {
  if (!formData.engineID) return
  loadingNamespaces.value = true
  try {
    const response = await getSchemas(formData.engineID)
    const schemaList = Array.isArray(response?.data) ? response.data : (response || [])
    namespaces.value = schemaList.filter(schema => schema?.name)
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.loadSchemaFailedMsg'))
  } finally {
    loadingNamespaces.value = false
  }
}

async function loadTargetTables() {
  if (!formData.engineID || !targetSchema.value) return
  loadingTables.value = true
  try {
    const response = await getTables(formData.engineID, targetSchema.value)
    const tableList = Array.isArray(response?.data) ? response.data : (response || [])
    targetTables.value = tableList.map(item => ({ name: item.name || item })).filter(table => table.name)
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.loadTargetTableFailed'))
  } finally {
    loadingTables.value = false
  }
}

async function restoreState() {
  const state = props.wizardState

  const config = state.targetConfig.value || {}
  outputFormat.value = config.format || 'csv'
  outputPath.value = config.resourcePath || ''
  outputFileName.value = config.resourceFile || ''
  csvHeaders.value = config.includeHeader !== false
  csvDelimiter.value = config.delimiter || ','
  targetSchema.value = config.schema || state.targetSchema?.value || ''
  targetTable.value = config.table || state.targetTable?.value || ''
  tableWriteMode.value = normalizeTableWriteMode(config.writeMode)
  if (isSpatialFormat.value) {
    applyDefaultGeometryConfig()
  }

  if (state.targetEngineID.value) {
    formData.engineID = state.targetEngineID.value
    await nextTick()
    if (isNativeTableTarget.value) {
      await loadNamespaces()
      if (targetSchema.value) {
        await loadTargetTables()
      }
    }
    syncTarget()
  }
}

function applyOutputFileExtension() {
  const extension = selectedOutputFormat.value.extension
  const current = outputFileName.value.trim()
  if (!current) return

  const knownExtensions = outputFormats.value.map(format => format.extension)
  const parts = current.split('/')
  const file = parts.pop() || ''
  const dotIndex = file.lastIndexOf('.')
  const currentExtension = dotIndex > 0 ? file.slice(dotIndex + 1).toLowerCase() : ''
  const baseName = dotIndex > 0 && knownExtensions.includes(currentExtension)
    ? file.slice(0, dotIndex)
    : file
  parts.push(`${baseName}.${extension}`)
  outputFileName.value = parts.join('/')
}

function applyDefaultGeometryConfig() {
  syncTarget()
}

function isGeometryField(field) {
  if (!field) return false
  if (field.is_spatial === true || field.isSpatial === true || field.is_geometry === true) return true
  const values = [
    field.type,
    field.data_type,
    field.standard_type,
    field.field_type,
    field.geometry_type,
    field.geometryType,
    field.original_type,
    field.originalType
  ].map(value => String(value || '').toLowerCase())
  return values.some(value => value.includes('geometry') || geometryTypes.some(type => type.toLowerCase() === value))
}

function inferGeometryType(field) {
  if (!field) return ''
  const values = [
    field.geometry_type,
    field.geometryType,
    field.type,
    field.data_type,
    field.standard_type,
    field.field_type,
    field.original_type,
    field.originalType
  ]
  for (const value of values) {
    const normalized = normalizeGeometryType(value)
    if (normalized) return normalized
  }
  return ''
}

function normalizeGeometryType(value) {
  const normalized = String(value || '').toLowerCase().replace(/[_\s-]/g, '')
  return geometryTypes.find(type => type.toLowerCase().replace(/[_\s-]/g, '') === normalized) || ''
}

function normalizeTableWriteMode(value) {
  const mode = String(value || '').toLowerCase()
  if (mode === 'append') return 'append'
  return 'overwrite'
}

function normalizeOutputFormatCapability(item) {
  const value = String(item?.value || '').toLowerCase()
  if (!value) return null
  const fallback = defaultOutputFormat(value)
  return {
    ...fallback,
    value,
    extension: item.extension || fallback.extension,
    spatial: item.spatial === true || value === 'geojson' || value === 'shapefile',
    backendType: item.backend_type || item.backendType || value,
    options: item.options || {}
  }
}

function defaultOutputFormat(value) {
  const defaults = {
    csv: { labelKey: 'transfer.taskWizard.formatCsv', value: 'csv', extension: 'csv' },
    tsv: { labelKey: 'transfer.taskWizard.formatTsv', value: 'tsv', extension: 'tsv' },
    jsonl: { labelKey: 'transfer.taskWizard.formatJsonl', value: 'jsonl', extension: 'jsonl', hintKey: 'transfer.taskWizard.jsonlFormatHint' },
    json: { labelKey: 'transfer.taskWizard.formatJson', value: 'json', extension: 'json', hintKey: 'transfer.taskWizard.jsonFormatHint' },
    parquet: { labelKey: 'transfer.taskWizard.formatParquet', value: 'parquet', extension: 'parquet' },
    geojson: { labelKey: 'transfer.taskWizard.formatGeojson', value: 'geojson', extension: 'geojson', spatial: true, hintKey: 'transfer.taskWizard.geojsonFormatHint' },
    shapefile: { labelKey: 'transfer.taskWizard.formatShapefile', value: 'shapefile', extension: 'shp', spatial: true, hintKey: 'transfer.taskWizard.shapefileFormatHint' }
  }
  return defaults[value] || { label: value, value, extension: value }
}

function defaultOutputFormats() {
  return ['csv', 'tsv', 'jsonl', 'json', 'parquet', 'geojson', 'shapefile'].map(defaultOutputFormat)
}

onMounted(async () => {
  await loadCapabilities()
  await loadEngines()
  await restoreState()
})
</script>

<style scoped>
.step2-select-target {
  max-width: 800px;
  margin: 0 auto;
}

.step-description {
  color: var(--addp-text-secondary);
  margin-bottom: 30px;
}

.csv-options {
  display: flex;
  align-items: center;
  gap: 20px;
}

.delimiter-control {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--addp-text-secondary);
}

.delimiter-control .el-input {
  width: 80px;
}

.field-hint {
  margin-top: 6px;
  line-height: 1.5;
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.write-mode-option {
  display: flex;
  flex-direction: column;
  line-height: 1.4;
}

.write-mode-option small {
  color: var(--addp-text-tertiary);
}
</style>
