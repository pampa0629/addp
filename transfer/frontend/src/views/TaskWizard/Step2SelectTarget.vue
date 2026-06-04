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
        <el-select
          v-model="outputFormat"
          :placeholder="t('transfer.taskWizard.outputFormatLabel')"
          :disabled="isRawCopySource"
          @change="handleOutputFormatChange"
        >
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
        <div v-else-if="isRawCopySource" class="field-hint">{{ t('transfer.taskWizard.rawCopyFormatHint') }}</div>
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
          :class="{ 'is-extension-error': !!outputFileNameError }"
          @input="syncTarget"
          @blur="applyOutputFileExtension"
        />
        <div v-if="outputFileNameError" class="field-hint error-hint">{{ outputFileNameError }}</div>
        <div v-else-if="finalOutputPath" class="field-hint">
          {{ t('transfer.taskWizard.finalOutputPathLabel') }}: {{ finalOutputPath }}
        </div>
        <div v-if="selectedOutputExtension && !isRawCopySource" class="field-hint">
          {{ t('transfer.taskWizard.outputFileExtensionAutoHint', { extension: `.${selectedOutputExtension}` }) }}
        </div>
        <div v-if="isRawCopySource" class="field-hint">
          {{ t('transfer.taskWizard.rawCopyTargetPathHint') }}
        </div>
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

    <ObjectStoragePathPicker
      v-if="isObjectStorageTarget"
      v-model:visible="showOutputPathPicker"
      :resource-id="formData.engineID"
      :initial-prefix="objectStorageInitialPrefix"
      @selected="handleOutputPathSelected"
    />

    <CatalogDirectoryPicker
      v-else-if="isContentTarget"
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
import ObjectStoragePathPicker from '@/components/ObjectStoragePathPicker.vue'
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

const outputFormat = ref('')
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
const restoringState = ref(false)
const supportedEncodedSourceFormats = computed(() => props.wizardState.readableEncodedFormats?.value || new Set())
const supportedRawCopyFormats = computed(() => props.wizardState.rawCopyFormats?.value || new Map())
const writableOutputFormats = ref([])

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
  if (sourceDataType.value === 'table') {
    return sourceRepresentation.value === 'native' ||
      (sourceRepresentation.value === 'encoded' && supportedEncodedSourceFormat(sourceFormat.value))
  }
  return isRawCopySource.value
})

const isRawCopySource = computed(() => {
  return ['document', 'media', 'unknown'].includes(sourceDataType.value) &&
    sourceRepresentation.value === 'encoded' &&
    supportedRawCopyFormats.value.get(String(sourceFormat.value || '').toLowerCase()) === sourceDataType.value
})

const targetEnginePlaceholder = computed(() => {
  if (!sourceTransferSupported.value) {
    return t('transfer.taskWizard.noSupportedTargetForSource')
  }
  return t('transfer.taskWizard.targetEnginePlaceholder')
})

const isNativeTableTarget = computed(() => {
  if (isRawCopySource.value) return false
  return isNativeTableEngine(selectedEngine.value)
})

const isContentTarget = computed(() => {
  return isContentEngine(selectedEngine.value)
})

const targetStorageKind = computed(() => {
  return isObjectStorageEngine(selectedEngine.value?.engine_type) ? 's3' : selectedEngine.value?.engine_type || ''
})

const isObjectStorageTarget = computed(() => {
  return isContentTarget.value && targetStorageKind.value === 's3'
})

const objectStorageInitialPrefix = computed(() => {
  if (!isObjectStorageTarget.value) return ''
  return normalizeObjectStoragePickerPrefix(outputPath.value, objectStorageConfiguredBucket.value)
})

const objectStorageConfiguredBucket = computed(() => {
  return String(selectedEngine.value?.connection_info?.bucket || selectedEngine.value?.connectionInfo?.bucket || '').trim()
})

const canProceed = computed(() => {
  if (isNativeTableTarget.value) {
    return !!(formData.engineID && targetSchema.value.trim() && targetTable.value.trim())
  }
  if (isContentTarget.value) {
    const hasRequiredPath = targetStorageKind.value === 's3' ? !!outputPath.value.trim() : true
    const hasOutputFormat = isRawCopySource.value || !!selectedOutputFormat.value.value
    return !!(formData.engineID && hasRequiredPath && hasOutputFormat && outputFileName.value.trim() && !outputFileNameError.value)
  }
  return false
})

const selectedOutputFormat = computed(() => {
  if (isRawCopySource.value) {
    return rawCopyOutputFormat()
  }
  return outputFormats.value.find(format => format.value === outputFormat.value) || outputFormats.value[0] || emptyOutputFormat()
})

