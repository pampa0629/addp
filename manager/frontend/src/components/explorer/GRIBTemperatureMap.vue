<template>
  <section v-if="canRead" class="grib-temperature-map" :data-message="message" :data-source-version="sourceVersion" data-testid="grib-temperature-map">
    <div class="grib-map-toolbar">
      <el-text v-if="validTime">{{ t('manager.spatialPreview.gribValidTime', { time: validTime }) }}</el-text>
      <el-button v-if="!status?.can_use_quick_view && canCreate && canGenerate" :loading="working" size="small"
        data-testid="generate-grib-map" @click="generate">{{ t('manager.spatialPreview.gribGenerateMap') }}</el-button>
      <el-button v-if="error" size="small" :loading="working" @click="load">{{ t('manager.vectorTile.retry') }}</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <div v-if="status?.can_use_quick_view" class="grib-map-canvas">
      <RasterTIFFQuickView :key="status.quick_view.preview_url" :status="status" />
    </div>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/store/auth'
import RasterTIFFQuickView from '@/components/map/RasterTIFFQuickView.vue'
import { quickViewAPI } from '@/api/quickView'
import { waitForRasterCOGExecution } from '@/utils/rasterCOGTask'
import { useCurrentResultConfirmation } from '@/composables/useCurrentResultConfirmation'
import { toQuickViewExistingResultPayload } from '@/utils/currentResultConfirmation'

const props = defineProps({
  locator: { type: String, required: true },
  sourceVersion: { type: String, required: true },
  message: { type: String, required: true },
  validTime: { type: String, default: '' }
})

const { t } = useI18n()
const auth = useAuthStore()
const status = ref(null)
const working = ref(false)
const error = ref('')
const cancel = new AbortController()
const confirmCurrentResult = useCurrentResultConfirmation()
const canRead = computed(() => auth.hasPermission('manager.content.read') && auth.hasPermission('manager.derived_artifact.read'))
const canCreate = computed(() => auth.hasPermission('manager.derived_artifact.create'))
const canGenerate = computed(() => status.value?.available_actions?.includes('generate_raster_cog'))

async function refresh() {
  const result = await quickViewAPI.getQuickViewCapabilityByLocator(props.locator, props.message, cancel.signal)
  if (cancel.signal.aborted) return
  if (result.can_use_quick_view && (result.quick_view?.raster_message !== props.message || result.quick_view?.raster_source_version !== props.sourceVersion)) {
    throw new Error(t('manager.spatialPreview.gribMapSourceChanged'))
  }
  status.value = result
}

async function load() {
  if (!canRead.value || working.value) return
  working.value = true
  error.value = ''
  try {
    await refresh()
  } catch (failure) {
    if (!cancel.signal.aborted) error.value = failure.response?.data?.error || t('manager.spatialPreview.loadRasterQuickViewFailed')
  } finally { working.value = false }
}

async function generate() {
  working.value = true
  error.value = ''
  try {
    const execution = await confirmCurrentResult(payload => quickViewAPI.executeQuickViewAction(props.locator, 'generate_raster_cog', {
      ...toQuickViewExistingResultPayload(payload), raster_message: props.message
    }))
    if (!execution || cancel.signal.aborted) return
    const result = await waitForRasterCOGExecution(execution.execution_id, quickViewAPI.getExecutionStatus, { signal: cancel.signal })
    if (cancel.signal.aborted) return
    if (!result.success) {
      error.value = t(result.completed ? 'manager.spatialPreview.generateRasterCOGExecutionFailed' : 'manager.spatialPreview.generateRasterCOGTimeout')
      return
    }
    await refresh()
  } catch (failure) {
    if (!cancel.signal.aborted) error.value = failure.response?.data?.error || t('manager.spatialPreview.generateRasterCOGFailed')
  } finally { working.value = false }
}

onMounted(load)
onBeforeUnmount(() => cancel.abort())
</script>

<style scoped>
.grib-temperature-map { margin-bottom: 12px; }
.grib-map-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 8px; }
.grib-map-canvas { height: 460px; }
</style>
