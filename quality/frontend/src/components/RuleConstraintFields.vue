<template>
  <div class="constraint-fields">
    <el-form-item
      v-if="type === 'allowed_values'"
      :label="t('quality.plan.allowedValues')"
    >
      <el-select
        :key="JSON.stringify(valueLabels)"
        v-model="params.values"
        :disabled="disabled"
        multiple
        filterable
        allow-create
        default-first-option
        :placeholder="t('quality.plan.allowedValuesPlaceholder')"
      >
        <el-option v-for="item in valueLabels" :key="item.code" :value="item.code" :label="item.label ? `${item.code} · ${item.label}` : item.code" />
      </el-select>
    </el-form-item>
    <template v-if="['format', 'length', 'value_range'].includes(type)">
      <el-form-item v-if="type === 'format'" :label="t('quality.plan.pattern')"
        ><el-input v-model="params.constraint.pattern" :disabled="disabled"
      /></el-form-item>
      <template v-else>
        <el-form-item :label="t('quality.plan.min')"
          ><el-input-number
            v-model="params.constraint.min"
            :disabled="disabled"
        /></el-form-item>
        <el-form-item :label="t('quality.plan.max')"
          ><el-input-number
            v-model="params.constraint.max"
            :disabled="disabled"
        /></el-form-item>
      </template>
    </template>
    <template v-if="type === 'row_count'">
      <el-form-item :label="t('quality.plan.rowCountMode')">
        <el-radio-group
          :model-value="
            params.mode || (Object.hasOwn(params, 'exact') ? 'exact' : 'range')
          "
          :disabled="disabled"
          @update:model-value="params.mode = $event"
        >
          <el-radio value="exact">{{
            t("quality.plan.rowCountExact")
          }}</el-radio
          ><el-radio value="range">{{
            t("quality.plan.rowCountRange")
          }}</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item
        v-if="
          (params.mode ||
            (Object.hasOwn(params, 'exact') ? 'exact' : 'range')) === 'exact'
        "
        :label="t('quality.plan.exact')"
        ><el-input-number
          v-model="params.exact"
          :min="0"
          :precision="0"
          :disabled="disabled"
      /></el-form-item>
      <template v-else
        ><el-form-item :label="t('quality.plan.min')"
          ><el-input-number
            v-model="params.min"
            :min="0"
            :precision="0"
            :disabled="disabled" /></el-form-item
        ><el-form-item :label="t('quality.plan.max')"
          ><el-input-number
            v-model="params.max"
            :min="0"
            :precision="0"
            :disabled="disabled" /></el-form-item
      ></template>
    </template>
    <template v-if="type === 'predicate_implication'">
      <el-form-item
        v-for="key in ['when', 'then']"
        :key="key"
        :label="t(`quality.plan.${key}Condition`)"
      >
        <div class="condition-fields">
          <el-select v-model="params[key].operator" :disabled="disabled"
            ><el-option
              v-for="op in PLAN_OPERATORS"
              :key="op"
              :value="op"
              :label="t(`quality.plan.operator_${op}`)"
          /></el-select>
          <template v-if="['eq', 'not_eq'].includes(params[key].operator)">
            <el-select
              :model-value="typeof params[key].value"
              :disabled="disabled"
              @update:model-value="
                params[key].value =
                  $event === 'number' ? 0 : $event === 'boolean' ? false : ''
              "
            >
              <el-option
                v-for="kind in ['string', 'number', 'boolean']"
                :key="kind"
                :value="kind"
                :label="t(`quality.rule.valueType_${kind}`)"
              />
            </el-select>
            <el-switch
              v-if="typeof params[key].value === 'boolean'"
              v-model="params[key].value"
              :disabled="disabled"
            />
            <el-input-number
              v-else-if="typeof params[key].value === 'number'"
              v-model="params[key].value"
              :disabled="disabled"
            />
            <el-input v-else v-model="params[key].value" :disabled="disabled" />
          </template>
        </div>
      </el-form-item>
    </template>
  </div>
</template>
<script setup>
import { useI18n } from "vue-i18n";
import { PLAN_OPERATORS } from "../utils/planContract";
defineProps({
  type: { type: String, required: true },
  params: { type: Object, required: true },
  disabled: Boolean,
  valueLabels: { type: Array, default: () => [] },
});
const { t } = useI18n();
</script>
<style scoped>
.constraint-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 260px), 1fr));
  gap: 0 16px;
}
.condition-fields {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}
.constraint-fields :deep(.el-select) {
  width: 100%;
}
</style>
