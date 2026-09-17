<template>
  <el-select
    :model-value="modelValue"
    filterable
    :remote="remote"
    :remote-method="remote ? search : undefined"
    :filter-method="remote ? undefined : search"
    :title="selectedPath"
    @update:model-value="$emit('update:modelValue', $event)"
    @visible-change="onVisibleChange"
  >
    <slot />
    <el-option v-for="option in visibleOptions" :key="option.id" :value="option.id" :label="option.name" :disabled="option.disabled">
      <span class="business-domain-option" :style="{ paddingInlineStart: `${option.depth * 16}px` }" :title="option.path.join(' / ')">
        <span class="business-domain-name">{{ showPath(option) ? option.path.join(' / ') : option.name }}</span>
        <span v-if="showCode && option.code" class="business-domain-code"> · {{ option.code }}</span>
        <slot name="suffix" :option="option" />
      </span>
    </el-option>
  </el-select>
</template>

<script setup>
import { computed, ref } from 'vue'
import { matchesBusinessDomain } from '../utils/businessDomainOptions.mjs'

const props = defineProps({
  modelValue: { type: [Number, String], default: null },
  options: { type: Array, default: () => [] },
  showCode: Boolean,
  remote: Boolean,
  remoteMethod: { type: Function, default: undefined }
})
const emit = defineEmits(['update:modelValue', 'visible-change'])
const query = ref('')
const visibleOptions = computed(() => props.remote ? props.options : props.options.filter(option => matchesBusinessDomain(option, query.value)))
const selectedPath = computed(() => props.options.find(option => option.id === props.modelValue)?.path.join(' / '))
function showPath(option) {
  if (!option.path.length) return false
  return query.value.trim() || (option.depth > 0 && !visibleOptions.value.some(parent => parent.path.length === option.path.length - 1 && parent.path.every((name, index) => name === option.path[index])))
}
function search(value) {
  query.value = value
  if (props.remote) props.remoteMethod?.(value)
}
function onVisibleChange(visible) {
  if (!visible) query.value = ''
  emit('visible-change', visible)
}
</script>

<style scoped>
.business-domain-option { display: flex; align-items: center; min-width: 0; max-width: min(36rem, calc(100vw - 64px)); box-sizing: border-box; }
.business-domain-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.business-domain-code { flex-shrink: 0; color: var(--addp-text-secondary); white-space: pre; }
</style>
