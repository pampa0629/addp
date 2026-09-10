<template>
  <div ref="runtimeElement" class="runtime" :class="{ 'runtime--wallboard': isWallboard, 'runtime--embedded': embedded }" v-loading="loading" data-testid="data-application-canvas">
    <header class="runtime-header" :class="{ 'runtime-header--compact': !showTitle }">
      <div v-if="showTitle"><h1>{{ application.snapshot.page.title }}</h1><p>{{ application.description }}</p></div>
      <div class="runtime-actions">
        <span>{{ statusLabel }}</span>
        <span v-if="refreshDelayMilliseconds">{{ t('workbench.automaticRefreshActive', { interval: refreshIntervalLabel }) }}</span>
        <el-button :disabled="!fullscreenSupported" @click="toggleFullscreen">{{ isFullscreen ? t('workbench.exitFullscreen') : t('workbench.enterFullscreen') }}</el-button>
        <el-button v-if="showQueryActions" data-testid="query-all-action" type="primary" :disabled="!canQueryAll" :loading="queryingAll" @click="queryAll">{{ t('workbench.queryAll') }}</el-button>
      </div>
    </header>

    <el-card v-if="showParameters && application.snapshot.parameters?.length" class="parameters-card">
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
      <div class="parameter-grid">
        <label v-for="parameter in application.snapshot.parameters" :key="parameter.key" class="parameter-field">
          <span>{{ parameter.label }}<em v-if="parameter.required">*</em></span>
          <ApplicationParameterValueInput :model-value="parameterValues[parameter.key]" :control-type="parameter.control_type" @update:model-value="updateParameterValue(parameter.key, $event)" />
        </label>
      </div>
    </el-card>

    <main v-if="application.snapshot.page" class="runtime-grid" :style="runtimeGridStyle(application.snapshot.page)">
      <el-card v-for="placement in application.snapshot.page.placements" :key="placement.component_id" class="runtime-component" data-testid="runtime-component" :data-component-id="placement.component_id" :style="runtimeLayoutStyle(placement)">
        <template #header>
          <div class="component-header">
            <strong>{{ component(placement.component_id)?.title }}</strong>
            <div v-if="showQueryActions" class="component-header-actions">
              <el-button link :loading="state(placement.component_id).exporting" :disabled="state(placement.component_id).querying || !canExecuteComponentQuery(state(placement.component_id))" @click="exportComponent(placement.component_id)">{{ t('workbench.export') }}</el-button>
              <el-button link type="primary" :loading="state(placement.component_id).querying" :disabled="state(placement.component_id).exporting || !canExecuteComponentQuery(state(placement.component_id))" @click="queryComponent(placement.component_id)">{{ t('workbench.query') }}</el-button>
            </div>
          </div>
        </template>
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
            :rows="state(placement.component_id).rows"
            :renderer-type="component(placement.component_id).renderer_type"
            :config="component(placement.component_id).renderer_config"
            :descriptor="state(placement.component_id).descriptor"
            :page="state(placement.component_id).page"
            :result-ready="state(placement.component_id).query_completed"
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
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { createLatestRequestCoordinator } from '@common-ui'
import { executeDescriptorOperation, getConsumerDescriptor } from '../api/services'
import { defaultApplicationParameterValues } from '../utils/dataApplicationParameters.mjs'
import { applicationParameterPreset, applicationRefreshDelayMilliseconds, buildComponentQuery, buildSelectionUpdate, canAttemptApplicationQuery, canExecuteComponentQuery, canRunApplicationRefresh, canRunPublishedApplicationInitialQuery, commitLatestComponentDescriptorState, componentBlockingError, initialApplicationParameterValues, invalidateApplicationParameterResults, runtimeGridStyle, runtimeLayoutStyle, runtimeSectionVisible } from '../utils/dataApplicationRuntime.mjs'
import { descriptorSupportsExport, downloadCurrentBoundedExport, exportFormatForRenderer } from '../utils/boundedExport.mjs'
import ApplicationParameterValueInput from './ApplicationParameterValueInput.vue'
import WorkbenchRendererHost from './WorkbenchRendererHost.vue'

const props = defineProps({
  application: { type: Object, required: true },
  mode: { type: String, default: 'published', validator: (value) => ['published', 'draft-preview'].includes(value) },
  embedded: { type: Boolean, default: false },
  initialPresetKey: { type: String, default: '' },
})

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
const queryAllRequests = createLatestRequestCoordinator()
const isWallboard = computed(() => application.value.snapshot.page?.display_mode === 'wallboard')
const showTitle = computed(() => runtimeSectionVisible(application.value.snapshot.page, 'title'))
const showParameters = computed(() => runtimeSectionVisible(application.value.snapshot.page, 'parameters'))
const showQueryActions = computed(() => runtimeSectionVisible(application.value.snapshot.page, 'query_actions'))
const parameterPresets = computed(() => application.value.snapshot.parameter_presets || [])
const canQueryAll = computed(() => canAttemptApplicationQuery(application.value.snapshot.components, componentStates))
const refreshDelayMilliseconds = computed(() => applicationRefreshDelayMilliseconds(application.value.snapshot.page))
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

