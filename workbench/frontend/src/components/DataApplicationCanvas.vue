<template>
  <div ref="runtimeElement" class="runtime" :class="{ 'runtime--wallboard': isWallboard, 'runtime--embedded': embedded, 'runtime--editing': editable }" v-loading="loading" data-testid="data-application-canvas">
    <header class="runtime-header" :class="{ 'runtime-header--compact': !showTitle }">
      <div v-if="showTitle"><h1>{{ application.snapshot.page.title }}</h1><p>{{ application.description }}</p></div>
      <div v-if="!editable" class="runtime-actions">
        <span>{{ statusLabel }}</span>
        <span v-if="refreshDelayMilliseconds">{{ t('workbench.automaticRefreshActive', { interval: refreshIntervalLabel }) }}</span>
        <el-button :disabled="!fullscreenSupported" @click="toggleFullscreen">{{ isFullscreen ? t('workbench.exitFullscreen') : t('workbench.enterFullscreen') }}</el-button>
        <el-button v-if="showQueryActions" data-testid="query-all-action" type="primary" :disabled="!canQueryAll" :loading="queryingAll" @click="queryAll">{{ t('workbench.queryAll') }}</el-button>
      </div>
    </header>

    <el-card v-if="!editable && showParameters && application.snapshot.parameters?.length" class="parameters-card">
      <template #header>
        <div class="parameter-card-header">
          <strong>{{ t('workbench.queryParameters') }}</strong>
          <div class="parameter-preset-actions">
            <el-select
              v-if="parameterPresets.length"
              v-model="selectedPresetKey"
              data-testid="parameter-preset-select"
              :placeholder="t('workbench.selectParameterPreset')"
              @change="applyParameterPreset"
            >
              <el-option v-for="preset in parameterPresets" :key="preset.key" :label="preset.name" :value="preset.key" />
            </el-select>
            <el-button v-if="mode === 'published' && selectedPresetKey" @click="copyPresetLink">{{ t('workbench.copyPresetLink') }}</el-button>
            <el-button @click="resetParameters">{{ t('workbench.resetParameters') }}</el-button>
          </div>
        </div>
      </template>
      <ApplicationParameterFields
        v-if="parameterPlacement.pageParameters.length"
        :parameters="parameterPlacement.pageParameters"
        :values="parameterValues"
        :domains="parameterDomains"
        :selection-sources="parameterSources"
        :submit-enabled="showQueryActions && canQueryAll && !queryingAll"
        @update-value="updateParameterValue"
        @focus-source="focusSelectionSource"
        @submit="queryAll"
      />
    </el-card>

    <div v-if="editable" class="editor-preview-status">
      <span>{{ previewNeedsRefresh ? t('workbench.studio.previewStale') : t('workbench.studio.previewDefaults') }}</span>
      <span v-if="application.snapshot.parameters.length">{{ t('workbench.studio.filterCount', { count: application.snapshot.parameters.length }) }}</span>
    </div>

    <main v-if="application.snapshot.page" class="runtime-grid" :style="editable ? {} : runtimeGridStyle(application.snapshot.page)">
      <el-card v-for="placement in application.snapshot.page.placements" :key="placement.component_id" :ref="element => setComponentElement(placement.component_id, element)" :tabindex="editable ? 0 : -1" :role="editable ? 'button' : undefined" :aria-pressed="editable ? selectedComponentId === placement.component_id : undefined" @click.capture="selectForEditing($event, placement.component_id)" @keydown.enter.self="selectForEditing($event, placement.component_id)" @keydown.space.self="selectForEditing($event, placement.component_id)" @dragover="allowDrop" @drop="dropComponent($event, placement.component_id)" :aria-label="component(placement.component_id)?.title" class="runtime-component" :class="{ 'runtime-component--selected': editable && selectedComponentId === placement.component_id }" data-testid="runtime-component" :data-component-id="placement.component_id" :style="runtimeLayoutStyle(placement)">
        <template #header>
          <div class="component-header">
            <div class="component-title"><button v-if="editable" class="drag-handle" draggable="true" :aria-label="t('workbench.studio.dragComponent')" @dragstart="dragComponent($event, placement.component_id)" @dragend="draggingComponentID = ''">⠿</button><strong>{{ component(placement.component_id)?.title }}</strong></div>
            <el-button v-if="editable" data-testid="canvas-edit-component" link type="primary" @click.stop="emit('edit-component', component(placement.component_id))">{{ t('workbench.editComponent') }}</el-button>
            <div v-if="showQueryActions" class="component-header-actions">
              <el-button link :loading="state(placement.component_id).exporting" :disabled="state(placement.component_id).querying || !canExecuteComponentQuery(state(placement.component_id))" @click="exportComponent(placement.component_id)">{{ t('workbench.export') }}</el-button>
              <el-button link type="primary" :loading="state(placement.component_id).querying" :disabled="state(placement.component_id).exporting || !canExecuteComponentQuery(state(placement.component_id))" @click="queryComponent(placement.component_id)">{{ t('workbench.query') }}</el-button>
            </div>
          </div>
        </template>
        <p v-if="component(placement.component_id)?.description" class="component-description" data-testid="component-description">{{ component(placement.component_id).description }}</p>
        <p v-if="selectionParameterLabels(placement.component_id).length" class="selection-hint" data-testid="selection-hint">{{ t(`workbench.selectionInstructions.${component(placement.component_id).renderer_type}`, { parameters: selectionParameterLabels(placement.component_id).join(', ') }) }}</p>
        <ApplicationParameterFields
          v-if="showParameters && parameterPlacement.componentParameters[placement.component_id]?.length"
          class="component-parameters"
          :inert="editable || undefined"
          data-testid="component-parameters"
          :parameters="parameterPlacement.componentParameters[placement.component_id]"
          :values="parameterValues"
          :domains="parameterDomains"
          :selection-sources="parameterSources"
          :submit-enabled="showQueryActions && !state(placement.component_id).querying && !state(placement.component_id).exporting && canExecuteComponentQuery(state(placement.component_id))"
          @update-value="updateParameterValue"
          @focus-source="focusSelectionSource"
          @submit="queryComponent(placement.component_id)"
        />
        <el-alert
          v-if="componentBlockingError(state(placement.component_id))"
          :data-testid="state(placement.component_id).contract_error ? 'contract-changed-alert' : 'descriptor-load-error-alert'"
          :type="state(placement.component_id).contract_error ? 'warning' : 'error'"
          :closable="false"
          :title="componentBlockingError(state(placement.component_id))"
        />
        <template v-else>
          <el-alert v-if="state(placement.component_id).query_error" data-testid="query-error-alert" type="error" :closable="false" :title="state(placement.component_id).query_error" />
          <WorkbenchRendererHost
            :inert="editable || undefined"
            :rows="state(placement.component_id).rows"
            :renderer-type="component(placement.component_id).renderer_type"
            :config="component(placement.component_id).renderer_config"
            :descriptor="state(placement.component_id).descriptor"
            :page="state(placement.component_id).page"
            :result-ready="state(placement.component_id).query_completed"
            :query-parameters="state(placement.component_id).result_parameters"
            :preserve-view="state(placement.component_id).preserve_map_view"
            @result-select="applySelection(placement.component_id, $event)"
          />
        </template>
        <div v-if="showQueryActions && component(placement.component_id)?.renderer_type === 'table'" class="component-pagination">
          <el-button size="small" :disabled="state(placement.component_id).querying || state(placement.component_id).cursor_index === 0" @click="previousPage(placement.component_id)">{{ t('workbench.previousPage') }}</el-button>
          <span>{{ t('workbench.pageNumber', { page: state(placement.component_id).cursor_index + 1 }) }}</span>
          <el-button size="small" :disabled="state(placement.component_id).querying || !state(placement.component_id).page?.has_more || !state(placement.component_id).page?.next_cursor" @click="nextPage(placement.component_id)">{{ t('workbench.nextPage') }}</el-button>
        </div>
      </el-card>
    </main>
  </div>
