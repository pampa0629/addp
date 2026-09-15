<template>
  <el-select v-if="options.length" :model-value="modelValue" :disabled="disabled" clearable @update:model-value="update">
    <el-option v-for="option in options" :key="JSON.stringify(option.value)" :value="option.value" :label="option.labels[locale]" />
  </el-select>
  <el-input-number v-else-if="controlType === 'number'" :model-value="modelValue" :disabled="disabled" :controls="false" @update:model-value="update" />
  <el-switch v-else-if="controlType === 'checkbox'" :model-value="Boolean(modelValue)" :disabled="disabled" @update:model-value="update" />
  <el-date-picker
    v-else-if="controlType === 'date' || controlType === 'datetime'"
    :model-value="modelValue" :disabled="disabled"
    :type="controlType === 'datetime' ? 'datetime' : 'date'"
    :value-format="controlType === 'datetime' ? 'YYYY-MM-DDTHH:mm:ssZ' : 'YYYY-MM-DD'"
    @update:model-value="update"
  />
  <el-select v-else-if="controlType === 'select'" :model-value="modelValue" :disabled="disabled" clearable @update:model-value="update">
    <el-option :value="true" :label="t('common.yes')" />
    <el-option :value="false" :label="t('common.no')" />
  </el-select>
  <el-select
    v-else-if="controlType === 'multiselect'"
    :model-value="modelValue" :disabled="disabled"
    multiple
    filterable
    allow-create
    default-first-option
    @update:model-value="update"
  />
  <div v-else-if="controlType === 'bbox'" class="bbox-inputs">
    <el-input-number
      v-for="position in 4"
      :key="position"
      :model-value="Array.isArray(modelValue) ? modelValue[position - 1] : undefined"
      :controls="false"
      :disabled="disabled"
      @update:model-value="updateBbox(position - 1, $event)"
    />
  </div>
  <el-input v-else :model-value="modelValue" :disabled="disabled" @update:model-value="update" />
</template>

<script setup>
import { useI18n } from 'vue-i18n'

const props = defineProps({ modelValue: { default: '' }, controlType: { type: String, required: true }, options: { type: Array, default: () => [] }, disabled: Boolean })
const emit = defineEmits(['update:modelValue'])
const { t, locale } = useI18n()
const update = (value) => emit('update:modelValue', value)
const updateBbox = (index, value) => {
  const next = Array.isArray(props.modelValue) ? [...props.modelValue] : ['', '', '', '']
  next[index] = value
  emit('update:modelValue', next)
}
</script>

<style scoped>
.bbox-inputs{display:grid;grid-template-columns:1fr 1fr;gap:4px}
</style>
