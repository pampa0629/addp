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

    <el-alert
      v-else-if="isContinuousSource"
      class="target-filter-alert"
      type="info"
      :closable="false"
      :title="t('transfer.taskWizard.continuousTargetTitle')"
      :description="t('transfer.taskWizard.continuousTargetDesc')"
    />

    <el-form class="target-form" :model="formData" label-width="120px">
      <el-form-item :label="t('transfer.taskWizard.targetEngineLabel')">
        <el-select
          v-model="formData.engineID"
          :placeholder="targetEnginePlaceholder"
          filterable
          :loading="loadingEngines"
          :disabled="!sourceTransferSupported"
          @change="handleTargetEngineChange"
          @visible-change="handleTargetEngineDropdownVisible"
        >
          <el-option
            v-for="engine in engines"
            :key="engine.id"
            :label="targetEngineOptionLabel(engine)"
            :value="engine.id"
            :disabled="!isAllowedTargetEngine(engine)"
          />
        </el-select>
      </el-form-item>

      <el-form-item v-if="isNativeTableTarget" :label="t('transfer.taskWizard.targetLocationLabel')">
        <ResourceTreePicker
          v-model="targetParentSelection"
          api-base-url="/api/v1/meta"
          :engine-id="formData.engineID"
          :initial-locator="targetPickerInitialLocator"
          :show-engine-selector="false"
          :selectable-filter="isTargetParentSelectable"
          mode="any"
          tree-height="320px"
          :disabled-label="t('transfer.taskWizard.unsupportedTargetLocationLabel')"
          :show-selection-summary="false"
          :show-count="false"
          @select="handleTargetParentSelect"
        />
      </el-form-item>

      <el-form-item v-if="isNativeTableTarget" :label="t('transfer.taskWizard.targetTableLabel')">
        <el-input
          v-model="targetTable"
          :placeholder="t('transfer.taskWizard.targetTablePlaceholder')"
          :disabled="!formData.engineID || !targetParentSelection?.identity?.locator"
          @input="handleTargetTableInput"
        />
      </el-form-item>

      <el-form-item v-if="isNativeTableTarget && !isContinuousSource" :label="t('transfer.taskWizard.writeModeLabel')">
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

      <el-form-item v-if="isContentTarget" :label="t('transfer.taskWizard.outputPathLabel')">
        <ResourceTreePicker
          v-model="targetParentSelection"
          api-base-url="/api/v1/meta"
          :engine-id="formData.engineID"
          :initial-locator="targetPickerInitialLocator"
          :show-engine-selector="false"
          :selectable-filter="isTargetParentSelectable"
          mode="any"
          tree-height="320px"
          :disabled-label="t('transfer.taskWizard.unsupportedTargetParentLabel')"
          :show-selection-summary="false"
          :show-count="false"
          @select="handleTargetParentSelect"
        />
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

      <el-form-item
        v-if="isContentTarget && selectedColumnarCompressionCapability"
        :label="t('transfer.taskWizard.columnCompressionLabel')"
      >
        <el-select v-model="outputCompression" @change="syncTarget">
          <el-option
            v-for="codec in selectedColumnarCompressionCapability.codecs"
            :key="codec"
            :label="columnarCompressionLabel(codec)"
            :value="codec"
          />
        </el-select>
        <div class="field-hint">{{ t('transfer.taskWizard.columnCompressionHint') }}</div>
      </el-form-item>

      <el-form-item v-if="isContentTarget" :label="t('transfer.taskWizard.outputFileNameLabel')">
        <el-input
          v-model="outputFileName"
          :placeholder="outputFileNamePlaceholder"
          :class="{ 'is-extension-error': !!outputFileNameError }"
          @focus="handleOutputFileNameFocus"
          @input="syncTarget"
          @blur="handleOutputFileNameBlur"
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

  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import {
  ResourceTreePicker,
  buildLocator as buildResourceLocator,
  engineSelectionState,
  isEngineSelectable
} from '@addp/common-frontend'
import { capabilitiesAPI } from '@/api/capabilities'
import { getItemFieldsByID, getNodeByCatalogPath } from '@/api/meta'
import { systemEnginesAPI } from '@/api/systemEngines'
import { parseTransferLocator } from '@/utils/resourceLocator'
import {
  isNativeTargetSelectable,
  sameNativeTargetParentIdentity
} from './nativeTargetSelection.mjs'
import {
  normalizeColumnarCompressionCapability,
  resolveColumnarCompression,
  withColumnarCompressionOption
} from './columnarCompression.mjs'
import {
  dataTypeLabel,
  engineOptionLabel,
  formatLabel,
	hasAtomicPartitionedTableChangeApply,
  hasIdempotentTableUpsert,
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
const outputCompression = ref('')
const outputPath = ref('')
const outputFileName = ref('')
const csvHeaders = ref(true)
const csvDelimiter = ref(',')
const targetParentSelection = ref(null)
const normalizedTargetParentLocator = ref('')
const restoredParentLocator = ref('')
const outputFileNameFocused = ref(false)
const targetSchema = ref('')
const targetTable = ref('')
const tableWriteMode = ref('overwrite')
const tableWriteModeOptions = ['overwrite', 'append']

const engines = ref([])
const loadingEngines = ref(false)
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
const geometryTypes = ['Geometry', 'Point', 'MultiPoint', 'LineString', 'MultiLineString', 'Polygon', 'MultiPolygon', 'GeometryCollection']
const shapefileGeometryTypes = ['Point', 'MultiPoint', 'LineString', 'MultiLineString', 'Polygon', 'MultiPolygon']

const selectedEngine = computed(() => {
  const currentEngineID = normalizeEngineID(formData.engineID)
  return engines.value.find(engine => normalizeEngineID(engine.id) === currentEngineID) || null
})

const sourceDataType = computed(() => props.wizardState.sourceDataType?.value || 'table')

const sourceRepresentation = computed(() => props.wizardState.sourceRepresentation?.value || 'native')

const sourceFormat = computed(() => props.wizardState.sourceFormat?.value || '')

const isContinuousSource = computed(() => props.wizardState.isContinuousTask?.value === true)

const sourceTransferSupported = computed(() => {
  if (isContinuousSource.value) return true
  if (sourceDataType.value === 'table') {
    return sourceRepresentation.value === 'native' ||
      (sourceRepresentation.value === 'encoded' && supportedEncodedSourceFormat(sourceFormat.value))
  }
  return isRawCopySource.value
})

const isRawCopySource = computed(() => {
  return ['document', 'media', 'cad', 'unknown'].includes(sourceDataType.value) &&
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
  if (isContinuousSource.value) {
		return isNativeTableEngine(selectedEngine.value) && hasAtomicPartitionedTableChangeApply(selectedEngine.value)
	}
  return isNativeTableEngine(selectedEngine.value)
})

const isContentTarget = computed(() => {
  return isContentEngine(selectedEngine.value)
})

const targetStorageKind = computed(() => {
  return isObjectStorageEngine(selectedEngine.value?.engine_type) ? 's3' : selectedEngine.value?.engine_type || ''
})

const targetPickerInitialLocator = computed(() => restoredParentLocator.value || '')

const canProceed = computed(() => {
  if (isNativeTableTarget.value) {
    const hasParentLocator = !!targetParentLocator.value
    return !!(formData.engineID && hasParentLocator && targetTable.value.trim())
  }
  if (isContentTarget.value) {
    const hasRequiredPath = !!targetParentLocator.value
    const hasOutputFormat = isRawCopySource.value || !!selectedOutputFormat.value.value
    const hasValidCompression = !selectedColumnarCompressionCapability.value ||
      selectedColumnarCompressionCapability.value.codecs.includes(outputCompression.value)
    return !!(formData.engineID && hasRequiredPath && hasOutputFormat && hasValidCompression && outputFileName.value.trim() && !outputFileNameError.value)
  }
  return false
})

const targetParentLocator = computed(() => {
  const selection = targetParentSelection.value
  if (normalizedTargetParentLocator.value) return normalizedTargetParentLocator.value
  if (!selection?.identity?.locator) return restoredParentLocator.value || ''
  return selection.identity.locator
})

const selectedOutputFormat = computed(() => {
  if (isRawCopySource.value) {
    return rawCopyOutputFormat()
  }
  return outputFormats.value.find(format => format.value === outputFormat.value) || outputFormats.value[0] || emptyOutputFormat()
})

const selectedColumnarCompressionCapability = computed(() => {
  if (isRawCopySource.value) return null
  return selectedOutputFormat.value.columnarCompression || null
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
  const loc = parseTransferLocator(sourceConfig.locator || props.wizardState.sourceLocator?.value || '')
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
  const geometryType = inferGeometryType(primaryGeometryField.value)
  if (outputFormat.value === 'shapefile' && !shapefileGeometryTypes.includes(geometryType)) {
    return ''
  }
  return geometryType
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
    syncTargetDraft()
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
    syncTargetDraft()
    return
  }

  const extra = isNativeTableTarget.value
    ? {
        schema: targetSchema.value,
        table: targetTable.value,
        parentLocator: targetParentLocator.value,
        writeMode: tableWriteMode.value
      }
    : {
        format: isRawCopySource.value ? sourceFormat.value : outputFormat.value,
        backendFormat: selectedOutputFormat.value.backendType,
        parentLocator: targetParentLocator.value,
        resourcePath: outputPath.value,
        resourceFile: targetResourceFile({ finalize: !outputFileNameFocused.value }),
        includeHeader: !isRawCopySource.value && csvHeaders.value,
        delimiter: isRawCopySource.value ? '' : (outputFormat.value === 'tsv' ? '\t' : csvDelimiter.value),
        geometryField: isRawCopySource.value ? '' : primaryGeometryFieldName.value,
        geometryType: isRawCopySource.value ? '' : primaryGeometryType.value,
        backendOptions: selectedFormatBackendOptions(),
        writeMode: 'overwrite',
        extensionError: outputFileNameError.value
      }

	props.wizardState.updateTarget({
		engineID: formData.engineID,
		engineType: selectedEngine.value.engine_type,
		capabilities: selectedEngine.value.capabilities,
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

function syncTargetDraft() {
  if (!selectedEngine.value || !formData.engineID) {
    clearTarget()
    return
  }
	props.wizardState.updateTarget({
		engineID: normalizeEngineID(formData.engineID),
		engineType: selectedEngine.value.engine_type,
		capabilities: selectedEngine.value.capabilities,
		targetType: targetStorageKind.value,
    schema: '',
    table: '',
    representation: isNativeTableTarget.value ? 'native' : 'encoded',
    extra: buildTargetDraftExtra()
  })
}

function buildTargetDraftExtra() {
  if (isNativeTableTarget.value) {
    return {
      schema: targetSchema.value,
      table: targetTable.value,
      parentLocator: targetParentLocator.value,
      writeMode: tableWriteMode.value
    }
  }
  if (isContentTarget.value) {
    return {
      format: isRawCopySource.value ? sourceFormat.value : outputFormat.value,
      backendFormat: selectedOutputFormat.value.backendType,
      parentLocator: targetParentLocator.value,
      resourcePath: outputPath.value,
      resourceFile: targetResourceFile({ finalize: false }),
      includeHeader: !isRawCopySource.value && csvHeaders.value,
      delimiter: isRawCopySource.value ? '' : (outputFormat.value === 'tsv' ? '\t' : csvDelimiter.value),
      geometryField: isRawCopySource.value ? '' : primaryGeometryFieldName.value,
      geometryType: isRawCopySource.value ? '' : primaryGeometryType.value,
      backendOptions: selectedFormatBackendOptions(),
      writeMode: 'overwrite',
      extensionError: outputFileNameError.value
    }
  }
  return {}
}

function handleOutputFormatChange() {
  resetColumnarCompression()
  applyOutputFileExtension({ replaceKnown: true })
  if (isSpatialFormat.value) {
    applyDefaultGeometryConfig()
  }
  syncTarget()
}

function handleOutputFileNameFocus() {
  outputFileNameFocused.value = true
}

function handleOutputFileNameBlur() {
  outputFileNameFocused.value = false
  applyOutputFileExtension()
}

async function handleTargetEngineChange() {
  formData.engineID = normalizeEngineID(formData.engineID)
  outputPath.value = ''
  outputFileName.value = ''
  targetParentSelection.value = null
  normalizedTargetParentLocator.value = ''
  restoredParentLocator.value = ''
  targetSchema.value = ''
  targetTable.value = ''
  outputCompression.value = ''
  props.wizardState.resetTargetFields?.()
  syncTarget()
}

async function handleTargetParentSelect(selection) {
  if (isNativeTableTarget.value) {
    if (selection?.resource?.kind === 'item') {
      const selected = await selectExistingNativeTarget(selection)
      if (!selected) return
    } else {
			const previousParentLocator = targetParentLocator.value
			const nextParentLocator = selection?.identity?.locator || ''
      const parentChanged = !sameTargetParentIdentity(previousParentLocator, nextParentLocator)
      targetParentSelection.value = selection
      normalizedTargetParentLocator.value = ''
      targetSchema.value = targetParentNameFromSelection(selection)
			if (parentChanged) {
				targetTable.value = ''
				props.wizardState.resetTargetFields?.()
			}
    }
  } else if (isContentTarget.value) {
    if (selection.resource?.kind === 'item') {
      const normalized = await normalizeExistingTargetSelection(selection)
      if (!normalized) {
        syncTarget()
        return
      }
      targetParentSelection.value = normalized.selection
      normalizedTargetParentLocator.value = normalized.parentLocator
      outputPath.value = normalized.parentPath
      outputFileName.value = normalized.fileName
      applyOutputFormatFromFileName(normalized.fileName)
    } else {
      targetParentSelection.value = selection
      normalizedTargetParentLocator.value = await normalizeNodeSelectionLocator(selection)
      outputPath.value = targetParentPathFromSelection(selection)
    }
  }
  syncTarget()
}

async function selectExistingNativeTarget(selection) {
  let normalized = null
  try {
    normalized = await normalizeExistingTargetSelection(selection)
  } catch {}
  if (!normalized) {
    targetParentSelection.value = null
    normalizedTargetParentLocator.value = ''
    targetSchema.value = ''
    targetTable.value = ''
    props.wizardState.resetTargetFields?.()
    ElMessage.warning(t('transfer.taskWizard.invalidExistingTargetSelection'))
    syncTarget()
    return false
  }

  targetParentSelection.value = selection
  normalizedTargetParentLocator.value = normalized.parentLocator
  targetSchema.value = targetParentNameFromSelection(selection)
  targetTable.value = targetNameFromSelection(selection)
  tableWriteMode.value = 'overwrite'
  await loadExistingTargetFields(selection)
  return true
}

async function loadExistingTargetFields(selection) {
  const itemID = Number(selection?.identity?.item_id || parseTransferLocator(selection?.identity?.locator || '').itemID || 0)
  if (!itemID) {
    props.wizardState.resetTargetFields?.()
    return
  }

  try {
    const response = await getItemFieldsByID(itemID)
    const fields = Array.isArray(response?.data) ? response.data : (response || [])
    props.wizardState.loadTargetFields(fields)
  } catch (error) {
    props.wizardState.resetTargetFields?.()
    ElMessage.warning(t('transfer.taskWizard.loadTargetFieldsWarning', {
      error: error.response?.data?.error || error.message
    }))
  }
}

function handleTargetTableInput() {
  if ((props.wizardState.targetFields?.value || []).length > 0) {
    props.wizardState.resetTargetFields?.()
  }
  syncTarget()
}

function sameTargetParentIdentity(left, right) {
	return sameNativeTargetParentIdentity(parseTransferLocator(left), parseTransferLocator(right))
}

function isTargetParentSelectable(node, context = {}) {
  const type = String(node?.type || '').toLowerCase()
  if (isNativeTableTarget.value) {
    if (isContinuousSource.value && type === 'table') return false
    return isNativeTargetSelectable(node, context)
  }
  if (['object', 'file'].includes(type)) {
    return !!context.locator?.itemId
  }
  return ['root', 'directory', 'dir', 'bucket', 'prefix', 'service'].includes(type) && !!context.locator?.nodeId
}

function targetParentNameFromSelection(selection) {
  const path = parseTransferLocator(selection?.identity?.locator || '').path
  if (selection?.resource?.kind === 'item') {
    return path[path.length - 2] || ''
  }
  return path[path.length - 1] || selection?.raw?.node?.label || ''
}

function targetParentPathFromSelection(selection) {
  const path = parseTransferLocator(selection?.identity?.locator || '').path
  return path.join('/')
}

function targetNameFromSelection(selection) {
  const path = parseTransferLocator(selection?.identity?.locator || '').path
  return path[path.length - 1] || selection?.display?.label || selection?.raw?.node?.label || ''
}

async function normalizeExistingTargetSelection(selection) {
  const loc = parseTransferLocator(selection?.identity?.locator || '')
  if (!loc.engineID || loc.path.length === 0) return null
  const parentPath = loc.path.slice(0, -1)
  const parentPathText = parentPath.join('/')
  const parentNode = await getNodeByCatalogPath(loc.engineID, parentPathText)
  if (!parentNode?.id) return null
  const parentType = String(parentNode.node_type || parentNode.type || parentLocatorTypeForPath(parentPath)).toLowerCase()
  const parentLocator = buildParentNodeLocator(loc.engineID, parentType, parentPath, parentNode.id)
  return {
    parentLocator,
    parentPath: parentPathText,
    fileName: targetNameFromSelection(selection),
    selection: {
      identity: {
        locator: parentLocator,
        engine_id: loc.engineID,
        node_id: parentNode.id
      },
      display: {
        label: parentPath[parentPath.length - 1] || selection?.raw?.engine?.name || '',
        path: parentPathText
      },
      resource: {
        kind: 'node',
        type: parentType
      },
      raw: {
        engine: selection?.raw?.engine,
        node: parentNode
      }
    }
  }
}

async function normalizeNodeSelectionLocator(selection) {
  const loc = parseTransferLocator(selection?.identity?.locator || '')
  if (!loc.engineID || !loc.type) return ''
  const nodeID = Number(selection?.identity?.node_id || selection?.raw?.node?.metadata?.node_id || 0)
  if (nodeID > 0) {
    return buildParentNodeLocator(loc.engineID, loc.type, loc.path, nodeID)
  }
  if (locatorHasNodeID(selection?.identity?.locator || '')) {
    return selection.identity.locator
  }
  const node = await getNodeByCatalogPath(loc.engineID, loc.path.join('/'))
  if (node?.id) {
    return buildParentNodeLocator(loc.engineID, node.node_type || loc.type, loc.path, node.id)
  }
  return selection?.identity?.locator || ''
}

function buildParentNodeLocator(engineID, type, path, nodeID) {
  return buildResourceLocator({
    engineId: engineID,
    type,
    path,
    nodeId: nodeID
  })
}

function parentLocatorTypeForPath(path) {
  if ((path || []).length <= 1) {
    return isObjectStorageEngine(selectedEngine.value?.engine_type) ? 'bucket' : 'root'
  }
  return isObjectStorageEngine(selectedEngine.value?.engine_type) ? 'prefix' : 'directory'
}

function applyOutputFormatFromFileName(fileName) {
  if (isRawCopySource.value) return
  const extension = currentFileExtension(fileName)
  if (!extension) return
  const matchedFormat = outputFormats.value.find(format => normalizeExtension(format.extension) === extension)
  if (!matchedFormat || matchedFormat.value === outputFormat.value) return
  outputFormat.value = matchedFormat.value
  resetColumnarCompression()
  if (isSpatialFormat.value) {
    applyDefaultGeometryConfig()
  }
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
    if (formData.engineID && !engines.value.some(engine => normalizeEngineID(engine.id) === normalizeEngineID(formData.engineID))) {
      formData.engineID = null
    }
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.loadTargetEngineFailedMsg'))
  } finally {
    loadingEngines.value = false
  }
}

function handleTargetEngineDropdownVisible(visible) {
  if (visible) loadEngines()
}

function targetEngineOptionLabel(engine) {
  return `${engineOptionLabel(engine)} · ${t(`common.engineStatus.${engineSelectionState(engine)}`)}`
}

function isAllowedTargetEngine(engine) {
  if (!sourceTransferSupported.value || !isEngineSelectable(engine)) return false
  if (isContinuousSource.value) return isNativeTableEngine(engine) && hasAtomicPartitionedTableChangeApply(engine)
  if (props.wizardState.isWatermarkIncremental?.value) {
    return isNativeTableEngine(engine) && hasIdempotentTableUpsert(engine)
  }
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
		rawCopyFormats: nextRawCopyFormats,
		databaseCDC: data?.continuous?.database_cdc || data?.continuous?.databaseCDC || null
    })
    writableOutputFormats.value = writable
    if (!writableOutputFormats.value.some(format => format.value === outputFormat.value)) {
      outputFormat.value = writableOutputFormats.value[0]?.value || ''
    }
		resetColumnarCompression(outputCompression.value)
		if (!outputFormat.value && !isRawCopySource.value && props.wizardState.targetRepresentation.value === 'encoded') {
      clearTarget()
    }
  } catch (error) {
    props.wizardState.updateFormatCapabilities()
    writableOutputFormats.value = []
    outputFormat.value = ''
    outputCompression.value = ''
    clearTarget()
    ElMessage.warning(t('transfer.taskWizard.loadCapabilitiesFailedMsg'))
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
    formData.engineID = normalizeEngineID(state.targetEngineID.value)
    await nextTick()
    outputFormat.value = isRawCopySource.value ? sourceFormat.value : (config.format || '')
    outputCompression.value = resolveColumnarCompression(
      selectedColumnarCompressionCapability.value,
      config.compression || config.backendOptions?.compression || ''
    )
    outputPath.value = normalizeFileCatalogPath(config.resourcePath || '')
    outputFileName.value = config.resourceFile || (isRawCopySource.value ? sourceFileName.value : '')
    csvHeaders.value = config.includeHeader !== false
    csvDelimiter.value = config.delimiter || ','
    targetSchema.value = config.schema || state.targetSchema?.value || ''
    targetTable.value = config.table || state.targetTable?.value || ''
    tableWriteMode.value = normalizeTableWriteMode(config.writeMode)
    restoredParentLocator.value = await normalizeRestoredParentLocator(config.parentLocator || '')
    normalizedTargetParentLocator.value = restoredParentLocator.value
    if (restoredParentLocator.value) {
      const label = isNativeTableTarget.value
        ? targetSchema.value
        : (normalizeFileCatalogPath(config.resourcePath || '') || '/')
      targetParentSelection.value = {
        identity: {
          locator: restoredParentLocator.value,
          engine_id: formData.engineID
        },
        display: {
          label,
          path: label
        },
        raw: {}
      }
    }
  } finally {
    restoringState.value = false
  }
  if (canProceed.value) {
    syncTarget()
  } else {
    syncTargetDraft()
  }
}

