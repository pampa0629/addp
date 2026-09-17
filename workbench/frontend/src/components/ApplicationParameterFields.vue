<template>
  <div class="parameter-grid">
    <div v-for="parameter in parameters" :key="parameter.key" class="parameter-field">
      <label class="parameter-input">
        <span>{{ parameter.label }}<em v-if="parameter.required">*</em></span>
        <ParameterValueInput :model-value="values[parameter.key]" :control-type="parameter.control_type" :options="domains[parameter.key].options" :disabled="!domains[parameter.key].ready" @update:model-value="emit('update-value', parameter.key, $event)" />
        <span v-if="!domains[parameter.key].ready">{{ t('workbench.parameterOptionsUnavailable', { components: domains[parameter.key].components.join(', ') }) }}</span>
      </label>
      <div v-if="selectionSources[parameter.key]?.length" class="parameter-selection-sources">
        <el-button v-for="source in selectionSources[parameter.key]" :key="source.id" link type="primary" @click="emit('focus-source', source.id)">{{ t('workbench.selectFromComponent', { component: source.title }) }}</el-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import ParameterValueInput from '../../../../common-frontend/basic/src/components/ParameterValueInput.vue'

defineProps({
  parameters: { type: Array, required: true },
  values: { type: Object, required: true },
  domains: { type: Object, required: true },
  selectionSources: { type: Object, required: true },
})
const emit = defineEmits(['update-value', 'focus-source'])
const { t } = useI18n()
</script>

<style scoped>
.parameter-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(220px, 100%), 1fr)); gap: 16px; }
.parameter-field, .parameter-input { min-width: 0; display: flex; flex-direction: column; gap: 8px; color: var(--addp-text-primary); }
.parameter-field em { color: var(--el-color-danger); font-style: normal; }
.parameter-selection-sources { display: flex; flex-wrap: wrap; gap: 8px; }
.parameter-selection-sources .el-button { margin: 0; white-space: normal; text-align: start; overflow-wrap: anywhere; }
</style>
