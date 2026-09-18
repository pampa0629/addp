<template>
  <div class="parameter-grid">
    <div v-for="parameter in parameters" :key="parameter.key" class="parameter-field">
      <component :is="selectionSources[parameter.key]?.length ? 'div' : 'label'" class="parameter-input" @keydown="submitOnEnter($event, parameter)">
        <span>{{ parameter.label }}<em v-if="parameter.required">*</em></span>
        <ApplicationSelectionValue v-if="selectionSources[parameter.key]?.length" :refresh-key="refreshKey" :snapshot="snapshot" :descriptors="descriptors" :parameter-key="parameter.key" :parameter-values="values" :value="values[parameter.key]" :sources="selectionSources[parameter.key]" :required="parameter.required" :disabled="!domains[parameter.key].ready" @choose="emit('focus-source', $event)" @clear="emit('update-value', parameter.key, null)" />
        <ParameterValueInput v-else :model-value="values[parameter.key]" :control-type="parameter.control_type" :options="domains[parameter.key].options" :disabled="!domains[parameter.key].ready" @update:model-value="emit('update-value', parameter.key, $event)" />
        <span v-if="!domains[parameter.key].ready">{{ t('workbench.parameterOptionsUnavailable', { components: domains[parameter.key].components.join(', ') }) }}</span>
      </component>
    </div>
  </div>
</template>

<script setup>
import ApplicationSelectionValue from './ApplicationSelectionValue.vue'
import { useI18n } from 'vue-i18n'
import ParameterValueInput from '../../../../common-frontend/basic/src/components/ParameterValueInput.vue'

const props = defineProps({
  parameters: { type: Array, required: true },
  refreshKey: { type: Number, default: 0 },
  snapshot: { type: Object, required: true },
  descriptors: { type: Object, required: true },
  values: { type: Object, required: true },
  domains: { type: Object, required: true },
  selectionSources: { type: Object, required: true },
  submitEnabled: { type: Boolean, default: false },
})
const emit = defineEmits(['update-value', 'focus-source', 'submit'])
const { t } = useI18n()

function submitOnEnter(event, parameter) {
  const domain = props.domains[parameter.key]
  if (event.key !== 'Enter' || event.isComposing || event.keyCode === 229 || event.repeat || event.ctrlKey || event.altKey || event.metaKey || event.shiftKey) return
  if (!props.submitEnabled || !domain.ready || domain.options.length || parameter.control_type !== 'text' || event.target?.tagName !== 'INPUT') return
  event.preventDefault()
  emit('submit')
}
</script>

<style scoped>
.parameter-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(220px, 100%), 1fr)); gap: 16px; }
.parameter-field, .parameter-input { min-width: 0; display: flex; flex-direction: column; gap: 8px; color: var(--addp-text-primary); }
.parameter-field em { color: var(--el-color-danger); font-style: normal; }
</style>
