<template>
  <el-dialog :model-value="Boolean(context)" :title="t('workbench.selectionValue.title', { parameter: titleLabel })" width="min(920px, 94vw)" append-to-body destroy-on-close @update:model-value="!$event && emit('close')">
    <DataApplicationCanvas v-if="context" :key="context.sourceID" :application="context.application" :selection-source-id="context.sourceID" mode="draft-preview" embedded @selection="choose" />
    <template #footer><el-button @click="emit('close')">{{ t('workbench.cancel') }}</el-button></template>
  </el-dialog>
</template>
<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import DataApplicationCanvas from './DataApplicationCanvas.vue'
const props = defineProps({ context: { type: Object, default: null } })
const emit = defineEmits(['select', 'close'])
const { t } = useI18n()
const titleLabel = ref('')
watch(() => props.context?.label, label => { if (label !== undefined) titleLabel.value = label }, { immediate: true })
function choose(values) {
  if (!Object.prototype.hasOwnProperty.call(values, props.context.parameterKey)) return
  emit('select', values[props.context.parameterKey])
  emit('close')
}
</script>