async function normalizeRestoredParentLocator(locator) {
  const parsed = parseTransferLocator(locator)
  if (!parsed.engineID || !parsed.type) return locator
  if (locatorHasNodeID(locator)) return locator

  const catalogPath = parsed.path.join('/')
  try {
    const node = await getNodeByCatalogPath(parsed.engineID, catalogPath)
    if (!node?.id) return locator
    return buildResourceLocator({
      engineId: parsed.engineID,
      type: node.node_type || parsed.type,
      path: parsed.path,
      nodeId: node.id
    })
  } catch {
    return ''
  }
}

function locatorHasNodeID(locator) {
  try {
    const url = new URL(locator)
    const nodeID = Number(url.searchParams.get('node_id') || 0)
    return nodeID > 0
  } catch {
    return false
  }
}

function resetLocalTargetForm() {
  formData.engineID = null
  outputPath.value = ''
  outputFileName.value = ''
  outputCompression.value = ''
  targetParentSelection.value = null
  normalizedTargetParentLocator.value = ''
  restoredParentLocator.value = ''
  targetSchema.value = ''
  targetTable.value = ''
  tableWriteMode.value = 'overwrite'
}

function targetStateMatchesLocal() {
  const state = props.wizardState
  const config = state.targetConfig.value || {}
  return normalizeEngineID(formData.engineID) === normalizeEngineID(state.targetEngineID.value) &&
    outputFormat.value === (isRawCopySource.value ? sourceFormat.value : (config.format || '')) &&
    outputCompression.value === targetConfigCompression(config) &&
    outputPath.value === normalizeFileCatalogPath(config.resourcePath || '') &&
    outputFileName.value === targetConfigResourceFileForLocalCompare(config) &&
    targetSchema.value === (config.schema || state.targetSchema?.value || '') &&
    targetTable.value === (config.table || state.targetTable?.value || '') &&
    targetParentLocator.value === (config.parentLocator || '') &&
    tableWriteMode.value === normalizeTableWriteMode(config.writeMode)
}

