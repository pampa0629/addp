<template>
  <div class="assertion-node" :data-assertion-op="modelValue.op">
    <div class="assertion-head">
      <el-select :model-value="modelValue.op" :disabled="disabled" :aria-label="t('quality.assertion.operator')" @update:model-value="emit('update:modelValue', defaultAssertion($event))">
        <el-option v-for="op in operators" :key="op" :value="op" :label="t(`quality.assertion.op_${op}`)" />
      </el-select>
      <template v-if="modelValue.op === 'field'">
        <el-select :model-value="modelValue.relation || ''" :disabled="disabled" :aria-label="t('quality.assertion.relation')" @update:model-value="setRelation">
          <el-option value="" :label="t('quality.assertion.root')" />
          <el-option v-for="role in roles" :key="role" :value="role" :label="role" />
        </el-select>
        <el-input v-model="modelValue.field" :disabled="disabled" :placeholder="t('quality.assertion.symbol')" :aria-label="t('quality.assertion.symbol')" />
      </template>
      <template v-if="modelValue.op === 'value'">
        <el-select :model-value="modelValue.value === null ? 'null' : typeof modelValue.value" :disabled="disabled" :aria-label="t('quality.assertion.valueType')" @update:model-value="modelValue.value = $event === 'boolean' ? false : $event === 'null' ? null : ''">
          <el-option v-for="kind in ['string', 'boolean', 'null']" :key="kind" :value="kind" :label="t(`quality.assertion.value_${kind}`)" />
        </el-select>
        <el-switch v-if="typeof modelValue.value === 'boolean'" v-model="modelValue.value" :disabled="disabled" :aria-label="t('quality.assertion.value')" />
        <el-input v-else-if="modelValue.value !== null" v-model="modelValue.value" :disabled="disabled" :aria-label="t('quality.assertion.value')" />
      </template>
      <el-input v-if="modelValue.op === 'number'" v-model="modelValue.value" inputmode="decimal" :disabled="disabled" :aria-label="t('quality.assertion.value')" />
      <template v-if="collection">
        <el-input v-model="modelValue.relation" :disabled="disabled" :placeholder="t('quality.assertion.relation')" :aria-label="t('quality.assertion.relation')" />
        <el-input v-if="modelValue.op === 'count_distinct'" v-model="modelValue.field" :disabled="disabled" :placeholder="t('quality.assertion.symbol')" :aria-label="t('quality.assertion.symbol')" />
      </template>
    </div>
    <div v-for="(arg, index) in modelValue.args || []" :key="index" class="assertion-child">
      <AssertionEditor v-model="modelValue.args[index]" :disabled="disabled" :roles="roles" :depth="depth + 1" />
      <el-button v-if="!disabled && ['and', 'or'].includes(modelValue.op) && modelValue.args.length > 2" link type="danger" @click="modelValue.args.splice(index, 1)">{{ t('quality.assertion.remove') }}</el-button>
    </div>
    <el-button v-if="!disabled && ['and', 'or'].includes(modelValue.op) && modelValue.args.length < 32 && depth < 12" link @click="modelValue.args.push(defaultAssertion())">{{ t('quality.assertion.add') }}</el-button>
    <template v-if="collection">
      <el-checkbox :model-value="Boolean(modelValue.where)" :disabled="disabled || (depth >= 12 && !modelValue.where)" @update:model-value="$event ? modelValue.where = defaultAssertion() : delete modelValue.where">{{ t('quality.assertion.filter') }}</el-checkbox>
      <AssertionEditor v-if="modelValue.where" v-model="modelValue.where" :disabled="disabled" :roles="[...roles, modelValue.relation].filter(Boolean)" :depth="depth + 1" />
    </template>
  </div>
</template>
<script setup>
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { ASSERTION_OPERATORS, ASSERTION_COLLECTIONS, defaultAssertion } from '../utils/assertion';
const props = defineProps({ modelValue: { type: Object, required: true }, disabled: Boolean, roles: { type: Array, default: () => [] }, depth: { type: Number, default: 1 } });
const emit = defineEmits(['update:modelValue']);
const { t } = useI18n();
const collection = computed(() => ASSERTION_COLLECTIONS.includes(props.modelValue.op));
const operators = computed(() => props.depth < 12 ? ASSERTION_OPERATORS : ['field', 'value', 'number', 'today', 'count', 'count_distinct', 'exists']);
const setRelation = (role) => { if (role) props.modelValue.relation = role; else delete props.modelValue.relation; };
</script>
<style scoped>
.assertion-node { border-left: 2px solid var(--el-border-color); padding: 8px 0 8px 12px; min-width: 0; }
.assertion-head { display: flex; flex-wrap: wrap; gap: 8px; }
.assertion-head > * { flex: 1 1 130px; min-width: 100px; }
.assertion-child { margin-top: 8px; }
</style>
