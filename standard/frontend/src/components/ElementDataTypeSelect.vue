<template>
  <div class="data-type-field">
    <el-select :model-value="modelValue" class="field-control" fit-input-width @update:model-value="$emit('update:modelValue', $event)" @change="$emit('change', $event)">
      <el-option v-for="type in ELEMENT_DATA_TYPES" :key="type" :value="type" :label="t(`standard.element.dataTypes.${type}.label`)" class="data-type-option">
        <span class="data-type-name">{{ t(`standard.element.dataTypes.${type}.label`) }}</span>
        <span class="data-type-example">{{ t('standard.element.dataTypeExample', { example: t(`standard.element.dataTypes.${type}.example`) }) }}</span>
      </el-option>
    </el-select>
    <p v-if="modelValue" class="data-type-hint" role="status">{{ t(`standard.element.dataTypes.${modelValue}.hint`) }}</p>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { ELEMENT_DATA_TYPES } from '../utils/standardRevisionForm'
defineProps({ modelValue: { type: String, required: true } })
defineEmits(['update:modelValue', 'change'])
const { t } = useI18n()
</script>

<style scoped>
.data-type-field, .field-control { width: 100%; min-width: 0; }
.data-type-option { height: auto; min-height: 58px; padding-top: 8px; padding-bottom: 8px; line-height: 1.5; white-space: normal; overflow-wrap: anywhere; }
.data-type-name, .data-type-example { display: block; }
.data-type-name { font-weight: 600; }
.data-type-example, .data-type-hint { color: var(--addp-text-secondary); font-size: 12px; font-weight: normal; }
.data-type-hint { margin: 6px 0 0; line-height: 1.6; overflow-wrap: anywhere; }
</style>