</template>

<script setup>
import { applicationQueryContext } from '../utils/applicationEditorLayout.mjs'
import { captureApplicationInitialValues } from '../utils/dataApplicationDraft.mjs'
import { applicationParameterOptions, assertApplicationOptionValues } from '../utils/applicationParameterOptions.mjs'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { createLatestRequestCoordinator } from '@common-ui'
import { executeDescriptorOperation, getConsumerDescriptor } from '../api/services'
import { applicationParameterPlacement, defaultApplicationParameterValues } from '../utils/dataApplicationParameters.mjs'
import { applicationParameterPreset, applicationRefreshDelayMilliseconds, buildComponentQuery, buildSelectionUpdate, canAttemptApplicationQuery, canExecuteComponentQuery, canRunApplicationRefresh, canRunPublishedApplicationInitialQuery, commitLatestComponentDescriptorState, componentBlockingError, initialApplicationParameterValues, invalidateApplicationParameterResults, runtimeGridStyle, runtimeLayoutStyle, runtimeSectionVisible } from '../utils/dataApplicationRuntime.mjs'
import { descriptorSupportsExport, downloadCurrentBoundedExport, exportFormatForRenderer } from '../utils/boundedExport.mjs'
import ApplicationParameterFields from './ApplicationParameterFields.vue'
import WorkbenchRendererHost from './WorkbenchRendererHost.vue'

