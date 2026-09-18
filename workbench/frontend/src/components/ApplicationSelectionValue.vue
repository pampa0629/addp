<template>
  <div class="selection-value" data-testid="selection-value">
    <span role="status">{{ display.status === 'resolved' ? display.label : t(`workbench.selectionValue.${display.status}`) }}</span>
    <el-button v-if="display.status === 'failed'" link type="primary" @click="retry += 1">{{ t('workbench.selectionValue.retry') }}</el-button>
    <el-button v-for="source in sources" :key="source.id" link type="primary" :disabled="disabled" @click="emit('choose', source.id)">{{ t('workbench.selectFromComponent', { component: source.title }) }}</el-button>
    <el-button v-if="!required && hasValue" link :disabled="disabled" @click="emit('clear')">{{ t('workbench.selectionValue.clear') }}</el-button>
  </div>
</template>
<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { executeDescriptorOperation } from '../api/services'
import { buildParameterDisplayQuery } from '../utils/applicationParameterDisplay.mjs'
import { createParameterDisplayResolver } from '../utils/parameterDisplayResolver.mjs'
import { useI18n } from 'vue-i18n'
const props = defineProps({ value: { default: null }, sources: { type: Array, required: true }, snapshot: { type: Object, required: true }, descriptors: { type: Object, required: true }, parameterKey: { type: String, required: true }, refreshKey: { type: Number, default: 0 }, parameterValues: { type: Object, required: true }, required: Boolean, disabled: Boolean })
const emit = defineEmits(['choose', 'clear'])
const { t } = useI18n()
const hasValue = computed(() => props.value !== null && props.value !== undefined && props.value !== '')
const retry = ref(0)
const display = ref({ status: 'selected', label: '' })
const resolver = createParameterDisplayResolver(executeDescriptorOperation, value => { display.value = value })
const queryContext = computed(() => {
  try {
    return JSON.stringify({ revision: [props.refreshKey, retry.value], query: buildParameterDisplayQuery(props.snapshot, props.descriptors, props.parameterKey, props.value, props.parameterValues), status: hasValue.value ? 'selected' : 'empty' })
  } catch { return JSON.stringify({ revision: [props.refreshKey, retry.value], query: null, status: 'unavailable' }) }
})
watch(queryContext, serialized => {
  const context = JSON.parse(serialized)
  void resolver.resolve(context.query, context.status)
}, { immediate: true, flush: 'sync' })
onBeforeUnmount(() => resolver.invalidate())
</script>
<style scoped>
.selection-value { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; min-width: 0; }
.selection-value > span { max-width: 100%; overflow-wrap: anywhere; color: var(--addp-text-secondary); }
.selection-value .el-button { margin: 0; white-space: normal; text-align: start; overflow-wrap: anywhere; }
</style>
