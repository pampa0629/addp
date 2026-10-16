<template>
  <el-button size="small" @click="open">{{ t('map.trajectory.open') }}</el-button>
  <el-dialog v-model="visible" :title="t('map.trajectory.title')" width="min(960px, 94vw)" destroy-on-close>
    <div v-loading="loading" class="static-trajectory">
      <p>{{ t('map.trajectory.scope', { limit: TRAJECTORY_POINT_LIMIT }) }}</p>
      <el-alert v-if="errorText" :title="errorText" type="error" :closable="false" show-icon role="alert" />
      <el-form v-if="table" label-position="top" class="trajectory-form" @submit.prevent="generate">
        <el-form-item v-for="role in ['device_field', 'time_field']" :key="role" :label="t(`map.trajectory.${role}`)">
          <el-select v-model="config[role]" :aria-label="t(`map.trajectory.${role}`)">
            <el-option v-for="field in table.columns" :key="field" :value="field" :label="field" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('map.trajectory.time_encoding')">
          <el-select v-model="config.time_encoding" :aria-label="t('map.trajectory.time_encoding')">
            <el-option v-for="encoding in ['iso', 'unix_s', 'unix_ms']" :key="encoding" :value="encoding" :label="t(`map.trajectory.${encoding}`)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('map.trajectory.coordinate_mode')">
          <el-select v-model="config.coordinate_mode" :aria-label="t('map.trajectory.coordinate_mode')">
            <el-option value="columns" :label="t('map.trajectory.columns')" />
            <el-option value="point" :label="t('map.trajectory.pointMode')" />
          </el-select>
        </el-form-item>
        <el-form-item v-for="role in coordinateFields" :key="role" :label="t(`map.trajectory.${role}`)">
          <el-select v-model="config[role]" :aria-label="t(`map.trajectory.${role}`)">
            <el-option v-for="field in table.columns" :key="field" :value="field" :label="field" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('map.trajectory.useRange')">
          <el-switch v-model="range.enabled" :aria-label="t('map.trajectory.useRange')" />
        </el-form-item>
        <template v-if="range.enabled">
          <el-form-item :label="t('map.trajectory.deviceKind')">
            <el-select v-model="range.device_kind" :aria-label="t('map.trajectory.deviceKind')">
              <el-option value="string" :label="t('map.trajectory.stringDevice')" />
              <el-option value="integer" :label="t('map.trajectory.integerDevice')" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('map.trajectory.devices')">
            <el-input v-model="range.devices" type="textarea" :rows="2" :aria-label="t('map.trajectory.devices')" :placeholder="t('map.trajectory.deviceLines')" />
          </el-form-item>
          <el-form-item v-for="edge in ['start', 'end']" :key="edge" :label="t(`map.trajectory.${edge}`)">
            <el-input v-model="range[edge]" :aria-label="t(`map.trajectory.${edge}`)" placeholder="2026-10-16T00:00:00Z" />
          </el-form-item>
        </template>
        <el-form-item :label="t('map.trajectory.crs')">
          <el-select v-model="selectedCRS" :aria-label="t('map.trajectory.crs')">
            <el-option v-if="sourceCRSAvailable" value="source" :label="t('map.trajectory.sourceCRS')" />
            <el-option value="EPSG:4326" label="EPSG:4326 — WGS84" />
            <el-option value="EPSG:3857" label="EPSG:3857 — Web Mercator" />
          </el-select>
        </el-form-item>
        <el-form-item class="trajectory-submit"><el-button native-type="submit" type="primary">{{ t('map.trajectory.generate') }}</el-button></el-form-item>
      </el-form>
      <template v-if="result">
        <p role="status">{{ t('map.trajectory.summary', { devices: result.tracks.length, points: result.pointCount }) }}</p>
        <div class="trajectory-map-controls">
          <el-select v-model="baseMapType" :aria-label="t('map.trajectory.baseMap')">
            <el-option value="none" :label="t('map.trajectory.noBaseMap')" />
            <el-option v-for="option in baseMapOptions" :key="option.value" :value="option.value" :label="option.label" />
          </el-select>
          <el-tag v-if="getBaseMapProfile(baseMapType)?.coordinate_policy === 'gcj02'" type="warning">{{ t('map.businessBrowseMode') }}</el-tag>
        </div>
        <MapContainer :key="renderVersion" :features="result.features" :features-only="baseMapType === 'none'" :base-map-type="baseMapType" :feature-style="featureStyle" height="420px" />
      </template>
    </div>
  </el-dialog>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import MapContainer from './map/MapContainer.vue'
import { useMapConfig } from '../composables/useMapConfig'
import { crsSuppressionStatus, getPreviewCRSTransform, transformGeoJSONGeometryToWGS84 } from '../utils/crsRegistry'
import { buildThematicContext } from '../utils/thematicMap.mjs'
import { buildStaticTrajectory, buildTrajectorySelection, TRAJECTORY_POINT_LIMIT, TrajectoryValidationError, validateTrajectoryScope, validateTrajectoryTable } from '../utils/staticTrajectory.mjs'