const props = defineProps({
  application: { type: Object, required: true },
  editable: { type: Boolean, default: false },
  selectedComponentId: { type: String, default: '' },
  mode: { type: String, default: 'published', validator: (value) => ['published', 'draft-preview'].includes(value) },
  embedded: { type: Boolean, default: false },
  initialPresetKey: { type: String, default: '' },
})

const emit = defineEmits(['select-component', 'edit-component', 'move-component'])
const previewNeedsRefresh = ref(false)
const draggingComponentID = ref('')
const editorPreviewRequests = createLatestRequestCoordinator()
const descriptorLoadRequests = createLatestRequestCoordinator()
function selectForEditing(event, id) {
  if (!props.editable || event.target.closest('[data-testid="canvas-edit-component"], .drag-handle')) return
  event.preventDefault()
  event.stopPropagation()
  emit('select-component', id)
}
function dragComponent(event, id) {
  draggingComponentID.value = id
  event.dataTransfer.effectAllowed = 'move'
  event.dataTransfer.setData('text/plain', id)
}
function allowDrop(event) { if (props.editable && draggingComponentID.value) event.preventDefault() }
function dropComponent(event, beforeID) {
  if (!props.editable || !draggingComponentID.value) return
  event.preventDefault()
  emit('move-component', { componentID: draggingComponentID.value, beforeID })
  draggingComponentID.value = ''
}
const { t } = useI18n()
const application = computed(() => props.application)
const loading = ref(false)
const queryingAll = ref(false)
const runtimeElement = ref(null)
const isFullscreen = ref(false)
const fullscreenSupported = ref(false)
const selectedPresetKey = ref(applicationParameterPreset(props.application.snapshot, props.initialPresetKey)?.key || '')
const parameterValues = reactive(initialApplicationParameterValues(props.application.snapshot, selectedPresetKey.value))
const componentStates = reactive({})
const componentElements = new Map()
const parameterPlacement = computed(() => applicationParameterPlacement(application.value.snapshot))
const parameterDomains = computed(() => {
  const snapshot = application.value.snapshot
  const descriptors = runtimeDescriptors()
  return Object.fromEntries(snapshot.parameters.map(parameter => [parameter.key, applicationParameterOptions(snapshot, descriptors, parameter.key)]))
})
const parameterSources = computed(() => Object.fromEntries(application.value.snapshot.parameters.map(parameter => [
  parameter.key,
  selectionGuidance.value.filter(item => item.parameters.some(target => target.key === parameter.key)).map(item => item.source),
])))
const selectionGuidance = computed(() => (application.value.snapshot.selection_bindings || []).map(binding => ({
  source: component(binding.source_component_id),
  parameters: (binding.assignments || []).map(assignment => application.value.snapshot.parameters.find(parameter => parameter.key === assignment.application_parameter_key)).filter(Boolean),
})).filter(item => item.source && item.parameters.length))
const queryAllRequests = createLatestRequestCoordinator()
const isWallboard = computed(() => !props.editable && application.value.snapshot.page?.display_mode === 'wallboard')
const showTitle = computed(() => runtimeSectionVisible(application.value.snapshot.page, 'title'))
const showParameters = computed(() => runtimeSectionVisible(application.value.snapshot.page, 'parameters'))
const showQueryActions = computed(() => !props.editable && runtimeSectionVisible(application.value.snapshot.page, 'query_actions'))
const parameterPresets = computed(() => application.value.snapshot.parameter_presets || [])
const canQueryAll = computed(() => canAttemptApplicationQuery(application.value.snapshot.components, componentStates))
const refreshDelayMilliseconds = computed(() => props.editable ? 0 : applicationRefreshDelayMilliseconds(application.value.snapshot.page))
const statusLabel = computed(() => props.mode === 'draft-preview' ? t('workbench.draftPreviewBadge') : t('workbench.revisionLabel', { revision: application.value.revision_number }))
const refreshIntervalLabel = computed(() => {
  switch (application.value.snapshot.page?.refresh_interval_seconds) {
    case 30: return t('workbench.refreshIntervals.seconds30')
    case 60: return t('workbench.refreshIntervals.minute1')
    case 300: return t('workbench.refreshIntervals.minutes5')
    default: return t('workbench.refreshIntervals.off')
  }
})
let refreshTimer = null
let runtimeMounted = false