function state(id) {
  return componentStates[id] || { rows: [], page: {}, descriptor: null, descriptor_error: '', contract_error: '', query_error: '', querying: false, exporting: false, query_completed: false, preserve_map_view: false, cursors: [''], cursor_index: 0 }
}

function createComponentState() {
  return { rows: [], page: { has_more: false, next_cursor: '' }, descriptor: null, descriptor_error: '', contract_error: '', query_error: '', querying: false, exporting: false, query_completed: false, preserve_map_view: false, cursors: [''], cursor_index: 0, descriptorRequests: createLatestRequestCoordinator(), requests: createLatestRequestCoordinator() }
}

function updateParameterValues(values, presetKey = '') {
  const parameterKeys = Object.keys(values)
  queryAllRequests.invalidate()
  queryingAll.value = false
  invalidateApplicationParameterResults(application.value.snapshot, componentStates, parameterKeys)
  for (const key of Object.keys(parameterValues)) {
    if (!Object.prototype.hasOwnProperty.call(values, key) && !application.value.snapshot.parameters.some((parameter) => parameter.key === key)) delete parameterValues[key]
  }
  Object.assign(parameterValues, structuredClone(values))
  selectedPresetKey.value = presetKey
}

function updateParameterValue(parameterKey, value) {
  updateParameterValues({ [parameterKey]: value })
}

async function applyParameterPreset(presetKey) {
  const preset = applicationParameterPreset(application.value.snapshot, presetKey)
  if (!preset) return
  updateParameterValues(initialApplicationParameterValues(application.value.snapshot, preset.key), preset.key)
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
  loading.value = true
  try {
    await Promise.all(application.value.snapshot.components.map(loadDescriptor))
  } finally {
    loading.value = false
  }
}

async function loadDescriptor(item) {
  const current = componentStates[item.id] || createComponentState()
  componentStates[item.id] = current
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
    const operation = current.descriptor.operations.find((candidate) => candidate.key === 'query')
    const { data } = await executeDescriptorOperation(operation, buildComponentQuery(application.value.snapshot, item, parameterValues, cursor))
    if (!current.requests.isCurrent(request, componentID)) return
    current.rows = data.data || []
    current.page = data.page || { has_more: false, next_cursor: '' }
    current.query_completed = true
    current.cursors = cursors
    current.cursor_index = cursorIndex
  } catch (error) {
    if (!current.requests.isCurrent(request, componentID)) return
    current.query_error = error?.response?.data?.error || (String(error?.message || '').startsWith('missing required') ? t('workbench.requiredParameterMissing') : t('workbench.queryFailed'))
  } finally {
    if (current.requests.isCurrent(request, componentID)) current.querying = false
  }
}

async function applySelection(componentID, selection) {
  const current = componentStates[componentID]
  if (!current?.descriptor) return
  try {
    const update = buildSelectionUpdate(application.value.snapshot, componentID, current.descriptor, current.rows, selection)
    if (!update) return
    updateParameterValues(update.parameter_values)
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

onMounted(async () => {
  runtimeMounted = true
  fullscreenSupported.value = Boolean(document.fullscreenEnabled && runtimeElement.value?.requestFullscreen)
  document.addEventListener('fullscreenchange', syncFullscreenState)
  document.addEventListener('visibilitychange', handleVisibilityChange)
  await loadDescriptors()
  if (props.mode === 'published') {
    if (canRunPublishedApplicationInitialQuery(application.value.snapshot, componentStates, parameterValues, !selectedPresetKey.value)) await queryAll()
    scheduleAutomaticRefresh()
  } else {
    await refreshAndSchedule()
  }
})
onBeforeUnmount(() => {
  runtimeMounted = false
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
.parameter-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; }
.parameter-field { display: flex; flex-direction: column; gap: 8px; color: var(--addp-text-primary); }
.parameter-field em { color: var(--el-color-danger); font-style: normal; }
.runtime-grid { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); grid-auto-rows: 64px; gap: 12px; }
.runtime-component { min-width: 0; min-height: 0; overflow: hidden; display: flex; flex-direction: column; }
.runtime-component:deep(.el-card__body) { flex: 1; min-height: 0; overflow: auto; display: flex; flex-direction: column; }
.runtime-component:deep([data-testid="renderer-host"]) { flex: 1; min-height: 0; height: 100%; }
.runtime-component:deep(.chart-renderer),
.runtime-component:deep(.map-container),
.runtime-component:deep(.geojson-result-renderer) { height: 100% !important; min-height: 0; }
.component-header { gap: 12px; }
.component-pagination { display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 12px; color: var(--addp-text-secondary); }
.runtime--embedded { height: calc(100vh - 72px); min-height: 0; overflow: auto; }
.runtime--wallboard { height: 100vh; min-height: 0; overflow: hidden; display: flex; flex-direction: column; padding: 16px; }
.runtime--embedded.runtime--wallboard { height: calc(100vh - 72px); }
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
</style>
