<template>
  <section class="state-rule-editor">
    <div class="state-rule-header">
      <span>{{ t('workbench.stateRules') }}</span>
      <el-button link type="primary" :disabled="rules.length >= 8" @click="addRule">{{ t('workbench.addStateRule') }}</el-button>
    </div>
    <div v-for="(rule, index) in rules" :key="index" class="state-rule-row">
      <el-select :model-value="rule.operator" :placeholder="t('workbench.stateOperator')" @update:model-value="updateRule(index, 'operator', $event)">
        <el-option v-for="operator in operators" :key="operator" :value="operator" :label="t(`workbench.stateOperators.${operator}`)" />
      </el-select>
      <el-input-number
        v-if="numeric"
        :model-value="rule.operand"
        :precision="integer ? 0 : undefined"
        :controls="false"
        :placeholder="t('workbench.stateOperand')"
        @update:model-value="updateRule(index, 'operand', $event)"
      />
      <el-select v-else-if="fieldType === 'bool'" :model-value="rule.operand" :placeholder="t('workbench.stateOperand')" @update:model-value="updateRule(index, 'operand', $event)">
        <el-option :value="true" :label="t('workbench.booleanValues.true')" />
        <el-option :value="false" :label="t('workbench.booleanValues.false')" />
      </el-select>
      <el-input v-else :model-value="rule.operand" :placeholder="t('workbench.stateOperand')" @update:model-value="updateRule(index, 'operand', $event)" />
      <el-input :model-value="rule.label" maxlength="50" :placeholder="t('workbench.stateLabel')" @update:model-value="updateRule(index, 'label', $event)" />
      <el-select :model-value="rule.tone" :placeholder="t('workbench.stateTone')" @update:model-value="updateRule(index, 'tone', $event)">
        <el-option v-for="tone in tones" :key="tone" :value="tone" :label="t(`workbench.stateTones.${tone}`)" />
      </el-select>
      <div class="state-rule-actions">
        <el-button link :disabled="index === 0" :aria-label="t('workbench.moveStateRuleUp')" :title="t('workbench.moveStateRuleUp')" @click="moveRule(index, -1)">↑</el-button>
        <el-button link :disabled="index === rules.length - 1" :aria-label="t('workbench.moveStateRuleDown')" :title="t('workbench.moveStateRuleDown')" @click="moveRule(index, 1)">↓</el-button>
        <el-button link type="danger" @click="removeRule(index)">{{ t('workbench.delete') }}</el-button>
      </div>
    </div>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  fieldType: { type: String, required: true },
})
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const numericTypes = new Set(['int', 'bigint', 'float', 'double', 'decimal'])
const tones = ['info', 'success', 'warning', 'danger']
const rules = computed(() => props.modelValue || [])
const numeric = computed(() => numericTypes.has(props.fieldType))
const integer = computed(() => ['int', 'bigint'].includes(props.fieldType))
const operators = computed(() => numeric.value ? ['eq', 'lt', 'lte', 'gt', 'gte'] : ['eq'])

function addRule() {
  if (rules.value.length >= 8) return
  const operand = numeric.value ? 0 : props.fieldType === 'bool' ? true : ''
  emit('update:modelValue', [...rules.value, { operator: 'eq', operand, label: '', tone: 'info' }])
}

function updateRule(index, field, value) {
  emit('update:modelValue', rules.value.map((rule, ruleIndex) => ruleIndex === index ? { ...rule, [field]: value } : rule))
}

function moveRule(index, offset) {
  const target = index + offset
  if (target < 0 || target >= rules.value.length) return
  const next = rules.value.map((rule) => ({ ...rule }))
  const moved = next[index]
  next[index] = next[target]
  next[target] = moved
  emit('update:modelValue', next)
}

function removeRule(index) {
  emit('update:modelValue', rules.value.filter((_, ruleIndex) => ruleIndex !== index))
}
</script>

<style scoped>
.state-rule-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 8px;
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color-light);
  border-radius: 6px;
}

.state-rule-header,
.state-rule-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
}

.state-rule-header span {
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.state-rule-row {
  display: grid;
  grid-template-columns: minmax(100px, .7fr) minmax(110px, .8fr) minmax(120px, 1fr) minmax(100px, .7fr) auto;
  gap: 8px;
  align-items: center;
}

.state-rule-actions {
  justify-content: flex-end;
}

@media (max-width: 1000px) {
  .state-rule-row { grid-template-columns: 1fr 1fr; }
}
</style>