function component(id) {
  return application.value.snapshot.components.find((item) => item.id === id)
}

function selectionParameterLabels(id) {
  return selectionGuidance.value.find(item => item.source.id === id)?.parameters.map(parameter => parameter.label) || []
}

function setComponentElement(id, element) {
  if (element) componentElements.set(id, element.$el)
  else componentElements.delete(id)
}

function focusSelectionSource(id) {
  const element = componentElements.get(id)
  element?.scrollIntoView({ block: 'center', behavior: 'instant' })
  element?.focus({ preventScroll: true })
}

function state(id) {
  return componentStates[id] || { rows: [], page: {}, descriptor: null, descriptor_error: '', contract_error: '', query_error: '', querying: false, exporting: false, query_completed: false, preserve_map_view: false, cursors: [''], cursor_index: 0 }
}

function createComponentState() {
  return { rows: [], page: { has_more: false, next_cursor: '' }, descriptor: null, descriptor_error: '', contract_error: '', query_error: '', querying: false, exporting: false, query_completed: false, preserve_map_view: false, cursors: [''], cursor_index: 0, descriptorRequests: createLatestRequestCoordinator(), requests: createLatestRequestCoordinator() }
}

function runtimeDescriptors() {
  return Object.fromEntries(Object.entries(componentStates).map(([id, state]) => [id, state.contract_error || state.descriptor_error ? null : state.descriptor]))
}
function assertComponentOptions(item) {
 const keys = application.value.snapshot.parameter_bindings.filter((b) => b.component_id === item.id).map((b) => b.application_parameter_key)
 assertApplicationOptionValues(application.value.snapshot, runtimeDescriptors(), parameterValues, keys)
}
function updateParameterValues(values, presetKey = '') {
  try { assertApplicationOptionValues(application.value.snapshot, runtimeDescriptors(), values) }
  catch { ElMessage.error(t('workbench.parameterOptionsInvalid')); return false }
  const parameterKeys = Object.keys(values)
  queryAllRequests.invalidate()
  queryingAll.value = false
  invalidateApplicationParameterResults(application.value.snapshot, componentStates, parameterKeys)
  for (const key of Object.keys(parameterValues)) {
    if (!Object.prototype.hasOwnProperty.call(values, key) && !application.value.snapshot.parameters.some((parameter) => parameter.key === key)) delete parameterValues[key]
  }
  Object.assign(parameterValues, structuredClone(values))
  selectedPresetKey.value = presetKey
  return true
}

