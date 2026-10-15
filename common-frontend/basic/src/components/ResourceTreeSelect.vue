<template>
  <div class="resource-tree-select">
    <el-popover :visible="visible" placement="bottom-start" :trigger="[]" :trigger-keys="[]" :width="560" :popper-style="{ maxWidth: 'calc(100vw - 48px)' }" @update:visible="visible = $event">
      <template #reference>
        <el-input v-bind="$attrs" ref="input" :model-value="modelValue" :disabled="disabled" :prefix-icon="Search" :placeholder="placeholder" clearable @input="edit" @keydown.esc.stop="close" @keydown.down.prevent="browse">
          <template #append><el-button class="resource-tree-select__toggle" :icon="ArrowDown" :aria-label="treeLabel" :aria-expanded="visible" :disabled="disabled || treeDisabled || !adapter" @click.stop="toggle" /></template>
        </el-input>
      </template>
      <div v-if="visible && adapter" class="resource-tree-select__panel">
        <el-alert v-if="failed" type="error" :closable="false" :title="errorText" />
        <ResourceTreePicker :adapter="adapter" :engine-id="engineId" :model-value="selection" :search-query="query" mode="any" :show-engine-selector="false" :show-search="false" :show-selection-summary="false" :search-selectable-only="true" :selectable-filter="selectableFilter" :title="title" tree-height="320px" @select="select" @error="onError" />
      </div>
    </el-popover>
  </div>
</template>

<script setup>
import { nextTick, ref, watch } from 'vue'
import { ArrowDown, Search } from '@element-plus/icons-vue'
import ResourceTreePicker from './ResourceTreePicker.vue'

defineOptions({ inheritAttrs: false })
const props = defineProps({
  modelValue: { type: String, default: '' },
  selection: { type: Object, default: null },
  adapter: { type: Object, default: null },
  engineId: { type: [Number, String], required: true },
  selectableFilter: { type: Function, required: true },
  searchResources: Boolean,
  disabled: Boolean,
  treeDisabled: Boolean,
  scopeKey: { type: String, default: '' },
  placeholder: { type: String, required: true },
  treeLabel: { type: String, required: true },
  title: { type: String, required: true },
  errorText: { type: String, required: true }
})
const emit = defineEmits(['update:modelValue', 'input', 'select', 'error'])
const input = ref(null), visible = ref(false), query = ref(''), failed = ref(false)
function close() { visible.value = false; query.value = ''; failed.value = false }
function browse() {
  if (props.disabled || props.treeDisabled || !props.adapter) return
  query.value = ''; failed.value = false; visible.value = true
}
function toggle() { if (visible.value) close(); else browse() }
function edit(value) {
  emit('update:modelValue', value); emit('input', value)
  failed.value = false
  if (props.searchResources && !props.treeDisabled && props.adapter && !props.disabled) {
    query.value = value; visible.value = true
  } else close()
}
function select(value) {
  if (props.disabled || props.treeDisabled || !value) return
  close(); emit('select', value)
  nextTick(() => input.value?.focus())
}
function onError(error) { failed.value = true; emit('error', error) }
// A benign owner refresh may replace the adapter object for the same engine.
// Availability and the explicit identity scope, not object identity, close it.
watch([() => props.disabled, () => props.treeDisabled, () => !!props.adapter, () => props.engineId, () => props.scopeKey], close)
defineExpose({ focus: () => input.value?.focus(), close })
</script>

<style scoped>
.resource-tree-select { width: 100%; min-width: 0; }
.resource-tree-select__panel { min-width: 0; }
.resource-tree-select__panel :deep(.search-result-path),
.resource-tree-select__panel :deep(.picker-node-label) { overflow-wrap: anywhere; }
</style>