function targetConfigResourceFileForLocalCompare(config) {
  const fallback = isRawCopySource.value ? sourceFileName.value : ''
  const resourceFile = config.resourceFile || fallback
  if (isRawCopySource.value || !outputFileNameFocused.value) return resourceFile
  return normalizeFileCatalogPath(resourceFile)
}

function targetConfigSignature(config) {
  return JSON.stringify({
    format: config.format || '',
    resourcePath: normalizeFileCatalogPath(config.resourcePath || ''),
    resourceFile: config.resourceFile || '',
    includeHeader: config.includeHeader !== false,
    delimiter: config.delimiter || ',',
    compression: targetConfigCompression(config),
    schema: config.schema || '',
    table: config.table || '',
    parentLocator: config.parentLocator || '',
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

function targetResourceFile(options = {}) {
  if (isRawCopySource.value) {
    return normalizeFileCatalogPath(outputFileName.value)
  }
  return options.finalize ? normalizedOutputFileName.value : normalizeFileCatalogPath(outputFileName.value)
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
  let normalized = String(value || '').toLowerCase().replace(/[_\s-]/g, '')
  if (normalized.startsWith('st')) {
    normalized = normalized.slice(2)
  }
  normalized = normalized.replace(/(zm|z|m)$/, '')
  return geometryTypes.find(type => type.toLowerCase().replace(/[_\s-]/g, '') === normalized) || ''
}

function normalizeTableWriteMode(value) {
  const mode = String(value || '').toLowerCase()
  if (mode === 'append') return 'append'
  return 'overwrite'
}

function normalizeEngineID(value) {
  const id = Number(value || 0)
  return Number.isFinite(id) && id > 0 ? id : null
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
    options: item.options || {},
    columnarCompression: normalizeColumnarCompressionCapability(
      item.columnar_compression || item.columnarCompression
    )
  }
}

function selectedFormatBackendOptions() {
  if (isRawCopySource.value) return {}
  return withColumnarCompressionOption(
    selectedOutputFormat.value.options || {},
    selectedColumnarCompressionCapability.value,
    outputCompression.value
  )
}

function resetColumnarCompression(selected = '') {
  outputCompression.value = resolveColumnarCompression(
    selectedColumnarCompressionCapability.value,
    selected
  )
}

function targetConfigCompression(config) {
  return resolveColumnarCompression(
    selectedColumnarCompressionCapability.value,
    config.compression || config.backendOptions?.compression || ''
  )
}

function columnarCompressionLabel(codec) {
  const key = `transfer.taskWizard.columnCompressionCodecs.${codec}`
  const label = t(key)
  return label === key ? codec : label
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

onMounted(async () => {
  await loadCapabilities()
  await loadEngines()
  await restoreState()
})
</script>

<style scoped>
.step2-select-target {
  max-width: 1120px;
  margin: 0 auto;
}

.step-description {
  color: var(--addp-text-secondary);
  margin-bottom: 30px;
}

.target-form :deep(.el-form-item__content) {
  width: min(960px, 100%);
  max-width: 100%;
}

.target-form :deep(.el-select),
.target-form :deep(.el-input),
.target-form :deep(.resource-tree-picker) {
  width: 100%;
  min-width: 0;
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