function updateParameterValue(parameterKey, value) {
  updateParameterValues({ [parameterKey]: value })
}

async function applyParameterPreset(presetKey) {
  const preset = applicationParameterPreset(application.value.snapshot, presetKey)
  if (!preset) return
  if (!updateParameterValues(initialApplicationParameterValues(application.value.snapshot, preset.key), preset.key)) return
  await queryAll()
}

async function resetParameters() {
  updateParameterValues(defaultApplicationParameterValues(application.value.snapshot))
  await queryAll()
}

async function copyPresetLink() {
  if (props.mode !== 'published' || !selectedPresetKey.value) return
  const url = new URL(window.location.href)
  url.search = ''
  url.hash = ''
  url.searchParams.set('preset', selectedPresetKey.value)
  try {
    await navigator.clipboard.writeText(url.toString())
    ElMessage.success(t('workbench.presetLinkCopied'))
  } catch {
    ElMessage.error(t('workbench.presetLinkCopyFailed'))
  }
}

async function loadDescriptors() {
  const request = descriptorLoadRequests.begin('descriptors')
  loading.value = true
  try {
    await Promise.all(application.value.snapshot.components.map(loadDescriptor))
  } finally {
    if (descriptorLoadRequests.isCurrent(request, 'descriptors')) loading.value = false
  }
}

async function loadDescriptor(item) {
  componentStates[item.id] ||= createComponentState()
  const current = componentStates[item.id]
  const request = current.descriptorRequests.begin(item.id)
  current.descriptor_error = ''
  try {
    const { data } = await getConsumerDescriptor(item.service_ref)
    if (data.contract_fingerprint !== item.contract_fingerprint) {
      commitLatestComponentDescriptorState(current, request, item.id, {
        descriptor: null,
        descriptor_error: '',
        contract_error: t('workbench.runtimeContractChanged'),
      })
      return
    }
    commitLatestComponentDescriptorState(current, request, item.id, {
      descriptor: data,
      descriptor_error: '',
      contract_error: '',
    })
  } catch (error) {
    commitLatestComponentDescriptorState(current, request, item.id, {
      descriptor: null,
      descriptor_error: error?.response?.data?.error || t('workbench.loadFailed'),
      contract_error: '',
    })
  }
}

async function queryComponent(componentID, cursor = '', cursorIndex = 0, cursors = [''], options = {}) {
  const item = component(componentID)
  const current = componentStates[componentID]
  if (!item || !canExecuteComponentQuery(current)) return
  const request = current.requests.begin(componentID)
  current.preserve_map_view = options.preserveMapView === true
  current.querying = true
  current.query_error = ''
  try {
    assertComponentOptions(item)
    const operation = current.descriptor.operations.find((candidate) => candidate.key === 'query')
    const requestBody = buildComponentQuery(application.value.snapshot, item, parameterValues, cursor)
    const { data } = await executeDescriptorOperation(operation, requestBody)
    if (!current.requests.isCurrent(request, componentID)) return
    current.rows = data.data || []
    current.result_parameters = structuredClone(requestBody.parameters || {})
    current.page = data.page || { has_more: false, next_cursor: '' }
    current.query_completed = true
    current.cursors = cursors
    current.cursor_index = cursorIndex
  } catch (error) {
    if (!current.requests.isCurrent(request, componentID)) return
    current.query_error = error?.response?.data?.error || (String(error?.message || '').startsWith('parameter-options:') ? t('workbench.parameterOptionsInvalid') : String(error?.message || '').startsWith('missing required') ? t('workbench.requiredParameterMissing') : t('workbench.queryFailed'))
  } finally {
    if (current.requests.isCurrent(request, componentID)) current.querying = false
  }
}

