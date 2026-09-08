<template>
  <el-dialog
    :model-value="modelValue"
    :title="task ? t('manager.rasterMosaic.editTitle') : t('manager.rasterMosaic.createTitle')"
    width="820px"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
    @closed="emit('closed')"
  >
    <el-form label-position="top" :model="form">
      <div class="form-grid">
        <el-form-item :label="t('manager.rasterMosaic.name')"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('manager.rasterMosaic.enabled')"><el-switch v-model="form.enabled" :active-text="t('manager.rasterMosaic.enabledYes')" :inactive-text="t('manager.rasterMosaic.enabledNo')" /></el-form-item>
      </div>
      <el-form-item :label="t('manager.rasterMosaic.description')"><el-input v-model="form.description" type="textarea" :rows="2" /></el-form-item>

      <el-divider content-position="left">{{ t('manager.rasterMosaic.sourceScope') }}</el-divider>
      <ResourceTreePicker v-model="sourceSelection" mode="node" :initial-locator="form.sourceLocator" :title="t('manager.rasterMosaic.sourceTreeTitle')" :engine-label="t('manager.rasterMosaic.engine')" tree-height="300px" />

      <el-divider content-position="left">{{ t('manager.rasterMosaic.placement') }}</el-divider>
      <el-form-item :label="t('manager.rasterMosaic.mode')">
        <el-radio-group v-model="form.mode">
          <el-radio-button value="detached">{{ t('manager.rasterMosaic.modes.detached') }}</el-radio-button>
          <el-radio-button value="in_place">{{ t('manager.rasterMosaic.modes.inPlace') }}</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-alert :title="modeHint" type="info" :closable="false" show-icon />
      <el-form-item v-if="form.mode === 'detached'" class="target-picker" :label="t('manager.rasterMosaic.targetScope')">
        <ResourceTreePicker v-model="targetSelection" mode="node" :initial-locator="form.targetLocator" :title="t('manager.rasterMosaic.targetTreeTitle')" :engine-label="t('manager.rasterMosaic.engine')" tree-height="300px" />
      </el-form-item>

      <div class="form-grid">
        <el-form-item :label="t('manager.rasterMosaic.datasetName')"><el-input v-model="form.datasetName" /></el-form-item>
        <el-form-item :label="t('manager.rasterMosaic.recursive')"><el-switch v-model="form.recursive" :active-text="t('manager.rasterMosaic.enabledYes')" :inactive-text="t('manager.rasterMosaic.enabledNo')" /></el-form-item>
      </div>
      <el-collapse>
        <el-collapse-item :title="t('manager.rasterMosaic.advancedOptions')" name="advanced">
          <div class="form-grid three">
            <el-form-item :label="t('manager.rasterMosaic.compression')"><el-select v-model="form.compression"><el-option label="DEFLATE" value="DEFLATE" /><el-option label="LZW" value="LZW" /><el-option label="ZSTD" value="ZSTD" /></el-select></el-form-item>
            <el-form-item :label="t('manager.rasterMosaic.blockSize')"><el-input-number v-model="form.blocksize" :min="128" :max="1024" :step="128" /></el-form-item>
            <el-form-item :label="t('manager.rasterMosaic.overviewResampling')"><el-select v-model="form.overviewResampling"><el-option label="NEAREST" value="NEAREST" /><el-option label="AVERAGE" value="AVERAGE" /><el-option label="BILINEAR" value="BILINEAR" /></el-select></el-form-item>
          </div>
        </el-collapse-item>
      </el-collapse>
    </el-form>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">{{ t('manager.rasterMosaic.cancel') }}</el-button>
      <el-button type="primary" :loading="saving" @click="save">{{ t('manager.rasterMosaic.save') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ResourceTreePicker } from '@addp/common-frontend'
import { quickViewAPI } from '@/api/quickView'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  task: { type: Object, default: null },
  locator: { type: String, default: '' }
})
const emit = defineEmits(['update:modelValue', 'saved', 'closed'])
const { t } = useI18n()
const saving = ref(false)
const sourceSelection = ref(null)
const targetSelection = ref(null)
const defaults = () => ({ name: '', description: '', enabled: true, mode: 'detached', sourceLocator: '', sourceEngineId: 0, targetLocator: '', targetEngineId: 0, datasetName: '', recursive: true, compression: 'DEFLATE', blocksize: 512, overviewResampling: 'NEAREST' })
const form = reactive(defaults())
const modeHint = computed(() => form.mode === 'in_place' ? t('manager.rasterMosaic.inPlaceHint') : t('manager.rasterMosaic.detachedHint'))
const selectionLocator = (selection, fallback) => String(selection?.identity?.locator || fallback || '').trim()
const selectionEngineID = (selection, fallback) => {
  const value = Number(selection?.identity?.engine_id || fallback || 0)
  return Number.isFinite(value) && value > 0 ? value : 0
}
const selectionEngineType = selection => String(selection?.display?.engine_type || selection?.raw?.engine?.engine_type || '').trim().toLowerCase()
const isObjectStoreSelection = selection => ['minio', 's3'].includes(selectionEngineType(selection))