const outputFileNamePlaceholder = computed(() => {
  if (isRawCopySource.value) {
    return sourceFileName.value || `copy.${selectedOutputFormat.value.extension || sourceFormat.value || 'bin'}`
  }
  const extension = selectedOutputFormat.value.extension
  return extension ? `example.${extension}` : 'example'
})

const selectedOutputExtension = computed(() => normalizeExtension(selectedOutputFormat.value.extension))

const normalizedOutputFileName = computed(() => outputFileNameWithExtension(outputFileName.value))

const outputFileNameError = computed(() => {
  if (isRawCopySource.value) return ''
  const extension = selectedOutputExtension.value
  const current = outputFileName.value.trim()
  if (!extension || !current) return ''
  const currentExtension = currentFileExtension(current)
  if (!currentExtension || currentExtension === extension) return ''
  return t('transfer.taskWizard.outputFileExtensionConflict', {
    current: `.${currentExtension}`,
    expected: `.${extension}`
  })
})

const finalOutputPath = computed(() => {
  const fileName = normalizedOutputFileName.value
  if (!fileName || outputFileNameError.value) return ''
  return [normalizeFileCatalogPath(outputPath.value), normalizeFileCatalogPath(fileName)].filter(Boolean).join('/')
})

const isDelimitedFormat = computed(() => {
  if (isRawCopySource.value) return false
  return outputFormat.value === 'csv' || outputFormat.value === 'tsv'
})

const isSpatialFormat = computed(() => {
  if (isRawCopySource.value) return false
  return !!selectedOutputFormat.value.spatial
})

const sourceFileName = computed(() => {
  const sourceConfig = props.wizardState.sourceConfig?.value || {}
  const loc = parseLocator(sourceConfig.locator || props.wizardState.sourceLocator?.value || '')
  const fullPath = loc.path.join('/') || sourceConfig.sourceLabel || ''
  return normalizeFileCatalogPath(fullPath).split('/').filter(Boolean).pop() || ''
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
  if (restoringState.value) return
  if (ready) {
    syncTarget()
  } else {
    clearTarget()
  }
})

watch(
  () => [
    props.wizardState.targetEngineID.value,
    props.wizardState.targetRepresentation.value,
    props.wizardState.targetSchema.value,
    props.wizardState.targetTable.value,
    targetConfigSignature(props.wizardState.targetConfig.value || {})
  ],
  async ([engineID]) => {
    if (!engineID) {
      resetLocalTargetForm()
      return
    }
    if (targetStateMatchesLocal()) return
    await restoreState()
  },
  { flush: 'post' }
)

function isObjectStorageEngine(engineType) {
  const type = (engineType || '').toLowerCase()
  return type.includes('s3') || type.includes('minio') || type.includes('oss') || type.includes('objectstore')
}

function syncTarget() {
  if (!selectedEngine.value || !canProceed.value) {
    clearTarget()
    return
  }

  const extra = isNativeTableTarget.value
    ? {
        schema: targetSchema.value,
        table: targetTable.value,
        writeMode: tableWriteMode.value
      }
    : {
        format: isRawCopySource.value ? sourceFormat.value : outputFormat.value,
        backendFormat: selectedOutputFormat.value.backendType,
        resourcePath: outputPath.value,
        resourceFile: isRawCopySource.value ? normalizeFileCatalogPath(outputFileName.value) : normalizedOutputFileName.value,
        includeHeader: !isRawCopySource.value && csvHeaders.value,
        delimiter: isRawCopySource.value ? '' : (outputFormat.value === 'tsv' ? '\t' : csvDelimiter.value),
        geometryField: isRawCopySource.value ? '' : primaryGeometryFieldName.value,
        geometryType: isRawCopySource.value ? '' : primaryGeometryType.value,
        backendOptions: isRawCopySource.value ? {} : selectedOutputFormat.value.options || {},
        writeMode: 'overwrite',
        extensionError: outputFileNameError.value
      }

  props.wizardState.updateTarget({
    engineID: formData.engineID,
    engineType: selectedEngine.value.engine_type,
    targetType: targetStorageKind.value,
    schema: isNativeTableTarget.value ? targetSchema.value : '',
    table: isNativeTableTarget.value ? targetTable.value : '',
    representation: isNativeTableTarget.value ? 'native' : 'encoded',
    extra
  })
}

function clearTarget() {
  props.wizardState.clearTarget?.()
}

function handleOutputFormatChange() {
  applyOutputFileExtension({ replaceKnown: true })
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
  outputPath.value = normalizeFileCatalogPath(path)
  syncTarget()
}