async function applySelection(componentID, selection) {
  if (props.editable) return
  const current = componentStates[componentID]
  if (!current?.descriptor) return
  try {
    const update = buildSelectionUpdate(application.value.snapshot, componentID, current.descriptor, current.rows, selection)
    if (!update) return
    if (!updateParameterValues(update.parameter_values)) return
    ElMessage.success(t('workbench.selectionParametersUpdated', { parameters: selectionParameterLabels(componentID).join(', ') }))
    await Promise.all(update.component_ids.map((targetID) => queryComponent(targetID, '', 0, [''], {
      preserveMapView: component(targetID)?.renderer_type === 'map',
    })))
  } catch {
    ElMessage.error(t('workbench.selectionApplyFailed'))
  }
}

async function nextPage(componentID) {
  const current = componentStates[componentID]
  const nextCursor = current?.page?.next_cursor
  if (!nextCursor) return
  const nextIndex = current.cursor_index + 1
  const cursors = current.cursors.slice(0, nextIndex)
  cursors[nextIndex] = nextCursor
  await queryComponent(componentID, nextCursor, nextIndex, cursors)
}

async function previousPage(componentID) {
  const current = componentStates[componentID]
  if (!current || current.cursor_index === 0) return
  const previousIndex = current.cursor_index - 1
  await queryComponent(componentID, current.cursors[previousIndex], previousIndex, current.cursors)
}

async function exportComponent(componentID) {
  const item = component(componentID)
  const current = componentStates[componentID]
  if (!item || !canExecuteComponentQuery(current)) return
  const format = exportFormatForRenderer(item.renderer_type)
  if (!descriptorSupportsExport(current.descriptor, item.renderer_type)) return ElMessage.warning(t('workbench.exportUnsupported'))
  const request = current.requests.begin(componentID)
  const descriptor = current.descriptor
  const operation = descriptor.operations.find((candidate) => candidate.key === 'query')
  try { assertComponentOptions(item) } catch { return ElMessage.error(t('workbench.parameterOptionsInvalid')) }
  const requestBody = buildComponentQuery(application.value.snapshot, item, parameterValues, '', format)
  current.exporting = true
  try {
    const response = await executeDescriptorOperation(operation, requestBody, { intent: 'export', responseType: 'blob' })
    const outcome = downloadCurrentBoundedExport(
      response,
      `workbench-${item.service_ref.service_type}-${item.service_ref.service_id}.${format}`,
      () => current.requests.isCurrent(request, componentID),
    )
    if (outcome === 'incomplete') ElMessage.warning(t('workbench.exportIncomplete'))
  } catch (error) {
    if (!current.requests.isCurrent(request, componentID)) return
    ElMessage.error(error?.response?.data?.error || t('workbench.exportFailed'))
  } finally {
    if (current.requests.isCurrent(request, componentID)) current.exporting = false
  }
}

async function queryAll() {
  const request = queryAllRequests.begin('query-all')
  queryingAll.value = true
  try {
    await Promise.all(application.value.snapshot.components.map(async (item) => {
      if (!componentStates[item.id]?.descriptor && !componentStates[item.id]?.contract_error) await loadDescriptor(item)
    }))
    await Promise.all(application.value.snapshot.components.map(async (item) => {
      if (!queryAllRequests.isCurrent(request, 'query-all')) return
      if (canExecuteComponentQuery(componentStates[item.id])) await queryComponent(item.id)
    }))
  } finally {
    if (queryAllRequests.isCurrent(request, 'query-all')) queryingAll.value = false
  }
}

function hasActiveComponentQuery() {
  return queryingAll.value || Object.values(componentStates).some((current) => current?.querying || current?.exporting)
}

function clearAutomaticRefreshTimer() {
  if (refreshTimer !== null) window.clearTimeout(refreshTimer)
  refreshTimer = null
}

function scheduleAutomaticRefresh() {
  clearAutomaticRefreshTimer()
  if (!runtimeMounted || document.hidden || refreshDelayMilliseconds.value === 0) return
  refreshTimer = window.setTimeout(() => { void refreshAndSchedule() }, refreshDelayMilliseconds.value)
}

