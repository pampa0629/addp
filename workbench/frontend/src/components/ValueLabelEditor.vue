<template>
  <section class="value-label-editor" data-testid="value-label-editor">
    <div class="value-label-header">
      <span>{{ t('workbench.valueLabels') }}</span>
      <el-button link type="primary" :disabled="modelValue.length >= 32" @click="add">{{ t('workbench.addValueLabel') }}</el-button>
    </div>
    <div v-for="(item, index) in modelValue" :key="index" class="value-label-row">
      <ParameterValueInput
        :model-value="item.value"
        :control-type="fieldType === 'bool' ? 'select' : 'text'"
        :aria-label="t('workbench.valueLabelValue')"
        @update:model-value="update(index, 'value', $event)"
      />
      <el-input :model-value="item.label" maxlength="100" :aria-label="t('workbench.valueLabelText')" :placeholder="t('workbench.valueLabelText')" @update:model-value="update(index, 'label', $event)" />
      <el-button link type="danger" @click="remove(index)">{{ t('workbench.delete') }}</el-button>
    </div>
    <span v-if="!valid" role="alert" class="value-label-error">{{ t('workbench.invalidValueLabels') }}</span>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ParameterValueInput } from '@common-ui'
import { valueLabelsValid } from '@common-ui/utils/fieldPresentation.mjs'

const props = defineProps({ modelValue: { type: Array, default: () => [] }, fieldType: { type: String, required: true } })
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const valid = computed(() => valueLabelsValid(props.modelValue, props.fieldType))
function add() {
  if (props.modelValue.length < 32) emit('update:modelValue', [...props.modelValue, { value: props.fieldType === 'bool' ? true : '', label: '' }])
}
function update(index, key, value) {
  emit('update:modelValue', props.modelValue.map((item, i) => i === index ? { ...item, [key]: value } : item))
}
function remove(index) {
  emit('update:modelValue', props.modelValue.filter((_, i) => i !== index))
}
</script>

<style scoped>
.value-label-editor { display: flex; flex-direction: column; gap: 8px; padding: 8px; border: 1px solid var(--addp-border-color-light); border-radius: 6px; }
.value-label-header { display: flex; align-items: center; justify-content: space-between; gap: 8px; font-size: 12px; color: var(--addp-text-secondary); }
.value-label-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto; gap: 8px; align-items: center; }
.value-label-error { color: var(--el-color-danger); font-size: 12px; }
@media (max-width: 600px) { .value-label-row { grid-template-columns: 1fr; } }
</style>
