<template>
  <section v-loading="loading" class="field-masking-choices">
    <p>{{ t('fieldMaskingChoices.hint') }}</p>
    <el-button :disabled="loading || disabled" @click="$emit('refresh')">{{ t('fieldMaskingChoices.refresh') }}</el-button>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-form label-position="top" :disabled="disabled || loading || Boolean(error)">
      <el-form-item v-for="field in fields" :key="maskingFieldKey(field)" :label="field.component_key">
        <el-select :model-value="selected(field)" style="width: 100%" @change="value => choose(field, value)">
          <el-option :label="t('fieldMaskingChoices.default')" value="" />
          <el-option v-if="stale(field)" :value="'__unavailable__'" :label="t('fieldMaskingChoices.stale')" disabled />
          <el-option v-for="option in field.options" :key="option.id" :label="option.name" :value="option.id" />
        </el-select>
      </el-form-item>
    </el-form>
    <div v-for="choice in orphans" :key="maskingFieldKey(choice)" class="field-masking-stale">
      <el-alert :title="`${choice.component_key}: ${t('fieldMaskingChoices.stale')}`" type="warning" :closable="false" />
      <el-button :disabled="disabled || loading" @click="remove(choice)">{{ t('fieldMaskingChoices.remove') }}</el-button>
    </div>
    <el-empty v-if="!loading && !error && !fields.length && !orphans.length" :description="t('fieldMaskingChoices.empty')" :image-size="48" />
  </section>
</template>
<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { maskingFieldKey, currentMaskingChoice, isCurrentMaskingChoice, setMaskingChoice } from '../utils/fieldMaskingChoices.mjs'
const props = defineProps({ modelValue: { type: Array, default: () => [] }, fields: { type: Array, default: () => [] }, loading: Boolean, disabled: Boolean, error: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue', 'refresh'])
const { t } = useI18n()
const orphans = computed(() => props.modelValue.filter(choice => !props.fields.some(field => maskingFieldKey(field) === maskingFieldKey(choice))))
const stale = field => { const choice = currentMaskingChoice(field, props.modelValue); return choice && !isCurrentMaskingChoice(field, choice) }
const selected = field => stale(field) ? '__unavailable__' : currentMaskingChoice(field, props.modelValue)?.selection.option_id || ''
const choose = (field, value) => emit('update:modelValue', setMaskingChoice(props.fields, props.modelValue, field, value))
const remove = choice => emit('update:modelValue', props.modelValue.filter(item => maskingFieldKey(item) !== maskingFieldKey(choice)))
</script>
<style scoped>
.field-masking-choices { display: grid; gap: 12px; }
.field-masking-choices p { color: var(--addp-text-secondary); margin: 0; }
.field-masking-stale { display: grid; gap: 8px; }
</style>