async function refreshAndSchedule() {
  clearAutomaticRefreshTimer()
  if (!runtimeMounted || refreshDelayMilliseconds.value === 0 || document.hidden) return
  if (canRunApplicationRefresh(application.value.snapshot.page, { hidden: document.hidden, querying: hasActiveComponentQuery() })) await queryAll()
  scheduleAutomaticRefresh()
}

function handleVisibilityChange() {
  clearAutomaticRefreshTimer()
  if (!document.hidden) void refreshAndSchedule()
}

function syncFullscreenState() {
  isFullscreen.value = document.fullscreenElement === runtimeElement.value
}

async function toggleFullscreen() {
  try {
    if (document.fullscreenElement) await document.exitFullscreen()
    else await runtimeElement.value.requestFullscreen()
  } catch {
    ElMessage.error(t('workbench.fullscreenFailed'))
  }
}

function invalidatePreview() {
  queryAllRequests.invalidate()
  queryingAll.value = false
  for (const [id, current] of Object.entries(componentStates)) {
    current.descriptorRequests?.invalidate()
    current.requests?.invalidate()
    delete componentStates[id]
  }
  for (const key of Object.keys(parameterValues)) delete parameterValues[key]
  Object.assign(parameterValues, defaultApplicationParameterValues(application.value.snapshot))
}
watch(() => props.editable ? applicationQueryContext(application.value.snapshot) : '', async () => {
  if (!props.editable || !runtimeMounted) return
  editorPreviewRequests.invalidate()
  previewNeedsRefresh.value = true
  invalidatePreview()
  await loadDescriptors()
})
async function refreshEditorPreview() {
  previewNeedsRefresh.value = false
  await queryAll()
}
function captureInitialValues() {
  if (props.mode !== 'draft-preview' || props.editable) throw new Error('not-interactive-preview')
  return captureApplicationInitialValues(application.value.snapshot, runtimeDescriptors(), parameterValues)
}
defineExpose({ refresh: refreshEditorPreview, focusComponent: focusSelectionSource, captureInitialValues })

