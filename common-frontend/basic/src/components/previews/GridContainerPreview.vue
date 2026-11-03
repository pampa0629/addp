<template>
  <ContainerPreview class="grid-container-preview" :children="children" :selected-child-key="selectedName" :selector-label="t('gridPreview.variable')" @child-change="selectVariable">
    <template #child-details="{ child }">
      <div class="grid-details" data-testid="grid-variable-details">
        <el-descriptions border size="small" :column="2">
          <el-descriptions-item :label="t('gridPreview.type')">{{ child.native?.type }}</el-descriptions-item>
          <el-descriptions-item :label="t('gridPreview.dimensions')">{{ child.native?.dimensions?.map(d => `${d.name} [${d.size}]`).join(' × ') }}</el-descriptions-item>
          <el-descriptions-item v-if="child.native?.grid" :label="t('gridPreview.grid')"><span data-testid="grid-native-transform">{{ child.native.grid.transform?.join(', ') }}</span></el-descriptions-item>
          <el-descriptions-item v-for="(attribute, key) in child.native?.attributes" :key="key" :label="key">{{ attribute.values?.join(', ') }}</el-descriptions-item>
        </el-descriptions>
      </div>
    </template>
    <template #child-preview="{ child }">
      <template v-if="child.native?.slice_supported">
        <el-form class="grid-controls" inline @submit.prevent="read">
          <el-form-item v-for="(dimension, index) in child.native.dimensions.slice(0, -2)" :key="dimension.name" :label="dimension.name">
            <el-input-number v-model="indices[index]" :min="0" :max="indexLimit(dimension.size)" :precision="0" :aria-label="dimension.name" />
          </el-form-item>
          <el-form-item v-for="key in ['x', 'y', 'width', 'height']" :key="key" :label="t(`gridPreview.${key}`)">
            <el-input-number v-model="window[key]" :min="key === 'x' || key === 'y' ? 0 : 1" :precision="0" :aria-label="t(`gridPreview.${key}`)" />
          </el-form-item>
          <el-button type="primary" :disabled="!selectionValid" :loading="loading" data-testid="read-grid-slice" @click="read">{{ t('gridPreview.read') }}</el-button>
        </el-form>
        <el-alert :title="t(`gridPreview.${valueModeKey}`)" type="info" :closable="false" show-icon />
        <el-alert v-if="error" :title="error" type="error" :closable="false" role="alert" />
        <template v-if="slice">
          <div class="grid-window" data-testid="grid-slice-result">
            <table>
              <thead><tr><th></th><th v-for="(_, x) in slice.values[0]" :key="x">{{ slice.selection.x + x }}</th></tr></thead>
              <tbody><tr v-for="(row, y) in slice.values" :key="y"><th>{{ slice.selection.y + y }}</th>
                <td v-for="(value, x) in row" :key="x" :data-valid="slice.valid[y][x]" :data-raw="value" :style="cellStyle(value, slice.valid[y][x])" :title="value">{{ slice.valid[y][x] ? value : t('gridPreview.missing') }}</td>
              </tr></tbody>
            </table>
          </div>
          <slot v-if="slice.spatial" name="spatial" :spatial="slice.spatial" />
          <el-text v-else type="info" data-testid="grid-no-location">{{ t(child.native?.grid ? 'gridPreview.noMapPreview' : 'gridPreview.noLocation') }}</el-text>
        </template>
      </template>
      <el-empty v-else :description="t('gridPreview.unsupportedVariable')" />
    </template>
  </ContainerPreview>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ContainerPreview from './ContainerPreview.vue'
import { gridColorValues, gridWindowLimit, gridValueModeKey, validateGridSelection } from '../../utils/gridSlice.js'
const props = defineProps({ variables: { type: Array, required: true }, loadSlice: { type: Function, required: true } })
const { t } = useI18n()
const selectedName = ref('')
const indices = ref([])
const window = reactive({ x: 0, y: 0, width: 1, height: 1 })
const slice = ref(null)
const error = ref('')
const loading = ref(false)
let request = null
let sequence = 0
const children = computed(() => props.variables.map(variable => ({ key: variable.name, name: variable.name, native: variable })))
const variable = computed(() => props.variables.find(item => item.name === selectedName.value))
const selection = computed(() => ({ variable: selectedName.value, indices: [...indices.value], ...window }))
const valueModeKey = computed(() => gridValueModeKey(variable.value?.value_mode))
const selectionValid = computed(() => Boolean(valueModeKey.value) && validateGridSelection(variable.value, selection.value))
const indexLimit = size => Number(BigInt(size) - 1n > BigInt(Number.MAX_SAFE_INTEGER) ? BigInt(Number.MAX_SAFE_INTEGER) : BigInt(size) - 1n)
function clear() { sequence++; request?.abort(); slice.value = null; error.value = ''; loading.value = false }
function reset() {
  clear()
  const dims = variable.value?.dimensions || []
  indices.value = dims.slice(0, -2).map(() => 0)
  Object.assign(window, { x: 0, y: 0, width: Math.min(64, gridWindowLimit(dims.at(-1)?.size)), height: Math.min(64, gridWindowLimit(dims.at(-2)?.size)) })
}
function selectVariable(child) { selectedName.value = child.name; reset() }
watch(() => props.variables, () => { selectedName.value = (props.variables.find(v => v.slice_supported) || props.variables[0])?.name || ''; reset() }, { immediate: true })
// Any changed selection invalidates the visible old slice, including during I/O.
watch(selection, clear, { flush: 'sync' })
async function read() {
  if (!selectionValid.value) return
  clear(); const current = sequence; request = new AbortController(); loading.value = true
  try {
    const result = await props.loadSlice(selection.value, request.signal)
    if (current === sequence) {
      if (result.value_mode !== variable.value?.value_mode) throw new Error(t('gridPreview.failed'))
      slice.value = result
    }
  } catch (failure) {
    if (current === sequence && !request.signal.aborted) error.value = failure?.response?.data?.error || failure?.message || t('gridPreview.failed')
  } finally { if (current === sequence) loading.value = false }
}
const colorValues = computed(() => slice.value ? gridColorValues(slice.value.values, slice.value.valid) : [])
const colorRange = computed(() => [Math.min(...colorValues.value), Math.max(...colorValues.value)])
function cellStyle(text, valid) {
  if (!valid || !gridColorValues([[text]], [[true]]).length || !colorValues.value.length) return {}
  const [low, high] = colorRange.value
  const percent = high === low || !Number.isFinite(high - low) ? 20 : 10 + Math.round((Number(text) - low) / (high - low) * 60)
  return { background: `color-mix(in srgb, var(--el-color-primary) ${percent}%, var(--el-bg-color))` }
}
onBeforeUnmount(clear)
</script>

<style scoped>
.grid-container-preview :deep(.child-preview) { overflow: auto; }
.grid-container-preview :deep(.child-preview > *) { flex-shrink: 0; }
.grid-details { margin-bottom: 12px; overflow-wrap: anywhere; }
.grid-controls { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 12px; }
.grid-window { max-height: 480px; overflow: auto; margin: 12px 0; }
table { border-collapse: collapse; font-variant-numeric: tabular-nums; white-space: nowrap; }
th, td { border: 1px solid var(--el-border-color); padding: 6px 10px; color: var(--el-text-color-primary); }
th { background: var(--el-fill-color-light); }
</style>