function restore() {
  Object.assign(form, defaults())
  sourceSelection.value = null
  targetSelection.value = null
  if (!props.task) {
    form.sourceLocator = String(props.locator || '').trim()
    return
  }
  const config = props.task.config || {}
  const source = config.source || {}
  const target = config.target || {}
  const placement = config.placement || {}
  const cog = config.cog || {}
  Object.assign(form, {
    name: props.task.name || '', description: props.task.description || '', enabled: Boolean(props.task.enabled),
    mode: placement.mode || 'detached', sourceLocator: source.node_locator || '', sourceEngineId: Number(source.source_engine_id || 0),
    targetLocator: target.storage_locator || '', targetEngineId: Number(target.target_engine_id || 0), datasetName: target.dataset_name || '',
    recursive: source.recursive !== false, compression: cog.compression || 'DEFLATE', blocksize: Number(cog.blocksize || 512), overviewResampling: cog.overview_resampling || 'NEAREST'
  })
}
watch(() => [props.modelValue, props.task, props.locator], ([visible]) => { if (visible) restore() }, { immediate: true })

function validate() {
  const sourceLocator = selectionLocator(sourceSelection.value, form.sourceLocator)
  const sourceEngineId = selectionEngineID(sourceSelection.value, form.sourceEngineId)
  const targetLocator = form.mode === 'in_place' ? sourceLocator : selectionLocator(targetSelection.value, form.targetLocator)
  const targetEngineId = form.mode === 'in_place' ? sourceEngineId : selectionEngineID(targetSelection.value, form.targetEngineId)
  if (!String(form.name || '').trim()) { ElMessage.warning(t('manager.rasterMosaic.nameRequired')); return null }
  if (!sourceLocator || !sourceEngineId) { ElMessage.warning(t('manager.rasterMosaic.sourceRequired')); return null }
  if (form.mode === 'detached' && (!targetLocator || !targetEngineId)) { ElMessage.warning(t('manager.rasterMosaic.targetRequired')); return null }
  if (form.mode === 'detached' && sourceLocator === targetLocator) { ElMessage.warning(t('manager.rasterMosaic.targetMustDiffer')); return null }
  if (form.mode === 'in_place' && isObjectStoreSelection(sourceSelection.value)) { ElMessage.warning(t('manager.rasterMosaic.inPlaceObjectStoreUnsupported')); return null }
  if (!String(form.datasetName || '').trim()) { ElMessage.warning(t('manager.rasterMosaic.datasetNameRequired')); return null }
  return { sourceLocator, sourceEngineId, targetLocator, targetEngineId }
}
function body(validated) {
  return { name: String(form.name || '').trim(), description: String(form.description || '').trim(), enabled: Boolean(form.enabled), config: {
    source: { node_locator: validated.sourceLocator, source_engine_id: validated.sourceEngineId, recursive: Boolean(form.recursive), include_patterns: ['*.tif', '*.tiff'], exclude_patterns: [] },
    placement: { mode: form.mode },
    target: { storage_locator: validated.targetLocator, target_engine_id: validated.targetEngineId, dataset_name: String(form.datasetName || '').trim() },
    cog: { compression: form.compression, blocksize: Number(form.blocksize || 512), overview_resampling: form.overviewResampling, validate_source_cog: true },
    overview: { enabled: true, max_pixels: 64000000, resampling: 'AVERAGE' },
    tiles: { enabled: false, min_zoom: 0, max_zoom: 0, format: 'webp' }
  } }
}
async function save() {
  const validated = validate()
  if (!validated) return
  saving.value = true
  try {
    const payload = body(validated)
    if (props.task) payload.version = Number(props.task.version || 0)
    if (props.task) await quickViewAPI.updateRasterMosaicTask(props.task.id, payload)
    else await quickViewAPI.createRasterMosaicTask(payload)
    ElMessage.success(props.task ? t('manager.rasterMosaic.updateSuccess') : t('manager.rasterMosaic.createSuccess'))
    emit('update:modelValue', false)
    emit('saved')
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || error?.message || t('manager.rasterMosaic.saveFailed'))
  } finally { saving.value = false }
}
</script>

<style scoped>
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.form-grid.three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.target-picker { margin-top: 16px; }
@media (max-width: 760px) { .form-grid, .form-grid.three { grid-template-columns: 1fr; } }
</style>