function normalizeObjectStoragePickerPrefix(path, configuredBucket = '') {
  const normalized = normalizeFileCatalogPath(path)
  if (!normalized) return ''
  const bucket = normalizeFileCatalogPath(configuredBucket)
  if (bucket && (normalized === bucket || normalized.startsWith(`${bucket}/`))) {
    const prefix = normalized.slice(bucket.length).replace(/^\/+/, '')
    return prefix ? `${prefix}/` : ''
  }
  return normalized.endsWith('/') ? normalized : `${normalized}/`
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
  if (isRawCopySource.value) return isContentEngine(engine)
  return isNativeTableEngine(engine) || isContentEngine(engine)
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
      .map(normalizeOutputFormatSupport)
      .filter(Boolean)
    const rawCopyFormats = data?.raw_copy_formats || data?.rawCopyFormats || []
    const nextRawCopyFormats = new Map()
    rawCopyFormats.forEach(format => {
      const value = String(format?.value || '').toLowerCase()
      const dataType = String(format?.data_type || format?.dataType || '').toLowerCase()
      if (value && dataType) {
        nextRawCopyFormats.set(value, dataType)
      }
    })
    props.wizardState.updateFormatCapabilities({
      readableEncodedFormats: readable,
      rawCopyFormats: nextRawCopyFormats
    })
    writableOutputFormats.value = writable
    if (!writableOutputFormats.value.some(format => format.value === outputFormat.value)) {
      outputFormat.value = writableOutputFormats.value[0]?.value || ''
    }
    if (!outputFormat.value && !isRawCopySource.value) {
      clearTarget()
    }
  } catch (error) {
    props.wizardState.updateFormatCapabilities()
    writableOutputFormats.value = []
    outputFormat.value = ''
    clearTarget()
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
  if (!state.targetEngineID.value) {
    resetLocalTargetForm()
    return
  }

  restoringState.value = true
  const config = state.targetConfig.value || {}
  try {
    formData.engineID = state.targetEngineID.value
    await nextTick()
    outputFormat.value = isRawCopySource.value ? sourceFormat.value : (config.format || '')
    outputPath.value = normalizeFileCatalogPath(config.resourcePath || '')
    outputFileName.value = config.resourceFile || (isRawCopySource.value ? sourceFileName.value : '')
    csvHeaders.value = config.includeHeader !== false
    csvDelimiter.value = config.delimiter || ','
    targetSchema.value = config.schema || state.targetSchema?.value || ''
    targetTable.value = config.table || state.targetTable?.value || ''
    tableWriteMode.value = normalizeTableWriteMode(config.writeMode)
    if (isNativeTableTarget.value) {
      await loadNamespaces()
      if (targetSchema.value) {
        await loadTargetTables()
      }
    }
  } finally {
    restoringState.value = false
  }
}

function resetLocalTargetForm() {
  formData.engineID = null
  outputPath.value = ''
  outputFileName.value = ''
  targetSchema.value = ''
  targetTable.value = ''
  targetTables.value = []
  namespaces.value = []
  tableWriteMode.value = 'overwrite'
}

function targetStateMatchesLocal() {
  const state = props.wizardState
  const config = state.targetConfig.value || {}
  return formData.engineID === state.targetEngineID.value &&
    outputFormat.value === (isRawCopySource.value ? sourceFormat.value : (config.format || '')) &&
    outputPath.value === normalizeFileCatalogPath(config.resourcePath || '') &&
    outputFileName.value === (config.resourceFile || (isRawCopySource.value ? sourceFileName.value : '')) &&
    targetSchema.value === (config.schema || state.targetSchema?.value || '') &&
    targetTable.value === (config.table || state.targetTable?.value || '') &&
    tableWriteMode.value === normalizeTableWriteMode(config.writeMode)
}

function targetConfigSignature(config) {
  return JSON.stringify({
    format: config.format || '',
    resourcePath: normalizeFileCatalogPath(config.resourcePath || ''),
    resourceFile: config.resourceFile || '',
    includeHeader: config.includeHeader !== false,
    delimiter: config.delimiter || ',',
    schema: config.schema || '',
    table: config.table || '',
    writeMode: normalizeTableWriteMode(config.writeMode)
  })
}

function applyOutputFileExtension(options = {}) {
  if (isRawCopySource.value) {
    syncTarget()
    return
  }
  const normalized = outputFileNameWithExtension(outputFileName.value, options)
  if (normalized && normalized !== outputFileName.value) {
    outputFileName.value = normalized
  }
  syncTarget()
}

