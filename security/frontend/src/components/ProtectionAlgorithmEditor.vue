<template>
  <el-alert v-if="loadError" :title="t('security.algorithms.loadFailed')" type="error" :closable="false">
    <el-button :loading="loading" @click="load">{{ t('security.common.refresh') }}</el-button>
  </el-alert>
  <el-form-item :label="t('security.fields.algorithm')" required>
    <el-select :model-value="configuration.algorithm" :loading="loading" :disabled="loading || loadError" @update:model-value="selectAlgorithm">
      <el-option v-for="algorithm in options" :key="algorithm.key" :value="algorithm.key" :label="t(algorithm.name_i18n_key)" />
    </el-select>
    <div v-if="selected" class="field-help">{{ t(selected.description_i18n_key) }}</div>
  </el-form-item>
  <template v-if="configuration.algorithm === prefixAlgorithm">
    <el-form-item :label="t('security.fields.keep_prefix')" :prop="parameterPath('prefix_runes')" :rules="integerRule('keep_prefix')" required>
      <el-input-number v-model="configuration.parameters.prefix_runes" :min="0" :max="baseline?.parameters?.prefix_runes" controls-position="right" />
    </el-form-item>
    <el-form-item :label="t('security.fields.keep_suffix')" :prop="parameterPath('suffix_runes')" :rules="integerRule('keep_suffix')" required>
      <el-input-number v-model="configuration.parameters.suffix_runes" :min="0" :max="baseline?.parameters?.suffix_runes" controls-position="right" />
    </el-form-item>
    <el-form-item :label="t('security.algorithms.maskRune')" required>
      <el-input v-model="configuration.parameters.mask_rune" />
    </el-form-item>
  </template>
  <template v-if="configuration.algorithm === constantAlgorithm">
    <el-form-item :label="t('security.algorithms.valueType')">
      <el-select :model-value="typeof configuration.parameters.value" @update:model-value="changeValueType">
        <el-option value="string" :label="t('security.algorithms.types.string')" />
        <el-option value="number" :label="t('security.algorithms.types.number')" />
        <el-option value="boolean" :label="t('security.algorithms.types.boolean')" />
      </el-select>
    </el-form-item>
    <el-form-item :label="t('security.algorithms.value')" required>
      <el-input v-if="typeof configuration.parameters.value === 'string'" v-model="configuration.parameters.value" maxlength="4096" />
      <el-input-number v-else-if="typeof configuration.parameters.value === 'number'" v-model="configuration.parameters.value" />
      <el-switch v-else v-model="configuration.parameters.value" />
    </el-form-item>
  </template>
  <el-form-item v-if="defaults" :label="t('security.algorithms.allowed')" required>
    <el-checkbox-group v-model="configuration.allowed_algorithms">
      <el-checkbox v-for="algorithm in catalog" :key="algorithm.key" :value="algorithm.key" :disabled="algorithm.key === configuration.algorithm || (algorithm.key === prefixAlgorithm && configuration.algorithm !== prefixAlgorithm)">
        {{ t(algorithm.name_i18n_key) }}
      </el-checkbox>
    </el-checkbox-group>
    <div class="field-help">{{ t('security.algorithms.allowedHelp') }}</div>
  </el-form-item>
  <el-alert v-if="invalid" :title="t('security.algorithms.invalid')" type="error" :closable="false" />
  <el-collapse v-if="catalog.length">
    <el-collapse-item :title="t('security.algorithms.catalog')">
      <div v-for="algorithm in catalog" :key="algorithm.key" class="algorithm-description">
        <strong>{{ t(algorithm.name_i18n_key) }}</strong>
        <p>{{ t(algorithm.description_i18n_key) }}</p>
        <small>{{ t('security.algorithms.supportedTypes') }}：{{ algorithm.supported_field_types.map(type => t(`security.algorithms.fieldTypes.${type}`)).join(', ') }}</small>
      </div>
    </el-collapse-item>
  </el-collapse>
</template>

<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { createNonNegativeIntegerRule } from '../utils/foundationForm.mjs'
import { protectionAlgorithmAPI } from '../api/security'
import { prefixAlgorithm, constantAlgorithm, algorithmParameters, validateProtectionConfiguration } from '../utils/protectionAlgorithm.mjs'

const props = defineProps({ configuration: { type: Object, required: true }, defaults: Boolean, baseline: Object, fieldType: String, path: {type:String,default:''} })
const { t } = useI18n()
const catalog = ref([])
const loading = ref(false)
const loadError = ref(false)
const invalid = ref(false)
let disposed = false
const options = computed(() => catalog.value.filter(item => (!props.baseline || props.baseline.allowed_algorithms?.includes(item.key)) && (!props.fieldType || item.supported_field_types.includes(props.fieldType))))
const selected = computed(() => catalog.value.find(item => item.key === props.configuration.algorithm))
async function load() {
  loading.value = true
  try {
    const result = await protectionAlgorithmAPI.list()
    if (!disposed) { catalog.value = Array.isArray(result) ? result : []; loadError.value = !catalog.value.length }
  } catch { if (!disposed) loadError.value = true }
  finally { if (!disposed) loading.value = false }
}
function selectAlgorithm(key) {
  props.configuration.algorithm = key
  props.configuration.parameters = algorithmParameters(key)
  if (props.baseline && key === prefixAlgorithm) props.configuration.parameters = { ...props.baseline.parameters }
  if (props.defaults) props.configuration.allowed_algorithms = [...new Set([...(props.configuration.allowed_algorithms || []).filter(item => key === prefixAlgorithm || item !== prefixAlgorithm), key])]
  invalid.value = false
}
function changeValueType(type) { props.configuration.parameters.value = type === 'number' ? 0 : type === 'boolean' ? false : '' }
function parameterPath(key) { return `${props.path ? props.path + '.' : ''}parameters.${key}` }
function integerRule(key) { return [createNonNegativeIntegerRule(t('security.common.requiredField', {name:t(`security.fields.${key}`)}))] }
function validate() {
  const valid = !loading.value && !loadError.value && validateProtectionConfiguration(props.configuration, catalog.value, props)
  invalid.value = !valid
  return valid
}
onMounted(load)
onBeforeUnmount(() => { disposed = true })
defineExpose({ validate })
</script>

<style scoped>
.field-help { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.6; width: 100%; margin-top: 6px; }
.algorithm-description { margin-bottom: 12px; }
.algorithm-description p { margin: 4px 0; }
</style>