const props = defineProps({ loadTable: { type: Function, required: true } })
const { t, locale } = useI18n()
const visible = ref(false), loading = ref(false), table = ref(null), result = ref(null), error = ref(null)
const selectedCRS = ref(''), renderVersion = ref(0), baseMapType = ref('none')
const { baseMapOptions, getBaseMapProfile, loadMapConfig } = useMapConfig()
const range = reactive({ enabled: false, devices: '', device_kind: 'string', start: '', end: '' })
const config = reactive({ device_field: '', time_field: '', coordinate_mode: 'columns', x_field: '', y_field: '', geometry_field: '', time_encoding: 'iso' })
let controller = null
const coordinateFields = computed(() => config.coordinate_mode === 'point' ? ['geometry_field'] : ['x_field', 'y_field'])
const sourceCRSAvailable = computed(() => table.value && !crsSuppressionStatus(getPreviewCRSTransform(table.value)))
const errorText = computed(() => error.value ? t(`map.trajectory.errors.${error.value.code}`, { row: error.value.row, limit: TRAJECTORY_POINT_LIMIT }) : '')
const featureStyle = computed(() => buildThematicContext(result.value?.features || [], { mode: 'categorical', field: 'device', palette: 'primary' }, null, locale.value))

function clear() {
  controller?.abort()
  controller = null
  table.value = null
  result.value = null
  error.value = null
  selectedCRS.value = ''
  baseMapType.value = 'none'
  loading.value = false
  Object.assign(range, { enabled: false, devices: '', device_kind: 'string', start: '', end: '' })
  Object.assign(config, { device_field: '', time_field: '', coordinate_mode: 'columns', x_field: '', y_field: '', geometry_field: '', time_encoding: 'iso' })
}
async function open() {
  clear()
  visible.value = true
  loadMapConfig()
  loading.value = true
  const pending = new AbortController()
  controller = pending
  try {
    const loaded = await props.loadTable(pending.signal)
    if (pending.signal.aborted || controller !== pending) return
    if (!Array.isArray(loaded?.rows) || !Array.isArray(loaded?.columns)) throw new TrajectoryValidationError('not_table')
    table.value = loaded
    if (sourceCRSAvailable.value) selectedCRS.value = 'source'
    validateTrajectoryTable(loaded)
  } catch (failure) {
    if (pending.signal.aborted || controller !== pending) return
    const status = failure?.response?.status
    error.value = failure instanceof TrajectoryValidationError ? failure : { code: status === 401 ? 'login' : status === 403 ? 'denied' : 'load' }
  } finally {
    if (controller === pending) loading.value = false
  }
}
async function generate() {
  result.value = null
  error.value = null
  let pending = null
  try {
    if (!selectedCRS.value) throw new TrajectoryValidationError('crs')
    let data = table.value
    if (range.enabled) {
      const selection = buildTrajectorySelection(config, range)
      pending = new AbortController()
      controller?.abort()
      controller = pending
      loading.value = true
      data = await props.loadTable(pending.signal, selection)
      if (pending.signal.aborted || controller !== pending) return
      validateTrajectoryScope(data, selection)
    }
    const descriptor = selectedCRS.value === 'source' ? data : { source_crs: selectedCRS.value, source_srid: Number(selectedCRS.value.split(':')[1]) }
    const transform = getPreviewCRSTransform(descriptor)
    if (crsSuppressionStatus(transform)) throw new TrajectoryValidationError('crs')
    result.value = buildStaticTrajectory(data, config, geometry => transformGeoJSONGeometryToWGS84(geometry, transform))
    renderVersion.value += 1
  } catch (failure) {
    if (pending && (pending.signal.aborted || controller !== pending)) return
    const apiCode = failure?.response?.data?.error_code
    const mapped = { trajectory_selection_invalid: 'range', trajectory_point_limit_exceeded: 'limit', trajectory_scan_budget_exceeded: 'budget', trajectory_source_invalid: 'source' }
    const status = failure?.response?.status
    error.value = failure instanceof TrajectoryValidationError ? failure : { code: status === 401 ? 'login' : status === 403 ? 'denied' : mapped[apiCode] || 'load' }
  } finally {
    if (pending && controller === pending) loading.value = false
  }
}
watch([config, selectedCRS, range], () => { controller?.abort(); controller = null; loading.value = false; result.value = null; error.value = null })
watch(visible, value => { if (!value) clear() })
onBeforeUnmount(clear)
</script>

<style scoped>
.static-trajectory { min-height: 100px; color: var(--addp-text-primary); }
.trajectory-form { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(210px, 100%), 1fr)); gap: 0 16px; margin-top: 16px; }
.trajectory-form :deep(.el-select) { width: 100%; }
.trajectory-submit { align-self: end; }
.trajectory-map-controls { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 8px; }
.trajectory-map-controls :deep(.el-select) { width: min(280px, 100%); }
</style>
