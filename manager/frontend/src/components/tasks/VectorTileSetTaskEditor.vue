<template>
  <el-dialog
    :model-value="modelValue"
    :title="task ? t('manager.vectorTileSet.edit') : t('manager.vectorTileSet.create')"
    width="860px"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
    @closed="emit('closed')"
  >
    <el-form label-position="top" :model="form">
      <div class="form-grid">
        <el-form-item :label="t('manager.vectorTileSet.name')"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('manager.vectorTileSet.fileName')"><el-input v-model="form.fileName"><template #append>.pmtiles</template></el-input></el-form-item>
      </div>
      <el-form-item :label="t('manager.vectorTileSet.source')">
        <div class="picker-wrap" v-loading="sourceDetecting">
          <ResourceTreePicker v-model="sourceSelection" mode="item" :initial-locator="form.sourceLocator" :selectable-filter="isVectorTileSourceItem" tree-height="280px" />
        </div>
      </el-form-item>
      <el-descriptions v-if="sourceFacts.detected" class="source-facts" :column="4" size="small" border>
        <el-descriptions-item :label="t('manager.vectorTileSet.sourceFormat')">{{ sourceFacts.format }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorTileSet.geometryColumn')">{{ form.geometryColumn }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorTileSet.sourceSRID')">EPSG:{{ form.sourceSRID }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorTileSet.recommendedZoom')">{{ sourceFacts.recommendedMinZoom }}-{{ sourceFacts.recommendedMaxZoom }}</el-descriptions-item>
      </el-descriptions>
      <el-form-item :label="t('manager.vectorTileSet.target')">
        <div class="picker-wrap">
          <ResourceTreePicker v-model="targetSelection" mode="node" :initial-locator="form.targetLocator" :engine-families="['object', 'file']" :engine-filter="isBusinessStorageEngine" :engine-label="''" tree-height="260px" />
        </div>
      </el-form-item>
      <div class="form-grid three">
        <el-form-item :label="t('manager.vectorTileSet.minZoom')"><el-input-number v-model="form.minZoom" :min="0" :max="form.maxZoom" /></el-form-item>
        <el-form-item :label="t('manager.vectorTileSet.maxZoom')"><el-input-number v-model="form.maxZoom" :min="form.minZoom" :max="24" /></el-form-item>
        <el-form-item :label="t('manager.vectorTileSet.layerName')"><el-input v-model="form.layerName" /></el-form-item>
      </div>
      <el-alert v-if="tileRangeEstimate.supported" :type="zoomAboveRecommendation ? 'warning' : 'info'" :closable="false" show-icon :title="tileEstimateMessage" />
    </el-form>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ResourceTreePicker } from '@addp/common-frontend'
import { quickViewAPI } from '@/api/quickView'
import { calculateTileRangeEstimate, isZoomAboveRecommendation } from '@/utils/vectorTileEstimate'
import { hasRequiredVectorTileSpatialFacts, isVectorTileSourceItem, resolveVectorTileZoomRecommendation } from '@/utils/vectorTileSetResource'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  task: { type: Object, default: null },
  locator: { type: String, default: '' }
})
const emit = defineEmits(['update:modelValue', 'saved', 'closed'])
const { t } = useI18n()
const sourceSelection = ref(null)
const targetSelection = ref(null)
const sourceDetecting = ref(false)
const saving = ref(false)
const sourceFacts = reactive({ detected: false, format: '', recommendedMinZoom: 0, recommendedMaxZoom: 0 })
const initial = () => ({ name: '', fileName: '', sourceLocator: '', sourceEngineID: 0, sourceItemID: 0, targetLocator: '', targetEngineID: 0, minZoom: 0, maxZoom: 12, layerName: '', sourceSRID: 0, extent: [], extentSRID: 0, geometryColumn: '' })
const form = reactive(initial())
let sourceDetectionSequence = 0

const tileRangeEstimate = computed(() => calculateTileRangeEstimate({ extent: form.extent, extentSRID: form.extentSRID, minZoom: form.minZoom, maxZoom: form.maxZoom }))
const zoomAboveRecommendation = computed(() => isZoomAboveRecommendation(form.maxZoom, sourceFacts.recommendedMaxZoom))
const tileEstimateMessage = computed(() => {
  const count = Number(tileRangeEstimate.value.tileCount || 0).toLocaleString()
  return zoomAboveRecommendation.value
    ? t('manager.vectorTileSet.tileEstimateAboveRecommendation', { count, current: form.maxZoom, recommended: sourceFacts.recommendedMaxZoom })
    : t('manager.vectorTileSet.tileEstimate', { count })
})
const locator = (selection, fallback = '') => String(selection?.identity?.locator || fallback || '').trim()
const engineID = (selection, fallback = 0) => Number(selection?.identity?.engine_id || fallback || 0)
const itemID = (selection, fallback = 0) => Number(selection?.identity?.item_id || fallback || 0)
const selectedName = selection => String(selection?.display?.label || selection?.raw?.node?.label || '').replace(/\.[^.]+$/, '')
const isBusinessStorageEngine = engine => engine?.is_builtin !== true

