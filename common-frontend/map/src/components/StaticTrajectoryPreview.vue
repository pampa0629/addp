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
import { buildStaticTrajectory, TRAJECTORY_POINT_LIMIT, TrajectoryValidationError, validateTrajectoryTable } from '../utils/staticTrajectory.mjs'

const props = defineProps({ loadTable: { type: Function, required: true } })
const { t, locale } = useI18n()
const visible = ref(false), loading = ref(false), table = ref(null), result = ref(null), error = ref(null)
const selectedCRS = ref(''), renderVersion = ref(0), baseMapType = ref('none')
const { baseMapOptions, getBaseMapProfile, loadMapConfig } = useMapConfig()
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
    validateTrajectoryTable(loaded)
    table.value = loaded
    if (sourceCRSAvailable.value) selectedCRS.value = 'source'
  } catch (failure) {
    if (pending.signal.aborted || controller !== pending) return
    const status = failure?.response?.status
    error.value = failure instanceof TrajectoryValidationError ? failure : { code: status === 401 ? 'login' : status === 403 ? 'denied' : 'load' }
  } finally {
    if (controller === pending) loading.value = false
  }
}
function generate() {
  result.value = null
  error.value = null
  try {
    if (!selectedCRS.value) throw new TrajectoryValidationError('crs')
    const descriptor = selectedCRS.value === 'source' ? table.value : { source_crs: selectedCRS.value, source_srid: Number(selectedCRS.value.split(':')[1]) }
    const transform = getPreviewCRSTransform(descriptor)
    if (crsSuppressionStatus(transform)) throw new TrajectoryValidationError('crs')
    result.value = buildStaticTrajectory(table.value, config, geometry => transformGeoJSONGeometryToWGS84(geometry, transform))
    renderVersion.value += 1
  } catch (failure) {
    error.value = failure instanceof TrajectoryValidationError ? failure : { code: 'coordinate' }
  }
}
watch([config, selectedCRS], () => { result.value = null; error.value = null })
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