function outputFileNameWithExtension(value, options = {}) {
  const extension = selectedOutputExtension.value
  const current = String(value || '').trim().replace(/\\/g, '/')
  if (!current || !extension) return current

  const knownExtensions = outputFormats.value.map(format => normalizeExtension(format.extension)).filter(Boolean)
  const parts = current.split('/')
  const file = parts.pop() || ''
  const dotIndex = file.lastIndexOf('.')
  const currentExtension = dotIndex > 0 ? normalizeExtension(file.slice(dotIndex + 1)) : ''
  if (!currentExtension) {
    parts.push(`${file}.${extension}`)
    return parts.join('/')
  }
  if (currentExtension === extension) return current
  if (options.replaceKnown && knownExtensions.includes(currentExtension)) {
    parts.push(`${file.slice(0, dotIndex)}.${extension}`)
    return parts.join('/')
  }
  return current
}

function currentFileExtension(value) {
  const file = String(value || '').trim().replace(/\\/g, '/').split('/').pop() || ''
  const dotIndex = file.lastIndexOf('.')
  if (dotIndex <= 0) return ''
  return normalizeExtension(file.slice(dotIndex + 1))
}

function normalizeExtension(value) {
  return String(value || '').trim().toLowerCase().replace(/^\.+/, '')
}

function applyDefaultGeometryConfig() {
  syncTarget()
}

function isGeometryField(field) {
  if (!field) return false
  if (field.is_spatial === true || field.isSpatial === true || field.is_geometry === true) return true
  const values = [
    field.type,
    field.geometry_type,
    field.geometryType
  ].map(value => String(value || '').toLowerCase())
  return values.some(value => value.includes('geometry') || geometryTypes.some(type => type.toLowerCase() === value))
}

function inferGeometryType(field) {
  if (!field) return ''
  const values = [
    field.geometry_type,
    field.geometryType,
    field.type
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

function normalizeFileCatalogPath(path) {
  return String(path || '')
    .replace(/\\/g, '/')
    .split('/')
    .map(part => part.trim())
    .filter(part => part && part !== '.' && part !== '/')
    .join('/')
}

function normalizeOutputFormatSupport(item) {
  const value = String(item?.value || '').toLowerCase()
  if (!value) return null
  const fallback = defaultOutputFormat(value)
  return {
    ...fallback,
    value,
    extension: item.extension || '',
    spatial: item.spatial === true,
    backendType: item.backend_type || item.backendType || value,
    options: item.options || {}
  }
}

function rawCopyOutputFormat() {
  return {
    label: sourceFormat.value || 'unknown',
    value: sourceFormat.value || 'unknown',
    backendType: sourceFormat.value || 'unknown',
    extension: sourceFileExtension(sourceFileName.value) || sourceFormat.value || ''
  }
}

function sourceFileExtension(name) {
  const file = String(name || '').trim().replace(/\\/g, '/').split('/').pop() || ''
  const dotIndex = file.lastIndexOf('.')
  if (dotIndex <= 0) return ''
  return normalizeExtension(file.slice(dotIndex + 1))
}

function defaultOutputFormat(value) {
  const defaults = {
    csv: { labelKey: 'transfer.taskWizard.formatCsv', value: 'csv' },
    tsv: { labelKey: 'transfer.taskWizard.formatTsv', value: 'tsv' },
    jsonl: { labelKey: 'transfer.taskWizard.formatJsonl', value: 'jsonl', hintKey: 'transfer.taskWizard.jsonlFormatHint' },
    json: { labelKey: 'transfer.taskWizard.formatJson', value: 'json', hintKey: 'transfer.taskWizard.jsonFormatHint' },
    parquet: { labelKey: 'transfer.taskWizard.formatParquet', value: 'parquet' },
    geojson: { labelKey: 'transfer.taskWizard.formatGeojson', value: 'geojson', hintKey: 'transfer.taskWizard.geojsonFormatHint' },
    shapefile: { labelKey: 'transfer.taskWizard.formatShapefile', value: 'shapefile', hintKey: 'transfer.taskWizard.shapefileFormatHint' }
  }
  return defaults[value] || { label: value, value }
}

function emptyOutputFormat() {
  return { label: '', value: '', extension: '', options: {}, backendType: '' }
}

function parseLocator(locator) {
  const result = { engineID: 0, path: [], type: '', itemID: 0 }
  const match = String(locator || '').match(/^addp:\/\/engine\/(\d+)\/path\/([^?]*)(?:\?(.*))?$/)
  if (!match) return result
  result.engineID = Number(match[1] || 0)
  result.path = String(match[2] || '')
    .split('/')
    .map(part => decodeURIComponent(part).trim())
    .filter(Boolean)
  const params = new URLSearchParams(match[3] || '')
  result.type = String(params.get('type') || '').toLowerCase()
  result.itemID = Number(params.get('item_id') || 0)
  return result
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

.error-hint {
  color: var(--el-color-danger);
}

.is-extension-error :deep(.el-input__wrapper) {
  box-shadow: 0 0 0 1px var(--el-color-danger) inset;
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