function resetSourceFacts() {
  Object.assign(sourceFacts, { detected: false, format: '', recommendedMinZoom: 0, recommendedMaxZoom: 0 })
  Object.assign(form, { sourceSRID: 0, extent: [], extentSRID: 0, geometryColumn: '' })
}
function applyCapability(capability, selection) {
  const quickView = capability?.quick_view || {}
  const renderFacts = capability?.render_facts || {}
  const zoom = renderFacts.zoom_recommendation || {}
  const geometryColumns = Array.isArray(quickView.geometry_columns) ? quickView.geometry_columns : []
  const geometryColumn = String(quickView.geometry_column || geometryColumns[0] || '').trim()
  const extent = Array.isArray(renderFacts.render_extent) ? renderFacts.render_extent : quickView.extent
  const sourceSRID = Number(renderFacts.source_srid || quickView.source_srid || 0)
  const extentSRID = Number(renderFacts.render_extent_srid || quickView.extent_srid || 0)
  if (!hasRequiredVectorTileSpatialFacts({ geometryColumn, sourceSRID, extent, extentSRID }, selection)) throw new Error('incomplete spatial capability')
  Object.assign(form, { geometryColumn, sourceSRID, extentSRID, extent: Array.isArray(extent) && extent.length === 4 ? extent.map(Number) : [] })
  const recommendation = resolveVectorTileZoomRecommendation(zoom, quickView, form.extent.length === 4)
  Object.assign(form, { minZoom: recommendation.minZoom, maxZoom: recommendation.maxZoom })
  Object.assign(sourceFacts, { detected: true, format: String(selection?.resource?.format || selection?.resource?.data_type || selection?.display?.type || '-').toUpperCase(), recommendedMinZoom: recommendation.minZoom, recommendedMaxZoom: recommendation.maxZoom })
}

watch(sourceSelection, async value => {
  const sequence = ++sourceDetectionSequence
  resetSourceFacts()
  if (!value) return
  const name = selectedName(value)
  if (!form.name) form.name = name
  if (!form.fileName) form.fileName = name
  if (!form.layerName) form.layerName = name
  sourceDetecting.value = true
  try {
    const capability = await quickViewAPI.getQuickViewCapabilityByLocator(locator(value))
    if (sequence === sourceDetectionSequence) applyCapability(capability, value)
  } catch {
    if (sequence === sourceDetectionSequence) ElMessage.error(t('manager.vectorTileSet.detectFailed'))
  } finally {
    if (sequence === sourceDetectionSequence) sourceDetecting.value = false
  }
})

function reset() {
  sourceDetectionSequence += 1
  Object.assign(form, initial())
  resetSourceFacts()
  sourceSelection.value = null
  targetSelection.value = null
  sourceDetecting.value = false
}
function restore() {
  reset()
  if (!props.task) {
    form.sourceLocator = String(props.locator || '').trim()
    return
  }
  const config = props.task.config || {}
  const source = config.source || {}
  const target = config.target || {}
  const tile = config.tile || {}
  const options = config.options || {}
  Object.assign(form, {
    name: props.task.name || '', fileName: String(target.name || '').replace(/\.pmtiles$/i, ''),
    sourceLocator: source.locator || '', sourceEngineID: source.source_engine_id || 0, sourceItemID: source.item_id || 0,
    targetLocator: target.storage_locator || '', targetEngineID: target.engine_id || 0,
    minZoom: tile.min_zoom ?? 0, maxZoom: tile.max_zoom ?? 12, layerName: options.layer_name || '',
    sourceSRID: tile.source_srid || 0, extent: tile.extent || [], extentSRID: tile.extent_srid || 0, geometryColumn: options.geometry_column || ''
  })
}
watch(() => [props.modelValue, props.task, props.locator], ([visible]) => { if (visible) restore() }, { immediate: true })

function body() {
  const tile = { archive_format: 'pmtiles', tile_type: 'mvt', tile_matrix_set: 'WebMercatorQuad', min_zoom: form.minZoom, max_zoom: form.maxZoom, source_srid: form.sourceSRID, target_srid: 3857 }
  if (form.extent.length === 4 && form.extentSRID > 0) Object.assign(tile, { extent: form.extent, extent_srid: form.extentSRID })
  return { name: form.name.trim(), enabled: true, config: {
    source: { source_engine_id: engineID(sourceSelection.value, form.sourceEngineID), locator: locator(sourceSelection.value, form.sourceLocator), item_id: itemID(sourceSelection.value, form.sourceItemID) },
    target: { engine_id: engineID(targetSelection.value, form.targetEngineID), storage_locator: locator(targetSelection.value, form.targetLocator), name: `${form.fileName.trim()}.pmtiles` },
    tile,
    options: { geometry_column: form.geometryColumn, layer_name: form.layerName.trim() }
  } }
}
async function save() {
  const payload = body()
  const facts = { geometryColumn: form.geometryColumn, sourceSRID: form.sourceSRID, extent: form.extent, extentSRID: form.extentSRID }
  if (!payload.name || !payload.config.source.locator || !payload.config.source.item_id || !payload.config.target.storage_locator || !form.fileName.trim() || !form.layerName.trim() || !hasRequiredVectorTileSpatialFacts(facts, sourceSelection.value)) {
    ElMessage.warning(t('manager.vectorTileSet.required'))
    return
  }
  saving.value = true
  try {
    if (props.task) payload.version = Number(props.task.version || 0)
    if (props.task) await quickViewAPI.updateVectorTileSetTask(props.task.id, payload)
    else await quickViewAPI.createVectorTileSetTask(payload)
    ElMessage.success(t('manager.vectorTileSet.saved'))
    emit('update:modelValue', false)
    emit('saved')
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('manager.vectorTileSet.saveFailed'))
  } finally { saving.value = false }
}
</script>

<style scoped>
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.form-grid.three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.picker-wrap { width: 100%; min-width: 0; }
.source-facts { margin: -4px 0 18px; }
@media (max-width: 760px) { .form-grid, .form-grid.three { grid-template-columns: 1fr; } }
</style>