onMounted(async () => {
  runtimeMounted = true
  fullscreenSupported.value = Boolean(document.fullscreenEnabled && runtimeElement.value?.requestFullscreen)
  document.addEventListener('fullscreenchange', syncFullscreenState)
  document.addEventListener('visibilitychange', handleVisibilityChange)
  const previewRequest = editorPreviewRequests.begin('editor-preview')
  await loadDescriptors()
  if (!runtimeMounted || !editorPreviewRequests.isCurrent(previewRequest, 'editor-preview')) return
  if (props.editable) {
    if (canRunPublishedApplicationInitialQuery(application.value.snapshot, componentStates, parameterValues, true)) await queryAll()
    else previewNeedsRefresh.value = true
  } else if (props.mode === 'published') {
    if (canRunPublishedApplicationInitialQuery(application.value.snapshot, componentStates, parameterValues, !selectedPresetKey.value)) await queryAll()
    scheduleAutomaticRefresh()
  } else {
    await refreshAndSchedule()
  }
})
onBeforeUnmount(() => {
  runtimeMounted = false
  editorPreviewRequests.invalidate()
  descriptorLoadRequests.invalidate()
  clearAutomaticRefreshTimer()
  queryAllRequests.invalidate()
  Object.values(componentStates).forEach((current) => {
    current?.descriptorRequests?.invalidate()
    current?.requests?.invalidate()
  })
  document.removeEventListener('fullscreenchange', syncFullscreenState)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>

<style scoped>
.runtime { min-height: 100vh; padding: 24px; background: var(--addp-bg-secondary); box-sizing: border-box; }
.runtime:fullscreen:not(.runtime--wallboard) { overflow: auto; }
.runtime-header, .runtime-actions, .component-header, .component-header-actions, .parameter-card-header, .parameter-preset-actions { display: flex; align-items: center; }
.runtime-header, .component-header { justify-content: space-between; }
.parameter-card-header { justify-content: space-between; gap: 16px; }
.parameter-preset-actions { gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.parameter-preset-actions .el-select { width: min(280px, 100%); }
.runtime-header { margin-bottom: 16px; gap: 24px; }
.runtime-header--compact { justify-content: flex-end; }
.runtime-header h1 { margin: 0; color: var(--addp-text-primary); font-size: 28px; }
.runtime-header p { margin: 6px 0 0; color: var(--addp-text-secondary); }
.runtime-actions, .component-header-actions { gap: 12px; }
.runtime-actions { color: var(--addp-text-secondary); flex-wrap: wrap; }
.parameters-card { margin-bottom: 16px; }
.component-parameters { flex: 0 0 auto; margin-bottom: 12px; }
.component-description, .selection-hint { flex: 0 0 auto; margin: 0 0 10px; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; overflow-wrap: anywhere; white-space: pre-wrap; }
.runtime-component:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 2px; }
.runtime-grid { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); grid-auto-rows: 64px; gap: 12px; }
.runtime-component { min-width: 0; min-height: 0; overflow: hidden; display: flex; flex-direction: column; }
.runtime-component:deep(.el-card__body) { flex: 1; min-height: 0; overflow: auto; display: flex; flex-direction: column; }
.runtime-component:deep([data-testid="renderer-host"]) { flex: 1; min-height: 0; height: 100%; }
.runtime-component:deep(.chart-renderer),
.runtime-component:deep(.map-container),
.runtime-component:deep(.geojson-result-renderer) { height: 100% !important; min-height: 0; }
.component-header { gap: 12px; }
.component-pagination { flex: 0 0 auto; display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 12px; color: var(--addp-text-secondary); }
.runtime--embedded { height: 100%; min-height: 0; overflow: auto; }
.runtime--wallboard { height: 100vh; min-height: 0; overflow: hidden; display: flex; flex-direction: column; padding: 16px; }
.runtime--embedded.runtime--wallboard { height: 100%; }
.runtime--wallboard .runtime-header, .runtime--wallboard .parameters-card { flex: 0 0 auto; margin-bottom: 12px; }
.runtime--wallboard .runtime-grid { flex: 1; min-height: 0; grid-auto-rows: minmax(0, 1fr); }
@media (max-width: 900px) {
  .runtime { padding: 16px; }
  .runtime-header { align-items: flex-start; flex-direction: column; }
  .parameter-card-header { align-items: stretch; flex-direction: column; }
  .parameter-preset-actions { justify-content: flex-start; }
  .runtime-header--compact { align-items: flex-end; }
  .runtime-grid { display: flex; flex-direction: column; }
  .runtime-component { min-height: 360px; }
  .runtime--wallboard .runtime-grid { display: grid; }
  .runtime--wallboard .runtime-component { min-height: 0; }
}
.runtime--editing { min-height: 480px; padding: 20px; }
.runtime--editing .runtime-header h1 { font-size: 22px; }
.runtime--editing .runtime-header p { font-size: 13px; }
.runtime--editing .runtime-grid { display: grid; grid-auto-rows: 52px; }
.runtime--editing .runtime-component { cursor: pointer; min-height: 0; border: 2px solid transparent; }
.runtime--editing .runtime-component:hover { border-color: var(--el-color-primary-light-5); }
.runtime--editing .runtime-component--selected { border-color: var(--el-color-primary); }
.runtime--editing .runtime-component :deep(.el-card__header) { padding: 12px; }
.runtime--editing .runtime-component :deep(.el-card__body) { padding: 12px; }
.runtime--editing [inert] { pointer-events: none; }
.component-title { display: flex; align-items: center; gap: 8px; min-width: 0; }
.component-title strong { overflow-wrap: anywhere; }
.drag-handle { cursor: grab; font-size: 22px; color: var(--addp-text-secondary); background: none; border: 0; padding: 0 4px; }
.editor-preview-status { display: flex; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; color: var(--addp-text-secondary); font-size: 12px; }
@media(max-width:760px) { .runtime--editing .runtime-grid { display: flex; }.runtime--editing .runtime-component { min-height: 320px; } }
</style>
